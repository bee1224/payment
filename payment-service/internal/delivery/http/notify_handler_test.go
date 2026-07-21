package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"payment-service/internal/config"
	"payment-service/internal/domain"
	"payment-service/internal/provider"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
	"payment-service/internal/service"
)

func TestRequestSourceIPPrefersForwardedHeadersFromTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodPost, "/notify/newebpay", nil)
	req.RemoteAddr = "10.0.0.9:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")

	trusted, err := newSourceIPAllowlist([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("trusted proxy allowlist: %v", err)
	}
	if got := requestSourceIP(req, trusted); got != "203.0.113.10" {
		t.Fatalf("expected forwarded IP, got %q", got)
	}
}

func TestRequestSourceIPIgnoresForwardedHeadersFromUntrustedSource(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodPost, "/notify/newebpay", nil)
	req.RemoteAddr = "203.0.113.20:1234"
	req.Header.Set("X-Forwarded-For", "35.220.239.87")

	trusted, err := newSourceIPAllowlist([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("trusted proxy allowlist: %v", err)
	}
	if got := requestSourceIP(req, trusted); got != "203.0.113.20" {
		t.Fatalf("expected remote IP from untrusted source, got %q", got)
	}
}

func TestBestEffortNotifyFieldExtraction(t *testing.T) {
	fields := map[string]string{
		"MerchantOrderNo": "P202607140001",
		"TradeNo":         "TRADE-001",
	}

	if got := bestEffortNotifyOrderNo(fields); got != "P202607140001" {
		t.Fatalf("unexpected provider order no: %q", got)
	}
	if got := bestEffortNotifyTradeNo(fields); got != "TRADE-001" {
		t.Fatalf("unexpected provider trade no: %q", got)
	}
}

func TestPostGatewayDepositCallbackRequires2xxAndOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	handler := &DepositHandler{}
	if err := handler.postGatewayDepositCallback(server.URL, []byte(`{}`)); err == nil {
		t.Fatal("expected non-2xx callback response to fail")
	}
}

func TestSourceIPAllowlistSupportsIPAndCIDR(t *testing.T) {
	allowlist, err := newSourceIPAllowlist([]string{"35.220.239.87", "198.51.100.0/24"})
	if err != nil {
		t.Fatalf("build allowlist: %v", err)
	}

	if !allowlist.Allows("35.220.239.87") {
		t.Fatal("exact IP should be allowed")
	}
	if !allowlist.Allows("198.51.100.24") {
		t.Fatal("CIDR IP should be allowed")
	}
	if allowlist.Allows("203.0.113.10") {
		t.Fatal("unexpected IP should be rejected")
	}
}

func TestSourceIPAllowlistFailsClosedWhenEmpty(t *testing.T) {
	allowlist, err := newSourceIPAllowlist(nil)
	if err != nil {
		t.Fatalf("build empty allowlist: %v", err)
	}
	if allowlist.Allows("203.0.113.10") {
		t.Fatal("empty allowlist must reject every source IP")
	}
}

func TestReadNotifyFieldsRejectsOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), int(maxDepositNotifyBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/notify/newebpay", bytes.NewReader(body))
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, maxDepositNotifyBodyBytes)
	if _, err := readNotifyFields(req); err == nil {
		t.Fatal("oversized notification body must be rejected")
	}
}

func TestDepositProviderNotifyRejectsSourceIPOutsideAllowlist(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code:   "M10001",
		Name:   "Merchant 1",
		APIKey: "merchant-secret",
		Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerGateway.NewPayoutClient("", "", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{TrustedProxyCIDRs: []string{"192.0.2.0/24"}}, config.GatewayConfig{
		HMACSecret:               testGatewayHMACSecret,
		DepositCallbackAllowlist: []string{"35.220.239.87"},
	})

	created, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "DEPOSIT-ALLOWLIST-001",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
	})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{
		"order_no":     created.Order.OrderNo,
		"amount_cents": "10000",
		"trade_no":     "PROVIDER-ALLOWLIST-001",
		"status":       "SUCCESS",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposits/providers/fake/notifications", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.10:4567"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized || !bytes.Contains(resp.Body.Bytes(), []byte("allowlist")) {
		t.Fatalf("blocked source IP should be rejected: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestDepositProviderNotifyAcceptsSourceIPInsideAllowlist(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code:   "M10001",
		Name:   "Merchant 1",
		APIKey: "merchant-secret",
		Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerGateway.NewPayoutClient("", "", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{TrustedProxyCIDRs: []string{"192.0.2.0/24"}}, config.GatewayConfig{
		HMACSecret:               testGatewayHMACSecret,
		DepositCallbackAllowlist: []string{"35.220.239.87", "198.51.100.0/24"},
	})

	created, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "DEPOSIT-ALLOWLIST-002",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
	})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{
		"order_no":     created.Order.OrderNo,
		"amount_cents": "10000",
		"trade_no":     "PROVIDER-ALLOWLIST-002",
		"status":       "SUCCESS",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposits/providers/fake/notifications", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.10:4567"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "35.220.239.87, 10.0.0.1")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("allowed source IP should succeed: status=%d body=%s", resp.Code, resp.Body.String())
	}
}
