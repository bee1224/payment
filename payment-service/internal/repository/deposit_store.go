package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/provider"
)

var ErrNotFound = errors.New("not found")
var ErrCallbackClaimLost = errors.New("deposit callback claim lost or expired")

const DepositCallbackMaxRetries = 8
const depositCallbackClaimLease = 2 * time.Minute

func DepositCallbackNextRetry(retryCount int, now time.Time) (time.Time, bool) {
	if retryCount+1 >= DepositCallbackMaxRetries {
		return time.Time{}, true
	}
	shift := retryCount
	if shift > 5 {
		shift = 5
	}
	return now.Add(time.Duration(1<<shift) * time.Minute), false
}

type DepositStore interface {
	CreateDepositOrder(ctx context.Context, order domain.DepositOrder, itemDesc string) (domain.DepositOrder, error)
	SaveDepositPaymentRequest(ctx context.Context, order domain.DepositOrder, payment provider.DepositPaymentRequest) error
	LoadDepositPaymentRequest(ctx context.Context, orderNo string) (provider.DepositPaymentRequest, error)
	FindDepositOrderByOrderNo(ctx context.Context, orderNo string) (domain.DepositOrder, error)
	FindDepositOrderByMerchantOrderNo(ctx context.Context, merchantCode, merchantOrderNo string) (domain.DepositOrder, error)
	RecordDepositNotificationFailure(ctx context.Context, providerCode string, trace domain.DepositNotifyTrace, fields map[string]string, errorMessage string) error
	ApplyDepositNotification(ctx context.Context, providerCode string, notification provider.DepositNotification, trace domain.DepositNotifyTrace) (domain.DepositOrder, *domain.LedgerEntry, bool, error)
	ExpireDueDepositOrders(ctx context.Context, before time.Time, limit int) ([]domain.DepositOrder, error)
}

type MySQLDepositStore struct {
	db *sql.DB
}

type storedDepositPaymentRequestPayload struct {
	Payment provider.DepositPaymentRequest     `json:"payment"`
	Gateway storedGatewayDepositRequestPayload `json:"gateway"`
	RY      storedGatewayDepositRequestPayload `json:"ry,omitempty"`
}

