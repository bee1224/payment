package http

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"payment-service/internal/config"
	"payment-service/internal/repository"
	"payment-service/internal/service"
)

func NewRouter(depositService *service.DepositService, payoutService *service.PayoutService, appCfg config.AppConfig, gatewayCfg config.GatewayConfig, nonceStores ...repository.ReplayNonceStore) http.Handler {
	return newRouter(depositService, payoutService, nil, nil, appCfg, gatewayCfg, nonceStores...)
}

// NewRouterWithManualPayouts keeps the public API router backwards compatible
// while allowing the application to attach the database-backed manual workflow.
func NewRouterWithManualPayouts(depositService *service.DepositService, payoutService *service.PayoutService, manualService *service.ManualPayoutService, appCfg config.AppConfig, gatewayCfg config.GatewayConfig, nonceStores ...repository.ReplayNonceStore) http.Handler {
	return newRouter(depositService, payoutService, manualService, nil, appCfg, gatewayCfg, nonceStores...)
}
func NewRouterWithOperations(depositService *service.DepositService, payoutService *service.PayoutService, manualService *service.ManualPayoutService, adminUsers *repository.AdminUserStore, appCfg config.AppConfig, gatewayCfg config.GatewayConfig, nonceStores ...repository.ReplayNonceStore) http.Handler {
	return newRouter(depositService, payoutService, manualService, adminUsers, appCfg, gatewayCfg, nonceStores...)
}

