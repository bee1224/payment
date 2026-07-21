package callback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const signatureVersion = "hmac-sha256-v1"

type Config struct {
	Path, MerchantID, CallbackKeyID, CallbackSigningSecret, ResponseMode, RecordsPath string
	TimeoutDelay, TimestampSkew                                                       time.Duration
	NonceCacheLimit                                                                   int
}
type Receiver struct {
	config Config
	mu     sync.Mutex
	nonces map[string]time.Time
	now    func() time.Time
}

type acceptanceRecord struct {
	ReceivedAt           string `json:"received_at"`
	Method               string `json:"method"`
	Path                 string `json:"path"`
	MerchantID           string `json:"merchant_id"`
	KeyID                string `json:"key_id"`
	CallbackTimestamp    string `json:"callback_timestamp"`
	NonceFingerprint     string `json:"nonce_fingerprint"`
	SignatureVersion     string `json:"signature_version"`
	SignatureFingerprint string `json:"signature_fingerprint"`
	BodySHA256           string `json:"body_sha256"`
	HMACValid            bool   `json:"hmac_valid"`
	Response             string `json:"response_mode"`
	HTTPStatus           int    `json:"http_status"`
}

func New(c Config) *Receiver {
	if c.TimestampSkew <= 0 {
		c.TimestampSkew = 300 * time.Second
	}
	if c.NonceCacheLimit <= 0 {
		c.NonceCacheLimit = 10000
	}
	return &Receiver{config: c, nonces: map[string]time.Time{}, now: time.Now}
}
func (r *Receiver) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) })
	m.HandleFunc("POST "+r.config.Path, r.receive)
	return m
}
func (r *Receiver) receive(w http.ResponseWriter, q *http.Request) {
	b, e := io.ReadAll(http.MaxBytesReader(w, q.Body, 1<<20))
	if e != nil {
		http.Error(w, "callback body too large", 413)
		return
	}
	status, ok := r.verify(q, b)
	if !ok {
		http.Error(w, "callback signature rejected", status)
		return
	}
	responseStatus := http.StatusOK
	switch r.config.ResponseMode {
	case "invalid_body":
		r.appendAcceptanceRecord(q, b, responseStatus)
		_, _ = w.Write([]byte("NOT_OK"))
	case "server_error":
		responseStatus = http.StatusServiceUnavailable
		r.appendAcceptanceRecord(q, b, responseStatus)
		http.Error(w, "controlled callback failure", 503)
	case "timeout":
		time.Sleep(r.config.TimeoutDelay)
		r.appendAcceptanceRecord(q, b, responseStatus)
		_, _ = w.Write([]byte("OK"))
	default:
		r.appendAcceptanceRecord(q, b, responseStatus)
		_, _ = w.Write([]byte("OK"))
	}
}

func (r *Receiver) appendAcceptanceRecord(q *http.Request, body []byte, status int) {
	if strings.TrimSpace(r.config.RecordsPath) == "" {
		return
	}
	hash := sha256.Sum256(body)
	record := acceptanceRecord{
		ReceivedAt:           r.now().UTC().Format(time.RFC3339Nano),
		Method:               q.Method,
		Path:                 q.URL.Path,
		MerchantID:           q.Header.Get("X-Callback-Merchant-Id"),
		KeyID:                q.Header.Get("X-Callback-Key-Id"),
		CallbackTimestamp:    q.Header.Get("X-Callback-Timestamp"),
		NonceFingerprint:     fingerprintHeader(q.Header.Get("X-Callback-Nonce")),
		SignatureVersion:     q.Header.Get("X-Callback-Signature-Version"),
		SignatureFingerprint: fingerprintHeader(q.Header.Get("X-Callback-Signature")),
		BodySHA256:           hex.EncodeToString(hash[:]),
		HMACValid:            true,
		Response:             r.config.ResponseMode,
		HTTPStatus:           status,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		log.Printf("merchant-sandbox callback record marshal failed: %v", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.config.RecordsPath), 0o700); err != nil {
		log.Printf("merchant-sandbox callback record directory failed: %v", err)
		return
	}
	f, err := os.OpenFile(r.config.RecordsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("merchant-sandbox callback record write failed: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		log.Printf("merchant-sandbox callback record write failed: %v", err)
	}
}

func fingerprintHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
func (r *Receiver) verify(q *http.Request, b []byte) (int, bool) {
	mid, kid, ts, nonce, sig := q.Header.Get("X-Callback-Merchant-Id"), q.Header.Get("X-Callback-Key-Id"), q.Header.Get("X-Callback-Timestamp"), q.Header.Get("X-Callback-Nonce"), q.Header.Get("X-Callback-Signature")
	if mid == "" || kid == "" || ts == "" || nonce == "" || sig == "" || q.Header.Get("X-Callback-Signature-Version") != signatureVersion || r.config.CallbackSigningSecret == "" {
		return 401, false
	}
	if (r.config.MerchantID != "" && mid != r.config.MerchantID) || (r.config.CallbackKeyID != "" && kid != r.config.CallbackKeyID) {
		return 401, false
	}
	n, e := strconv.ParseInt(ts, 10, 64)
	if e != nil {
		return 401, false
	}
	now := r.now()
	at := time.Unix(n, 0)
	if at.Before(now.Add(-r.config.TimestampSkew)) || at.After(now.Add(r.config.TimestampSkew)) {
		return 401, false
	}
	h := sha256.Sum256(b)
	c := strings.Join([]string{mid, kid, ts, nonce, q.Method, q.URL.Path, hex.EncodeToString(h[:])}, "\n")
	m := hmac.New(sha256.New, []byte(r.config.CallbackSigningSecret))
	_, _ = m.Write([]byte(c))
	p, e := hex.DecodeString(sig)
	if e != nil || !hmac.Equal(p, m.Sum(nil)) {
		return 401, false
	}
	if !r.useNonce(mid, kid, nonce, now) {
		return 409, false
	}
	return 200, true
}
func (r *Receiver) useNonce(mid, kid, nonce string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, x := range r.nonces {
		if !x.After(now) {
			delete(r.nonces, k)
		}
	}
	k := mid + "\n" + kid + "\n" + nonce
	if _, ok := r.nonces[k]; ok {
		return false
	}
	if len(r.nonces) >= r.config.NonceCacheLimit {
		return false
	}
	r.nonces[k] = now.Add(r.config.TimestampSkew)
	return true
}
