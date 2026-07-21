package http

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"payment-service/internal/config"
	"payment-service/internal/repository"
	"payment-service/internal/service"
)

const adminSessionCookie = "payment_admin_session"

const (
	adminLoginFailureLimit  = 5
	adminLoginFailureWindow = 15 * time.Minute
)

type adminLoginFailure struct {
	count     int
	firstSeen time.Time
}

type adminSession struct {
	Username    string `json:"u"`
	Role        string `json:"r"`
	Expires     int64  `json:"e"`
	CSRF        string `json:"c"`
	MFAVerified bool   `json:"m"`
}
type AdminHandler struct {
	payouts                     *service.PayoutService
	deposits                    *service.DepositService
	manual                      *service.ManualPayoutService
	secret                      []byte
	username, passwordHash      string
	secureCookie                bool
	cookieDomain                string
	cookieSameSite              http.SameSite
	users                       *repository.AdminUserStore
	cipher                      repository.MerchantSecretCipher
	testDepositCallbacksEnabled bool
	loginFailures               map[string]adminLoginFailure
	loginFailuresMu             sync.Mutex
}

func NewAdminHandler(payouts *service.PayoutService, cfg config.AppConfig) *AdminHandler {
	sameSite := http.SameSiteLaxMode
	if strings.EqualFold(cfg.AdminCookieSameSite, "strict") {
		sameSite = http.SameSiteStrictMode
	}
	return &AdminHandler{payouts: payouts, secret: []byte(strings.TrimSpace(cfg.AdminSessionSecret)), username: strings.TrimSpace(cfg.AdminInitialUsername), passwordHash: strings.TrimSpace(cfg.AdminInitialPasswordHash), secureCookie: strings.EqualFold(strings.TrimSpace(cfg.Env), "production"), cookieDomain: strings.TrimSpace(cfg.AdminCookieDomain), cookieSameSite: sameSite, cipher: repository.NewMerchantSecretCipher(cfg.MerchantSecretEncryptionKey), testDepositCallbacksEnabled: cfg.TestDepositCallbacksEnabled, loginFailures: make(map[string]adminLoginFailure)}
}
func (h *AdminHandler) SetManualPayoutService(v *service.ManualPayoutService) { h.manual = v }
func (h *AdminHandler) SetAdminUserStore(v *repository.AdminUserStore)        { h.users = v }
func (h *AdminHandler) SetDepositService(v *service.DepositService)           { h.deposits = v }
func (h *AdminHandler) configured() bool {
	return len(h.secret) >= 32 && h.username != "" && h.passwordHash != ""
}

func (h *AdminHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.configured() {
		writeAdminError(w, http.StatusServiceUnavailable, "admin console is not configured", adminRequestID(r))
		return
	}
	if r.Method != http.MethodPost {
		writeAdminError(w, http.StatusMethodNotAllowed, "method not allowed", adminRequestID(r))
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeAdminJSON(r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid login payload", adminRequestID(r))
		return
	}
	username := strings.ToUpper(strings.TrimSpace(req.Username))
	loginKey := adminLoginKey(r, username)
	if h.loginRateLimited(loginKey, time.Now()) {
		writeAdminError(w, http.StatusTooManyRequests, "too many login attempts; try again later", adminRequestID(r))
		return
	}
	hash, role, mfaSecret := h.passwordHash, "ADMIN", ""
	if h.users != nil {
		user, err := h.users.FindActive(r.Context(), username)
		if err != nil {
			h.recordLoginFailure(loginKey, time.Now())
			writeAdminError(w, http.StatusUnauthorized, "invalid username or password", adminRequestID(r))
			return
		}
		hash, role, mfaSecret = user.PasswordHash, user.Role, user.MFASecret
	} else if subtle.ConstantTimeCompare([]byte(username), []byte(h.username)) != 1 {
		h.recordLoginFailure(loginKey, time.Now())
		writeAdminError(w, http.StatusUnauthorized, "invalid username or password", adminRequestID(r))
		return
	}
	if !verifyAdminPassword(req.Password, hash) {
		h.recordLoginFailure(loginKey, time.Now())
		h.auditAuth(r, username, "login_password_rejected")
		writeAdminError(w, http.StatusUnauthorized, "invalid username or password", adminRequestID(r))
		return
	}
	mfaVerified := h.users == nil
	if mfaSecret != "" {
		plain, err := h.cipher.DecryptIfEncrypted(mfaSecret)
		if err != nil || !validTOTP(plain, r.Header.Get("X-MFA-Code"), time.Now()) {
			h.recordLoginFailure(loginKey, time.Now())
			h.auditAuth(r, username, "login_mfa_rejected")
			writeAdminError(w, http.StatusUnauthorized, "valid MFA code is required", adminRequestID(r))
			return
		}
		mfaVerified = true
	}
	h.clearLoginFailures(loginKey)
	s := adminSession{Username: username, Role: strings.ToUpper(strings.TrimSpace(role)), Expires: time.Now().Add(8 * time.Hour).Unix(), CSRF: randomAdminToken(h.secret, time.Now()), MFAVerified: mfaVerified}
	h.setSession(w, s)
	h.auditAuth(r, username, "login_succeeded")
	if !s.MFAVerified {
		writeAdminJSON(w, http.StatusAccepted, map[string]any{"username": s.Username, "role": s.Role, "mfa_enrollment_required": true, "csrf_token": s.CSRF}, adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"username": s.Username, "role": s.Role, "csrf_token": s.CSRF, "mfa_verified": true}, adminRequestID(r))
}

func adminLoginKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	return host + "\x00" + username
}

