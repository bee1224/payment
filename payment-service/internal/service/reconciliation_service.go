package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/repository"
)

type ReconciliationService struct {
	store repository.ReconciliationStore
	now   func() time.Time
}

type RunReconciliationRequest struct {
	MerchantID string `json:"merchant_id"`
	OrderNo    string `json:"order_no"`
	PayoutNo   string `json:"payout_no"`
}

type ResolveReconciliationAdjustmentRequest struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Note     string `json:"note"`
	Reason   string `json:"reason"`
}

type ResolveReconciliationReversalRequest struct {
	LedgerEntryID int64  `json:"ledger_entry_id"`
	Note          string `json:"note"`
	Reason        string `json:"reason"`
}

type ReconciliationMismatchView struct {
	ID                      int64                                 `json:"id"`
	MismatchType            domain.ReconciliationMismatchType     `json:"mismatch_type"`
	MerchantID              int64                                 `json:"merchant_id,omitempty"`
	MerchantCode            string                                `json:"merchant_code,omitempty"`
	EntityType              string                                `json:"entity_type"`
	EntityID                int64                                 `json:"entity_id,omitempty"`
	OrderNo                 string                                `json:"order_no,omitempty"`
	PayoutNo                string                                `json:"payout_no,omitempty"`
	TableName               string                                `json:"table_name"`
	FieldName               string                                `json:"field_name"`
	ExpectedValue           string                                `json:"expected_value,omitempty"`
	ActualValue             string                                `json:"actual_value,omitempty"`
	Details                 string                                `json:"details,omitempty"`
	ResolutionStatus        domain.ReconciliationResolutionStatus `json:"resolution_status"`
	ResolutionType          domain.ReconciliationResolutionType   `json:"resolution_type,omitempty"`
	ResolutionNote          string                                `json:"resolution_note,omitempty"`
	ResolutionLedgerEntryID int64                                 `json:"resolution_ledger_entry_id,omitempty"`
	ResolvedAt              *time.Time                            `json:"resolved_at,omitempty"`
	ResolvedBy              string                                `json:"resolved_by,omitempty"`
	CreatedAt               time.Time                             `json:"created_at"`
}

type ListReconciliationReportsRequest struct {
	DateFrom     *time.Time
	DateTo       *time.Time
	MerchantID   string
	OrderType    domain.ReconciliationOrderType
	MismatchType domain.ReconciliationMismatchType
}

type ReconciliationReportView struct {
	ID            int64                          `json:"id"`
	ScopeType     domain.ReconciliationScopeType `json:"scope_type"`
	Status        domain.ReconciliationStatus    `json:"status"`
	MerchantID    string                         `json:"merchant_id,omitempty"`
	OrderNo       string                         `json:"order_no,omitempty"`
	PayoutNo      string                         `json:"payout_no,omitempty"`
	MismatchCount int                            `json:"mismatch_count"`
	StartedAt     time.Time                      `json:"started_at"`
	CompletedAt   *time.Time                     `json:"completed_at,omitempty"`
	Items         []ReconciliationMismatchView   `json:"items"`
}

func NewReconciliationService(store repository.ReconciliationStore) *ReconciliationService {
	return &ReconciliationService{
		store: store,
		now:   time.Now,
	}
}

func (s *ReconciliationService) RunReconciliation(ctx context.Context, req RunReconciliationRequest) (ReconciliationReportView, error) {
	if s.store == nil {
		return ReconciliationReportView{}, errors.New("reconciliation store is not configured")
	}
	filter := domain.ReconciliationFilter{
		MerchantCode: strings.TrimSpace(req.MerchantID),
		OrderNo:      strings.TrimSpace(req.OrderNo),
		PayoutNo:     strings.TrimSpace(req.PayoutNo),
	}
	report, err := s.store.RunReconciliation(ctx, filter)
	if err != nil {
		return ReconciliationReportView{}, err
	}
	return buildReconciliationReportView(report), nil
}

