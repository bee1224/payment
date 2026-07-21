package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type CallbackSigningKey struct {
	MerchantID   int64
	MerchantCode string
	KeyID        string
	Secret       string
}

type CallbackSigningKeyResolver interface {
	ResolveCurrentCallbackSigningKey(context.Context, int64) (CallbackSigningKey, error)
}

type MySQLCallbackSigningKeyStore struct {
	db     *sql.DB
	cipher MerchantSecretCipher
}

const resolveCurrentCallbackSigningKeySQL = `
		SELECT k.key_id, k.secret_ciphertext, m.code
		FROM merchant_callback_signing_keys k JOIN merchants m ON m.id = k.merchant_id
		WHERE k.merchant_id = ? AND k.status = 'active' AND k.is_primary = TRUE AND k.revoked_at IS NULL
		ORDER BY k.id DESC LIMIT 1
	`

func NewMySQLCallbackSigningKeyStore(db *sql.DB, cipher MerchantSecretCipher) *MySQLCallbackSigningKeyStore {
	return &MySQLCallbackSigningKeyStore{db: db, cipher: cipher}
}

func (s *MySQLCallbackSigningKeyStore) ResolveCurrentCallbackSigningKey(ctx context.Context, merchantID int64) (CallbackSigningKey, error) {
	var keyID, ciphertext, merchantCode string
	err := s.db.QueryRowContext(ctx, resolveCurrentCallbackSigningKeySQL, merchantID).Scan(&keyID, &ciphertext, &merchantCode)
	if errors.Is(err, sql.ErrNoRows) {
		return CallbackSigningKey{}, ErrNotFound
	}
	if err != nil {
		return CallbackSigningKey{}, err
	}
	secret, err := s.cipher.Decrypt(ciphertext)
	if err != nil {
		return CallbackSigningKey{}, err
	}
	if strings.TrimSpace(keyID) == "" || strings.TrimSpace(secret) == "" {
		return CallbackSigningKey{}, ErrNotFound
	}
	return CallbackSigningKey{MerchantID: merchantID, MerchantCode: merchantCode, KeyID: keyID, Secret: secret}, nil
}

func SeedMerchantCallbackSigningKey(ctx context.Context, db *sql.DB, merchantCode, keyID, secret string, cipher MerchantSecretCipher) error {
	merchantCode, keyID, secret = strings.TrimSpace(merchantCode), strings.TrimSpace(keyID), strings.TrimSpace(secret)
	if merchantCode == "" || keyID == "" || secret == "" {
		return nil
	}
	if !cipher.Enabled() {
		return fmt.Errorf("merchant callback signing secret encryption is not configured")
	}
	ciphertext, err := cipher.Encrypt(secret)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var merchantID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM merchants WHERE code = ? LIMIT 1`, merchantCode).Scan(&merchantID); err != nil {
		return err
	}
	var existingCiphertext string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(secret_ciphertext, '') FROM merchant_callback_signing_keys WHERE merchant_id = ? AND key_id = ? LIMIT 1`, merchantID, keyID).Scan(&existingCiphertext)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		existingSecret, decryptErr := cipher.Decrypt(existingCiphertext)
		if decryptErr != nil || existingSecret != secret {
			return fmt.Errorf("callback signing key_id %q already exists with a different secret; use a new key_id for rotation", keyID)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE merchant_callback_signing_keys SET is_primary = FALSE, status = 'previous', previous_expires_at = DATE_ADD(CURRENT_TIMESTAMP, INTERVAL 7 DAY), updated_at = CURRENT_TIMESTAMP WHERE merchant_id = ? AND is_primary = TRUE AND revoked_at IS NULL`, merchantID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO merchant_callback_signing_keys (merchant_id, key_id, secret_ciphertext, status, is_primary) VALUES (?, ?, ?, 'active', TRUE)`, merchantID, keyID, ciphertext); err != nil {
			return err
		}
	}
	return tx.Commit()
}
