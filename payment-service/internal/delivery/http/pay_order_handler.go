package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/domain"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/service"
	"payment-service/pkg/response"
)

type PayOrderRequest struct {
	PayCustomerID  string                `json:"pay_customer_id"`
	PayApplyDate   gatewayFlexibleString `json:"pay_apply_date"`
	PayOrderID     string                `json:"pay_order_id"`
	PayAmount      any                   `json:"pay_amount"`
	PayChannelID   string                `json:"pay_channel_id"`
	BankAccount    []string              `json:"bank_account"`
	StoreNumber    []string              `json:"store_number"`
	PayNotifyURL   string                `json:"pay_notify_url"`
	PayProductName string                `json:"pay_product_name"`
	UserName       string                `json:"user_name"`
	BankID         string                `json:"bank_id"`
	PayCurrency    string                `json:"pay_currency"`
	Mobile         string                `json:"mobile"`
	IDNo           string                `json:"id_no"`
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
	PayCustomerID string                `json:"pay_customer_id"`
	PayApplyDate  gatewayFlexibleString `json:"pay_apply_date"`
	PayOrderID    any                   `json:"pay_order_id"`
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
	HMACSecret               string
	PreviousHMACSecret       string
	MaxSkewSeconds           int
	CustomerID               string
	HMACDiagnosticsEnabled   bool
	DepositCallbackAllowlist []string
	PayoutCallbackAllowlist  []string
}

type gatewayFlexibleString string

func (s *gatewayFlexibleString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = gatewayFlexibleString(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(data, &num); err == nil {
		*s = gatewayFlexibleString(num.String())
		return nil
	}
	return fmt.Errorf("value must be a string or number")
}

func (s gatewayFlexibleString) String() string {
	return string(s)
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
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_JSON", "invalid JSON body", "", "")
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_JSON", "invalid JSON body", "", "")
		return
	}

	amount, err := parseGatewayAmount(req.PayAmount)
	if err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_AMOUNT", err.Error(), "pay_amount", "")
		return
	}
	if err := h.verifyGatewayCustomerID(req.PayCustomerID); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_CUSTOMER", err.Error(), "pay_customer_id", "")
		return
	}
	if err := h.verifyGatewayNotifyURL(req.PayNotifyURL); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_NOTIFY_URL", err.Error(), "pay_notify_url", "")
		return
	}
	if err := h.verifyGatewayApplyDate(req.PayApplyDate.String()); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_APPLY_DATE", err.Error(), "pay_apply_date", "")
		return
	}
	if err := ensureConsistentGatewayCustomerID("pay_customer_id", strings.TrimSpace(r.Header.Get("X-Customer-Id")), req.PayCustomerID); err != nil {
		writePayOrderError(w, nethttp.StatusBadRequest, "INVALID_CUSTOMER", err.Error(), "pay_customer_id", "")
		return
	}
	if err := authenticateGatewayRequest(r, buildGatewayRequestAuth(r, req.PayCustomerID, body), h.gateway, h.nonceStore, time.Now()); err != nil {
		code := "INVALID_SIGN"
		field := "X-Signature"
		if strings.Contains(err.Error(), "customer") {
			code = "INVALID_CUSTOMER"
			field = "pay_customer_id"
		}
		writePayOrderError(w, nethttp.StatusBadRequest, code, err.Error(), field, "")
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
			ViewURL:       h.buildAbsoluteURL(fmt.Sprintf("/api/v1/deposits/%s/redirect", result.Order.OrderNo)),
			Expired:       formatGatewayExpiry(result.Order.ExpiresAt),
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
	body, err := readPayoutRequestBody(r)
	if err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1000, "invalid JSON body", "", "")
		return
	}
	if err := decodePayoutJSONBytes(body, &req); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1000, "invalid JSON body", "", "")
		return
	}

	req.PayCustomerID = strings.TrimSpace(req.PayCustomerID)
	req.PayApplyDate = gatewayFlexibleString(strings.TrimSpace(req.PayApplyDate.String()))
	orderIDs, err := parseGatewayOrderIDs(req.PayOrderID)
	if err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1004, err.Error(), "pay_order_id", "")
		return
	}
	if req.PayCustomerID == "" {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1001, "pay_customer_id is required", "pay_customer_id", "")
		return
	}
	if err := h.verifyGatewayCustomerID(req.PayCustomerID); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1001, err.Error(), "pay_customer_id", "")
		return
	}
	if err := h.verifyGatewayApplyDate(req.PayApplyDate.String()); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1002, err.Error(), "pay_apply_date", "")
		return
	}
	if len(orderIDs) == 0 {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1004, "pay_order_id is required", "pay_order_id", "")
		return
	}
	if err := ensureConsistentGatewayCustomerID("pay_customer_id", strings.TrimSpace(r.Header.Get("X-Customer-Id")), req.PayCustomerID); err != nil {
		writeQueryTransactionError(w, nethttp.StatusBadRequest, 1001, err.Error(), "pay_customer_id", "")
		return
	}
	if err := authenticateGatewayRequest(r, buildGatewayRequestAuth(r, req.PayCustomerID, body), h.gateway, h.nonceStore, time.Now()); err != nil {
		code := 1003
		field := "X-Signature"
		if strings.Contains(err.Error(), "customer") {
			code = 1001
			field = "pay_customer_id"
		}
		writeQueryTransactionError(w, nethttp.StatusBadRequest, code, err.Error(), field, "")
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
			Expired:          formatGatewayExpiry(order.ExpiresAt),
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
			ViewURL:      h.buildAbsoluteURL(fmt.Sprintf("/api/v1/deposits/%s/redirect", order.OrderNo)),
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
	case json.Number:
		return parseGatewayAmount(v.String())
	case float64:
		if v <= 0 || v > float64(maxGatewayTWDAmount) {
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
		if n > maxGatewayTWDAmount {
			return 0, fmt.Errorf("pay_amount is too large")
		}
		return n, nil
	default:
		return 0, fmt.Errorf("pay_amount must be a number or numeric string")
	}
}

