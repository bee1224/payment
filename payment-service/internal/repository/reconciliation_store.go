package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/domain"
)

const reconciliationStuckThreshold = 24 * time.Hour

type ReconciliationStore interface {
	RunReconciliation(ctx context.Context, filter domain.ReconciliationFilter) (domain.ReconciliationReport, error)
	GetReconciliationReport(ctx context.Context, runID int64) (domain.ReconciliationReport, error)
	ListReconciliationReports(ctx context.Context, query domain.ReconciliationReportQuery) ([]domain.ReconciliationReport, error)
	GetReconciliationTrace(ctx context.Context, query domain.ReconciliationTraceQuery) (domain.ReconciliationTrace, error)
	ResolveMismatchWithAdjustment(ctx context.Context, itemID int64, params ReconciliationAdjustmentParams) (domain.ReconciliationMismatch, error)
	ResolveMismatchWithReversal(ctx context.Context, itemID int64, params ReconciliationReversalParams) (domain.ReconciliationMismatch, error)
}

type ReconciliationResolutionAudit struct {
	Actor     string
	Checker   string
	Reason    string
	RequestID string
	SourceIP  string
	UserAgent string
	Metadata  map[string]any
}

type ReconciliationAdjustmentParams struct {
	AmountCents int64
	Currency    string
	Note        string
	Audit       ReconciliationResolutionAudit
}

type ReconciliationReversalParams struct {
	TargetEntryID int64
	Note          string
	Audit         ReconciliationResolutionAudit
}

type InMemoryReconciliationStore struct {
	nextRunID      int64
	nextMismatchID int64
	nextActionID   int64
	reports        map[int64]domain.ReconciliationReport
}

func NewInMemoryReconciliationStore() *InMemoryReconciliationStore {
	return &InMemoryReconciliationStore{
		nextRunID:      1,
		nextMismatchID: 1,
		nextActionID:   1,
		reports:        make(map[int64]domain.ReconciliationReport),
	}
}

func (s *InMemoryReconciliationStore) RunReconciliation(_ context.Context, filter domain.ReconciliationFilter) (domain.ReconciliationReport, error) {
	now := time.Now()
	scope := domain.ReconciliationScopeFull
	if hasReconciliationFilter(filter) {
		scope = domain.ReconciliationScopePartial
	}
	report := domain.ReconciliationReport{
		ID:           s.nextRunID,
		ScopeType:    scope,
		Status:       domain.ReconciliationStatusCompleted,
		MerchantCode: strings.TrimSpace(filter.MerchantCode),
		OrderNo:      strings.TrimSpace(filter.OrderNo),
		PayoutNo:     strings.TrimSpace(filter.PayoutNo),
		StartedAt:    now,
		CompletedAt:  &now,
	}
	s.nextRunID++
	s.reports[report.ID] = report
	return report, nil
}

func (s *InMemoryReconciliationStore) GetReconciliationReport(_ context.Context, runID int64) (domain.ReconciliationReport, error) {
	report, ok := s.reports[runID]
	if !ok {
		return domain.ReconciliationReport{}, ErrNotFound
	}
	return report, nil
}

func (s *InMemoryReconciliationStore) ListReconciliationReports(_ context.Context, query domain.ReconciliationReportQuery) ([]domain.ReconciliationReport, error) {
	reports := make([]domain.ReconciliationReport, 0)
	for _, report := range s.reports {
		if query.DateFrom != nil && report.StartedAt.Before(*query.DateFrom) || query.DateTo != nil && report.StartedAt.After(*query.DateTo) {
			continue
		}
		items := filterReconciliationItems(report.Items, query)
		if hasReportItemFilter(query) && len(items) == 0 {
			continue
		}
		report.Items = items
		report.MismatchCount = len(items)
		reports = append(reports, report)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].StartedAt.After(reports[j].StartedAt) })
	return reports, nil
}

func (s *InMemoryReconciliationStore) GetReconciliationTrace(_ context.Context, query domain.ReconciliationTraceQuery) (domain.ReconciliationTrace, error) {
	if emptyTraceQuery(query) {
		return domain.ReconciliationTrace{}, errors.New("one trace lookup value is required")
	}
	trace := newReconciliationTrace(query)
	for _, report := range s.reports {
		for _, item := range report.Items {
			if traceMismatchMatches(item, query) {
				trace.Mismatches = append(trace.Mismatches, item)
			}
		}
	}
	return trace, nil
}

func (s *InMemoryReconciliationStore) SeedReport(report domain.ReconciliationReport) {
	if report.ID == 0 {
		report.ID = s.nextRunID
		s.nextRunID++
	}
	for idx := range report.Items {
		if report.Items[idx].ID == 0 {
			report.Items[idx].ID = s.nextMismatchID
			s.nextMismatchID++
		}
		report.Items[idx].RunID = report.ID
		if report.Items[idx].ResolutionStatus == "" {
			report.Items[idx].ResolutionStatus = domain.ReconciliationResolutionStatusOpen
		}
	}
	s.reports[report.ID] = report
}

func (s *InMemoryReconciliationStore) ResolveMismatchWithAdjustment(_ context.Context, itemID int64, params ReconciliationAdjustmentParams) (domain.ReconciliationMismatch, error) {
	return s.resolveMismatch(itemID, domain.ReconciliationResolutionTypeAdjustment, params.Note, int64(1000+s.nextActionID), params.Audit)
}

func (s *InMemoryReconciliationStore) ResolveMismatchWithReversal(_ context.Context, itemID int64, params ReconciliationReversalParams) (domain.ReconciliationMismatch, error) {
	targetID := params.TargetEntryID
	if targetID == 0 {
		targetID = int64(2000 + s.nextActionID)
	}
	return s.resolveMismatch(itemID, domain.ReconciliationResolutionTypeReversal, params.Note, targetID, params.Audit)
}

func (s *InMemoryReconciliationStore) resolveMismatch(itemID int64, resolutionType domain.ReconciliationResolutionType, note string, ledgerEntryID int64, audit ReconciliationResolutionAudit) (domain.ReconciliationMismatch, error) {
	for reportID, report := range s.reports {
		for idx := range report.Items {
			if report.Items[idx].ID != itemID {
				continue
			}
			if report.Items[idx].ResolutionStatus == domain.ReconciliationResolutionStatusResolved {
				return domain.ReconciliationMismatch{}, errors.New("reconciliation item is already resolved")
			}
			now := time.Now()
			report.Items[idx].ResolutionStatus = domain.ReconciliationResolutionStatusResolved
			report.Items[idx].ResolutionType = resolutionType
			report.Items[idx].ResolutionNote = strings.TrimSpace(note)
			report.Items[idx].ResolutionLedgerEntryID = ledgerEntryID
			report.Items[idx].ResolvedAt = &now
			report.Items[idx].ResolvedBy = strings.TrimSpace(audit.Actor)
			if report.Items[idx].ResolvedBy == "" {
				report.Items[idx].ResolvedBy = "system"
			}
			s.nextActionID++
			s.reports[reportID] = report
			return report.Items[idx], nil
		}
	}
	return domain.ReconciliationMismatch{}, ErrNotFound
}

type MySQLReconciliationStore struct {
	db *sql.DB
}

func NewMySQLReconciliationStore(db *sql.DB) *MySQLReconciliationStore {
	return &MySQLReconciliationStore{db: db}
}