type storedGatewayDepositRequestPayload struct {
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
	var existingOrderNo string
	err = tx.QueryRowContext(ctx, `SELECT order_no FROM orders WHERE merchant_id = ? AND merchant_order_no = ? LIMIT 1`, order.MerchantID, order.MerchantOrderNo).Scan(&existingOrderNo)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return domain.DepositOrder{}, err
		}
		return s.FindDepositOrderByOrderNo(ctx, existingOrderNo)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, err
	}

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
			merchant_id, channel_id, order_no, merchant_order_no, amount_cents, currency, status, item_desc, expired_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.MerchantID, channelID, order.OrderNo, order.MerchantOrderNo, order.AmountCents, order.Currency, string(order.Status), itemDesc, nullableTime(order.ExpiresAt), order.CreatedAt, order.UpdatedAt)
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
		Gateway: storedGatewayDepositRequestPayload{
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

func (s *MySQLDepositStore) LoadDepositPaymentRequest(ctx context.Context, orderNo string) (provider.DepositPaymentRequest, error) {
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT request_payload FROM provider_transactions WHERE provider_order_no = ? LIMIT 1`, orderNo).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return provider.DepositPaymentRequest{}, ErrNotFound
		}
		return provider.DepositPaymentRequest{}, err
	}
	var payload storedDepositPaymentRequestPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return provider.DepositPaymentRequest{}, err
	}
	if payload.Payment.HTML == "" {
		return provider.DepositPaymentRequest{}, ErrNotFound
	}
	return payload.Payment, nil
}

func (s *MySQLDepositStore) FindDepositOrderByOrderNo(ctx context.Context, orderNo string) (domain.DepositOrder, error) {
	var order domain.DepositOrder
	var status string
	var requestPayload []byte
	var expiresAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.expired_at, o.created_at, o.updated_at, pt.request_payload
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
		&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &expiresAt, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.Status = domain.DepositOrderStatus(status)
	if expiresAt.Valid {
		order.ExpiresAt = &expiresAt.Time
	}
	applyStoredGatewayDepositRequestPayload(&order, requestPayload)
	return order, nil
}

func (s *MySQLDepositStore) FindDepositOrderByMerchantOrderNo(ctx context.Context, merchantCode, merchantOrderNo string) (domain.DepositOrder, error) {
	var order domain.DepositOrder
	var status string
	var requestPayload []byte
	var expiresAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.expired_at, o.created_at, o.updated_at, pt.request_payload
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
		&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &expiresAt, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.Status = domain.DepositOrderStatus(status)
	if expiresAt.Valid {
		order.ExpiresAt = &expiresAt.Time
	}
	applyStoredGatewayDepositRequestPayload(&order, requestPayload)
	return order, nil
}

// ListDepositOrdersForAdmin reads the existing collection orders. It does not
// alter payment state, balances, or the provider notification workflow.
func (s *MySQLDepositStore) ListDepositOrdersForAdmin(ctx context.Context, limit int) ([]domain.DepositOrder, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''), o.amount_cents, o.currency, o.item_desc,
		       o.status, o.expired_at, o.created_at, o.updated_at, pt.request_payload
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN payment_channels pc ON pc.id = o.channel_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN payment_providers p ON p.id = pt.provider_id
		ORDER BY o.created_at DESC, o.id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := make([]domain.DepositOrder, 0)
	for rows.Next() {
		var order domain.DepositOrder
		var status string
		var expiresAt sql.NullTime
		var requestPayload []byte
		if err := rows.Scan(&order.ID, &order.MerchantID, &order.MerchantCode, &order.CallbackURL, &order.ChannelCode, &order.OrderNo, &order.MerchantOrderNo, &order.Provider, &order.ProviderTradeNo, &order.AmountCents, &order.Currency, &order.ItemDesc, &status, &expiresAt, &order.CreatedAt, &order.UpdatedAt, &requestPayload); err != nil {
			return nil, err
		}
		order.Status = domain.DepositOrderStatus(status)
		if expiresAt.Valid {
			order.ExpiresAt = &expiresAt.Time
		}
		applyStoredGatewayDepositRequestPayload(&order, requestPayload)
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (s *MySQLDepositStore) RecordDepositNotificationFailure(ctx context.Context, providerCode string, trace domain.DepositNotifyTrace, fields map[string]string, errorMessage string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	providerID, err := ensureProvider(ctx, tx, normalizeProviderCode(providerCode))
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"fields":            fields,
		"provider_order_no": strings.TrimSpace(trace.ProviderOrderNo),
		"provider_trade_no": strings.TrimSpace(trace.ProviderTradeNo),
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"request_headers": trace.Headers,
		"source_ip":       strings.TrimSpace(trace.SourceIP),
	})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_callbacks (
			provider_id, provider_trade_no, provider_order_no, payload, headers, status, error_message, processed_at
		) VALUES (?, ?, ?, ?, ?, 'failed', ?, CURRENT_TIMESTAMP)
	`, providerID, nullableString(strings.TrimSpace(trace.ProviderTradeNo)), nullableString(strings.TrimSpace(trace.ProviderOrderNo)), payload, headers, errorMessage); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLDepositStore) ApplyDepositNotification(ctx context.Context, providerCode string, notification provider.DepositNotification, trace domain.DepositNotifyTrace) (domain.DepositOrder, *domain.LedgerEntry, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.DepositOrder{}, nil, false, err
	}
	defer rollback(tx)

	providerCode = normalizeProviderCode(providerCode)
	providerID, err := ensureProvider(ctx, tx, providerCode)
	if err != nil {
		return domain.DepositOrder{}, nil, false, err
	}

	order, err := findDepositOrderForUpdate(ctx, tx, providerID, notification.OrderNo)
	if err != nil {
		_ = insertDepositProviderCallback(ctx, tx, providerID, nil, notification, &trace, "failed", err.Error())
		_ = tx.Commit()
		return domain.DepositOrder{}, nil, false, err
	}
	if notification.AmountCents != 0 && notification.AmountCents != order.AmountCents {
		err := fmt.Errorf("amount mismatch: got %d want %d", notification.AmountCents, order.AmountCents)
		_ = insertDepositProviderCallback(ctx, tx, providerID, &order.ID, notification, &trace, "failed", err.Error())
		_ = tx.Commit()
		return domain.DepositOrder{}, nil, false, err
	}
	transactionID, storedTradeNo, transactionStatus, err := findDepositProviderTransactionForUpdate(ctx, tx, providerID, order.OrderNo)
	if err != nil {
		return domain.DepositOrder{}, nil, false, err
	}
	ledgerCount, err := countDepositPaidLedgerEntries(ctx, tx, order.ID, transactionID, order.AmountCents)
	if err != nil {
		return domain.DepositOrder{}, nil, false, err
	}
	idempotent, err := validateDepositNotificationState(order.Status, notification.Status, transactionStatus, storedTradeNo, notification.TradeNo, ledgerCount)
	if err != nil {
		return domain.DepositOrder{}, nil, false, err
	}
	if idempotent {
		if err := insertDepositProviderCallback(ctx, tx, providerID, &order.ID, notification, &trace, "processed", ""); err != nil {
			return domain.DepositOrder{}, nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return domain.DepositOrder{}, nil, false, err
		}
		return order, nil, false, nil
	}

	previousStatus := order.Status
	status := domain.DepositOrderStatusFailed
	if isDepositProviderSuccess(notification.Status) {
		status = domain.DepositOrderStatusPaid
	}
	if order.Status == domain.DepositOrderStatusPaid {
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
		return domain.DepositOrder{}, nil, false, err
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
		return domain.DepositOrder{}, nil, false, err
	}

	if err := insertDepositProviderCallback(ctx, tx, providerID, &order.ID, notification, &trace, "processed", ""); err != nil {
		return domain.DepositOrder{}, nil, false, err
	}

	var ledger *domain.LedgerEntry
	if status == domain.DepositOrderStatusPaid {
		availableBefore, _, err := ensureMerchantBalanceForUpdate(ctx, tx, order.MerchantID, order.Currency)
		if err != nil {
			return domain.DepositOrder{}, nil, false, err
		}
		availableAfter := availableBefore + order.AmountCents
		entry := domain.LedgerEntry{
			MerchantID:            order.MerchantID,
			OrderID:               order.ID,
			ProviderTransactionID: transactionID,
			OrderNo:               order.OrderNo,
			AmountCents:           order.AmountCents,
			Direction:             domain.LedgerDirectionCredit,
			Type:                  domain.LedgerEntryTypeDepositPaid,
			Currency:              order.Currency,
			BalanceBeforeCents:    availableBefore,
			BalanceAfterCents:     availableAfter,
			ReferenceType:         domain.LedgerReferenceTypeProviderTransaction,
			ReferenceID:           transactionID,
			SourceEvent:           domain.LedgerSourceEventDepositPaid,
			CreatedAt:             now,
		}
		if err := applyLedgerEntryAndBalanceUpdate(ctx, tx, entry, order.AmountCents, 0); err != nil {
			return domain.DepositOrder{}, nil, false, err
		}
		ledger = &entry
	}

	if err := tx.Commit(); err != nil {
		return domain.DepositOrder{}, nil, false, err
	}
	return order, ledger, previousStatus != order.Status, nil
}

