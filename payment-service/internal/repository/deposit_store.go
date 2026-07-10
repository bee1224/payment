package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/provider"
)

var ErrNotFound = errors.New("not found")

type DepositStore interface {
	CreateDepositOrder(ctx context.Context, order domain.DepositOrder, itemDesc string) (domain.DepositOrder, error)
	SaveDepositPaymentRequest(ctx context.Context, order domain.DepositOrder, payment provider.DepositPaymentRequest) error
	FindDepositOrderByOrderNo(ctx context.Context, orderNo string) (domain.DepositOrder, error)
	FindDepositOrderByMerchantOrderNo(ctx context.Context, merchantCode, merchantOrderNo string) (domain.DepositOrder, error)
	RecordDepositNotificationFailure(ctx context.Context, providerCode string, fields map[string]string, errorMessage string) error
	ApplyDepositNotification(ctx context.Context, providerCode string, notification provider.DepositNotification) (domain.DepositOrder, *domain.LedgerEntry, error)
}

type MySQLDepositStore struct {
	db *sql.DB
}

type storedDepositPaymentRequestPayload struct {
	Payment provider.DepositPaymentRequest `json:"payment"`
	RY     storedRYDepositRequestPayload `json:"ry"`
}

type storedRYDepositRequestPayload struct {
	CallbackURL  string   `json:"callback_url,omitempty"`
	BankAccounts []string `json:"bank_account,omitempty"`
	StoreNumbers []string `json:"store_number,omitempty"`
	UserName     string   `json:"user_name,omitempty"`
	BankID       string   `json:"bank_id,omitempty"`
	PayCurrency  string   `json:"pay_currency,omitempty"`
	Mobile       string   `json:"mobile,omitempty"`
	IDNo         string   `json:"id_no,omitempty"`
}

func NewMySQLDepositStore(db *sql.DB) *MySQLDepositStore {
	return &MySQLDepositStore{db: db}
}

