package http

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/service"
	"payment-service/pkg/response"
)

type PayOrderRequest struct {
	PayCustomerID  string   `json:"pay_customer_id"`
	PayApplyDate   string   `json:"pay_apply_date"`
	PayOrderID     string   `json:"pay_order_id"`
	PayAmount      any      `json:"pay_amount"`
	PayChannelID   string   `json:"pay_channel_id"`
	BankAccount    []string `json:"bank_account"`
	StoreNumber    []string `json:"store_number"`
	PayNotifyURL   string   `json:"pay_notify_url"`
	PayProductName string   `json:"pay_product_name"`
	UserName       string   `json:"user_name"`
	BankID         string   `json:"bank_id"`
	PayCurrency    string   `json:"pay_currency"`
	Mobile         string   `json:"mobile"`
	IDNo           string   `json:"id_no"`
	PayMD5Sign     string   `json:"pay_md5_sign"`
}

type PayOrderResponse struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    *PayOrderData  `json:"data,omitempty"`
	Error   *PayOrderError `json:"error,omitempty"`
}

type PayOrderData struct {
	OrderID       string `json:"order_id"`
	TransactionID string `json:"transaction_id"`
	ViewURL       string `json:"view_url"`
	QRURL         string `json:"qr_url,omitempty"`
	Expired       string `json:"expired,omitempty"`
	UserName      string `json:"user_name,omitempty"`
	BillPrice     string `json:"bill_price,omitempty"`
	RealPrice     string `json:"real_price,omitempty"`
	BankNo        string `json:"bank_no,omitempty"`
	BankName      string `json:"bank_name,omitempty"`
	BankFrom      string `json:"bank_from,omitempty"`
	BankOwner     string `json:"bank_owner,omitempty"`
	Remark        string `json:"remark,omitempty"`
	AlipayQrcode  string `json:"alipay_qrcode,omitempty"`
	Rate          string `json:"rate,omitempty"`
}

type PayOrderError struct {
	Field   string `json:"field,omitempty"`
	Details string `json:"details,omitempty"`
}

type QueryTransactionRequest struct {
	PayCustomerID string `json:"pay_customer_id"`
	PayApplyDate  string `json:"pay_apply_date"`
	PayOrderID    any    `json:"pay_order_id"`
	PayMD5Sign    string `json:"pay_md5_sign"`
}

type QueryTransactionResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    []QueryTransactionData `json:"data,omitempty"`
	Error   *PayOrderError         `json:"error,omitempty"`
}

type QueryTransactionData struct {
	CustomerID       string                     `json:"customer_id"`
	OrderID          string                     `json:"order_id"`
	TransactionID    string                     `json:"transaction_id"`
	Status           int                        `json:"status"`
	OrderAmount      string                     `json:"order_amount"`
	RealAmount       string                     `json:"real_amount"`
	Created          string                     `json:"created,omitempty"`
	Expired          string                     `json:"expired,omitempty"`
	NotifyURL        string                     `json:"notify_url,omitempty"`
	CustomerCallback string                     `json:"customer_callback,omitempty"`
	Extra            QueryTransactionExtra      `json:"extra"`
	RCFeedback       QueryTransactionRCFeedback `json:"rc_feedback"`
	PayChannelID     string                     `json:"pay_channel_id,omitempty"`
	ViewURL          string                     `json:"view_url,omitempty"`
}

type QueryTransactionExtra struct {
	UserName       string `json:"user_name"`
	PayProductName any    `json:"pay_product_name"`
}

type QueryTransactionRCFeedback struct {
	Rate         any `json:"rate"`
	DisplayPrice any `json:"display_price"`
}

type GatewaySecurityConfig struct {
	SignKey        string
	MaxSkewSeconds int
}

var gatewayDepositChannelIDToCode = map[string]string{
	"1000": "CREDIT",
	"1001": "APPLEPAY",
	"1002": "GOOGLEPAY",
	"1005": "WEBATM",
	"1006": "VACC",
	"1007": "CVS",
	"1008": "BARCODE",
}

