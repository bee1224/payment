package http

import (
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"payment-service/internal/config"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/service"
	"payment-service/pkg/response"
)

type PayoutHandler struct {
	client      *providerGateway.PayoutClient
	service     *service.PayoutService
	reviewToken string
}

func NewPayoutHandler(cfg config.GatewayConfig, payoutService *service.PayoutService, reviewToken string) *PayoutHandler {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	return &PayoutHandler{
		client: providerGateway.NewPayoutClient(
			cfg.BaseURL,
			cfg.CustomerID,
			cfg.SignKey,
			cfg.PayoutNotifyURL,
			timeout,
		),
		service:     payoutService,
		reviewToken: strings.TrimSpace(reviewToken),
	}
}

func (h *PayoutHandler) CreatePayoutOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req providerGateway.CreatePayoutRequest
	if err := decodePayoutJSON(r, &req); err != nil {
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
	if err := decodePayoutJSON(r, &req); err != nil {
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
	if err := decodePayoutJSON(r, &req); err != nil {
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
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	order, err := h.service.CreatePayoutOrder(r.Context(), req)
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
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	order, err := h.service.GetPayoutOrder(r.Context(), req)
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
	order, err := h.service.GetPayoutOrder(r.Context(), service.QueryPayoutOrderRequest{
		MerchantID: strings.TrimSpace(r.URL.Query().Get("merchant_id")),
		APIKey:     strings.TrimSpace(r.URL.Query().Get("api_key")),
		PayoutNo:   r.PathValue("payout_no"),
	})
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
	if err := h.authorizePayoutReview(r); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	order, err := h.service.ApprovePayoutOrder(r.Context(), r.PathValue("payout_no"))
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
	if err := h.authorizePayoutReview(r); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	var req service.RejectPayoutOrderRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	order, err := h.service.RejectPayoutOrder(r.Context(), r.PathValue("payout_no"), req.Reason)
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
	if err := h.authorizePayoutReview(r); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	var req service.CancelPayoutOrderRequest
	if err := decodePayoutJSON(r, &req); err != nil && !errors.Is(err, nethttp.ErrBodyNotAllowed) {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	order, err := h.service.CancelPayoutOrder(r.Context(), r.PathValue("payout_no"), req.Reason)
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
	if err := h.authorizePayoutReview(r); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	order, err := h.service.ResendMerchantCallback(r.Context(), r.PathValue("payout_no"))
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
	if err := h.authorizePayoutReview(r); err != nil {
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

func (h *PayoutHandler) RotateMerchantAPIKey(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.authorizePayoutReview(r); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	var req service.RotateMerchantAPIKeyRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	keys, err := h.service.RotateMerchantAPIKey(r.Context(), r.PathValue("merchant_id"), req)
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

func (h *PayoutHandler) RevokeMerchantAPIKey(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.authorizePayoutReview(r); err != nil {
		writePayoutError(w, nethttp.StatusUnauthorized, err.Error())
		return
	}
	var req service.RevokeMerchantAPIKeyRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	keys, err := h.service.RevokeMerchantAPIKey(r.Context(), r.PathValue("merchant_id"), req)
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
	var req providerGateway.PayoutCallbackRequest
	if err := decodePayoutJSON(r, &req); err != nil {
		writePayoutError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := h.client.VerifyCallback(req); err != nil {
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
	case strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "invalid"),
		strings.Contains(err.Error(), "must be"),
		strings.Contains(err.Error(), "RFC3339"),
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

func (h *PayoutHandler) authorizePayoutReview(r *nethttp.Request) error {
	if h.reviewToken == "" {
		return errors.New("payout review token is not configured")
	}
	provided := strings.TrimSpace(r.Header.Get("X-Payout-Review-Token"))
	if provided == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			provided = strings.TrimSpace(auth[7:])
		}
	}
	if provided == "" || provided != h.reviewToken {
		return errors.New("payout review authorization failed")
	}
	return nil
}
