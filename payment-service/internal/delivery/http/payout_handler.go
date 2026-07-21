package http

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/config"
	"payment-service/internal/domain"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
	"payment-service/internal/service"
	"payment-service/pkg/response"
)

type PayoutHandler struct {
	client           *providerGateway.PayoutClient
	service          *service.PayoutService
	reviewToken      string
	reviewActors     map[string]struct{}
	reviewActorRoles map[string][]string
	gateway          GatewaySecurityConfig
	allowlist        *sourceIPAllowlist
	nonceStore       repository.ReplayNonceStore
	sourceIP         *sourceIPResolver
}

type resolvePayoutOperationalAlertRequest struct {
	Reason string `json:"reason"`
}

type runReconciliationRequest struct {
	MerchantID string `json:"merchant_id"`
	OrderNo    string `json:"order_no"`
	PayoutNo   string `json:"payout_no"`
}

func NewPayoutHandler(cfg config.GatewayConfig, payoutService *service.PayoutService, reviewToken string, reviewActorAllowlist []string, reviewActorRoles map[string][]string, trustedProxyCIDRs []string, nonceStore repository.ReplayNonceStore) *PayoutHandler {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	allowlist, err := newSourceIPAllowlist(cfg.PayoutCallbackAllowlist)
	if err != nil {
		panic(err)
	}
	sourceIP, err := newSourceIPResolver(trustedProxyCIDRs)
	if err != nil {
		panic(err)
	}
	return &PayoutHandler{
		client: providerGateway.NewPayoutClient(
			cfg.BaseURL,
			cfg.CustomerID,
			cfg.HMACSecret,
			cfg.PayoutNotifyURL,
			timeout,
		),
		service:          payoutService,
		reviewToken:      strings.TrimSpace(reviewToken),
		reviewActors:     normalizeReviewActorAllowlist(reviewActorAllowlist),
		reviewActorRoles: cloneReviewActorRoles(reviewActorRoles),
		gateway: GatewaySecurityConfig{
			HMACSecret:              cfg.HMACSecret,
			PreviousHMACSecret:      cfg.PreviousHMACSecret,
			MaxSkewSeconds:          cfg.MaxSkewSeconds,
			CustomerID:              cfg.CustomerID,
			PayoutCallbackAllowlist: append([]string(nil), cfg.PayoutCallbackAllowlist...),
		},
		allowlist:  allowlist,
		nonceStore: nonceStore,
		sourceIP:   sourceIP,
	}
}

func (h *PayoutHandler) CreatePayoutOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req providerGateway.CreatePayoutRequest
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := ensureConsistentGatewayCustomerID("pay_customer_id", strings.TrimSpace(r.Header.Get("X-Customer-Id")), req.PayCustomerID); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := authenticateGatewayRequest(r, buildGatewayRequestAuth(r, req.PayCustomerID, body), h.gateway, h.nonceStore, time.Now()); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	result, err := h.client.CreatePayout(r.Context(), req)
	if err != nil {
		writePayoutUpstreamError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, result)
}

func (h *PayoutHandler) QueryPayoutTransaction(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req providerGateway.QueryPayoutRequest
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := ensureConsistentGatewayCustomerID("pay_customer_id", strings.TrimSpace(r.Header.Get("X-Customer-Id")), req.PayCustomerID); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := authenticateGatewayRequest(r, buildGatewayRequestAuth(r, req.PayCustomerID, body), h.gateway, h.nonceStore, time.Now()); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	result, err := h.client.QueryPayout(r.Context(), req)
	if err != nil {
		writePayoutUpstreamError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, result)
}

func (h *PayoutHandler) QueryPayoutBalance(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req providerGateway.BalanceRequest
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := ensureConsistentGatewayCustomerID("pay_customer_id", strings.TrimSpace(r.Header.Get("X-Customer-Id")), req.PayCustomerID); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := authenticateGatewayRequest(r, buildGatewayRequestAuth(r, req.PayCustomerID, body), h.gateway, h.nonceStore, time.Now()); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	result, err := h.client.QueryBalance(r.Context(), req)
	if err != nil {
		writePayoutUpstreamError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, result)
}