var gatewayDepositChannelCodeToID = map[string]string{
	"CREDIT":    "1000",
	"APPLEPAY":  "1001",
	"GOOGLEPAY": "1002",
	"WEBATM":    "1005",
	"VACC":      "1006",
	"CVS":       "1007",
	"BARCODE":   "1008",
}

func (h *DepositHandler) CreatePayOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req PayOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_JSON", "invalid JSON body", "", "")
		return
	}

	amount, err := parseGatewayAmount(req.PayAmount)
	if err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_AMOUNT", err.Error(), "pay_amount", "")
		return
	}
	if err := h.verifyGatewayApplyDate(req.PayApplyDate); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_APPLY_DATE", err.Error(), "pay_apply_date", "")
		return
	}
	if err := h.verifyGatewaySignature(req.payOrderSignFields(), req.PayMD5Sign); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_SIGN", err.Error(), "pay_md5_sign", "")
		return
	}

	channelCode, err := mapGatewayDepositChannelIDToCode(strings.TrimSpace(req.PayChannelID))
	if err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_CHANNEL", err.Error(), "pay_channel_id", "")
		return
	}

	result, err := h.depositService.CreateDeposit(service.CreateDepositRequest{
		MerchantID:      strings.TrimSpace(req.PayCustomerID),
		MerchantOrderNo: strings.TrimSpace(req.PayOrderID),
		Amount:          amount,
		Currency:        "TWD",
		ItemDesc:        strings.TrimSpace(req.PayProductName),
		ChannelCode:     channelCode,
		NotifyURL:       strings.TrimSpace(req.PayNotifyURL),
		BankAccounts:    append([]string(nil), req.BankAccount...),
		StoreNumbers:    append([]string(nil), req.StoreNumber...),
		UserName:        strings.TrimSpace(req.UserName),
		BankID:          strings.TrimSpace(req.BankID),
		PayCurrency:     strings.TrimSpace(req.PayCurrency),
		Mobile:          strings.TrimSpace(req.Mobile),
		IDNo:            strings.TrimSpace(req.IDNo),
	})
	if err != nil {
		field := ""
		if strings.Contains(err.Error(), "channel_code") {
			field = "pay_channel_id"
		}
		if strings.Contains(err.Error(), "merchant_order_no") {
			field = "pay_order_id"
		}
		if strings.Contains(err.Error(), "amount") {
			field = "pay_amount"
		}
		writePayOrderError(w, nethttp.StatusBadRequest, "CREATE_ORDER_FAILED", err.Error(), field, "")
		return
	}

	response.JSON(w, nethttp.StatusOK, PayOrderResponse{
		Code:    0,
		Message: "success",
		Data: &PayOrderData{
			OrderID:       result.Order.MerchantOrderNo,
			TransactionID: result.Order.OrderNo,
			ViewURL:       buildAbsoluteURL(r, fmt.Sprintf("/api/v1/deposits/%s/redirect", result.Order.OrderNo)),
			UserName:      result.Order.UserName,
			BillPrice:     formatGatewayAmount(result.Order.AmountCents),
			RealPrice:     formatGatewayAmount(result.Order.AmountCents),
			Remark:        buildGatewayDepositRemark(result.Order),
			BankNo:        firstNonEmpty(result.Order.BankAccounts),
			BankName:      buildGatewayDepositBankName(result.Order),
			BankFrom:      buildGatewayDepositBankFrom(result.Order),
			BankOwner:     result.Order.UserName,
		},
	})
}

