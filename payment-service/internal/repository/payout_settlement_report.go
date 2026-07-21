package repository

import (
	"context"
	"fmt"
	"time"

	"payment-service/internal/domain"
)

type PayoutSettlementSnapshot struct {
	GeneratedAt time.Time
	Currencies  []PayoutSettlementCurrencySnapshot
}

type PayoutSettlementCurrencySnapshot struct {
	Currency                    string
	MerchantAvailableCents      int64
	MerchantPendingCents        int64
	PendingReviewCents          int64
	ApprovedCents               int64
	SubmittingCents             int64
	ProcessingCents             int64
	CompletedCents              int64
	FailedCents                 int64
	CancelledOrRejectedCents    int64
	ReversedCents               int64
	OpenOrderCount              int64
	ProviderInFlightCents       int64
	InternalManualHoldCents     int64
	InternalTotalUnsettledCents int64
}

func buildSettlementCurrencySnapshot(currency string, available, pending, openOrderCount int64, byStatus map[domain.PayoutOrderStatus]int64) PayoutSettlementCurrencySnapshot {
	snapshot := PayoutSettlementCurrencySnapshot{
		Currency:                 currency,
		MerchantAvailableCents:   available,
		MerchantPendingCents:     pending,
		PendingReviewCents:       byStatus[domain.PayoutOrderStatusPendingReview],
		ApprovedCents:            byStatus[domain.PayoutOrderStatusApproved],
		SubmittingCents:          byStatus[domain.PayoutOrderStatusSubmitting],
		ProcessingCents:          byStatus[domain.PayoutOrderStatusProcessing],
		CompletedCents:           byStatus[domain.PayoutOrderStatusCompleted],
		FailedCents:              byStatus[domain.PayoutOrderStatusFailed],
		CancelledOrRejectedCents: byStatus[domain.PayoutOrderStatusCancelled] + byStatus[domain.PayoutOrderStatusRejected],
		ReversedCents:            byStatus[domain.PayoutOrderStatusReversed],
		OpenOrderCount:           openOrderCount,
	}
	snapshot.ProviderInFlightCents = snapshot.SubmittingCents + snapshot.ProcessingCents
	snapshot.InternalManualHoldCents = snapshot.PendingReviewCents + snapshot.ApprovedCents
	snapshot.InternalTotalUnsettledCents = snapshot.InternalManualHoldCents + snapshot.ProviderInFlightCents
	return snapshot
}

func (s *InMemoryPayoutStore) BuildPayoutSettlementSnapshot(_ context.Context) (PayoutSettlementSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byCurrencyStatus := make(map[string]map[domain.PayoutOrderStatus]int64)
	openOrderCounts := make(map[string]int64)
	for _, order := range s.orders {
		if byCurrencyStatus[order.Currency] == nil {
			byCurrencyStatus[order.Currency] = make(map[domain.PayoutOrderStatus]int64)
		}
		byCurrencyStatus[order.Currency][order.Status] += order.TotalDebitCents
		if !order.Status.IsTerminal() {
			openOrderCounts[order.Currency]++
		}
	}

	currencies := make([]PayoutSettlementCurrencySnapshot, 0, len(s.balances))
	seen := make(map[string]struct{})
	for key := range s.balances {
		merchantID, currency := parseBalanceKey(key)
		_ = merchantID
		if _, ok := seen[currency]; ok {
			continue
		}
		seen[currency] = struct{}{}
		var totalAvailable, totalPending int64
		for candidateKey, candidateAvailable := range s.balances {
			_, candidateCurrency := parseBalanceKey(candidateKey)
			if candidateCurrency != currency {
				continue
			}
			totalAvailable += candidateAvailable
			totalPending += s.pendingBalances[candidateKey]
		}
		currencies = append(currencies, buildSettlementCurrencySnapshot(currency, totalAvailable, totalPending, openOrderCounts[currency], byCurrencyStatus[currency]))
	}
	for currency := range byCurrencyStatus {
		if _, ok := seen[currency]; ok {
			continue
		}
		currencies = append(currencies, buildSettlementCurrencySnapshot(currency, 0, 0, openOrderCounts[currency], byCurrencyStatus[currency]))
	}
	return PayoutSettlementSnapshot{
		GeneratedAt: time.Now(),
		Currencies:  currencies,
	}, nil
}