func findDepositProviderTransactionForUpdate(ctx context.Context, tx *sql.Tx, providerID int64, orderNo string) (int64, string, string, error) {
	var id int64
	var tradeNo, status string
	err := tx.QueryRowContext(ctx, `
		SELECT id, COALESCE(provider_trade_no, ''), status
		FROM provider_transactions
		WHERE provider_id = ? AND provider_order_no = ?
		LIMIT 1
		FOR UPDATE
	`, providerID, orderNo).Scan(&id, &tradeNo, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", ErrNotFound
	}
	return id, tradeNo, status, err
}

func countDepositPaidLedgerEntries(ctx context.Context, tx *sql.Tx, orderID, transactionID, amountCents int64) (int64, error) {
	var count int64
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE order_id = ? AND provider_transaction_id = ?
		  AND type = ? AND direction = ? AND amount_cents = ?
	`, orderID, transactionID, domain.LedgerEntryTypeDepositPaid, domain.LedgerDirectionCredit, amountCents).Scan(&count)
	return count, err
}

// validateDepositNotificationState is called while the order and provider
// transaction rows are locked. It only accepts a retry as idempotent when the
// durable paid state and its single matching ledger entry already exist.
func validateDepositNotificationState(orderStatus domain.DepositOrderStatus, notificationStatus, transactionStatus, storedTradeNo, notificationTradeNo string, ledgerCount int64) (bool, error) {
	if orderStatus != domain.DepositOrderStatusPaid {
		if ledgerCount != 0 {
			return false, fmt.Errorf("order status %s conflicts with %d existing deposit paid ledger entries", orderStatus, ledgerCount)
		}
		return false, nil
	}
	if !isDepositProviderSuccess(notificationStatus) {
		return false, fmt.Errorf("paid order received non-success provider status: %s", notificationStatus)
	}
	if transactionStatus != string(domain.DepositOrderStatusPaid) || strings.TrimSpace(storedTradeNo) != strings.TrimSpace(notificationTradeNo) {
		return false, fmt.Errorf("paid order provider transaction does not match notification")
	}
	if ledgerCount != 1 {
		return false, fmt.Errorf("paid order has %d deposit paid ledger entries", ledgerCount)
	}
	return true, nil
}

func ensureMerchant(ctx context.Context, tx *sql.Tx, code, callbackURL string) (int64, error) {
	if code == "" {
		code = "default"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchants (code, name, api_key_hash, callback_url)
		VALUES (?, ?, '', ?)
		ON DUPLICATE KEY UPDATE
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
	var expiresAt sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.expired_at, o.created_at, o.updated_at, pt.request_payload
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
		&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &expiresAt, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DepositOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.DepositOrder{}, err
	}
	order.Provider = normalizeProviderCode(order.Provider)
	order.Status = domain.DepositOrderStatus(status)
	if expiresAt.Valid {
		order.ExpiresAt = &expiresAt.Time
	}
	applyStoredGatewayDepositRequestPayload(&order, requestPayload)
	return order, nil
}

func (s *MySQLDepositStore) ExpireDueDepositOrders(ctx context.Context, before time.Time, limit int) ([]domain.DepositOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	query := `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.expired_at, o.created_at, o.updated_at, pt.request_payload
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		LEFT JOIN payment_channels pc ON pc.id = o.channel_id
		LEFT JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN payment_providers p ON p.id = pt.provider_id
		WHERE o.status = 'pending' AND o.expired_at IS NOT NULL AND o.expired_at <= ?
		ORDER BY o.expired_at ASC
	`
	args := []any{before}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expiredOrders []domain.DepositOrder
	var ids []int64
	for rows.Next() {
		var order domain.DepositOrder
		var status string
		var requestPayload []byte
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&order.ID, &order.MerchantID, &order.MerchantCode, &order.CallbackURL, &order.ChannelCode, &order.OrderNo, &order.MerchantOrderNo,
			&order.Provider, &order.ProviderTradeNo,
			&order.AmountCents, &order.Currency, &order.ItemDesc, &status, &expiresAt, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
		); err != nil {
			return nil, err
		}
		order.Status = domain.DepositOrderStatusExpired
		if expiresAt.Valid {
			order.ExpiresAt = &expiresAt.Time
		}
		applyStoredGatewayDepositRequestPayload(&order, requestPayload)
		expiredOrders = append(expiredOrders, order)
		ids = append(ids, order.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	updateArgs := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		updateArgs = append(updateArgs, id)
	}
	inClause := strings.Join(placeholders, ",")
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE id IN (`+inClause+`)`, updateArgs...); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE provider_transactions
		SET status = 'expired', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'pending' AND order_id IN (`+inClause+`)`, updateArgs...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return expiredOrders, nil
}

// FindTerminalDepositOrdersMissingCallbackTasks supports recovery when the
// state transaction completed but a later callback-task insert did not.
func (s *MySQLDepositStore) FindTerminalDepositOrdersMissingCallbackTasks(ctx context.Context, limit int) ([]domain.DepositOrder, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.merchant_id, m.code, COALESCE(m.callback_url, ''), COALESCE(pc.code, ''), o.order_no, o.merchant_order_no,
		       COALESCE(p.code, ''), COALESCE(pt.provider_trade_no, ''),
		       o.amount_cents, o.currency, o.item_desc, o.status, o.expired_at, o.created_at, o.updated_at, pt.request_payload
		FROM orders o
		JOIN merchants m ON m.id = o.merchant_id
		JOIN provider_transactions pt ON pt.order_id = o.id
		LEFT JOIN payment_channels pc ON pc.id = o.channel_id
		LEFT JOIN payment_providers p ON p.id = pt.provider_id
		LEFT JOIN merchant_deposit_callback_tasks task ON task.event_key = CONCAT('merchant.deposit:', o.order_no, ':deposit.', o.status)
		WHERE o.status IN ('paid', 'failed', 'expired') AND task.id IS NULL
		ORDER BY o.updated_at ASC, o.id ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := make([]domain.DepositOrder, 0)
	for rows.Next() {
		var order domain.DepositOrder
		var status string
		var expiresAt sql.NullTime
		var requestPayload []byte
		if err := rows.Scan(
			&order.ID, &order.MerchantID, &order.MerchantCode, &order.CallbackURL, &order.ChannelCode, &order.OrderNo, &order.MerchantOrderNo,
			&order.Provider, &order.ProviderTradeNo, &order.AmountCents, &order.Currency, &order.ItemDesc, &status, &expiresAt, &order.CreatedAt, &order.UpdatedAt, &requestPayload,
		); err != nil {
			return nil, err
		}
		order.Status = domain.DepositOrderStatus(status)
		if expiresAt.Valid {
			order.ExpiresAt = &expiresAt.Time
		}
		applyStoredGatewayDepositRequestPayload(&order, requestPayload)
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func normalizeProviderCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "newebpay"
	}
	return code
}

func applyStoredGatewayDepositRequestPayload(order *domain.DepositOrder, payload []byte) {
	if order == nil || len(payload) == 0 {
		return
	}

	var wrapped storedDepositPaymentRequestPayload
	if err := json.Unmarshal(payload, &wrapped); err == nil {
		if hasStoredGatewayDepositPayload(wrapped.Gateway) {
			mergeGatewayDepositPayload(order, wrapped.Gateway)
			return
		}
		mergeGatewayDepositPayload(order, wrapped.RY)
		return
	}

	var legacy provider.DepositPaymentRequest
	if err := json.Unmarshal(payload, &legacy); err == nil {
		return
	}
}

func hasStoredGatewayDepositPayload(payload storedGatewayDepositRequestPayload) bool {
	return payload.CallbackURL != "" ||
		len(payload.BankAccounts) > 0 ||
		len(payload.StoreNumbers) > 0 ||
		payload.UserName != "" ||
		payload.BankID != "" ||
		payload.PayCurrency != "" ||
		payload.Mobile != "" ||
		payload.IDNo != ""
}

func mergeGatewayDepositPayload(order *domain.DepositOrder, ry storedGatewayDepositRequestPayload) {
	if strings.TrimSpace(ry.CallbackURL) != "" {
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

func insertDepositProviderCallback(ctx context.Context, tx *sql.Tx, providerID int64, orderID *int64, notification provider.DepositNotification, trace *domain.DepositNotifyTrace, status, errorMessage string) error {
	payload, _ := json.Marshal(map[string]any{
		"order_no":     notification.OrderNo,
		"trade_no":     notification.TradeNo,
		"amount_cents": notification.AmountCents,
		"status":       notification.Status,
		"raw":          string(notification.RawPayload),
	})
	headersPayload, _ := json.Marshal(map[string]any{
		"request_headers": func() map[string][]string {
			if trace == nil {
				return nil
			}
			return trace.Headers
		}(),
		"source_ip": func() string {
			if trace == nil {
				return ""
			}
			return strings.TrimSpace(trace.SourceIP)
		}(),
	})
	_, err := tx.ExecContext(ctx, `
		INSERT INTO provider_callbacks (
			provider_id, order_id, provider_trade_no, provider_order_no, payload, headers, status, error_message, processed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, providerID, nullableInt64(orderID), nullableString(strings.TrimSpace(notification.TradeNo)), nullableString(strings.TrimSpace(notification.OrderNo)), payload, headersPayload, status, nullableString(errorMessage))
	return err
}