func (s *MySQLReconciliationStore) RunReconciliation(ctx context.Context, filter domain.ReconciliationFilter) (domain.ReconciliationReport, error) {
	report := domain.ReconciliationReport{
		ScopeType:    domain.ReconciliationScopeFull,
		Status:       domain.ReconciliationStatusRunning,
		MerchantCode: strings.TrimSpace(filter.MerchantCode),
		OrderNo:      strings.TrimSpace(filter.OrderNo),
		PayoutNo:     strings.TrimSpace(filter.PayoutNo),
		StartedAt:    time.Now(),
	}
	if hasReconciliationFilter(filter) {
		report.ScopeType = domain.ReconciliationScopePartial
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO reconciliation_runs (
			scope_type, status, merchant_code, order_no, payout_no, mismatch_count, started_at
		) VALUES (?, ?, ?, ?, ?, 0, ?)
	`, string(report.ScopeType), string(report.Status), nullableString(report.MerchantCode), nullableString(report.OrderNo), nullableString(report.PayoutNo), report.StartedAt)
	if err != nil {
		return domain.ReconciliationReport{}, err
	}
	report.ID, err = result.LastInsertId()
	if err != nil {
		return domain.ReconciliationReport{}, err
	}

	mismatches, collectErr := s.collectMismatches(ctx, report.ID, filter)
	completedAt := time.Now()
	status := domain.ReconciliationStatusCompleted
	if collectErr != nil {
		status = domain.ReconciliationStatusFailed
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE reconciliation_runs
		SET status = ?, mismatch_count = ?, completed_at = ?
		WHERE id = ?
	`, string(status), len(mismatches), completedAt, report.ID); err != nil {
		return domain.ReconciliationReport{}, err
	}
	if collectErr != nil {
		return domain.ReconciliationReport{}, collectErr
	}
	report.Status = status
	report.MismatchCount = len(mismatches)
	report.CompletedAt = &completedAt
	report.Items = mismatches
	return report, nil
}

func (s *MySQLReconciliationStore) GetReconciliationReport(ctx context.Context, runID int64) (domain.ReconciliationReport, error) {
	var report domain.ReconciliationReport
	var scopeType, status string
	var merchantCode, orderNo, payoutNo sql.NullString
	var completedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, scope_type, status, merchant_code, order_no, payout_no, mismatch_count, started_at, completed_at
		FROM reconciliation_runs
		WHERE id = ?
		LIMIT 1
	`, runID).Scan(
		&report.ID, &scopeType, &status, &merchantCode, &orderNo, &payoutNo, &report.MismatchCount, &report.StartedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReconciliationReport{}, ErrNotFound
	}
	if err != nil {
		return domain.ReconciliationReport{}, err
	}
	report.ScopeType = domain.ReconciliationScopeType(scopeType)
	report.Status = domain.ReconciliationStatus(status)
	report.MerchantCode = merchantCode.String
	report.OrderNo = orderNo.String
	report.PayoutNo = payoutNo.String
	if completedAt.Valid {
		report.CompletedAt = &completedAt.Time
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, mismatch_type, merchant_id, COALESCE(merchant_code, ''), entity_type, entity_id,
		       COALESCE(order_no, ''), COALESCE(payout_no, ''), table_name, field_name,
		       COALESCE(expected_value, ''), COALESCE(actual_value, ''), COALESCE(details, ''),
		       resolution_status, COALESCE(resolution_type, ''), COALESCE(resolution_note, ''),
		       COALESCE(resolution_ledger_entry_id, 0), resolved_at, COALESCE(resolved_by, ''), created_at
		FROM reconciliation_run_items
		WHERE run_id = ?
		ORDER BY id ASC
	`, runID)
	if err != nil {
		return domain.ReconciliationReport{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.ReconciliationMismatch
		var merchantID sql.NullInt64
		var entityID sql.NullInt64
		var resolutionLedgerEntryID int64
		var resolvedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.RunID, &item.MismatchType, &merchantID, &item.MerchantCode, &item.EntityType, &entityID,
			&item.OrderNo, &item.PayoutNo, &item.TableName, &item.FieldName, &item.ExpectedValue, &item.ActualValue, &item.Details,
			&item.ResolutionStatus, &item.ResolutionType, &item.ResolutionNote, &resolutionLedgerEntryID, &resolvedAt, &item.ResolvedBy, &item.CreatedAt,
		); err != nil {
			return domain.ReconciliationReport{}, err
		}
		if merchantID.Valid {
			item.MerchantID = merchantID.Int64
		}
		if entityID.Valid {
			item.EntityID = entityID.Int64
		}
		item.ResolutionLedgerEntryID = resolutionLedgerEntryID
		if resolvedAt.Valid {
			item.ResolvedAt = &resolvedAt.Time
		}
		if item.ResolutionStatus == "" {
			item.ResolutionStatus = domain.ReconciliationResolutionStatusOpen
		}
		report.Items = append(report.Items, item)
	}
	return report, rows.Err()
}

