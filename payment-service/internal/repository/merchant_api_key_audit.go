package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type MerchantAPIKeyAuditEntry struct {
	ID               int64
	MerchantID       int64
	MerchantAPIKeyID *int64
	Action           string
	KeyHash          string
	Actor            string
	Reason           string
	RequestID        string
	SourceIP         string
	UserAgent        string
	Metadata         string
	CreatedAt        time.Time
}

type MerchantAPIKeyAuditLog struct {
	Action    string
	KeyHash   string
	Actor     string
	Reason    string
	RequestID string
	SourceIP  string
	UserAgent string
	Metadata  map[string]any
}

func insertMerchantAPIKeyAuditLogTx(ctx context.Context, tx *sql.Tx, merchantID int64, log MerchantAPIKeyAuditLog) error {
	if merchantID == 0 {
		return nil
	}

	keyHash := strings.ToLower(strings.TrimSpace(log.KeyHash))
	if keyHash == "" {
		return nil
	}
	actor := strings.TrimSpace(log.Actor)
	if actor == "" {
		actor = "system"
	}

	var merchantAPIKeyID any
	var keyID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM merchant_api_keys
		WHERE merchant_id = ? AND key_hash = ?
		ORDER BY id DESC
		LIMIT 1
	`, merchantID, keyHash).Scan(&keyID)
	if err == nil {
		merchantAPIKeyID = keyID
	} else if err != sql.ErrNoRows {
		return err
	}

	var metadata any
	if len(log.Metadata) > 0 {
		raw, err := json.Marshal(log.Metadata)
		if err != nil {
			return err
		}
		metadata = string(raw)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO merchant_api_key_audit_logs (
			merchant_id, merchant_api_key_id, action, key_hash, actor, reason, request_id, source_ip, user_agent, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, merchantID, merchantAPIKeyID, strings.TrimSpace(log.Action), keyHash, actor, nullableString(strings.TrimSpace(log.Reason)),
		nullableString(strings.TrimSpace(log.RequestID)), nullableString(strings.TrimSpace(log.SourceIP)),
		nullableString(strings.TrimSpace(log.UserAgent)), metadata)
	return err
}

func listMerchantAPIKeyAuditLogsTx(ctx context.Context, tx *sql.Tx, merchantID int64, limit int) ([]MerchantAPIKeyAuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, merchant_id, merchant_api_key_id, action, key_hash, actor, COALESCE(reason, ''), COALESCE(request_id, ''),
		       COALESCE(source_ip, ''), COALESCE(user_agent, ''), COALESCE(CAST(metadata AS CHAR), ''), created_at
		FROM merchant_api_key_audit_logs
		WHERE merchant_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MerchantAPIKeyAuditEntry
	for rows.Next() {
		var entry MerchantAPIKeyAuditEntry
		var merchantAPIKeyID sql.NullInt64
		if err := rows.Scan(
			&entry.ID,
			&entry.MerchantID,
			&merchantAPIKeyID,
			&entry.Action,
			&entry.KeyHash,
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
		if merchantAPIKeyID.Valid {
			entry.MerchantAPIKeyID = &merchantAPIKeyID.Int64
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
