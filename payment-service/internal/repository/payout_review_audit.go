package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type PayoutReviewAuditEntry struct {
	ID            int64
	MerchantID    int64
	PayoutOrderID int64
	Action        string
	Actor         string
	Reason        string
	RequestID     string
	SourceIP      string
	UserAgent     string
	Metadata      string
	CreatedAt     time.Time
}

type PayoutReviewAuditLog struct {
	Action    string
	Actor     string
	Reason    string
	RequestID string
	SourceIP  string
	UserAgent string
	Metadata  map[string]any
}

func insertPayoutReviewAuditLogTx(ctx context.Context, tx *sql.Tx, merchantID, payoutOrderID int64, log PayoutReviewAuditLog) error {
	if merchantID == 0 || payoutOrderID == 0 {
		return nil
	}
	actor := strings.TrimSpace(log.Actor)
	if actor == "" {
		actor = "system"
	}
	var metadata any
	if len(log.Metadata) > 0 {
		raw, err := json.Marshal(log.Metadata)
		if err != nil {
			return err
		}
		metadata = string(raw)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO payout_review_audit_logs (
			merchant_id, payout_order_id, action, actor, reason, request_id, source_ip, user_agent, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, merchantID, payoutOrderID, strings.TrimSpace(log.Action), actor, nullableString(strings.TrimSpace(log.Reason)),
		nullableString(strings.TrimSpace(log.RequestID)), nullableString(strings.TrimSpace(log.SourceIP)),
		nullableString(strings.TrimSpace(log.UserAgent)), metadata)
	return err
}

func listPayoutReviewAuditLogsTx(ctx context.Context, tx *sql.Tx, payoutOrderID int64, limit int) ([]PayoutReviewAuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, merchant_id, payout_order_id, action, actor, COALESCE(reason, ''), COALESCE(request_id, ''),
		       COALESCE(source_ip, ''), COALESCE(user_agent, ''), COALESCE(CAST(metadata AS CHAR), ''), created_at
		FROM payout_review_audit_logs
		WHERE payout_order_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, payoutOrderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []PayoutReviewAuditEntry
	for rows.Next() {
		var entry PayoutReviewAuditEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.MerchantID,
			&entry.PayoutOrderID,
			&entry.Action,
			&entry.Actor,
			&entry.Reason,
			&entry.RequestID,
			&entry.SourceIP,
			&entry.UserAgent,
			&entry.Metadata,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