func (s *ReconciliationService) GetReconciliationReport(ctx context.Context, runID int64) (ReconciliationReportView, error) {
	if s.store == nil {
		return ReconciliationReportView{}, errors.New("reconciliation store is not configured")
	}
	if runID <= 0 {
		return ReconciliationReportView{}, errors.New("run_id must be a positive integer")
	}
	report, err := s.store.GetReconciliationReport(ctx, runID)
	if err != nil {
		return ReconciliationReportView{}, err
	}
	return buildReconciliationReportView(report), nil
}

func (s *ReconciliationService) ListReconciliationReports(ctx context.Context, req ListReconciliationReportsRequest) ([]ReconciliationReportView, error) {
	if s.store == nil {
		return nil, errors.New("reconciliation store is not configured")
	}
	if req.OrderType != "" && req.OrderType != domain.ReconciliationOrderTypeDeposit && req.OrderType != domain.ReconciliationOrderTypePayout {
		return nil, errors.New("order_type must be deposit or payout")
	}
	if req.MismatchType != "" && !validMismatchType(req.MismatchType) {
		return nil, errors.New("invalid mismatch_type")
	}
	if req.DateFrom != nil && req.DateTo != nil && !req.DateFrom.Before(*req.DateTo) {
		return nil, errors.New("date_from must be before date_to")
	}
	reports, err := s.store.ListReconciliationReports(ctx, domain.ReconciliationReportQuery{DateFrom: req.DateFrom, DateTo: req.DateTo, MerchantCode: strings.TrimSpace(req.MerchantID), OrderType: req.OrderType, MismatchType: req.MismatchType})
	if err != nil {
		return nil, err
	}
	views := make([]ReconciliationReportView, 0, len(reports))
	for _, report := range reports {
		views = append(views, buildReconciliationReportView(report))
	}
	return views, nil
}

func (s *ReconciliationService) GetReconciliationTrace(ctx context.Context, query domain.ReconciliationTraceQuery) (domain.ReconciliationTrace, error) {
	if s.store == nil {
		return domain.ReconciliationTrace{}, errors.New("reconciliation store is not configured")
	}
	query.MerchantOrderNo = strings.TrimSpace(query.MerchantOrderNo)
	query.PayoutNo = strings.TrimSpace(query.PayoutNo)
	query.ProviderTradeNo = strings.TrimSpace(query.ProviderTradeNo)
	query.LedgerEntryNo = strings.TrimSpace(query.LedgerEntryNo)
	return s.store.GetReconciliationTrace(ctx, query)
}

func (s *ReconciliationService) ResolveMismatchWithAdjustment(ctx context.Context, itemID int64, req ResolveReconciliationAdjustmentRequest, audit PayoutReviewAuditContext) (ReconciliationMismatchView, error) {
	if s.store == nil {
		return ReconciliationMismatchView{}, errors.New("reconciliation store is not configured")
	}
	if itemID <= 0 {
		return ReconciliationMismatchView{}, errors.New("item_id must be a positive integer")
	}
	amountCents, err := parseSignedAmountToCents(req.Amount)
	if err != nil {
		return ReconciliationMismatchView{}, err
	}
	if amountCents == 0 {
		return ReconciliationMismatchView{}, errors.New("adjustment amount must not be zero")
	}
	params := repository.ReconciliationAdjustmentParams{
		AmountCents: amountCents,
		Currency:    strings.ToUpper(strings.TrimSpace(req.Currency)),
		Note:        strings.TrimSpace(req.Note),
		Audit:       buildReconciliationResolutionAudit(audit, req.Reason, map[string]any{"amount_cents": amountCents, "currency": strings.ToUpper(strings.TrimSpace(req.Currency))}),
	}
	if err := validateReconciliationResolutionAudit(params.Audit); err != nil {
		return ReconciliationMismatchView{}, err
	}
	item, err := s.store.ResolveMismatchWithAdjustment(ctx, itemID, params)
	if err != nil {
		return ReconciliationMismatchView{}, err
	}
	return buildReconciliationMismatchView(item), nil
}

