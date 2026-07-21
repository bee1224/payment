package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/provider"
	"payment-service/internal/repository"
)

type DepositService struct {
	mu                  sync.Mutex
	nextID              int64
	orders              map[string]domain.DepositOrder
	payments            map[string]provider.DepositPaymentRequest
	gateways            map[string]provider.DepositGateway
	channelProviders    map[string]string
	ledger              *LedgerService
	store               repository.DepositStore
	now                 func() time.Time
	orderTTL            time.Duration
	callbackBuilder     func(domain.DepositOrder) (string, []byte, error)
	callbackSender      func(string, []byte) error
	deliveryEngine      domain.CallbackDeliveryEngine
	callbackSigningKeys repository.CallbackSigningKeyResolver
}

const maxDepositCallbackRetries = 8

type adminDepositLister interface {
	ListDepositOrdersForAdmin(context.Context, int) ([]domain.DepositOrder, error)
}

type AdminDepositOrderView struct {
	OrderNo         string                    `json:"order_no"`
	MerchantCode    string                    `json:"merchant_code"`
	MerchantOrderNo string                    `json:"merchant_order_no"`
	ChannelCode     string                    `json:"channel_code"`
	BankID          string                    `json:"bank_id"`
	BankAccounts    []string                  `json:"bank_accounts"`
	UserName        string                    `json:"user_name"`
	Amount          int64                     `json:"amount"`
	Currency        string                    `json:"currency"`
	Status          domain.DepositOrderStatus `json:"status"`
	ItemDesc        string                    `json:"item_desc"`
	ExpiresAt       *time.Time                `json:"expires_at,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

func BuildAdminDepositOrderView(order domain.DepositOrder) AdminDepositOrderView {
	return AdminDepositOrderView{OrderNo: order.OrderNo, MerchantCode: order.MerchantCode, MerchantOrderNo: order.MerchantOrderNo, ChannelCode: order.ChannelCode, BankID: order.BankID, BankAccounts: append([]string(nil), order.BankAccounts...), UserName: order.UserName, Amount: order.AmountCents, Currency: order.Currency, Status: order.Status, ItemDesc: order.ItemDesc, ExpiresAt: order.ExpiresAt, CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt}
}

func (s *DepositService) ListDepositOrdersForAdmin(ctx context.Context, limit int) ([]AdminDepositOrderView, error) {
	var orders []domain.DepositOrder
	if lister, ok := s.store.(adminDepositLister); ok {
		var err error
		orders, err = lister.ListDepositOrdersForAdmin(ctx, limit)
		if err != nil {
			return nil, err
		}
	} else {
		s.mu.Lock()
		for _, order := range s.orders {
			orders = append(orders, order)
		}
		s.mu.Unlock()
		sort.Slice(orders, func(i, j int) bool { return orders[i].CreatedAt.After(orders[j].CreatedAt) })
		if limit > 0 && len(orders) > limit {
			orders = orders[:limit]
		}
	}
	views := make([]AdminDepositOrderView, 0, len(orders))
	for _, order := range orders {
		views = append(views, BuildAdminDepositOrderView(order))
	}
	return views, nil
}

type CreateDepositRequest struct {
	MerchantID      string   `json:"merchant_id"`
	MerchantOrderNo string   `json:"merchant_order_no"`
	Amount          int64    `json:"amount"`
	Currency        string   `json:"currency"`
	ItemDesc        string   `json:"item_desc"`
	ChannelCode     string   `json:"channel_code"`
	NotifyURL       string   `json:"notify_url"`
	ProviderCode    string   `json:"provider_code,omitempty"`
	BankAccounts    []string `json:"bank_account"`
	StoreNumbers    []string `json:"store_number"`
	UserName        string   `json:"user_name"`
	BankID          string   `json:"bank_id"`
	PayCurrency     string   `json:"pay_currency"`
	Mobile          string   `json:"mobile"`
	IDNo            string   `json:"id_no"`
}

type CreateDepositResult struct {
	Order        domain.DepositOrder `json:"order"`
	Provider     string              `json:"provider"`
	ChannelCode  string              `json:"channel_code"`
	PaymentURL   string              `json:"payment_url"`
	Method       string              `json:"method"`
	Fields       map[string]string   `json:"fields"`
	PaymentHTML  string              `json:"payment_html"`
	Instructions map[string]string   `json:"instructions,omitempty"`
}

type DepositNotifyResult struct {
	Order          domain.DepositOrder `json:"order"`
	Ledger         *domain.LedgerEntry `json:"ledger,omitempty"`
	NotifyMerchant bool                `json:"notify_merchant"`
}

func NewDepositService(gateways map[string]provider.DepositGateway, channelProviders map[string]string, ledger *LedgerService) *DepositService {
	return &DepositService{
		nextID:           1,
		orders:           make(map[string]domain.DepositOrder),
		payments:         make(map[string]provider.DepositPaymentRequest),
		gateways:         cloneDepositGatewayMap(gateways),
		channelProviders: cloneStringMap(channelProviders),
		ledger:           ledger,
		now:              time.Now,
		orderTTL:         30 * time.Minute,
		deliveryEngine:   PublicHTTPSCallbackDeliveryEngine{Timeout: 10 * time.Second},
	}
}

func NewPersistentDepositService(gateways map[string]provider.DepositGateway, channelProviders map[string]string, ledger *LedgerService, store repository.DepositStore) *DepositService {
	service := NewDepositService(gateways, channelProviders, ledger)
	service.store = store
	return service
}

func (s *DepositService) SetMerchantDepositCallbackDispatcher(
	build func(domain.DepositOrder) (string, []byte, error),
	send func(string, []byte) error,
) {
	s.callbackBuilder = build
	s.callbackSender = send
}

func (s *DepositService) SetCallbackSigningKeyResolver(resolver repository.CallbackSigningKeyResolver) {
	s.callbackSigningKeys = resolver
}

func (s *DepositService) CreateDeposit(req CreateDepositRequest) (CreateDepositResult, error) {
	if req.MerchantOrderNo == "" {
		return CreateDepositResult{}, errors.New("merchant_order_no is required")
	}
	if req.Amount <= 0 {
		return CreateDepositResult{}, errors.New("amount must be greater than zero")
	}
	if req.Amount > 92233720368547758 {
		return CreateDepositResult{}, errors.New("amount is too large")
	}
	req.ChannelCode = strings.ToUpper(strings.TrimSpace(req.ChannelCode))
	if req.ChannelCode == "" {
		return CreateDepositResult{}, errors.New("channel_code is required")
	}
	if !isSupportedDepositChannelCode(req.ChannelCode) {
		return CreateDepositResult{}, fmt.Errorf("unsupported channel_code: %s", req.ChannelCode)
	}
	if req.Currency == "" {
		req.Currency = "TWD"
	}
	if req.ItemDesc == "" {
		req.ItemDesc = "Deposit"
	}

	providerCode, err := s.resolveDepositProviderCode(req.ProviderCode, req.ChannelCode)
	if err != nil {
		return CreateDepositResult{}, err
	}
	if s.store != nil {
		if existing, findErr := s.store.FindDepositOrderByMerchantOrderNo(context.Background(), strings.TrimSpace(req.MerchantID), strings.TrimSpace(req.MerchantOrderNo)); findErr == nil {
			if !matchesDepositCreateRequest(existing, req, providerCode) {
				return CreateDepositResult{}, errors.New("merchant_order_no already exists with different order details")
			}
			return depositResultForExistingOrder(existing), nil
		} else if !errors.Is(findErr, repository.ErrNotFound) {
			return CreateDepositResult{}, findErr
		}
	} else if existing, ok := s.FindDepositByMerchantOrderNo(strings.TrimSpace(req.MerchantID), strings.TrimSpace(req.MerchantOrderNo)); ok {
		if !matchesDepositCreateRequest(existing, req, providerCode) {
			return CreateDepositResult{}, errors.New("merchant_order_no already exists with different order details")
		}
		return depositResultForExistingOrder(existing), nil
	}
	gateway, err := s.depositGatewayFor(providerCode)
	if err != nil {
		return CreateDepositResult{}, err
	}

	now := s.now()
	expiresAt := now.Add(s.orderTTL)
	order := domain.DepositOrder{
		MerchantCode:    req.MerchantID,
		CallbackURL:     strings.TrimSpace(req.NotifyURL),
		ChannelCode:     req.ChannelCode,
		BankAccounts:    append([]string(nil), req.BankAccounts...),
		StoreNumbers:    append([]string(nil), req.StoreNumbers...),
		OrderNo:         buildDepositPlatformOrderNo(req.MerchantOrderNo),
		MerchantOrderNo: req.MerchantOrderNo,
		Provider:        providerCode,
		AmountCents:     req.Amount * 100,
		Currency:        strings.ToUpper(req.Currency),
		ItemDesc:        req.ItemDesc,
		UserName:        strings.TrimSpace(req.UserName),
		BankID:          strings.TrimSpace(req.BankID),
		PayCurrency:     strings.TrimSpace(req.PayCurrency),
		Mobile:          strings.TrimSpace(req.Mobile),
		IDNo:            strings.TrimSpace(req.IDNo),
		Status:          domain.DepositOrderStatusPending,
		ExpiresAt:       &expiresAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	payment, err := gateway.CreateDepositPayment(order, req.ItemDesc)
	if err != nil {
		return CreateDepositResult{}, err
	}

	if s.store != nil {
		created, err := s.store.CreateDepositOrder(context.Background(), order, req.ItemDesc)
		if err != nil {
			return CreateDepositResult{}, err
		}
		order = created
	} else {
		s.mu.Lock()
		if _, exists := s.orders[order.OrderNo]; exists {
			s.mu.Unlock()
			return CreateDepositResult{}, fmt.Errorf("order already exists: %s", order.OrderNo)
		}
		order.ID = s.nextID
		s.nextID++
		s.orders[order.OrderNo] = order
		s.mu.Unlock()
	}

	if s.store != nil {
		if err := s.store.SaveDepositPaymentRequest(context.Background(), order, payment); err != nil {
			return CreateDepositResult{}, err
		}
	}
	s.mu.Lock()
	s.payments[order.OrderNo] = payment
	s.mu.Unlock()

	return CreateDepositResult{
		Order:       order,
		Provider:    order.Provider,
		ChannelCode: req.ChannelCode,
		PaymentURL:  payment.URL,
		Method:      payment.Method,
		Fields:      payment.Fields,
		PaymentHTML: payment.HTML,
		Instructions: map[string]string{
			"browser": "Return payment_html as text/html to auto-submit the user to NewebPay.",
			"api":     "For API clients, POST fields to payment_url with method POST.",
		},
	}, nil
}

func depositResultForExistingOrder(order domain.DepositOrder) CreateDepositResult {
	return CreateDepositResult{
		Order:       order,
		Provider:    order.Provider,
		ChannelCode: order.ChannelCode,
		Instructions: map[string]string{
			"browser": "Reuse the canonical redirect URL returned by the gateway API.",
		},
	}
}

func matchesDepositCreateRequest(order domain.DepositOrder, req CreateDepositRequest, providerCode string) bool {
	return order.AmountCents == req.Amount*100 &&
		order.Currency == strings.ToUpper(req.Currency) &&
		order.ChannelCode == req.ChannelCode &&
		order.Provider == providerCode &&
		order.CallbackURL == strings.TrimSpace(req.NotifyURL)
}

func isSupportedDepositChannelCode(channelCode string) bool {
	switch channelCode {
	case "CREDIT", "APPLEPAY", "GOOGLEPAY", "WEBATM", "VACC", "CVS", "BARCODE":
		return true
	default:
		return false
	}
}

func (s *DepositService) HandleDepositProviderNotification(providerCode string, fields map[string]string, trace domain.DepositNotifyTrace) (DepositNotifyResult, error) {
	gateway, err := s.depositGatewayFor(providerCode)
	if err != nil {
		return DepositNotifyResult{}, err
	}
	if enricher, ok := gateway.(interface {
		EnrichDepositNotifyTrace(map[string]string, domain.DepositNotifyTrace) domain.DepositNotifyTrace
	}); ok {
		trace = enricher.EnrichDepositNotifyTrace(fields, trace)
	}

	notification, err := gateway.VerifyDepositNotification(fields)
	if err != nil {
		if s.store != nil {
			if recordErr := s.store.RecordDepositNotificationFailure(context.Background(), providerCode, trace, fields, err.Error()); recordErr != nil {
				return DepositNotifyResult{}, fmt.Errorf("%w; record notification failure: %v", err, recordErr)
			}
		}
		return DepositNotifyResult{}, err
	}
	if notification.AmountCents <= 0 {
		err := errors.New("provider notification amount must be positive")
		if s.store != nil {
			if recordErr := s.store.RecordDepositNotificationFailure(context.Background(), providerCode, trace, fields, err.Error()); recordErr != nil {
				return DepositNotifyResult{}, fmt.Errorf("%w; record notification failure: %v", err, recordErr)
			}
		}
		return DepositNotifyResult{}, err
	}
	if strings.TrimSpace(trace.ProviderOrderNo) == "" {
		trace.ProviderOrderNo = notification.OrderNo
	}
	if strings.TrimSpace(trace.ProviderTradeNo) == "" {
		trace.ProviderTradeNo = notification.TradeNo
	}

	if s.store != nil {
		order, ledger, notifyMerchant, err := s.store.ApplyDepositNotification(context.Background(), providerCode, notification, trace)
		if err != nil {
			return DepositNotifyResult{}, err
		}
		return DepositNotifyResult{Order: order, Ledger: ledger, NotifyMerchant: notifyMerchant}, nil
	}

	s.mu.Lock()
	order, exists := s.orders[notification.OrderNo]
	if !exists {
		s.mu.Unlock()
		return DepositNotifyResult{}, fmt.Errorf("order not found: %s", notification.OrderNo)
	}
	if notification.AmountCents != 0 && notification.AmountCents != order.AmountCents {
		s.mu.Unlock()
		return DepositNotifyResult{}, fmt.Errorf("amount mismatch: got %d want %d", notification.AmountCents, order.AmountCents)
	}

	previousStatus := order.Status
	if order.Status == domain.DepositOrderStatusPaid {
		if !strings.EqualFold(notification.Status, "SUCCESS") || strings.TrimSpace(order.ProviderTradeNo) != strings.TrimSpace(notification.TradeNo) {
			s.mu.Unlock()
			return DepositNotifyResult{}, fmt.Errorf("paid order provider transaction does not match notification")
		}
		s.orders[order.OrderNo] = order
		s.mu.Unlock()
		return DepositNotifyResult{Order: order, NotifyMerchant: false}, nil
	}
	order.ProviderTradeNo = notification.TradeNo
	order.UpdatedAt = time.Now()
	if strings.EqualFold(notification.Status, "SUCCESS") {
		order.Status = domain.DepositOrderStatusPaid
	} else {
		order.Status = domain.DepositOrderStatusFailed
	}
	s.orders[order.OrderNo] = order
	s.mu.Unlock()

	var ledger *domain.LedgerEntry
	if order.Status == domain.DepositOrderStatusPaid {
		entry, err := s.ledger.RecordDeposit(order)
		if err != nil {
			return DepositNotifyResult{}, err
		}
		ledger = &entry
	}

	return DepositNotifyResult{
		Order:          order,
		Ledger:         ledger,
		NotifyMerchant: previousStatus != order.Status,
	}, nil
}

func (s *DepositService) HandleNewebpayDepositNotification(fields map[string]string) (DepositNotifyResult, error) {
	return s.HandleDepositProviderNotification("newebpay", fields, domain.DepositNotifyTrace{})
}

func (s *DepositService) FindDepositByOrderNo(orderNo string) (domain.DepositOrder, bool) {
	if s.store != nil {
		order, err := s.store.FindDepositOrderByOrderNo(context.Background(), orderNo)
		if err != nil {
			return domain.DepositOrder{}, false
		}
		return s.applyDepositExpiry(order), true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[orderNo]
	if !ok {
		return domain.DepositOrder{}, false
	}
	return s.applyDepositExpiry(order), true
}

// SendTestPaidCallback sends one signed merchant callback using an in-memory
// paid view of an existing order. It deliberately does not persist any state
// or enqueue retries, so it is safe for an explicitly enabled integration test.
func (s *DepositService) SendTestPaidCallback(ctx context.Context, orderNo string) (domain.DepositOrder, error) {
	order, ok := s.FindDepositByOrderNo(orderNo)
	if !ok {
		return domain.DepositOrder{}, fmt.Errorf("order not found: %s", orderNo)
	}
	if s.callbackBuilder == nil || s.callbackSender == nil {
		return domain.DepositOrder{}, errors.New("merchant callback dispatcher is unavailable")
	}
	testOrder := order
	testOrder.Status = domain.DepositOrderStatusPaid
	callbackURL, body, err := s.callbackBuilder(testOrder)
	if err != nil {
		return domain.DepositOrder{}, err
	}
	if strings.TrimSpace(callbackURL) == "" {
		return domain.DepositOrder{}, errors.New("merchant callback URL is not configured")
	}
	if err := s.callbackSender(callbackURL, body); err != nil {
		return domain.DepositOrder{}, err
	}
	return testOrder, nil
}

type depositCallbackTaskStore interface {
	ClaimDueMerchantDepositCallbackTasks(ctx context.Context, before, staleBefore time.Time, limit int) ([]domain.MerchantDepositCallbackTask, error)
}

type depositCallbackLifecycleStore interface {
	depositCallbackTaskStore
	BeginMerchantDepositCallbackAttempt(context.Context, int64, string, time.Time) (domain.MerchantDepositCallbackAttempt, error)
	FinalizeMerchantDepositCallbackSuccess(context.Context, int64, string, int64, domain.DeliveryResult, time.Time) error
	FinalizeMerchantDepositCallbackFailure(context.Context, int64, string, int64, domain.DeliveryResult, time.Time, time.Time) error
	RecoverStaleMerchantDepositCallbacks(context.Context, time.Time, int) error
}

type idempotentDepositCallbackTaskStore interface {
	EnsureMerchantDepositCallbackTask(context.Context, domain.MerchantDepositCallbackTask) (domain.MerchantDepositCallbackTask, bool, error)
}

// missingDepositCallbackTaskStore exposes terminal orders that have no durable
// callback task. It is a recovery path for the narrow window where a payment
// state transaction commits but the subsequent outbox insert is unavailable.
type missingDepositCallbackTaskStore interface {
	FindTerminalDepositOrdersMissingCallbackTasks(context.Context, int) ([]domain.DepositOrder, error)
}

func (s *DepositService) EnqueueDepositCallback(ctx context.Context, order domain.DepositOrder, payload string) error {
	if s.store == nil {
		return nil
	}
	if _, ok := s.store.(depositCallbackTaskStore); !ok {
		return nil
	}
	if strings.TrimSpace(order.CallbackURL) == "" || strings.TrimSpace(payload) == "" {
		return nil
	}
	task := domain.MerchantDepositCallbackTask{
		MerchantID:  order.MerchantID,
		OrderID:     order.ID,
		CallbackURL: order.CallbackURL,
		Payload:     payload,
		NextRetryAt: s.now(),
	}
	var keyErr error
	task.EventKey, keyErr = domain.MerchantDepositCallbackEventKey(order.OrderNo, string(order.Status))
	if keyErr != nil {
		return keyErr
	}
	ensured, ok := s.store.(idempotentDepositCallbackTaskStore)
	if !ok {
		return errors.New("deposit callback store does not support idempotent outbox ensure")
	}
	_, recovered, err := ensured.EnsureMerchantDepositCallbackTask(ctx, task)
	if err == nil && recovered {
		log.Printf("deposit callback enqueue recovered uncertain result: event_key=%s", task.EventKey)
	}
	return err
}

// RecoverMissingDepositCallbacks makes callback delivery eventually recoverable
// after a post-commit outbox write failure. The event key used by Enqueue is
// unique, so concurrent recovery or a provider retry cannot create duplicates.
func (s *DepositService) RecoverMissingDepositCallbacks(ctx context.Context, limit int) error {
	if s.store == nil {
		return nil
	}
	store, ok := s.store.(missingDepositCallbackTaskStore)
	if !ok {
		return nil
	}
	orders, err := store.FindTerminalDepositOrdersMissingCallbackTasks(ctx, limit)
	if err != nil {
		return err
	}
	for _, order := range orders {
		if strings.TrimSpace(order.CallbackURL) == "" {
			continue
		}
		if err := s.DispatchMerchantDepositCallback(ctx, order); err != nil {
			return err
		}
	}
	return nil
}

func (s *DepositService) RetryDepositCallbacks(ctx context.Context, limit int) error {
	if s.store == nil {
		return nil
	}
	taskStore, ok := s.store.(depositCallbackLifecycleStore)
	if !ok {
		return nil
	}
	now := s.now()
	if err := taskStore.RecoverStaleMerchantDepositCallbacks(ctx, now, limit); err != nil {
		return err
	}
	tasks, err := taskStore.ClaimDueMerchantDepositCallbackTasks(ctx, now, now.Add(-2*time.Minute), limit)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		attempt, err := taskStore.BeginMerchantDepositCallbackAttempt(ctx, task.ID, task.ClaimToken, now)
		if err != nil {
			if !errors.Is(err, repository.ErrCallbackClaimLost) {
				return err
			}
			continue
		}
		engine := s.deliveryEngine
		if engine == nil {
			engine = PublicHTTPSCallbackDeliveryEngine{Timeout: 10 * time.Second}
		}
		headers, headerErr := s.depositCallbackHeaders(ctx, task.MerchantID, task.CallbackURL, []byte(task.Payload), s.now())
		result := domain.DeliveryResult{Error: headerErr, ErrorCode: "callback_auth_config_missing", Retryable: false}
		if headerErr == nil {
			result = engine.Deliver(ctx, domain.CallbackDeliveryRequest{URL: task.CallbackURL, Body: []byte(task.Payload), Headers: headers})
		}
		if result.Error == nil {
			if err := taskStore.FinalizeMerchantDepositCallbackSuccess(ctx, task.ID, task.ClaimToken, attempt.ID, result, s.now()); err != nil && !errors.Is(err, repository.ErrCallbackClaimLost) {
				return err
			}
			log.Printf("component=deposit_callback_worker operation=delivery_finalized task_id=%d order_id=%d attempt_id=%d status=sent http_status=%d", task.ID, task.OrderID, attempt.ID, result.HTTPStatus)
			continue
		}
		next, _ := repository.DepositCallbackNextRetry(task.RetryCount, s.now())
		if err := taskStore.FinalizeMerchantDepositCallbackFailure(ctx, task.ID, task.ClaimToken, attempt.ID, result, next, s.now()); err != nil && !errors.Is(err, repository.ErrCallbackClaimLost) {
			return err
		}
		if task.RetryCount+1 >= repository.DepositCallbackMaxRetries {
			log.Printf("deposit callback dead-lettered: task_id=%d order_id=%d code=%s", task.ID, task.OrderID, result.ErrorCode)
		}
	}
	return nil
}

func (s *DepositService) depositCallbackHeaders(ctx context.Context, merchantID int64, rawURL string, body []byte, now time.Time) (map[string][]string, error) {
	if s.callbackSigningKeys == nil {
		return nil, errors.New("merchant callback signing key resolver is unavailable")
	}
	key, err := s.callbackSigningKeys.ResolveCurrentCallbackSigningKey(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return BuildMerchantCallbackHeaders(key, "POST", rawURL, body, now)
}

func (s *DepositService) FindDepositByMerchantOrderNo(merchantCode, merchantOrderNo string) (domain.DepositOrder, bool) {
	if s.store != nil {
		order, err := s.store.FindDepositOrderByMerchantOrderNo(context.Background(), merchantCode, merchantOrderNo)
		if err != nil {
			return domain.DepositOrder{}, false
		}
		return s.applyDepositExpiry(order), true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, order := range s.orders {
		if order.MerchantCode == merchantCode && order.MerchantOrderNo == merchantOrderNo {
			return s.applyDepositExpiry(order), true
		}
	}
	return domain.DepositOrder{}, false
}

func (s *DepositService) ExpireDueDeposits(ctx context.Context, limit int) error {
	var expiredOrders []domain.DepositOrder
	if s.store != nil {
		taskStore, ok := s.store.(interface {
			ExpireDueDepositOrders(context.Context, time.Time, int) ([]domain.DepositOrder, error)
		})
		if !ok {
			return nil
		}
		orders, err := taskStore.ExpireDueDepositOrders(ctx, s.now(), limit)
		if err != nil {
			return err
		}
		expiredOrders = orders
	} else {
		s.mu.Lock()
		for orderNo, order := range s.orders {
			if order.Status == domain.DepositOrderStatusPending && order.ExpiresAt != nil && !order.ExpiresAt.After(s.now()) {
				order.Status = domain.DepositOrderStatusExpired
				order.UpdatedAt = s.now()
				s.orders[orderNo] = order
				expiredOrders = append(expiredOrders, order)
			}
		}
		s.mu.Unlock()
	}
	for _, order := range expiredOrders {
		_ = s.DispatchMerchantDepositCallback(ctx, order)
	}
	return nil
}

func (s *DepositService) DispatchMerchantDepositCallback(ctx context.Context, order domain.DepositOrder) error {
	if s.callbackBuilder == nil || s.callbackSender == nil {
		return nil
	}
	callbackURL, body, err := s.callbackBuilder(order)
	if err != nil || strings.TrimSpace(callbackURL) == "" {
		return err
	}
	// Persist before delivery.  The worker is the only component allowed to
	// perform HTTP delivery so each request can be audited and claimed once.
	return s.EnqueueDepositCallback(ctx, order, string(body))
}

func (s *DepositService) applyDepositExpiry(order domain.DepositOrder) domain.DepositOrder {
	if order.Status == domain.DepositOrderStatusPending && order.ExpiresAt != nil && !order.ExpiresAt.After(s.now()) {
		order.Status = domain.DepositOrderStatusExpired
	}
	return order
}

func (s *DepositService) DepositPaymentHTML(orderNo string) (string, bool) {
	payment, ok := s.DepositPaymentRequest(orderNo)
	if !ok || payment.HTML == "" {
		return "", false
	}
	return payment.HTML, true
}

// DepositPaymentRequest returns the locally generated provider form. It does
// not submit the form or create a provider-side transaction.
func (s *DepositService) DepositPaymentRequest(orderNo string) (provider.DepositPaymentRequest, bool) {
	s.mu.Lock()
	payment, ok := s.payments[orderNo]
	s.mu.Unlock()
	if ok {
		return payment, true
	}
	if s.store != nil {
		payment, err := s.store.LoadDepositPaymentRequest(context.Background(), orderNo)
		if err == nil {
			s.mu.Lock()
			s.payments[orderNo] = payment
			s.mu.Unlock()
			return payment, true
		}
	}
	return provider.DepositPaymentRequest{}, false
}

func buildDepositPlatformOrderNo(merchantOrderNo string) string {
	clean := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, merchantOrderNo)
	if clean == "" {
		clean = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if len(clean) > 14 {
		clean = clean[len(clean)-14:]
	}
	suffix := time.Now().Format("150405")
	return "P" + clean + suffix
}

func (s *DepositService) resolveDepositProviderCode(providerCode, channelCode string) (string, error) {
	providerCode = strings.ToLower(strings.TrimSpace(providerCode))
	if providerCode != "" {
		if _, err := s.depositGatewayFor(providerCode); err != nil {
			return "", err
		}
		return providerCode, nil
	}

	if mapped, ok := s.channelProviders[strings.ToUpper(strings.TrimSpace(channelCode))]; ok && strings.TrimSpace(mapped) != "" {
		return strings.ToLower(strings.TrimSpace(mapped)), nil
	}

	if _, ok := s.gateways["newebpay"]; ok {
		return "newebpay", nil
	}
	return "", fmt.Errorf("no provider mapped for channel_code: %s", channelCode)
}

func (s *DepositService) depositGatewayFor(providerCode string) (provider.DepositGateway, error) {
	providerCode = strings.ToLower(strings.TrimSpace(providerCode))
	gateway, ok := s.gateways[providerCode]
	if !ok || gateway == nil {
		return nil, fmt.Errorf("unsupported provider_code: %s", providerCode)
	}
	return gateway, nil
}

func cloneDepositGatewayMap(src map[string]provider.DepositGateway) map[string]provider.DepositGateway {
	dst := make(map[string]provider.DepositGateway, len(src))
	for key, value := range src {
		dst[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return dst
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[strings.ToUpper(strings.TrimSpace(key))] = strings.ToLower(strings.TrimSpace(value))
	}
	return dst
}
