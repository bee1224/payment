package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"payment-service/internal/config"
	"payment-service/internal/domain"
	"payment-service/internal/provider"
	providerRY "payment-service/internal/provider/ry"
	"payment-service/internal/repository"
	"payment-service/internal/service"
)

const testRYSignKey = "test-ry-sign-key"

type fakeDepositGateway struct{}

func (fakeDepositGateway) CreateDepositPayment(domain.DepositOrder, string) (provider.DepositPaymentRequest, error) {
	return provider.DepositPaymentRequest{
		URL:    "https://provider.example/deposits",
		Method: http.MethodPost,
		Fields: map[string]string{"token": "test"},
		HTML:   "<html>deposit</html>",
	}, nil
}

func (fakeDepositGateway) VerifyDepositNotification(fields map[string]string) (provider.DepositNotification, error) {
	amountCents, _ := strconv.ParseInt(fields["amount_cents"], 10, 64)
	return provider.DepositNotification{
		OrderNo:     fields["order_no"],
		AmountCents: amountCents,
		TradeNo:     fields["trade_no"],
		Status:      fields["status"],
	}, nil
}

func newTestDepositRouter() (http.Handler, *service.DepositService) {
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
		providerRY.NewPayoutClient("", "", testRYSignKey, "https://merchant.example/api/payments/callback", time.Second),
	)
	return NewRouter(depositService, payoutService, config.AppConfig{}, config.RYConfig{
		SignKey:        testRYSignKey,
		MaxSkewSeconds: 300,
	}), depositService
}