func (h *DepositHandler) buildAbsoluteURL(path string) string {
	return h.publicBaseURL + path
}

const maxGatewayTWDAmount int64 = 92233720368547758

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
	case "expired":
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
	case "expired":
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
		"pay_apply_date":   strings.TrimSpace(r.PayApplyDate.String()),
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
		"pay_apply_date":  strings.TrimSpace(r.PayApplyDate.String()),
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

func (h *DepositHandler) verifyGatewayCustomerID(customerID string) error {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return fmt.Errorf("pay_customer_id is required")
	}
	if expected := strings.TrimSpace(h.gateway.CustomerID); expected != "" && customerID != expected {
		return fmt.Errorf("pay_customer_id does not match configured gateway customer")
	}
	return nil
}

func (h *DepositHandler) verifyGatewayNotifyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("pay_notify_url is required")
	}
	callbackURL, err := url.ParseRequestURI(raw)
	if err != nil || callbackURL.Scheme == "" || callbackURL.Host == "" {
		return fmt.Errorf("pay_notify_url must be an absolute HTTP(S) URL")
	}
	if callbackURL.Scheme != "https" {
		return fmt.Errorf("pay_notify_url must use HTTPS")
	}
	host := strings.TrimSpace(callbackURL.Hostname())
	if host == "" {
		return fmt.Errorf("pay_notify_url host is required")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("pay_notify_url must not target localhost")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
			return fmt.Errorf("pay_notify_url must not target a private or loopback address")
		}
	}
	return nil
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
	case "INVALID_CUSTOMER":
		return 1001
	case "INVALID_APPLY_DATE":
		return 1002
	case "INVALID_SIGN":
		return 1003
	case "INVALID_CHANNEL":
		return 1004
	case "CREATE_ORDER_FAILED":
		return 1005
	case "INVALID_NOTIFY_URL":
		return 1006
	default:
		return 9999
	}
}

func (h *DepositHandler) buildGatewayDepositCallbackRequest(order domain.DepositOrder) (string, []byte, error) {
	callbackURL := strings.TrimSpace(order.CallbackURL)
	if callbackURL == "" {
		return "", nil, nil
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
		"event":       "deposit_result",
		"merchant_id": order.MerchantCode,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	return callbackURL, body, nil
}

func (h *DepositHandler) deliverGatewayDepositCallback(order domain.DepositOrder) error {
	callbackURL, body, err := h.buildGatewayDepositCallbackRequest(order)
	if err != nil || callbackURL == "" {
		return err
	}
	return h.postGatewayDepositCallback(callbackURL, body)
}

func (h *DepositHandler) postGatewayDepositCallback(callbackURL string, body []byte) error {
	req, err := nethttp.NewRequest(nethttp.MethodPost, callbackURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(h.gateway.HMACSecret) != "" {
		customerID, path, err := buildGatewayCallbackAuthContext(callbackURL, body)
		if err != nil {
			return err
		}
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
		signature, err := providerGateway.BuildHMACSignature(providerGateway.HMACRequestAuth{
			CustomerID: customerID,
			Timestamp:  timestamp,
			Nonce:      nonce,
			Method:     nethttp.MethodPost,
			Path:       path,
			Body:       body,
		}, h.gateway.HMACSecret)
		if err != nil {
			return err
		}
		req.Header.Set("X-Customer-Id", customerID)
		req.Header.Set("X-Timestamp", timestamp)
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Signature", signature)
	}
	resp, err := service.PostPublicHTTPSCallback(context.Background(), callbackURL, body, 10*time.Second, req.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || strings.TrimSpace(string(respBody)) != "OK" {
		return fmt.Errorf("callback response was not OK: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func buildGatewayCallbackAuthContext(callbackURL string, body []byte) (string, string, error) {
	parsed, err := url.ParseRequestURI(callbackURL)
	if err != nil {
		return "", "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", err
	}
	customerID := strings.TrimSpace(fmt.Sprintf("%v", payload["customer_id"]))
	if customerID == "" {
		return "", "", fmt.Errorf("customer_id is required")
	}
	return customerID, parsed.Path, nil
}

func mapGatewayDepositCallbackStatus(status string) (string, string) {
	switch status {
	case "paid":
		return "30000", "paid"
	case "failed":
		return "50000", "failed"
	case "expired":
		return "40000", "expired"
	default:
		return "10000", "processing"
	}
}

func formatGatewayExpiry(expiresAt *time.Time) string {
	if expiresAt == nil || expiresAt.IsZero() {
		return ""
	}
	return expiresAt.In(asiaTaipeiLocation).Format("2006-01-02 15:04:05")
}

var asiaTaipeiLocation = time.FixedZone("Asia/Taipei", 8*60*60)
