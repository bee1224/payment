//go:build integration

package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/repository"
)

const workerDSNEnv = "PAYMENT_SERVICE_TEST_MARIADB_DSN"
const workerMigrationsEnv = "PAYMENT_SERVICE_TEST_MIGRATIONS_PATH"

func TestDepositCallbackWorkerFirstDeliveryHMAC(t *testing.T) {
	h := newWorkerHarness(t)
	secret := "integration-hmac-secret"
	customerID := "callback-test-customer"
	body := []byte(`{"event_key":"merchant.deposit:ORDER-HMAC-001:deposit.paid","order_no":"ORDER-HMAC-001","status":"paid"}`)
	server := newCallbackHTTPServer(t, secret, []int{http.StatusOK})
	defer server.Close()
	engine := &localCallbackEngine{client: server.Client()}
	svc := h.worker(engine, customerID, secret)
	h.insertTask(t, server.URL+"/merchant/callback?ignored=1", string(body), "merchant.deposit:ORDER-HMAC-001:deposit.paid")
	if err := svc.RetryDepositCallbacks(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	requests := server.requests()
	if len(requests) != 1 {
		t.Fatalf("HTTP requests=%d", len(requests))
	}
	requests[0].assertGatewayHMAC(t, secret, customerID, body, "/merchant/callback")
	if engine.calls != 1 {
		t.Fatalf("engine calls=%d", engine.calls)
	}
	h.assertTaskAttempt(t, "sent", "success", 1, "", 0)
}

func TestDepositCallbackWorkerRetryDeliveryHMAC(t *testing.T) {
	h := newWorkerHarness(t)
	secret := "integration-hmac-secret"
	customerID := "callback-test-customer"
	body := []byte(`{"event_key":"merchant.deposit:ORDER-HMAC-RETRY:deposit.paid","order_no":"ORDER-HMAC-RETRY","status":"paid"}`)
	server := newCallbackHTTPServer(t, secret, []int{http.StatusInternalServerError, http.StatusOK})
	defer server.Close()
	engine := &localCallbackEngine{client: server.Client()}
	svc := h.worker(engine, customerID, secret)
	h.insertTask(t, server.URL+"/merchant/callback", string(body), "merchant.deposit:ORDER-HMAC-RETRY:deposit.paid")
	if err := svc.RetryDepositCallbacks(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Minute)
	if err := svc.RetryDepositCallbacks(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	requests := server.requests()
	if len(requests) != 2 || engine.calls != 2 {
		t.Fatalf("requests=%d engine=%d", len(requests), engine.calls)
	}
	for _, request := range requests {
		request.assertGatewayHMAC(t, secret, customerID, body, "/merchant/callback")
	}
	if requests[0].nonce == requests[1].nonce || requests[0].signature == requests[1].signature {
		t.Fatal("retry must use fresh nonce and signature")
	}
	if requests[0].timestamp == requests[1].timestamp {
		t.Fatal("retry timestamp must advance with controlled worker clock")
	}
	if !bytes.Equal(requests[0].body, requests[1].body) || !bytes.Equal(requests[0].body, body) {
		t.Fatal("retry changed callback event identity payload")
	}
	h.assertTaskAttempt(t, "sent", "success", 2, "", 1)
	h.assertAttemptStatuses(t, []string{"failed", "success"})
}

func TestDepositCallbackWorkerMissingCustomerIDStopsTransport(t *testing.T) {
	h := newWorkerHarness(t)
	h.testMissingAuth(t, "", "integration-hmac-secret")
}

func TestDepositCallbackWorkerMissingHMACSecretStopsTransport(t *testing.T) {
	h := newWorkerHarness(t)
	h.testMissingAuth(t, "callback-test-customer", "")
}

func (h *workerHarness) testMissingAuth(t *testing.T, customerID, secret string) {
	t.Helper()
	server := newCallbackHTTPServer(t, "integration-hmac-secret", []int{http.StatusOK})
	defer server.Close()
	engine := &localCallbackEngine{client: server.Client()}
	svc := h.worker(engine, customerID, secret)
	h.insertTask(t, server.URL+"/merchant/callback", `{"event_key":"merchant.deposit:ORDER-AUTH:deposit.paid","order_no":"ORDER-AUTH","status":"paid"}`, "merchant.deposit:ORDER-AUTH:deposit.paid")
	if err := svc.RetryDepositCallbacks(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if got := len(server.requests()); got != 0 || engine.calls != 0 {
		t.Fatalf("transport ran: requests=%d engine=%d", got, engine.calls)
	}
	h.assertTaskAttempt(t, "dead_letter", "failed", 1, "callback_auth_config_missing", 1)
}

// localCallbackEngine is a test-only implementation of the production
// CallbackDeliveryEngine interface.  The production worker calls this shared
// instance for both deliveries; it performs real HTTP only to httptest.Server.
type localCallbackEngine struct {
	client *http.Client
	calls  int
	mu     sync.Mutex
}

func (e *localCallbackEngine) Deliver(ctx context.Context, request domain.CallbackDeliveryRequest) domain.DeliveryResult {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, request.URL, bytes.NewReader(request.Body))
	if err != nil {
		return domain.DeliveryResult{Error: err, ErrorCode: "request_build", Retryable: false}
	}
	for name, values := range request.Headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return domain.DeliveryResult{Error: err, ErrorCode: "connection_error", Retryable: true}
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	result := domain.DeliveryResult{HTTPStatus: resp.StatusCode, ResponseSummary: domain.SanitizeCallbackResponseSummary(response)}
	if resp.StatusCode >= 500 {
		result.Error = errors.New("callback returned non-2xx status")
		result.ErrorCode = "http_5xx"
		result.Retryable = true
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = errors.New("callback returned non-2xx status")
		result.ErrorCode = "http_4xx"
	} else if strings.TrimSpace(string(response)) != "OK" {
		result.Error = errors.New("callback response was not OK")
		result.ErrorCode = "response_not_ok"
		result.Retryable = true
	}
	return result
}

type capturedCallbackRequest struct {
	method, path, contentType, customerID, timestamp, nonce, signature string
	body                                                               []byte
}
type callbackHTTPServer struct {
	*httptest.Server
	expectedSecret string
	statuses       []int
	mu             sync.Mutex
	seen           []capturedCallbackRequest
}

func newCallbackHTTPServer(t *testing.T, secret string, statuses []int) *callbackHTTPServer {
	t.Helper()
	s := &callbackHTTPServer{expectedSecret: secret, statuses: statuses}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		index := len(s.seen)
		s.seen = append(s.seen, capturedCallbackRequest{r.Method, r.URL.EscapedPath(), r.Header.Get("Content-Type"), r.Header.Get("X-Customer-Id"), r.Header.Get("X-Timestamp"), r.Header.Get("X-Nonce"), r.Header.Get("X-Signature"), body})
		s.mu.Unlock()
		status := s.statuses[index]
		w.WriteHeader(status)
		if status >= 200 && status < 300 {
			_, _ = w.Write([]byte("OK"))
		} else {
			_, _ = w.Write([]byte("retry"))
		}
	}))
	return s
}
func (s *callbackHTTPServer) requests() []capturedCallbackRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedCallbackRequest(nil), s.seen...)
}
func (r capturedCallbackRequest) assertGatewayHMAC(t *testing.T, secret, customerID string, body []byte, path string) {
	t.Helper()
	if r.method != "POST" || r.path != path || r.contentType != "application/json" || r.customerID != customerID || r.timestamp == "" || r.nonce == "" || r.signature == "" || !bytes.Equal(r.body, body) {
		t.Fatalf("callback request shape invalid: method=%s path=%s contentType=%s customerMatch=%v timestamp=%v nonce=%v signature=%v bodyMatch=%v", r.method, r.path, r.contentType, r.customerID == customerID, r.timestamp != "", r.nonce != "", r.signature != "", bytes.Equal(r.body, body))
	}
	bodyHash := sha256.Sum256(r.body)
	canonical := strings.Join([]string{r.customerID, r.timestamp, r.nonce, strings.ToUpper(r.method), r.path, hex.EncodeToString(bodyHash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(r.signature)) {
		t.Fatal("independent Gateway HMAC verification failed")
	}
}

type workerHarness struct {
	t        *testing.T
	db       *sql.DB
	dsn      string
	store    *repository.MySQLDepositStore
	now      time.Time
	sequence int
}

func newWorkerHarness(t *testing.T) *workerHarness {
	t.Helper()
	dsn := os.Getenv(workerDSNEnv)
	migrations := os.Getenv(workerMigrationsEnv)
	if dsn == "" || migrations == "" {
		t.Skip("MariaDB integration environment is not set")
	}
	abs, err := filepath.Abs(migrations)
	if err != nil {
		t.Fatal(err)
	}
	releaseWorkerIntegrationFixtureLock(t, dsn)
	if err := repository.Migrate(dsn, abs); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{t: t, db: db, dsn: dsn, store: repository.NewMySQLDepositStore(db), now: time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{"DELETE FROM merchant_deposit_callback_attempts", "DELETE FROM merchant_deposit_callback_tasks", "DELETE FROM orders", "DELETE FROM merchants"} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return h
}
func releaseWorkerIntegrationFixtureLock(t *testing.T, dsn string) {
	t.Helper()
	lockDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := lockDB.Conn(context.Background())
	if err != nil {
		_ = lockDB.Close()
		t.Fatal(err)
	}
	var acquired int
	if err := conn.QueryRowContext(context.Background(), `SELECT GET_LOCK('payment-service-integration-fixture', 30)`).Scan(&acquired); err != nil || acquired != 1 {
		_ = conn.Close()
		_ = lockDB.Close()
		t.Fatalf("acquire integration fixture lock: acquired=%d err=%v", acquired, err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DO RELEASE_LOCK('payment-service-integration-fixture')`)
		_ = conn.Close()
		_ = lockDB.Close()
	})
}
func (h *workerHarness) worker(engine domain.CallbackDeliveryEngine, customerID, secret string) *DepositService {
	svc := NewPersistentDepositService(nil, nil, NewLedgerService(), h.store)
	svc.now = func() time.Time { return h.now }
	svc.deliveryEngine = engine
	svc.SetDepositCallbackGatewayHMAC(customerID, secret)
	return svc
}
func (h *workerHarness) insertTask(t *testing.T, url, payload, eventKey string) {
	t.Helper()
	h.sequence++
	code := fmt.Sprintf("worker-it-%d", h.sequence)
	var merchantID, orderID int64
	if err := h.db.QueryRow(`INSERT INTO merchants(code,name,api_key_hash) VALUES(?,'worker integration','hash') RETURNING id`, code).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	orderNo := fmt.Sprintf("worker-order-%d", h.sequence)
	if err := h.db.QueryRow(`INSERT INTO orders(merchant_id,order_no,merchant_order_no,amount_cents,currency,status,item_desc) VALUES(?,?,?,100,'TWD','pending','worker') RETURNING id`, merchantID, orderNo, orderNo).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.store.EnsureMerchantDepositCallbackTask(context.Background(), domain.MerchantDepositCallbackTask{MerchantID: merchantID, OrderID: orderID, EventKey: eventKey, CallbackURL: url, Payload: payload, NextRetryAt: h.now}); err != nil {
		t.Fatal(err)
	}
}
func (h *workerHarness) assertTaskAttempt(t *testing.T, wantTask, wantAttempt string, wantAttempts int, wantCode string, wantRetries int) {
	t.Helper()
	var status, code string
	var retries, count int
	if err := h.db.QueryRow(`SELECT status,retry_count,attempt_count,COALESCE(last_error,'') FROM merchant_deposit_callback_tasks LIMIT 1`).Scan(&status, &retries, &count, &code); err != nil {
		t.Fatal(err)
	}
	var attemptStatus string
	var attempts int
	if err := h.db.QueryRow(`SELECT COUNT(*),COALESCE(MAX(status),'') FROM merchant_deposit_callback_attempts`).Scan(&attempts, &attemptStatus); err != nil {
		t.Fatal(err)
	}
	if status != wantTask || attemptStatus != wantAttempt || count != wantAttempts || attempts != wantAttempts || code != wantCode || retries != wantRetries {
		t.Fatalf("task=%s retries=%d count=%d code=%s attempts=%d status=%s", status, retries, count, code, attempts, attemptStatus)
	}
}
func (h *workerHarness) assertAttemptStatuses(t *testing.T, want []string) {
	t.Helper()
	rows, err := h.db.Query(`SELECT status FROM merchant_deposit_callback_attempts ORDER BY attempt_no`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("attempt statuses=%v", got)
	}
}