func (s *MySQLDepositStore) CreateMerchantDepositCallbackTask(ctx context.Context, task domain.MerchantDepositCallbackTask) error {
	// Deprecated compatibility wrapper.  Never issue the pre-000004 INSERT:
	// event_key is mandatory after the reliability migration.
	if strings.TrimSpace(task.EventKey) == "" {
		return errors.New("event_key is required; use EnsureMerchantDepositCallbackTask")
	}
	_, _, err := s.EnsureMerchantDepositCallbackTask(ctx, task)
	return err
}

// EnsureMerchantDepositCallbackTask makes the outbox idempotent.  A bad
// connection is ambiguous: MariaDB may have committed the INSERT before the
// driver lost the response, so only that class of error is followed by a fresh
// lookup.  The unique event key remains the final duplicate boundary.
func (s *MySQLDepositStore) EnsureMerchantDepositCallbackTask(ctx context.Context, task domain.MerchantDepositCallbackTask) (domain.MerchantDepositCallbackTask, bool, error) {
	if strings.TrimSpace(task.EventKey) == "" {
		return domain.MerchantDepositCallbackTask{}, false, errors.New("event_key is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO merchant_deposit_callback_tasks (merchant_id, order_id, event_key, callback_url, payload, status, retry_count, next_retry_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, task.MerchantID, task.OrderID, task.EventKey, task.CallbackURL, task.Payload, task.NextRetryAt)
	if err == nil {
		found, lookupErr := s.findMerchantDepositCallbackTaskByEventKey(ctx, task.EventKey)
		return found, false, lookupErr
	}
	if isDuplicateKeyError(err) || errors.Is(err, driver.ErrBadConn) {
		lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		found, lookupErr := s.findMerchantDepositCallbackTaskByEventKey(lookupCtx, task.EventKey)
		if lookupErr == nil {
			return found, true, nil
		}
		if isDuplicateKeyError(err) {
			return domain.MerchantDepositCallbackTask{}, false, lookupErr
		}
	}
	return domain.MerchantDepositCallbackTask{}, false, err
}

func (s *MySQLDepositStore) findMerchantDepositCallbackTaskByEventKey(ctx context.Context, eventKey string) (domain.MerchantDepositCallbackTask, error) {
	var task domain.MerchantDepositCallbackTask
	err := s.db.QueryRowContext(ctx, `SELECT id, merchant_id, order_id, event_key, callback_url, payload, status, retry_count, next_retry_at, COALESCE(last_error,''), created_at, updated_at FROM merchant_deposit_callback_tasks WHERE event_key = ?`, eventKey).Scan(&task.ID, &task.MerchantID, &task.OrderID, &task.EventKey, &task.CallbackURL, &task.Payload, &task.Status, &task.RetryCount, &task.NextRetryAt, &task.LastError, &task.CreatedAt, &task.UpdatedAt)
	return task, err
}

func isDuplicateKeyError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

func (s *MySQLDepositStore) ClaimDueMerchantDepositCallbackTasks(ctx context.Context, before, staleBefore time.Time, limit int) ([]domain.MerchantDepositCallbackTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, merchant_id, order_id, callback_url, payload, status, retry_count, next_retry_at, COALESCE(last_error, ''), sent_at, created_at, updated_at
		FROM merchant_deposit_callback_tasks
		WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= ?) AND retry_count < ?
		ORDER BY next_retry_at ASC
		LIMIT ?
		FOR UPDATE
	`, before, DepositCallbackMaxRetries, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []domain.MerchantDepositCallbackTask
	for rows.Next() {
		var task domain.MerchantDepositCallbackTask
		var sentAt sql.NullTime
		if err := rows.Scan(&task.ID, &task.MerchantID, &task.OrderID, &task.CallbackURL, &task.Payload, &task.Status, &task.RetryCount, &task.NextRetryAt, &task.LastError, &sentAt, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			task.SentAt = &sentAt.Time
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// mysql permits only one active result set per transaction.  Release the
	// locking cursor before applying claim updates on the same transaction.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range tasks {
		tasks[i].ClaimToken = newCallbackClaimToken()
		lease := before.Add(depositCallbackClaimLease)
		if _, err := tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_tasks SET status = 'processing', claim_token = ?, claimed_at = ?, claim_expires_at = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, tasks[i].ClaimToken, before, lease, before, tasks[i].ID); err != nil {
			return nil, err
		}
		tasks[i].ClaimedAt, tasks[i].ClaimExpiresAt = &before, &lease
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *MySQLDepositStore) BeginMerchantDepositCallbackAttempt(ctx context.Context, taskID int64, claimToken string, now time.Time) (domain.MerchantDepositCallbackAttempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MerchantDepositCallbackAttempt{}, err
	}
	defer rollback(tx)
	var count int
	err = tx.QueryRowContext(ctx, `SELECT attempt_count FROM merchant_deposit_callback_tasks WHERE id=? AND status='processing' AND claim_token=? AND claim_expires_at > ? FOR UPDATE`, taskID, claimToken, now).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.MerchantDepositCallbackAttempt{}, ErrCallbackClaimLost
	}
	if err != nil {
		return domain.MerchantDepositCallbackAttempt{}, err
	}
	// The task-row lock serializes BeginAttempt for this task.  Use a separate
	// locking read so MariaDB observes a running attempt committed while this
	// transaction was waiting for that lock; a NOT EXISTS subquery can otherwise
	// retain its earlier REPEATABLE-READ snapshot.
	var runningAttemptID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM merchant_deposit_callback_attempts WHERE task_id=? AND status='running' LIMIT 1 FOR UPDATE`, taskID).Scan(&runningAttemptID)
	if err == nil {
		return domain.MerchantDepositCallbackAttempt{}, ErrCallbackClaimLost
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.MerchantDepositCallbackAttempt{}, err
	}
	count++
	if _, err = tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_tasks SET attempt_count=?, updated_at=? WHERE id=? AND status='processing' AND claim_token=? AND claim_expires_at > ?`, count, now, taskID, claimToken, now); err != nil {
		return domain.MerchantDepositCallbackAttempt{}, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO merchant_deposit_callback_attempts (task_id, attempt_no, stage, status, started_at) VALUES (?, ?, 'delivery', 'running', ?)`, taskID, count, now)
	if err != nil {
		return domain.MerchantDepositCallbackAttempt{}, err
	}
	id, _ := r.LastInsertId()
	if err = tx.Commit(); err != nil {
		return domain.MerchantDepositCallbackAttempt{}, err
	}
	return domain.MerchantDepositCallbackAttempt{ID: id, TaskID: taskID, AttemptNo: count, Stage: "delivery", Status: domain.CallbackAttemptRunning, StartedAt: now}, nil
}

