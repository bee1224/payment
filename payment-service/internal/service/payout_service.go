package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/domain"
	providerRY "payment-service/internal/provider/ry"
	"payment-service/internal/repository"
)

var ErrMerchantAuthFailed = errors.New("merchant authentication failed")

type PayoutService struct {
	store      repository.PayoutStore
	client     *providerRY.PayoutClient
	httpClient *http.Client
	now        func() time.Time
}

type CreatePayoutOrderRequest struct {
	MerchantID       string `json:"merchant_id"`
	APIKey           string `json:"api_key"`
	MerchantPayoutNo string `json:"merchant_payout_no"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency"`
	CallbackURL      string `json:"callback_url"`
	PayAccountName   string `json:"pay_account_name"`
	PayCardNo        string `json:"pay_card_no"`
	PayBankName      string `json:"pay_bank_name"`
	PaySubBranch     string `json:"pay_sub_branch,omitempty"`
	PaySubBranchCode string `json:"pay_sub_branch_code,omitempty"`
	PayCity          string `json:"pay_city,omitempty"`
	PayValidateID    string `json:"pay_validate_id,omitempty"`
	PayCurrency      string `json:"pay_currency,omitempty"`
}

type QueryPayoutOrderRequest struct {
	MerchantID       string `json:"merchant_id"`
	APIKey           string `json:"api_key"`
	PayoutNo         string `json:"payout_no"`
	MerchantPayoutNo string `json:"merchant_payout_no"`
}

type RejectPayoutOrderRequest struct {
	Reason string `json:"reason"`
}

type CancelPayoutOrderRequest struct {
	Reason string `json:"reason"`
}

