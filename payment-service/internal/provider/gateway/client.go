package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	PayoutCreatePath  = "/api/payments/pay_order"
	PayoutQueryPath   = "/api/payments/query_transaction"
	PayoutBalancePath = "/api/payments/balance"
)

type PayoutClient struct {
	baseURL    string
	customerID string
	hmacSecret string
	notifyURL  string
	httpClient *http.Client
	now        func() time.Time
}

type CreatePayoutRequest struct {
	PayCustomerID    string `json:"pay_customer_id"`
	PayApplyDate     string `json:"pay_apply_date"`
	PayOrderID       string `json:"pay_order_id"`
	PayNotifyURL     string `json:"pay_notify_url"`
	PayAmount        string `json:"pay_amount"`
	PayAccountName   string `json:"pay_account_name"`
	PayCardNo        string `json:"pay_card_no"`
	PayBankName      string `json:"pay_bank_name"`
	PayChannelID     string `json:"pay_channel_id,omitempty"`
	PaySubBranch     string `json:"pay_sub_branch,omitempty"`
	PaySubBranchCode string `json:"pay_sub_branch_code,omitempty"`
	PayCity          string `json:"pay_city,omitempty"`
	PayValidateID    string `json:"pay_validate_id,omitempty"`
	PayCurrency      string `json:"pay_currency,omitempty"`
}

type CreatePayoutResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    *CreatePayoutData `json:"data,omitempty"`
}

type CreatePayoutData struct {
	OrderID       string `json:"order_id,omitempty"`
	TransactionID string `json:"transaction_id"`
	Amount        string `json:"amount"`
}

type QueryPayoutRequest struct {
	PayCustomerID string   `json:"pay_customer_id"`
	PayApplyDate  string   `json:"pay_apply_date"`
	PayOrderID    []string `json:"pay_order_id"`
}

type QueryPayoutResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    *QueryPayoutData `json:"data,omitempty"`
}

type QueryPayoutData struct {
	Status             int    `json:"status"`
	StatusName         string `json:"status_name"`
	Message            any    `json:"msg"`
	MemberID           any    `json:"member_id"`
	OutTradeNo         string `json:"out_trade_no"`
	Amount             string `json:"amount"`
	Fee                string `json:"fee"`
	PaymentID          string `json:"payment_id"`
	PaymentSuccessTime string `json:"payment_success_time"`
}

type BalanceRequest struct {
	PayCustomerID string `json:"pay_customer_id"`
	PayApplyDate  string `json:"pay_apply_date"`
}

type BalanceResponse struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    *BalanceData `json:"data,omitempty"`
}

type BalanceData struct {
	Balance             string `json:"balance"`
	BalanceOriginal     string `json:"balance_original"`
	BalanceAvailable    string `json:"balance_available"`
	BalanceUnsettlement string `json:"balance_unsettlement"`
}

type PayoutCallbackRequest struct {
	CustomerID      any    `json:"customer_id"`
	OrderID         string `json:"order_id"`
	Amount          string `json:"amount"`
	DateTime        string `json:"datetime"`
	TransactionID   string `json:"transaction_id"`
	TransactionCode string `json:"transaction_code"`
	TransactionMsg  string `json:"transaction_msg"`
}

type UpstreamError struct {
	StatusCode int
	Body       string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("gateway payout upstream returned HTTP %d", e.StatusCode)
}

func NewPayoutClient(baseURL, customerID, hmacSecret, notifyURL string, timeout time.Duration) *PayoutClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &PayoutClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		customerID: strings.TrimSpace(customerID),
		hmacSecret: strings.TrimSpace(hmacSecret),
		notifyURL:  strings.TrimSpace(notifyURL),
		httpClient: &http.Client{Timeout: timeout},
		now:        time.Now,
	}
}

func (c *PayoutClient) NotifyURL() string {
	return c.notifyURL
}

func (c *PayoutClient) CustomerID() string {
	return c.customerID
}

func (c *PayoutClient) CreatePayout(ctx context.Context, req CreatePayoutRequest) (CreatePayoutResponse, error) {
	if err := c.prepareCreateRequest(&req); err != nil {
		return CreatePayoutResponse{}, err
	}
	var result CreatePayoutResponse
	if err := c.postJSON(ctx, PayoutCreatePath, req, &result); err != nil {
		return CreatePayoutResponse{}, err
	}
	return result, nil
}

