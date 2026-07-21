package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/provider"
	"payment-service/internal/repository"
)

type callbackTaskStoreStub struct {
	repository.DepositStore
	createErr error
	tasks     []domain.MerchantDepositCallbackTask
	missing   []domain.DepositOrder
}

func (s *callbackTaskStoreStub) EnsureMerchantDepositCallbackTask(_ context.Context, task domain.MerchantDepositCallbackTask) (domain.MerchantDepositCallbackTask, bool, error) {
	if s.createErr != nil {
		return domain.MerchantDepositCallbackTask{}, false, s.createErr
	}
	for _, existing := range s.tasks {
		if existing.EventKey == task.EventKey {
			return existing, true, nil
		}
	}
	s.tasks = append(s.tasks, task)
	return task, false, nil
}

func (*callbackTaskStoreStub) ClaimDueMerchantDepositCallbackTasks(context.Context, time.Time, time.Time, int) ([]domain.MerchantDepositCallbackTask, error) {
	return nil, nil
}

func (*callbackTaskStoreStub) MarkMerchantDepositCallbackTaskResult(context.Context, int64, string, bool, time.Time, string) error {
	return nil
}

func (s *callbackTaskStoreStub) FindTerminalDepositOrdersMissingCallbackTasks(context.Context, int) ([]domain.DepositOrder, error) {
	return append([]domain.DepositOrder(nil), s.missing...), nil
}

type testDepositGateway struct{}

func (testDepositGateway) CreateDepositPayment(domain.DepositOrder, string) (provider.DepositPaymentRequest, error) {
	return provider.DepositPaymentRequest{
		URL:    "https://provider.example/deposit",
		Method: "POST",
		Fields: map[string]string{"token": "ok"},
	}, nil
}

func (testDepositGateway) VerifyDepositNotification(fields map[string]string) (provider.DepositNotification, error) {
	return provider.DepositNotification{}, nil
}

type notifyDepositGateway struct{}

func (notifyDepositGateway) CreateDepositPayment(domain.DepositOrder, string) (provider.DepositPaymentRequest, error) {
	return provider.DepositPaymentRequest{
		URL:    "https://provider.example/deposit",
		Method: "POST",
	}, nil
}

func (notifyDepositGateway) VerifyDepositNotification(fields map[string]string) (provider.DepositNotification, error) {
	return provider.DepositNotification{
		OrderNo:     fields["order_no"],
		TradeNo:     fields["trade_no"],
		AmountCents: 10000,
		Status:      fields["status"],
	}, nil
}

type zeroAmountNotifyDepositGateway struct{ notifyDepositGateway }

func (zeroAmountNotifyDepositGateway) VerifyDepositNotification(fields map[string]string) (provider.DepositNotification, error) {
	return provider.DepositNotification{
		OrderNo: fields["order_no"],
		TradeNo: fields["trade_no"],
		Status:  fields["status"],
	}, nil
}

