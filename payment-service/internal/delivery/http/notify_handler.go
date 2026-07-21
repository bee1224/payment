package http

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	nethttp "net/http"
	"strings"

	"payment-service/internal/domain"
	"payment-service/pkg/response"
)

const maxDepositNotifyBodyBytes int64 = 1 << 20

func (h *DepositHandler) NewebpayDepositNotify(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.DepositProviderNotify(w, r, "newebpay")
}

func (h *DepositHandler) DepositProviderNotify(w nethttp.ResponseWriter, r *nethttp.Request, providerCode string) {
	sourceIP := h.requestSourceIP(r)
	if !h.allowlist.Allows(sourceIP) {
		log.Printf("%s notify rejected: source_ip=%s reason=allowlist", providerCode, sourceIP)
		response.JSON(w, nethttp.StatusUnauthorized, map[string]string{"error": "source ip is not in deposit callback allowlist"})
		return
	}
	r.Body = nethttp.MaxBytesReader(w, r.Body, maxDepositNotifyBodyBytes)
	fields, err := readNotifyFields(r)
	if err != nil {
		log.Printf("%s notify read failed: source_ip=%s error=%v", providerCode, sourceIP, err)
		response.JSON(w, nethttp.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	trace := domain.DepositNotifyTrace{
		Headers:         snapshotHeaders(r.Header),
		SourceIP:        sourceIP,
		ProviderOrderNo: bestEffortNotifyOrderNo(fields),
		ProviderTradeNo: bestEffortNotifyTradeNo(fields),
	}

	result, err := h.depositService.HandleDepositProviderNotification(providerCode, fields, trace)
	if err != nil {
		log.Printf("%s notify failed: source_ip=%s provider_order_no=%s provider_trade_no=%s fields=%v %s error=%v", providerCode, trace.SourceIP, trace.ProviderOrderNo, trace.ProviderTradeNo, fieldNames(fields), notifyFingerprint(fields), err)
		response.JSON(w, nethttp.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	log.Printf("%s notify success: source_ip=%s order_no=%s status=%s trade_no=%s %s", providerCode, trace.SourceIP, result.Order.OrderNo, result.Order.Status, result.Order.ProviderTradeNo, notifyFingerprint(fields))
	if result.NotifyMerchant {
		if err := h.depositService.DispatchMerchantDepositCallback(r.Context(), result.Order); err != nil {
			log.Printf("gateway callback failed: order_no=%s error=%v", result.Order.OrderNo, err)
		}
	}

	response.JSON(w, nethttp.StatusOK, map[string]any{
		"status": "ok",
		"result": result,
	})
}

func (h *DepositHandler) GenericDepositProviderNotify(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.DepositProviderNotify(w, r, r.PathValue("provider"))
}

func notifyFingerprint(fields map[string]string) string {
	tradeInfo := fields["TradeInfo"]
	sum := sha256.Sum256([]byte(tradeInfo))
	return fmt.Sprintf("trade_info_len=%d trade_info_sha256=%x", len(tradeInfo), sum[:8])
}

func fieldNames(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

func snapshotHeaders(headers nethttp.Header) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func bestEffortNotifyOrderNo(fields map[string]string) string {
	return firstNonEmptyNotifyField(fields, "MerchantOrderNo", "merchant_order_no", "order_no", "OrderNo")
}

func bestEffortNotifyTradeNo(fields map[string]string) string {
	return firstNonEmptyNotifyField(fields, "TradeNo", "trade_no", "tradeNo")
}

func firstNonEmptyNotifyField(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func readNotifyFields(r *nethttp.Request) (map[string]string, error) {
	if err := r.ParseForm(); err == nil && len(r.PostForm) > 0 {
		fields := make(map[string]string, len(r.PostForm))
		for key := range r.PostForm {
			fields[key] = r.PostForm.Get(key)
		}
		return fields, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var fields map[string]string
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}
