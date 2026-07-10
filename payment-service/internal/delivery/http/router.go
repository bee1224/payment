package http

import (
	"log"
	"net/http"

	"payment-service/internal/config"
	"payment-service/internal/service"
)

func NewRouter(depositService *service.DepositService, payoutService *service.PayoutService, appCfg config.AppConfig, ry config.RYConfig) http.Handler {
	depositHandler := NewDepositHandler(depositService, RYSecurityConfig{
		SignKey:        ry.SignKey,
		MaxSkewSeconds: ry.MaxSkewSeconds,
	})
	payoutHandler := NewPayoutHandler(ry, payoutService, appCfg.PayoutReviewToken)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", HealthHandler)

	// Canonical RY collection APIs use the names defined by the provider contract.
	mux.HandleFunc("POST /api/pay_order", depositHandler.CreatePayOrder)
	mux.HandleFunc("POST /api/query_transaction", depositHandler.QueryTransaction)
	mux.HandleFunc("POST /api/payments/pay_order", payoutHandler.CreatePayoutOrder)
	mux.HandleFunc("POST /api/payments/query_transaction", payoutHandler.QueryPayoutTransaction)
	mux.HandleFunc("POST /api/payments/balance", payoutHandler.QueryPayoutBalance)
	mux.HandleFunc("POST /api/payments/callback", payoutHandler.PayoutCallback)
	mux.HandleFunc("POST /api/payouts", payoutHandler.CreateWorkflowPayout)
	mux.HandleFunc("POST /api/payouts/query", payoutHandler.QueryWorkflowPayout)
	mux.HandleFunc("GET /api/payouts/{payout_no}", payoutHandler.GetWorkflowPayout)
	mux.HandleFunc("POST /api/payouts/{payout_no}/approve", payoutHandler.ApproveWorkflowPayout)
	mux.HandleFunc("POST /api/payouts/{payout_no}/reject", payoutHandler.RejectWorkflowPayout)
	mux.HandleFunc("POST /api/payouts/{payout_no}/cancel", payoutHandler.CancelWorkflowPayout)
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
	return logRequests(mux)
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