func (s *MySQLDepositStore) CreateDepositOrder(ctx context.Context, order domain.DepositOrder, itemDesc string) (domain.DepositOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.DepositOrder{}, err
	}
	defer rollback(tx)

	merchantID, err := ensureMerchant(ctx, tx, order.MerchantCode, order.CallbackURL)
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.MerchantID = merchantID

	providerCode := normalizeProviderCode(order.Provider)
	providerID, err := ensureProvider(ctx, tx, providerCode)
	if err != nil {
		return domain.DepositOrder{}, err
	}

	channelID, err := ensureChannel(ctx, tx, providerID, order.ChannelCode)
	if err != nil {
		return domain.DepositOrder{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO orders (
			merchant_id, channel_id, order_no, merchant_order_no, amount_cents, currency, status, item_desc, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.MerchantID, channelID, order.OrderNo, order.MerchantOrderNo, order.AmountCents, order.Currency, string(order.Status), itemDesc, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.ID, err = result.LastInsertId()
	if err != nil {
		return domain.DepositOrder{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_transactions (
			order_id, provider_id, provider_order_no, status, amount_cents, currency
		) VALUES (?, ?, ?, ?, ?, ?)
	`, order.ID, providerID, order.OrderNo, string(order.Status), order.AmountCents, order.Currency); err != nil {
		return domain.DepositOrder{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.DepositOrder{}, err
	}
	return order, nil
}

func (s *MySQLDepositStore) SaveDepositPaymentRequest(ctx context.Context, order domain.DepositOrder, payment provider.DepositPaymentRequest) error {
	payload, err := json.Marshal(storedDepositPaymentRequestPayload{
		Payment: payment,
		RY: storedRYDepositRequestPayload{
			CallbackURL:  order.CallbackURL,
			BankAccounts: append([]string(nil), order.BankAccounts...),
			StoreNumbers: append([]string(nil), order.StoreNumbers...),
			UserName:     order.UserName,
			BankID:       order.BankID,
			PayCurrency:  order.PayCurrency,
			Mobile:       order.Mobile,
			IDNo:         order.IDNo,
		},
	})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE provider_transactions
		SET request_payload = ?, updated_at = CURRENT_TIMESTAMP
		WHERE provider_order_no = ?
	`, payload, order.OrderNo)
	return err
}

func (s *MySQLDepositStore) FindDepositOrderByOrderNo(ctx context.Context, orderNo string) (domain.DepositOrder, error) {
	var order domain.DepositOrder
	var status string
	var requestPayload []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.created_at, o.updated_at, pt.request_payload
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN payment_channels pc ON pc.id = o.channel_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN payment_providers p ON p.id = pt.provider_id
		WHERE o.order_no = ?
		LIMIT 1
	`, orderNo).Scan(
		&order.ID, &order.MerchantID, &order.MerchantCode, &order.CallbackURL, &order.ChannelCode, &order.OrderNo, &order.MerchantOrderNo,
		&order.Provider, &order.ProviderTradeNo,
		&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.Status = domain.DepositOrderStatus(status)
	applyStoredRYDepositRequestPayload(&order, requestPayload)
	return order, nil
}

func (s *MySQLDepositStore) FindDepositOrderByMerchantOrderNo(ctx context.Context, merchantCode, merchantOrderNo string) (domain.DepositOrder, error) {
	var order domain.DepositOrder
	var status string
	var requestPayload []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.created_at, o.updated_at, pt.request_payload
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN payment_channels pc ON pc.id = o.channel_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN payment_providers p ON p.id = pt.provider_id
		WHERE m.code = ? AND o.merchant_order_no = ?
		LIMIT 1
	`, merchantCode, merchantOrderNo).Scan(
		&order.ID, &order.MerchantID, &order.MerchantCode, &order.CallbackURL, &order.ChannelCode, &order.OrderNo, &order.MerchantOrderNo,
		&order.Provider, &order.ProviderTradeNo,
		&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.Status = domain.DepositOrderStatus(status)
	applyStoredRYDepositRequestPayload(&order, requestPayload)
	return order, nil
}

func (s *MySQLDepositStore) RecordDepositNotificationFailure(ctx context.Context, providerCode string, fields map[string]string, errorMessage string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	providerID, err := ensureProvider(ctx, tx, normalizeProviderCode(providerCode))
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"fields": fields})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_callbacks (
			provider_id, payload, status, error_message, processed_at
		) VALUES (?, ?, 'failed', ?, CURRENT_TIMESTAMP)
	`, providerID, payload, errorMessage); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLDepositStore) ApplyDepositNotification(ctx context.Context, providerCode string, notification provider.DepositNotification) (domain.DepositOrder, *domain.LedgerEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.DepositOrder{}, nil, err
	}
	defer rollback(tx)

	providerCode = normalizeProviderCode(providerCode)
	providerID, err := ensureProvider(ctx, tx, providerCode)
	if err != nil {
		return domain.DepositOrder{}, nil, err
	}

	order, err := findDepositOrderForUpdate(ctx, tx, providerID, notification.OrderNo)
	if err != nil {
		_ = insertDepositProviderCallback(ctx, tx, providerID, nil, notification, "failed", err.Error())
		_ = tx.Commit()
		return domain.DepositOrder{}, nil, err
	}
	if notification.AmountCents != 0 && notification.AmountCents != order.AmountCents {
		err := fmt.Errorf("amount mismatch: got %d want %d", notification.AmountCents, order.AmountCents)
		_ = insertDepositProviderCallback(ctx, tx, providerID, &order.ID, notification, "failed", err.Error())
		_ = tx.Commit()
		return domain.DepositOrder{}, nil, err
	}

	status := domain.DepositOrderStatusFailed
	if isDepositProviderSuccess(notification.Status) {
		status = domain.DepositOrderStatusPaid
	}
	now := time.Now()
	order.Status = status
	order.ProviderTradeNo = notification.TradeNo
	order.UpdatedAt = now

	_, err = tx.ExecContext(ctx, `
		UPDATE orders
		SET status = ?, paid_at = CASE WHEN ? = 'paid' THEN COALESCE(paid_at, ?) ELSE paid_at END, updated_at = ?
		WHERE id = ?
	`, string(status), string(status), now, now, order.ID)
	if err != nil {
		return domain.DepositOrder{}, nil, err
	}

	var transactionID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM provider_transactions
		WHERE provider_id = ? AND provider_order_no = ?
		LIMIT 1
	`, providerID, order.OrderNo).Scan(&transactionID)
	if err != nil {
		return domain.DepositOrder{}, nil, err
	}

	notifyPayload, _ := json.Marshal(map[string]any{
		"order_no":     notification.OrderNo,
		"trade_no":     notification.TradeNo,
		"amount_cents": notification.AmountCents,
		"status":       notification.Status,
		"raw":          string(notification.RawPayload),
	})
	_, err = tx.ExecContext(ctx, `
		UPDATE provider_transactions
		SET provider_trade_no = ?, notify_payload = ?, status = ?,
		    paid_at = CASE WHEN ? = 'paid' THEN COALESCE(paid_at, ?) ELSE paid_at END,
		    updated_at = ?
		WHERE id = ?
	`, notification.TradeNo, notifyPayload, string(status), string(status), now, now, transactionID)
	if err != nil {
		return domain.DepositOrder{}, nil, err
	}

	if err := insertDepositProviderCallback(ctx, tx, providerID, &order.ID, notification, "processed", ""); err != nil {
		return domain.DepositOrder{}, nil, err
	}

	var ledger *domain.LedgerEntry
	if status == domain.DepositOrderStatusPaid {
		entryNo := fmt.Sprintf("LE%s", order.OrderNo)
		result, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO ledger_entries (
				merchant_id, order_id, provider_transaction_id, entry_no, direction, type, amount_cents, currency
			) VALUES (?, ?, ?, ?, 'credit', 'deposit', ?, ?)
		`, order.MerchantID, order.ID, transactionID, entryNo, order.AmountCents, order.Currency)
		if err != nil {
			return domain.DepositOrder{}, nil, err
		}
		if rows, _ := result.RowsAffected(); rows > 0 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO merchant_balances (merchant_id, currency, available_cents, pending_cents)
				VALUES (?, ?, ?, 0)
				ON DUPLICATE KEY UPDATE available_cents = available_cents + VALUES(available_cents)
			`, order.MerchantID, order.Currency, order.AmountCents); err != nil {
				return domain.DepositOrder{}, nil, err
			}
		}
		ledger = &domain.LedgerEntry{
			OrderID:     order.ID,
			OrderNo:     order.OrderNo,
			AmountCents: order.AmountCents,
			Type:        "deposit",
			CreatedAt:   now,
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.DepositOrder{}, nil, err
	}
	return order, ledger, nil
}