func (s *MySQLPayoutStore) BuildPayoutSettlementSnapshot(ctx context.Context) (PayoutSettlementSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PayoutSettlementSnapshot{}, err
	}
	defer rollback(tx)

	balanceRows, err := tx.QueryContext(ctx, `
		SELECT currency, COALESCE(SUM(available_cents), 0), COALESCE(SUM(pending_cents), 0)
		FROM merchant_balances
		GROUP BY currency
	`)
	if err != nil {
		return PayoutSettlementSnapshot{}, err
	}
	defer balanceRows.Close()

	type balanceSummary struct {
		available int64
		pending   int64
	}
	balances := make(map[string]balanceSummary)
	for balanceRows.Next() {
		var currency string
		var available int64
		var pending int64
		if err := balanceRows.Scan(&currency, &available, &pending); err != nil {
			return PayoutSettlementSnapshot{}, err
		}
		balances[currency] = balanceSummary{available: available, pending: pending}
	}
	if err := balanceRows.Err(); err != nil {
		return PayoutSettlementSnapshot{}, err
	}

	statusRows, err := tx.QueryContext(ctx, `
		SELECT currency, status, COALESCE(SUM(total_debit_cents), 0)
		FROM payout_orders
		GROUP BY currency, status
	`)
	if err != nil {
		return PayoutSettlementSnapshot{}, err
	}
	defer statusRows.Close()

	byCurrencyStatus := make(map[string]map[domain.PayoutOrderStatus]int64)
	openOrderCounts := make(map[string]int64)
	for statusRows.Next() {
		var currency string
		var status string
		var amount int64
		if err := statusRows.Scan(&currency, &status, &amount); err != nil {
			return PayoutSettlementSnapshot{}, err
		}
		if byCurrencyStatus[currency] == nil {
			byCurrencyStatus[currency] = make(map[domain.PayoutOrderStatus]int64)
		}
		byCurrencyStatus[currency][domain.PayoutOrderStatus(status)] += amount
	}
	if err := statusRows.Err(); err != nil {
		return PayoutSettlementSnapshot{}, err
	}

	countRows, err := tx.QueryContext(ctx, `
		SELECT currency, COUNT(1)
		FROM payout_orders
		WHERE status IN ('pending_review', 'approved', 'submitting', 'processing')
		GROUP BY currency
	`)
	if err != nil {
		return PayoutSettlementSnapshot{}, err
	}
	defer countRows.Close()
	for countRows.Next() {
		var currency string
		var count int64
		if err := countRows.Scan(&currency, &count); err != nil {
			return PayoutSettlementSnapshot{}, err
		}
		openOrderCounts[currency] = count
	}
	if err := countRows.Err(); err != nil {
		return PayoutSettlementSnapshot{}, err
	}

	currencies := make([]PayoutSettlementCurrencySnapshot, 0, len(balances)+len(byCurrencyStatus))
	seen := make(map[string]struct{})
	for currency, summary := range balances {
		seen[currency] = struct{}{}
		currencies = append(currencies, buildSettlementCurrencySnapshot(currency, summary.available, summary.pending, openOrderCounts[currency], byCurrencyStatus[currency]))
	}
	for currency := range byCurrencyStatus {
		if _, ok := seen[currency]; ok {
			continue
		}
		currencies = append(currencies, buildSettlementCurrencySnapshot(currency, 0, 0, openOrderCounts[currency], byCurrencyStatus[currency]))
	}

	if err := tx.Commit(); err != nil {
		return PayoutSettlementSnapshot{}, err
	}
	return PayoutSettlementSnapshot{
		GeneratedAt: time.Now(),
		Currencies:  currencies,
	}, nil
}

func parseBalanceKey(key string) (int64, string) {
	var merchantID int64
	var currency string
	_, _ = fmt.Sscanf(key, "%d|%s", &merchantID, &currency)
	return merchantID, currency
}