func (s *MySQLReconciliationStore) ListReconciliationReports(ctx context.Context, query domain.ReconciliationReportQuery) ([]domain.ReconciliationReport, error) {
	where := []string{"1 = 1"}
	args := make([]any, 0, 8)
	if query.DateFrom != nil {
		where = append(where, "r.started_at >= ?")
		args = append(args, *query.DateFrom)
	}
	if query.DateTo != nil {
		where = append(where, "r.started_at < ?")
		args = append(args, *query.DateTo)
	}
	if hasReportItemFilter(query) {
		itemWhere := []string{"i.run_id = r.id"}
		if query.MerchantCode != "" {
			itemWhere = append(itemWhere, "i.merchant_code = ?")
			args = append(args, query.MerchantCode)
		}
		if query.MismatchType != "" {
			itemWhere = append(itemWhere, "i.mismatch_type = ?")
			args = append(args, string(query.MismatchType))
		}
		if query.OrderType == domain.ReconciliationOrderTypeDeposit {
			itemWhere = append(itemWhere, "i.entity_type IN ('order', 'provider_transaction')")
		}
		if query.OrderType == domain.ReconciliationOrderTypePayout {
			itemWhere = append(itemWhere, "i.entity_type = 'payout_order'")
		}
		where = append(where, "EXISTS (SELECT 1 FROM reconciliation_run_items i WHERE "+strings.Join(itemWhere, " AND ")+")")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id FROM reconciliation_runs r WHERE `+strings.Join(where, " AND ")+` ORDER BY r.started_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := make([]domain.ReconciliationReport, 0)
	for rows.Next() {
		var runID int64
		if err := rows.Scan(&runID); err != nil {
			return nil, err
		}
		report, err := s.GetReconciliationReport(ctx, runID)
		if err != nil {
			return nil, err
		}
		report.Items = filterReconciliationItems(report.Items, query)
		if hasReportItemFilter(query) && len(report.Items) == 0 {
			continue
		}
		report.MismatchCount = len(report.Items)
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

func (s *MySQLReconciliationStore) GetReconciliationTrace(ctx context.Context, query domain.ReconciliationTraceQuery) (domain.ReconciliationTrace, error) {
	if emptyTraceQuery(query) {
		return domain.ReconciliationTrace{}, errors.New("one trace lookup value is required")
	}
	trace := newReconciliationTrace(query)
	deposits, err := s.traceDeposits(ctx, query)
	if err != nil {
		return domain.ReconciliationTrace{}, err
	}
	payouts, err := s.tracePayouts(ctx, query)
	if err != nil {
		return domain.ReconciliationTrace{}, err
	}
	trace.Deposits, trace.Payouts = deposits, payouts
	orderIDs, payoutIDs, merchantIDs := traceIDs(deposits, payouts)
	if err := s.populateTraceDetails(ctx, &trace, query, orderIDs, payoutIDs, merchantIDs); err != nil {
		return domain.ReconciliationTrace{}, err
	}
	return trace, nil
}

func (s *MySQLReconciliationStore) traceDeposits(ctx context.Context, query domain.ReconciliationTraceQuery) ([]domain.ReconciliationTraceRecord, error) {
	where, args := traceWhere(query, []string{
		"o.merchant_order_no", "o.order_no", "pt.provider_trade_no", "le.entry_no",
	})
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT o.id, o.merchant_id, m.code, o.order_no, o.merchant_order_no, '',
		COALESCE(pt.provider_trade_no, ''), o.status, o.amount_cents, o.currency, o.paid_at, o.created_at
		FROM orders o JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN ledger_entries le ON le.order_id = o.id
		WHERE `+where+` ORDER BY o.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTraceRecords(rows, "deposit")
}

func (s *MySQLReconciliationStore) tracePayouts(ctx context.Context, query domain.ReconciliationTraceQuery) ([]domain.ReconciliationTraceRecord, error) {
	where, args := traceWhere(query, []string{
		"po.merchant_payout_no", "po.payout_no", "COALESCE(po.provider_trade_no, '')", "COALESCE(pt.provider_trade_no, '')", "le.entry_no",
	})
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT po.id, po.merchant_id, m.code, '', po.merchant_payout_no, po.payout_no,
		COALESCE(po.provider_trade_no, pt.provider_trade_no, ''), po.status, po.total_debit_cents, po.currency, po.completed_at, po.created_at
		FROM payout_orders po JOIN merchants m ON m.id = po.merchant_id
		LEFT JOIN payout_transactions pt ON pt.payout_order_id = po.id
		LEFT JOIN ledger_entries le ON le.payout_order_id = po.id
		WHERE `+where+` ORDER BY po.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTraceRecords(rows, "payout")
}

func (s *MySQLReconciliationStore) populateTraceDetails(ctx context.Context, trace *domain.ReconciliationTrace, query domain.ReconciliationTraceQuery, orderIDs, payoutIDs, merchantIDs []int64) error {
	if len(merchantIDs) > 0 {
		placeholders, args := int64Placeholders(merchantIDs)
		rows, err := s.db.QueryContext(ctx, `SELECT mb.merchant_id, m.code, mb.currency, mb.available_cents, mb.pending_cents, mb.updated_at FROM merchant_balances mb JOIN merchants m ON m.id = mb.merchant_id WHERE mb.merchant_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b domain.ReconciliationBalanceSnapshot
			if err := rows.Scan(&b.MerchantID, &b.MerchantCode, &b.Currency, &b.AvailableCents, &b.PendingCents, &b.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			trace.Balances = append(trace.Balances, b)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	if len(orderIDs) > 0 {
		placeholders, args := int64Placeholders(orderIDs)
		rows, err := s.db.QueryContext(ctx, `SELECT pt.id, o.merchant_id, m.code, o.order_no, o.merchant_order_no, '', pt.provider_trade_no, pt.status, pt.amount_cents, pt.currency, pt.paid_at, pt.created_at FROM provider_transactions pt JOIN orders o ON o.id = pt.order_id JOIN merchants m ON m.id = o.merchant_id WHERE pt.order_id IN (`+placeholders+`) ORDER BY pt.id`, args...)
		if err != nil {
			return err
		}
		records, err := scanTraceRecords(rows, "deposit_provider_transaction")
		if err != nil {
			return err
		}
		trace.Providers = append(trace.Providers, records...)
	}
	if len(payoutIDs) > 0 {
		placeholders, args := int64Placeholders(payoutIDs)
		rows, err := s.db.QueryContext(ctx, `SELECT pt.id, po.merchant_id, m.code, '', po.merchant_payout_no, po.payout_no, pt.provider_trade_no, pt.status, po.total_debit_cents, po.currency, pt.completed_at, pt.created_at FROM payout_transactions pt JOIN payout_orders po ON po.id = pt.payout_order_id JOIN merchants m ON m.id = po.merchant_id WHERE pt.payout_order_id IN (`+placeholders+`) ORDER BY pt.id`, args...)
		if err != nil {
			return err
		}
		records, err := scanTraceRecords(rows, "payout_provider_transaction")
		if err != nil {
			return err
		}
		trace.Providers = append(trace.Providers, records...)
	}
	if err := s.populateTraceCallbacks(ctx, trace, orderIDs, payoutIDs); err != nil {
		return err
	}
	if err := s.populateTraceLedgers(ctx, trace, query, orderIDs, payoutIDs); err != nil {
		return err
	}
	return s.populateTraceMismatches(ctx, trace, query, orderIDs, payoutIDs)
}

func (s *MySQLReconciliationStore) populateTraceCallbacks(ctx context.Context, trace *domain.ReconciliationTrace, orderIDs, payoutIDs []int64) error {
	if len(orderIDs) > 0 {
		placeholders, args := int64Placeholders(orderIDs)
		rows, err := s.db.QueryContext(ctx, `SELECT pc.id, o.merchant_id, m.code, o.order_no, o.merchant_order_no, '', COALESCE(pc.provider_trade_no, ''), pc.status, 0, '', pc.processed_at, pc.received_at FROM provider_callbacks pc JOIN orders o ON o.id = pc.order_id JOIN merchants m ON m.id = o.merchant_id WHERE pc.order_id IN (`+placeholders+`) ORDER BY pc.received_at`, args...)
		if err != nil {
			return err
		}
		records, err := scanTraceRecords(rows, "deposit_provider_callback")
		if err != nil {
			return err
		}
		trace.Callbacks = append(trace.Callbacks, records...)
	}
	if len(payoutIDs) > 0 {
		placeholders, args := int64Placeholders(payoutIDs)
		rows, err := s.db.QueryContext(ctx, `SELECT pc.id, po.merchant_id, m.code, '', po.merchant_payout_no, po.payout_no, COALESCE(pc.provider_trade_no, ''), pc.status, 0, '', pc.processed_at, pc.received_at FROM payout_callbacks pc JOIN payout_orders po ON po.id = pc.payout_order_id JOIN merchants m ON m.id = po.merchant_id WHERE pc.payout_order_id IN (`+placeholders+`) ORDER BY pc.received_at`, args...)
		if err != nil {
			return err
		}
		records, err := scanTraceRecords(rows, "payout_provider_callback")
		if err != nil {
			return err
		}
		trace.Callbacks = append(trace.Callbacks, records...)
	}
	return nil
}

func (s *MySQLReconciliationStore) populateTraceLedgers(ctx context.Context, trace *domain.ReconciliationTrace, query domain.ReconciliationTraceQuery, orderIDs, payoutIDs []int64) error {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, len(orderIDs)+len(payoutIDs)+1)
	if len(orderIDs) > 0 {
		placeholders, values := int64Placeholders(orderIDs)
		clauses = append(clauses, "le.order_id IN ("+placeholders+")")
		args = append(args, values...)
	}
	if len(payoutIDs) > 0 {
		placeholders, values := int64Placeholders(payoutIDs)
		clauses = append(clauses, "le.payout_order_id IN ("+placeholders+")")
		args = append(args, values...)
	}
	if query.LedgerEntryNo != "" {
		clauses = append(clauses, "le.entry_no = ?")
		args = append(args, query.LedgerEntryNo)
	}
	if len(clauses) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT le.id, le.merchant_id, m.code, COALESCE(o.order_no, ''), COALESCE(o.merchant_order_no, ''), COALESCE(po.payout_no, ''), COALESCE(pt.provider_trade_no, ppt.provider_trade_no, po.provider_trade_no, ''), le.type, le.amount_cents, le.currency, le.created_at, le.created_at, le.entry_no, le.direction, le.balance_before_cents, le.balance_after_cents, le.source_event FROM ledger_entries le JOIN merchants m ON m.id = le.merchant_id LEFT JOIN orders o ON o.id = le.order_id LEFT JOIN payout_orders po ON po.id = le.payout_order_id LEFT JOIN provider_transactions pt ON pt.id = le.provider_transaction_id LEFT JOIN payout_transactions ppt ON ppt.id = le.payout_transaction_id WHERE `+strings.Join(clauses, " OR ")+` ORDER BY le.created_at`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var record domain.ReconciliationTraceRecord
		var occurred time.Time
		var before, after sql.NullInt64
		var direction, sourceEvent string
		if err := rows.Scan(&record.ID, &record.MerchantID, &record.MerchantCode, &record.OrderNo, &record.MerchantOrderNo, &record.PayoutNo, &record.ProviderTradeNo, &record.Status, &record.AmountCents, &record.Currency, &occurred, &record.CreatedAt, &record.LedgerEntryNo, &direction, &before, &after, &sourceEvent); err != nil {
			return err
		}
		record.RecordType = "ledger_entry"
		record.OccurredAt = &occurred
		record.Details = fmt.Sprintf("direction=%s balance_before=%s balance_after=%s source_event=%s", direction, nullInt64Text(before), nullInt64Text(after), sourceEvent)
		trace.Ledgers = append(trace.Ledgers, record)
	}
	return rows.Err()
}

func (s *MySQLReconciliationStore) populateTraceMismatches(ctx context.Context, trace *domain.ReconciliationTrace, query domain.ReconciliationTraceQuery, orderIDs, payoutIDs []int64) error {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, len(orderIDs)+len(payoutIDs)+1)
	if len(orderIDs) > 0 {
		placeholders, values := int64Placeholders(orderIDs)
		clauses = append(clauses, "entity_id IN ("+placeholders+") AND entity_type IN ('order', 'provider_transaction')")
		args = append(args, values...)
	}
	if len(payoutIDs) > 0 {
		placeholders, values := int64Placeholders(payoutIDs)
		clauses = append(clauses, "entity_id IN ("+placeholders+") AND entity_type = 'payout_order'")
		args = append(args, values...)
	}
	if query.LedgerEntryNo != "" {
		clauses = append(clauses, "resolution_ledger_entry_id = (SELECT id FROM ledger_entries WHERE entry_no = ?)")
		args = append(args, query.LedgerEntryNo)
	}
	if len(clauses) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, mismatch_type, merchant_id, COALESCE(merchant_code, ''), entity_type, entity_id, COALESCE(order_no, ''), COALESCE(payout_no, ''), table_name, field_name, COALESCE(expected_value, ''), COALESCE(actual_value, ''), COALESCE(details, ''), resolution_status, COALESCE(resolution_type, ''), COALESCE(resolution_note, ''), COALESCE(resolution_ledger_entry_id, 0), resolved_at, COALESCE(resolved_by, ''), created_at FROM reconciliation_run_items WHERE `+strings.Join(clauses, " OR ")+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items, err := scanReconciliationItems(rows)
	if err != nil {
		return err
	}
	trace.Mismatches = items
	return nil
}

func scanTraceRecords(rows *sql.Rows, recordType string) ([]domain.ReconciliationTraceRecord, error) {
	defer rows.Close()
	records := make([]domain.ReconciliationTraceRecord, 0)
	for rows.Next() {
		var record domain.ReconciliationTraceRecord
		var occurred sql.NullTime
		if err := rows.Scan(&record.ID, &record.MerchantID, &record.MerchantCode, &record.OrderNo, &record.MerchantOrderNo, &record.PayoutNo, &record.ProviderTradeNo, &record.Status, &record.AmountCents, &record.Currency, &occurred, &record.CreatedAt); err != nil {
			return nil, err
		}
		record.RecordType = recordType
		if occurred.Valid {
			record.OccurredAt = &occurred.Time
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func scanReconciliationItems(rows *sql.Rows) ([]domain.ReconciliationMismatch, error) {
	items := make([]domain.ReconciliationMismatch, 0)
	for rows.Next() {
		var item domain.ReconciliationMismatch
		var merchantID, entityID sql.NullInt64
		var resolvedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.RunID, &item.MismatchType, &merchantID, &item.MerchantCode, &item.EntityType, &entityID, &item.OrderNo, &item.PayoutNo, &item.TableName, &item.FieldName, &item.ExpectedValue, &item.ActualValue, &item.Details, &item.ResolutionStatus, &item.ResolutionType, &item.ResolutionNote, &item.ResolutionLedgerEntryID, &resolvedAt, &item.ResolvedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if merchantID.Valid {
			item.MerchantID = merchantID.Int64
		}
		if entityID.Valid {
			item.EntityID = entityID.Int64
		}
		if resolvedAt.Valid {
			item.ResolvedAt = &resolvedAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func traceWhere(query domain.ReconciliationTraceQuery, columns []string) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, 5)
	if query.MerchantOrderNo != "" {
		clauses = append(clauses, columns[0]+" = ?")
		args = append(args, query.MerchantOrderNo)
	}
	if query.PayoutNo != "" {
		clauses = append(clauses, columns[1]+" = ?")
		args = append(args, query.PayoutNo)
	}
	if query.ProviderTradeNo != "" {
		providerClauses := make([]string, 0, len(columns)-3)
		for _, column := range columns[2 : len(columns)-1] {
			providerClauses = append(providerClauses, column+" = ?")
			args = append(args, query.ProviderTradeNo)
		}
		clauses = append(clauses, "("+strings.Join(providerClauses, " OR ")+")")
	}
	if query.LedgerEntryNo != "" {
		clauses = append(clauses, columns[len(columns)-1]+" = ?")
		args = append(args, query.LedgerEntryNo)
	}
	return strings.Join(clauses, " OR "), args
}

func traceIDs(deposits, payouts []domain.ReconciliationTraceRecord) ([]int64, []int64, []int64) {
	orderIDs := make([]int64, 0, len(deposits))
	payoutIDs := make([]int64, 0, len(payouts))
	merchantIDs := make([]int64, 0, len(deposits)+len(payouts))
	for _, record := range deposits {
		orderIDs = append(orderIDs, record.ID)
		merchantIDs = append(merchantIDs, record.MerchantID)
	}
	for _, record := range payouts {
		payoutIDs = append(payoutIDs, record.ID)
		merchantIDs = append(merchantIDs, record.MerchantID)
	}
	return uniqueInt64s(orderIDs), uniqueInt64s(payoutIDs), uniqueInt64s(merchantIDs)
}

func int64Placeholders(values []int64) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for idx, value := range values {
		placeholders[idx] = "?"
		args[idx] = value
	}
	return strings.Join(placeholders, ","), args
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value != 0 {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	return result
}

func nullInt64Text(value sql.NullInt64) string {
	if !value.Valid {
		return ""
	}
	return strconv.FormatInt(value.Int64, 10)
}

func emptyTraceQuery(query domain.ReconciliationTraceQuery) bool {
	return strings.TrimSpace(query.MerchantOrderNo) == "" && strings.TrimSpace(query.PayoutNo) == "" && strings.TrimSpace(query.ProviderTradeNo) == "" && strings.TrimSpace(query.LedgerEntryNo) == ""
}

func newReconciliationTrace(query domain.ReconciliationTraceQuery) domain.ReconciliationTrace {
	return domain.ReconciliationTrace{Query: query, Balances: make([]domain.ReconciliationBalanceSnapshot, 0), Deposits: make([]domain.ReconciliationTraceRecord, 0), Payouts: make([]domain.ReconciliationTraceRecord, 0), Providers: make([]domain.ReconciliationTraceRecord, 0), Callbacks: make([]domain.ReconciliationTraceRecord, 0), Ledgers: make([]domain.ReconciliationTraceRecord, 0), Mismatches: make([]domain.ReconciliationMismatch, 0)}
}

func hasReportItemFilter(query domain.ReconciliationReportQuery) bool {
	return query.MerchantCode != "" || query.OrderType != "" || query.MismatchType != ""
}

func filterReconciliationItems(items []domain.ReconciliationMismatch, query domain.ReconciliationReportQuery) []domain.ReconciliationMismatch {
	filtered := make([]domain.ReconciliationMismatch, 0, len(items))
	for _, item := range items {
		if query.MerchantCode != "" && item.MerchantCode != query.MerchantCode {
			continue
		}
		if query.MismatchType != "" && item.MismatchType != query.MismatchType {
			continue
		}
		if query.OrderType == domain.ReconciliationOrderTypeDeposit && item.EntityType != "order" && item.EntityType != "provider_transaction" {
			continue
		}
		if query.OrderType == domain.ReconciliationOrderTypePayout && item.EntityType != "payout_order" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func traceMismatchMatches(item domain.ReconciliationMismatch, query domain.ReconciliationTraceQuery) bool {
	return query.PayoutNo != "" && item.PayoutNo == query.PayoutNo || query.MerchantOrderNo != "" && item.OrderNo == query.MerchantOrderNo || query.ProviderTradeNo != "" && strings.Contains(item.Details, query.ProviderTradeNo)
}

func (s *MySQLReconciliationStore) collectMismatches(ctx context.Context, runID int64, filter domain.ReconciliationFilter) ([]domain.ReconciliationMismatch, error) {
	now := time.Now()
	cutoff := now.Add(-reconciliationStuckThreshold)
	mismatches := make([]domain.ReconciliationMismatch, 0)

	pendingBalanceMismatches, err := s.collectPendingBalanceMismatches(ctx, runID, filter)
	if err != nil {
		return nil, err
	}
	mismatches = append(mismatches, pendingBalanceMismatches...)

	availableBalanceMismatches, err := s.collectAvailableBalanceMismatches(ctx, runID, filter)
	if err != nil {
		return nil, err
	}
	mismatches = append(mismatches, availableBalanceMismatches...)

	depositMismatches, err := s.collectDepositMismatches(ctx, runID, filter, cutoff)
	if err != nil {
		return nil, err
	}
	mismatches = append(mismatches, depositMismatches...)

	payoutMismatches, err := s.collectPayoutMismatches(ctx, runID, filter, cutoff)
	if err != nil {
		return nil, err
	}
	mismatches = append(mismatches, payoutMismatches...)

	for _, item := range mismatches {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO reconciliation_run_items (
				run_id, mismatch_type, merchant_id, merchant_code, entity_type, entity_id, order_no, payout_no,
				table_name, field_name, expected_value, actual_value, details, resolution_status, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.RunID, string(item.MismatchType), nullableInt64Value(item.MerchantID), nullableString(item.MerchantCode),
			item.EntityType, nullableInt64Value(item.EntityID), nullableString(item.OrderNo), nullableString(item.PayoutNo),
			item.TableName, item.FieldName, nullableString(item.ExpectedValue), nullableString(item.ActualValue), nullableString(item.Details), string(domain.ReconciliationResolutionStatusOpen), now); err != nil {
			return nil, err
		}
	}
	for idx := range mismatches {
		mismatches[idx].CreatedAt = now
		mismatches[idx].ResolutionStatus = domain.ReconciliationResolutionStatusOpen
	}
	return mismatches, nil
}

func (s *MySQLReconciliationStore) ResolveMismatchWithAdjustment(ctx context.Context, itemID int64, params ReconciliationAdjustmentParams) (domain.ReconciliationMismatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	defer rollback(tx)

	item, err := findReconciliationItemForUpdate(ctx, tx, itemID)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if item.ResolutionStatus == domain.ReconciliationResolutionStatusResolved {
		return domain.ReconciliationMismatch{}, errors.New("reconciliation item is already resolved")
	}
	if item.MerchantID == 0 {
		return domain.ReconciliationMismatch{}, errors.New("reconciliation item is missing merchant context")
	}
	currency := strings.ToUpper(strings.TrimSpace(params.Currency))
	if currency == "" {
		currency = "TWD"
	}
	availableBefore, _, err := ensureMerchantBalanceForUpdate(ctx, tx, item.MerchantID, currency)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	entry := domain.LedgerEntry{
		MerchantID:         item.MerchantID,
		AmountCents:        absInt64(params.AmountCents),
		Direction:          ledgerDirectionForDelta(params.AmountCents),
		Type:               domain.LedgerEntryTypeAdjustment,
		Currency:           currency,
		BalanceBeforeCents: availableBefore,
		BalanceAfterCents:  availableBefore + params.AmountCents,
		ReferenceType:      domain.LedgerReferenceTypeReconciliationItem,
		ReferenceID:        item.ID,
		SourceEvent:        domain.LedgerSourceEventManualAdjustment,
	}
	if params.AmountCents == 0 {
		return domain.ReconciliationMismatch{}, errors.New("adjustment amount must not be zero")
	}
	if err := applyLedgerEntryAndBalanceUpdate(ctx, tx, entry, params.AmountCents, 0); err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	ledgerEntryID, err := findLatestReconciliationResolutionLedgerEntryID(ctx, tx, item.MerchantID, item.ID, domain.LedgerEntryTypeAdjustment)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	resolved, err := resolveReconciliationItemTx(ctx, tx, item, domain.ReconciliationResolutionTypeAdjustment, strings.TrimSpace(params.Note), ledgerEntryID, params.Audit)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	return resolved, nil
}

func (s *MySQLReconciliationStore) ResolveMismatchWithReversal(ctx context.Context, itemID int64, params ReconciliationReversalParams) (domain.ReconciliationMismatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	defer rollback(tx)

	item, err := findReconciliationItemForUpdate(ctx, tx, itemID)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if item.ResolutionStatus == domain.ReconciliationResolutionStatusResolved {
		return domain.ReconciliationMismatch{}, errors.New("reconciliation item is already resolved")
	}
	target, err := findLedgerEntryForUpdate(ctx, tx, params.TargetEntryID)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if item.MerchantID != 0 && target.MerchantID != item.MerchantID {
		return domain.ReconciliationMismatch{}, errors.New("target ledger entry belongs to a different merchant")
	}
	availableBefore, _, err := ensureMerchantBalanceForUpdate(ctx, tx, target.MerchantID, target.Currency)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	delta := target.AmountCents
	if target.Direction == domain.LedgerDirectionCredit {
		delta = -delta
	}
	entry := domain.LedgerEntry{
		MerchantID:            target.MerchantID,
		OrderID:               target.OrderID,
		PayoutOrderID:         target.PayoutOrderID,
		ProviderTransactionID: target.ProviderTransactionID,
		PayoutTransactionID:   target.PayoutTransactionID,
		OrderNo:               target.OrderNo,
		PayoutNo:              target.PayoutNo,
		AmountCents:           target.AmountCents,
		Direction:             oppositeLedgerDirection(target.Direction),
		Type:                  domain.LedgerEntryTypeReversal,
		Currency:              target.Currency,
		BalanceBeforeCents:    availableBefore,
		BalanceAfterCents:     availableBefore + delta,
		ReferenceType:         domain.LedgerReferenceTypeLedgerEntry,
		ReferenceID:           target.ID,
		SourceEvent:           domain.LedgerSourceEventManualReversal,
		ReversalOfEntryID:     target.ID,
	}
	if err := applyLedgerEntryAndBalanceUpdate(ctx, tx, entry, delta, 0); err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	ledgerEntryID, err := findLatestReconciliationResolutionLedgerEntryID(ctx, tx, target.MerchantID, target.ID, domain.LedgerEntryTypeReversal)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	resolved, err := resolveReconciliationItemTx(ctx, tx, item, domain.ReconciliationResolutionTypeReversal, strings.TrimSpace(params.Note), ledgerEntryID, params.Audit)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	return resolved, nil
}

func findReconciliationItemForUpdate(ctx context.Context, tx *sql.Tx, itemID int64) (domain.ReconciliationMismatch, error) {
	var item domain.ReconciliationMismatch
	var merchantID, entityID sql.NullInt64
	var resolutionLedgerEntryID sql.NullInt64
	var resolutionType, resolutionNote, resolvedBy sql.NullString
	var resolvedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT id, run_id, mismatch_type, merchant_id, COALESCE(merchant_code, ''), entity_type, entity_id,
		       COALESCE(order_no, ''), COALESCE(payout_no, ''), table_name, field_name,
		       COALESCE(expected_value, ''), COALESCE(actual_value, ''), COALESCE(details, ''),
		       resolution_status, resolution_type, resolution_note, resolution_ledger_entry_id, resolved_at, resolved_by, created_at
		FROM reconciliation_run_items
		WHERE id = ?
		FOR UPDATE
	`, itemID).Scan(
		&item.ID, &item.RunID, &item.MismatchType, &merchantID, &item.MerchantCode, &item.EntityType, &entityID,
		&item.OrderNo, &item.PayoutNo, &item.TableName, &item.FieldName, &item.ExpectedValue, &item.ActualValue, &item.Details,
		&item.ResolutionStatus, &resolutionType, &resolutionNote, &resolutionLedgerEntryID, &resolvedAt, &resolvedBy, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReconciliationMismatch{}, ErrNotFound
	}
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if merchantID.Valid {
		item.MerchantID = merchantID.Int64
	}
	if entityID.Valid {
		item.EntityID = entityID.Int64
	}
	item.ResolutionType = domain.ReconciliationResolutionType(resolutionType.String)
	item.ResolutionNote = resolutionNote.String
	item.ResolutionLedgerEntryID = resolutionLedgerEntryID.Int64
	item.ResolvedBy = resolvedBy.String
	if resolvedAt.Valid {
		item.ResolvedAt = &resolvedAt.Time
	}
	if item.ResolutionStatus == "" {
		item.ResolutionStatus = domain.ReconciliationResolutionStatusOpen
	}
	return item, nil
}

func findLedgerEntryForUpdate(ctx context.Context, tx *sql.Tx, entryID int64) (domain.LedgerEntry, error) {
	var entry domain.LedgerEntry
	var orderID, payoutOrderID, providerTransactionID, payoutTransactionID, reversalOfEntryID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT le.id, le.merchant_id, le.order_id, le.payout_order_id, le.provider_transaction_id, le.payout_transaction_id,
		       COALESCE(o.order_no, ''), COALESCE(po.payout_no, ''), le.amount_cents, le.direction, le.type, le.currency,
		       le.balance_before_cents, le.balance_after_cents, le.reference_type, le.reference_id, le.source_event,
		       le.reversal_of_entry_id, le.created_at
		FROM ledger_entries le
		LEFT JOIN orders o ON o.id = le.order_id
		LEFT JOIN payout_orders po ON po.id = le.payout_order_id
		WHERE le.id = ?
		FOR UPDATE
	`, entryID).Scan(
		&entry.ID, &entry.MerchantID, &orderID, &payoutOrderID, &providerTransactionID, &payoutTransactionID,
		&entry.OrderNo, &entry.PayoutNo, &entry.AmountCents, &entry.Direction, &entry.Type, &entry.Currency,
		&entry.BalanceBeforeCents, &entry.BalanceAfterCents, &entry.ReferenceType, &entry.ReferenceID, &entry.SourceEvent,
		&reversalOfEntryID, &entry.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LedgerEntry{}, ErrNotFound
	}
	if err != nil {
		return domain.LedgerEntry{}, err
	}
	entry.OrderID = orderID.Int64
	entry.PayoutOrderID = payoutOrderID.Int64
	entry.ProviderTransactionID = providerTransactionID.Int64
	entry.PayoutTransactionID = payoutTransactionID.Int64
	entry.ReversalOfEntryID = reversalOfEntryID.Int64
	return entry, nil
}

func findLatestReconciliationResolutionLedgerEntryID(ctx context.Context, tx *sql.Tx, merchantID, referenceID int64, entryType string) (int64, error) {
	referenceType := domain.LedgerReferenceTypeReconciliationItem
	if entryType == domain.LedgerEntryTypeReversal {
		referenceType = domain.LedgerReferenceTypeLedgerEntry
	}
	var entryID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM ledger_entries
		WHERE merchant_id = ? AND type = ? AND reference_type = ? AND reference_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, merchantID, entryType, referenceType, referenceID).Scan(&entryID)
	return entryID, err
}

func resolveReconciliationItemTx(ctx context.Context, tx *sql.Tx, item domain.ReconciliationMismatch, resolutionType domain.ReconciliationResolutionType, note string, ledgerEntryID int64, audit ReconciliationResolutionAudit) (domain.ReconciliationMismatch, error) {
	actor := strings.TrimSpace(audit.Actor)
	checker := strings.TrimSpace(audit.Checker)
	reason := strings.TrimSpace(audit.Reason)
	requestID := strings.TrimSpace(audit.RequestID)
	if actor == "" || checker == "" || reason == "" || requestID == "" {
		return domain.ReconciliationMismatch{}, errors.New("resolution audit requires actor, checker, reason, and request_id")
	}
	now := time.Now()
	result, err := tx.ExecContext(ctx, `
		UPDATE reconciliation_run_items
		SET resolution_status = ?, resolution_type = ?, resolution_note = ?, resolution_ledger_entry_id = ?, resolved_at = ?, resolved_by = ?
		WHERE id = ? AND resolution_status = ?
	`, string(domain.ReconciliationResolutionStatusResolved), string(resolutionType), nullableString(note), ledgerEntryID, now, actor,
		item.ID, string(domain.ReconciliationResolutionStatusOpen))
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if updated != 1 {
		return domain.ReconciliationMismatch{}, errors.New("reconciliation item is already resolved")
	}

	metadata := audit.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["resolution_note"] = note
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return domain.ReconciliationMismatch{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reconciliation_resolution_actions (
			run_id, reconciliation_item_id, merchant_id, action_type, ledger_entry_id,
			actor, checker, reason, request_id, source_ip, user_agent, metadata
		) VALUES (?, ?, NULLIF(?, 0), ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.RunID, item.ID, item.MerchantID, string(resolutionType), ledgerEntryID,
		actor, checker, reason, requestID, nullableString(strings.TrimSpace(audit.SourceIP)),
		nullableString(strings.TrimSpace(audit.UserAgent)), string(rawMetadata)); err != nil {
		return domain.ReconciliationMismatch{}, err
	}

	item.ResolutionStatus = domain.ReconciliationResolutionStatusResolved
	item.ResolutionType = resolutionType
	item.ResolutionNote = note
	item.ResolutionLedgerEntryID = ledgerEntryID
	item.ResolvedAt = &now
	item.ResolvedBy = actor
	return item, nil
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func ledgerDirectionForDelta(delta int64) string {
	if delta >= 0 {
		return domain.LedgerDirectionCredit
	}
	return domain.LedgerDirectionDebit
}

func oppositeLedgerDirection(direction string) string {
	if direction == domain.LedgerDirectionCredit {
		return domain.LedgerDirectionDebit
	}
	return domain.LedgerDirectionCredit
}

func (s *MySQLReconciliationStore) collectPendingBalanceMismatches(ctx context.Context, runID int64, filter domain.ReconciliationFilter) ([]domain.ReconciliationMismatch, error) {
	where, args := buildMerchantScopedWhere("m.code = ?", filter)
	query := `
		SELECT mb.merchant_id, m.code, mb.currency, mb.pending_cents,
		       COALESCE(SUM(CASE WHEN po.status IN ('pending_review', 'approved', 'submitting', 'processing') THEN po.total_debit_cents ELSE 0 END), 0) AS expected_pending
		FROM merchant_balances mb
		JOIN merchants m ON m.id = mb.merchant_id
		LEFT JOIN payout_orders po ON po.merchant_id = mb.merchant_id AND po.currency = mb.currency
	`
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY mb.merchant_id, m.code, mb.currency, mb.pending_cents"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ReconciliationMismatch
	for rows.Next() {
		var merchantID int64
		var merchantCode, currency string
		var actualPending, expectedPending int64
		if err := rows.Scan(&merchantID, &merchantCode, &currency, &actualPending, &expectedPending); err != nil {
			return nil, err
		}
		if actualPending == expectedPending {
			continue
		}
		items = append(items, domain.ReconciliationMismatch{
			RunID:         runID,
			MismatchType:  domain.ReconciliationMismatchBalanceMismatch,
			MerchantID:    merchantID,
			MerchantCode:  merchantCode,
			EntityType:    "merchant_balance",
			TableName:     "merchant_balances",
			FieldName:     "pending_cents",
			ExpectedValue: strconv.FormatInt(expectedPending, 10),
			ActualValue:   strconv.FormatInt(actualPending, 10),
			Details:       fmt.Sprintf("currency=%s expected from open payout orders", currency),
		})
	}
	return items, rows.Err()
}

func (s *MySQLReconciliationStore) collectAvailableBalanceMismatches(ctx context.Context, runID int64, filter domain.ReconciliationFilter) ([]domain.ReconciliationMismatch, error) {
	where, args := buildMerchantScopedWhere("m.code = ?", filter)
	query := `
		SELECT mb.merchant_id, m.code, mb.currency, mb.available_cents, le.balance_after_cents, le.entry_no
		FROM merchant_balances mb
		JOIN merchants m ON m.id = mb.merchant_id
		JOIN ledger_entries le ON le.id = (
			SELECT le2.id
			FROM ledger_entries le2
			WHERE le2.merchant_id = mb.merchant_id
			  AND le2.currency = mb.currency
			  AND le2.balance_after_cents IS NOT NULL
			ORDER BY le2.created_at DESC, le2.id DESC
			LIMIT 1
		)
	`
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ReconciliationMismatch
	for rows.Next() {
		var merchantID int64
		var merchantCode, currency, entryNo string
		var actualAvailable, expectedAvailable int64
		if err := rows.Scan(&merchantID, &merchantCode, &currency, &actualAvailable, &expectedAvailable, &entryNo); err != nil {
			return nil, err
		}
		if actualAvailable == expectedAvailable {
			continue
		}
		items = append(items, domain.ReconciliationMismatch{
			RunID:         runID,
			MismatchType:  domain.ReconciliationMismatchBalanceMismatch,
			MerchantID:    merchantID,
			MerchantCode:  merchantCode,
			EntityType:    "merchant_balance",
			TableName:     "merchant_balances",
			FieldName:     "available_cents",
			ExpectedValue: strconv.FormatInt(expectedAvailable, 10),
			ActualValue:   strconv.FormatInt(actualAvailable, 10),
			Details:       fmt.Sprintf("currency=%s latest ledger entry=%s", currency, entryNo),
		})
	}
	return items, rows.Err()
}

func (s *MySQLReconciliationStore) collectDepositMismatches(ctx context.Context, runID int64, filter domain.ReconciliationFilter, cutoff time.Time) ([]domain.ReconciliationMismatch, error) {
	where, args := buildDepositWhere(filter)
	query := `
		SELECT o.id, o.merchant_id, m.code, o.order_no, o.status, o.amount_cents, o.updated_at,
		       COALESCE(MAX(pt.status), ''), COALESCE(MAX(pt.provider_trade_no), ''), COALESCE(MAX(pt.updated_at), o.updated_at),
		       COUNT(DISTINCT CASE WHEN le.type = '` + domain.LedgerEntryTypeDepositPaid + `' THEN le.id END) AS deposit_ledger_count
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN ledger_entries le ON le.order_id = o.id
	`
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY o.id, o.merchant_id, m.code, o.order_no, o.status, o.amount_cents, o.updated_at"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ReconciliationMismatch
	for rows.Next() {
		var orderID, merchantID, amountCents int64
		var merchantCode, orderNo, orderStatus, providerStatus, providerTradeNo string
		var orderUpdatedAt, providerUpdatedAt time.Time
		var depositLedgerCount int64
		if err := rows.Scan(&orderID, &merchantID, &merchantCode, &orderNo, &orderStatus, &amountCents, &orderUpdatedAt, &providerStatus, &providerTradeNo, &providerUpdatedAt, &depositLedgerCount); err != nil {
			return nil, err
		}
		if orderStatus == "paid" && depositLedgerCount == 0 {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchMissingLedger, merchantID, merchantCode, "order", orderID, orderNo, "", "ledger_entries", "type", domain.LedgerEntryTypeDepositPaid, "missing", fmt.Sprintf("paid order amount_cents=%d", amountCents)))
		}
		if depositLedgerCount > 1 {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchDuplicateLedger, merchantID, merchantCode, "order", orderID, orderNo, "", "ledger_entries", "type", "1", strconv.FormatInt(depositLedgerCount, 10), "deposit_paid ledger count exceeded expected 1"))
		}
		if orderStatus == "paid" && providerStatus != "paid" {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchProviderState, merchantID, merchantCode, "order", orderID, orderNo, "", "provider_transactions", "status", "paid", providerStatus, "order is paid but provider transaction is not paid"))
		}
		if providerStatus == "paid" && orderStatus != "paid" {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchProviderState, merchantID, merchantCode, "order", orderID, orderNo, "", "orders", "status", "paid", orderStatus, "provider transaction is paid but order status is not paid"))
		}
		if orderStatus == "pending" && orderUpdatedAt.Before(cutoff) {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchStuckPending, merchantID, merchantCode, "order", orderID, orderNo, "", "orders", "status", "resolved within 24h", orderStatus, fmt.Sprintf("updated_at=%s", orderUpdatedAt.Format(time.RFC3339))))
		}
		if providerStatus == "pending" && providerUpdatedAt.Before(cutoff) {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchStuckPending, merchantID, merchantCode, "provider_transaction", orderID, orderNo, "", "provider_transactions", "status", "resolved within 24h", providerStatus, fmt.Sprintf("provider_trade_no=%s updated_at=%s", providerTradeNo, providerUpdatedAt.Format(time.RFC3339))))
		}
	}
	return items, rows.Err()
}

func (s *MySQLReconciliationStore) collectPayoutMismatches(ctx context.Context, runID int64, filter domain.ReconciliationFilter, cutoff time.Time) ([]domain.ReconciliationMismatch, error) {
	where, args := buildPayoutWhere(filter)
	query := `
		SELECT po.id, po.merchant_id, m.code, po.payout_no, po.status, po.total_debit_cents, po.updated_at,
		       COALESCE(po.provider_order_no, ''), COALESCE(po.provider_trade_no, ''),
		       COUNT(DISTINCT CASE WHEN le.type = '` + domain.LedgerEntryTypePayoutHold + `' THEN le.id END) AS hold_count,
		       COUNT(DISTINCT CASE WHEN le.type = '` + domain.LedgerEntryTypePayoutComplete + `' THEN le.id END) AS complete_count,
		       COUNT(DISTINCT CASE WHEN le.type = '` + domain.LedgerEntryTypePayoutRelease + `' THEN le.id END) AS release_count,
		       COUNT(DISTINCT CASE WHEN le.type = '` + domain.LedgerEntryTypeReversal + `' THEN le.id END) AS reversal_count,
		       COUNT(DISTINCT pt.id) AS payout_tx_count,
		       COALESCE(MAX(pt.status), '') AS latest_tx_status
		FROM payout_orders po
		JOIN merchants m ON m.id = po.merchant_id
		LEFT JOIN payout_transactions pt ON pt.payout_order_id = po.id
		LEFT JOIN ledger_entries le ON le.payout_order_id = po.id
	`
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY po.id, po.merchant_id, m.code, po.payout_no, po.status, po.total_debit_cents, po.updated_at, po.provider_order_no, po.provider_trade_no"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.ReconciliationMismatch
	for rows.Next() {
		var payoutID, merchantID, totalDebitCents int64
		var merchantCode, payoutNo, payoutStatus, providerOrderNo, providerTradeNo, latestTxStatus string
		var updatedAt time.Time
		var holdCount, completeCount, releaseCount, reversalCount, payoutTxCount int64
		if err := rows.Scan(&payoutID, &merchantID, &merchantCode, &payoutNo, &payoutStatus, &totalDebitCents, &updatedAt, &providerOrderNo, &providerTradeNo, &holdCount, &completeCount, &releaseCount, &reversalCount, &payoutTxCount, &latestTxStatus); err != nil {
			return nil, err
		}

		items = append(items, compareLedgerExpectation(runID, merchantID, merchantCode, payoutID, "", payoutNo, domain.LedgerEntryTypePayoutHold, holdCount, 1)...)
		expectedComplete := int64(0)
		expectedRelease := int64(0)
		expectedReversal := int64(0)
		switch payoutStatus {
		case "completed":
			expectedComplete = 1
		case "failed", "rejected", "cancelled":
			expectedRelease = 1
		case "reversed":
			expectedComplete = 1
			expectedReversal = 1
		}
		items = append(items, compareLedgerExpectation(runID, merchantID, merchantCode, payoutID, "", payoutNo, domain.LedgerEntryTypePayoutComplete, completeCount, expectedComplete)...)
		items = append(items, compareLedgerExpectation(runID, merchantID, merchantCode, payoutID, "", payoutNo, domain.LedgerEntryTypePayoutRelease, releaseCount, expectedRelease)...)
		items = append(items, compareLedgerExpectation(runID, merchantID, merchantCode, payoutID, "", payoutNo, domain.LedgerEntryTypeReversal, reversalCount, expectedReversal)...)

		if (payoutStatus == "processing" || payoutStatus == "completed" || payoutStatus == "reversed" || payoutStatus == "failed") && payoutTxCount == 0 {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchProviderState, merchantID, merchantCode, "payout_order", payoutID, "", payoutNo, "payout_transactions", "id", ">=1", "0", "payout entered provider lifecycle without payout_transactions record"))
		}
		if (payoutStatus == "processing" || payoutStatus == "completed" || payoutStatus == "reversed" || payoutStatus == "failed") && strings.TrimSpace(providerOrderNo) == "" {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchProviderState, merchantID, merchantCode, "payout_order", payoutID, "", payoutNo, "payout_orders", "provider_order_no", "non-empty", providerOrderNo, "provider-facing payout should carry provider_order_no"))
		}
		if (payoutStatus == "completed" || payoutStatus == "reversed") && strings.TrimSpace(providerTradeNo) == "" {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchProviderState, merchantID, merchantCode, "payout_order", payoutID, "", payoutNo, "payout_orders", "provider_trade_no", "non-empty", providerTradeNo, "terminal payout should carry provider_trade_no"))
		}
		if payoutStatus == "processing" && latestTxStatus == "failed" {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchProviderState, merchantID, merchantCode, "payout_order", payoutID, "", payoutNo, "payout_transactions", "status", "submitted", latestTxStatus, "processing payout has latest payout_transaction marked failed"))
		}
		if (payoutStatus == "approved" || payoutStatus == "submitting" || payoutStatus == "processing") && updatedAt.Before(cutoff) {
			items = append(items, buildReconciliationItem(runID, domain.ReconciliationMismatchStuckPending, merchantID, merchantCode, "payout_order", payoutID, "", payoutNo, "payout_orders", "status", "resolved within 24h", payoutStatus, fmt.Sprintf("updated_at=%s total_debit_cents=%d", updatedAt.Format(time.RFC3339), totalDebitCents)))
		}
	}
	return items, rows.Err()
}

func hasReconciliationFilter(filter domain.ReconciliationFilter) bool {
	return strings.TrimSpace(filter.MerchantCode) != "" || strings.TrimSpace(filter.OrderNo) != "" || strings.TrimSpace(filter.PayoutNo) != ""
}

func buildMerchantScopedWhere(merchantCondition string, filter domain.ReconciliationFilter) (string, []any) {
	var conditions []string
	var args []any
	if merchant := strings.TrimSpace(filter.MerchantCode); merchant != "" {
		conditions = append(conditions, merchantCondition)
		args = append(args, merchant)
	}
	return strings.Join(conditions, " AND "), args
}

func buildDepositWhere(filter domain.ReconciliationFilter) (string, []any) {
	var conditions []string
	var args []any
	if merchant := strings.TrimSpace(filter.MerchantCode); merchant != "" {
		conditions = append(conditions, "m.code = ?")
		args = append(args, merchant)
	}
	if orderNo := strings.TrimSpace(filter.OrderNo); orderNo != "" {
		conditions = append(conditions, "o.order_no = ?")
		args = append(args, orderNo)
	}
	return strings.Join(conditions, " AND "), args
}

func buildPayoutWhere(filter domain.ReconciliationFilter) (string, []any) {
	var conditions []string
	var args []any
	if merchant := strings.TrimSpace(filter.MerchantCode); merchant != "" {
		conditions = append(conditions, "m.code = ?")
		args = append(args, merchant)
	}
	if payoutNo := strings.TrimSpace(filter.PayoutNo); payoutNo != "" {
		conditions = append(conditions, "po.payout_no = ?")
		args = append(args, payoutNo)
	}
	return strings.Join(conditions, " AND "), args
}

func buildReconciliationItem(runID int64, mismatchType domain.ReconciliationMismatchType, merchantID int64, merchantCode, entityType string, entityID int64, orderNo, payoutNo, tableName, fieldName, expected, actual, details string) domain.ReconciliationMismatch {
	return domain.ReconciliationMismatch{
		RunID:         runID,
		MismatchType:  mismatchType,
		MerchantID:    merchantID,
		MerchantCode:  merchantCode,
		EntityType:    entityType,
		EntityID:      entityID,
		OrderNo:       strings.TrimSpace(orderNo),
		PayoutNo:      strings.TrimSpace(payoutNo),
		TableName:     tableName,
		FieldName:     fieldName,
		ExpectedValue: expected,
		ActualValue:   actual,
		Details:       details,
	}
}

func compareLedgerExpectation(runID, merchantID int64, merchantCode string, payoutID int64, orderNo, payoutNo, ledgerType string, actual, expected int64) []domain.ReconciliationMismatch {
	if actual == expected {
		return nil
	}
	if actual < expected {
		return []domain.ReconciliationMismatch{
			buildReconciliationItem(runID, domain.ReconciliationMismatchMissingLedger, merchantID, merchantCode, "payout_order", payoutID, orderNo, payoutNo, "ledger_entries", "type", ledgerType+" x"+strconv.FormatInt(expected, 10), ledgerType+" x"+strconv.FormatInt(actual, 10), "expected ledger entry is missing"),
		}
	}
	return []domain.ReconciliationMismatch{
		buildReconciliationItem(runID, domain.ReconciliationMismatchDuplicateLedger, merchantID, merchantCode, "payout_order", payoutID, orderNo, payoutNo, "ledger_entries", "type", ledgerType+" x"+strconv.FormatInt(expected, 10), ledgerType+" x"+strconv.FormatInt(actual, 10), "ledger entry count exceeded expected count"),
	}
}

func nullableInt64Value(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
