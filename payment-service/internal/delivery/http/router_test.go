package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
	"payment-service/internal/service"
)

const (
	testGatewayHMACSecret         = "test-gateway-hmac-secret"
	testPreviousGatewayHMACSecret = "test-gateway-hmac-secret-previous"
)

type fakeDepositGateway struct{}

type failingDepositGateway struct{}

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

func (failingDepositGateway) CreateDepositPayment(domain.DepositOrder, string) (provider.DepositPaymentRequest, error) {
	return provider.DepositPaymentRequest{}, context.DeadlineExceeded
}

func (failingDepositGateway) VerifyDepositNotification(fields map[string]string) (provider.DepositNotification, error) {
	return fakeDepositGateway{}.VerifyDepositNotification(fields)
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
		providerGateway.NewPayoutClient("", "", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	return NewRouter(depositService, payoutService, config.AppConfig{PublicBaseURL: "https://sandbox-api.nnviopp.com"}, config.GatewayConfig{
		HMACSecret:               testGatewayHMACSecret,
		MaxSkewSeconds:           300,
		DepositCallbackAllowlist: []string{"192.0.2.1"},
	}), depositService
}

func signedGatewayRequest(t *testing.T, customerID, secret, method, path string, body []byte, timestamp int64, nonce string) map[string]string {
	t.Helper()
	signature, err := providerGateway.BuildHMACSignature(providerGateway.HMACRequestAuth{
		CustomerID: customerID,
		Timestamp:  strconv.FormatInt(timestamp, 10),
		Nonce:      nonce,
		Method:     method,
		Path:       path,
		Body:       body,
	}, secret)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"X-Customer-Id": customerID,
		"X-Timestamp":   strconv.FormatInt(timestamp, 10),
		"X-Nonce":       nonce,
		"X-Signature":   signature,
	}
}

