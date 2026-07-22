package callback

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedRequest(t *testing.T, path string, body []byte) *http.Request {
	t.Helper()
	return signedRequestAt(t, path, body, strconv.FormatInt(time.Now().Unix(), 10), "nonce", "secret")
}

func signedRequestAt(t *testing.T, path string, body []byte, timestamp, nonce, secret string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	hash := sha256.Sum256(body)
	canonical := strings.Join([]string{"merchant", "v1", timestamp, nonce, "POST", path, hex.EncodeToString(hash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set("X-Callback-Merchant-Id", "merchant")
	req.Header.Set("X-Callback-Key-Id", "v1")
	req.Header.Set("X-Callback-Timestamp", timestamp)
	req.Header.Set("X-Callback-Nonce", nonce)
	req.Header.Set("X-Callback-Signature-Version", signatureVersion)
	req.Header.Set("X-Callback-Signature", hex.EncodeToString(mac.Sum(nil)))
	return req
}
func TestReceiverResponseModes(t *testing.T) {
	body := []byte(`{"transaction_id":"tx-1","status":"30000"}`)
	for _, tc := range []struct {
		mode   string
		status int
		body   string
	}{{"success", 200, "OK"}, {"invalid_body", 200, "NOT_OK"}, {"server_error", 503, "controlled callback failure"}, {"timeout", 200, "OK"}} {
		t.Run(tc.mode, func(t *testing.T) {
			r := New(Config{Path: "/callbacks/payment", MerchantID: "merchant", CallbackKeyID: "v1", CallbackSigningSecret: "secret", ResponseMode: tc.mode, TimeoutDelay: 20 * time.Millisecond})
			w := httptest.NewRecorder()
			r.Handler().ServeHTTP(w, signedRequest(t, "/callbacks/payment", body))
			if w.Code != tc.status || !bytes.Contains(w.Body.Bytes(), []byte(tc.body)) {
				t.Fatalf("status/body=%d/%q", w.Code, w.Body.String())
			}
		})
	}
}
func TestReceiverRejectsModifiedPayload(t *testing.T) {
	r := New(Config{Path: "/callbacks/payment", MerchantID: "merchant", CallbackKeyID: "v1", CallbackSigningSecret: "secret", ResponseMode: "success"})
	req := signedRequest(t, "/callbacks/payment", []byte(`{"status":"paid"}`))
	req.Body = io.NopCloser(bytes.NewReader([]byte(`{"status":"changed"}`)))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestReceiverRejectsReplayAndExpiredTimestamp(t *testing.T) {
	r := New(Config{Path: "/callbacks/payment", MerchantID: "merchant", CallbackKeyID: "v1", CallbackSigningSecret: "secret"})
	body := []byte(`{"status":"paid"}`)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, signedRequest(t, "/callbacks/payment", body))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, signedRequest(t, "/callbacks/payment", body))
	if w.Code != http.StatusConflict {
		t.Fatalf("replay=%d", w.Code)
	}
}

func TestReceiverRejectsInvalidAndFutureTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	r := New(Config{Path: "/callbacks/payment", MerchantID: "merchant", CallbackKeyID: "v1", CallbackSigningSecret: "secret", TimestampSkew: 5 * time.Minute})
	r.now = func() time.Time { return now }
	for _, tc := range []struct {
		name      string
		timestamp string
		secret    string
	}{
		{name: "invalid timestamp", timestamp: "not-a-time", secret: "secret"},
		{name: "expired", timestamp: strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10), secret: "secret"},
		{name: "too far in future", timestamp: strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10), secret: "secret"},
		{name: "wrong secret", timestamp: strconv.FormatInt(now.Unix(), 10), secret: "wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.Handler().ServeHTTP(w, signedRequestAt(t, "/callbacks/payment", []byte(`{"status":"paid"}`), tc.timestamp, tc.name, tc.secret))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

func TestReceiverWritesNonSensitiveAcceptanceRecord(t *testing.T) {
	path := t.TempDir() + "/callback-records.jsonl"
	r := New(Config{Path: "/callbacks/payment", MerchantID: "merchant", CallbackKeyID: "v1", CallbackSigningSecret: "secret", ResponseMode: "success", RecordsPath: path})
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, signedRequestAt(t, "/callbacks/payment", []byte(`{"order_id":"merchant-order-1","transaction_id":"tx-1","status":"paid"}`), strconv.FormatInt(time.Now().Unix(), 10), "record-nonce", "secret"))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte(`"hmac_valid":true`)) ||
		!bytes.Contains(contents, []byte(`"merchant_order_id":"merchant-order-1"`)) ||
		!bytes.Contains(contents, []byte(`"timestamp_valid":true`)) ||
		!bytes.Contains(contents, []byte(`"response_body_is_exact_ok":true`)) ||
		!bytes.Contains(contents, []byte(`"callback_timestamp":`)) ||
		!bytes.Contains(contents, []byte(`"nonce_fingerprint":`)) ||
		!bytes.Contains(contents, []byte(`"signature_fingerprint":`)) ||
		bytes.Contains(contents, []byte("secret")) ||
		bytes.Contains(contents, []byte("record-nonce")) ||
		bytes.Contains(contents, []byte("transaction_id")) {
		t.Fatalf("unexpected record %s", contents)
	}
}

func TestLoadAcceptanceStatusSummarizesMatchingRecords(t *testing.T) {
	path := t.TempDir() + "/callback-records.jsonl"
	r := New(Config{Path: "/callbacks/payment", MerchantID: "merchant", CallbackKeyID: "v1", CallbackSigningSecret: "secret", ResponseMode: "success", RecordsPath: path})
	body := []byte(`{"order_id":"merchant-order-1","transaction_id":"tx-1","status":"paid"}`)
	for _, nonce := range []string{"record-nonce-1", "record-nonce-2"} {
		w := httptest.NewRecorder()
		r.Handler().ServeHTTP(w, signedRequestAt(t, "/callbacks/payment", body, strconv.FormatInt(time.Now().Unix(), 10), nonce, "secret"))
		if w.Code != http.StatusOK || w.Body.String() != "OK" {
			t.Fatalf("status/body=%d/%q", w.Code, w.Body.String())
		}
	}
	status, err := LoadAcceptanceStatus(path, "merchant-order-1")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Received || status.ReceivedCount != 2 || status.HMACValid == nil || !*status.HMACValid || status.TimestampValid == nil || !*status.TimestampValid || status.NonceReplayDetected == nil || *status.NonceReplayDetected || status.SignatureVersion == nil || *status.SignatureVersion != signatureVersion || status.ResponseStatus == nil || *status.ResponseStatus != http.StatusOK || status.ResponseBodyExactOK == nil || !*status.ResponseBodyExactOK || status.FirstReceivedAt == nil || status.LastReceivedAt == nil {
		t.Fatalf("unexpected status: %+v", status)
	}
}
