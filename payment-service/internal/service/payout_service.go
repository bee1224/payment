package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/domain"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
)

var ErrMerchantAuthFailed = errors.New("merchant authentication failed")

type PayoutService struct {
	store               repository.PayoutStore
	client              *providerGateway.PayoutClient
	httpClient          *http.Client
	merchantSecrets     map[string]string
	callbackSigningKeys repository.CallbackSigningKeyResolver
	authMaxSkew         time.Duration
	nonceStore          repository.ReplayNonceStore
	now                 func() time.Time
	reconciliation      *ReconciliationService
	alertNotifier       PayoutAlertNotifier
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

type RotateMerchantAPIKeyRequest struct {
	APIKey            string `json:"api_key"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	PreviousExpiresAt string `json:"previous_expires_at,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type RevokeMerchantAPIKeyRequest struct {
	APIKey string `json:"api_key"`
	Reason string `json:"reason,omitempty"`
}

type IssueMerchantAPIKeyRequest struct {
	ExpiresAt         string `json:"expires_at,omitempty"`
	PreviousExpiresAt string `json:"previous_expires_at,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type IssuedMerchantAPIKeyView struct {
	APIKey string               `json:"api_key"`
	Keys   []MerchantAPIKeyView `json:"keys"`
}

type MerchantAPIKeyView struct {
	KeyHash       string     `json:"key_hash"`
	Status        string     `json:"status"`
	IsPrimary     bool       `json:"is_primary"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	LastRotatedAt time.Time  `json:"last_rotated_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type MerchantAPIKeyAuditContext struct {
	Actor        string
	ActorRoles   []string
	Checker      string
	CheckerRoles []string
	RequestID    string
	SourceIP     string
	UserAgent    string
}

type MerchantAPIKeyAuditView struct {
	Action           string         `json:"action"`
	KeyHash          string         `json:"key_hash"`
	Actor            string         `json:"actor"`
	Reason           string         `json:"reason,omitempty"`
	RequestID        string         `json:"request_id,omitempty"`
	SourceIP         string         `json:"source_ip,omitempty"`
	UserAgent        string         `json:"user_agent,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	MerchantAPIKeyID *int64         `json:"merchant_api_key_id,omitempty"`
}

type PayoutReviewAuditContext struct {
	Actor        string
	ActorRoles   []string
	Checker      string
	CheckerRoles []string
	Reason       string
	RequestID    string
	SourceIP     string
	UserAgent    string
}

type PayoutReviewAuditView struct {
	Action        string         `json:"action"`
	Actor         string         `json:"actor"`
	Reason        string         `json:"reason,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	SourceIP      string         `json:"source_ip,omitempty"`
	UserAgent     string         `json:"user_agent,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	MerchantID    int64          `json:"merchant_id"`
	PayoutOrderID int64          `json:"payout_order_id"`
}

type PayoutOperationalAlertView struct {
	ID              int64      `json:"id"`
	MerchantID      int64      `json:"merchant_id"`
	PayoutOrderID   int64      `json:"payout_order_id"`
	PayoutNo        string     `json:"payout_no"`
	Category        string     `json:"category"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	Summary         string     `json:"summary"`
	Details         string     `json:"details,omitempty"`
	OccurrenceCount int        `json:"occurrence_count"`
	FirstOccurredAt time.Time  `json:"first_occurred_at"`
	LastOccurredAt  time.Time  `json:"last_occurred_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy      string     `json:"resolved_by,omitempty"`
	ResolveReason   string     `json:"resolve_reason,omitempty"`
}

type PayoutSettlementCurrencyView struct {
	Currency                    string `json:"currency"`
	MerchantAvailableCents      int64  `json:"merchant_available_cents"`
	MerchantPendingCents        int64  `json:"merchant_pending_cents"`
	PendingReviewCents          int64  `json:"pending_review_cents"`
	ApprovedCents               int64  `json:"approved_cents"`
	SubmittingCents             int64  `json:"submitting_cents"`
	ProcessingCents             int64  `json:"processing_cents"`
	CompletedCents              int64  `json:"completed_cents"`
	FailedCents                 int64  `json:"failed_cents"`
	CancelledOrRejectedCents    int64  `json:"cancelled_or_rejected_cents"`
	ReversedCents               int64  `json:"reversed_cents"`
	OpenOrderCount              int64  `json:"open_order_count"`
	ProviderInFlightCents       int64  `json:"provider_inflight_cents"`
	InternalManualHoldCents     int64  `json:"internal_manual_hold_cents"`
	InternalTotalUnsettledCents int64  `json:"internal_total_unsettled_cents"`
	ProviderBalanceCents        int64  `json:"provider_balance_cents"`
	ProviderAvailableCents      int64  `json:"provider_available_cents"`
	ProviderUnsettlementCents   int64  `json:"provider_unsettlement_cents"`
	ProviderVsInflightGapCents  int64  `json:"provider_vs_inflight_gap_cents"`
	MerchantPendingGapCents     int64  `json:"merchant_pending_gap_cents"`
}

type PayoutSettlementReportView struct {
	GeneratedAt time.Time                      `json:"generated_at"`
	CustomerID  string                         `json:"customer_id"`
	Currencies  []PayoutSettlementCurrencyView `json:"currencies"`
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

// AdminPayoutListRequest is intentionally limited to non-sensitive list data.
type AdminPayoutListRequest struct {
	Page, PageSize         int
	Status, Query          string
	CreatedFrom, CreatedTo *time.Time
}

type AdminPayoutListResult struct {
	Items    []PayoutOrderView `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int               `json:"total"`
}

func NewPayoutService(store repository.PayoutStore, client *providerGateway.PayoutClient) *PayoutService {
	return NewPayoutServiceWithSecrets(store, client, nil)
}

func NewPayoutServiceWithSecrets(store repository.PayoutStore, client *providerGateway.PayoutClient, merchantSecrets map[string]string) *PayoutService {
	clonedSecrets := make(map[string]string, len(merchantSecrets))
	for merchantCode, secret := range merchantSecrets {
		merchantCode = strings.TrimSpace(merchantCode)
		secret = strings.TrimSpace(secret)
		if merchantCode == "" || secret == "" {
			continue
		}
		clonedSecrets[merchantCode] = secret
	}
	return &PayoutService{
		store:           store,
		client:          client,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		merchantSecrets: clonedSecrets,
		authMaxSkew:     5 * time.Minute,
		nonceStore:      repository.NewInMemoryReplayNonceStore(),
		now:             time.Now,
	}
}

func (s *PayoutService) SetMerchantAuthMaxSkew(skew time.Duration) {
	if skew > 0 {
		s.authMaxSkew = skew
	}
}

func (s *PayoutService) SetCallbackSigningKeyResolver(resolver repository.CallbackSigningKeyResolver) {
	s.callbackSigningKeys = resolver
}

func (s *PayoutService) SetReplayNonceStore(store repository.ReplayNonceStore) {
	if store != nil {
		s.nonceStore = store
	}
}

func (s *PayoutService) SetReconciliationService(reconciliation *ReconciliationService) {
	s.reconciliation = reconciliation
}

func (s *PayoutService) SetAlertNotifier(notifier PayoutAlertNotifier) { s.alertNotifier = notifier }

func (s *PayoutService) RunReconciliation(ctx context.Context, req RunReconciliationRequest) (ReconciliationReportView, error) {
	if s.reconciliation == nil {
		return ReconciliationReportView{}, errors.New("reconciliation service is not configured")
	}
	return s.reconciliation.RunReconciliation(ctx, req)
}

func (s *PayoutService) GetReconciliationReport(ctx context.Context, runID int64) (ReconciliationReportView, error) {
	if s.reconciliation == nil {
		return ReconciliationReportView{}, errors.New("reconciliation service is not configured")
	}
	return s.reconciliation.GetReconciliationReport(ctx, runID)
}

func (s *PayoutService) ListReconciliationReports(ctx context.Context, req ListReconciliationReportsRequest) ([]ReconciliationReportView, error) {
	if s.reconciliation == nil {
		return nil, errors.New("reconciliation service is not configured")
	}
	return s.reconciliation.ListReconciliationReports(ctx, req)
}

func (s *PayoutService) GetReconciliationTrace(ctx context.Context, query domain.ReconciliationTraceQuery) (domain.ReconciliationTrace, error) {
	if s.reconciliation == nil {
		return domain.ReconciliationTrace{}, errors.New("reconciliation service is not configured")
	}
	return s.reconciliation.GetReconciliationTrace(ctx, query)
}

func (s *PayoutService) ResolveReconciliationMismatchWithAdjustment(ctx context.Context, itemID int64, req ResolveReconciliationAdjustmentRequest, audit PayoutReviewAuditContext) (ReconciliationMismatchView, error) {
	if s.reconciliation == nil {
		return ReconciliationMismatchView{}, errors.New("reconciliation service is not configured")
	}
	return s.reconciliation.ResolveMismatchWithAdjustment(ctx, itemID, req, audit)
}

func (s *PayoutService) ResolveReconciliationMismatchWithReversal(ctx context.Context, itemID int64, req ResolveReconciliationReversalRequest, audit PayoutReviewAuditContext) (ReconciliationMismatchView, error) {
	if s.reconciliation == nil {
		return ReconciliationMismatchView{}, errors.New("reconciliation service is not configured")
	}
	return s.reconciliation.ResolveMismatchWithReversal(ctx, itemID, req, audit)
}

func (s *PayoutService) CreatePayoutOrder(ctx context.Context, req CreatePayoutOrderRequest) (domain.PayoutOrder, error) {
	merchant, err := s.authenticateMerchant(ctx, req.MerchantID, req.APIKey)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return s.CreatePayoutOrderForMerchant(ctx, merchant, req)
}

func (s *PayoutService) CreatePayoutOrderForMerchant(ctx context.Context, merchant domain.Merchant, req CreatePayoutOrderRequest) (domain.PayoutOrder, error) {
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
	if !providerGateway.IsSupportedPayoutBankCode(req.PayBankName) {
		return domain.PayoutOrder{}, errors.New("pay_bank_name is not in gateway supported bank code whitelist")
	}
	callbackURL := strings.TrimSpace(req.CallbackURL)
	if callbackURL == "" {
		callbackURL = merchant.CallbackURL
	}
	if callbackURL != "" {
		if err := validatePayoutCallbackURL(callbackURL); err != nil {
			return domain.PayoutOrder{}, err
		}
	}
	order := domain.PayoutOrder{
		MerchantCode:     merchant.Code,
		PayoutNo:         buildPayoutNo(req.MerchantPayoutNo),
		MerchantPayoutNo: strings.TrimSpace(req.MerchantPayoutNo),
		Provider:         "gateway",
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
	merchant, err := s.authenticateMerchant(ctx, req.MerchantID, req.APIKey)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return s.GetPayoutOrderForMerchant(ctx, merchant, req)
}

// GetPayoutOrderForAdmin is only wired to the authenticated internal console.
// It must never be exposed through the merchant routes because it bypasses
// merchant ownership checks.
func (s *PayoutService) GetPayoutOrderForAdmin(ctx context.Context, payoutNo string) (domain.PayoutOrder, error) {
	return s.store.FindPayoutOrderByPayoutNo(ctx, strings.TrimSpace(payoutNo))
}

// ListPayoutOrdersForAdmin is deliberately restricted to the administration
// surface. Merchant-facing APIs continue to use their own authenticated query.
func (s *PayoutService) ListPayoutOrdersForAdmin(ctx context.Context, limit int) ([]PayoutOrderView, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	orders, err := s.store.ListPayoutsForReconcile(ctx, []domain.PayoutOrderStatus{
		domain.PayoutOrderStatusPendingReview, domain.PayoutOrderStatusApproved,
		domain.PayoutOrderStatusSubmitting, domain.PayoutOrderStatusProcessing,
		domain.PayoutOrderStatusCompleted, domain.PayoutOrderStatusFailed,
		domain.PayoutOrderStatusRejected, domain.PayoutOrderStatusCancelled,
		domain.PayoutOrderStatus("PROCESSING"), domain.PayoutOrderStatus("PAID_PENDING_REVIEW"),
		domain.PayoutOrderStatus("SUCCESS"), domain.PayoutOrderStatus("FAILED"), domain.PayoutOrderStatus("CANCELLED"),
	}, time.Now().AddDate(100, 0, 0), limit)
	if err != nil {
		return nil, err
	}
	views := make([]PayoutOrderView, 0, len(orders))
	for _, order := range orders {
		views = append(views, BuildPayoutOrderView(order))
	}
	return views, nil
}

func (s *PayoutService) ListPayoutOrdersForAdminPage(ctx context.Context, req AdminPayoutListRequest) (AdminPayoutListResult, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	orders, err := s.ListPayoutOrdersForAdmin(ctx, 200)
	if err != nil {
		return AdminPayoutListResult{}, err
	}
	query, status := strings.ToLower(strings.TrimSpace(req.Query)), strings.ToLower(strings.TrimSpace(req.Status))
	filtered := make([]PayoutOrderView, 0, len(orders))
	for _, order := range orders {
		if status != "" && strings.ToLower(string(order.Status)) != status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(order.PayoutNo+" "+order.MerchantPayoutNo+" "+order.MerchantID), query) {
			continue
		}
		if req.CreatedFrom != nil && order.CreatedAt.Before(*req.CreatedFrom) {
			continue
		}
		if req.CreatedTo != nil && order.CreatedAt.After(*req.CreatedTo) {
			continue
		}
		order.CallbackURL, order.FailureMessage = "", ""
		filtered = append(filtered, order)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })
	result := AdminPayoutListResult{Page: req.Page, PageSize: req.PageSize, Total: len(filtered)}
	start := (req.Page - 1) * req.PageSize
	if start >= len(filtered) {
		result.Items = []PayoutOrderView{}
		return result, nil
	}
	end := start + req.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	result.Items = filtered[start:end]
	return result, nil
}

func (s *PayoutService) GetPayoutOrderForMerchant(ctx context.Context, merchant domain.Merchant, req QueryPayoutOrderRequest) (domain.PayoutOrder, error) {
	var (
		order domain.PayoutOrder
		err   error
	)
	switch {
	case strings.TrimSpace(req.PayoutNo) != "":
		order, err = s.store.FindPayoutOrderByPayoutNo(ctx, strings.TrimSpace(req.PayoutNo))
	case strings.TrimSpace(req.MerchantPayoutNo) != "":
		order, err = s.store.FindPayoutOrderByMerchantPayoutNo(ctx, strings.TrimSpace(merchant.Code), strings.TrimSpace(req.MerchantPayoutNo))
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

func (s *PayoutService) ApprovePayoutOrder(ctx context.Context, payoutNo string, auditCtx PayoutReviewAuditContext) (domain.PayoutOrder, error) {
	order, err := s.store.ApprovePayoutOrder(ctx, payoutNo, buildPayoutReviewAuditLog("approve", auditCtx, map[string]any{
		"status_after": string(domain.PayoutOrderStatusApproved),
	}))
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return order, nil
}

func (s *PayoutService) RejectPayoutOrder(ctx context.Context, payoutNo, reason string, auditCtx PayoutReviewAuditContext) (domain.PayoutOrder, error) {
	return s.store.RejectPayoutOrder(ctx, payoutNo, reason, buildPayoutReviewAuditLog("reject", auditCtx, map[string]any{
		"status_after": string(domain.PayoutOrderStatusRejected),
	}))
}

func (s *PayoutService) CancelPayoutOrder(ctx context.Context, payoutNo, reason string, auditCtx PayoutReviewAuditContext) (domain.PayoutOrder, error) {
	return s.store.CancelPayoutOrder(ctx, payoutNo, reason, buildPayoutReviewAuditLog("cancel", auditCtx, map[string]any{
		"status_after": string(domain.PayoutOrderStatusCancelled),
	}))
}

func (s *PayoutService) ResendMerchantCallback(ctx context.Context, payoutNo string, auditCtx PayoutReviewAuditContext) (domain.PayoutOrder, error) {
	order, err := s.store.FindPayoutOrderByPayoutNo(ctx, strings.TrimSpace(payoutNo))
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if !order.Status.IsTerminal() {
		return domain.PayoutOrder{}, errors.New("payout must be terminal before callback resend")
	}
	if strings.TrimSpace(order.CallbackURL) == "" {
		return domain.PayoutOrder{}, errors.New("callback_url is required for resend")
	}
	if err := validatePayoutCallbackURL(order.CallbackURL); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := s.enqueueMerchantCallback(ctx, order); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := s.store.CreatePayoutReviewAuditLog(ctx, payoutNo, buildPayoutReviewAuditLog("resend_callback", auditCtx, map[string]any{
		"status":       string(order.Status),
		"callback_url": order.CallbackURL,
	})); err != nil {
		return domain.PayoutOrder{}, err
	}
	return order, nil
}

func (s *PayoutService) ListMerchantAPIKeys(ctx context.Context, merchantCode string) ([]MerchantAPIKeyView, error) {
	records, err := s.store.ListMerchantAPIKeys(ctx, strings.TrimSpace(merchantCode))
	if err != nil {
		return nil, err
	}
	return buildMerchantAPIKeyViews(records), nil
}

func (s *PayoutService) ListMerchantAPIKeyAuditLogs(ctx context.Context, merchantCode string, limit int) ([]MerchantAPIKeyAuditView, error) {
	entries, err := s.store.ListMerchantAPIKeyAuditLogs(ctx, strings.TrimSpace(merchantCode), limit)
	if err != nil {
		return nil, err
	}
	return buildMerchantAPIKeyAuditViews(entries), nil
}

func (s *PayoutService) ListPayoutReviewAuditLogs(ctx context.Context, payoutNo string, limit int) ([]PayoutReviewAuditView, error) {
	entries, err := s.store.ListPayoutReviewAuditLogs(ctx, strings.TrimSpace(payoutNo), limit)
	if err != nil {
		return nil, err
	}
	return buildPayoutReviewAuditViews(entries), nil
}

func (s *PayoutService) ListPayoutOperationalAlerts(ctx context.Context, status string, limit int) ([]PayoutOperationalAlertView, error) {
	alerts, err := s.store.ListPayoutOperationalAlerts(ctx, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	return buildPayoutOperationalAlertViews(alerts), nil
}

func (s *PayoutService) ResolvePayoutOperationalAlert(ctx context.Context, alertID int64, auditCtx PayoutReviewAuditContext) error {
	if alertID == 0 {
		return errors.New("alert_id is required")
	}
	if strings.TrimSpace(auditCtx.Actor) == "" {
		return errors.New("actor is required")
	}
	if strings.TrimSpace(auditCtx.Reason) == "" {
		return errors.New("review reason is required")
	}
	return s.store.ResolvePayoutOperationalAlert(ctx, alertID, repository.PayoutOperationalAlertResolve{
		ResolvedBy:    strings.TrimSpace(auditCtx.Actor),
		ResolveReason: strings.TrimSpace(auditCtx.Reason),
	})
}

func (s *PayoutService) RotateMerchantAPIKey(ctx context.Context, merchantCode string, req RotateMerchantAPIKeyRequest, auditCtx MerchantAPIKeyAuditContext) ([]MerchantAPIKeyView, error) {
	expiresAt, err := parseOptionalRFC3339Time(req.ExpiresAt)
	if err != nil {
		return nil, err
	}
	previousExpiresAt, err := parseOptionalRFC3339Time(req.PreviousExpiresAt)
	if err != nil {
		return nil, err
	}
	records, err := s.store.RotateMerchantAPIKey(
		ctx,
		strings.TrimSpace(merchantCode),
		req.APIKey,
		expiresAt,
		previousExpiresAt,
		buildMerchantAPIKeyAuditLog(req.APIKey, req.Reason, expiresAt, previousExpiresAt, auditCtx),
	)
	if err != nil {
		return nil, err
	}
	s.setMerchantSigningSecret(merchantCode, req.APIKey)
	return buildMerchantAPIKeyViews(records), nil
}

func (s *PayoutService) IssueMerchantAPIKey(ctx context.Context, merchantCode string, req IssueMerchantAPIKeyRequest, auditCtx MerchantAPIKeyAuditContext) (IssuedMerchantAPIKeyView, error) {
	expiresAt, err := parseOptionalRFC3339Time(req.ExpiresAt)
	if err != nil {
		return IssuedMerchantAPIKeyView{}, err
	}
	previousExpiresAt, err := parseOptionalRFC3339Time(req.PreviousExpiresAt)
	if err != nil {
		return IssuedMerchantAPIKeyView{}, err
	}
	apiKey, err := repository.GenerateMerchantAPIKey()
	if err != nil {
		return IssuedMerchantAPIKeyView{}, err
	}
	records, err := s.store.IssueMerchantAPIKey(
		ctx,
		strings.TrimSpace(merchantCode),
		apiKey,
		expiresAt,
		previousExpiresAt,
		buildMerchantAPIKeyAuditLog(apiKey, req.Reason, expiresAt, previousExpiresAt, auditCtx),
	)
	if err != nil {
		return IssuedMerchantAPIKeyView{}, err
	}
	s.setMerchantSigningSecret(merchantCode, apiKey)
	return IssuedMerchantAPIKeyView{
		APIKey: apiKey,
		Keys:   buildMerchantAPIKeyViews(records),
	}, nil
}

func (s *PayoutService) RevokeMerchantAPIKey(ctx context.Context, merchantCode string, req RevokeMerchantAPIKeyRequest, auditCtx MerchantAPIKeyAuditContext) ([]MerchantAPIKeyView, error) {
	records, err := s.store.RevokeMerchantAPIKey(
		ctx,
		strings.TrimSpace(merchantCode),
		req.APIKey,
		buildMerchantAPIKeyAuditLog(req.APIKey, req.Reason, nil, nil, auditCtx),
	)
	if err != nil {
		return nil, err
	}
	s.clearMerchantSigningSecretIfMatch(merchantCode, req.APIKey)
	return buildMerchantAPIKeyViews(records), nil
}

func (s *PayoutService) ResolveMerchantCallbackSigningKey(ctx context.Context, merchantCode string) (repository.CallbackSigningKey, error) {
	if s.callbackSigningKeys == nil {
		return repository.CallbackSigningKey{}, errors.New("merchant callback signing key resolver is unavailable")
	}
	merchant, err := s.store.FindMerchantByCode(ctx, strings.TrimSpace(merchantCode))
	if err != nil {
		return repository.CallbackSigningKey{}, err
	}
	return s.callbackSigningKeys.ResolveCurrentCallbackSigningKey(ctx, merchant.ID)
}

func (s *PayoutService) HandleGatewayCallback(ctx context.Context, req providerGateway.PayoutCallbackRequest) (domain.PayoutOrder, bool, error) {
	result := repository.PayoutProviderResult{
		ProviderCode:     "gateway",
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
				_ = s.raisePayoutOperationalAlert(ctx, order, "dispatch_failed", "critical", "approved payout dispatch failed", err.Error())
				continue
			}
			continue
		}
		if _, err := s.refreshPayoutStatus(ctx, order); err != nil {
			_ = s.raisePayoutOperationalAlert(ctx, order, "reconcile_failed", "warning", "payout reconcile query failed", err.Error())
			continue
		}
		if s.now().Sub(order.UpdatedAt) >= 10*time.Minute {
			_ = s.raisePayoutOperationalAlert(ctx, order, "reconcile_stuck", "warning", "payout remains in non-terminal status beyond reconcile threshold", fmt.Sprintf("status=%s updated_at=%s", order.Status, order.UpdatedAt.Format(time.RFC3339)))
			continue
		}
	}
	return nil
}

func (s *PayoutService) RetryMerchantCallbacks(ctx context.Context, limit int) error {
	now := s.now()
	tasks, err := s.store.ClaimDueMerchantPayoutCallbackTasks(ctx, now, now.Add(-2*time.Minute), limit)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if s.callbackSigningKeys == nil {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, task.ClaimToken, false, nextRetryTime(task.RetryCount), "callback_signing_key_unavailable")
			s.raisePayoutAlertFromTask(ctx, task, "merchant_callback_failed", "warning", "merchant payout callback signing key unavailable", "callback_signing_key_unavailable")
			continue
		}
		key, keyErr := s.callbackSigningKeys.ResolveCurrentCallbackSigningKey(ctx, task.MerchantID)
		if keyErr != nil {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, task.ClaimToken, false, nextRetryTime(task.RetryCount), "callback_signing_key_unavailable")
			s.raisePayoutAlertFromTask(ctx, task, "merchant_callback_failed", "warning", "merchant payout callback signing key unavailable", "callback_signing_key_unavailable")
			continue
		}
		headers, headerErr := BuildMerchantCallbackHeaders(key, http.MethodPost, task.CallbackURL, []byte(task.Payload), now)
		if headerErr != nil {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, task.ClaimToken, false, nextRetryTime(task.RetryCount), "callback_signing_header_build_failed")
			s.raisePayoutAlertFromTask(ctx, task, "merchant_callback_failed", "warning", "merchant payout callback signing header failed", "callback_signing_header_build_failed")
			continue
		}
		resp, err := s.postPublicPayoutCallback(ctx, task.CallbackURL, []byte(task.Payload), headers)
		if err != nil {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, task.ClaimToken, false, nextRetryTime(task.RetryCount), err.Error())
			s.raisePayoutAlertFromTask(ctx, task, "merchant_callback_failed", "warning", "merchant payout callback delivery failed", err.Error())
			continue
		}
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		success := isSuccessfulMerchantCallbackResponse(resp.StatusCode, body.Bytes())
		if success {
			_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, task.ClaimToken, true, time.Time{}, "")
			continue
		}
		errMessage := fmt.Sprintf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(body.String()))
		_ = s.store.MarkMerchantPayoutCallbackTaskResult(ctx, task.ID, task.ClaimToken, false, nextRetryTime(task.RetryCount), errMessage)
		s.raisePayoutAlertFromTask(ctx, task, "merchant_callback_failed", "warning", "merchant payout callback delivery failed", errMessage)
	}
	return nil
}

// postPublicPayoutCallback resolves once, rejects non-public IPs, then dials
// that exact address. This prevents DNS rebinding and blocks redirects.
func (s *PayoutService) postPublicPayoutCallback(ctx context.Context, rawURL string, body []byte, headers map[string][]string) (*http.Response, error) {
	return PostPublicHTTPSCallback(ctx, rawURL, body, 10*time.Second, headers)
}

func (s *PayoutService) dispatchApprovedPayout(ctx context.Context, order domain.PayoutOrder) (domain.PayoutOrder, error) {
	requestPayload := providerGateway.CreatePayoutRequest{
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
	requestJSON := redactPayoutRequestJSON(requestPayload)
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
	result, err := s.client.QueryPayout(ctx, providerGateway.QueryPayoutRequest{
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
		ProviderCode:     "gateway",
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
	if err := validatePayoutCallbackURL(order.CallbackURL); err != nil {
		return err
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
		"event":              "payout_result",
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
	valid, err := s.store.ValidateMerchantAPIKey(ctx, merchant.ID, apiKey)
	if err != nil {
		return domain.Merchant{}, err
	}
	if valid {
		return merchant, nil
	}
	return domain.Merchant{}, ErrMerchantAuthFailed
}

func (s *PayoutService) resolveMerchantRequestSigningSecret(ctx context.Context, merchant domain.Merchant) string {
	if secret := strings.TrimSpace(s.merchantSecrets[strings.TrimSpace(merchant.Code)]); secret != "" {
		return secret
	}
	if secret, err := s.store.GetActiveMerchantAPIKeySecret(ctx, merchant.ID); err == nil && strings.TrimSpace(secret) != "" {
		return strings.TrimSpace(secret)
	}
	if secret := strings.TrimSpace(merchant.APISecret); secret != "" {
		return secret
	}
	if repositoryLooksLikeStoredHash(merchant.APIKey) {
		return ""
	}
	return strings.TrimSpace(merchant.APIKey)
}

func (s *PayoutService) setMerchantSigningSecret(merchantCode, apiKey string) {
	merchantCode = strings.TrimSpace(merchantCode)
	apiKey = strings.TrimSpace(apiKey)
	if merchantCode == "" || apiKey == "" {
		return
	}
	if s.merchantSecrets == nil {
		s.merchantSecrets = make(map[string]string)
	}
	s.merchantSecrets[merchantCode] = apiKey
}

func (s *PayoutService) clearMerchantSigningSecretIfMatch(merchantCode, apiKey string) {
	merchantCode = strings.TrimSpace(merchantCode)
	apiKey = strings.TrimSpace(apiKey)
	if merchantCode == "" || apiKey == "" || s.merchantSecrets == nil {
		return
	}
	if strings.TrimSpace(s.merchantSecrets[merchantCode]) == apiKey {
		delete(s.merchantSecrets, merchantCode)
	}
}

func validatePayoutCallbackURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("callback_url must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "https" {
		return errors.New("callback_url must use HTTPS")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return errors.New("callback_url host is required")
	}
	if strings.EqualFold(host, "localhost") {
		return errors.New("callback_url must not target localhost")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
			return errors.New("callback_url must not target a private or loopback address")
		}
	}
	return nil
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
	if strings.Contains(value, ".") {
		return 0, errors.New("amount must be a whole TWD amount")
	}
	whole, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.New("invalid amount")
	}
	if whole > (1<<63-1)/100 {
		return 0, errors.New("amount is too large")
	}
	total := whole * 100
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

func parseOptionalRFC3339Time(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("expires_at must be RFC3339 format")
	}
	return &parsed, nil
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

func responseTransactionID(result providerGateway.CreatePayoutResponse) string {
	if result.Data == nil {
		return ""
	}
	return strings.TrimSpace(result.Data.TransactionID)
}

func repositoryLooksLikeStoredHash(secret string) bool {
	secret = strings.TrimSpace(secret)
	if len(secret) != 64 {
		return false
	}
	for _, r := range secret {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func payoutEventKey(req providerGateway.PayoutCallbackRequest) string {
	return strings.Join([]string{
		fmt.Sprintf("%v", req.CustomerID),
		strings.TrimSpace(req.OrderID),
		strings.TrimSpace(req.TransactionID),
		strings.TrimSpace(req.TransactionCode),
		strings.TrimSpace(req.DateTime),
	}, "|")
}

func redactPayoutRequestJSON(req providerGateway.CreatePayoutRequest) string {
	req.PayAccountName = maskLeadingPreserveTail(req.PayAccountName, 0, 1)
	req.PayCardNo = maskLeadingPreserveTail(req.PayCardNo, 0, 4)
	req.PayValidateID = maskLeadingPreserveTail(req.PayValidateID, 0, 4)
	return mustJSON(req)
}

func maskLeadingPreserveTail(value string, keepPrefix, keepSuffix int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if keepPrefix < 0 {
		keepPrefix = 0
	}
	if keepSuffix < 0 {
		keepSuffix = 0
	}
	if keepPrefix+keepSuffix >= len(runes) {
		if len(runes) <= 2 {
			return strings.Repeat("*", len(runes))
		}
		keepPrefix = 1
		keepSuffix = 1
	}
	var b strings.Builder
	for idx, r := range runes {
		switch {
		case idx < keepPrefix:
			b.WriteRune(r)
		case idx >= len(runes)-keepSuffix:
			b.WriteRune(r)
		default:
			b.WriteByte('*')
		}
	}
	return b.String()
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

func buildMerchantAPIKeyViews(records []repository.MerchantAPIKeyRecord) []MerchantAPIKeyView {
	views := make([]MerchantAPIKeyView, 0, len(records))
	for _, record := range records {
		views = append(views, MerchantAPIKeyView{
			KeyHash:       record.KeyHash,
			Status:        record.Status,
			IsPrimary:     record.IsPrimary,
			LastUsedAt:    record.LastUsedAt,
			LastRotatedAt: record.LastRotatedAt,
			ExpiresAt:     record.ExpiresAt,
			RevokedAt:     record.RevokedAt,
			CreatedAt:     record.CreatedAt,
			UpdatedAt:     record.UpdatedAt,
		})
	}
	return views
}

func buildMerchantAPIKeyAuditLog(apiKey, reason string, expiresAt, previousExpiresAt *time.Time, auditCtx MerchantAPIKeyAuditContext) repository.MerchantAPIKeyAuditLog {
	metadata := map[string]any{}
	if expiresAt != nil {
		metadata["expires_at"] = expiresAt.Format(time.RFC3339)
	}
	if previousExpiresAt != nil {
		metadata["previous_expires_at"] = previousExpiresAt.Format(time.RFC3339)
	}
	if len(auditCtx.ActorRoles) > 0 {
		metadata["actor_roles"] = append([]string(nil), auditCtx.ActorRoles...)
	}
	if strings.TrimSpace(auditCtx.Checker) != "" {
		metadata["checker"] = strings.TrimSpace(auditCtx.Checker)
	}
	if len(auditCtx.CheckerRoles) > 0 {
		metadata["checker_roles"] = append([]string(nil), auditCtx.CheckerRoles...)
	}
	return repository.MerchantAPIKeyAuditLog{
		KeyHash:   hashAuditAPIKey(apiKey),
		Actor:     strings.TrimSpace(auditCtx.Actor),
		Reason:    strings.TrimSpace(reason),
		RequestID: strings.TrimSpace(auditCtx.RequestID),
		SourceIP:  strings.TrimSpace(auditCtx.SourceIP),
		UserAgent: strings.TrimSpace(auditCtx.UserAgent),
		Metadata:  metadata,
	}
}

func buildMerchantAPIKeyAuditViews(entries []repository.MerchantAPIKeyAuditEntry) []MerchantAPIKeyAuditView {
	views := make([]MerchantAPIKeyAuditView, 0, len(entries))
	for _, entry := range entries {
		view := MerchantAPIKeyAuditView{
			Action:           entry.Action,
			KeyHash:          entry.KeyHash,
			Actor:            entry.Actor,
			Reason:           entry.Reason,
			RequestID:        entry.RequestID,
			SourceIP:         entry.SourceIP,
			UserAgent:        entry.UserAgent,
			CreatedAt:        entry.CreatedAt,
			MerchantAPIKeyID: entry.MerchantAPIKeyID,
		}
		if strings.TrimSpace(entry.Metadata) != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(entry.Metadata), &metadata); err == nil && len(metadata) > 0 {
				view.Metadata = metadata
			}
		}
		views = append(views, view)
	}
	return views
}

func buildPayoutReviewAuditLog(action string, auditCtx PayoutReviewAuditContext, metadata map[string]any) repository.PayoutReviewAuditLog {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	if len(auditCtx.ActorRoles) > 0 {
		metadata["actor_roles"] = append([]string(nil), auditCtx.ActorRoles...)
	}
	if strings.TrimSpace(auditCtx.Checker) != "" {
		metadata["checker"] = strings.TrimSpace(auditCtx.Checker)
	}
	if len(auditCtx.CheckerRoles) > 0 {
		metadata["checker_roles"] = append([]string(nil), auditCtx.CheckerRoles...)
	}
	return repository.PayoutReviewAuditLog{
		Action:    strings.TrimSpace(action),
		Actor:     strings.TrimSpace(auditCtx.Actor),
		Reason:    strings.TrimSpace(auditCtx.Reason),
		RequestID: strings.TrimSpace(auditCtx.RequestID),
		SourceIP:  strings.TrimSpace(auditCtx.SourceIP),
		UserAgent: strings.TrimSpace(auditCtx.UserAgent),
		Metadata:  metadata,
	}
}

func buildPayoutReviewAuditViews(entries []repository.PayoutReviewAuditEntry) []PayoutReviewAuditView {
	views := make([]PayoutReviewAuditView, 0, len(entries))
	for _, entry := range entries {
		view := PayoutReviewAuditView{
			Action:        entry.Action,
			Actor:         entry.Actor,
			Reason:        entry.Reason,
			RequestID:     entry.RequestID,
			SourceIP:      entry.SourceIP,
			UserAgent:     entry.UserAgent,
			CreatedAt:     entry.CreatedAt,
			MerchantID:    entry.MerchantID,
			PayoutOrderID: entry.PayoutOrderID,
		}
		if strings.TrimSpace(entry.Metadata) != "" {
			var metadata map[string]any
			if err := json.Unmarshal([]byte(entry.Metadata), &metadata); err == nil && len(metadata) > 0 {
				view.Metadata = metadata
			}
		}
		views = append(views, view)
	}
	return views
}

func buildPayoutOperationalAlertViews(alerts []domain.PayoutOperationalAlert) []PayoutOperationalAlertView {
	views := make([]PayoutOperationalAlertView, 0, len(alerts))
	for _, alert := range alerts {
		views = append(views, PayoutOperationalAlertView{
			ID:              alert.ID,
			MerchantID:      alert.MerchantID,
			PayoutOrderID:   alert.PayoutOrderID,
			PayoutNo:        alert.PayoutNo,
			Category:        alert.Category,
			Severity:        alert.Severity,
			Status:          alert.Status,
			Summary:         alert.Summary,
			Details:         alert.Details,
			OccurrenceCount: alert.OccurrenceCount,
			FirstOccurredAt: alert.FirstOccurredAt,
			LastOccurredAt:  alert.LastOccurredAt,
			ResolvedAt:      alert.ResolvedAt,
			ResolvedBy:      alert.ResolvedBy,
			ResolveReason:   alert.ResolveReason,
		})
	}
	return views
}

func (s *PayoutService) BuildPayoutSettlementReport(ctx context.Context) (PayoutSettlementReportView, error) {
	snapshot, err := s.store.BuildPayoutSettlementSnapshot(ctx)
	if err != nil {
		return PayoutSettlementReportView{}, err
	}
	if s.client == nil {
		return PayoutSettlementReportView{}, errors.New("payout gateway client is not configured")
	}
	balance, err := s.client.QueryBalance(ctx, providerGateway.BalanceRequest{})
	if err != nil {
		return PayoutSettlementReportView{}, err
	}
	var providerBalanceCents int64
	var providerAvailableCents int64
	var providerUnsettlementCents int64
	if balance.Data != nil {
		balanceValue := strings.TrimSpace(balance.Data.Balance)
		if balanceValue == "" {
			balanceValue = strings.TrimSpace(balance.Data.BalanceOriginal)
		}
		providerBalanceCents, err = parseAmountToCents(balanceValue)
		if err != nil {
			providerBalanceCents = 0
		}
		providerAvailableCents, err = parseAmountToCents(balance.Data.BalanceAvailable)
		if err != nil {
			providerAvailableCents = 0
		}
		providerUnsettlementCents, err = parseAmountToCents(balance.Data.BalanceUnsettlement)
		if err != nil {
			providerUnsettlementCents = 0
		}
	}

	report := PayoutSettlementReportView{
		GeneratedAt: snapshot.GeneratedAt,
		CustomerID:  s.client.CustomerID(),
		Currencies:  make([]PayoutSettlementCurrencyView, 0, len(snapshot.Currencies)),
	}
	for _, currency := range snapshot.Currencies {
		view := PayoutSettlementCurrencyView{
			Currency:                    currency.Currency,
			MerchantAvailableCents:      currency.MerchantAvailableCents,
			MerchantPendingCents:        currency.MerchantPendingCents,
			PendingReviewCents:          currency.PendingReviewCents,
			ApprovedCents:               currency.ApprovedCents,
			SubmittingCents:             currency.SubmittingCents,
			ProcessingCents:             currency.ProcessingCents,
			CompletedCents:              currency.CompletedCents,
			FailedCents:                 currency.FailedCents,
			CancelledOrRejectedCents:    currency.CancelledOrRejectedCents,
			ReversedCents:               currency.ReversedCents,
			OpenOrderCount:              currency.OpenOrderCount,
			ProviderInFlightCents:       currency.ProviderInFlightCents,
			InternalManualHoldCents:     currency.InternalManualHoldCents,
			InternalTotalUnsettledCents: currency.InternalTotalUnsettledCents,
		}
		if strings.EqualFold(currency.Currency, "TWD") {
			view.ProviderBalanceCents = providerBalanceCents
			view.ProviderAvailableCents = providerAvailableCents
			view.ProviderUnsettlementCents = providerUnsettlementCents
			view.ProviderVsInflightGapCents = providerUnsettlementCents - currency.ProviderInFlightCents
			view.MerchantPendingGapCents = currency.MerchantPendingCents - currency.InternalTotalUnsettledCents
		}
		report.Currencies = append(report.Currencies, view)
	}
	return report, nil
}

func (s *PayoutService) raisePayoutOperationalAlert(ctx context.Context, order domain.PayoutOrder, category, severity, summary, details string) error {
	if strings.TrimSpace(order.PayoutNo) == "" {
		return nil
	}
	err := s.store.UpsertPayoutOperationalAlert(ctx, order.PayoutNo, repository.PayoutOperationalAlertUpsert{
		MerchantID:    order.MerchantID,
		PayoutOrderID: order.ID,
		Category:      strings.TrimSpace(category),
		Severity:      strings.TrimSpace(severity),
		Summary:       strings.TrimSpace(summary),
		Details:       strings.TrimSpace(details),
	})
	if err != nil || s.alertNotifier == nil {
		return err
	}
	alerts, listErr := s.store.ListPayoutOperationalAlerts(ctx, "open", 100)
	if listErr != nil {
		return nil
	}
	for _, alert := range alerts {
		if alert.PayoutNo == order.PayoutNo && alert.Category == category {
			// The database keeps every recurrence count, while the external
			// notifier only receives the first occurrence of an open incident.
			if alert.OccurrenceCount == 1 {
				_ = s.alertNotifier.Notify(ctx, alert)
			}
			break
		}
	}
	return nil
}

func findPayoutNoFromTaskPayload(payload string) string {
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", body["payout_no"]))
}

func (s *PayoutService) raisePayoutAlertFromTask(ctx context.Context, task domain.MerchantPayoutCallbackTask, category, severity, summary, details string) {
	payoutNo := findPayoutNoFromTaskPayload(task.Payload)
	if strings.TrimSpace(payoutNo) == "" {
		return
	}
	if order, err := s.store.FindPayoutOrderByPayoutNo(ctx, payoutNo); err == nil {
		_ = s.raisePayoutOperationalAlert(ctx, order, category, severity, summary, details)
	}
}

func hashAuditAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}
