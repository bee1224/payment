package domain

import "time"

const (
	LedgerDirectionCredit = "credit"
	LedgerDirectionDebit  = "debit"
)

const (
	LedgerEntryTypeDepositPaid    = "deposit_paid"
	LedgerEntryTypePayoutHold     = "payout_hold"
	LedgerEntryTypePayoutComplete = "payout_complete"
	LedgerEntryTypePayoutRelease  = "payout_release"
	LedgerEntryTypeAdjustment     = "adjustment"
	LedgerEntryTypeReversal       = "reversal"
)

const (
	LedgerReferenceTypeOrder               = "order"
	LedgerReferenceTypeProviderTransaction = "provider_transaction"
	LedgerReferenceTypePayoutOrder         = "payout_order"
	LedgerReferenceTypePayoutTransaction   = "payout_transaction"
	LedgerReferenceTypeLedgerEntry         = "ledger_entry"
	LedgerReferenceTypeReconciliationItem  = "reconciliation_item"
)

const (
	LedgerSourceEventDepositPaid      = "deposit_paid"
	LedgerSourceEventPayoutHold       = "payout_hold"
	LedgerSourceEventPayoutComplete   = "payout_complete"
	LedgerSourceEventPayoutReject     = "payout_reject"
	LedgerSourceEventPayoutCancel     = "payout_cancel"
	LedgerSourceEventPayoutFail       = "payout_fail"
	LedgerSourceEventPayoutReverse    = "payout_reverse"
	LedgerSourceEventManualAdjustment = "manual_adjustment"
	LedgerSourceEventManualReversal   = "manual_reversal"
)

type LedgerEntry struct {
	ID                    int64
	MerchantID            int64
	OrderID               int64
	PayoutOrderID         int64
	ProviderTransactionID int64
	PayoutTransactionID   int64
	OrderNo               string
	PayoutNo              string
	AmountCents           int64
	Direction             string
	Type                  string
	Currency              string
	BalanceBeforeCents    int64
	BalanceAfterCents     int64
	ReferenceType         string
	ReferenceID           int64
	SourceEvent           string
	ReversalOfEntryID     int64
	CreatedAt             time.Time
}