type PayoutOrderView struct {
	PayoutNo         string                   `json:"payout_no"`
	MerchantID       string                   `json:"merchant_id"`
	MerchantPayoutNo string                   `json:"merchant_payout_no"`
	ProviderOrderNo  string                   `json:"provider_order_no,omitempty"`
	ProviderTradeNo  string                   `json:"provider_trade_no,omitempty"`
	Status           domain.PayoutOrderStatus `json:"status"`
	Amount           string                   `json:"amount"`
	Fee              string                   `json:"fee"`
	Currency         string                   `json:"currency"`
	CallbackURL      string                   `json:"callback_url,omitempty"`
	FailureMessage   string                   `json:"failure_message,omitempty"`
	SubmittedAt      *time.Time               `json:"submitted_at,omitempty"`
	CompletedAt      *time.Time               `json:"completed_at,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

func NewPayoutService(store repository.PayoutStore, client *providerRY.PayoutClient) *PayoutService {
	return &PayoutService{
		store:      store,
		client:     client,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		now:        time.Now,
	}
}

func (s *PayoutService) CreatePayoutOrder(ctx context.Context, req CreatePayoutOrderRequest) (domain.PayoutOrder, error) {
	merchant, err := s.authenticateMerchant(ctx, req.MerchantID, req.APIKey)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	amountCents, err := parseAmountToCents(req.Amount)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "TWD"
	}
	if strings.TrimSpace(req.MerchantPayoutNo) == "" {
		return domain.PayoutOrder{}, errors.New("merchant_payout_no is required")
	}
	if strings.TrimSpace(req.PayAccountName) == "" || strings.TrimSpace(req.PayCardNo) == "" || strings.TrimSpace(req.PayBankName) == "" {
		return domain.PayoutOrder{}, errors.New("pay_account_name, pay_card_no and pay_bank_name are required")
	}
	req.PayBankName = strings.TrimSpace(req.PayBankName)
	if len(req.PayBankName) != 3 {
		return domain.PayoutOrder{}, errors.New("pay_bank_name must be a 3-digit bank code")
	}
	if !providerRY.IsSupportedPayoutBankCode(req.PayBankName) {
		return domain.PayoutOrder{}, errors.New("pay_bank_name is not in RY supported bank code whitelist")
	}
	callbackURL := strings.TrimSpace(req.CallbackURL)
	if callbackURL == "" {
		callbackURL = merchant.CallbackURL
	}
	order := domain.PayoutOrder{
		MerchantCode:     merchant.Code,
		PayoutNo:         buildPayoutNo(req.MerchantPayoutNo),
		MerchantPayoutNo: strings.TrimSpace(req.MerchantPayoutNo),
		Provider:         "ry",
		AmountCents:      amountCents,
		FeeCents:         0,
		TotalDebitCents:  amountCents,
		Currency:         currency,
		Status:           domain.PayoutOrderStatusPendingReview,
		CallbackURL:      callbackURL,
	}
	beneficiary := domain.PayoutBeneficiary{
		PayAccountName:   strings.TrimSpace(req.PayAccountName),
		PayCardNo:        strings.TrimSpace(req.PayCardNo),
		PayBankName:      req.PayBankName,
		PaySubBranch:     strings.TrimSpace(req.PaySubBranch),
		PaySubBranchCode: strings.TrimSpace(req.PaySubBranchCode),
		PayCity:          strings.TrimSpace(req.PayCity),
		PayValidateID:    strings.TrimSpace(req.PayValidateID),
		PayCurrency:      strings.TrimSpace(req.PayCurrency),
	}
	created, err := s.store.CreatePayoutOrder(ctx, order, beneficiary)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return created, nil
}

func (s *PayoutService) GetPayoutOrder(ctx context.Context, req QueryPayoutOrderRequest) (domain.PayoutOrder, error) {
	if _, err := s.authenticateMerchant(ctx, req.MerchantID, req.APIKey); err != nil {
		return domain.PayoutOrder{}, err
	}
	var (
		order domain.PayoutOrder
		err   error
	)
	switch {
	case strings.TrimSpace(req.PayoutNo) != "":
		order, err = s.store.FindPayoutOrderByPayoutNo(ctx, strings.TrimSpace(req.PayoutNo))
	case strings.TrimSpace(req.MerchantPayoutNo) != "":
		order, err = s.store.FindPayoutOrderByMerchantPayoutNo(ctx, strings.TrimSpace(req.MerchantID), strings.TrimSpace(req.MerchantPayoutNo))
	default:
		return domain.PayoutOrder{}, errors.New("payout_no or merchant_payout_no is required")
	}
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if order.Status == domain.PayoutOrderStatusApproved || order.Status == domain.PayoutOrderStatusSubmitting || order.Status == domain.PayoutOrderStatusProcessing {
		refreshed, refreshErr := s.refreshPayoutStatus(ctx, order)
		if refreshErr == nil {
			order = refreshed
		}
	}
	return order, nil
}

func (s *PayoutService) ApprovePayoutOrder(ctx context.Context, payoutNo string) (domain.PayoutOrder, error) {
	order, err := s.store.ApprovePayoutOrder(ctx, payoutNo)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return s.dispatchApprovedPayout(ctx, order)
}

func (s *PayoutService) RejectPayoutOrder(ctx context.Context, payoutNo, reason string) (domain.PayoutOrder, error) {
	return s.store.RejectPayoutOrder(ctx, payoutNo, reason)
}

func (s *PayoutService) CancelPayoutOrder(ctx context.Context, payoutNo, reason string) (domain.PayoutOrder, error) {
	return s.store.CancelPayoutOrder(ctx, payoutNo, reason)
}

func (s *PayoutService) HandleRYCallback(ctx context.Context, req providerRY.PayoutCallbackRequest) (domain.PayoutOrder, bool, error) {
	result := repository.PayoutProviderResult{
		ProviderCode:     "ry",
		MerchantPayoutNo: strings.TrimSpace(req.OrderID),
		ProviderOrderNo:  strings.TrimSpace(req.TransactionID),
		ProviderTradeNo:  strings.TrimSpace(req.TransactionID),
		EventKey:         payoutEventKey(req),
		Payload:          mustJSON(req),
		StatusCode:       strings.TrimSpace(req.TransactionCode),
		StatusMessage:    strings.TrimSpace(req.TransactionMsg),
		CompletedAt:      s.now(),
	}
	order, changed, err := s.store.ApplyPayoutResult(ctx, result)
	if err != nil {
		return domain.PayoutOrder{}, false, err
	}
	if changed && order.Status.IsTerminal() {
		_ = s.enqueueMerchantCallback(ctx, order)
	}
	return order, changed, nil
}

func (s *PayoutService) ReconcilePendingPayouts(ctx context.Context, limit int) error {
	orders, err := s.store.ListPayoutsForReconcile(ctx, []domain.PayoutOrderStatus{
		domain.PayoutOrderStatusApproved,
		domain.PayoutOrderStatusSubmitting,
		domain.PayoutOrderStatusProcessing,
	}, s.now().Add(-5*time.Second), limit)
	if err != nil {
		return err
	}
	for _, order := range orders {
		if order.Status == domain.PayoutOrderStatusApproved {
			if _, err := s.dispatchApprovedPayout(ctx, order); err != nil {
				continue
			}
			continue
		}
		if _, err := s.refreshPayoutStatus(ctx, order); err != nil {
			continue
		}
	}
	return nil
}

func (s *PayoutService) RetryMerchantCallbacks(ctx context.Context, limit int) error {
	tasks, err := s.store.ListDueMerchantPayoutCallbackTasks(ctx, s.now(), limit)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, task.CallbackURL, bytes.NewReader([]byte(task.Payload)))
		if err != nil {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, false, nextRetryTime(task.RetryCount), err.Error())
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, false, nextRetryTime(task.RetryCount), err.Error())
			continue
		}
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		success := resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.EqualFold(strings.TrimSpace(body.String()), "OK")
		if success {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, true, time.Time{}, "")
			continue
		}
		_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, false, nextRetryTime(task.RetryCount), fmt.Sprintf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(body.String())))
	}
	return nil
}

func (s *PayoutService) dispatchApprovedPayout(ctx context.Context, order domain.PayoutOrder) (domain.PayoutOrder, error) {
	requestPayload := providerRY.CreatePayoutRequest{
		PayOrderID:       order.MerchantPayoutNo,
		PayNotifyURL:     s.client.NotifyURL(),
		PayAmount:        formatDecimal(order.AmountCents),
		PayAccountName:   order.PayAccountName,
		PayCardNo:        order.PayCardNo,
		PayBankName:      order.PayBankName,
		PaySubBranch:     order.PaySubBranch,
		PaySubBranchCode: order.PaySubBranchCode,
		PayCity:          order.PayCity,
		PayValidateID:    order.PayValidateID,
		PayCurrency:      order.PayCurrency,
	}
	requestJSON := mustJSON(requestPayload)
	result, err := s.client.CreatePayout(ctx, requestPayload)
	if err != nil {
		attempt := domain.PayoutTransaction{
			Status:         "failed",
			ErrorMessage:   err.Error(),
			RequestPayload: requestJSON,
		}
		retryable := !strings.Contains(err.Error(), "required") && !strings.Contains(err.Error(), "must be") && !strings.Contains(err.Error(), "does not match")
		return s.store.MarkPayoutSubmissionFailure(ctx, order.PayoutNo, attempt, retryable)
	}
	attempt := domain.PayoutTransaction{
		Status:          "submitted",
		ProviderOrderNo: responseTransactionID(result),
		ProviderTradeNo: responseTransactionID(result),
		RequestPayload:  requestJSON,
		ResponsePayload: mustJSON(result),
	}
	return s.store.MarkPayoutSubmitted(ctx, order.PayoutNo, attempt)
}

func (s *PayoutService) refreshPayoutStatus(ctx context.Context, order domain.PayoutOrder) (domain.PayoutOrder, error) {
	result, err := s.client.QueryPayout(ctx, providerRY.QueryPayoutRequest{
		PayOrderID: []string{order.MerchantPayoutNo},
	})
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if result.Data == nil {
		return order, nil
	}
	statusCode := "40000"
	statusMessage := result.Data.StatusName
	switch result.Data.Status {
	case 1, 2:
		statusCode = "30000"
	case 3, 4, 5:
		statusCode = "40000"
	default:
		return order, nil
	}
	updated, changed, err := s.store.ApplyPayoutResult(ctx, repository.PayoutProviderResult{
		ProviderCode:     "ry",
		MerchantPayoutNo: order.MerchantPayoutNo,
		ProviderOrderNo:  strings.TrimSpace(result.Data.PaymentID),
		ProviderTradeNo:  strings.TrimSpace(result.Data.PaymentID),
		EventKey:         "query:" + order.MerchantPayoutNo + ":" + strconv.Itoa(result.Data.Status) + ":" + result.Data.PaymentSuccessTime,
		Payload:          mustJSON(result),
		StatusCode:       statusCode,
		StatusMessage:    statusMessage,
		CompletedAt:      parseOptionalTime(result.Data.PaymentSuccessTime, s.now()),
	})
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if changed && updated.Status.IsTerminal() {
		_ = s.enqueueMerchantCallback(ctx, updated)
	}
	return updated, nil
}

func (s *PayoutService) enqueueMerchantCallback(ctx context.Context, order domain.PayoutOrder) error {
	if strings.TrimSpace(order.CallbackURL) == "" {
		return nil
	}
	merchant, err := s.store.FindMerchantByCode(ctx, order.MerchantCode)
	if err != nil {
		return err
	}
	payloadMap := map[string]any{
		"merchant_id":        merchant.Code,
		"merchant_payout_no": order.MerchantPayoutNo,
		"payout_no":          order.PayoutNo,
		"provider_order_no":  order.ProviderOrderNo,
		"status":             order.Status,
		"amount":             formatDecimal(order.AmountCents),
		"fee":                formatDecimal(order.FeeCents),
		"currency":           order.Currency,
		"completed_at":       formatTimePointer(order.CompletedAt),
		"sign":               "",
	}
	if strings.TrimSpace(merchant.APIKey) != "" {
		sign, signErr := providerRY.Sign(map[string]any{
			"merchant_id":        merchant.Code,
			"merchant_payout_no": order.MerchantPayoutNo,
			"payout_no":          order.PayoutNo,
			"provider_order_no":  order.ProviderOrderNo,
			"status":             string(order.Status),
			"amount":             formatDecimal(order.AmountCents),
			"fee":                formatDecimal(order.FeeCents),
			"currency":           order.Currency,
			"completed_at":       formatTimePointer(order.CompletedAt),
		}, merchant.APIKey)
		if signErr == nil {
			payloadMap["sign"] = sign
		}
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return err
	}
	task := domain.MerchantPayoutCallbackTask{
		MerchantID:    order.MerchantID,
		PayoutOrderID: order.ID,
		CallbackURL:   order.CallbackURL,
		Payload:       string(payload),
		NextRetryAt:   s.now(),
	}
	if err := s.store.CreateMerchantPayoutCallbackTask(ctx, task); err != nil {
		return err
	}
	return s.RetryMerchantCallbacks(ctx, 1)
}

func (s *PayoutService) authenticateMerchant(ctx context.Context, merchantCode, apiKey string) (domain.Merchant, error) {
	merchantCode = strings.TrimSpace(merchantCode)
	if merchantCode == "" {
		return domain.Merchant{}, errors.New("merchant_id is required")
	}
	merchant, err := s.store.FindMerchantByCode(ctx, merchantCode)
	if err != nil {
		return domain.Merchant{}, err
	}
	if strings.ToLower(merchant.Status) == "disabled" {
		return domain.Merchant{}, ErrMerchantAuthFailed
	}
	secret := strings.TrimSpace(merchant.APIKey)
	if secret == "" {
		return domain.Merchant{}, ErrMerchantAuthFailed
	}
	if apiKey == secret {
		return merchant, nil
	}
	sum := sha256.Sum256([]byte(apiKey))
	if strings.EqualFold(secret, hex.EncodeToString(sum[:])) {
		return merchant, nil
	}
	return domain.Merchant{}, ErrMerchantAuthFailed
}

func buildPayoutNo(merchantPayoutNo string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, merchantPayoutNo)
	if clean == "" {
		clean = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if len(clean) > 14 {
		clean = clean[len(clean)-14:]
	}
	return "W" + clean + time.Now().Format("150405")
}

func parseAmountToCents(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("amount is required")
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		return 0, errors.New("amount must be greater than zero")
	}
	parts := strings.SplitN(value, ".", 3)
	if len(parts) > 2 {
		return 0, errors.New("invalid amount")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("invalid amount")
	}
	var cents int64
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) > 2 {
			fraction = fraction[:2]
		}
		for len(fraction) < 2 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, errors.New("invalid amount")
		}
	}
	total := whole*100 + cents
	if total <= 0 {
		return 0, errors.New("amount must be greater than zero")
	}
	return total, nil
}

func formatDecimal(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func formatTimePointer(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func parseOptionalTime(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
	if err != nil {
		return fallback
	}
	return parsed
}

func nextRetryTime(retryCount int) time.Time {
	delay := time.Duration(1<<minInt(retryCount, 5)) * time.Minute
	return time.Now().Add(delay)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func responseTransactionID(result providerRY.CreatePayoutResponse) string {
	if result.Data == nil {
		return ""
	}
	return strings.TrimSpace(result.Data.TransactionID)
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func payoutEventKey(req providerRY.PayoutCallbackRequest) string {
	return strings.Join([]string{
		fmt.Sprintf("%v", req.CustomerID),
		strings.TrimSpace(req.OrderID),
		strings.TrimSpace(req.TransactionID),
		strings.TrimSpace(req.TransactionCode),
		strings.TrimSpace(req.DateTime),
	}, "|")
}

func BuildPayoutOrderView(order domain.PayoutOrder) PayoutOrderView {
	return PayoutOrderView{
		PayoutNo:         order.PayoutNo,
		MerchantID:       order.MerchantCode,
		MerchantPayoutNo: order.MerchantPayoutNo,
		ProviderOrderNo:  order.ProviderOrderNo,
		ProviderTradeNo:  order.ProviderTradeNo,
		Status:           order.Status,
		Amount:           formatDecimal(order.AmountCents),
		Fee:              formatDecimal(order.FeeCents),
		Currency:         order.Currency,
		CallbackURL:      order.CallbackURL,
		FailureMessage:   order.FailureMessage,
		SubmittedAt:      order.SubmittedAt,
		CompletedAt:      order.CompletedAt,
		CreatedAt:        order.CreatedAt,
		UpdatedAt:        order.UpdatedAt,
	}
}