func newRouter(depositService *service.DepositService, payoutService *service.PayoutService, manualService *service.ManualPayoutService, adminUsers *repository.AdminUserStore, appCfg config.AppConfig, gatewayCfg config.GatewayConfig, nonceStores ...repository.ReplayNonceStore) http.Handler {
	var nonceStore repository.ReplayNonceStore = repository.NewInMemoryReplayNonceStore()
	if len(nonceStores) > 0 && nonceStores[0] != nil {
		nonceStore = nonceStores[0]
	}
	depositHandler := NewDepositHandler(depositService, GatewaySecurityConfig{
		HMACSecret:               gatewayCfg.HMACSecret,
		PreviousHMACSecret:       gatewayCfg.PreviousHMACSecret,
		MaxSkewSeconds:           gatewayCfg.MaxSkewSeconds,
		CustomerID:               gatewayCfg.CustomerID,
		HMACDiagnosticsEnabled:   strings.EqualFold(strings.TrimSpace(appCfg.Env), "sandbox") && appCfg.HMACDiagnosticsEnabled,
		DepositCallbackAllowlist: append([]string(nil), gatewayCfg.DepositCallbackAllowlist...),
		PayoutCallbackAllowlist:  append([]string(nil), gatewayCfg.PayoutCallbackAllowlist...),
	}, appCfg.PublicBaseURL, appCfg.TrustedProxyCIDRs, nonceStore)
	payoutService.SetReplayNonceStore(nonceStore)
	payoutHandler := NewPayoutHandler(gatewayCfg, payoutService, appCfg.PayoutReviewToken, appCfg.PayoutReviewActorAllowlist, appCfg.PayoutReviewActorRoles, appCfg.TrustedProxyCIDRs, nonceStore)
	adminHandler := NewAdminHandler(payoutService, appCfg)
	adminHandler.SetDepositService(depositService)
	adminHandler.SetManualPayoutService(manualService)
	adminHandler.SetAdminUserStore(adminUsers)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", HealthHandler)
	mux.Handle("GET /metrics", newMetricsHandler())
	mux.HandleFunc("POST /api/admin/auth/login", adminHandler.Login)
	mux.HandleFunc("POST /api/admin/auth/logout", adminHandler.Logout)
	mux.HandleFunc("GET /api/admin/auth/me", adminHandler.Me)
	mux.HandleFunc("POST /api/admin/auth/mfa/enroll", adminHandler.BeginMFAEnrollment)
	mux.HandleFunc("POST /api/admin/auth/mfa/confirm", adminHandler.ConfirmMFAEnrollment)
	mux.HandleFunc("GET /api/admin/payouts", adminHandler.Payouts)
	mux.HandleFunc("GET /api/admin/collections", adminHandler.Collections)
	mux.HandleFunc("POST /api/admin/collections/{order_no}/test-callback", adminHandler.SendTestDepositCallback)
	mux.HandleFunc("GET /api/admin/payouts/{payout_no}", adminHandler.PayoutDetail)
	mux.HandleFunc("POST /api/admin/payouts/{payout_no}/start-processing", adminHandler.StartManualPayout)
	mux.HandleFunc("POST /api/admin/payouts/{payout_no}/receipt", adminHandler.UploadManualReceipt)
	mux.HandleFunc("POST /api/admin/payouts/{payout_no}/confirm-success", adminHandler.ConfirmManualPayout)
	mux.HandleFunc("POST /api/admin/payouts/{payout_no}/mark-failed", adminHandler.FailManualPayout)
	mux.HandleFunc("POST /api/admin/payouts/{payout_no}/cancel", adminHandler.CancelManualPayout)
	mux.HandleFunc("GET /api/admin/payouts/{payout_no}/receipt", adminHandler.DownloadLatestManualReceipt)
	mux.HandleFunc("GET /api/admin/payouts/{payout_no}/callback-attempts", adminHandler.CallbackAttempts)
	mux.HandleFunc("POST /api/admin/payouts/{payout_no}/callback/retry", adminHandler.RetryCallback)
	mux.HandleFunc("GET /api/admin/payouts/{payout_no}/receipts/{receipt_id}", adminHandler.DownloadManualReceipt)

	// Canonical gateway-compatible collection APIs keep the existing provider contract field names.
	mux.HandleFunc("POST /api/pay_order", depositHandler.CreatePayOrder)
	mux.HandleFunc("POST /api/query_transaction", depositHandler.QueryTransaction)
	// This endpoint used to forward funds upstream without creating a local
	// payout, ledger hold, or review record. It is unsafe for production use.
	mux.HandleFunc("POST /api/payments/pay_order", retiredUnsafePayoutCompatibility)
	mux.HandleFunc("POST /api/payments/query_transaction", payoutHandler.QueryPayoutTransaction)
	mux.HandleFunc("POST /api/payments/balance", payoutHandler.QueryPayoutBalance)
	mux.HandleFunc("POST /api/payments/callback", payoutHandler.PayoutCallback)
	mux.HandleFunc("POST /api/payouts", payoutHandler.CreateWorkflowPayout)
	mux.HandleFunc("POST /api/payouts/query", payoutHandler.QueryWorkflowPayout)
	mux.HandleFunc("GET /api/payouts/{payout_no}", payoutHandler.GetWorkflowPayout)
	// High-risk operational endpoints formerly accepted a review token plus
	// caller-controlled headers. They are intentionally retired until each
	// operation is exposed through the MFA-backed admin RBAC surface.
	mux.HandleFunc("POST /api/payouts/{payout_no}/approve", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/payouts/{payout_no}/reject", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/payouts/{payout_no}/cancel", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/payouts/{payout_no}/resend-callback", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/payouts/{payout_no}/audit-logs", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/payouts/alerts", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/payouts/alerts/{alert_id}/resolve", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/reports/payout-settlement", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/reconciliation/run", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/reconciliation/reports", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/reconciliation/reports/{run_id}", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/reconciliation/trace", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/reconciliation/items/{item_id}/adjustment", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/reconciliation/items/{item_id}/reversal", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/merchants/{merchant_id}/api-keys", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/merchants/{merchant_id}/api-keys/audit-logs", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/merchants/{merchant_id}/api-keys/issue", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/merchants/{merchant_id}/api-keys/rotate", retiredReviewAuthorization)
	mux.HandleFunc("POST /api/merchants/{merchant_id}/api-keys/revoke", retiredReviewAuthorization)
	mux.HandleFunc("GET /api/v1/deposits/{order_no}", depositHandler.GetDeposit)
	mux.HandleFunc("GET /api/v1/deposits/{order_no}/redirect", depositHandler.RedirectDeposit)
	mux.HandleFunc("POST /api/v1/deposits/providers/{provider}/notifications", depositHandler.GenericDepositProviderNotify)
	mux.HandleFunc("GET /api/v1/deposits/payment-result", DepositPaymentResultHandler)
	mux.HandleFunc("POST /api/v1/deposits/payment-result", DepositPaymentResultHandler)

	// Compatibility aliases retained for clients that adopted the earlier internal naming.
	mux.HandleFunc("POST /deposits", deprecatedDepositRoute("/api/pay_order", depositHandler.CreateDeposit))
	mux.HandleFunc("POST /api/v1/deposits", deprecatedDepositRoute("/api/pay_order", depositHandler.CreatePayOrder))
	mux.HandleFunc("POST /api/v1/deposits/query", deprecatedDepositRoute("/api/query_transaction", depositHandler.QueryTransaction))
	mux.HandleFunc("GET /deposits/{order_no}", deprecatedDepositRouteForRequest(
		func(r *http.Request) string { return "/api/v1/deposits/" + r.PathValue("order_no") },
		depositHandler.GetDeposit,
	))
	mux.HandleFunc("GET /deposits/{order_no}/redirect", deprecatedDepositRouteForRequest(
		func(r *http.Request) string { return "/api/v1/deposits/" + r.PathValue("order_no") + "/redirect" },
		depositHandler.RedirectDeposit,
	))
	mux.HandleFunc("POST /notify/{provider}", deprecatedDepositRouteForRequest(
		func(r *http.Request) string {
			return "/api/v1/deposits/providers/" + r.PathValue("provider") + "/notifications"
		},
		depositHandler.GenericDepositProviderNotify,
	))
	mux.HandleFunc("POST /notify/newebpay", deprecatedDepositRoute(
		"/api/v1/deposits/providers/newebpay/notifications",
		depositHandler.NewebpayDepositNotify,
	))
	mux.HandleFunc("GET /payment/result", deprecatedDepositRoute("/api/v1/deposits/payment-result", DepositPaymentResultHandler))
	mux.HandleFunc("POST /payment/result", deprecatedDepositRoute("/api/v1/deposits/payment-result", DepositPaymentResultHandler))
	return adminCORS(appCfg.AdminAllowedOrigins, securityHeaders(logRequests(trackHTTPMetrics(mux))))
}

func retiredReviewAuthorization(w http.ResponseWriter, r *http.Request) {
	responseJSON := `{"success":false,"error":{"message":"review-token authorization has been retired; use the MFA-backed admin API"}}`
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	_, _ = w.Write([]byte(responseJSON))
}

func retiredUnsafePayoutCompatibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Link", "</api/payouts>; rel=\"successor-version\"")
	w.WriteHeader(http.StatusGone)
	_, _ = w.Write([]byte(`{"code":9000,"message":"unsafe compatibility payout creation has been retired; use POST /api/payouts"}`))
}

