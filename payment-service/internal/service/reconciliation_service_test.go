package service

import (
	"context"
	"testing"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/repository"
)

type fakeReconciliationStore struct {
	lastFilter     domain.ReconciliationFilter
	lastAdjustment repository.ReconciliationAdjustmentParams
	report         domain.ReconciliationReport
}

func (s *fakeReconciliationStore) RunReconciliation(_ context.Context, filter domain.ReconciliationFilter) (domain.ReconciliationReport, error) {
	s.lastFilter = filter
	return s.report, nil
}

func (s *fakeReconciliationStore) GetReconciliationReport(_ context.Context, _ int64) (domain.ReconciliationReport, error) {
	return s.report, nil
}

func (s *fakeReconciliationStore) ListReconciliationReports(_ context.Context, _ domain.ReconciliationReportQuery) ([]domain.ReconciliationReport, error) {
	return []domain.ReconciliationReport{s.report}, nil
}

func (s *fakeReconciliationStore) GetReconciliationTrace(_ context.Context, query domain.ReconciliationTraceQuery) (domain.ReconciliationTrace, error) {
	return domain.ReconciliationTrace{Query: query}, nil
}

func (s *fakeReconciliationStore) ResolveMismatchWithAdjustment(_ context.Context, itemID int64, params repository.ReconciliationAdjustmentParams) (domain.ReconciliationMismatch, error) {
	s.lastAdjustment = params
	return domain.ReconciliationMismatch{ID: itemID, ResolutionStatus: domain.ReconciliationResolutionStatusResolved, ResolutionType: domain.ReconciliationResolutionTypeAdjustment, ResolvedBy: params.Audit.Actor}, nil
}

func (s *fakeReconciliationStore) ResolveMismatchWithReversal(_ context.Context, itemID int64, params repository.ReconciliationReversalParams) (domain.ReconciliationMismatch, error) {
	return domain.ReconciliationMismatch{ID: itemID, ResolutionStatus: domain.ReconciliationResolutionStatusResolved, ResolutionType: domain.ReconciliationResolutionTypeReversal, ResolutionLedgerEntryID: params.TargetEntryID, ResolvedBy: params.Audit.Actor}, nil
}

func TestReconciliationServiceAdjustmentCarriesReviewAudit(t *testing.T) {
	store := &fakeReconciliationStore{}
	service := NewReconciliationService(store)

	item, err := service.ResolveMismatchWithAdjustment(context.Background(), 17, ResolveReconciliationAdjustmentRequest{
		Amount:   "-12",
		Currency: "twd",
		Note:     "provider settlement correction",
		Reason:   "settlement report verified",
	}, PayoutReviewAuditContext{
		Actor:     "ops.finance",
		Checker:   "ops.controller",
		RequestID: "req-adjustment-001",
		SourceIP:  "203.0.113.10",
		UserAgent: "reconciliation-console",
	})
	if err != nil {
		t.Fatalf("ResolveMismatchWithAdjustment() error = %v", err)
	}
	if item.ResolutionStatus != domain.ReconciliationResolutionStatusResolved || item.ResolutionType != domain.ReconciliationResolutionTypeAdjustment {
		t.Fatalf("ResolveMismatchWithAdjustment() item = %+v", item)
	}
	if store.lastAdjustment.AmountCents != -1200 || store.lastAdjustment.Currency != "TWD" {
		t.Fatalf("ResolveMismatchWithAdjustment() params = %+v", store.lastAdjustment)
	}
	if store.lastAdjustment.Audit.Actor != "ops.finance" || store.lastAdjustment.Audit.Checker != "ops.controller" || store.lastAdjustment.Audit.Reason != "settlement report verified" || store.lastAdjustment.Audit.RequestID != "req-adjustment-001" {
		t.Fatalf("ResolveMismatchWithAdjustment() audit = %+v", store.lastAdjustment.Audit)
	}
}

func TestReconciliationServiceRunReconciliationPassesFilters(t *testing.T) {
	completedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	store := &fakeReconciliationStore{
		report: domain.ReconciliationReport{
			ID:            7,
			ScopeType:     domain.ReconciliationScopePartial,
			Status:        domain.ReconciliationStatusCompleted,
			MerchantCode:  "M10001",
			OrderNo:       "D202607160001",
			MismatchCount: 1,
			StartedAt:     completedAt.Add(-time.Minute),
			CompletedAt:   &completedAt,
			Items: []domain.ReconciliationMismatch{{
				MismatchType:  domain.ReconciliationMismatchMissingLedger,
				MerchantID:    1,
				MerchantCode:  "M10001",
				EntityType:    "order",
				EntityID:      99,
				OrderNo:       "D202607160001",
				TableName:     "ledger_entries",
				FieldName:     "type",
				ExpectedValue: "deposit",
				ActualValue:   "missing",
			}},
		},
	}
	service := NewReconciliationService(store)

	report, err := service.RunReconciliation(context.Background(), RunReconciliationRequest{
		MerchantID: "M10001",
		OrderNo:    "D202607160001",
	})
	if err != nil {
		t.Fatalf("RunReconciliation() error = %v", err)
	}
	if store.lastFilter.MerchantCode != "M10001" || store.lastFilter.OrderNo != "D202607160001" {
		t.Fatalf("RunReconciliation() filter = %+v", store.lastFilter)
	}
	if report.ID != 7 || report.MismatchCount != 1 {
		t.Fatalf("RunReconciliation() report = %+v", report)
	}
	if len(report.Items) != 1 || report.Items[0].MismatchType != domain.ReconciliationMismatchMissingLedger {
		t.Fatalf("RunReconciliation() items = %+v", report.Items)
	}
}
