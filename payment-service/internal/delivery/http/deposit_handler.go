package http

import (
	"encoding/json"
	"fmt"
	"log"
	nethttp "net/http"
	"net/url"
	"strings"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/repository"
	"payment-service/internal/service"

	"payment-service/pkg/response"
)

type DepositHandler struct {
	depositService *service.DepositService
	gateway        GatewaySecurityConfig
	publicBaseURL  string
	allowlist      *sourceIPAllowlist
	nonceStore     repository.ReplayNonceStore
	sourceIP       *sourceIPResolver
}

func NewDepositHandler(depositService *service.DepositService, gateway GatewaySecurityConfig, publicBaseURL string, trustedProxyCIDRs []string, nonceStore repository.ReplayNonceStore) *DepositHandler {
	allowlist, err := newSourceIPAllowlist(gateway.DepositCallbackAllowlist)
	if err != nil {
		panic(err)
	}
	sourceIP, err := newSourceIPResolver(trustedProxyCIDRs)
	if err != nil {
		panic(err)
	}
	handler := &DepositHandler{
		depositService: depositService,
		gateway:        gateway,
		publicBaseURL:  strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		allowlist:      allowlist,
		nonceStore:     nonceStore,
		sourceIP:       sourceIP,
	}
	depositService.SetMerchantDepositCallbackDispatcher(handler.buildGatewayDepositCallbackRequest, handler.postGatewayDepositCallback)
	return handler
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
	requestID := redirectRequestID(r)
	sourceIP := h.requestSourceIP(r)
	order, orderFound := h.depositService.FindDepositByOrderNo(orderNo)
	payment, paymentFound := h.depositService.DepositPaymentRequest(orderNo)
	if !paymentFound || payment.HTML == "" {
		log.Printf("deposit redirect: request_id=%s transaction_id=%s merchant_order_no=%s source_ip=%s user_agent=%q html_ready=false", requestID, orderNo, redirectMerchantOrderNo(order, orderFound), sourceIP, r.UserAgent())
		response.JSON(w, nethttp.StatusNotFound, map[string]string{"error": "payment form not found"})
		return
	}
	mpgHost := ""
	if parsed, err := url.Parse(payment.URL); err == nil {
		mpgHost = parsed.Hostname()
	}
	fieldsComplete := hasRequiredNewebpayPaymentFields(payment.Fields)
	log.Printf("deposit redirect: request_id=%s transaction_id=%s merchant_order_no=%s source_ip=%s user_agent=%q newebpay_environment=%s mpg_host=%s html_ready=true method=%s required_fields=%t", requestID, orderNo, redirectMerchantOrderNo(order, orderFound), sourceIP, r.UserAgent(), newebpayEnvironment(mpgHost), mpgHost, payment.Method, fieldsComplete)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write([]byte(payment.HTML))
}

func redirectRequestID(r *nethttp.Request) string {
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	return fmt.Sprintf("redirect-%d", time.Now().UTC().UnixNano())
}

func redirectMerchantOrderNo(order domain.DepositOrder, found bool) string {
	if found {
		return order.OrderNo
	}
	return ""
}

func hasRequiredNewebpayPaymentFields(fields map[string]string) bool {
	for _, key := range []string{"MerchantID", "TradeInfo", "TradeSha", "Version", "EncryptType"} {
		if strings.TrimSpace(fields[key]) == "" {
			return false
		}
	}
	return true
}

func newebpayEnvironment(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "ccore.newebpay.com" {
		return "test"
	}
	if host == "core.newebpay.com" {
		return "production"
	}
	return "unknown"
}

func (h *DepositHandler) requestSourceIP(r *nethttp.Request) string {
	if h.sourceIP == nil {
		return requestSourceIP(r, nil)
	}
	return h.sourceIP.Resolve(r)
}