func (h *DepositHandler) QueryTransaction(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req QueryTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1000, "invalid JSON body", "", "")
		return
	}

	req.PayCustomerID = strings.TrimSpace(req.PayCustomerID)
	req.PayApplyDate = strings.TrimSpace(req.PayApplyDate)
	orderIDs, err := parseGatewayOrderIDs(req.PayOrderID)
	if err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1004, err.Error(), "pay_order_id", "")
		return
	}
	if req.PayCustomerID == "" {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1001, "pay_customer_id is required", "pay_customer_id", "")
		return
	}
	if err := h.verifyGatewayApplyDate(req.PayApplyDate); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1002, err.Error(), "pay_apply_date", "")
		return
	}
	if len(orderIDs) == 0 {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1004, "pay_order_id is required", "pay_order_id", "")
		return
	}
	if err := h.verifyGatewaySignature(req.queryTransactionSignFields(), req.PayMD5Sign); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1003, err.Error(), "pay_md5_sign", "")
		return
	}

	items := make([]QueryTransactionData, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		order, ok := h.depositService.FindDepositByMerchantOrderNo(req.PayCustomerID, orderID)
		if !ok {
			continue
		}
		items = append(items, QueryTransactionData{
			CustomerID:       order.MerchantCode,
			OrderID:          order.MerchantOrderNo,
			TransactionID:    order.OrderNo,
			Status:           mapGatewayDepositQueryStatus(string(order.Status)),
			OrderAmount:      formatGatewayAmount(order.AmountCents),
			RealAmount:       formatGatewayAmount(order.AmountCents),
			Created:          order.CreatedAt.Format("2006-01-02 15:04:05"),
			Expired:          "",
			NotifyURL:        order.CallbackURL,
			CustomerCallback: "",
			Extra: QueryTransactionExtra{
				UserName:       order.UserName,
				PayProductName: order.ItemDesc,
			},
			RCFeedback: QueryTransactionRCFeedback{
				Rate:         nil,
				DisplayPrice: nil,
			},
			PayChannelID: mapGatewayDepositChannelCodeToID(order.ChannelCode),
			ViewURL:      buildAbsoluteURL(r, fmt.Sprintf("/api/v1/deposits/%s/redirect", order.OrderNo)),
		})
	}

	response.JSON(w, nethttp.StatusOK, QueryTransactionResponse{
		Code:    0,
		Message: "success",
		Data:    items,
	})
}

func parseGatewayAmount(value any) (int64, error) {
	switch v := value.(type) {
	case float64:
		if v <= 0 {
			return 0, fmt.Errorf("pay_amount must be greater than zero")
		}
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("pay_amount must be a whole TWD amount")
		}
		return int64(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, fmt.Errorf("pay_amount is required")
		}
		if strings.Contains(trimmed, ".") {
			return 0, fmt.Errorf("pay_amount must be a whole TWD amount")
		}
		n, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("pay_amount must be a valid integer")
		}
		if n <= 0 {
			return 0, fmt.Errorf("pay_amount must be greater than zero")
		}
		return n, nil
	default:
		return 0, fmt.Errorf("pay_amount must be a number or numeric string")
	}
}

func buildAbsoluteURL(r *nethttp.Request, path string) string {
	scheme := "http"
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	} else if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, r.Host, path)
}

func writePayOrderError(w nethttp.ResponseWriter, status int, code, message, field, details string) {
	response.JSON(w, status, PayOrderResponse{
		Code:    mapGatewayDepositErrorCode(code),
		Message: message,
		Error: &PayOrderError{
			Field:   field,
			Details: details,
		},
	})
}

func writeQueryTransactionError(w nethttp.ResponseWriter, status int, code int, message, field, details string) {
	response.JSON(w, status, QueryTransactionResponse{
		Code:    code,
		Message: message,
		Error: &PayOrderError{
			Field:   field,
			Details: details,
		},
	})
}

func mapGatewayDepositStatus(status string) string {
	switch status {
	case "paid":
		return "30000"
	case "failed":
		return "40000"
	default:
		return "10000"
	}
}

func mapGatewayDepositQueryStatus(status string) int {
	switch status {
	case "paid":
		return 2
	case "failed":
		return 5
	default:
		return 0
	}
}

func mapGatewayDepositChannelIDToCode(channelID string) (string, error) {
	channelCode, ok := gatewayDepositChannelIDToCode[channelID]
	if !ok {
		return "", fmt.Errorf("unsupported pay_channel_id: %s", channelID)
	}
	return channelCode, nil
}

