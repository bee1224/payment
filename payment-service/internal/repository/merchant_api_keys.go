package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type MerchantAPIKeyRecord struct {
	ID               int64
	MerchantID       int64
	KeyHash          string
	SecretCiphertext string
	Status           string
	IsPrimary        bool
	LastUsedAt       *time.Time
	LastRotatedAt    time.Time
	ExpiresAt        *time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func hashMerchantAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}

func isLikelySHA256Hex(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
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

func merchantAPIKeyMatches(storedSecret, providedAPIKey string) bool {
	storedSecret = strings.TrimSpace(storedSecret)
	providedAPIKey = strings.TrimSpace(providedAPIKey)
	if storedSecret == "" || providedAPIKey == "" {
		return false
	}
	if isLikelySHA256Hex(storedSecret) {
		return subtle.ConstantTimeCompare([]byte(strings.ToLower(storedSecret)), []byte(hashMerchantAPIKey(providedAPIKey))) == 1
	}
	return subtle.ConstantTimeCompare([]byte(storedSecret), []byte(providedAPIKey)) == 1
}

func syncMerchantAPIKeyTx(ctx context.Context, tx *sql.Tx, merchantID int64, apiKey, secretCiphertext string) error {
	apiKey = strings.TrimSpace(apiKey)
	if merchantID == 0 || apiKey == "" {
		return nil
	}

	var existingCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM merchant_api_keys
		WHERE merchant_id = ?
	`, merchantID).Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		if strings.TrimSpace(secretCiphertext) != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE merchant_api_keys
				SET secret_ciphertext = CASE
					WHEN secret_ciphertext IS NULL OR secret_ciphertext = '' THEN ?
					ELSE secret_ciphertext
				END,
				updated_at = CURRENT_TIMESTAMP
				WHERE merchant_id = ? AND key_hash = ?
			`, secretCiphertext, merchantID, hashMerchantAPIKey(apiKey)); err != nil {
				return err
			}
		}
		return nil
	}

	keyHash := hashMerchantAPIKey(apiKey)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchant_api_keys (merchant_id, key_hash, secret_ciphertext, status, is_primary, last_rotated_at, revoked_at)
		VALUES (?, ?, ?, 'active', TRUE, CURRENT_TIMESTAMP, NULL)
		ON DUPLICATE KEY UPDATE
			secret_ciphertext = CASE
				WHEN VALUES(secret_ciphertext) IS NULL OR VALUES(secret_ciphertext) = '' THEN secret_ciphertext
				ELSE VALUES(secret_ciphertext)
			END,
			status = 'active',
			is_primary = TRUE,
			last_rotated_at = CURRENT_TIMESTAMP,
			revoked_at = NULL,
			updated_at = CURRENT_TIMESTAMP
	`, merchantID, keyHash, nullableString(strings.TrimSpace(secretCiphertext))); err != nil {
		return err
	}
	return nil
}

func GenerateMerchantAPIKey() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

func issueMerchantAPIKeyTx(ctx context.Context, tx *sql.Tx, merchantID int64, apiKey, secretCiphertext string, expiresAt, previousExpiresAt *time.Time) error {
	apiKey = strings.TrimSpace(apiKey)
	if merchantID == 0 || apiKey == "" {
		return errors.New("api_key is required")
	}
	keyHash := hashMerchantAPIKey(apiKey)
	if _, err := tx.ExecContext(ctx, `
		UPDATE merchant_api_keys
		SET is_primary = FALSE,
		    expires_at = CASE
		    	WHEN ? IS NULL THEN expires_at
		    	WHEN revoked_at IS NOT NULL THEN expires_at
		    	WHEN expires_at IS NULL OR expires_at > ? THEN ?
		    	ELSE expires_at
		    END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ?
		  AND status = 'active'
		  AND revoked_at IS NULL
		  AND is_primary = TRUE
	`, nullableTime(previousExpiresAt), nullableTime(previousExpiresAt), nullableTime(previousExpiresAt), merchantID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO merchant_api_keys (merchant_id, key_hash, secret_ciphertext, status, is_primary, last_rotated_at, expires_at, revoked_at)
		VALUES (?, ?, ?, 'active', TRUE, CURRENT_TIMESTAMP, ?, NULL)
		ON DUPLICATE KEY UPDATE
			secret_ciphertext = CASE
				WHEN VALUES(secret_ciphertext) IS NULL OR VALUES(secret_ciphertext) = '' THEN secret_ciphertext
				ELSE VALUES(secret_ciphertext)
			END,
			status = 'active',
			is_primary = TRUE,
			last_rotated_at = CURRENT_TIMESTAMP,
			expires_at = VALUES(expires_at),
			revoked_at = NULL,
			updated_at = CURRENT_TIMESTAMP
	`, merchantID, keyHash, nullableString(strings.TrimSpace(secretCiphertext)), nullableTime(expiresAt))
	return err
}