func (h *AdminHandler) loginRateLimited(key string, now time.Time) bool {
	h.loginFailuresMu.Lock()
	defer h.loginFailuresMu.Unlock()
	failure, ok := h.loginFailures[key]
	if !ok || now.Sub(failure.firstSeen) >= adminLoginFailureWindow {
		if ok {
			delete(h.loginFailures, key)
		}
		return false
	}
	return failure.count >= adminLoginFailureLimit
}

func (h *AdminHandler) recordLoginFailure(key string, now time.Time) {
	h.loginFailuresMu.Lock()
	defer h.loginFailuresMu.Unlock()
	failure := h.loginFailures[key]
	if failure.firstSeen.IsZero() || now.Sub(failure.firstSeen) >= adminLoginFailureWindow {
		failure = adminLoginFailure{firstSeen: now}
	}
	failure.count++
	h.loginFailures[key] = failure
}

func (h *AdminHandler) clearLoginFailures(key string) {
	h.loginFailuresMu.Lock()
	defer h.loginFailuresMu.Unlock()
	delete(h.loginFailures, key)
}
func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	_ = s
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: "", Path: "/", Domain: h.cookieDomain, MaxAge: -1, HttpOnly: true, Secure: h.secureCookie, SameSite: h.cookieSameSite})
	writeAdminJSON(w, http.StatusOK, map[string]any{}, adminRequestID(r))
}
func (h *AdminHandler) Me(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(r)
	if !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"username": s.Username, "role": s.Role, "csrf_token": s.CSRF, "mfa_verified": s.MFAVerified}, adminRequestID(r))
}