func (h *PayoutHandler) CreateWorkflowPayout(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.CreatePayoutOrderRequest
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auth := buildMerchantRequestAuth(r, req.MerchantID, req.APIKey, body)
	if err := ensureConsistentMerchantID(auth.MerchantID, req.MerchantID); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	merchant, err := h.service.AuthenticateMerchantRequest(r.Context(), auth)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	order, err := h.service.CreatePayoutOrderForMerchant(r.Context(), merchant, req)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) QueryWorkflowPayout(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.QueryPayoutOrderRequest
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auth := buildMerchantRequestAuth(r, req.MerchantID, req.APIKey, body)
	if err := ensureConsistentMerchantID(auth.MerchantID, req.MerchantID); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	merchant, err := h.service.AuthenticateMerchantRequest(r.Context(), auth)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	order, err := h.service.GetPayoutOrderForMerchant(r.Context(), merchant, req)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) GetWorkflowPayout(w nethttp.ResponseWriter, r *nethttp.Request) {
	req := service.QueryPayoutOrderRequest{
		MerchantID: strings.TrimSpace(firstNonEmpty([]string{strings.TrimSpace(r.Header.Get("X-Merchant-Id")), r.URL.Query().Get("merchant_id")})),
		APIKey:     strings.TrimSpace(r.URL.Query().Get("api_key")),
		PayoutNo:   r.PathValue("payout_no"),
	}
	auth := buildMerchantRequestAuth(r, req.MerchantID, req.APIKey, nil)
	if err := ensureConsistentMerchantID(strings.TrimSpace(r.Header.Get("X-Merchant-Id")), strings.TrimSpace(r.URL.Query().Get("merchant_id"))); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	merchant, err := h.service.AuthenticateMerchantRequest(r.Context(), auth)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	order, err := h.service.GetPayoutOrderForMerchant(r.Context(), merchant, req)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) ApproveWorkflowPayout(w nethttp.ResponseWriter, r *nethttp.Request) {
	auditCtx, err := h.authorizePayoutReview(r, "approve", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	auditCtx.Reason = h.reviewReasonFromRequest(r)
	if strings.TrimSpace(auditCtx.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	order, err := h.service.ApprovePayoutOrder(r.Context(), r.PathValue("payout_no"), auditCtx)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) RejectWorkflowPayout(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.RejectPayoutOrderRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) && !errors.Is(err, io.EOF) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "reject", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	auditCtx.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(auditCtx.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	order, err := h.service.RejectPayoutOrder(r.Context(), r.PathValue("payout_no"), auditCtx.Reason, auditCtx)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) CancelWorkflowPayout(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.CancelPayoutOrderRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) && !errors.Is(err, io.EOF) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "cancel", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	auditCtx.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(auditCtx.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	order, err := h.service.CancelPayoutOrder(r.Context(), r.PathValue("payout_no"), auditCtx.Reason, auditCtx)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) ResendWorkflowPayoutCallback(w nethttp.ResponseWriter, r *nethttp.Request) {
	auditCtx, err := h.authorizePayoutReview(r, "resend_callback", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	auditCtx.Reason = h.reviewReasonFromRequest(r)
	if strings.TrimSpace(auditCtx.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	order, err := h.service.ResendMerchantCallback(r.Context(), r.PathValue("payout_no"), auditCtx)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    service.BuildPayoutOrderView(order),
	})
}

func (h *PayoutHandler) ListMerchantAPIKeys(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "merchant_api_key_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	keys, err := h.service.ListMerchantAPIKeys(r.Context(), r.PathValue("merchant_id"))
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    keys,
	})
}

func (h *PayoutHandler) ListMerchantAPIKeyAuditLogs(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "merchant_api_key_audit_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writePayoutError(w, nethttp.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	logs, err := h.service.ListMerchantAPIKeyAuditLogs(r.Context(), r.PathValue("merchant_id"), limit)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    logs,
	})
}

func (h *PayoutHandler) ListWorkflowPayoutAuditLogs(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "payout_audit_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writePayoutError(w, nethttp.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	logs, err := h.service.ListPayoutReviewAuditLogs(r.Context(), r.PathValue("payout_no"), limit)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    logs,
	})
}

func (h *PayoutHandler) ListPayoutOperationalAlerts(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "payout_alert_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writePayoutError(w, nethttp.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	alerts, err := h.service.ListPayoutOperationalAlerts(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), limit)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    alerts,
	})
}