func findMerchantAPIKeyRecordTx(ctx context.Context, tx *sql.Tx, merchantID int64, apiKey string) (MerchantAPIKeyRecord, error) {
	apiKey = strings.TrimSpace(apiKey)
	if merchantID == 0 || apiKey == "" {
		return MerchantAPIKeyRecord{}, ErrNotFound
	}
	keyHash := hashMerchantAPIKey(apiKey)

	var record MerchantAPIKeyRecord
	var lastUsedAt, expiresAt, revokedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT id, merchant_id, key_hash, COALESCE(secret_ciphertext, ''), status, is_primary, last_used_at, last_rotated_at, expires_at, revoked_at, created_at, updated_at
		FROM merchant_api_keys
		WHERE merchant_id = ? AND key_hash = ?
		LIMIT 1
	`, merchantID, keyHash).Scan(
		&record.ID,
		&record.MerchantID,
		&record.KeyHash,
		&record.SecretCiphertext,
		&record.Status,
		&record.IsPrimary,
		&lastUsedAt,
		&record.LastRotatedAt,
		&expiresAt,
		&revokedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MerchantAPIKeyRecord{}, ErrNotFound
	}
	if err != nil {
		return MerchantAPIKeyRecord{}, err
	}
	if lastUsedAt.Valid {
		record.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		record.ExpiresAt = &expiresAt.Time
	}
	if revokedAt.Valid {
		record.RevokedAt = &revokedAt.Time
	}
	return record, nil
}

func revokeMerchantAPIKeyTx(ctx context.Context, tx *sql.Tx, merchantID int64, apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if merchantID == 0 || apiKey == "" {
		return errors.New("api_key is required")
	}
	keyHash := hashMerchantAPIKey(apiKey)
	result, err := tx.ExecContext(ctx, `
		UPDATE merchant_api_keys
		SET status = 'revoked',
		    is_primary = FALSE,
		    revoked_at = COALESCE(revoked_at, CURRENT_TIMESTAMP),
		    updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ?
		  AND key_hash = ?
		  AND status <> 'revoked'
	`, merchantID, keyHash)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func listMerchantAPIKeysTx(ctx context.Context, tx *sql.Tx, merchantID int64) ([]MerchantAPIKeyRecord, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, merchant_id, key_hash, COALESCE(secret_ciphertext, ''), status, is_primary, last_used_at, last_rotated_at, expires_at, revoked_at, created_at, updated_at
		FROM merchant_api_keys
		WHERE merchant_id = ?
		ORDER BY is_primary DESC, created_at DESC, id DESC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MerchantAPIKeyRecord
	for rows.Next() {
		var record MerchantAPIKeyRecord
		var lastUsedAt, expiresAt, revokedAt sql.NullTime
		if err := rows.Scan(
			&record.ID,
			&record.MerchantID,
			&record.KeyHash,
			&record.SecretCiphertext,
			&record.Status,
			&record.IsPrimary,
			&lastUsedAt,
			&record.LastRotatedAt,
			&expiresAt,
			&revokedAt,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastUsedAt.Valid {
			record.LastUsedAt = &lastUsedAt.Time
		}
		if expiresAt.Valid {
			record.ExpiresAt = &expiresAt.Time
		}
		if revokedAt.Valid {
			record.RevokedAt = &revokedAt.Time
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}
