package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/provider"
	"payment-service/internal/repository"
)

type DepositService struct {
	mu               sync.Mutex
	nextID           int64
	orders           map[string]domain.DepositOrder
	payments         map[string]provider.DepositPaymentRequest
	gateways         map[string]provider.DepositGateway
	channelProviders map[string]string
	ledger           *LedgerService
	store            repository.DepositStore
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
	Order  domain.DepositOrder `json:"order"`
	Ledger *domain.LedgerEntry `json:"ledger,omitempty"`
}

func NewDepositService(gateways map[string]provider.DepositGateway, channelProviders map[string]string, ledger *LedgerService) *DepositService {
	return &DepositService{
		nextID:           1,
		orders:           make(map[string]domain.DepositOrder),
		payments:         make(map[string]provider.DepositPaymentRequest),
		gateways:         cloneDepositGatewayMap(gateways),
		channelProviders: cloneStringMap(channelProviders),
		ledger:           ledger,
	}
}

func NewPersistentDepositService(gateways map[string]provider.DepositGateway, channelProviders map[string]string, ledger *LedgerService, store repository.DepositStore) *DepositService {
	service := NewDepositService(gateways, channelProviders, ledger)
	service.store = store
	return service
}

func (s *DepositService) CreateDeposit(req CreateDepositRequest) (CreateDepositResult, error) {
	if req.MerchantOrderNo == "" {
		return CreateDepositResult{}, errors.New("merchant_order_no is required")
	}
	if req.Amount <= 0 {
		return CreateDepositResult{}, errors.New("amount must be greater than zero")
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
	gateway, err := s.depositGatewayFor(providerCode)
	if err != nil {
		return CreateDepositResult{}, err
	}

	now := time.Now()
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
		CreatedAt:       now,
		UpdatedAt:       now,
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

	payment, err := gateway.CreateDepositPayment(order, req.ItemDesc)
	if err != nil {
		return CreateDepositResult{}, err
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

func isSupportedDepositChannelCode(channelCode string) bool {
	switch channelCode {
	case "CREDIT", "APPLEPAY", "GOOGLEPAY", "WEBATM", "VACC", "CVS", "BARCODE":
		return true
	default:
		return false
	}
}

func (s *DepositService) HandleDepositProviderNotification(providerCode string, fields map[string]string) (DepositNotifyResult, error) {
	gateway, err := s.depositGatewayFor(providerCode)
	if err != nil {
		return DepositNotifyResult{}, err
	}

	notification, err := gateway.VerifyDepositNotification(fields)
	if err != nil {
		if s.store != nil {
			if recordErr := s.store.RecordDepositNotificationFailure(context.Background(), providerCode, fields, err.Error()); recordErr != nil {
				return DepositNotifyResult{}, fmt.Errorf("%w; record notification failure: %v", err, recordErr)
			}
		}
		return DepositNotifyResult{}, err
	}

	if s.store != nil {
		order, ledger, err := s.store.ApplyDepositNotification(context.Background(), providerCode, notification)
		if err != nil {
			return DepositNotifyResult{}, err
		}
		return DepositNotifyResult{Order: order, Ledger: ledger}, nil
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

	return DepositNotifyResult{Order: order, Ledger: ledger}, nil
}

func (s *DepositService) HandleNewebpayDepositNotification(fields map[string]string) (DepositNotifyResult, error) {
	return s.HandleDepositProviderNotification("newebpay", fields)
}

func (s *DepositService) FindDepositByOrderNo(orderNo string) (domain.DepositOrder, bool) {
	if s.store != nil {
		order, err := s.store.FindDepositOrderByOrderNo(context.Background(), orderNo)
		return order, err == nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[orderNo]
	return order, ok
}

func (s *DepositService) FindDepositByMerchantOrderNo(merchantCode, merchantOrderNo string) (domain.DepositOrder, bool) {
	if s.store != nil {
		order, err := s.store.FindDepositOrderByMerchantOrderNo(context.Background(), merchantCode, merchantOrderNo)
		return order, err == nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, order := range s.orders {
		if order.MerchantCode == merchantCode && order.MerchantOrderNo == merchantOrderNo {
			return order, true
		}
	}
	return domain.DepositOrder{}, false
}

func (s *DepositService) DepositPaymentHTML(orderNo string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	payment, ok := s.payments[orderNo]
	if !ok || payment.HTML == "" {
		return "", false
	}
	return payment.HTML, true
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