func (s *MySQLDepositStore) FinalizeMerchantDepositCallbackSuccess(ctx context.Context, taskID int64, token string, attemptID int64, result domain.DeliveryResult, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = verifyDepositCallbackClaim(ctx, tx, taskID, token, now); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_attempts SET status='success', finished_at=?, http_status=?, response_body_summary=?, elapsed_ms=?, error_code=NULL, next_retry_at=NULL WHERE id=? AND task_id=? AND status='running'`, now, nullableHTTPStatus(result.HTTPStatus), nullableString(result.ResponseSummary), result.Elapsed.Milliseconds(), attemptID, taskID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrCallbackClaimLost
	}
	r, err = tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_tasks SET status='sent', sent_at=?, claim_token=NULL, claimed_at=NULL, claim_expires_at=NULL, next_retry_at=NULL, last_error=NULL, updated_at=? WHERE id=? AND status='processing' AND claim_token=?`, now, now, taskID, token)
	if err != nil {
		return err
	}
	n, _ = r.RowsAffected()
	if n != 1 {
		return ErrCallbackClaimLost
	}
	return tx.Commit()
}

func (s *MySQLDepositStore) FinalizeMerchantDepositCallbackFailure(ctx context.Context, taskID int64, token string, attemptID int64, result domain.DeliveryResult, next time.Time, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = verifyDepositCallbackClaim(ctx, tx, taskID, token, now); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_attempts SET status='failed', finished_at=?, http_status=?, response_body_summary=?, error_code=?, elapsed_ms=?, next_retry_at=? WHERE id=? AND task_id=? AND status='running'`, now, nullableHTTPStatus(result.HTTPStatus), nullableString(result.ResponseSummary), nullableString(result.ErrorCode), result.Elapsed.Milliseconds(), next, attemptID, taskID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrCallbackClaimLost
	}
	deadLetter := !result.Retryable
	r, err = tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_tasks SET retry_count=retry_count+1, status=CASE WHEN ? OR retry_count+1 >= ? THEN 'dead_letter' ELSE 'pending' END, next_retry_at=CASE WHEN ? OR retry_count+1 >= ? THEN NULL ELSE ? END, last_error=?, claim_token=NULL, claimed_at=NULL, claim_expires_at=NULL, updated_at=? WHERE id=? AND status='processing' AND claim_token=?`, deadLetter, DepositCallbackMaxRetries, deadLetter, DepositCallbackMaxRetries, next, nullableString(result.ErrorCode), now, taskID, token)
	if err != nil {
		return err
	}
	n, _ = r.RowsAffected()
	if n != 1 {
		return ErrCallbackClaimLost
	}
	return tx.Commit()
}