func (h *PayoutHandler) ResolvePayoutOperationalAlert(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req resolvePayoutOperationalAlertRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) && !errors.Is(err, io.EOF) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "payout_alert_resolve", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	auditCtx.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(auditCtx.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	alertID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("alert_id")), 10, 64)
	if err != nil || alertID <= 0 {
		writePayoutError(w, nethttp.StatusBadRequest, "alert_id must be a positive integer")
		return
	}
	if err := h.service.ResolvePayoutOperationalAlert(r.Context(), alertID, auditCtx); err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
	})
}

func (h *PayoutHandler) GetPayoutSettlementReport(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "settlement_report_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	report, err := h.service.BuildPayoutSettlementReport(r.Context())
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    report,
	})
}

func (h *PayoutHandler) RunReconciliation(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req runReconciliationRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) && !errors.Is(err, io.EOF) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.authorizePayoutReview(r, "reconciliation_run", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	report, err := h.service.RunReconciliation(r.Context(), service.RunReconciliationRequest{
		MerchantID: strings.TrimSpace(req.MerchantID),
		OrderNo:    strings.TrimSpace(req.OrderNo),
		PayoutNo:   strings.TrimSpace(req.PayoutNo),
	})
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    report,
	})
}

func (h *PayoutHandler) GetReconciliationReport(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "reconciliation_report_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	runID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("run_id")), 10, 64)
	if err != nil || runID <= 0 {
		writePayoutError(w, nethttp.StatusBadRequest, "run_id must be a positive integer")
		return
	}
	report, err := h.service.GetReconciliationReport(r.Context(), runID)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    report,
	})
}

func (h *PayoutHandler) ListReconciliationReports(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "reconciliation_report_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	dateFrom, err := reconciliationQueryDate(r.URL.Query().Get("date_from"), false)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	dateTo, err := reconciliationQueryDate(r.URL.Query().Get("date_to"), true)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	reports, err := h.service.ListReconciliationReports(r.Context(), service.ListReconciliationReportsRequest{
		DateFrom: dateFrom, DateTo: dateTo, MerchantID: strings.TrimSpace(r.URL.Query().Get("merchant_id")),
		OrderType:    domain.ReconciliationOrderType(strings.TrimSpace(r.URL.Query().Get("order_type"))),
		MismatchType: domain.ReconciliationMismatchType(strings.TrimSpace(r.URL.Query().Get("mismatch_type"))),
	})
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{"code": 0, "message": "success", "data": reports})
}

func (h *PayoutHandler) GetReconciliationTrace(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, err := h.authorizePayoutReview(r, "reconciliation_report_read", false); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	trace, err := h.service.GetReconciliationTrace(r.Context(), domain.ReconciliationTraceQuery{
		MerchantOrderNo: r.URL.Query().Get("merchant_order_no"), PayoutNo: r.URL.Query().Get("payout_no"),
		ProviderTradeNo: r.URL.Query().Get("provider_trade_no"), LedgerEntryNo: r.URL.Query().Get("ledger_entry_no"),
	})
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{"code": 0, "message": "success", "data": trace})
}

func reconciliationQueryDate(value string, inclusiveEnd bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, errors.New("date filters must use YYYY-MM-DD")
	}
	if inclusiveEnd {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return &parsed, nil
}

func (h *PayoutHandler) ResolveReconciliationMismatchWithAdjustment(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.ResolveReconciliationAdjustmentRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "reconciliation_adjustment", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	req.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(req.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	itemID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("item_id")), 10, 64)
	if err != nil || itemID <= 0 {
		writePayoutError(w, nethttp.StatusBadRequest, "item_id must be a positive integer")
		return
	}
	item, err := h.service.ResolveReconciliationMismatchWithAdjustment(r.Context(), itemID, req, auditCtx)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    item,
	})
}

func (h *PayoutHandler) ResolveReconciliationMismatchWithReversal(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.ResolveReconciliationReversalRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "reconciliation_reversal", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	req.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(req.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	itemID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("item_id")), 10, 64)
	if err != nil || itemID <= 0 {
		writePayoutError(w, nethttp.StatusBadRequest, "item_id must be a positive integer")
		return
	}
	item, err := h.service.ResolveReconciliationMismatchWithReversal(r.Context(), itemID, req, auditCtx)
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    item,
	})
}