func signedDepositOrderBody(t *testing.T, merchantOrderNo string) []byte {
	t.Helper()
	req := PayOrderRequest{
		PayCustomerID:  "M10001",
		PayApplyDate:   gatewayFlexibleString(strconv.FormatInt(time.Now().Unix(), 10)),
		PayOrderID:     merchantOrderNo,
		PayAmount:      "100",
		PayChannelID:   "1000",
		PayNotifyURL:   "https://merchant.example/callback",
		PayProductName: "Deposit Test",
	}
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
		PayApplyDate:  gatewayFlexibleString(strconv.FormatInt(time.Now().Unix(), 10)),
		PayOrderID:    []string{"MISSING"},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func serveJSON(handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	return serveJSONWithHeaders(handler, method, path, body, nil)
}

func serveJSONWithHeaders(handler http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	return serveJSONWithHeadersAndRemoteAddr(handler, method, path, body, headers, "")
}

func serveJSONWithHeadersAndRemoteAddr(handler http.Handler, method, path string, body []byte, headers map[string]string, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func signMerchantWorkflowRequest(t *testing.T, merchantID, secret, method, path string, body []byte, timestamp int64, nonce string) map[string]string {
	t.Helper()
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{
		merchantID,
		strconv.FormatInt(timestamp, 10),
		nonce,
		strings.ToUpper(method),
		path,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")))
	return map[string]string{
		"X-Merchant-Id": merchantID,
		"X-Timestamp":   strconv.FormatInt(timestamp, 10),
		"X-Nonce":       nonce,
		"X-Signature":   hex.EncodeToString(mac.Sum(nil)),
	}
}

func TestPayOrderCanonicalAndCompatibilityRoutes(t *testing.T) {
	canonicalRouter, _ := newTestDepositRouter()
	compatibilityRouter, _ := newTestDepositRouter()
	body := signedDepositOrderBody(t, "DEPOSIT-COMPAT-001")
	headers := signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/pay_order", body, time.Now().Unix(), "deposit-canonical-001")
	compatibilityHeaders := signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/v1/deposits", body, time.Now().Unix(), "deposit-compatibility-001")

	canonical := serveJSONWithHeaders(canonicalRouter, http.MethodPost, "/api/pay_order", body, headers)
	compatibility := serveJSONWithHeaders(compatibilityRouter, http.MethodPost, "/api/v1/deposits", body, compatibilityHeaders)

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
	if !strings.HasPrefix(canonicalResponse.Data.ViewURL, "https://sandbox-api.nnviopp.com/api/v1/deposits/") ||
		!strings.HasPrefix(compatibilityResponse.Data.ViewURL, "https://sandbox-api.nnviopp.com/api/v1/deposits/") {
		t.Fatalf("deposit responses must advertise an absolute canonical redirect URL: canonical=%q compatibility=%q", canonicalResponse.Data.ViewURL, compatibilityResponse.Data.ViewURL)
	}
}

func TestPayOrderAcceptsNumericApplyDate(t *testing.T) {
	router, _ := newTestDepositRouter()
	applyDate := time.Now().Unix()
	body := []byte(`{
		"pay_customer_id":"M10001",
		"pay_apply_date":` + strconv.FormatInt(applyDate, 10) + `,
		"pay_order_id":"NUMERIC-APPLY-DATE-001",
		"pay_notify_url":"https://merchant.example/callback",
		"pay_amount":"100",
		"pay_channel_id":"1000"
	}`)

	resp := serveJSONWithHeaders(router, http.MethodPost, "/api/pay_order", body, signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/pay_order", body, time.Now().Unix(), "numeric-apply-001"))
	if resp.Code != http.StatusOK {
		t.Fatalf("numeric apply date should be accepted: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestPayOrderRejectsInvalidNotifyURL(t *testing.T) {
	router, _ := newTestDepositRouter()
	req := PayOrderRequest{
		PayCustomerID: "M10001",
		PayApplyDate:  gatewayFlexibleString(strconv.FormatInt(time.Now().Unix(), 10)),
		PayOrderID:    "INVALID-NOTIFY-001",
		PayAmount:     "100",
		PayChannelID:  "1000",
		PayNotifyURL:  "http://merchant.example/callback",
	}
	body, _ := json.Marshal(req)
	resp := serveJSONWithHeaders(router, http.MethodPost, "/api/pay_order", body, signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/pay_order", body, time.Now().Unix(), "invalid-notify-001"))
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "pay_notify_url") {
		t.Fatalf("invalid notify url should be rejected: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestPayOrderRejectsUnexpectedCustomerID(t *testing.T) {
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
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{
		HMACSecret:     testGatewayHMACSecret,
		MaxSkewSeconds: 300,
		CustomerID:     "RIG001",
	})
	req := PayOrderRequest{
		PayCustomerID: "M99999",
		PayApplyDate:  gatewayFlexibleString(strconv.FormatInt(time.Now().Unix(), 10)),
		PayOrderID:    "WRONG-CUSTOMER-001",
		PayNotifyURL:  "https://merchant.example/callback",
		PayAmount:     "100",
		PayChannelID:  "1000",
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp := serveJSONWithHeaders(router, http.MethodPost, "/api/pay_order", body, signedGatewayRequest(t, "M99999", testGatewayHMACSecret, http.MethodPost, "/api/pay_order", body, time.Now().Unix(), "wrong-customer-001"))
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "pay_customer_id") {
		t.Fatalf("unexpected customer must be rejected: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestQueryTransactionCanonicalAndCompatibilityResponsesMatch(t *testing.T) {
	router, _ := newTestDepositRouter()
	body := signedDepositQueryBody(t)
	headers := signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/query_transaction", body, time.Now().Unix(), "query-canonical-001")
	compatibilityHeaders := signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/v1/deposits/query", body, time.Now().Unix(), "query-compatibility-001")

	canonical := serveJSONWithHeaders(router, http.MethodPost, "/api/query_transaction", body, headers)
	compatibility := serveJSONWithHeaders(router, http.MethodPost, "/api/v1/deposits/query", body, compatibilityHeaders)

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

func TestQueryTransactionReturnsAbsoluteViewURL(t *testing.T) {
	router, depositService := newTestDepositRouter()
	created, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "QUERY-ABSOLUTE-URL-001",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(QueryTransactionRequest{
		PayCustomerID: "M10001",
		PayApplyDate:  gatewayFlexibleString(strconv.FormatInt(time.Now().Unix(), 10)),
		PayOrderID:    created.Order.MerchantOrderNo,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := serveJSONWithHeaders(router, http.MethodPost, "/api/query_transaction", body, signedGatewayRequest(t, "M10001", testGatewayHMACSecret, http.MethodPost, "/api/query_transaction", body, time.Now().Unix(), "query-absolute-url-001"))
	if resp.Code != http.StatusOK {
		t.Fatalf("query transaction: status=%d body=%s", resp.Code, resp.Body.String())
	}

	var response QueryTransactionResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("query result count = %d, want 1", len(response.Data))
	}
	want := "https://sandbox-api.nnviopp.com/api/v1/deposits/" + created.Order.OrderNo + "/redirect"
	if got := response.Data[0].ViewURL; got != want {
		t.Fatalf("view_url = %q, want %q", got, want)
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

func TestDepositProviderDuplicateSuccessNotifyCallbacksMerchantOnlyOnce(t *testing.T) {
	callbackHits := make(chan struct{}, 4)
	router, depositService := newTestDepositRouter()
	depositService.SetMerchantDepositCallbackDispatcher(
		func(domain.DepositOrder) (string, []byte, error) {
			return "https://merchant.example/callback", []byte(`{}`), nil
		},
		func(string, []byte) error { callbackHits <- struct{}{}; return nil },
	)
	created, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "DUPLICATE-NOTIFY-001",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
		NotifyURL:       "https://merchant.example/callback",
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
	first := serveJSON(router, http.MethodPost, "/api/v1/deposits/providers/fake/notifications", body)
	second := serveJSON(router, http.MethodPost, "/api/v1/deposits/providers/fake/notifications", body)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("notify route should stay successful: first=%d second=%d", first.Code, second.Code)
	}

	hits := 0
	for {
		select {
		case <-callbackHits:
			hits++
		case <-time.After(200 * time.Millisecond):
			if hits != 0 {
				t.Fatalf("notify handler must enqueue instead of sending directly, got %d direct callbacks", hits)
			}
			return
		}
	}
}

func TestDepositProviderNotifyRejectsDifferentTradeOrAmountAfterPaid(t *testing.T) {
	router, depositService := newTestDepositRouter()
	created, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID: "M10001", MerchantOrderNo: "DUPLICATE-NOTIFY-MISMATCH-001", Amount: 100,
		Currency: "TWD", ChannelCode: "CREDIT", NotifyURL: "https://merchant.example/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	firstBody, _ := json.Marshal(map[string]string{"order_no": created.Order.OrderNo, "amount_cents": "10000", "trade_no": "PROVIDER-001", "status": "SUCCESS"})
	if resp := serveJSON(router, http.MethodPost, "/api/v1/deposits/providers/fake/notifications", firstBody); resp.Code != http.StatusOK {
		t.Fatalf("first notify status=%d body=%s", resp.Code, resp.Body.String())
	}
	differentTrade, _ := json.Marshal(map[string]string{"order_no": created.Order.OrderNo, "amount_cents": "10000", "trade_no": "PROVIDER-002", "status": "SUCCESS"})
	if resp := serveJSON(router, http.MethodPost, "/api/v1/deposits/providers/fake/notifications", differentTrade); resp.Code != http.StatusBadRequest {
		t.Fatalf("different trade status=%d body=%s", resp.Code, resp.Body.String())
	}
	differentAmount, _ := json.Marshal(map[string]string{"order_no": created.Order.OrderNo, "amount_cents": "10100", "trade_no": "PROVIDER-001", "status": "SUCCESS"})
	if resp := serveJSON(router, http.MethodPost, "/api/v1/deposits/providers/fake/notifications", differentAmount); resp.Code != http.StatusBadRequest {
		t.Fatalf("different amount status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCreateDepositDoesNotPersistPendingOrderWhenGatewayBuildFails(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": failingDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)

	_, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "FAIL-BEFORE-PERSIST",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
	})
	if err == nil {
		t.Fatal("expected gateway build to fail")
	}
	if _, ok := depositService.FindDepositByMerchantOrderNo("M10001", "FAIL-BEFORE-PERSIST"); ok {
		t.Fatal("failed build must not leave persisted pending order")
	}
}

func TestPaidDepositDoesNotRegressToFailedOnLaterNotify(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)

	created, err := depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      "M10001",
		MerchantOrderNo: "NOTIFY-NO-REGRESS",
		Amount:          100,
		Currency:        "TWD",
		ChannelCode:     "CREDIT",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := depositService.HandleDepositProviderNotification("fake", map[string]string{
		"order_no":     created.Order.OrderNo,
		"amount_cents": "10000",
		"trade_no":     "PROVIDER-PAID",
		"status":       "SUCCESS",
	}, domain.DepositNotifyTrace{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Order.Status != domain.DepositOrderStatusPaid {
		t.Fatalf("expected paid status after success notify, got %s", first.Order.Status)
	}

	second, err := depositService.HandleDepositProviderNotification("fake", map[string]string{
		"order_no":     created.Order.OrderNo,
		"amount_cents": "10000",
		"trade_no":     "PROVIDER-FAILED",
		"status":       "FAILED",
	}, domain.DepositNotifyTrace{})
	if err == nil {
		t.Fatal("paid order with a different provider trade must be rejected")
	}
	if order, ok := depositService.FindDepositByOrderNo(created.Order.OrderNo); !ok || order.Status != domain.DepositOrderStatusPaid {
		t.Fatalf("paid order must not regress, got %+v", order)
	}
	_ = second
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
		if r.URL.Path != providerGateway.PayoutCreatePath {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		var req providerGateway.CreatePayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.PayCustomerID != "50000" {
			t.Fatalf("request customer id mismatch: %#v", req)
		}
		if r.Header.Get("X-Signature") == "" || r.Header.Get("X-Timestamp") == "" || r.Header.Get("X-Nonce") == "" {
			t.Fatalf("request missing HMAC headers")
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
		providerGateway.NewPayoutClient(upstream.URL, "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	body := []byte(`{
		"pay_order_id":"PAYOUT-001",
		"pay_amount":"100.00",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	result := serveJSONWithHeaders(NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{
		BaseURL:            upstream.URL,
		CustomerID:         "50000",
		HMACSecret:         testGatewayHMACSecret,
		PayoutNotifyURL:    "https://merchant.example/api/payments/callback",
		HTTPTimeoutSeconds: 2,
	}), http.MethodPost, "/api/payments/pay_order", body, signedGatewayRequest(t, "50000", testGatewayHMACSecret, http.MethodPost, "/api/payments/pay_order", body, time.Now().Unix(), "payout-create-001"))
	if result.Code != http.StatusGone || !strings.Contains(result.Body.String(), "use POST /api/payouts") {
		t.Fatalf("unsafe compatibility payout route should be retired: status=%d body=%s", result.Code, result.Body.String())
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
		providerGateway.NewPayoutClient("", "", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{TrustedProxyCIDRs: []string{"192.0.2.0/24"}}, config.GatewayConfig{
		HMACSecret:              testGatewayHMACSecret,
		PayoutCallbackAllowlist: []string{"35.220.239.87"},
	})
	if _, err := payoutService.CreatePayoutOrder(context.Background(), service.CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "PAYOUT-001",
		Amount:           "100",
		Currency:         "TWD",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	}); err != nil {
		t.Fatal(err)
	}
	callback := providerGateway.PayoutCallbackRequest{
		CustomerID: 50000, OrderID: "PAYOUT-001", Amount: "100.0000",
		DateTime: "2026-07-05 12:00:00", TransactionID: "P123",
		TransactionCode: "30000", TransactionMsg: "paid",
	}
	body, _ := json.Marshal(callback)
	headers := signedGatewayRequest(t, "50000", testGatewayHMACSecret, http.MethodPost, "/api/payments/callback", body, time.Now().Unix(), "payout-callback-001")
	headers["X-Forwarded-For"] = "35.220.239.87, 10.0.0.1"
	result := serveJSONWithHeadersAndRemoteAddr(router, http.MethodPost, "/api/payments/callback", body, headers, "192.0.2.10:4567")
	if result.Code != http.StatusOK || result.Body.String() != "OK" {
		t.Fatalf("callback failed: status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestRYPayoutCallbackAcceptsPreviousHMACSecret(t *testing.T) {
	_, depositService := newTestDepositRouter()
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerGateway.NewPayoutClient("", "", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{TrustedProxyCIDRs: []string{"192.0.2.0/24"}}, config.GatewayConfig{
		HMACSecret:              testGatewayHMACSecret,
		PreviousHMACSecret:      testPreviousGatewayHMACSecret,
		PayoutCallbackAllowlist: []string{"192.0.2.10"},
	})
	if _, err := payoutService.CreatePayoutOrder(context.Background(), service.CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "PAYOUT-ROTATE-001",
		Amount:           "100",
		Currency:         "TWD",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	}); err != nil {
		t.Fatal(err)
	}
	callback := providerGateway.PayoutCallbackRequest{
		CustomerID: 50000, OrderID: "PAYOUT-ROTATE-001", Amount: "100.0000",
		DateTime: "2026-07-05 12:00:00", TransactionID: "P124",
		TransactionCode: "30000", TransactionMsg: "paid",
	}
	body, _ := json.Marshal(callback)
	headers := signedGatewayRequest(t, "50000", testPreviousGatewayHMACSecret, http.MethodPost, "/api/payments/callback", body, time.Now().Unix(), "payout-callback-rotate-001")
	result := serveJSONWithHeadersAndRemoteAddr(router, http.MethodPost, "/api/payments/callback", body, headers, "192.0.2.10:4567")
	if result.Code != http.StatusOK || result.Body.String() != "OK" {
		t.Fatalf("callback signed by previous secret should succeed: status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestRYPayoutCallbackRejectsSourceIPOutsideAllowlist(t *testing.T) {
	_, depositService := newTestDepositRouter()
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutService(
		payoutStore,
		providerGateway.NewPayoutClient("", "", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{TrustedProxyCIDRs: []string{"192.0.2.0/24"}}, config.GatewayConfig{
		HMACSecret:              testGatewayHMACSecret,
		PayoutCallbackAllowlist: []string{"35.220.239.87"},
	})
	if _, err := payoutService.CreatePayoutOrder(context.Background(), service.CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "PAYOUT-ALLOWLIST-001",
		Amount:           "100",
		Currency:         "TWD",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	}); err != nil {
		t.Fatal(err)
	}
	callback := providerGateway.PayoutCallbackRequest{
		CustomerID: 50000, OrderID: "PAYOUT-ALLOWLIST-001", Amount: "100.0000",
		DateTime: "2026-07-05 12:00:00", TransactionID: "P125",
		TransactionCode: "30000", TransactionMsg: "paid",
	}
	body, _ := json.Marshal(callback)
	headers := signedGatewayRequest(t, "50000", testGatewayHMACSecret, http.MethodPost, "/api/payments/callback", body, time.Now().Unix(), "payout-callback-allowlist-001")
	headers["X-Forwarded-For"] = "203.0.113.10"
	result := serveJSONWithHeadersAndRemoteAddr(router, http.MethodPost, "/api/payments/callback", body, headers, "192.0.2.10:4567")
	if result.Code != http.StatusUnauthorized || !strings.Contains(result.Body.String(), "allowlist") {
		t.Fatalf("callback from blocked IP should be rejected: status=%d body=%s", result.Code, result.Body.String())
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
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
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

func TestWorkflowPayoutCreateAndQueryWithHMACSignature(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret, MaxSkewSeconds: 300})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"merchant_payout_no":"WD-HMAC-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payouts", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	for key, value := range signMerchantWorkflowRequest(t, "M10001", "merchant-secret", http.MethodPost, "/api/payouts", createBody, time.Now().Unix(), "nonce-create-001") {
		createReq.Header.Set(key, value)
	}
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK || !strings.Contains(createResp.Body.String(), `"status":"pending_review"`) {
		t.Fatalf("create payout workflow with HMAC failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	queryBody := []byte(`{
		"merchant_id":"M10001",
		"merchant_payout_no":"WD-HMAC-001"
	}`)
	queryReq := httptest.NewRequest(http.MethodPost, "/api/payouts/query", bytes.NewReader(queryBody))
	queryReq.Header.Set("Content-Type", "application/json")
	for key, value := range signMerchantWorkflowRequest(t, "M10001", "merchant-secret", http.MethodPost, "/api/payouts/query", queryBody, time.Now().Unix(), "nonce-query-001") {
		queryReq.Header.Set(key, value)
	}
	queryResp := httptest.NewRecorder()
	router.ServeHTTP(queryResp, queryReq)
	if queryResp.Code != http.StatusOK || !strings.Contains(queryResp.Body.String(), `"merchant_payout_no":"WD-HMAC-001"`) {
		t.Fatalf("query payout workflow with HMAC failed: status=%d body=%s", queryResp.Code, queryResp.Body.String())
	}

	var queryResult struct {
		Data struct {
			PayoutNo string `json:"payout_no"`
		} `json:"data"`
	}
	if err := json.Unmarshal(queryResp.Body.Bytes(), &queryResult); err != nil {
		t.Fatalf("unmarshal query response: %v", err)
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/payouts/"+queryResult.Data.PayoutNo, nil)
	for key, value := range signMerchantWorkflowRequest(t, "M10001", "merchant-secret", http.MethodGet, "/api/payouts/"+queryResult.Data.PayoutNo, nil, time.Now().Unix(), "nonce-get-001") {
		getReq.Header.Set(key, value)
	}
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK || !strings.Contains(getResp.Body.String(), `"payout_no":"`+queryResult.Data.PayoutNo+`"`) {
		t.Fatalf("get payout workflow with HMAC failed: status=%d body=%s", getResp.Code, getResp.Body.String())
	}
}

func TestWorkflowPayoutRejectsReplayNonce(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code: "M10001", Name: "Merchant 1", APIKey: "merchant-secret", Status: "active",
	}, 500000)
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret, MaxSkewSeconds: 300})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"merchant_payout_no":"WD-HMAC-REPLAY-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/payouts", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	for key, value := range signMerchantWorkflowRequest(t, "M10001", "merchant-secret", http.MethodPost, "/api/payouts", createBody, time.Now().Unix(), "nonce-replay-create") {
		createReq.Header.Set(key, value)
	}
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create payout workflow failed: status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	queryBody := []byte(`{
		"merchant_id":"M10001",
		"merchant_payout_no":"WD-HMAC-REPLAY-001"
	}`)
	headers := signMerchantWorkflowRequest(t, "M10001", "merchant-secret", http.MethodPost, "/api/payouts/query", queryBody, time.Now().Unix(), "nonce-replay-query")
	firstReq := httptest.NewRequest(http.MethodPost, "/api/payouts/query", bytes.NewReader(queryBody))
	firstReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		firstReq.Header.Set(key, value)
	}
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first signed query failed: status=%d body=%s", firstResp.Code, firstResp.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/payouts/query", bytes.NewReader(queryBody))
	secondReq.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		secondReq.Header.Set(key, value)
	}
	secondResp := httptest.NewRecorder()
	router.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusConflict || !strings.Contains(secondResp.Body.String(), "request has already been used") {
		t.Fatalf("replay nonce should be rejected: status=%d body=%s", secondResp.Code, secondResp.Body.String())
	}
}

func TestWorkflowPayoutCreateAcceptsHashedMerchantKey(t *testing.T) {
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	sum := sha256.Sum256([]byte("merchant-secret"))
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code:   "M10001",
		Name:   "Merchant 1",
		APIKey: hex.EncodeToString(sum[:]),
		Status: "active",
	}, 500000)
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-HASH-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusOK || !strings.Contains(createResp.Body.String(), `"status":"pending_review"`) {
		t.Fatalf("create payout workflow with hashed merchant key failed: status=%d body=%s", createResp.Code, createResp.Body.String())
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
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-002",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"000"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusBadRequest || !strings.Contains(createResp.Body.String(), "gateway supported bank code whitelist") {
		t.Fatalf("expected whitelist validation error: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
}

func TestWorkflowPayoutRejectsPrivateCallbackURL(t *testing.T) {
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
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://merchant.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-PRIVATE-CALLBACK-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"http://127.0.0.1/internal",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`)
	createResp := serveJSON(router, http.MethodPost, "/api/payouts", createBody)
	if createResp.Code != http.StatusBadRequest || !strings.Contains(createResp.Body.String(), "callback_url") {
		t.Fatalf("private callback url should be rejected: status=%d body=%s", createResp.Code, createResp.Body.String())
	}
}

func TestWorkflowPayoutApproveRequiresReviewToken(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
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
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{
		PayoutReviewToken: "review-secret",
		TrustedProxyCIDRs: []string{"192.0.2.0/24"},
	}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-003",
		"amount":"100",
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
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
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
		providerGateway.NewPayoutClient(upstream.URL, "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		BaseURL:         upstream.URL,
		CustomerID:      "50000",
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createBody := []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-004",
		"amount":"100",
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
	req.Header.Set("X-Operator-Id", "ops.approver")
	req.Header.Set("X-Checker-Id", "ops.checker")
	req.Header.Set("X-Request-ID", "req-approve-001")
	req.Header.Set("X-Review-Reason", "manual compliance approval")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected approve success with review token, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve should only mark status approved, got body=%s", resp.Body.String())
	}
	if upstreamCalls != 0 {
		t.Fatalf("approve should not dispatch upstream automatically, got %d upstream calls", upstreamCalls)
	}
}

func TestWorkflowPayoutCancelRequiresReviewToken(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
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
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-005",
		"amount":"100",
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
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
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
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-006",
		"amount":"100",
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
	req.Header.Set("X-Operator-Id", "ops.cancel")
	req.Header.Set("X-Checker-Id", "ops.checker")
	req.Header.Set("X-Request-ID", "req-cancel-001")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("expected cancel success, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestWorkflowPayoutResendCallbackWithReviewTokenSucceeds(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	sum := sha256.Sum256([]byte("merchant-secret"))
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutStore.SeedMerchant(domain.Merchant{
		Code:   "M10001",
		Name:   "Merchant 1",
		APIKey: hex.EncodeToString(sum[:]),
		Status: "active",
	}, 500000)
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-RESEND-001",
		"amount":"100",
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

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/payouts/"+created.Data.PayoutNo+"/cancel", strings.NewReader(`{"reason":"ops cancel"}`))
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelReq.Header.Set("X-Payout-Review-Token", "review-secret")
	cancelReq.Header.Set("X-Operator-Id", "ops.cancel")
	cancelReq.Header.Set("X-Checker-Id", "ops.checker")
	cancelReq.Header.Set("X-Request-ID", "req-cancel-002")
	cancelResp := httptest.NewRecorder()
	router.ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusOK {
		t.Fatalf("cancel payout workflow failed: status=%d body=%s", cancelResp.Code, cancelResp.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/payouts/"+created.Data.PayoutNo+"/resend-callback", nil)
	req.Header.Set("X-Payout-Review-Token", "review-secret")
	req.Header.Set("X-Operator-Id", "ops.resend")
	req.Header.Set("X-Checker-Id", "ops.checker")
	req.Header.Set("X-Request-ID", "req-resend-001")
	req.Header.Set("X-Review-Reason", "merchant callback replay requested")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected resend success, got %d body=%s", resp.Code, resp.Body.String())
	}

	tasks, err := payoutStore.ClaimDueMerchantPayoutCallbackTasks(context.Background(), time.Now().Add(time.Minute), time.Now().Add(-time.Minute), 10)
	if err != nil {
		t.Fatalf("list callback tasks: %v", err)
	}
	found := false
	for _, task := range tasks {
		if strings.Contains(task.Payload, `"merchant_payout_no":"WD-RESEND-001"`) {
			found = true
			for _, field := range []string{`"sign":`, `"signature":`, `"callback_timestamp":`, `"callback_nonce":`, `"signature_version":`} {
				if strings.Contains(task.Payload, field) {
					t.Fatalf("callback payload unexpectedly contains %s: %s", field, task.Payload)
				}
			}
		}
	}
	if !found {
		t.Fatal("expected resend callback task to be queued")
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/payouts/"+created.Data.PayoutNo+"/audit-logs?limit=5", nil)
	auditReq.Header.Set("X-Payout-Review-Token", "review-secret")
	auditResp := httptest.NewRecorder()
	router.ServeHTTP(auditResp, auditReq)
	if auditResp.Code != http.StatusOK {
		t.Fatalf("list payout audit logs failed: status=%d body=%s", auditResp.Code, auditResp.Body.String())
	}
	if !strings.Contains(auditResp.Body.String(), `"action":"cancel"`) ||
		!strings.Contains(auditResp.Body.String(), `"action":"resend_callback"`) ||
		!strings.Contains(auditResp.Body.String(), `"actor":"ops.resend"`) ||
		!strings.Contains(auditResp.Body.String(), `"request_id":"req-resend-001"`) {
		t.Fatalf("payout audit response missing review trail: body=%s", auditResp.Body.String())
	}
}

func TestMerchantAPIKeyIssueRotateListAndRevoke(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
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
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	issueReq := httptest.NewRequest(http.MethodPost, "/api/merchants/M10001/api-keys/issue", strings.NewReader(`{"previous_expires_at":"2026-07-31T00:00:00Z","reason":"bootstrap new operator key"}`))
	issueReq.Header.Set("Content-Type", "application/json")
	issueReq.Header.Set("X-Payout-Review-Token", "review-secret")
	issueReq.Header.Set("X-Operator-Id", "ops.alice")
	issueReq.Header.Set("X-Checker-Id", "ops.checker")
	issueReq.Header.Set("X-Request-ID", "req-issue-001")
	issueReq.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.8")
	issueReq.RemoteAddr = "192.0.2.10:4567"
	issueResp := httptest.NewRecorder()
	router.ServeHTTP(issueResp, issueReq)
	if issueResp.Code != http.StatusOK || !strings.Contains(issueResp.Body.String(), `"api_key":"`) {
		t.Fatalf("issue merchant api key failed: status=%d body=%s", issueResp.Code, issueResp.Body.String())
	}
	var issued struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("unmarshal issue response: %v", err)
	}
	if issued.Data.APIKey == "" {
		t.Fatal("issued api key must not be empty")
	}

	issuedKeyResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"`+issued.Data.APIKey+`",
		"merchant_payout_no":"WD-ISSUE-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if issuedKeyResp.Code != http.StatusOK {
		t.Fatalf("issued key should authenticate: status=%d body=%s", issuedKeyResp.Code, issuedKeyResp.Body.String())
	}

	legacyKeyResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-ISSUE-002",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if legacyKeyResp.Code != http.StatusOK {
		t.Fatalf("legacy key should remain valid during transition: status=%d body=%s", legacyKeyResp.Code, legacyKeyResp.Body.String())
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/merchants/M10001/api-keys/rotate", strings.NewReader(`{"api_key":"merchant-secret-v2","expires_at":"2026-12-31T00:00:00Z","previous_expires_at":"2026-08-15T00:00:00Z","reason":"scheduled rotation"}`))
	rotateReq.Header.Set("Content-Type", "application/json")
	rotateReq.Header.Set("X-Payout-Review-Token", "review-secret")
	rotateReq.Header.Set("X-Operator-Id", "ops.bob")
	rotateReq.Header.Set("X-Checker-Id", "ops.checker")
	rotateReq.Header.Set("X-Request-ID", "req-rotate-001")
	rotateReq.Header.Set("X-Real-IP", "198.51.100.24")
	rotateReq.RemoteAddr = "192.0.2.10:4567"
	rotateResp := httptest.NewRecorder()
	router.ServeHTTP(rotateResp, rotateReq)
	if rotateResp.Code != http.StatusOK || !strings.Contains(rotateResp.Body.String(), `"status":"active"`) {
		t.Fatalf("rotate merchant api key failed: status=%d body=%s", rotateResp.Code, rotateResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/merchants/M10001/api-keys", nil)
	listReq.Header.Set("X-Payout-Review-Token", "review-secret")
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK || !strings.Contains(listResp.Body.String(), `"is_primary":true`) {
		t.Fatalf("list merchant api keys failed: status=%d body=%s", listResp.Code, listResp.Body.String())
	}

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret-v2",
		"merchant_payout_no":"WD-ROTATE-001",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if createResp.Code != http.StatusOK {
		t.Fatalf("rotated key should authenticate: status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	oldKeyResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-ROTATE-002",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if oldKeyResp.Code != http.StatusOK {
		t.Fatalf("old key should remain valid during grace period: status=%d body=%s", oldKeyResp.Code, oldKeyResp.Body.String())
	}

	issuedAfterRotateResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"`+issued.Data.APIKey+`",
		"merchant_payout_no":"WD-ROTATE-002B",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if issuedAfterRotateResp.Code != http.StatusOK {
		t.Fatalf("prior issued key should remain valid during grace period: status=%d body=%s", issuedAfterRotateResp.Code, issuedAfterRotateResp.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodPost, "/api/merchants/M10001/api-keys/revoke", strings.NewReader(`{"api_key":"merchant-secret-v2","reason":"suspected leak"}`))
	revokeReq.Header.Set("Content-Type", "application/json")
	revokeReq.Header.Set("X-Payout-Review-Token", "review-secret")
	revokeReq.Header.Set("X-Actor", "ops.carol")
	revokeReq.Header.Set("X-Checker-Id", "ops.checker")
	revokeReq.Header.Set("X-Request-ID", "req-revoke-001")
	revokeReq.RemoteAddr = "192.0.2.33:4567"
	revokeResp := httptest.NewRecorder()
	router.ServeHTTP(revokeResp, revokeReq)
	if revokeResp.Code != http.StatusOK || !strings.Contains(revokeResp.Body.String(), `"status":"revoked"`) {
		t.Fatalf("revoke merchant api key failed: status=%d body=%s", revokeResp.Code, revokeResp.Body.String())
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/merchants/M10001/api-keys/audit-logs?limit=5", nil)
	auditReq.Header.Set("X-Payout-Review-Token", "review-secret")
	auditResp := httptest.NewRecorder()
	router.ServeHTTP(auditResp, auditReq)
	if auditResp.Code != http.StatusOK {
		t.Fatalf("list merchant api key audit logs failed: status=%d body=%s", auditResp.Code, auditResp.Body.String())
	}
	if !strings.Contains(auditResp.Body.String(), `"action":"issue"`) ||
		!strings.Contains(auditResp.Body.String(), `"action":"rotate"`) ||
		!strings.Contains(auditResp.Body.String(), `"action":"revoke"`) {
		t.Fatalf("audit response missing actions: body=%s", auditResp.Body.String())
	}
	if !strings.Contains(auditResp.Body.String(), `"actor":"ops.alice"`) ||
		!strings.Contains(auditResp.Body.String(), `"actor":"ops.bob"`) ||
		!strings.Contains(auditResp.Body.String(), `"actor":"ops.carol"`) {
		t.Fatalf("audit response missing actors: body=%s", auditResp.Body.String())
	}
	if !strings.Contains(auditResp.Body.String(), `"request_id":"req-rotate-001"`) ||
		!strings.Contains(auditResp.Body.String(), `"reason":"suspected leak"`) {
		t.Fatalf("audit response missing request metadata: body=%s", auditResp.Body.String())
	}
	if !strings.Contains(auditResp.Body.String(), `"source_ip":"192.0.2.33"`) &&
		!strings.Contains(auditResp.Body.String(), `"source_ip":"192.0.2.10"`) &&
		!strings.Contains(auditResp.Body.String(), `"source_ip":"198.51.100.24"`) {
		t.Fatalf("audit response missing source_ip metadata: body=%s", auditResp.Body.String())
	}

	revokedKeyResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret-v2",
		"merchant_payout_no":"WD-ROTATE-003",
		"amount":"100",
		"currency":"TWD",
		"callback_url":"https://merchant.example/payout-callback",
		"pay_account_name":"Tester",
		"pay_card_no":"202008372239",
		"pay_bank_name":"013"
	}`))
	if revokedKeyResp.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key should no longer authenticate: status=%d body=%s", revokedKeyResp.Code, revokedKeyResp.Body.String())
	}
}

func TestPayoutOperationalAlertListAndResolve(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
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
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	createResp := serveJSON(router, http.MethodPost, "/api/payouts", []byte(`{
		"merchant_id":"M10001",
		"api_key":"merchant-secret",
		"merchant_payout_no":"WD-ALERT-001",
		"amount":"100",
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
	if err := payoutStore.UpsertPayoutOperationalAlert(context.Background(), created.Data.PayoutNo, repository.PayoutOperationalAlertUpsert{
		Category: "merchant_callback_failed",
		Severity: "warning",
		Summary:  "merchant payout callback retry threshold exceeded",
		Details:  "status=502 body=FAIL",
	}); err != nil {
		t.Fatalf("UpsertPayoutOperationalAlert() error = %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/payouts/alerts?status=open&limit=10", nil)
	listReq.Header.Set("X-Payout-Review-Token", "review-secret")
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK || !strings.Contains(listResp.Body.String(), `"category":"merchant_callback_failed"`) {
		t.Fatalf("list payout alerts failed: status=%d body=%s", listResp.Code, listResp.Body.String())
	}

	var listed struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal list alerts response: %v", err)
	}
	if len(listed.Data) == 0 || listed.Data[0].ID == 0 {
		t.Fatalf("expected alert id in list response: %s", listResp.Body.String())
	}

	resolveReq := httptest.NewRequest(http.MethodPost, "/api/payouts/alerts/"+strconv.FormatInt(listed.Data[0].ID, 10)+"/resolve", strings.NewReader(`{"reason":"incident reviewed"}`))
	resolveReq.Header.Set("Content-Type", "application/json")
	resolveReq.Header.Set("X-Payout-Review-Token", "review-secret")
	resolveReq.Header.Set("X-Operator-Id", "ops.alert")
	resolveReq.Header.Set("X-Checker-Id", "ops.checker")
	resolveReq.Header.Set("X-Request-ID", "req-alert-resolve-001")
	resolveResp := httptest.NewRecorder()
	router.ServeHTTP(resolveResp, resolveReq)
	if resolveResp.Code != http.StatusOK {
		t.Fatalf("resolve payout alert failed: status=%d body=%s", resolveResp.Code, resolveResp.Body.String())
	}
}

func TestReconciliationEndpoints(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
	depositService := service.NewDepositService(
		map[string]provider.DepositGateway{"fake": fakeDepositGateway{}},
		map[string]string{"CREDIT": "fake"},
		service.NewLedgerService(),
	)
	payoutStore := repository.NewInMemoryPayoutStore()
	payoutService := service.NewPayoutServiceWithSecrets(
		payoutStore,
		providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second),
		nil,
	)
	reconciliationStore := repository.NewInMemoryReconciliationStore()
	reconciliationStore.SeedReport(domain.ReconciliationReport{
		ID: 2,
		Items: []domain.ReconciliationMismatch{{
			ID:           10,
			MerchantID:   1,
			MerchantCode: "M10001",
			MismatchType: domain.ReconciliationMismatchBalanceMismatch,
			EntityType:   "merchant_balance",
			TableName:    "merchant_balances",
			FieldName:    "available_cents",
		}, {
			ID:           11,
			MerchantID:   1,
			MerchantCode: "M10001",
			MismatchType: domain.ReconciliationMismatchDuplicateLedger,
			EntityType:   "ledger_entry",
			TableName:    "ledger_entries",
			FieldName:    "type",
		}},
	})
	payoutService.SetReconciliationService(service.NewReconciliationService(reconciliationStore))
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{
		HMACSecret:      testGatewayHMACSecret,
		PayoutNotifyURL: "https://payment-service.example/api/payments/callback",
	})

	runReq := httptest.NewRequest(http.MethodPost, "/api/reconciliation/run", strings.NewReader(`{"merchant_id":"M10001","order_no":"ORD-001"}`))
	runReq.Header.Set("Content-Type", "application/json")
	runReq.Header.Set("X-Payout-Review-Token", "review-secret")
	runResp := httptest.NewRecorder()
	router.ServeHTTP(runResp, runReq)
	if runResp.Code != http.StatusOK || !strings.Contains(runResp.Body.String(), `"scope_type":"partial"`) {
		t.Fatalf("run reconciliation failed: status=%d body=%s", runResp.Code, runResp.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/reconciliation/reports/1", nil)
	getReq.Header.Set("X-Payout-Review-Token", "review-secret")
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK || !strings.Contains(getResp.Body.String(), `"merchant_id":"M10001"`) {
		t.Fatalf("get reconciliation report failed: status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	adjustmentReq := httptest.NewRequest(http.MethodPost, "/api/reconciliation/items/10/adjustment", strings.NewReader(`{"amount":"12.34","currency":"TWD","note":"balance correction","reason":"verified provider settlement gap"}`))
	adjustmentReq.Header.Set("Content-Type", "application/json")
	adjustmentReq.Header.Set("X-Payout-Review-Token", "review-secret")
	adjustmentReq.Header.Set("X-Operator-Id", "ops.finance")
	adjustmentReq.Header.Set("X-Checker-Id", "ops.checker")
	adjustmentReq.Header.Set("X-Request-ID", "req-reconciliation-adjustment-001")
	adjustmentResp := httptest.NewRecorder()
	router.ServeHTTP(adjustmentResp, adjustmentReq)
	if adjustmentResp.Code != http.StatusOK || !strings.Contains(adjustmentResp.Body.String(), `"resolution_status":"resolved"`) || !strings.Contains(adjustmentResp.Body.String(), `"resolution_type":"adjustment"`) {
		t.Fatalf("resolve reconciliation adjustment failed: status=%d body=%s", adjustmentResp.Code, adjustmentResp.Body.String())
	}

	reversalReq := httptest.NewRequest(http.MethodPost, "/api/reconciliation/items/11/reversal", strings.NewReader(`{"ledger_entry_id":901,"note":"duplicate ledger reversal","reason":"duplicate entry confirmed"}`))
	reversalReq.Header.Set("Content-Type", "application/json")
	reversalReq.Header.Set("X-Payout-Review-Token", "review-secret")
	reversalReq.Header.Set("X-Operator-Id", "ops.finance")
	reversalReq.Header.Set("X-Checker-Id", "ops.checker")
	reversalReq.Header.Set("X-Request-ID", "req-reconciliation-reversal-001")
	reversalResp := httptest.NewRecorder()
	router.ServeHTTP(reversalResp, reversalReq)
	if reversalResp.Code != http.StatusOK || !strings.Contains(reversalResp.Body.String(), `"resolution_status":"resolved"`) || !strings.Contains(reversalResp.Body.String(), `"resolution_type":"reversal"`) || !strings.Contains(reversalResp.Body.String(), `"resolution_ledger_entry_id":901`) {
		t.Fatalf("resolve reconciliation reversal failed: status=%d body=%s", reversalResp.Code, reversalResp.Body.String())
	}

	resolvedReportReq := httptest.NewRequest(http.MethodGet, "/api/reconciliation/reports/2", nil)
	resolvedReportReq.Header.Set("X-Payout-Review-Token", "review-secret")
	resolvedReportResp := httptest.NewRecorder()
	router.ServeHTTP(resolvedReportResp, resolvedReportReq)
	if resolvedReportResp.Code != http.StatusOK || !strings.Contains(resolvedReportResp.Body.String(), `"resolved_by":"ops.finance"`) || !strings.Contains(resolvedReportResp.Body.String(), `"resolution_type":"reversal"`) {
		t.Fatalf("resolved reconciliation report did not retain closure state: status=%d body=%s", resolvedReportResp.Code, resolvedReportResp.Body.String())
	}
}

func TestReconciliationReportQueryAndTraceEndpoints(t *testing.T) {
	t.Skip("review-token authorization was retired; covered by MFA-backed admin authorization tests")
	depositService := service.NewDepositService(map[string]provider.DepositGateway{"fake": fakeDepositGateway{}}, map[string]string{"CREDIT": "fake"}, service.NewLedgerService())
	payoutService := service.NewPayoutServiceWithSecrets(repository.NewInMemoryPayoutStore(), providerGateway.NewPayoutClient("", "50000", testGatewayHMACSecret, "https://payment-service.example/api/payments/callback", time.Second), nil)
	reconciliationStore := repository.NewInMemoryReconciliationStore()
	now := time.Now()
	reconciliationStore.SeedReport(domain.ReconciliationReport{MerchantCode: "M10001", StartedAt: now, Items: []domain.ReconciliationMismatch{{MerchantCode: "M10001", EntityType: "order", OrderNo: "ORD-001", MismatchType: domain.ReconciliationMismatchMissingLedger}}})
	payoutService.SetReconciliationService(service.NewReconciliationService(reconciliationStore))
	router := NewRouter(depositService, payoutService, config.AppConfig{PayoutReviewToken: "review-secret"}, config.GatewayConfig{HMACSecret: testGatewayHMACSecret})

	reportsReq := httptest.NewRequest(http.MethodGet, "/api/reconciliation/reports?merchant_id=M10001&order_type=deposit&mismatch_type=missing_ledger", nil)
	reportsReq.Header.Set("X-Payout-Review-Token", "review-secret")
	reportsResp := httptest.NewRecorder()
	router.ServeHTTP(reportsResp, reportsReq)
	if reportsResp.Code != http.StatusOK || !strings.Contains(reportsResp.Body.String(), `"mismatch_type":"missing_ledger"`) {
		t.Fatalf("list reconciliation reports failed: status=%d body=%s", reportsResp.Code, reportsResp.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/reconciliation/trace?merchant_order_no=ORD-001", nil)
	traceReq.Header.Set("X-Payout-Review-Token", "review-secret")
	traceResp := httptest.NewRecorder()
	router.ServeHTTP(traceResp, traceReq)
	if traceResp.Code != http.StatusOK || !strings.Contains(traceResp.Body.String(), `"mismatches"`) {
		t.Fatalf("get reconciliation trace failed: status=%d body=%s", traceResp.Code, traceResp.Body.String())
	}
}