// RecoverStaleMerchantDepositCallbacks abandons only attempts that were
// durably running before a worker lease expired.  Claim-only crashes do not
// manufacture an audit attempt.
func (s *MySQLDepositStore) RecoverStaleMerchantDepositCallbacks(ctx context.Context, now time.Time, limit int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	rows, err := tx.QueryContext(ctx, `SELECT id, retry_count FROM merchant_deposit_callback_tasks WHERE status='processing' AND claim_expires_at <= ? ORDER BY claim_expires_at LIMIT ? FOR UPDATE`, now, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	type staleTask struct {
		id         int64
		retryCount int
	}
	var tasks []staleTask
	for rows.Next() {
		var task staleTask
		if err := rows.Scan(&task.id, &task.retryCount); err != nil {
			return err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// As with claiming, release the result set before issuing updates on this
	// transaction; go-sql-driver/mysql allows only one active result set.
	if err := rows.Close(); err != nil {
		return err
	}
	for _, task := range tasks {
		r, err := tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_attempts SET status='abandoned', finished_at=?, error_code='worker_lease_expired' WHERE task_id=? AND status='running'`, now, task.id)
		if err != nil {
			return err
		}
		_, _ = r.RowsAffected()
		next, _ := DepositCallbackNextRetry(task.retryCount, now)
		_, err = tx.ExecContext(ctx, `UPDATE merchant_deposit_callback_tasks SET retry_count=retry_count+1, status=CASE WHEN retry_count+1 >= ? THEN 'dead_letter' ELSE 'pending' END, next_retry_at=CASE WHEN retry_count+1 >= ? THEN NULL ELSE ? END, last_error='worker_lease_expired', claim_token=NULL, claimed_at=NULL, claim_expires_at=NULL, updated_at=? WHERE id=? AND status='processing' AND claim_expires_at <= ?`, DepositCallbackMaxRetries, DepositCallbackMaxRetries, next, now, task.id, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func verifyDepositCallbackClaim(ctx context.Context, tx *sql.Tx, taskID int64, token string, now time.Time) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM merchant_deposit_callback_tasks WHERE id=? AND status='processing' AND claim_token=? AND claim_expires_at > ? FOR UPDATE`, taskID, token, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCallbackClaimLost
	}
	return err
}

func nullableHTTPStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
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
