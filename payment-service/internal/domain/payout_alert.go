package domain

import "time"

type PayoutOperationalAlert struct {
	ID              int64
	MerchantID      int64
	PayoutOrderID   int64
	PayoutNo        string
	Category        string
	Severity        string
	Status          string
	Summary         string
	Details         string
	OccurrenceCount int
	FirstOccurredAt time.Time
	LastOccurredAt  time.Time
	ResolvedAt      *time.Time
	ResolvedBy      string
	ResolveReason   string
}
