package service

import (
	"sync"
	"time"

	"payment-service/internal/domain"
)

const (
	LedgerTypeDeposit        = "deposit"
	LedgerTypePayoutHold     = "payout_hold"
	LedgerTypePayoutComplete = "payout_complete"
	LedgerTypePayoutRelease  = "payout_release"
	LedgerTypePayoutReturn   = "payout_return"
)

type LedgerService struct {
	mu      sync.Mutex
	nextID  int64
	entries []domain.LedgerEntry
}

func NewLedgerService() *LedgerService {
	return &LedgerService{nextID: 1}
}

func (s *LedgerService) RecordDeposit(order domain.DepositOrder) (domain.LedgerEntry, error) {
	entry := domain.LedgerEntry{
		MerchantID:  order.MerchantID,
		OrderID:     order.ID,
		OrderNo:     order.OrderNo,
		AmountCents: order.AmountCents,
		Direction:   "credit",
		Type:        LedgerTypeDeposit,
		Currency:    order.Currency,
		CreatedAt:   time.Now(),
	}
	return s.Record(entry)
}

func (s *LedgerService) Record(entry domain.LedgerEntry) (domain.LedgerEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry.ID = s.nextID
	s.nextID++
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	s.entries = append(s.entries, entry)
	return entry, nil
}

func (s *LedgerService) Entries() []domain.LedgerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]domain.LedgerEntry, len(s.entries))
	copy(entries, s.entries)
	return entries
}
