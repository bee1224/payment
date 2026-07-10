package http

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	nethttp "net/http"

	"payment-service/pkg/response"
)

func (h *DepositHandler) NewebpayDepositNotify(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.DepositProviderNotify(w, r, "newebpay")
}

func (h *DepositHandler) DepositProviderNotify(w nethttp.ResponseWriter, r *nethttp.Request, providerCode string) {
	fields, err := readNotifyFields(r)
	if err != nil {
		log.Printf("%s notify read failed: %v", providerCode, err)
		response.JSON(w, nethttp.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, err := h.depositService.HandleDepositProviderNotification(providerCode, fields)
	if err != nil {
		log.Printf("%s notify failed: fields=%v %s error=%v", providerCode, fieldNames(fields), notifyFingerprint(fields), err)
		response.JSON(w, nethttp.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	log.Printf("%s notify success: order_no=%s status=%s trade_no=%s %s", providerCode, result.Order.OrderNo, result.Order.Status, result.Order.ProviderTradeNo, notifyFingerprint(fields))
	if err := h.deliverRYDepositCallback(result.Order); err != nil {
		log.Printf("ry callback failed: order_no=%s error=%v", result.Order.OrderNo, err)
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
