package ry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignMatchesProviderExample(t *testing.T) {
	fields := map[string]any{
		"bank_account": []string{
			"7000000000123456001",
			"7000000000123456002",
			"7000000000123456003",
		},
		"pay_amount":      "10000",
		"pay_apply_date":  "1693236045",
		"pay_channel_id":  "渠道代碼",
		"pay_customer_id": "88888",
		"pay_notify_url":  "https://貴司接收訂單通知的網址",
		"pay_order_id":    "TEST0123456",
		"user_name":       "客戶姓名",
	}
	got, err := Sign(fields, "12345")
	if err != nil {
		t.Fatal(err)
	}
	if got != "74A556F0414A605B538FC832B46A5420" {
		t.Fatalf("unexpected signature: %s", got)
	}
}

func TestCreatePayoutUsesProviderPathAndSignsPayload(t *testing.T) {
	var received CreatePayoutRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != PayoutCreatePath {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"transaction_id":"P123","amount":"100.0000"}}`))
	}))
	defer server.Close()

	client := NewPayoutClient(server.URL, "50000", "secret", "https://merchant.example/api/payments/callback", time.Second)
	client.now = func() time.Time { return time.Unix(1686642036, 0) }
	result, err := client.CreatePayout(context.Background(), CreatePayoutRequest{
		PayOrderID:     "ORDER-1",
		PayAmount:      "100.00",
		PayAccountName: "周傑倫",
		PayCardNo:      "202008372239",
		PayBankName:    "013",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Data == nil || result.Data.TransactionID != "P123" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if received.PayCustomerID != "50000" || received.PayApplyDate != "1686642036" {
		t.Fatalf("credentials/timestamp not populated: %#v", received)
	}
	fields := map[string]any{
		"pay_customer_id": received.PayCustomerID, "pay_apply_date": received.PayApplyDate,
		"pay_order_id": received.PayOrderID, "pay_notify_url": received.PayNotifyURL,
		"pay_amount": received.PayAmount, "pay_account_name": received.PayAccountName,
		"pay_card_no": received.PayCardNo, "pay_bank_name": received.PayBankName,
	}
	expected, err := Sign(fields, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if received.PayMD5Sign != expected {
		t.Fatalf("signature mismatch: got %s want %s", received.PayMD5Sign, expected)
	}
}

func TestQueryPayoutSignsOrderIDAsJSONArray(t *testing.T) {
	var received QueryPayoutRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PayoutQueryPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"status":1}}`))
	}))
	defer server.Close()
	client := NewPayoutClient(server.URL, "50000", "secret", "", time.Second)
	client.now = func() time.Time { return time.Unix(1686642036, 0) }
	_, err := client.QueryPayout(context.Background(), QueryPayoutRequest{PayOrderID: []string{"ORDER-1"}})
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := Sign(map[string]any{
		"pay_customer_id": "50000",
		"pay_apply_date":  "1686642036",
		"pay_order_id":    []string{"ORDER-1"},
	}, "secret")
	if received.PayMD5Sign != expected {
		t.Fatalf("array signature mismatch: got %s want %s", received.PayMD5Sign, expected)
	}
}

func TestCreatePayoutRejectsUnsupportedBankCode(t *testing.T) {
	client := NewPayoutClient("https://provider.example", "50000", "secret", "https://merchant.example/api/payments/callback", time.Second)
	client.now = func() time.Time { return time.Unix(1686642036, 0) }
	_, err := client.CreatePayout(context.Background(), CreatePayoutRequest{
		PayOrderID:     "ORDER-1",
		PayAmount:      "100.00",
		PayAccountName: "Tester",
		PayCardNo:      "202008372239",
		PayBankName:    "000",
	})
	if err == nil || err.Error() != "pay_bank_name is not in RY supported bank code whitelist" {
		t.Fatalf("expected whitelist error, got %v", err)
	}
}

func TestVerifyPayoutCallback(t *testing.T) {
	client := NewPayoutClient("https://provider.example", "50000", "secret", "", time.Second)
	req := PayoutCallbackRequest{
		CustomerID: 50000, OrderID: "ORDER-1", Amount: "300.0000",
		DateTime: "2020-05-12 21:06:57", TransactionID: "P123",
		TransactionCode: "30000", TransactionMsg: "支付成功",
	}
	req.Sign, _ = Sign(map[string]any{
		"customer_id": req.CustomerID, "order_id": req.OrderID, "amount": req.Amount,
		"datetime": req.DateTime, "transaction_id": req.TransactionID,
		"transaction_code": req.TransactionCode, "transaction_msg": req.TransactionMsg,
	}, "secret")
	if err := client.VerifyCallback(req); err != nil {
		t.Fatal(err)
	}
	req.Amount = "301.0000"
	if err := client.VerifyCallback(req); err == nil {
		t.Fatal("tampered callback must fail verification")
	}
}