func (s *ReconciliationService) ResolveMismatchWithReversal(ctx context.Context, itemID int64, req ResolveReconciliationReversalRequest, audit PayoutReviewAuditContext) (ReconciliationMismatchView, error) {
	if s.store == nil {
		return ReconciliationMismatchView{}, errors.New("reconciliation store is not configured")
	}
	if itemID <= 0 || req.LedgerEntryID <= 0 {
		return ReconciliationMismatchView{}, errors.New("item_id and ledger_entry_id must be positive integers")
	}
	params := repository.ReconciliationReversalParams{
		TargetEntryID: req.LedgerEntryID,
		Note:          strings.TrimSpace(req.Note),
		Audit:         buildReconciliationResolutionAudit(audit, req.Reason, map[string]any{"target_ledger_entry_id": req.LedgerEntryID}),
	}
	if err := validateReconciliationResolutionAudit(params.Audit); err != nil {
		return ReconciliationMismatchView{}, err
	}
	item, err := s.store.ResolveMismatchWithReversal(ctx, itemID, params)
	if err != nil {
		return ReconciliationMismatchView{}, err
	}
	return buildReconciliationMismatchView(item), nil
}

func buildReconciliationResolutionAudit(audit PayoutReviewAuditContext, reason string, metadata map[string]any) repository.ReconciliationResolutionAudit {
	return repository.ReconciliationResolutionAudit{
		Actor:     strings.TrimSpace(audit.Actor),
		Checker:   strings.TrimSpace(audit.Checker),
		Reason:    strings.TrimSpace(reason),
		RequestID: strings.TrimSpace(audit.RequestID),
		SourceIP:  strings.TrimSpace(audit.SourceIP),
		UserAgent: strings.TrimSpace(audit.UserAgent),
		Metadata:  metadata,
	}
}

func validateReconciliationResolutionAudit(audit repository.ReconciliationResolutionAudit) error {
	if audit.Actor == "" || audit.Checker == "" || audit.Reason == "" || audit.RequestID == "" {
		return errors.New("reconciliation resolution requires actor, checker, reason, and request_id")
	}
	return nil
}

func parseSignedAmountToCents(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("amount is required")
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = strings.TrimSpace(strings.TrimPrefix(value, "-"))
	}
	amountCents, err := parseAmountToCents(value)
	if err != nil {
		return 0, err
	}
	if negative {
		return -amountCents, nil
	}
	return amountCents, nil
}

func buildReconciliationReportView(report domain.ReconciliationReport) ReconciliationReportView {
	view := ReconciliationReportView{
		ID:            report.ID,
		ScopeType:     report.ScopeType,
		Status:        report.Status,
		MerchantID:    report.MerchantCode,
		OrderNo:       report.OrderNo,
		PayoutNo:      report.PayoutNo,
		MismatchCount: report.MismatchCount,
		StartedAt:     report.StartedAt,
		CompletedAt:   report.CompletedAt,
		Items:         make([]ReconciliationMismatchView, 0, len(report.Items)),
	}
	for _, item := range report.Items {
		view.Items = append(view.Items, buildReconciliationMismatchView(item))
	}
	return view
}

func buildReconciliationMismatchView(item domain.ReconciliationMismatch) ReconciliationMismatchView {
	return ReconciliationMismatchView{
		ID:                      item.ID,
		MismatchType:            item.MismatchType,
		MerchantID:              item.MerchantID,
		MerchantCode:            item.MerchantCode,
		EntityType:              item.EntityType,
		EntityID:                item.EntityID,
		OrderNo:                 item.OrderNo,
		PayoutNo:                item.PayoutNo,
		TableName:               item.TableName,
		FieldName:               item.FieldName,
		ExpectedValue:           item.ExpectedValue,
		ActualValue:             item.ActualValue,
		Details:                 item.Details,
		ResolutionStatus:        item.ResolutionStatus,
		ResolutionType:          item.ResolutionType,
		ResolutionNote:          item.ResolutionNote,
		ResolutionLedgerEntryID: item.ResolutionLedgerEntryID,
		ResolvedAt:              item.ResolvedAt,
		ResolvedBy:              item.ResolvedBy,
		CreatedAt:               item.CreatedAt,
	}
}

func validMismatchType(value domain.ReconciliationMismatchType) bool {
	switch value {
	case domain.ReconciliationMismatchBalanceMismatch, domain.ReconciliationMismatchMissingLedger, domain.ReconciliationMismatchDuplicateLedger, domain.ReconciliationMismatchStuckPending, domain.ReconciliationMismatchProviderState:
		return true
	default:
		return false
	}
}