func (c *PayoutClient) QueryPayout(ctx context.Context, req QueryPayoutRequest) (QueryPayoutResponse, error) {
	if err := c.prepareQueryRequest(&req); err != nil {
		return QueryPayoutResponse{}, err
	}
	var result QueryPayoutResponse
	if err := c.postJSON(ctx, PayoutQueryPath, req, &result); err != nil {
		return QueryPayoutResponse{}, err
	}
	return result, nil
}

func (c *PayoutClient) QueryBalance(ctx context.Context, req BalanceRequest) (BalanceResponse, error) {
	if err := c.prepareBalanceRequest(&req); err != nil {
		return BalanceResponse{}, err
	}
	var result BalanceResponse
	if err := c.postJSON(ctx, PayoutBalancePath, req, &result); err != nil {
		return BalanceResponse{}, err
	}
	return result, nil
}

func (c *PayoutClient) prepareCreateRequest(req *CreatePayoutRequest) error {
	if err := c.prepareCommon(&req.PayCustomerID, &req.PayApplyDate); err != nil {
		return err
	}
	if req.PayNotifyURL == "" {
		req.PayNotifyURL = c.notifyURL
	}
	if strings.TrimSpace(req.PayOrderID) == "" || strings.TrimSpace(req.PayNotifyURL) == "" ||
		strings.TrimSpace(req.PayAmount) == "" || strings.TrimSpace(req.PayAccountName) == "" ||
		strings.TrimSpace(req.PayCardNo) == "" || strings.TrimSpace(req.PayBankName) == "" {
		return errors.New("pay_order_id, pay_notify_url, pay_amount, pay_account_name, pay_card_no and pay_bank_name are required")
	}
	req.PayBankName = strings.TrimSpace(req.PayBankName)
	if len(req.PayBankName) != 3 {
		return errors.New("pay_bank_name must be a 3-digit bank code")
	}
	if !IsSupportedPayoutBankCode(req.PayBankName) {
		return errors.New("pay_bank_name is not in gateway supported bank code whitelist")
	}
	amount, err := strconv.ParseFloat(req.PayAmount, 64)
	if err != nil || amount <= 0 {
		return errors.New("pay_amount must be greater than zero")
	}
	callbackURL, err := url.ParseRequestURI(req.PayNotifyURL)
	if err != nil || (callbackURL.Scheme != "http" && callbackURL.Scheme != "https") {
		return errors.New("pay_notify_url must be an absolute HTTP(S) URL")
	}
	return nil
}

func (c *PayoutClient) prepareQueryRequest(req *QueryPayoutRequest) error {
	if err := c.prepareCommon(&req.PayCustomerID, &req.PayApplyDate); err != nil {
		return err
	}
	if len(req.PayOrderID) == 0 {
		return errors.New("pay_order_id is required")
	}
	return nil
}

func (c *PayoutClient) prepareBalanceRequest(req *BalanceRequest) error {
	if err := c.prepareCommon(&req.PayCustomerID, &req.PayApplyDate); err != nil {
		return err
	}
	return nil
}

func (c *PayoutClient) prepareCommon(customerID, applyDate *string) error {
	if c.baseURL == "" {
		return errors.New("gateway base URL is not configured")
	}
	if c.hmacSecret == "" {
		return errors.New("gateway hmac secret is not configured")
	}
	if c.customerID != "" {
		if *customerID != "" && strings.TrimSpace(*customerID) != c.customerID {
			return errors.New("pay_customer_id does not match configured gateway customer")
		}
		*customerID = c.customerID
	}
	if strings.TrimSpace(*customerID) == "" {
		return errors.New("pay_customer_id is required")
	}
	if strings.TrimSpace(*applyDate) == "" {
		*applyDate = strconv.FormatInt(c.now().Unix(), 10)
	}
	return nil
}

func (c *PayoutClient) postJSON(ctx context.Context, path string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	nonce := strconv.FormatInt(c.now().UnixNano(), 10)
	signature, err := BuildHMACSignature(HMACRequestAuth{
		CustomerID: c.customerID,
		Timestamp:  timestamp,
		Nonce:      nonce,
		Method:     http.MethodPost,
		Path:       path,
		Body:       body,
	}, c.hmacSecret)
	if err != nil {
		return err
	}
	req.Header.Set("X-Customer-Id", c.customerID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", signature)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gateway payout request failed; do not retry before querying provider state: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &UpstreamError{StatusCode: res.StatusCode, Body: string(raw)}
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(result); err != nil {
		return fmt.Errorf("decode gateway payout response: %w", err)
	}
	return nil
}
