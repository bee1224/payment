package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"payment-service/internal/domain"
)

type PayoutOperationalAlertUpsert struct {
	MerchantID    int64
	PayoutOrderID int64
	Category      string
	Severity      string
	Summary       string
	Details       string
}

type PayoutOperationalAlertResolve struct {
	ResolvedBy    string
	ResolveReason string
}

func upsertPayoutOperationalAlertTx(ctx context.Context, tx *sql.Tx, alert PayoutOperationalAlertUpsert, now time.Time) error {
	if alert.MerchantID == 0 || alert.PayoutOrderID == 0 {
		return nil
	}
	category := strings.TrimSpace(alert.Category)
	if category == "" {
		return nil
	}
	severity := strings.TrimSpace(alert.Severity)
	if severity == "" {
		severity = "warning"
	}
	var existingID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM payout_operational_alerts
		WHERE payout_order_id = ? AND category = ? AND status = 'open'
		LIMIT 1
	`, alert.PayoutOrderID, category).Scan(&existingID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE payout_operational_alerts
			SET severity = ?, summary = ?, details = ?, occurrence_count = occurrence_count + 1,
			    last_occurred_at = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, severity, strings.TrimSpace(alert.Summary), nullableString(strings.TrimSpace(alert.Details)), now, existingID)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO payout_operational_alerts (
			merchant_id, payout_order_id, category, severity, status, summary, details, occurrence_count, first_occurred_at, last_occurred_at
		) VALUES (?, ?, ?, ?, 'open', ?, ?, 1, ?, ?)
	`, alert.MerchantID, alert.PayoutOrderID, category, severity, strings.TrimSpace(alert.Summary), nullableString(strings.TrimSpace(alert.Details)), now, now)
	return err
}

func listPayoutOperationalAlertsTx(ctx context.Context, tx *sql.Tx, status string, limit int) ([]domain.PayoutOperationalAlert, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT pa.id, pa.merchant_id, pa.payout_order_id, po.payout_no, pa.category, pa.severity, pa.status,
		       pa.summary, COALESCE(pa.details, ''), pa.occurrence_count, pa.first_occurred_at, pa.last_occurred_at,
		       pa.resolved_at, COALESCE(pa.resolved_by, ''), COALESCE(pa.resolve_reason, '')
		FROM payout_operational_alerts pa
		JOIN payout_orders po ON po.id = pa.payout_order_id
	`
	args := []any{}
	if trimmed := strings.TrimSpace(status); trimmed != "" {
		query += " WHERE pa.status = ?"
		args = append(args, trimmed)
	}
	query += " ORDER BY pa.last_occurred_at DESC, pa.id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var alerts []domain.PayoutOperationalAlert
	for rows.Next() {
		var alert domain.PayoutOperationalAlert
		var resolvedAt sql.NullTime
		if err := rows.Scan(
			&alert.ID, &alert.MerchantID, &alert.PayoutOrderID, &alert.PayoutNo, &alert.Category, &alert.Severity,
			&alert.Status, &alert.Summary, &alert.Details, &alert.OccurrenceCount, &alert.FirstOccurredAt,
			&alert.LastOccurredAt, &resolvedAt, &alert.ResolvedBy, &alert.ResolveReason,
		); err != nil {
			return nil, err
		}
		if resolvedAt.Valid {
			alert.ResolvedAt = &resolvedAt.Time
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func resolvePayoutOperationalAlertTx(ctx context.Context, tx *sql.Tx, alertID int64, resolve PayoutOperationalAlertResolve, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE payout_operational_alerts
		SET status = 'resolved', resolved_at = ?, resolved_by = ?, resolve_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'open'
	`, now, nullableString(strings.TrimSpace(resolve.ResolvedBy)), nullableString(strings.TrimSpace(resolve.ResolveReason)), alertID)
	return err
}
