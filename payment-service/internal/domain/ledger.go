package domain

import "time"

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
	BalanceAfterCents     int64
	CreatedAt             time.Time
}