func ensureMerchant(ctx context.Context, tx *sql.Tx, code, callbackURL string) (int64, error) {
	if code == "" {
		code = "default"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchants (code, name, api_key_hash, callback_url)
		VALUES (?, ?, '', ?)
		ON DUPLICATE KEY UPDATE
			callback_url = CASE
				WHEN VALUES(callback_url) IS NULL OR VALUES(callback_url) = '' THEN callback_url
				ELSE VALUES(callback_url)
			END,
			updated_at = CURRENT_TIMESTAMP
	`, code, code, nullableString(callbackURL)); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM merchants WHERE code = ?`, code).Scan(&id)
	return id, err
}

func ensureProvider(ctx context.Context, tx *sql.Tx, code string) (int64, error) {
	code = normalizeProviderCode(code)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payment_providers (code, name)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE updated_at = CURRENT_TIMESTAMP
	`, code, code); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM payment_providers WHERE code = ?`, code).Scan(&id)
	return id, err
}

func ensureChannel(ctx context.Context, tx *sql.Tx, providerID int64, code string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payment_channels (provider_id, code, name, method, currency, enabled)
		VALUES (?, ?, ?, ?, 'TWD', TRUE)
		ON DUPLICATE KEY UPDATE
			provider_id = VALUES(provider_id),
			name = VALUES(name),
			method = VALUES(method),
			enabled = TRUE,
			updated_at = CURRENT_TIMESTAMP
	`, providerID, code, code, code); err != nil {
		return 0, err
	}

	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM payment_channels WHERE provider_id = ? AND code = ?`, providerID, code).Scan(&id)
	return id, err
}

func findDepositOrderForUpdate(ctx context.Context, tx *sql.Tx, providerID int64, orderNo string) (domain.DepositOrder, error) {
	var order domain.DepositOrder
	var status string
	var requestPayload []byte
	err := tx.QueryRowContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.created_at, o.updated_at, pt.request_payload
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN payment_channels pc ON pc.id = o.channel_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN payment_providers p ON p.id = pt.provider_id
		WHERE o.order_no = ?
		  AND pt.provider_id = ?
		FOR UPDATE
	`, orderNo, providerID).Scan(
		&order.ID, &order.MerchantID, &order.MerchantCode, &order.CallbackURL, &order.ChannelCode, &order.OrderNo, &order.MerchantOrderNo,
		&order.Provider,
		&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.Provider = normalizeProviderCode(order.Provider)
	order.Status = domain.DepositOrderStatus(status)
	applyStoredRYDepositRequestPayload(&order, requestPayload)
	return order, nil
}

func normalizeProviderCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "newebpay"
	}
	return code
}

func applyStoredRYDepositRequestPayload(order *domain.DepositOrder, payload []byte) {
	if order == nil || len(payload) == 0 {
		return
	}

	var wrapped storedDepositPaymentRequestPayload
	if err := json.Unmarshal(payload, &wrapped); err == nil {
		mergeRYDepositPayload(order, wrapped.RY)
		return
	}

	var legacy provider.DepositPaymentRequest
	if err := json.Unmarshal(payload, &legacy); err == nil {
		return
	}
}

func mergeRYDepositPayload(order *domain.DepositOrder, ry storedRYDepositRequestPayload) {
	if order.CallbackURL == "" {
		order.CallbackURL = ry.CallbackURL
	}
	order.BankAccounts = append([]string(nil), ry.BankAccounts...)
	order.StoreNumbers = append([]string(nil), ry.StoreNumbers...)
	order.UserName = ry.UserName
	order.BankID = ry.BankID
	order.PayCurrency = ry.PayCurrency
	order.Mobile = ry.Mobile
	order.IDNo = ry.IDNo
}

func insertDepositProviderCallback(ctx context.Context, tx *sql.Tx, providerID int64, orderID *int64, notification provider.DepositNotification, status, errorMessage string) error {
	payload, _ := json.Marshal(map[string]any{
		"order_no":     notification.OrderNo,
		"trade_no":     notification.TradeNo,
		"amount_cents": notification.AmountCents,
		"status":       notification.Status,
		"raw":          string(notification.RawPayload),
	})
	_, err := tx.ExecContext(ctx, `
		INSERT INTO provider_callbacks (
			provider_id, order_id, provider_trade_no, provider_order_no, payload, status, error_message, processed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, providerID, nullableInt64(orderID), notification.TradeNo, notification.OrderNo, payload, status, nullableString(errorMessage))
	return err
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func isDepositProviderSuccess(status string) bool {
	return status == "SUCCESS" || status == "paid"
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
