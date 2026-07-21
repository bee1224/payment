package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"payment-service/internal/domain"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCreatePayoutOrderAcceptsHashedMerchantAPIKey(t *testing.T) {
	sum := sha256.Sum256([]byte("merchant-secret"))
	store := repository.NewInMemoryPayoutStore()
	store.SeedMerchant(domain.Merchant{
		Code:        "M10001",
		Name:        "Merchant 1",
		APIKey:      hex.EncodeToString(sum[:]),
		Status:      "active",
		CallbackURL: "https://merchant.example/payout-callback",
	}, 500000)

	service := NewPayoutServiceWithSecrets(
		store,
		providerGateway.NewPayoutClient("", "50000", "sign-key", "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)

	order, err := service.CreatePayoutOrder(context.Background(), CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "HASH-001",
		Amount:           "100",
		Currency:         "TWD",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	})
	if err != nil {
		t.Fatalf("CreatePayoutOrder() error = %v", err)
	}
	if order.MerchantCode != "M10001" {
		t.Fatalf("CreatePayoutOrder() merchant = %s, want M10001", order.MerchantCode)
	}
}

func TestCreatePayoutOrderRejectsFractionalTWD(t *testing.T) {
	store := repository.NewInMemoryPayoutStore()
	store.SeedMerchant(domain.Merchant{Code: "M10001", APIKey: "test-key"}, 100000)
	svc := NewPayoutService(store, nil)
	_, err := svc.CreatePayoutOrder(context.Background(), CreatePayoutOrderRequest{
		MerchantID: "M10001", APIKey: "test-key", MerchantPayoutNo: "FRACTION-001", Amount: "100.01", Currency: "TWD", CallbackURL: "https://merchant.example/callback",
		PayAccountName: "Tester", PayCardNo: "1234567890", PayBankName: "004",
	})
	if err == nil {
		t.Fatal("fractional TWD payout must be rejected")
	}
}

func TestPayoutCallbackTaskCanOnlyBeClaimedOnce(t *testing.T) {
	store := repository.NewInMemoryPayoutStore()
	if err := store.CreateMerchantPayoutCallbackTask(context.Background(), domain.MerchantPayoutCallbackTask{MerchantID: 1, PayoutOrderID: 1, CallbackURL: "https://merchant.example/callback", Payload: `{}`}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, err := store.ClaimDueMerchantPayoutCallbackTasks(context.Background(), now, now.Add(-time.Minute), 10)
	if err != nil || len(first) != 1 || first[0].ClaimToken == "" {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	second, err := store.ClaimDueMerchantPayoutCallbackTasks(context.Background(), now, now.Add(-time.Minute), 10)
	if err != nil || len(second) != 0 {
		t.Fatalf("duplicate claim = %+v, %v", second, err)
	}
}

func TestRedactPayoutRequestJSONMasksSensitiveFields(t *testing.T) {
	raw := redactPayoutRequestJSON(providerGateway.CreatePayoutRequest{
		PayAccountName: "Tester",
		PayCardNo:      "202008372239",
		PayValidateID:  "A123456789",
	})
	if strings.Contains(raw, "Tester") || strings.Contains(raw, "202008372239") || strings.Contains(raw, "A123456789") {
		t.Fatalf("redacted payload should not contain raw sensitive values: %s", raw)
	}
}

func TestValidatePayoutCallbackURLRequiresHTTPS(t *testing.T) {
	if err := validatePayoutCallbackURL("http://merchant.example/callback"); err == nil {
		t.Fatal("HTTP callback URL must be rejected")
	}
	if err := validatePayoutCallbackURL("https://127.0.0.1/callback"); err == nil {
		t.Fatal("private callback IP must be rejected")
	}
}

func TestRetryMerchantCallbacksRaisesOperationalAlertAfterThreshold(t *testing.T) {
	store := repository.NewInMemoryPayoutStore()
	store.SeedMerchant(domain.Merchant{
		Code:   "M10001",
		Name:   "Merchant 1",
		APIKey: "merchant-secret",
		Status: "active",
	}, 500000)

	svc := NewPayoutServiceWithSecrets(
		store,
		providerGateway.NewPayoutClient("", "50000", "sign-key", "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	svc.now = func() time.Time { return time.Date(2026, 7, 16, 13, 30, 0, 0, time.UTC) }

	order, err := svc.CreatePayoutOrder(context.Background(), CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "ALERT-001",
		Amount:           "100",
		Currency:         "TWD",
		CallbackURL:      "://bad-callback-url",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	})
	if err == nil {
		t.Fatal("expected invalid callback url create to fail")
	}

	order, err = svc.CreatePayoutOrder(context.Background(), CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "ALERT-002",
		Amount:           "100",
		Currency:         "TWD",
		CallbackURL:      "https://merchant.example/payout-callback",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	})
	if err != nil {
		t.Fatalf("CreatePayoutOrder() error = %v", err)
	}
	svc.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader("FAIL")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	order, err = store.CancelPayoutOrder(context.Background(), order.PayoutNo, "ops cancel", repository.PayoutReviewAuditLog{Action: "cancel", Actor: "ops"})
	if err != nil {
		t.Fatalf("CancelPayoutOrder() error = %v", err)
	}
	if err := svc.enqueueMerchantCallback(context.Background(), order); err != nil {
		t.Fatalf("enqueueMerchantCallback() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := svc.RetryMerchantCallbacks(context.Background(), 10); err != nil {
			t.Fatalf("RetryMerchantCallbacks() error = %v", err)
		}
	}
	alerts, err := svc.ListPayoutOperationalAlerts(context.Background(), "open", 10)
	if err != nil {
		t.Fatalf("ListPayoutOperationalAlerts() error = %v", err)
	}
	found := false
	for _, alert := range alerts {
		if alert.PayoutNo == order.PayoutNo && alert.Category == "merchant_callback_failed" {
			found = true
			if alert.OccurrenceCount < 1 {
				t.Fatalf("alert occurrence_count = %d", alert.OccurrenceCount)
			}
		}
	}
	if !found {
		t.Fatal("expected merchant_callback_failed alert to be raised")
	}
}

func TestPayoutLedgerChainTracksHoldCompleteAndReversal(t *testing.T) {
	store := repository.NewInMemoryPayoutStore()
	store.SeedMerchant(domain.Merchant{
		Code:   "M10001",
		Name:   "Merchant 1",
		APIKey: "merchant-secret",
		Status: "active",
	}, 500000)

	svc := NewPayoutServiceWithSecrets(
		store,
		providerGateway.NewPayoutClient("", "50000", "sign-key", "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)

	order, err := svc.CreatePayoutOrder(context.Background(), CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "LEDGER-001",
		Amount:           "100",
		Currency:         "TWD",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	})
	if err != nil {
		t.Fatalf("CreatePayoutOrder() error = %v", err)
	}

	if _, err := store.ApprovePayoutOrder(context.Background(), order.PayoutNo, repository.PayoutReviewAuditLog{Actor: "ops"}); err != nil {
		t.Fatalf("ApprovePayoutOrder() error = %v", err)
	}
	if _, err := store.MarkPayoutSubmitted(context.Background(), order.PayoutNo, domain.PayoutTransaction{
		ProviderOrderNo: "PROVIDER-001",
		ProviderTradeNo: "TRADE-001",
		Status:          "submitted",
	}); err != nil {
		t.Fatalf("MarkPayoutSubmitted() error = %v", err)
	}
	if _, _, err := store.ApplyPayoutResult(context.Background(), repository.PayoutProviderResult{
		MerchantPayoutNo: order.MerchantPayoutNo,
		ProviderOrderNo:  "PROVIDER-001",
		ProviderTradeNo:  "TRADE-001",
		StatusCode:       "30000",
		StatusMessage:    "completed",
		CompletedAt:      time.Date(2026, 7, 16, 14, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("ApplyPayoutResult(completed) error = %v", err)
	}
	if _, _, err := store.ApplyPayoutResult(context.Background(), repository.PayoutProviderResult{
		MerchantPayoutNo: order.MerchantPayoutNo,
		ProviderOrderNo:  "PROVIDER-001",
		ProviderTradeNo:  "TRADE-001",
		StatusCode:       "40000",
		StatusMessage:    "reversed",
		CompletedAt:      time.Date(2026, 7, 16, 15, 0, 0, 0, time.UTC),
		EventKey:         "reverse-001",
	}); err != nil {
		t.Fatalf("ApplyPayoutResult(reversed) error = %v", err)
	}

	entries := store.LedgerEntries()
	if len(entries) != 3 {
		t.Fatalf("ledger entries = %d, want 3", len(entries))
	}
	if entries[0].Type != domain.LedgerEntryTypePayoutHold {
		t.Fatalf("entry[0].type = %s", entries[0].Type)
	}
	if entries[0].BalanceBeforeCents != 500000 || entries[0].BalanceAfterCents != 490000 {
		t.Fatalf("hold balances = %d -> %d", entries[0].BalanceBeforeCents, entries[0].BalanceAfterCents)
	}
	if entries[1].Type != domain.LedgerEntryTypePayoutComplete {
		t.Fatalf("entry[1].type = %s", entries[1].Type)
	}
	if entries[1].ReferenceType != domain.LedgerReferenceTypePayoutTransaction || entries[1].ReferenceID == 0 {
		t.Fatalf("complete reference = %s/%d", entries[1].ReferenceType, entries[1].ReferenceID)
	}
	if entries[2].Type != domain.LedgerEntryTypeReversal {
		t.Fatalf("entry[2].type = %s", entries[2].Type)
	}
	if entries[2].ReversalOfEntryID != entries[1].ID {
		t.Fatalf("reversal_of_entry_id = %d, want %d", entries[2].ReversalOfEntryID, entries[1].ID)
	}
	if entries[2].SourceEvent != domain.LedgerSourceEventPayoutReverse {
		t.Fatalf("reversal source_event = %s", entries[2].SourceEvent)
	}
	if entries[2].BalanceBeforeCents != 490000 || entries[2].BalanceAfterCents != 500000 {
		t.Fatalf("reversal balances = %d -> %d", entries[2].BalanceBeforeCents, entries[2].BalanceAfterCents)
	}
}
