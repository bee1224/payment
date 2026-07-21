package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL        string
	customerID     string
	customerSecret string
	merchantID     string
	merchantSecret string
	apiKey         string
	httpClient     *http.Client
}

type Credentials struct {
	BaseURL, CustomerID, CustomerSecret, MerchantID, MerchantSecret, APIKey string
}

func NewClient(c Credentials) (*Client, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("PAYMENT_SANDBOX_BASE_URL is required for API calls")
	}
	u, err := url.ParseRequestURI(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid payment base URL: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("PAYMENT_SANDBOX_BASE_URL must be an HTTPS URL")
	}
	if strings.EqualFold(u.Hostname(), "api.nnviopp.com") {
		return nil, fmt.Errorf("Production payment URL is not permitted")
	}
	return &Client{baseURL: strings.TrimRight(c.BaseURL, "/"), customerID: c.CustomerID, customerSecret: c.CustomerSecret, merchantID: c.MerchantID, merchantSecret: c.MerchantSecret, apiKey: c.APIKey, httpClient: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (c *Client) CreateCollection(ctx context.Context, request CollectionCreateRequest) ([]byte, error) {
	request.PayCustomerID = c.customerID
	if err := request.validate(); err != nil {
		return nil, err
	}
	return c.post(ctx, "/api/pay_order", c.customerID, c.customerSecret, "X-Customer-Id", request)
}

func (c *Client) QueryCollection(ctx context.Context, request CollectionQueryRequest) ([]byte, error) {
	request.PayCustomerID = c.customerID
	if err := request.validate(); err != nil {
		return nil, err
	}
	return c.post(ctx, "/api/query_transaction", c.customerID, c.customerSecret, "X-Customer-Id", request)
}

func (c *Client) CreatePayout(ctx context.Context, request PayoutCreateRequest) ([]byte, error) {
	request.MerchantID = c.merchantID
	request.APIKey = c.apiKey
	if err := request.validate(); err != nil {
		return nil, err
	}
	return c.post(ctx, "/api/payouts", c.merchantID, c.merchantSecret, "X-Merchant-Id", request)
}

func (c *Client) QueryPayout(ctx context.Context, request PayoutQueryRequest) ([]byte, error) {
	request.MerchantID = c.merchantID
	request.APIKey = c.apiKey
	if err := request.validate(); err != nil {
		return nil, err
	}
	return c.post(ctx, "/api/payouts/query", c.merchantID, c.merchantSecret, "X-Merchant-Id", request)
}

func (c *Client) post(ctx context.Context, path, identifier, secret, identifierHeader string, request any) ([]byte, error) {
	if identifier == "" || secret == "" {
		return nil, fmt.Errorf("Sandbox identifier and HMAC secret are required")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(identifierHeader, identifier)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", Sign(identifier, timestamp, nonce, http.MethodPost, path, body, secret))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request payment sandbox: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("payment sandbox returned HTTP %d", resp.StatusCode)
	}
	return responseBody, nil
}

func (r CollectionCreateRequest) validate() error {
	if strings.TrimSpace(r.PayCustomerID) == "" || strings.TrimSpace(r.PayApplyDate) == "" || strings.TrimSpace(r.PayOrderID) == "" || r.PayAmount <= 0 || strings.TrimSpace(r.PayChannelID) == "" || strings.TrimSpace(r.PayNotifyURL) == "" {
		return fmt.Errorf("collection create requires customer ID, apply date, order ID, positive amount, channel ID, and notify URL")
	}
	if _, err := strconv.ParseInt(r.PayApplyDate, 10, 64); err != nil {
		return fmt.Errorf("pay_apply_date must be a Unix timestamp: %w", err)
	}
	if err := validateCallbackURL(r.PayNotifyURL); err != nil {
		return fmt.Errorf("pay_notify_url: %w", err)
	}
	return nil
}

func (r CollectionQueryRequest) validate() error {
	if strings.TrimSpace(r.PayCustomerID) == "" || strings.TrimSpace(r.PayApplyDate) == "" || strings.TrimSpace(r.PayOrderID) == "" {
		return fmt.Errorf("collection query requires customer ID, apply date, and order ID")
	}
	if _, err := strconv.ParseInt(r.PayApplyDate, 10, 64); err != nil {
		return fmt.Errorf("pay_apply_date must be a Unix timestamp: %w", err)
	}
	return nil
}

func (r PayoutCreateRequest) validate() error {
	if strings.TrimSpace(r.MerchantID) == "" || strings.TrimSpace(r.APIKey) == "" || strings.TrimSpace(r.MerchantPayoutNo) == "" || strings.TrimSpace(r.PayAccountName) == "" || strings.TrimSpace(r.PayCardNo) == "" || strings.TrimSpace(r.PayBankName) == "" {
		return fmt.Errorf("payout create requires merchant ID, API key, payout number, and recipient account fields")
	}
	amount, err := strconv.ParseInt(strings.TrimSpace(r.Amount), 10, 64)
	if err != nil || amount <= 0 {
		return fmt.Errorf("payout amount must be a positive integer")
	}
	if r.CallbackURL != "" {
		if err := validateCallbackURL(r.CallbackURL); err != nil {
			return fmt.Errorf("callback_url: %w", err)
		}
	}
	return nil
}

func validateCallbackURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("must be a public HTTPS URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || host == "api.nnviopp.com" {
		return fmt.Errorf("must not use a local or Production host")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return fmt.Errorf("must not use a private IP address")
	}
	return nil
}

func (r PayoutQueryRequest) validate() error {
	if strings.TrimSpace(r.MerchantID) == "" || strings.TrimSpace(r.APIKey) == "" || (strings.TrimSpace(r.MerchantPayoutNo) == "" && strings.TrimSpace(r.PayoutNo) == "") {
		return fmt.Errorf("payout query requires merchant ID, API key, and merchant_payout_no or payout_no")
	}
	return nil
}

func Sign(identifier, timestamp, nonce, method, path string, rawBody []byte, secret string) string {
	bodyHash := sha256.Sum256(rawBody)
	canonical := strings.Join([]string{identifier, timestamp, nonce, strings.ToUpper(method), path, hex.EncodeToString(bodyHash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

type CollectionCreateRequest struct {
	PayCustomerID  string `json:"pay_customer_id"`
	PayApplyDate   string `json:"pay_apply_date"`
	PayOrderID     string `json:"pay_order_id"`
	PayAmount      int64  `json:"pay_amount"`
	PayChannelID   string `json:"pay_channel_id"`
	PayNotifyURL   string `json:"pay_notify_url"`
	PayProductName string `json:"pay_product_name,omitempty"`
	UserName       string `json:"user_name,omitempty"`
}

type CollectionQueryRequest struct {
	PayCustomerID string `json:"pay_customer_id"`
	PayApplyDate  string `json:"pay_apply_date"`
	PayOrderID    string `json:"pay_order_id"`
}

type PayoutCreateRequest struct {
	MerchantID       string `json:"merchant_id"`
	APIKey           string `json:"api_key"`
	MerchantPayoutNo string `json:"merchant_payout_no"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency,omitempty"`
	CallbackURL      string `json:"callback_url,omitempty"`
	PayAccountName   string `json:"pay_account_name"`
	PayCardNo        string `json:"pay_card_no"`
	PayBankName      string `json:"pay_bank_name"`
}

type PayoutQueryRequest struct {
	MerchantID       string `json:"merchant_id"`
	APIKey           string `json:"api_key"`
	MerchantPayoutNo string `json:"merchant_payout_no,omitempty"`
	PayoutNo         string `json:"payout_no,omitempty"`
}
