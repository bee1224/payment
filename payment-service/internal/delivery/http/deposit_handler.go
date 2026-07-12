package http

import (
	"encoding/json"
	nethttp "net/http"

	"payment-service/internal/service"

	"payment-service/pkg/response"
)

type DepositHandler struct {
	depositService *service.DepositService
	gateway        GatewaySecurityConfig
}

func NewDepositHandler(depositService *service.DepositService, gateway GatewaySecurityConfig) *DepositHandler {
	return &DepositHandler{depositService: depositService, gateway: gateway}
}

func (h *DepositHandler) CreateDeposit(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req service.CreateDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, nethttp.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := h.depositService.CreateDeposit(req)
	if err != nil {
		response.JSON(w, nethttp.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	response.JSON(w, nethttp.StatusCreated, result)
}

func (h *DepositHandler) GetDeposit(w nethttp.ResponseWriter, r *nethttp.Request) {
	orderNo := r.PathValue("order_no")
	order, ok := h.depositService.FindDepositByOrderNo(orderNo)
	if !ok {
		response.JSON(w, nethttp.StatusNotFound, map[string]string{"error": "order not found"})
		return
	}

	response.JSON(w, nethttp.StatusOK, map[string]any{
		"order":        order,
		"channel_code": order.ChannelCode,
	})
}

func (h *DepositHandler) RedirectDeposit(w nethttp.ResponseWriter, r *nethttp.Request) {
	orderNo := r.PathValue("order_no")
	html, ok := h.depositService.DepositPaymentHTML(orderNo)
	if !ok {
		response.JSON(w, nethttp.StatusNotFound, map[string]string{"error": "payment form not found"})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write([]byte(html))
}