func (h *PayoutHandler) RotateMerchantAPIKey(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.RotateMerchantAPIKeyRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "merchant_api_key_rotate", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	req.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(req.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	keys, err := h.service.RotateMerchantAPIKey(r.Context(), r.PathValue("merchant_id"), req, h.buildMerchantAPIKeyAuditContext(auditCtx))
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    keys,
	})
}

func (h *PayoutHandler) IssueMerchantAPIKey(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.IssueMerchantAPIKeyRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "merchant_api_key_issue", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	req.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(req.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	issued, err := h.service.IssueMerchantAPIKey(r.Context(), r.PathValue("merchant_id"), req, h.buildMerchantAPIKeyAuditContext(auditCtx))
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    issued,
	})
}

func (h *PayoutHandler) RevokeMerchantAPIKey(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.RevokeMerchantAPIKeyRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	auditCtx, err := h.authorizePayoutReview(r, "merchant_api_key_revoke", true)
	if err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	req.Reason = firstNonEmpty([]string{strings.TrimSpace(req.Reason), h.reviewReasonFromRequest(r)})
	if strings.TrimSpace(req.Reason) == "" {
		writePayoutError(w, nethttp.StatusBadRequest, "review reason is required")
		return
	}
	keys, err := h.service.RevokeMerchantAPIKey(r.Context(), r.PathValue("merchant_id"), req, h.buildMerchantAPIKeyAuditContext(auditCtx))
	if err != nil {
		writeWorkflowPayoutError(w, err)
		return
	}
	response.JSON(w, nethttp.StatusOK, map[string]any{
		"code":    0,
		"message": "success",
		"data":    keys,
	})
}