func adminCORS(origins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && strings.HasPrefix(r.URL.Path, "/api/admin/") {
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Request-ID")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func deprecatedDepositRoute(successor string, next http.HandlerFunc) http.HandlerFunc {
	return deprecatedDepositRouteForRequest(func(*http.Request) string { return successor }, next)
}

func deprecatedDepositRouteForRequest(successor func(*http.Request) string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Link", "<"+successor(r)+">; rel=\"successor-version\"")
		next(w, r)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("request: method=%s path=%s remote=%s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

type metricsRecorder struct {
	mu      sync.Mutex
	counts  map[string]int64
	latency map[string]time.Duration
}

func newMetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metricsRecorderInstance.writePrometheus(w)
	})
}

var metricsRecorderInstance = &metricsRecorder{
	counts:  make(map[string]int64),
	latency: make(map[string]time.Duration),
}

func trackHTTPMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusCapturingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		metricsRecorderInstance.observe(r.Method, routePattern(r), recorder.statusCode, time.Since(start))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (m *metricsRecorder) observe(method, route string, status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s|%s|%d", method, route, status)
	m.counts[key]++
	m.latency[key] += duration
}

func (m *metricsRecorder) writePrometheus(w http.ResponseWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	for key, count := range m.counts {
		parts := strings.Split(key, "|")
		if len(parts) != 3 {
			continue
		}
		method := parts[0]
		route := parts[1]
		status := parts[2]
		fmt.Fprintf(w, "http_requests_total{method=%q,route=%q,status=%q} %d\n", method, route, status, count)
		fmt.Fprintf(w, "http_request_duration_ms_total{method=%q,route=%q,status=%q} %d\n", method, route, status, m.latency[key].Milliseconds())
	}
}

func routePattern(r *http.Request) string {
	// http.Request.Pattern is unavailable in the Go 1.22 Docker toolchain.
	// Keep metrics compatible with the declared Go version; route registration
	// middleware can later provide a normalized template when needed.
	return r.URL.Path
}