func signedDepositOrderBody(t *testing.T, merchantOrderNo string) []byte {
	t.Helper()
	req := PayOrderRequest{
		PayCustomerID:  "M10001",
		PayApplyDate:   strconv.FormatInt(time.Now().Unix(), 10),
		PayOrderID:     merchantOrderNo,
		PayAmount:      "100",
		PayChannelID:   "1000",
		PayNotifyURL:   "https://merchant.example/callback",
		PayProductName: "Deposit Test",
	}
	signature, err := buildRYMD5Signature(req.payOrderSignFields(), testRYSignKey)
	if err != nil {
		t.Fatal(err)
	}
	req.PayMD5Sign = signature
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func signedDepositQueryBody(t *testing.T) []byte {
	t.Helper()
	req := QueryTransactionRequest{
		PayCustomerID: "M10001",
		PayApplyDate:  strconv.FormatInt(time.Now().Unix(), 10),
		PayOrderID:    []string{"MISSING"},
	}
	signature, err := buildRYMD5Signature(req.queryTransactionSignFields(), testRYSignKey)
	if err != nil {
		t.Fatal(err)
	}
	req.PayMD5Sign = signature
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func serveJSON(handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestPayOrderCanonicalAndCompatibilityRoutes(t *testing.T) {
	canonicalRouter, _ := newTestDepositRouter()
	compatibilityRouter, _ := newTestDepositRouter()
	body := signedDepositOrderBody(t, "DEPOSIT-COMPAT-001")

	canonical := serveJSON(canonicalRouter, http.MethodPost, "/api/pay_order", body)
	compatibility := serveJSON(compatibilityRouter, http.MethodPost, "/api/v1/deposits", body)

	if canonical.Code != http.StatusOK || compatibility.Code != http.StatusOK {
		t.Fatalf("unexpected statuses: canonical=%d compatibility=%d", canonical.Code, compatibility.Code)
	}
	if canonical.Header().Get("Deprecation") != "" {
		t.Fatal("canonical route must not be marked deprecated")
	}
	if compatibility.Header().Get("Deprecation") != "true" {
		t.Fatal("compatibility route must be marked deprecated")
	}
	if link := compatibility.Header().Get("Link"); !strings.Contains(link, "/api/pay_order") {
		t.Fatalf("compatibility route missing successor link: %q", link)
	}

	var canonicalResponse, compatibilityResponse PayOrderResponse
	if err := json.Unmarshal(canonical.Body.Bytes(), &canonicalResponse); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(compatibility.Body.Bytes(), &compatibilityResponse); err != nil {
		t.Fatal(err)
	}
	if canonicalResponse.Code != compatibilityResponse.Code || canonicalResponse.Data.OrderID != compatibilityResponse.Data.OrderID {
		t.Fatalf("canonical and compatibility routes returned different business responses")
	}
	if !strings.Contains(canonicalResponse.Data.ViewURL, "/api/v1/deposits/") ||
		!strings.Contains(compatibilityResponse.Data.ViewURL, "/api/v1/deposits/") {
		t.Fatal("deposit responses must advertise the canonical redirect URL")
	}
}

func TestQueryTransactionCanonicalAndCompatibilityResponsesMatch(t *testing.T) {
	router, _ := newTestDepositRouter()
	body := signedDepositQueryBody(t)

	canonical := serveJSON(router, http.MethodPost, "/api/query_transaction", body)
	compatibility := serveJSON(router, http.MethodPost, "/api/v1/deposits/query", body)

	if canonical.Code != http.StatusOK || compatibility.Code != http.StatusOK {
		t.Fatalf("unexpected statuses: canonical=%d compatibility=%d", canonical.Code, compatibility.Code)
	}
	if canonical.Body.String() != compatibility.Body.String() {
		t.Fatalf("query responses differ:\ncanonical=%s\ncompatibility=%s", canonical.Body.String(), compatibility.Body.String())
	}
	if compatibility.Header().Get("Deprecation") != "true" || canonical.Header().Get("Deprecation") != "" {
		t.Fatal("deprecation headers were not isolated to the compatibility route")
	}
}

func TestDepositProviderNotificationCanonicalAndLegacyRoutes(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		deprecated bool
	}{
		{name: "canonical", path: "/api/v1/deposits/providers/fake/notifications"},
		{name: "compatibility", path: "/notify/fake", deprecated: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, depositService := newTestDepositRouter()
			created, err := depositService.CreateDeposit(service.CreateDepositRequest{
				MerchantID:      "M10001",
				MerchantOrderNo: "NOTIFY-" + test.name,
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
				"trade_no":     "PROVIDER-001",
				"status":       "SUCCESS",
			})

			response := serveJSON(router, http.MethodPost, test.path, body)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"Status":"paid"`) {
				t.Fatalf("notification failed: status=%d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Deprecation"); (got == "true") != test.deprecated {
				t.Fatalf("unexpected deprecation header: %q", got)
			}
		})
	}
}

func TestPayoutNamespaceIsNotHandledByDepositRouter(t *testing.T) {
	router, _ := newTestDepositRouter()
	response := serveJSON(router, http.MethodPost, "/api/v1/payouts", []byte(`{}`))
	if response.Code != http.StatusNotFound {
		t.Fatalf("payout namespace must not be routed to deposits: got %d", response.Code)
	}
}

func TestRYPayoutRoutesUseProviderContract(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != providerRY.PayoutCreatePath {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		var req providerRY.CreatePayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.PayCustomerID != "50000" || req.PayMD5Sign == "" {
			t.Fatalf("request was not credentialed/signed: %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"transaction_id":"P123","amount":"100.0000"}}`))
	}))
	defer upstream.Close()

	_, depositService := newTestDepositRouter()
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient(upstream.URL, "50000", testRYSignKey, "https://merchant.example/api/payments/callback", time.Second),
	)
	body := []byte(`{
		"pay_order_id":"PAYOUT-001",
		"pay_amount":"100.00",
		"pay_account_name":"周傑倫",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	result := serveJSON(NewRouter(depositService, payoutService, config.AppConfig{}, config.RYConfig{
		BaseURL:            upstream.URL,
		CustomerID:         "50000",
		SignKey:            testRYSignKey,
		PayoutNotifyURL:    "https://merchant.example/api/payments/callback",
		HTTPTimeoutSeconds: 2,
	}), http.MethodPost, "/api/payments/pay_order", body)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"transaction_id":"P123"`) {
		t.Fatalf("payout create failed: status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestRYPayoutCallbackRequiresValidSignature(t *testing.T) {
	_, depositService := newTestDepositRouter()
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient("", "", testRYSignKey, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.RYConfig{SignKey: testRYSignKey})
	if _, err := payoutService.CreatePayoutOrder(context.Background(), service.CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "PAYOUT-001",
		Amount:           "100.00",
		Currency:         "TWD",
		PayAccountName:   "周傑倫",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	}); err != nil {
		t.Fatal(err)
	}
	callback := providerRY.PayoutCallbackRequest{
		CustomerID: 50000, OrderID: "PAYOUT-001", Amount: "100.0000",
		DateTime: "2026-07-05 12:00:00", TransactionID: "P123",
		TransactionCode: "30000", TransactionMsg: "支付成功",
	}
	callback.Sign, _ = providerRY.Sign(map[string]any{
		"customer_id": callback.CustomerID, "order_id": callback.OrderID,
		"amount": callback.Amount, "datetime": callback.DateTime,
		"transaction_id":   callback.TransactionID,
		"transaction_code": callback.TransactionCode,
		"transaction_msg":  callback.TransactionMsg,
	}, testRYSignKey)
	body, _ := json.Marshal(callback)
	result := serveJSON(router, http.MethodPost, "/api/payments/callback", body)
	if result.Code != http.StatusOK || result.Body.String() != "OK" {
		t.Fatalf("callback failed: status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestWorkflowPayoutCreateAndQuery(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient("", "50000", testRYSignKey, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.RYConfig{SignKey: testRYSignKey})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-001",
		"amount":"100.00",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"周傑倫",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusOK || !strings.Contains(createResp.Body.String(), `"status":"pending_review"`) {
		t.Fatalf("create payout workflow failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	queryBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-001"
	}`)
	queryResp := serveJSON(router, http.MethodPost, "/api/payouts/query", queryBody)
	if queryResp.Code != http.StatusOK || !strings.Contains(queryResp.Body.String(), `"merchant_payout_no":"WD-001"`) {
		t.Fatalf("query payout workflow failed: status=%d body=%s", queryResp.Code, queryResp.Body.String())
	}
}

func TestWorkflowPayoutRejectsUnsupportedBankCode(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient("", "50000", testRYSignKey, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.RYConfig{SignKey: testRYSignKey})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-002",
		"amount":"100.00",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"000"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusBadRequest || !strings.Contains(createResp.Body.String(), "RY supported bank code whitelist") {
		t.Fatalf("expected whitelist validation error: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
}

func TestWorkflowPayoutApproveRequiresReviewToken(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient("", "50000", testRYSignKey, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.RYConfig{
		SignKey:         testRYSignKey,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-003",
		"amount":"100.00",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create payout workflow failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Data struct {
			PayoutNo string `json:"payout_no"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/payouts/"+created.Data.PayoutNo+"/approve", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without review token, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestWorkflowPayoutApproveWithReviewTokenSucceeds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"transaction_id":"P123","amount":"100.0000"}}`))
	}))
	defer upstream.Close()

	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient(upstream.URL, "50000", testRYSignKey, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.RYConfig{
		BaseURL:         upstream.URL,
		CustomerID:      "50000",
		SignKey:         testRYSignKey,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-004",
		"amount":"100.00",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create payout workflow failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Data struct {
			PayoutNo string `json:"payout_no"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/payouts/"+created.Data.PayoutNo+"/approve", nil)
	req.Header.Set("X-Payout-Review-Token", "review-secret")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected approve success with review token, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestWorkflowPayoutCancelRequiresReviewToken(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient("", "50000", testRYSignKey, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.RYConfig{
		SignKey:         testRYSignKey,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-005",
		"amount":"100.00",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if createResp.Code != http.StatusOK {
		t.Fatalf("create payout workflow failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Data struct {
			PayoutNo string `json:"payout_no"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/payouts/"+created.Data.PayoutNo+"/cancel", strings.NewReader(`{"reason":"ops cancel"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without review token, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestWorkflowPayoutCancelWithReviewTokenSucceeds(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerRY.NewPayoutClient("", "50000", testRYSignKey, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.RYConfig{
		SignKey:         testRYSignKey,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-006",
		"amount":"100.00",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if createResp.Code != http.StatusOK {
		t.Fatalf("create payout workflow failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Data struct {
			PayoutNo string `json:"payout_no"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/payouts/"+created.Data.PayoutNo+"/cancel", strings.NewReader(`{"reason":"ops cancel"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Payout-Review-Token", "review-secret")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("expected cancel success, got %d body=%s", resp.Code, resp.Body.String())
	}
}