func (h *PayoutHandler) PayoutCallback(w nethttp.ResponseWriter, r *nethttp.Request) {
	sourceIP := h.requestSourceIP(r)
	if !h.allowlist.Allows(sourceIP) {
		writePayoutError(w, nethttp.StatusUnauthorized, "source ip is not in payout callback allowlist")
		return
	}
	var req providerGateway.PayoutCallbackRequest
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	customerID := strings.TrimSpace(firstNonEmpty([]string{strings.TrimSpace(r.Header.Get("X-Customer-Id")), strings.TrimSpace(stringifyCustomerID(req.CustomerID))}))
	if err := ensureConsistentGatewayCustomerID("customer_id", strings.TrimSpace(r.Header.Get("X-Customer-Id")), stringifyCustomerID(req.CustomerID)); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := authenticateGatewayRequest(r, buildGatewayRequestAuth(r, customerID, body), h.gateway, h.nonceStore, time.Now()); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	if req.TransactionCode != "30000" && req.TransactionCode != "40000" {
		writePayoutError(w, nethttp.StatusBadRequest, "unsupported transaction_code")
		return
	}
	if _, _, err := h.service.HandleGatewayCallback(r.Context(), req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func decodePayoutJSON(r *nethttp.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func decodePayoutJSONBytes(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func readPayoutRequestBody(r *nethttp.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func writePayoutError(w nethttp.ResponseWriter, status int, message string) {
	response.JSON(w, status, map[string]any{
		"code":    9000,
		"message": message,
	})
}

func writePayoutUpstreamError(w nethttp.ResponseWriter, err error) {
	message := "gateway payout request failed; confirm the order in the upstream back office or query API before retrying"
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") ||
		strings.Contains(err.Error(), "does not match") || strings.Contains(err.Error(), "not configured") {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	writePayoutError(w, nethttp.StatusBadGateway, message)
}

func writeWorkflowPayoutError(w nethttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMerchantAuthFailed):
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrReplayRequest):
		writePayoutError(w, nethttp.StatusConflict, err.Error())
	case strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "invalid"),
		strings.Contains(err.Error(), "must be"),
		strings.Contains(err.Error(), "RFC3339"),
		strings.Contains(err.Error(), "timestamp"),
		strings.Contains(err.Error(), "nonce"),
		strings.Contains(err.Error(), "whitelist"),
		strings.Contains(err.Error(), "callback_url"),
		strings.Contains(err.Error(), "insufficient"),
		strings.Contains(err.Error(), "cannot be"),
		strings.Contains(err.Error(), "cannot be cancelled"):
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
	case strings.Contains(err.Error(), "not found"):
		writePayoutError(w, nethttp.StatusNotFound, err.Error())
	default:
		writePayoutError(w, nethttp.StatusBadGateway, err.Error())
	}
}

func (h *PayoutHandler) authorizePayoutReview(r *nethttp.Request, action string, requireActionContext bool) (service.PayoutReviewAuditContext, error) {
	if h.reviewToken == "" {
		return service.PayoutReviewAuditContext{}, errors.New("payout review token is not configured")
	}
	provided := strings.TrimSpace(r.Header.Get("X-Payout-Review-Token"))
	if provided == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			provided = strings.TrimSpace(auth[7:])
		}
	}
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.reviewToken)) != 1 {
		return service.PayoutReviewAuditContext{}, errors.New("payout review authorization failed")
	}
	actor := strings.TrimSpace(firstNonEmpty([]string{r.Header.Get("X-Operator-Id"), r.Header.Get("X-Actor")}))
	actorRoles := h.resolveReviewRoles(actor, strings.TrimSpace(r.Header.Get("X-Operator-Role")))
	auditCtx := service.PayoutReviewAuditContext{
		Actor:      actor,
		ActorRoles: actorRoles,
		RequestID:  strings.TrimSpace(firstNonEmpty([]string{r.Header.Get("X-Request-ID"), r.Header.Get("X-Request-Id")})),
		SourceIP:   h.requestSourceIP(r),
		UserAgent:  strings.TrimSpace(r.UserAgent()),
	}
	if !requireActionContext {
		if !h.isReviewActionAllowed(action, auditCtx.ActorRoles, false) {
			return service.PayoutReviewAuditContext{}, errors.New("operator role is not authorized for this payout review action")
		}
		return auditCtx, nil
	}
	if auditCtx.Actor == "" {
		return service.PayoutReviewAuditContext{}, errors.New("X-Operator-Id is required for payout review actions")
	}
	if len(h.reviewActors) > 0 {
		if _, ok := h.reviewActors[auditCtx.Actor]; !ok {
			return service.PayoutReviewAuditContext{}, errors.New("operator is not in payout review allowlist")
		}
	}
	if auditCtx.RequestID == "" {
		return service.PayoutReviewAuditContext{}, errors.New("X-Request-ID is required for payout review actions")
	}
	if !h.isReviewActionAllowed(action, auditCtx.ActorRoles, false) {
		return service.PayoutReviewAuditContext{}, errors.New("operator role is not authorized for this payout review action")
	}
	checker := strings.TrimSpace(r.Header.Get("X-Checker-Id"))
	checkerRoles := h.resolveReviewRoles(checker, strings.TrimSpace(r.Header.Get("X-Checker-Role")))
	if checker == "" {
		return service.PayoutReviewAuditContext{}, errors.New("X-Checker-Id is required for payout review actions")
	}
	if checker == auditCtx.Actor {
		return service.PayoutReviewAuditContext{}, errors.New("checker must be different from operator")
	}
	if len(h.reviewActors) > 0 {
		if _, ok := h.reviewActors[checker]; !ok {
			return service.PayoutReviewAuditContext{}, errors.New("checker is not in payout review allowlist")
		}
	}
	if !h.isReviewActionAllowed(action, checkerRoles, true) {
		return service.PayoutReviewAuditContext{}, errors.New("checker role is not authorized for this payout review action")
	}
	auditCtx.Checker = checker
	auditCtx.CheckerRoles = checkerRoles
	return auditCtx, nil
}

func (h *PayoutHandler) buildMerchantAPIKeyAuditContext(auditCtx service.PayoutReviewAuditContext) service.MerchantAPIKeyAuditContext {
	return service.MerchantAPIKeyAuditContext{
		Actor:        auditCtx.Actor,
		ActorRoles:   append([]string(nil), auditCtx.ActorRoles...),
		Checker:      auditCtx.Checker,
		CheckerRoles: append([]string(nil), auditCtx.CheckerRoles...),
		RequestID:    auditCtx.RequestID,
		SourceIP:     auditCtx.SourceIP,
		UserAgent:    auditCtx.UserAgent,
	}
}

func (h *PayoutHandler) reviewReasonFromRequest(r *nethttp.Request) string {
	return strings.TrimSpace(firstNonEmpty([]string{r.Header.Get("X-Review-Reason"), r.Header.Get("X-Reason")}))
}

