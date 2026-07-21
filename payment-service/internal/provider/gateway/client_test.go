package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestCreatePayoutUsesProviderPathAndSignsPayload(t *testing.T) {
	var received CreatePayoutRequest
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != PayoutCreatePath {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		receivedHeaders = r.Header.Clone()
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
		PayAccountName: "Tester",
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
	body, err := json.Marshal(received)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := BuildHMACSignature(HMACRequestAuth{
		CustomerID: "50000",
		Timestamp:  receivedHeaders.Get("X-Timestamp"),
		Nonce:      receivedHeaders.Get("X-Nonce"),
		Method:     http.MethodPost,
		Path:       PayoutCreatePath,
		Body:       body,
	}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if receivedHeaders.Get("X-Signature") != expected {
		t.Fatalf("signature mismatch: got %s want %s", receivedHeaders.Get("X-Signature"), expected)
	}
}

func TestQueryPayoutSignsOrderIDAsJSONArray(t *testing.T) {
	var received QueryPayoutRequest
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PayoutQueryPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		receivedHeaders = r.Header.Clone()
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
	body, err := json.Marshal(received)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := BuildHMACSignature(HMACRequestAuth{
		CustomerID: "50000",
		Timestamp:  receivedHeaders.Get("X-Timestamp"),
		Nonce:      receivedHeaders.Get("X-Nonce"),
		Method:     http.MethodPost,
		Path:       PayoutQueryPath,
		Body:       body,
	}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if receivedHeaders.Get("X-Signature") != expected {
		t.Fatalf("array signature mismatch: got %s want %s", receivedHeaders.Get("X-Signature"), expected)
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
	if err == nil || err.Error() != "pay_bank_name is not in gateway supported bank code whitelist" {
		t.Fatalf("expected whitelist error, got %v", err)
	}
}

func TestVerifyPayoutCallback(t *testing.T) {
	client := NewPayoutClient("https://provider.example", "50000", "secret", "", time.Second)
	req := PayoutCallbackRequest{
		CustomerID: 50000, OrderID: "ORDER-1", Amount: "300.0000",
		DateTime: "2020-05-12 21:06:57", TransactionID: "P123",
		TransactionCode: "30000", TransactionMsg: "paid",
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	auth := HMACRequestAuth{
		CustomerID: "50000",
		Timestamp:  strconv.FormatInt(time.Now().Unix(), 10),
		Nonce:      "callback-nonce-001",
		Method:     http.MethodPost,
		Path:       "/api/payments/callback",
		Body:       body,
	}
	signature, err := BuildHMACSignature(auth, "secret")
	if err != nil {
		t.Fatal(err)
	}
	auth.Signature = signature
	if err := VerifyHMACRequest(auth, client.hmacSecret, time.Now(), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	req.Amount = "301.0000"
	body, _ = json.Marshal(req)
	auth.Body = body
	if err := VerifyHMACRequest(auth, client.hmacSecret, time.Now(), 5*time.Minute); err == nil {
		t.Fatal("tampered callback must fail verification")
	}
}