func TestExpireDueDepositsMarksPendingOrdersExpired(t *testing.T) {
	svc := NewDepositService(
		map[string]provider.DepositGateway{"fake": testDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		NewLedgerService(),
	)
	svc.orderTTL = -time.Minute
	callbackCount := 0
	svc.SetMerchantDepositCallbackDispatcher(
		func(order domain.DepositOrder) (string, []byte, error) {
			if order.Status != domain.DepositOrderStatusExpired {
				t.Fatalf("expected expired callback status, got %s", order.Status)
			}
			return "https://merchant.example/callback", []byte(`{"status":"expired"}`), nil
		},
		func(string, []byte) error {
			callbackCount++
			return nil
		},
	)

	created, err := svc.CreateDeposit(CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "EXPIRE-001",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
		NotifyURL:       "https://merchant.example/callback",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ExpireDueDeposits(context.Background(), 10); err != nil {
		t.Fatal(err)
	}

	order, ok := svc.FindDepositByOrderNo(created.Order.OrderNo)
	if !ok {
		t.Fatal("expected deposit order to exist")
	}
	if order.Status != domain.DepositOrderStatusExpired {
		t.Fatalf("expected expired status, got %s", order.Status)
	}
	if order.ExpiresAt == nil {
		t.Fatal("expected expiresAt to be set")
	}
	if callbackCount != 0 {
		t.Fatalf("task-first callback must not deliver from expiry flow, got %d direct deliveries", callbackCount)
	}
}

func TestSendTestPaidCallbackDoesNotPersistOrderStateOrLedger(t *testing.T) {
	ledger := NewLedgerService()
	svc := NewDepositService(
		map[string]provider.DepositGateway{"fake": testDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		ledger,
	)
	created, err := svc.CreateDeposit(CreateDepositRequest{
		MerchantID: "M10001", MerchantOrderNo: "TEST-CALLBACK-001", Amount: 100,
		Currency: "TWD", ChannelCode: "CREDIT", NotifyURL: "https://merchant.example/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	var sentOrder domain.DepositOrder
	svc.SetMerchantDepositCallbackDispatcher(
		func(order domain.DepositOrder) (string, []byte, error) {
			sentOrder = order
			return order.CallbackURL, []byte(`{"status":"paid"}`), nil
		},
		func(string, []byte) error { return nil },
	)

	result, err := svc.SendTestPaidCallback(context.Background(), created.Order.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.DepositOrderStatusPaid || sentOrder.Status != domain.DepositOrderStatusPaid {
		t.Fatalf("test callback status = %s / %s, want paid", result.Status, sentOrder.Status)
	}
	persisted, ok := svc.FindDepositByOrderNo(created.Order.OrderNo)
	if !ok || persisted.Status != domain.DepositOrderStatusPending {
		t.Fatalf("persisted order status = %s, want pending", persisted.Status)
	}
	if entries := ledger.Entries(); len(entries) != 0 {
		t.Fatalf("test callback unexpectedly recorded %d ledger entries", len(entries))
	}
}

func TestHandleDepositProviderNotificationRecordsDepositPaidLedger(t *testing.T) {
	ledger := NewLedgerService()
	svc := NewDepositService(
		map[string]provider.DepositGateway{"fake": notifyDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		ledger,
	)

	created, err := svc.CreateDeposit(CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "PAID-001",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.HandleDepositProviderNotification("fake", map[string]string{
		"order_no": created.Order.OrderNo,
		"trade_no": "TRADE-PAID-001",
		"status":   "SUCCESS",
	}, domain.DepositNotifyTrace{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ledger == nil {
		t.Fatal("expected ledger entry")
	}
	if result.Ledger.Type != domain.LedgerEntryTypeDepositPaid {
		t.Fatalf("ledger type = %s, want %s", result.Ledger.Type, domain.LedgerEntryTypeDepositPaid)
	}
	if result.Ledger.SourceEvent != domain.LedgerSourceEventDepositPaid {
		t.Fatalf("ledger source_event = %s, want %s", result.Ledger.SourceEvent, domain.LedgerSourceEventDepositPaid)
	}
	if result.Ledger.ReferenceType != domain.LedgerReferenceTypeOrder {
		t.Fatalf("ledger reference_type = %s, want %s", result.Ledger.ReferenceType, domain.LedgerReferenceTypeOrder)
	}
	if result.Ledger.ReferenceID != created.Order.ID {
		t.Fatalf("ledger reference_id = %d, want %d", result.Ledger.ReferenceID, created.Order.ID)
	}
}

func TestHandleDepositProviderNotificationRejectsMissingAmount(t *testing.T) {
	svc := NewDepositService(
		map[string]provider.DepositGateway{"fake": zeroAmountNotifyDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		NewLedgerService(),
	)
	created, err := svc.CreateDeposit(CreateDepositRequest{
		MerchantID: "M10001", MerchantOrderNo: "PAID-NO-AMOUNT-001", Amount: 100, Currency: "TWD", ChannelCode: "CREDIT",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.HandleDepositProviderNotification("fake", map[string]string{
		"order_no": created.Order.OrderNo, "trade_no": "TRADE-NO-AMOUNT-001", "status": "SUCCESS",
	}, domain.DepositNotifyTrace{})
	if err == nil || !strings.Contains(err.Error(), "amount must be positive") {
		t.Fatalf("expected missing amount rejection, got %v", err)
	}
}

func TestCreateDepositReturnsExistingOrderForIdempotentMerchantOrderNo(t *testing.T) {
	svc := NewDepositService(
		map[string]provider.DepositGateway{"fake": testDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		NewLedgerService(),
	)
	req := CreateDepositRequest{MerchantID: "M10001", MerchantOrderNo: "IDEMPOTENT-001", Amount: 100, Currency: "TWD", ChannelCode: "CREDIT", NotifyURL: "https://merchant.example/callback"}
	first, err := svc.CreateDeposit(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateDeposit(req)
	if err != nil {
		t.Fatal(err)
	}
	if second.Order.OrderNo != first.Order.OrderNo {
		t.Fatalf("idempotent retry returned order %s, want %s", second.Order.OrderNo, first.Order.OrderNo)
	}
	req.Amount = 101
	if _, err := svc.CreateDeposit(req); err == nil {
		t.Fatal("same merchant order number with changed details must be rejected")
	}
}

func TestCreateDepositRejectsAmountThatWouldOverflowStoredMinorUnits(t *testing.T) {
	svc := NewDepositService(map[string]provider.DepositGateway{"fake": testDepositGateway{}}, map[string]string{"CREDIT": "fake"}, NewLedgerService())
	_, err := svc.CreateDeposit(CreateDepositRequest{MerchantID: "M10001", MerchantOrderNo: "OVERFLOW-001", Amount: 92233720368547759, Currency: "TWD", ChannelCode: "CREDIT"})
	if err == nil {
		t.Fatal("overflowing amount must be rejected")
	}
}

func TestEnqueueDepositCallbackPersistsTaskWithoutImmediateRetry(t *testing.T) {
	store := &callbackTaskStoreStub{}
	svc := &DepositService{store: store, now: time.Now}
	order := domain.DepositOrder{ID: 7, MerchantID: 9, OrderNo: "TX-CALLBACK-001", Status: domain.DepositOrderStatusPaid, CallbackURL: "https://merchant.example/callback"}
	if err := svc.EnqueueDepositCallback(context.Background(), order, `{"status":"paid"}`); err != nil {
		t.Fatalf("enqueue callback: %v", err)
	}
	if len(store.tasks) != 1 {
		t.Fatalf("queued tasks = %d, want 1", len(store.tasks))
	}
}

func TestEnqueueDepositCallbackReturnsInsertFailure(t *testing.T) {
	store := &callbackTaskStoreStub{createErr: errors.New("driver: bad connection")}
	svc := &DepositService{store: store, now: time.Now}
	err := svc.EnqueueDepositCallback(context.Background(), domain.DepositOrder{OrderNo: "TX-CALLBACK-002", Status: domain.DepositOrderStatusPaid, CallbackURL: "https://merchant.example/callback"}, `{}`)
	if err == nil || !strings.Contains(err.Error(), "bad connection") {
		t.Fatalf("enqueue error = %v", err)
	}
}

func TestRecoverMissingDepositCallbacksQueuesTerminalOrder(t *testing.T) {
	store := &callbackTaskStoreStub{missing: []domain.DepositOrder{{
		ID: 7, MerchantID: 9, MerchantCode: "M10001", OrderNo: "TX-RECOVER-001", MerchantOrderNo: "ORDER-RECOVER-001",
		Status: domain.DepositOrderStatusPaid, CallbackURL: "https://merchant.example/callback",
	}}}
	svc := &DepositService{store: store, now: time.Now}
	svc.SetMerchantDepositCallbackDispatcher(
		func(order domain.DepositOrder) (string, []byte, error) {
			return order.CallbackURL, []byte(`{"status":"paid"}`), nil
		},
		func(string, []byte) error { return nil },
	)
	if err := svc.RecoverMissingDepositCallbacks(context.Background(), 10); err != nil {
		t.Fatalf("recover missing callbacks: %v", err)
	}
	if len(store.tasks) != 1 || store.tasks[0].EventKey != "merchant.deposit:TX-RECOVER-001:deposit.paid" {
		t.Fatalf("recovered tasks = %#v", store.tasks)
	}
}

type callbackSigningKeyResolverFunc func(context.Context, int64) (repository.CallbackSigningKey, error)

func (f callbackSigningKeyResolverFunc) ResolveCurrentCallbackSigningKey(ctx context.Context, merchantID int64) (repository.CallbackSigningKey, error) {
	return f(ctx, merchantID)
}

func TestDepositCallbackHeadersUseMerchantSigningKeyForEveryDelivery(t *testing.T) {
	svc := NewDepositService(nil, nil, NewLedgerService())
	svc.SetCallbackSigningKeyResolver(callbackSigningKeyResolverFunc(func(_ context.Context, merchantID int64) (repository.CallbackSigningKey, error) {
		return repository.CallbackSigningKey{MerchantID: merchantID, MerchantCode: "merchant-1", KeyID: "v1", Secret: "test-secret"}, nil
	}))
	now := time.Unix(1700000000, 0)
	first, err := svc.depositCallbackHeaders(context.Background(), 1, "https://merchant.example/callback", []byte(`{"status":"paid"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.depositCallbackHeaders(context.Background(), 1, "https://merchant.example/callback", []byte(`{"status":"paid"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, headers := range []map[string][]string{first, second} {
		for _, name := range []string{"Content-Type", "X-Callback-Merchant-Id", "X-Callback-Key-Id", "X-Callback-Timestamp", "X-Callback-Nonce", "X-Callback-Signature-Version", "X-Callback-Signature"} {
			if len(headers[name]) != 1 || headers[name][0] == "" {
				t.Fatalf("missing %s", name)
			}
		}
	}
	if first["X-Callback-Nonce"][0] == second["X-Callback-Nonce"][0] || first["X-Callback-Signature"][0] == second["X-Callback-Signature"][0] {
		t.Fatal("each delivery must get a fresh nonce and signature")
	}
}

func TestDepositCallbackHeadersRejectMissingSecret(t *testing.T) {
	svc := NewDepositService(nil, nil, NewLedgerService())
	if _, err := svc.depositCallbackHeaders(context.Background(), 1, "https://merchant.example/callback", []byte(`{}`), time.Now()); err == nil {
		t.Fatal("missing secret must prevent delivery")
	}
}

func TestRetryDepositCallbacksLogsSanitizedSuccessSummary(t *testing.T) {
	task := domain.MerchantDepositCallbackTask{ID: 71, OrderID: 72, ClaimToken: "claim-token-must-not-log", CallbackURL: "https://merchant.example/callback?secret=must-not-log", Payload: `{"secret":"must-not-log","signature":"must-not-log"}`}
	store := &callbackWorkerSuccessStore{task: task}
	svc := &DepositService{store: store, now: func() time.Time { return time.Unix(1700000000, 0) }, deliveryEngine: callbackDeliveryEngineFunc(func(context.Context, domain.CallbackDeliveryRequest) domain.DeliveryResult {
		return domain.DeliveryResult{HTTPStatus: 200}
	})}
	svc.SetCallbackSigningKeyResolver(callbackSigningKeyResolverFunc(func(_ context.Context, merchantID int64) (repository.CallbackSigningKey, error) {
		return repository.CallbackSigningKey{MerchantID: merchantID, MerchantCode: "merchant", KeyID: "v1", Secret: "callback-secret-must-not-log"}, nil
	}))
	var output bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previousWriter)
	if err := svc.RetryDepositCallbacks(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	line := output.String()
	if !strings.Contains(line, "component=deposit_callback_worker operation=delivery_finalized task_id=71 order_id=72 attempt_id=73 status=sent http_status=200") {
		t.Fatalf("missing success summary: %s", line)
	}
	for _, forbidden := range []string{"claim-token-must-not-log", "hmac-secret-must-not-log", "must-not-log", "merchant.example"} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("sensitive callback data leaked: %s", line)
		}
	}
}

type callbackWorkerSuccessStore struct {
	repository.DepositStore
	task domain.MerchantDepositCallbackTask
}

func (s *callbackWorkerSuccessStore) RecoverStaleMerchantDepositCallbacks(context.Context, time.Time, int) error {
	return nil
}
func (s *callbackWorkerSuccessStore) ClaimDueMerchantDepositCallbackTasks(context.Context, time.Time, time.Time, int) ([]domain.MerchantDepositCallbackTask, error) {
	task := s.task
	s.task.ID = 0
	return []domain.MerchantDepositCallbackTask{task}, nil
}
func (*callbackWorkerSuccessStore) BeginMerchantDepositCallbackAttempt(context.Context, int64, string, time.Time) (domain.MerchantDepositCallbackAttempt, error) {
	return domain.MerchantDepositCallbackAttempt{ID: 73}, nil
}
func (*callbackWorkerSuccessStore) FinalizeMerchantDepositCallbackSuccess(context.Context, int64, string, int64, domain.DeliveryResult, time.Time) error {
	return nil
}
func (*callbackWorkerSuccessStore) FinalizeMerchantDepositCallbackFailure(context.Context, int64, string, int64, domain.DeliveryResult, time.Time, time.Time) error {
	return nil
}

type callbackDeliveryEngineFunc func(context.Context, domain.CallbackDeliveryRequest) domain.DeliveryResult

func (f callbackDeliveryEngineFunc) Deliver(ctx context.Context, request domain.CallbackDeliveryRequest) domain.DeliveryResult {
	return f(ctx, request)
}

func TestConcurrentDuplicatePaidNotifyRecordsOneLedgerEntry(t *testing.T) {
	ledger := NewLedgerService()
	svc := NewDepositService(map[string]provider.DepositGateway{"fake": notifyDepositGateway{}}, map[string]string{"CREDIT": "fake"}, ledger)
	created, err := svc.CreateDeposit(CreateDepositRequest{MerchantID: "M10001", MerchantOrderNo: "CONCURRENT-PAID-001", Amount: 100, Currency: "TWD", ChannelCode: "CREDIT"})
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{"order_no": created.Order.OrderNo, "trade_no": "TRADE-CONCURRENT-001", "status": "SUCCESS"}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.HandleDepositProviderNotification("fake", fields, domain.DepositNotifyTrace{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if entries := ledger.Entries(); len(entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(entries))
	}
}
