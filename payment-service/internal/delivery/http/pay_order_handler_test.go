package http

import (
	"encoding/json"
	"testing"
	"time"

	"payment-service/internal/domain"
)

func TestFormatGatewayExpiryConvertsUTCToAsiaTaipei(t *testing.T) {
	utc := time.Date(2026, time.July, 18, 9, 47, 12, 0, time.UTC)
	if got, want := formatGatewayExpiry(&utc), "2026-07-18 17:47:12"; got != want {
		t.Fatalf("formatGatewayExpiry() = %q, want %q", got, want)
	}
}

func TestDepositHandlerBuildAbsoluteURLUsesConfiguredPublicBaseURL(t *testing.T) {
	handler := &DepositHandler{publicBaseURL: "https://sandbox-api.nnviopp.com"}
	if got, want := handler.buildAbsoluteURL("/api/v1/deposits/TX-001/redirect"), "https://sandbox-api.nnviopp.com/api/v1/deposits/TX-001/redirect"; got != want {
		t.Fatalf("buildAbsoluteURL() = %q, want %q", got, want)
	}
}

func TestNewebpayRedirectMetadata(t *testing.T) {
	fields := map[string]string{
		"MerchantID":  "masked",
		"TradeInfo":   "masked",
		"TradeSha":    "masked",
		"Version":     "2.3",
		"EncryptType": "0",
	}
	if !hasRequiredNewebpayPaymentFields(fields) {
		t.Fatal("required NewebPay fields should be recognized")
	}
	if got, want := newebpayEnvironment("core.newebpay.com"), "production"; got != want {
		t.Fatalf("newebpayEnvironment() = %q, want %q", got, want)
	}
	if got, want := newebpayEnvironment("ccore.newebpay.com"), "test"; got != want {
		t.Fatalf("newebpayEnvironment() = %q, want %q", got, want)
	}
}

func TestBuildGatewayDepositCallbackRequestContainsNoPayloadSignature(t *testing.T) {
	handler := &DepositHandler{}
	order := domain.DepositOrder{
		MerchantCode:    "QAT001",
		MerchantOrderNo: "ORDER-001",
		OrderNo:         "TX-001",
		AmountCents:     10000,
		Status:          domain.DepositOrderStatusPaid,
		CallbackURL:     "https://merchant.example/callback",
		UserName:        "QAT Tester",
		ItemDesc:        "Sandbox callback verification",
	}

	_, body, err := handler.buildGatewayDepositCallbackRequest(order)
	if err != nil {
		t.Fatalf("build callback: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	for _, field := range []string{"sign", "signature", "callback_timestamp", "callback_nonce", "signature_version"} {
		if _, exists := payload[field]; exists {
			t.Fatalf("payload must not include %s", field)
		}
	}
}

func TestParseGatewayAmountAcceptsJSONNumber(t *testing.T) {
	amount, err := parseGatewayAmount(json.Number("100"))
	if err != nil || amount != 100 {
		t.Fatalf("amount=%d error=%v", amount, err)
	}
}
