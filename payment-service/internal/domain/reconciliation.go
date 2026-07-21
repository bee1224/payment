package domain

import "time"

type ReconciliationScopeType string

const (
	ReconciliationScopeFull    ReconciliationScopeType = "full"
	ReconciliationScopePartial ReconciliationScopeType = "partial"
)

type ReconciliationStatus string

const (
	ReconciliationStatusRunning   ReconciliationStatus = "running"
	ReconciliationStatusCompleted ReconciliationStatus = "completed"
	ReconciliationStatusFailed    ReconciliationStatus = "failed"
)

type ReconciliationMismatchType string

const (
	ReconciliationMismatchBalanceMismatch ReconciliationMismatchType = "balance_mismatch"
	ReconciliationMismatchMissingLedger   ReconciliationMismatchType = "missing_ledger"
	ReconciliationMismatchDuplicateLedger ReconciliationMismatchType = "duplicate_ledger"
	ReconciliationMismatchStuckPending    ReconciliationMismatchType = "stuck_pending"
	ReconciliationMismatchProviderState   ReconciliationMismatchType = "provider_state_mismatch"
)

type ReconciliationResolutionStatus string

const (
	ReconciliationResolutionStatusOpen     ReconciliationResolutionStatus = "open"
	ReconciliationResolutionStatusResolved ReconciliationResolutionStatus = "resolved"
)

type ReconciliationResolutionType string

const (
	ReconciliationResolutionTypeAdjustment ReconciliationResolutionType = "adjustment"
	ReconciliationResolutionTypeReversal   ReconciliationResolutionType = "reversal"
)

type ReconciliationFilter struct {
	MerchantCode string
	OrderNo      string
	PayoutNo     string
}

type ReconciliationOrderType string

const (
	ReconciliationOrderTypeDeposit ReconciliationOrderType = "deposit"
	ReconciliationOrderTypePayout  ReconciliationOrderType = "payout"
)

type ReconciliationReportQuery struct {
	DateFrom     *time.Time
	DateTo       *time.Time
	MerchantCode string
	OrderType    ReconciliationOrderType
	MismatchType ReconciliationMismatchType
}

type ReconciliationTraceQuery struct {
	MerchantOrderNo string
	PayoutNo        string
	ProviderTradeNo string
	LedgerEntryNo   string
}

type ReconciliationBalanceSnapshot struct {
	MerchantID     int64     `json:"merchant_id"`
	MerchantCode   string    `json:"merchant_code"`
	Currency       string    `json:"currency"`
	AvailableCents int64     `json:"available_cents"`
	PendingCents   int64     `json:"pending_cents"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ReconciliationTraceRecord struct {
	ID              int64      `json:"id"`
	RecordType      string     `json:"record_type"`
	MerchantID      int64      `json:"merchant_id,omitempty"`
	MerchantCode    string     `json:"merchant_code,omitempty"`
	OrderNo         string     `json:"order_no,omitempty"`
	MerchantOrderNo string     `json:"merchant_order_no,omitempty"`
	PayoutNo        string     `json:"payout_no,omitempty"`
	ProviderTradeNo string     `json:"provider_trade_no,omitempty"`
	LedgerEntryNo   string     `json:"ledger_entry_no,omitempty"`
	Status          string     `json:"status,omitempty"`
	AmountCents     int64      `json:"amount_cents,omitempty"`
	Currency        string     `json:"currency,omitempty"`
	OccurredAt      *time.Time `json:"occurred_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	Details         string     `json:"details,omitempty"`
}

type ReconciliationTrace struct {
	Query      ReconciliationTraceQuery        `json:"query"`
	Balances   []ReconciliationBalanceSnapshot `json:"balances"`
	Deposits   []ReconciliationTraceRecord     `json:"deposits"`
	Payouts    []ReconciliationTraceRecord     `json:"payouts"`
	Providers  []ReconciliationTraceRecord     `json:"provider_transactions"`
	Callbacks  []ReconciliationTraceRecord     `json:"provider_callbacks"`
	Ledgers    []ReconciliationTraceRecord     `json:"ledger_entries"`
	Mismatches []ReconciliationMismatch        `json:"mismatches"`
}

type ReconciliationReport struct {
	ID            int64
	ScopeType     ReconciliationScopeType
	Status        ReconciliationStatus
	MerchantCode  string
	OrderNo       string
	PayoutNo      string
	MismatchCount int
	StartedAt     time.Time
	CompletedAt   *time.Time
	Items         []ReconciliationMismatch
}

type ReconciliationMismatch struct {
	ID                      int64
	RunID                   int64
	MismatchType            ReconciliationMismatchType
	MerchantID              int64
	MerchantCode            string
	EntityType              string
	EntityID                int64
	OrderNo                 string
	PayoutNo                string
	TableName               string
	FieldName               string
	ExpectedValue           string
	ActualValue             string
	Details                 string
	ResolutionStatus        ReconciliationResolutionStatus
	ResolutionType          ReconciliationResolutionType
	ResolutionNote          string
	ResolutionLedgerEntryID int64
	ResolvedAt              *time.Time
	ResolvedBy              string
	CreatedAt               time.Time
}
