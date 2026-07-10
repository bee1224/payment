package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"payment-service/internal/domain"
)

type MerchantBootstrap struct {
	Code              string
	Name              string
	APIKey            string
	CallbackURL       string
	InitialBalanceTWD int64
}

func (m MerchantBootstrap) Enabled() bool {
	return strings.TrimSpace(m.Code) != "" && strings.TrimSpace(m.APIKey) != ""
}

func (m MerchantBootstrap) Merchant() domain.Merchant {
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = strings.TrimSpace(m.Code)
	}
	now := time.Now()
	return domain.Merchant{
		Code:        strings.TrimSpace(m.Code),
		Name:        name,
		APIKey:      strings.TrimSpace(m.APIKey),
		Status:      "active",
		CallbackURL: strings.TrimSpace(m.CallbackURL),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func SeedMerchantInDB(ctx context.Context, db *sql.DB, bootstrap MerchantBootstrap) error {
	if db == nil || !bootstrap.Enabled() {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	merchant := bootstrap.Merchant()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchants (code, name, api_key_hash, status, callback_url)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			api_key_hash = VALUES(api_key_hash),
			status = VALUES(status),
			callback_url = CASE
				WHEN VALUES(callback_url) IS NULL OR VALUES(callback_url) = '' THEN callback_url
				ELSE VALUES(callback_url)
			END,
			updated_at = CURRENT_TIMESTAMP
	`, merchant.Code, merchant.Name, merchant.APIKey, merchant.Status, nullableString(merchant.CallbackURL)); err != nil {
		return err
	}

	var merchantID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM merchants WHERE code = ? LIMIT 1`, merchant.Code).Scan(&merchantID); err != nil {
		return err
	}

	initialBalanceCents := bootstrap.InitialBalanceTWD * 100
	if initialBalanceCents < 0 {
		return fmt.Errorf("merchant initial balance must be zero or greater")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchant_balances (merchant_id, currency, available_cents, pending_cents)
		VALUES (?, 'TWD', ?, 0)
		ON DUPLICATE KEY UPDATE
			available_cents = GREATEST(available_cents, VALUES(available_cents)),
			updated_at = CURRENT_TIMESTAMP
	`, merchantID, initialBalanceCents); err != nil {
		return err
	}

	return tx.Commit()
}
