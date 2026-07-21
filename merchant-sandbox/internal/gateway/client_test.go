package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignMatchesCanonicalHMACSHA256(t *testing.T) {
	body := []byte(`{"pay_order_id":"order-1"}`)
	got := Sign("customer", "1700000000", "nonce", "POST", "/api/pay_order", body, "secret")
	const want = "5f8f33d99a18459f46fec384b3b6ef5e81b2aa445fd054f267a9d483ed16e1d2"
	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}

func TestNewClientRejectsInsecureAndProductionURLs(t *testing.T) {
	for _, rawURL := range []string{"http://sandbox-api.nnviopp.com", "https://api.nnviopp.com"} {
		if _, err := NewClient(Credentials{BaseURL: rawURL}); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestCollectionClientBuildsDocumentedHMACHeaders(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pay_order" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		var body CollectionCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.PayCustomerID != "customer" {
			t.Fatalf("customer=%q", body.PayCustomerID)
		}
		raw, _ := json.Marshal(body)
		if got, want := r.Header.Get("X-Signature"), Sign("customer", r.Header.Get("X-Timestamp"), r.Header.Get("X-Nonce"), http.MethodPost, "/api/pay_order", raw, "customer-secret"); got != want {
			t.Fatalf("signature=%q want=%q", got, want)
		}
		if r.Header.Get("X-Customer-Id") != "customer" || r.Header.Get("X-Timestamp") == "" || r.Header.Get("X-Nonce") == "" {
			t.Fatalf("missing documented HMAC headers")
		}
		_, _ = w.Write([]byte(`{"data":{"order_id":"order-1"}}`))
	}))
	defer server.Close()
	client, err := NewClient(Credentials{BaseURL: server.URL, CustomerID: "customer", CustomerSecret: "customer-secret"})
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = server.Client()
	if _, err := client.CreateCollection(context.Background(), CollectionCreateRequest{PayApplyDate: "1700000000", PayOrderID: "order-1", PayAmount: 100, PayChannelID: "1000", PayNotifyURL: "https://merchant.example/callback"}); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsInvalidRequestsBeforeNetworkAndSanitizesHTTPError(t *testing.T) {
	client, err := NewClient(Credentials{BaseURL: "https://sandbox.example", CustomerID: "customer", CustomerSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateCollection(context.Background(), CollectionCreateRequest{PayApplyDate: "not-a-time"}); err == nil {
		t.Fatal("expected local request validation error")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("sensitive upstream detail"))
	}))
	defer server.Close()
	client.baseURL = server.URL
	client.httpClient = server.Client()
	_, err = client.CreateCollection(context.Background(), CollectionCreateRequest{PayApplyDate: "1700000000", PayOrderID: "order-1", PayAmount: 1, PayChannelID: "1000", PayNotifyURL: "https://merchant.example/callback"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestClientRejectsUnsafeCallbackURLs(t *testing.T) {
	client, err := NewClient(Credentials{BaseURL: "https://sandbox.example", CustomerID: "customer", CustomerSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, rawURL := range []string{"http://merchant.example/callback", "https://localhost/callback", "https://127.0.0.1/callback", "https://api.nnviopp.com/callback"} {
		_, err := client.CreateCollection(context.Background(), CollectionCreateRequest{PayApplyDate: "1700000000", PayOrderID: "order-1", PayAmount: 1, PayChannelID: "1000", PayNotifyURL: rawURL})
		if err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestPayoutClientInjectsMerchantCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/payouts/query" || r.Header.Get("X-Merchant-Id") != "merchant" {
			t.Fatalf("unexpected payout request")
		}
		var body PayoutQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.MerchantID != "merchant" || body.APIKey != "api-key" {
			t.Fatalf("credentials not injected: %+v", body)
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()
	client, err := NewClient(Credentials{BaseURL: server.URL, MerchantID: "merchant", MerchantSecret: "merchant-secret", APIKey: "api-key"})
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = server.Client()
	if _, err := client.QueryPayout(context.Background(), PayoutQueryRequest{MerchantPayoutNo: "payout-1"}); err != nil {
		t.Fatal(err)
	}
}