func mapGatewayDepositChannelCodeToID(channelCode string) string {
	if channelID, ok := gatewayDepositChannelCodeToID[channelCode]; ok {
		return channelID
	}
	return channelCode
}

func (r PayOrderRequest) payOrderSignFields() map[string]any {
	return map[string]any{
		"pay_customer_id":  strings.TrimSpace(r.PayCustomerID),
		"pay_apply_date":   strings.TrimSpace(r.PayApplyDate),
		"pay_order_id":     strings.TrimSpace(r.PayOrderID),
		"pay_notify_url":   strings.TrimSpace(r.PayNotifyURL),
		"pay_amount":       normalizeGatewaySignValue(r.PayAmount),
		"pay_channel_id":   strings.TrimSpace(r.PayChannelID),
		"bank_account":     r.BankAccount,
		"store_number":     r.StoreNumber,
		"pay_product_name": strings.TrimSpace(r.PayProductName),
		"user_name":        strings.TrimSpace(r.UserName),
		"bank_id":          strings.TrimSpace(r.BankID),
		"pay_currency":     strings.TrimSpace(r.PayCurrency),
		"mobile":           strings.TrimSpace(r.Mobile),
		"id_no":            strings.TrimSpace(r.IDNo),
	}
}

func (r QueryTransactionRequest) queryTransactionSignFields() map[string]any {
	return map[string]any{
		"pay_customer_id": strings.TrimSpace(r.PayCustomerID),
		"pay_apply_date":  strings.TrimSpace(r.PayApplyDate),
		"pay_order_id":    normalizeGatewayOrderIDValue(r.PayOrderID),
	}
}

func (h *DepositHandler) verifyGatewayApplyDate(applyDate string) error {
	applyDate = strings.TrimSpace(applyDate)
	if applyDate == "" {
		return fmt.Errorf("pay_apply_date is required")
	}
	ts, err := strconv.ParseInt(applyDate, 10, 64)
	if err != nil {
		return fmt.Errorf("pay_apply_date must be a valid unix timestamp")
	}
	maxSkew := h.gateway.MaxSkewSeconds
	if maxSkew <= 0 {
		maxSkew = 300
	}
	now := time.Now().Unix()
	diff := now - ts
	if diff < 0 {
		diff = -diff
	}
	if diff > int64(maxSkew) {
		return fmt.Errorf("pay_apply_date exceeded allowed time skew")
	}
	return nil
}

func (h *DepositHandler) verifyGatewaySignature(fields map[string]any, provided string) error {
	provided = strings.ToUpper(strings.TrimSpace(provided))
	if provided == "" {
		return fmt.Errorf("pay_md5_sign is required")
	}
	if strings.TrimSpace(h.gateway.SignKey) == "" {
		return fmt.Errorf("gateway sign key is not configured")
	}
	expected, err := buildGatewayMD5Signature(fields, h.gateway.SignKey)
	if err != nil {
		return err
	}
	if expected != provided {
		return fmt.Errorf("pay_md5_sign verification failed")
	}
	return nil
}

func buildGatewayMD5Signature(fields map[string]any, signKey string) (string, error) {
	keys := make([]string, 0, len(fields))
	for key, value := range fields {
		if isGatewayEmptyValue(value) {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var parts []string
	for _, key := range keys {
		value, err := renderGatewaySignValue(fields[key])
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}
	parts = append(parts, fmt.Sprintf("key=%s", signKey))
	sum := md5.Sum([]byte(strings.Join(parts, "&")))
	return strings.ToUpper(fmt.Sprintf("%x", sum)), nil
}

func renderGatewaySignValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), nil
	case []string:
		data, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func isGatewayEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		return len(v) == 0
	default:
		return false
	}
}

func normalizeGatewaySignValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func normalizeGatewayOrderIDValue(value any) any {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return v
	case []any:
		ids := make([]string, 0, len(v))
		for _, item := range v {
			ids = append(ids, strings.TrimSpace(fmt.Sprintf("%v", item)))
		}
		return ids
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseGatewayOrderIDs(value any) ([]string, error) {
	switch v := value.(type) {
	case string:
		id := strings.TrimSpace(v)
		if id == "" {
			return nil, nil
		}
		return []string{id}, nil
	case []any:
		ids := make([]string, 0, len(v))
		for _, item := range v {
			id := strings.TrimSpace(fmt.Sprintf("%v", item))
			if id != "" {
				ids = append(ids, id)
			}
		}
		return ids, nil
	case []string:
		ids := make([]string, 0, len(v))
		for _, item := range v {
			id := strings.TrimSpace(item)
			if id != "" {
				ids = append(ids, id)
			}
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("pay_order_id must be a string or array")
	}
}

func formatGatewayAmount(amountCents int64) string {
	return fmt.Sprintf("%.8f", float64(amountCents)/100)
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildGatewayDepositRemark(order domain.DepositOrder) string {
	parts := make([]string, 0, 2)
	if payCurrency := strings.TrimSpace(order.PayCurrency); payCurrency != "" {
		parts = append(parts, fmt.Sprintf("pay_currency=%s", payCurrency))
	}
	if mobile := strings.TrimSpace(order.Mobile); mobile != "" {
		parts = append(parts, fmt.Sprintf("mobile=%s", mobile))
	}
	return strings.Join(parts, "; ")
}

func buildGatewayDepositBankName(order domain.DepositOrder) string {
	switch {
	case len(order.BankAccounts) > 0:
		return "Bank Transfer"
	case len(order.StoreNumbers) > 0:
		return "Convenience Store"
	case strings.TrimSpace(order.ChannelCode) != "":
		return strings.TrimSpace(order.ChannelCode)
	default:
		return ""
	}
}

func buildGatewayDepositBankFrom(order domain.DepositOrder) string {
	return strings.TrimSpace(order.BankID)
}

func mapGatewayDepositErrorCode(code string) int {
	switch code {
	case "INVALID_JSON":
		return 1000
	case "INVALID_AMOUNT":
		return 1001
	case "INVALID_APPLY_DATE":
		return 1002
	case "INVALID_SIGN":
		return 1003
	case "INVALID_CHANNEL":
		return 1004
	case "CREATE_ORDER_FAILED":
		return 1005
	default:
		return 9999
	}
}

func (h *DepositHandler) deliverGatewayDepositCallback(order domain.DepositOrder) error {
	callbackURL := strings.TrimSpace(order.CallbackURL)
	if callbackURL == "" {
		return nil
	}

	status, message := mapGatewayDepositCallbackStatus(string(order.Status))
	payerInfo := firstNonEmpty(append(append([]string(nil), order.BankAccounts...), order.StoreNumbers...))
	payload := map[string]any{
		"customer_id":    order.MerchantCode,
		"order_id":       order.MerchantOrderNo,
		"transaction_id": order.OrderNo,
		"order_amount":   order.AmountCents / 100,
		"real_amount":    order.AmountCents / 100,
		"status":         status,
		"message":        message,
		"payer_info":     payerInfo,
		"extra": map[string]any{
			"user_name":        order.UserName,
			"pay_product_name": order.ItemDesc,
		},
	}

	signFields := map[string]any{
		"customer_id":    order.MerchantCode,
		"order_id":       order.MerchantOrderNo,
		"transaction_id": order.OrderNo,
		"order_amount":   order.AmountCents / 100,
		"real_amount":    order.AmountCents / 100,
		"status":         status,
		"message":        message,
		"payer_info":     payerInfo,
	}
	if strings.TrimSpace(h.gateway.SignKey) != "" {
		sign, err := buildGatewayMD5Signature(signFields, h.gateway.SignKey)
		if err != nil {
			return err
		}
		payload["sign"] = sign
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := (&nethttp.Client{Timeout: 10 * time.Second}).Post(callbackURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(respBody)) != "OK" {
		return fmt.Errorf("callback response was not OK: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func mapGatewayDepositCallbackStatus(status string) (string, string) {
	switch status {
	case "paid":
		return "30000", "paid"
	case "failed":
		return "50000", "failed"
	default:
		return "10000", "processing"
	}
}