func normalizeReviewActorAllowlist(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	normalized := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		normalized[value] = struct{}{}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func cloneReviewActorRoles(values map[string][]string) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(values))
	for actor, roles := range values {
		actor = strings.TrimSpace(actor)
		if actor == "" {
			continue
		}
		cloned[actor] = append([]string(nil), roles...)
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func (h *PayoutHandler) resolveReviewRoles(actor, headerRole string) []string {
	actor = strings.TrimSpace(actor)
	if actor != "" && len(h.reviewActorRoles) > 0 {
		if roles, ok := h.reviewActorRoles[actor]; ok && len(roles) > 0 {
			return append([]string(nil), roles...)
		}
	}
	headerRole = strings.TrimSpace(headerRole)
	if headerRole == "" {
		return nil
	}
	return []string{headerRole}
}

func (h *PayoutHandler) isReviewActionAllowed(action string, roles []string, checker bool) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return true
	}
	if len(h.reviewActorRoles) == 0 && len(roles) == 0 {
		return true
	}
	allowed := map[string]map[string]struct{}{
		"approve":                     {"checker": {}, "approver": {}, "admin": {}},
		"reject":                      {"checker": {}, "approver": {}, "admin": {}},
		"cancel":                      {"checker": {}, "approver": {}, "admin": {}},
		"resend_callback":             {"checker": {}, "approver": {}, "admin": {}, "support": {}},
		"payout_alert_resolve":        {"checker": {}, "approver": {}, "admin": {}, "support": {}},
		"merchant_api_key_issue":      {"checker": {}, "approver": {}, "admin": {}},
		"merchant_api_key_rotate":     {"checker": {}, "approver": {}, "admin": {}},
		"merchant_api_key_revoke":     {"checker": {}, "approver": {}, "admin": {}},
		"merchant_api_key_read":       {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "support": {}},
		"merchant_api_key_audit_read": {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "support": {}},
		"payout_audit_read":           {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "support": {}},
		"payout_alert_read":           {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "support": {}},
		"settlement_report_read":      {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "finance": {}},
		"reconciliation_run":          {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "finance": {}, "support": {}},
		"reconciliation_report_read":  {"auditor": {}, "checker": {}, "approver": {}, "admin": {}, "finance": {}, "support": {}},
		"reconciliation_adjustment":   {"checker": {}, "approver": {}, "admin": {}, "finance": {}},
		"reconciliation_reversal":     {"checker": {}, "approver": {}, "admin": {}, "finance": {}},
	}
	if !checker {
		switch action {
		case "approve", "reject", "cancel", "resend_callback", "payout_alert_resolve", "merchant_api_key_issue", "merchant_api_key_rotate", "merchant_api_key_revoke", "reconciliation_adjustment", "reconciliation_reversal":
			allowed[action]["maker"] = struct{}{}
			allowed[action]["operator"] = struct{}{}
		}
	}
	roleSet, ok := allowed[action]
	if !ok {
		return true
	}
	for _, role := range roles {
		if _, ok := roleSet[strings.TrimSpace(role)]; ok {
			return true
		}
	}
	return false
}

func (h *PayoutHandler) requestSourceIP(r *nethttp.Request) string {
	if h.sourceIP == nil {
		return requestSourceIP(r, nil)
	}
	return h.sourceIP.Resolve(r)
}

func buildMerchantRequestAuth(r *nethttp.Request, merchantID, apiKey string, body []byte) service.MerchantRequestAuth {
	return service.MerchantRequestAuth{
		MerchantID: firstNonEmpty([]string{strings.TrimSpace(r.Header.Get("X-Merchant-Id")), strings.TrimSpace(merchantID)}),
		APIKey:     strings.TrimSpace(apiKey),
		Timestamp:  strings.TrimSpace(r.Header.Get("X-Timestamp")),
		Nonce:      strings.TrimSpace(r.Header.Get("X-Nonce")),
		Signature:  strings.TrimSpace(r.Header.Get("X-Signature")),
		Method:     r.Method,
		Path:       r.URL.Path,
		Body:       append([]byte(nil), body...),
	}
}

func ensureConsistentMerchantID(primary, secondary string) error {
	primary = strings.TrimSpace(primary)
	secondary = strings.TrimSpace(secondary)
	if primary != "" && secondary != "" && primary != secondary {
		return errors.New("merchant_id does not match X-Merchant-Id")
	}
	return nil
}

func stringifyCustomerID(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}