// BeginMFAEnrollment only accepts a password-authenticated provisional session.
// The secret is held encrypted server-side and expires after ten minutes.
func (h *AdminHandler) BeginMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "")
	if !ok || s.MFAVerified || h.users == nil || !h.cipher.Enabled() {
		writeAdminError(w, http.StatusForbidden, "MFA enrollment is unavailable", adminRequestID(r))
		return
	}
	secret, err := newTOTPSecret()
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "unable to generate MFA secret", adminRequestID(r))
		return
	}
	encrypted, err := h.cipher.Encrypt(secret)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "unable to protect MFA secret", adminRequestID(r))
		return
	}
	if err := h.users.BeginMFAEnrollment(r.Context(), s.Username, encrypted); err != nil {
		writeAdminError(w, http.StatusBadGateway, "unable to start MFA enrollment", adminRequestID(r))
		return
	}
	h.auditAuth(r, s.Username, "mfa_enrollment_started")
	issuer := url.QueryEscape("payment-service")
	label := url.QueryEscape("payment-service:" + s.Username)
	writeAdminJSON(w, http.StatusOK, map[string]any{"secret": secret, "otpauth_url": "otpauth://totp/" + label + "?secret=" + secret + "&issuer=" + issuer + "&algorithm=SHA1&digits=6&period=30"}, adminRequestID(r))
}
func (h *AdminHandler) ConfirmMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeSession(r, "")
	if !ok || s.MFAVerified || h.users == nil {
		writeAdminError(w, http.StatusForbidden, "MFA enrollment is unavailable", adminRequestID(r))
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeAdminJSON(r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid MFA confirmation", adminRequestID(r))
		return
	}
	encrypted, err := h.users.EnrollmentSecret(r.Context(), s.Username)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "MFA enrollment has expired", adminRequestID(r))
		return
	}
	secret, err := h.cipher.DecryptIfEncrypted(encrypted)
	if err != nil || !validTOTP(secret, req.Code, time.Now()) {
		writeAdminError(w, http.StatusUnauthorized, "invalid MFA code", adminRequestID(r))
		return
	}
	if err := h.users.CompleteMFAEnrollment(r.Context(), s.Username, encrypted); err != nil {
		writeAdminError(w, http.StatusBadGateway, "unable to enable MFA", adminRequestID(r))
		return
	}
	h.auditAuth(r, s.Username, "mfa_enrollment_completed")
	s.MFAVerified = true
	h.setSession(w, s)
	writeAdminJSON(w, http.StatusOK, map[string]any{"mfa_verified": true}, adminRequestID(r))
}
func (h *AdminHandler) Payouts(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeSession(r, "payout.read"); !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	page, pageSize := adminPositiveInt(r.URL.Query().Get("page"), 1), adminPositiveInt(r.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	var from, to *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("created_from")); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			from = &parsed
		} else {
			writeAdminError(w, http.StatusBadRequest, "invalid created_from", adminRequestID(r))
			return
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_to")); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			to = &parsed
		} else {
			writeAdminError(w, http.StatusBadRequest, "invalid created_to", adminRequestID(r))
			return
		}
	}
	if from != nil && to != nil && from.After(*to) {
		writeAdminError(w, http.StatusBadRequest, "created_from must not be after created_to", adminRequestID(r))
		return
	}
	orders, err := h.payouts.ListPayoutOrdersForAdminPage(r.Context(), service.AdminPayoutListRequest{Page: page, PageSize: pageSize, Status: r.URL.Query().Get("status"), Query: r.URL.Query().Get("query"), CreatedFrom: from, CreatedTo: to})
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "unable to load payouts", adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, orders, adminRequestID(r))
}

func adminPositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
func (h *AdminHandler) Collections(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeSession(r, "collection.read"); !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	if h.deposits == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "collection service is unavailable", adminRequestID(r))
		return
	}
	orders, err := h.deposits.ListDepositOrdersForAdmin(r.Context(), 100)
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "unable to load collections", adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, orders, adminRequestID(r))
}
func (h *AdminHandler) SendTestDepositCallback(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "deposit.callback.test")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	if !h.testDepositCallbacksEnabled {
		writeAdminError(w, http.StatusNotFound, "test deposit callbacks are disabled", adminRequestID(r))
		return
	}
	if h.deposits == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "collection service is unavailable", adminRequestID(r))
		return
	}
	orderNo := strings.TrimSpace(r.PathValue("order_no"))
	var req struct {
		ConfirmOrderNo string `json:"confirm_order_no"`
	}
	if err := decodeAdminJSON(r, &req); err != nil || subtle.ConstantTimeCompare([]byte(orderNo), []byte(strings.TrimSpace(req.ConfirmOrderNo))) != 1 {
		writeAdminError(w, http.StatusBadRequest, "confirm_order_no must match the order number", adminRequestID(r))
		return
	}
	h.auditAuth(r, s.Username, "test_deposit_callback_requested")
	order, err := h.deposits.SendTestPaidCallback(r.Context(), orderNo)
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "test deposit callback failed", adminRequestID(r))
		return
	}
	h.auditAuth(r, s.Username, "test_deposit_callback_sent")
	writeAdminJSON(w, http.StatusOK, map[string]any{"order_no": order.OrderNo, "merchant_order_no": order.MerchantOrderNo, "status": "paid", "test": true}, adminRequestID(r))
}
func (h *AdminHandler) PayoutDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeSession(r, "payout.read"); !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	no := strings.TrimSpace(r.PathValue("payout_no"))
	order, err := h.payouts.GetPayoutOrderForAdmin(r.Context(), no)
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "payout not found", adminRequestID(r))
		return
	}
	audit, _ := h.payouts.ListPayoutReviewAuditLogs(r.Context(), no, 50)
	result := map[string]any{"payout": service.BuildPayoutOrderView(order), "audit_logs": audit}
	if h.manual != nil {
		if manual, err := h.manual.Find(r.Context(), no); err == nil {
			result["manual_case"] = manual
		}
	}
	writeAdminJSON(w, http.StatusOK, result, adminRequestID(r))
}
func (h *AdminHandler) StartManualPayout(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "manual_payout.start")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	result, err := h.manual.Start(r.Context(), r.PathValue("payout_no"), s.Username, adminRequestID(r))
	h.writeManualResult(w, result, err, r)
}
func (h *AdminHandler) UploadManualReceipt(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "manual_payout.receipt.upload")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 11*1024*1024)
	if err := r.ParseMultipartForm(11 * 1024 * 1024); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid receipt upload", adminRequestID(r))
		return
	}
	f, header, err := r.FormFile("receipt")
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "receipt is required", adminRequestID(r))
		return
	}
	defer f.Close()
	result, err := h.manual.UploadReceipt(r.Context(), r.PathValue("payout_no"), header.Filename, header.Header.Get("Content-Type"), s.Username, adminRequestID(r), f)
	h.writeManualResult(w, result, err, r)
}
func (h *AdminHandler) ConfirmManualPayout(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "manual_payout.confirm")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	result, err := h.manual.Confirm(r.Context(), r.PathValue("payout_no"), s.Username, adminRequestID(r))
	h.writeManualResult(w, result, err, r)
}
func (h *AdminHandler) FailManualPayout(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "manual_payout.confirm")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeAdminJSON(r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid failure payload", adminRequestID(r))
		return
	}
	result, err := h.manual.Fail(r.Context(), r.PathValue("payout_no"), s.Username, req.Reason, adminRequestID(r))
	h.writeManualResult(w, result, err, r)
}
func (h *AdminHandler) CancelManualPayout(w http.ResponseWriter, r *http.Request) {
	s, ok := h.authorizeWrite(r, "manual_payout.cancel")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeAdminJSON(r, &req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid cancellation payload", adminRequestID(r))
		return
	}
	result, err := h.manual.Cancel(r.Context(), r.PathValue("payout_no"), s.Username, req.Reason, adminRequestID(r))
	h.writeManualResult(w, result, err, r)
}
func (h *AdminHandler) DownloadManualReceipt(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeSession(r, "payout.read"); !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("receipt_id"), 10, 64)
	if err != nil || id <= 0 {
		writeAdminError(w, http.StatusBadRequest, "invalid receipt", adminRequestID(r))
		return
	}
	receipt, file, err := h.manual.OpenReceipt(r.Context(), r.PathValue("payout_no"), id)
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "receipt not found", adminRequestID(r))
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", receipt.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="receipt-`+strconv.FormatInt(receipt.ID, 10)+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, receipt.OriginalFilename, receipt.CreatedAt, file)
}
func (h *AdminHandler) DownloadLatestManualReceipt(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeSession(r, "payout.read"); !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	receipt, file, err := h.manual.OpenLatestReceipt(r.Context(), r.PathValue("payout_no"))
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "receipt not found", adminRequestID(r))
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", receipt.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="receipt-`+strconv.FormatInt(receipt.ID, 10)+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, receipt.OriginalFilename, receipt.CreatedAt, file)
}
func (h *AdminHandler) CallbackAttempts(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeSession(r, "callback.read"); !ok {
		writeAdminError(w, http.StatusUnauthorized, "authentication required", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	items, err := h.manual.ListCallbackAttempts(r.Context(), r.PathValue("payout_no"))
	if err != nil {
		writeAdminError(w, http.StatusBadGateway, "unable to load callback attempts", adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, items, adminRequestID(r))
}
func (h *AdminHandler) RetryCallback(w http.ResponseWriter, r *http.Request) {
	_, ok := h.authorizeWrite(r, "callback.retry")
	if !ok {
		writeAdminError(w, http.StatusForbidden, "forbidden", adminRequestID(r))
		return
	}
	if h.manual == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "manual workflow unavailable", adminRequestID(r))
		return
	}
	if err := h.manual.RetryCallback(r.Context(), r.PathValue("payout_no")); err != nil {
		writeAdminError(w, http.StatusNotFound, "callback job not found or cannot be retried", adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{}, adminRequestID(r))
}
func (h *AdminHandler) writeManualResult(w http.ResponseWriter, result any, err error, r *http.Request) {
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "invalid manual payout transition") {
			status = http.StatusConflict
		}
		writeAdminError(w, status, err.Error(), adminRequestID(r))
		return
	}
	writeAdminJSON(w, http.StatusOK, result, adminRequestID(r))
}
func (h *AdminHandler) authorizeWrite(r *http.Request, permission string) (adminSession, bool) {
	s, ok := h.authorizeSession(r, permission)
	if !ok || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
		return adminSession{}, false
	}
	return s, true
}
func (h *AdminHandler) authorizeSession(r *http.Request, permission string) (adminSession, bool) {
	s, ok := h.session(r)
	if !ok || (!s.MFAVerified && permission != "") || (permission != "" && !adminPermissionAllowed(s.Role, permission)) {
		return adminSession{}, false
	}
	return s, true
}
func (h *AdminHandler) session(r *http.Request) (adminSession, bool) {
	if !h.configured() {
		return adminSession{}, false
	}
	c, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return adminSession{}, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return adminSession{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return adminSession{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return adminSession{}, false
	}
	mac := hmac.New(sha256.New, h.secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return adminSession{}, false
	}
	var s adminSession
	if json.Unmarshal(payload, &s) != nil || s.Username == "" || s.Expires < time.Now().Unix() {
		return adminSession{}, false
	}
	if h.users != nil {
		user, err := h.users.FindActive(r.Context(), s.Username)
		if err != nil {
			return adminSession{}, false
		}
		s.Role = strings.ToUpper(strings.TrimSpace(user.Role))
	}
	return s, true
}
func (h *AdminHandler) setSession(w http.ResponseWriter, s adminSession) {
	payload, _ := json.Marshal(s)
	mac := hmac.New(sha256.New, h.secret)
	_, _ = mac.Write(payload)
	value := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: value, Path: "/", Domain: h.cookieDomain, HttpOnly: true, Secure: h.secureCookie, SameSite: h.cookieSameSite, MaxAge: 8 * 60 * 60})
}
func (h *AdminHandler) auditAuth(r *http.Request, username, eventType string) {
	if h.users != nil {
		h.users.LogAuthEvent(r.Context(), username, eventType, r.RemoteAddr, adminRequestID(r))
	}
}
func decodeAdminJSON(r *http.Request, target any) error {
	if r.Body == nil {
		return errors.New("request body required")
	}
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(target)
}
func writeAdminJSON(w http.ResponseWriter, status int, data any, requestID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data, "error": nil, "request_id": requestID})
}
func writeAdminError(w http.ResponseWriter, status int, message, requestID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "data": nil, "error": map[string]string{"message": message}, "request_id": requestID})
}
func adminPermissionAllowed(role, permission string) bool {
	role = strings.ToUpper(strings.TrimSpace(role))
	permissions := map[string]map[string]bool{
		"ADMIN":    {"*": true},
		"OPERATOR": {"payout.read": true, "collection.read": true, "manual_payout.start": true, "manual_payout.receipt.upload": true, "manual_payout.cancel": true, "callback.read": true},
		"REVIEWER": {"payout.read": true, "collection.read": true, "manual_payout.confirm": true, "callback.read": true, "callback.retry": true},
		"AUDITOR":  {"payout.read": true, "collection.read": true, "callback.read": true},
	}
	return permissions[role]["*"] || permissions[role][permission]
}
func adminRequestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); id != "" {
		return id
	}
	return fmt.Sprintf("admin-%d", time.Now().UTC().UnixNano())
}
func randomAdminToken(secret []byte, now time.Time) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(now.UTC().String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func verifyAdminPassword(password, configured string) bool {
	parts := strings.Split(configured, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 210000 || iterations > 2000000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil || len(salt) < 16 {
		return false
	}
	expected, err := hex.DecodeString(parts[3])
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(pbkdf2SHA256([]byte(strings.ToUpper(password)), salt, iterations, len(expected)), expected) == 1
}
func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	result := make([]byte, 0, keyLength)
	for block := uint32(1); len(result) < keyLength; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		result = append(result, t...)
	}
	return result[:keyLength]
}
func NewAdminPasswordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const iterations = 210000
	key := pbkdf2SHA256([]byte(strings.ToUpper(password)), salt, iterations, sha256.Size)
	return "pbkdf2-sha256:" + strconv.Itoa(iterations) + ":" + hex.EncodeToString(salt) + ":" + hex.EncodeToString(key), nil
}
