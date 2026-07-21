package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"payment-service/internal/domain"
)

type PayoutProviderResult struct {
	ProviderCode     string
	MerchantPayoutNo string
	ProviderOrderNo  string
	ProviderTradeNo  string
	EventKey         string
	Payload          string
	StatusCode       string
	StatusMessage    string
	CompletedAt      time.Time
}

const maxMerchantPayoutCallbackAttempts = 8

type PayoutStore interface {
	FindMerchantByCode(ctx context.Context, code string) (domain.Merchant, error)
	ValidateMerchantAPIKey(ctx context.Context, merchantID int64, apiKey string) (bool, error)
	GetActiveMerchantAPIKeySecret(ctx context.Context, merchantID int64) (string, error)
	ListMerchantAPIKeys(ctx context.Context, merchantCode string) ([]MerchantAPIKeyRecord, error)
	ListMerchantAPIKeyAuditLogs(ctx context.Context, merchantCode string, limit int) ([]MerchantAPIKeyAuditEntry, error)
	ListPayoutReviewAuditLogs(ctx context.Context, payoutNo string, limit int) ([]PayoutReviewAuditEntry, error)
	ListPayoutOperationalAlerts(ctx context.Context, status string, limit int) ([]domain.PayoutOperationalAlert, error)
	IssueMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, expiresAt, previousExpiresAt *time.Time, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error)
	RotateMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, expiresAt, previousExpiresAt *time.Time, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error)
	RevokeMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error)
	CreatePayoutOrder(ctx context.Context, order domain.PayoutOrder, beneficiary domain.PayoutBeneficiary) (domain.PayoutOrder, error)
	FindPayoutOrderByPayoutNo(ctx context.Context, payoutNo string) (domain.PayoutOrder, error)
	FindPayoutOrderByMerchantPayoutNo(ctx context.Context, merchantCode, merchantPayoutNo string) (domain.PayoutOrder, error)
	ApprovePayoutOrder(ctx context.Context, payoutNo string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error)
	RejectPayoutOrder(ctx context.Context, payoutNo, reason string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error)
	CancelPayoutOrder(ctx context.Context, payoutNo, reason string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error)
	MarkPayoutSubmitted(ctx context.Context, payoutNo string, tx domain.PayoutTransaction) (domain.PayoutOrder, error)
	MarkPayoutSubmissionFailure(ctx context.Context, payoutNo string, tx domain.PayoutTransaction, retryable bool) (domain.PayoutOrder, error)
	ApplyPayoutResult(ctx context.Context, result PayoutProviderResult) (domain.PayoutOrder, bool, error)
	ListPayoutsForReconcile(ctx context.Context, statuses []domain.PayoutOrderStatus, before time.Time, limit int) ([]domain.PayoutOrder, error)
	CreatePayoutReviewAuditLog(ctx context.Context, payoutNo string, audit PayoutReviewAuditLog) error
	UpsertPayoutOperationalAlert(ctx context.Context, payoutNo string, alert PayoutOperationalAlertUpsert) error
	ResolvePayoutOperationalAlert(ctx context.Context, alertID int64, resolve PayoutOperationalAlertResolve) error
	BuildPayoutSettlementSnapshot(ctx context.Context) (PayoutSettlementSnapshot, error)
	CreateMerchantPayoutCallbackTask(ctx context.Context, task domain.MerchantPayoutCallbackTask) error
	ClaimDueMerchantPayoutCallbackTasks(ctx context.Context, before, staleBefore time.Time, limit int) ([]domain.MerchantPayoutCallbackTask, error)
	MarkMerchantPayoutCallbackTaskResult(ctx context.Context, taskID int64, claimToken string, success bool, nextRetryAt time.Time, errorMessage string) error
}

type InMemoryPayoutStore struct {
	mu                    sync.Mutex
	nextOrderID           int64
	nextTxID              int64
	nextCallbackID        int64
	nextTaskID            int64
	nextLedgerID          int64
	merchants             map[string]domain.Merchant
	balances              map[string]int64
	pendingBalances       map[string]int64
	orders                map[string]domain.PayoutOrder
	merchantIndex         map[string]string
	attempts              map[string][]domain.PayoutTransaction
	callbackEvents        map[string]struct{}
	tasks                 map[int64]domain.MerchantPayoutCallbackTask
	ledgerEntries         []domain.LedgerEntry
	merchantAPIKeys       map[int64][]MerchantAPIKeyRecord
	auditLogs             map[int64][]MerchantAPIKeyAuditEntry
	payoutAuditLogs       map[string][]PayoutReviewAuditEntry
	payoutAlerts          map[int64]domain.PayoutOperationalAlert
	payoutAlertIndex      map[string]int64
	merchantAPIKeySecrets map[int64]map[string]string
}

func NewInMemoryPayoutStore() *InMemoryPayoutStore {
	return &InMemoryPayoutStore{
		nextOrderID:           1,
		nextTxID:              1,
		nextCallbackID:        1,
		nextTaskID:            1,
		nextLedgerID:          1,
		merchants:             make(map[string]domain.Merchant),
		balances:              make(map[string]int64),
		pendingBalances:       make(map[string]int64),
		orders:                make(map[string]domain.PayoutOrder),
		merchantIndex:         make(map[string]string),
		attempts:              make(map[string][]domain.PayoutTransaction),
		callbackEvents:        make(map[string]struct{}),
		tasks:                 make(map[int64]domain.MerchantPayoutCallbackTask),
		ledgerEntries:         make([]domain.LedgerEntry, 0),
		merchantAPIKeys:       make(map[int64][]MerchantAPIKeyRecord),
		auditLogs:             make(map[int64][]MerchantAPIKeyAuditEntry),
		payoutAuditLogs:       make(map[string][]PayoutReviewAuditEntry),
		payoutAlerts:          make(map[int64]domain.PayoutOperationalAlert),
		payoutAlertIndex:      make(map[string]int64),
		merchantAPIKeySecrets: make(map[int64]map[string]string),
	}
}

func (s *InMemoryPayoutStore) SeedMerchant(merchant domain.Merchant, availableCents int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if merchant.ID == 0 {
		merchant.ID = int64(len(s.merchants) + 1)
	}
	if merchant.Status == "" {
		merchant.Status = "active"
	}
	if merchant.CreatedAt.IsZero() {
		now := time.Now()
		merchant.CreatedAt = now
		merchant.UpdatedAt = now
	}
	s.merchants[merchant.Code] = merchant
	s.balances[balanceKey(merchant.ID, "TWD")] = availableCents
	if strings.TrimSpace(merchant.APIKey) != "" {
		now := time.Now()
		s.merchantAPIKeys[merchant.ID] = []MerchantAPIKeyRecord{{
			ID:            1,
			MerchantID:    merchant.ID,
			KeyHash:       strings.TrimSpace(merchant.APIKey),
			Status:        "active",
			IsPrimary:     true,
			LastRotatedAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}}
		s.setMerchantAPIKeySecretLocked(merchant.ID, strings.TrimSpace(merchant.APIKey), strings.TrimSpace(merchant.APIKey))
	}
}

func (s *InMemoryPayoutStore) FindMerchantByCode(_ context.Context, code string) (domain.Merchant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merchant, ok := s.merchants[code]
	if !ok {
		return domain.Merchant{}, ErrNotFound
	}
	return merchant, nil
}

func (s *InMemoryPayoutStore) ValidateMerchantAPIKey(_ context.Context, merchantID int64, apiKey string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if records, ok := s.merchantAPIKeys[merchantID]; ok && len(records) > 0 {
		now := time.Now()
		for idx, record := range records {
			if record.Status == "revoked" || record.RevokedAt != nil {
				continue
			}
			if record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
				continue
			}
			if merchantAPIKeyMatches(record.KeyHash, apiKey) {
				record.LastUsedAt = &now
				record.UpdatedAt = now
				records[idx] = record
				s.merchantAPIKeys[merchantID] = records
				return true, nil
			}
		}
		return false, nil
	}
	for _, merchant := range s.merchants {
		if merchant.ID == merchantID {
			return merchantAPIKeyMatches(merchant.APIKey, apiKey), nil
		}
	}
	return false, ErrNotFound
}

func (s *InMemoryPayoutStore) GetActiveMerchantAPIKeySecret(_ context.Context, merchantID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.merchantAPIKeys[merchantID]
	for _, record := range records {
		if !record.IsPrimary || record.Status == "revoked" || record.RevokedAt != nil {
			continue
		}
		if secret := strings.TrimSpace(s.lookupMerchantAPIKeySecretLocked(merchantID, record.KeyHash)); secret != "" {
			return secret, nil
		}
	}
	return "", ErrNotFound
}

func (s *InMemoryPayoutStore) ListMerchantAPIKeys(_ context.Context, merchantCode string) ([]MerchantAPIKeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merchant, ok := s.merchants[strings.TrimSpace(merchantCode)]
	if !ok {
		return nil, ErrNotFound
	}
	records := append([]MerchantAPIKeyRecord(nil), s.merchantAPIKeys[merchant.ID]...)
	return records, nil
}

func (s *InMemoryPayoutStore) ListMerchantAPIKeyAuditLogs(_ context.Context, merchantCode string, limit int) ([]MerchantAPIKeyAuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merchant, ok := s.merchants[strings.TrimSpace(merchantCode)]
	if !ok {
		return nil, ErrNotFound
	}
	entries := append([]MerchantAPIKeyAuditEntry(nil), s.auditLogs[merchant.ID]...)
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *InMemoryPayoutStore) ListPayoutReviewAuditLogs(_ context.Context, payoutNo string, limit int) ([]PayoutReviewAuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := append([]PayoutReviewAuditEntry(nil), s.payoutAuditLogs[strings.TrimSpace(payoutNo)]...)
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *InMemoryPayoutStore) ListPayoutOperationalAlerts(_ context.Context, status string, limit int) ([]domain.PayoutOperationalAlert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var alerts []domain.PayoutOperationalAlert
	for _, alert := range s.payoutAlerts {
		if strings.TrimSpace(status) != "" && alert.Status != strings.TrimSpace(status) {
			continue
		}
		alerts = append(alerts, alert)
		if limit > 0 && len(alerts) >= limit {
			break
		}
	}
	return alerts, nil
}

func (s *InMemoryPayoutStore) IssueMerchantAPIKey(_ context.Context, merchantCode, apiKey string, expiresAt, previousExpiresAt *time.Time, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merchant, ok := s.merchants[strings.TrimSpace(merchantCode)]
	if !ok {
		return nil, ErrNotFound
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("api_key is required")
	}
	now := time.Now()
	records := append([]MerchantAPIKeyRecord(nil), s.merchantAPIKeys[merchant.ID]...)
	for idx := range records {
		if !records[idx].IsPrimary || records[idx].Status == "revoked" || records[idx].RevokedAt != nil {
			continue
		}
		records[idx].IsPrimary = false
		records[idx].UpdatedAt = now
		if previousExpiresAt != nil && (records[idx].ExpiresAt == nil || records[idx].ExpiresAt.After(*previousExpiresAt)) {
			expiresAtCopy := *previousExpiresAt
			records[idx].ExpiresAt = &expiresAtCopy
		}
	}
	records = append([]MerchantAPIKeyRecord{{
		ID:            int64(len(records) + 1),
		MerchantID:    merchant.ID,
		KeyHash:       hashMerchantAPIKey(apiKey),
		Status:        "active",
		IsPrimary:     true,
		LastRotatedAt: now,
		ExpiresAt:     expiresAt,
		CreatedAt:     now,
		UpdatedAt:     now,
	}}, records...)
	s.merchantAPIKeys[merchant.ID] = records
	s.setMerchantAPIKeySecretLocked(merchant.ID, records[0].KeyHash, apiKey)
	action := strings.TrimSpace(audit.Action)
	if action == "" {
		action = "issue"
	}
	s.appendAuditLogLocked(merchant.ID, records[0], action, audit, now)
	return append([]MerchantAPIKeyRecord(nil), records...), nil
}

func (s *InMemoryPayoutStore) RotateMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, expiresAt, previousExpiresAt *time.Time, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error) {
	return s.IssueMerchantAPIKey(ctx, merchantCode, apiKey, expiresAt, previousExpiresAt, audit.withAction("rotate"))
}

func (s *InMemoryPayoutStore) RevokeMerchantAPIKey(_ context.Context, merchantCode, apiKey string, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merchant, ok := s.merchants[strings.TrimSpace(merchantCode)]
	if !ok {
		return nil, ErrNotFound
	}
	records := append([]MerchantAPIKeyRecord(nil), s.merchantAPIKeys[merchant.ID]...)
	now := time.Now()
	found := false
	for idx := range records {
		if merchantAPIKeyMatches(records[idx].KeyHash, apiKey) {
			records[idx].Status = "revoked"
			records[idx].IsPrimary = false
			records[idx].UpdatedAt = now
			if records[idx].RevokedAt == nil {
				records[idx].RevokedAt = &now
			}
			found = true
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	s.merchantAPIKeys[merchant.ID] = records
	for _, record := range records {
		if merchantAPIKeyMatches(record.KeyHash, apiKey) {
			s.appendAuditLogLocked(merchant.ID, record, "revoke", audit, now)
			break
		}
	}
	return append([]MerchantAPIKeyRecord(nil), records...), nil
}

func (s *InMemoryPayoutStore) setMerchantAPIKeySecretLocked(merchantID int64, keyHashOrSecret, secret string) {
	keyHash := strings.TrimSpace(keyHashOrSecret)
	if !isLikelySHA256Hex(keyHash) {
		keyHash = hashMerchantAPIKey(keyHashOrSecret)
	}
	secret = strings.TrimSpace(secret)
	if keyHash == "" || secret == "" {
		return
	}
	if s.merchantAPIKeySecrets[merchantID] == nil {
		s.merchantAPIKeySecrets[merchantID] = make(map[string]string)
	}
	s.merchantAPIKeySecrets[merchantID][strings.ToLower(keyHash)] = secret
}

func (s *InMemoryPayoutStore) lookupMerchantAPIKeySecretLocked(merchantID int64, keyHash string) string {
	if s.merchantAPIKeySecrets[merchantID] == nil {
		return ""
	}
	return s.merchantAPIKeySecrets[merchantID][strings.ToLower(strings.TrimSpace(keyHash))]
}

func (s *InMemoryPayoutStore) appendAuditLogLocked(merchantID int64, record MerchantAPIKeyRecord, action string, audit MerchantAPIKeyAuditLog, createdAt time.Time) {
	entry := MerchantAPIKeyAuditEntry{
		ID:         int64(len(s.auditLogs[merchantID]) + 1),
		MerchantID: merchantID,
		Action:     action,
		KeyHash:    record.KeyHash,
		Actor:      strings.TrimSpace(audit.Actor),
		Reason:     strings.TrimSpace(audit.Reason),
		RequestID:  strings.TrimSpace(audit.RequestID),
		SourceIP:   strings.TrimSpace(audit.SourceIP),
		UserAgent:  strings.TrimSpace(audit.UserAgent),
		CreatedAt:  createdAt,
	}
	if entry.Actor == "" {
		entry.Actor = "system"
	}
	if record.ID != 0 {
		entry.MerchantAPIKeyID = &record.ID
	}
	if len(audit.Metadata) > 0 {
		raw, _ := json.Marshal(audit.Metadata)
		entry.Metadata = string(raw)
	}
	s.auditLogs[merchantID] = append([]MerchantAPIKeyAuditEntry{entry}, s.auditLogs[merchantID]...)
}

func (s *InMemoryPayoutStore) appendPayoutAuditLogLocked(order domain.PayoutOrder, audit PayoutReviewAuditLog, createdAt time.Time) {
	entry := PayoutReviewAuditEntry{
		ID:            int64(len(s.payoutAuditLogs[order.PayoutNo]) + 1),
		MerchantID:    order.MerchantID,
		PayoutOrderID: order.ID,
		Action:        strings.TrimSpace(audit.Action),
		Actor:         strings.TrimSpace(audit.Actor),
		Reason:        strings.TrimSpace(audit.Reason),
		RequestID:     strings.TrimSpace(audit.RequestID),
		SourceIP:      strings.TrimSpace(audit.SourceIP),
		UserAgent:     strings.TrimSpace(audit.UserAgent),
		CreatedAt:     createdAt,
	}
	if entry.Actor == "" {
		entry.Actor = "system"
	}
	if len(audit.Metadata) > 0 {
		raw, _ := json.Marshal(audit.Metadata)
		entry.Metadata = string(raw)
	}
	s.payoutAuditLogs[order.PayoutNo] = append([]PayoutReviewAuditEntry{entry}, s.payoutAuditLogs[order.PayoutNo]...)
}

func (s *InMemoryPayoutStore) CreatePayoutOrder(_ context.Context, order domain.PayoutOrder, beneficiary domain.PayoutBeneficiary) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	merchant, ok := s.merchants[order.MerchantCode]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	merchantBalanceKey := balanceKey(merchant.ID, order.Currency)
	available := s.balances[merchantBalanceKey]
	if available < order.TotalDebitCents {
		return domain.PayoutOrder{}, fmt.Errorf("insufficient merchant balance")
	}
	if existingNo, ok := s.merchantIndex[merchant.Code+"|"+order.MerchantPayoutNo]; ok {
		return s.orders[existingNo], nil
	}
	now := time.Now()
	order.ID = s.nextOrderID
	s.nextOrderID++
	order.MerchantID = merchant.ID
	order.CreatedAt = now
	order.UpdatedAt = now
	beneficiary.PayoutOrderID = order.ID
	_ = beneficiary
	s.balances[merchantBalanceKey] = available - order.TotalDebitCents
	s.pendingBalances[merchantBalanceKey] += order.TotalDebitCents
	s.orders[order.PayoutNo] = order
	s.merchantIndex[merchant.Code+"|"+order.MerchantPayoutNo] = order.PayoutNo
	s.appendLedgerEntryLocked(domain.LedgerEntry{
		MerchantID:         merchant.ID,
		PayoutOrderID:      order.ID,
		PayoutNo:           order.PayoutNo,
		AmountCents:        order.TotalDebitCents,
		Direction:          domain.LedgerDirectionDebit,
		Type:               domain.LedgerEntryTypePayoutHold,
		Currency:           order.Currency,
		BalanceBeforeCents: available,
		BalanceAfterCents:  available - order.TotalDebitCents,
		ReferenceType:      domain.LedgerReferenceTypePayoutOrder,
		ReferenceID:        order.ID,
		SourceEvent:        domain.LedgerSourceEventPayoutHold,
		CreatedAt:          now,
	})
	return order, nil
}

func (s *InMemoryPayoutStore) FindPayoutOrderByPayoutNo(_ context.Context, payoutNo string) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[payoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	return order, nil
}

func (s *InMemoryPayoutStore) FindPayoutOrderByMerchantPayoutNo(_ context.Context, merchantCode, merchantPayoutNo string) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payoutNo, ok := s.merchantIndex[merchantCode+"|"+merchantPayoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	return s.orders[payoutNo], nil
}

func (s *InMemoryPayoutStore) ApprovePayoutOrder(_ context.Context, payoutNo string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[payoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	if order.Status != domain.PayoutOrderStatusPendingReview {
		return domain.PayoutOrder{}, fmt.Errorf("payout status %s cannot be approved", order.Status)
	}
	now := time.Now()
	order.Status = domain.PayoutOrderStatusApproved
	order.ApprovedAt = &now
	order.UpdatedAt = now
	s.orders[payoutNo] = order
	s.appendPayoutAuditLogLocked(order, audit.withAction("approve"), now)
	return order, nil
}

func (s *InMemoryPayoutStore) RejectPayoutOrder(_ context.Context, payoutNo, reason string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[payoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	if order.Status.IsTerminal() {
		return domain.PayoutOrder{}, fmt.Errorf("payout status %s cannot be rejected", order.Status)
	}
	now := time.Now()
	order.Status = domain.PayoutOrderStatusRejected
	order.FailureMessage = strings.TrimSpace(reason)
	order.UpdatedAt = now
	s.orders[payoutNo] = order
	s.releaseHold(order, domain.LedgerSourceEventPayoutReject, 0)
	s.appendPayoutAuditLogLocked(order, audit.withAction("reject"), now)
	return order, nil
}

func (s *InMemoryPayoutStore) CancelPayoutOrder(_ context.Context, payoutNo, reason string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[payoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	if !canCancelPayoutOrder(order, len(s.attempts[payoutNo])) {
		return domain.PayoutOrder{}, fmt.Errorf("payout status %s cannot be cancelled", order.Status)
	}
	now := time.Now()
	order.Status = domain.PayoutOrderStatusCancelled
	order.FailureMessage = strings.TrimSpace(reason)
	order.CompletedAt = &now
	order.UpdatedAt = now
	s.orders[payoutNo] = order
	s.releaseHold(order, domain.LedgerSourceEventPayoutCancel, 0)
	s.appendPayoutAuditLogLocked(order, audit.withAction("cancel"), now)
	return order, nil
}

func (l PayoutReviewAuditLog) withAction(action string) PayoutReviewAuditLog {
	l.Action = action
	return l
}

func (s *InMemoryPayoutStore) CreatePayoutReviewAuditLog(_ context.Context, payoutNo string, audit PayoutReviewAuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[strings.TrimSpace(payoutNo)]
	if !ok {
		return ErrNotFound
	}
	s.appendPayoutAuditLogLocked(order, audit, time.Now())
	return nil
}

func (s *InMemoryPayoutStore) UpsertPayoutOperationalAlert(_ context.Context, payoutNo string, alert PayoutOperationalAlertUpsert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[strings.TrimSpace(payoutNo)]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	key := fmt.Sprintf("%s|%s|open", order.PayoutNo, strings.TrimSpace(alert.Category))
	if id, ok := s.payoutAlertIndex[key]; ok {
		current := s.payoutAlerts[id]
		current.Severity = strings.TrimSpace(alert.Severity)
		current.Summary = strings.TrimSpace(alert.Summary)
		current.Details = strings.TrimSpace(alert.Details)
		current.OccurrenceCount++
		current.LastOccurredAt = now
		s.payoutAlerts[id] = current
		return nil
	}
	id := int64(len(s.payoutAlerts) + 1)
	if strings.TrimSpace(alert.Severity) == "" {
		alert.Severity = "warning"
	}
	s.payoutAlerts[id] = domain.PayoutOperationalAlert{
		ID:              id,
		MerchantID:      order.MerchantID,
		PayoutOrderID:   order.ID,
		PayoutNo:        order.PayoutNo,
		Category:        strings.TrimSpace(alert.Category),
		Severity:        strings.TrimSpace(alert.Severity),
		Status:          "open",
		Summary:         strings.TrimSpace(alert.Summary),
		Details:         strings.TrimSpace(alert.Details),
		OccurrenceCount: 1,
		FirstOccurredAt: now,
		LastOccurredAt:  now,
	}
	s.payoutAlertIndex[key] = id
	return nil
}

func (s *InMemoryPayoutStore) ResolvePayoutOperationalAlert(_ context.Context, alertID int64, resolve PayoutOperationalAlertResolve) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	alert, ok := s.payoutAlerts[alertID]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	alert.Status = "resolved"
	alert.ResolvedAt = &now
	alert.ResolvedBy = strings.TrimSpace(resolve.ResolvedBy)
	alert.ResolveReason = strings.TrimSpace(resolve.ResolveReason)
	s.payoutAlerts[alertID] = alert
	delete(s.payoutAlertIndex, fmt.Sprintf("%s|%s|open", alert.PayoutNo, alert.Category))
	return nil
}

func (s *InMemoryPayoutStore) MarkPayoutSubmitted(_ context.Context, payoutNo string, tx domain.PayoutTransaction) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[payoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	now := time.Now()
	order.Status = domain.PayoutOrderStatusProcessing
	order.ProviderOrderNo = tx.ProviderOrderNo
	order.ProviderTradeNo = tx.ProviderTradeNo
	order.SubmittedAt = &now
	order.UpdatedAt = now
	tx.ID = s.nextTxID
	s.nextTxID++
	tx.PayoutOrderID = order.ID
	tx.CreatedAt = now
	tx.UpdatedAt = now
	tx.SubmittedAt = &now
	s.attempts[payoutNo] = append(s.attempts[payoutNo], tx)
	s.orders[payoutNo] = order
	return order, nil
}

func (s *InMemoryPayoutStore) MarkPayoutSubmissionFailure(_ context.Context, payoutNo string, tx domain.PayoutTransaction, retryable bool) (domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[payoutNo]
	if !ok {
		return domain.PayoutOrder{}, ErrNotFound
	}
	now := time.Now()
	tx.ID = s.nextTxID
	s.nextTxID++
	tx.PayoutOrderID = order.ID
	tx.CreatedAt = now
	tx.UpdatedAt = now
	s.attempts[payoutNo] = append(s.attempts[payoutNo], tx)
	if retryable {
		order.Status = domain.PayoutOrderStatusApproved
	} else {
		order.Status = domain.PayoutOrderStatusFailed
		order.FailureMessage = tx.ErrorMessage
		order.CompletedAt = &now
		s.releaseHold(order, domain.LedgerSourceEventPayoutFail, 0)
	}
	order.UpdatedAt = now
	s.orders[payoutNo] = order
	return order, nil
}

func (s *InMemoryPayoutStore) ApplyPayoutResult(_ context.Context, result PayoutProviderResult) (domain.PayoutOrder, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var order domain.PayoutOrder
	found := false
	for _, candidate := range s.orders {
		if candidate.MerchantPayoutNo == result.MerchantPayoutNo {
			order = candidate
			found = true
			break
		}
	}
	if !found {
		return domain.PayoutOrder{}, false, ErrNotFound
	}
	if result.EventKey != "" {
		if _, exists := s.callbackEvents[result.EventKey]; exists {
			return order, false, nil
		}
		s.callbackEvents[result.EventKey] = struct{}{}
	}
	now := result.CompletedAt
	if now.IsZero() {
		now = time.Now()
	}
	changed := false
	order.ProviderOrderNo = result.ProviderOrderNo
	order.ProviderTradeNo = result.ProviderTradeNo
	switch result.StatusCode {
	case "30000":
		if order.Status != domain.PayoutOrderStatusCompleted {
			order.Status = domain.PayoutOrderStatusCompleted
			order.CompletedAt = &now
			s.finalizeHold(order)
			changed = true
		}
	case "40000":
		if order.Status == domain.PayoutOrderStatusCompleted {
			if err := ensurePayoutReversalMatchesCompletedTransaction(order, result); err != nil {
				return domain.PayoutOrder{}, false, err
			}
			order.Status = domain.PayoutOrderStatusReversed
			order.CompletedAt = &now
			s.restoreCompleted(order)
			changed = true
		} else if order.Status != domain.PayoutOrderStatusFailed && order.Status != domain.PayoutOrderStatusRejected {
			order.Status = domain.PayoutOrderStatusFailed
			order.FailureMessage = result.StatusMessage
			order.CompletedAt = &now
			txID := int64(0)
			if attempts := s.attempts[order.PayoutNo]; len(attempts) > 0 {
				txID = attempts[len(attempts)-1].ID
			}
			s.releaseHold(order, domain.LedgerSourceEventPayoutFail, txID)
			changed = true
		}
	default:
		return domain.PayoutOrder{}, false, fmt.Errorf("unsupported payout status code: %s", result.StatusCode)
	}
	order.UpdatedAt = now
	s.orders[order.PayoutNo] = order
	return order, changed, nil
}

func (s *InMemoryPayoutStore) ListPayoutsForReconcile(_ context.Context, statuses []domain.PayoutOrderStatus, before time.Time, limit int) ([]domain.PayoutOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := make(map[domain.PayoutOrderStatus]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	var result []domain.PayoutOrder
	for _, order := range s.orders {
		if _, ok := allowed[order.Status]; !ok {
			continue
		}
		if !before.IsZero() && order.UpdatedAt.After(before) {
			continue
		}
		result = append(result, order)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *InMemoryPayoutStore) CreateMerchantPayoutCallbackTask(_ context.Context, task domain.MerchantPayoutCallbackTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	task.ID = s.nextTaskID
	s.nextTaskID++
	if task.Status == "" {
		task.Status = "pending"
	}
	if task.NextRetryAt.IsZero() {
		task.NextRetryAt = now
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	s.tasks[task.ID] = task
	return nil
}

func (s *InMemoryPayoutStore) ClaimDueMerchantPayoutCallbackTasks(_ context.Context, before, staleBefore time.Time, limit int) ([]domain.MerchantPayoutCallbackTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []domain.MerchantPayoutCallbackTask
	for _, task := range s.tasks {
		if task.Status == "sent" || (task.Status == "processing" && task.UpdatedAt.After(staleBefore)) || (task.Status == "pending" && task.NextRetryAt.After(before)) {
			continue
		}
		task.Status = "processing"
		task.ClaimToken = newCallbackClaimToken()
		task.UpdatedAt = before
		s.tasks[task.ID] = task
		result = append(result, task)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *InMemoryPayoutStore) MarkMerchantPayoutCallbackTaskResult(_ context.Context, taskID int64, claimToken string, success bool, nextRetryAt time.Time, errorMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrNotFound
	}
	if task.Status != "processing" || task.ClaimToken != claimToken {
		return ErrNotFound
	}
	now := time.Now()
	task.UpdatedAt = now
	if success {
		task.Status = "sent"
		task.SentAt = &now
		task.LastError = ""
		task.ClaimToken = ""
	} else {
		task.RetryCount++
		if task.RetryCount >= maxMerchantPayoutCallbackAttempts {
			task.Status = "dead_letter"
		} else {
			task.Status = "pending"
		}
		task.NextRetryAt = nextRetryAt
		task.LastError = errorMessage
		task.ClaimToken = ""
	}
	s.tasks[taskID] = task
	return nil
}

func (s *InMemoryPayoutStore) releaseHold(order domain.PayoutOrder, sourceEvent string, payoutTransactionID int64) {
	key := balanceKey(order.MerchantID, order.Currency)
	availableBefore := s.balances[key]
	s.pendingBalances[key] -= order.TotalDebitCents
	if s.pendingBalances[key] < 0 {
		s.pendingBalances[key] = 0
	}
	s.balances[key] += order.TotalDebitCents
	entry := domain.LedgerEntry{
		MerchantID:          order.MerchantID,
		PayoutOrderID:       order.ID,
		PayoutTransactionID: payoutTransactionID,
		PayoutNo:            order.PayoutNo,
		AmountCents:         order.TotalDebitCents,
		Direction:           domain.LedgerDirectionCredit,
		Type:                domain.LedgerEntryTypePayoutRelease,
		Currency:            order.Currency,
		BalanceBeforeCents:  availableBefore,
		BalanceAfterCents:   s.balances[key],
		ReferenceType:       payoutLedgerReferenceType(payoutTransactionID),
		ReferenceID:         payoutLedgerReferenceID(order.ID, payoutTransactionID),
		SourceEvent:         sourceEvent,
		ReversalOfEntryID:   s.findLatestLedgerEntryIDLocked(order.ID, domain.LedgerEntryTypePayoutHold),
		CreatedAt:           time.Now(),
	}
	s.appendLedgerEntryLocked(entry)
}

func (s *InMemoryPayoutStore) finalizeHold(order domain.PayoutOrder) {
	key := balanceKey(order.MerchantID, order.Currency)
	s.pendingBalances[key] -= order.TotalDebitCents
	if s.pendingBalances[key] < 0 {
		s.pendingBalances[key] = 0
	}
	txID := int64(0)
	if attempts := s.attempts[order.PayoutNo]; len(attempts) > 0 {
		txID = attempts[len(attempts)-1].ID
	}
	s.appendLedgerEntryLocked(domain.LedgerEntry{
		MerchantID:          order.MerchantID,
		PayoutOrderID:       order.ID,
		PayoutTransactionID: txID,
		PayoutNo:            order.PayoutNo,
		AmountCents:         order.TotalDebitCents,
		Direction:           domain.LedgerDirectionDebit,
		Type:                domain.LedgerEntryTypePayoutComplete,
		Currency:            order.Currency,
		BalanceBeforeCents:  s.balances[key],
		BalanceAfterCents:   s.balances[key],
		ReferenceType:       payoutLedgerReferenceType(txID),
		ReferenceID:         payoutLedgerReferenceID(order.ID, txID),
		SourceEvent:         domain.LedgerSourceEventPayoutComplete,
		CreatedAt:           time.Now(),
	})
}

func (s *InMemoryPayoutStore) restoreCompleted(order domain.PayoutOrder) {
	key := balanceKey(order.MerchantID, order.Currency)
	availableBefore := s.balances[key]
	s.balances[key] += order.TotalDebitCents
	txID := int64(0)
	if attempts := s.attempts[order.PayoutNo]; len(attempts) > 0 {
		txID = attempts[len(attempts)-1].ID
	}
	s.appendLedgerEntryLocked(domain.LedgerEntry{
		MerchantID:          order.MerchantID,
		PayoutOrderID:       order.ID,
		PayoutTransactionID: txID,
		PayoutNo:            order.PayoutNo,
		AmountCents:         order.TotalDebitCents,
		Direction:           domain.LedgerDirectionCredit,
		Type:                domain.LedgerEntryTypeReversal,
		Currency:            order.Currency,
		BalanceBeforeCents:  availableBefore,
		BalanceAfterCents:   s.balances[key],
		ReferenceType:       payoutLedgerReferenceType(txID),
		ReferenceID:         payoutLedgerReferenceID(order.ID, txID),
		SourceEvent:         domain.LedgerSourceEventPayoutReverse,
		ReversalOfEntryID:   s.findLatestLedgerEntryIDLocked(order.ID, domain.LedgerEntryTypePayoutComplete),
		CreatedAt:           time.Now(),
	})
}

func (s *InMemoryPayoutStore) LedgerEntries() []domain.LedgerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]domain.LedgerEntry, len(s.ledgerEntries))
	copy(entries, s.ledgerEntries)
	return entries
}

func (s *InMemoryPayoutStore) appendLedgerEntryLocked(entry domain.LedgerEntry) {
	entry.ID = s.nextLedgerID
	s.nextLedgerID++
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	s.ledgerEntries = append(s.ledgerEntries, entry)
}

func (s *InMemoryPayoutStore) findLatestLedgerEntryIDLocked(payoutOrderID int64, entryType string) int64 {
	for idx := len(s.ledgerEntries) - 1; idx >= 0; idx-- {
		entry := s.ledgerEntries[idx]
		if entry.PayoutOrderID == payoutOrderID && entry.Type == entryType {
			return entry.ID
		}
	}
	return 0
}

func balanceKey(merchantID int64, currency string) string {
	return fmt.Sprintf("%d|%s", merchantID, currency)
}

type MySQLPayoutStore struct {
	db     *sql.DB
	cipher MerchantSecretCipher
}

func NewMySQLPayoutStore(db *sql.DB, secretCipher MerchantSecretCipher) *MySQLPayoutStore {
	return &MySQLPayoutStore{db: db, cipher: secretCipher}
}

func (s *MySQLPayoutStore) FindMerchantByCode(ctx context.Context, code string) (domain.Merchant, error) {
	var merchant domain.Merchant
	err := s.db.QueryRowContext(ctx, `
		SELECT id, code, name, api_key_hash, status, COALESCE(callback_url, ''), created_at, updated_at
		FROM merchants
		WHERE code = ?
		LIMIT 1
	`, code).Scan(&merchant.ID, &merchant.Code, &merchant.Name, &merchant.APIKey, &merchant.Status, &merchant.CallbackURL, &merchant.CreatedAt, &merchant.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Merchant{}, ErrNotFound
	}
	return merchant, err
}

func (s *MySQLPayoutStore) ValidateMerchantAPIKey(ctx context.Context, merchantID int64, apiKey string) (bool, error) {
	keyHash := hashMerchantAPIKey(apiKey)

	var anyManagedKey int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM merchant_api_keys
		WHERE merchant_id = ?
	`, merchantID).Scan(&anyManagedKey)
	if err != nil {
		return false, err
	}
	if anyManagedKey > 0 {
		var matched int
		err = s.db.QueryRowContext(ctx, `
		SELECT 1
		FROM merchant_api_keys
		WHERE merchant_id = ?
		  AND status = 'active'
		  AND key_hash = ?
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		LIMIT 1
	`, merchantID, keyHash).Scan(&matched)
		if err == nil {
			_, _ = s.db.ExecContext(ctx, `
				UPDATE merchant_api_keys
				SET last_used_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
				WHERE merchant_id = ? AND key_hash = ?
			`, merchantID, keyHash)
			return true, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		return false, nil
	}

	var legacySecret string
	err = s.db.QueryRowContext(ctx, `
		SELECT api_key_hash
		FROM merchants
		WHERE id = ?
		LIMIT 1
	`, merchantID).Scan(&legacySecret)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return merchantAPIKeyMatches(legacySecret, apiKey), nil
}

func (s *MySQLPayoutStore) GetActiveMerchantAPIKeySecret(ctx context.Context, merchantID int64) (string, error) {
	var ciphertext string
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(secret_ciphertext, '')
		FROM merchant_api_keys
		WHERE merchant_id = ?
		  AND status = 'active'
		  AND is_primary = TRUE
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY id DESC
		LIMIT 1
	`, merchantID).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ciphertext) == "" {
		return "", ErrNotFound
	}
	return s.cipher.Decrypt(ciphertext)
}

func (s *MySQLPayoutStore) ListMerchantAPIKeys(ctx context.Context, merchantCode string) ([]MerchantAPIKeyRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	merchant, err := findMerchantForUpdate(ctx, tx, strings.TrimSpace(merchantCode))
	if err != nil {
		return nil, err
	}
	records, err := listMerchantAPIKeysTx(ctx, tx, merchant.ID)
	if err != nil {
		return nil, err
	}
	return records, tx.Commit()
}

func (s *MySQLPayoutStore) ListMerchantAPIKeyAuditLogs(ctx context.Context, merchantCode string, limit int) ([]MerchantAPIKeyAuditEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	merchant, err := findMerchantForUpdate(ctx, tx, strings.TrimSpace(merchantCode))
	if err != nil {
		return nil, err
	}
	entries, err := listMerchantAPIKeyAuditLogsTx(ctx, tx, merchant.ID, limit)
	if err != nil {
		return nil, err
	}
	return entries, tx.Commit()
}

func (s *MySQLPayoutStore) ListPayoutReviewAuditLogs(ctx context.Context, payoutNo string, limit int) ([]PayoutReviewAuditEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, strings.TrimSpace(payoutNo), true)
	if err != nil {
		return nil, err
	}
	entries, err := listPayoutReviewAuditLogsTx(ctx, tx, order.ID, limit)
	if err != nil {
		return nil, err
	}
	return entries, tx.Commit()
}

func (s *MySQLPayoutStore) ListPayoutOperationalAlerts(ctx context.Context, status string, limit int) ([]domain.PayoutOperationalAlert, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	alerts, err := listPayoutOperationalAlertsTx(ctx, tx, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	return alerts, tx.Commit()
}

func (s *MySQLPayoutStore) IssueMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, expiresAt, previousExpiresAt *time.Time, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	merchant, err := findMerchantForUpdate(ctx, tx, strings.TrimSpace(merchantCode))
	if err != nil {
		return nil, err
	}
	secretCiphertext, err := s.encryptMerchantSecret(apiKey)
	if err != nil {
		return nil, err
	}
	if err := issueMerchantAPIKeyTx(ctx, tx, merchant.ID, apiKey, secretCiphertext, expiresAt, previousExpiresAt); err != nil {
		return nil, err
	}
	record, err := findMerchantAPIKeyRecordTx(ctx, tx, merchant.ID, apiKey)
	if err != nil {
		return nil, err
	}
	if err := insertMerchantAPIKeyAuditLogTx(ctx, tx, merchant.ID, audit.withAction("issue").withKeyHash(record.KeyHash)); err != nil {
		return nil, err
	}
	records, err := listMerchantAPIKeysTx(ctx, tx, merchant.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *MySQLPayoutStore) RotateMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, expiresAt, previousExpiresAt *time.Time, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	merchant, err := findMerchantForUpdate(ctx, tx, strings.TrimSpace(merchantCode))
	if err != nil {
		return nil, err
	}
	secretCiphertext, err := s.encryptMerchantSecret(apiKey)
	if err != nil {
		return nil, err
	}
	if err := issueMerchantAPIKeyTx(ctx, tx, merchant.ID, apiKey, secretCiphertext, expiresAt, previousExpiresAt); err != nil {
		return nil, err
	}
	record, err := findMerchantAPIKeyRecordTx(ctx, tx, merchant.ID, apiKey)
	if err != nil {
		return nil, err
	}
	if err := insertMerchantAPIKeyAuditLogTx(ctx, tx, merchant.ID, audit.withAction("rotate").withKeyHash(record.KeyHash)); err != nil {
		return nil, err
	}
	records, err := listMerchantAPIKeysTx(ctx, tx, merchant.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *MySQLPayoutStore) RevokeMerchantAPIKey(ctx context.Context, merchantCode, apiKey string, audit MerchantAPIKeyAuditLog) ([]MerchantAPIKeyRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	merchant, err := findMerchantForUpdate(ctx, tx, strings.TrimSpace(merchantCode))
	if err != nil {
		return nil, err
	}
	if err := revokeMerchantAPIKeyTx(ctx, tx, merchant.ID, apiKey); err != nil {
		return nil, err
	}
	if err := insertMerchantAPIKeyAuditLogTx(ctx, tx, merchant.ID, audit.withAction("revoke").withKeyHash(hashMerchantAPIKey(apiKey))); err != nil {
		return nil, err
	}
	records, err := listMerchantAPIKeysTx(ctx, tx, merchant.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

func (l MerchantAPIKeyAuditLog) withAction(action string) MerchantAPIKeyAuditLog {
	l.Action = action
	return l
}

func (l MerchantAPIKeyAuditLog) withKeyHash(keyHash string) MerchantAPIKeyAuditLog {
	l.KeyHash = strings.ToLower(strings.TrimSpace(keyHash))
	return l
}

func (s *MySQLPayoutStore) encryptMerchantSecret(secret string) (string, error) {
	if !s.cipher.Enabled() {
		return "", errors.New("merchant secret cipher is not configured")
	}
	return s.cipher.Encrypt(secret)
}

func (s *MySQLPayoutStore) CreatePayoutOrder(ctx context.Context, order domain.PayoutOrder, beneficiary domain.PayoutBeneficiary) (domain.PayoutOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	defer rollback(tx)

	merchant, err := findMerchantForUpdate(ctx, tx, order.MerchantCode)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	order.MerchantID = merchant.ID
	if existing, err := s.findPayoutOrderByMerchantPayoutNoTx(ctx, tx, order.MerchantCode, order.MerchantPayoutNo, false); err == nil {
		return existing, tx.Commit()
	} else if !errors.Is(err, ErrNotFound) {
		return domain.PayoutOrder{}, err
	}

	available, pending, err := ensureMerchantBalanceForUpdate(ctx, tx, merchant.ID, order.Currency)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if available < order.TotalDebitCents {
		return domain.PayoutOrder{}, fmt.Errorf("insufficient merchant balance")
	}

	now := time.Now()
	order.CreatedAt = now
	order.UpdatedAt = now
	result, err := tx.ExecContext(ctx, `
		INSERT INTO payout_orders (
			merchant_id, payout_no, merchant_payout_no, provider_code, amount_cents, fee_cents, total_debit_cents,
			currency, status, callback_url, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, merchant.ID, order.PayoutNo, order.MerchantPayoutNo, order.Provider, order.AmountCents, order.FeeCents, order.TotalDebitCents, order.Currency, string(order.Status), nullableString(order.CallbackURL), now, now)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	order.ID, err = result.LastInsertId()
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	encryptedAccountName, err := s.cipher.EncryptIfConfigured(beneficiary.PayAccountName)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	encryptedCardNo, err := s.cipher.EncryptIfConfigured(beneficiary.PayCardNo)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	encryptedValidateID, err := s.cipher.EncryptIfConfigured(beneficiary.PayValidateID)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	beneficiary.PayoutOrderID = order.ID
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payout_beneficiaries (
			payout_order_id, pay_account_name, pay_card_no, pay_bank_name, pay_sub_branch, pay_sub_branch_code, pay_city, pay_validate_id, pay_currency, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.ID, encryptedAccountName, encryptedCardNo, beneficiary.PayBankName, nullableString(beneficiary.PaySubBranch), nullableString(beneficiary.PaySubBranchCode), nullableString(beneficiary.PayCity), nullableString(encryptedValidateID), nullableString(beneficiary.PayCurrency), now); err != nil {
		return domain.PayoutOrder{}, err
	}

	availableAfter := available - order.TotalDebitCents
	pendingAfter := pending + order.TotalDebitCents
	if _, err := tx.ExecContext(ctx, `
		UPDATE merchant_balances
		SET available_cents = ?, pending_cents = ?, updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ? AND currency = ?
	`, availableAfter, pendingAfter, merchant.ID, order.Currency); err != nil {
		return domain.PayoutOrder{}, err
	}
	entry := domain.LedgerEntry{
		MerchantID:         merchant.ID,
		PayoutOrderID:      order.ID,
		PayoutNo:           order.PayoutNo,
		AmountCents:        order.TotalDebitCents,
		Direction:          domain.LedgerDirectionDebit,
		Type:               domain.LedgerEntryTypePayoutHold,
		Currency:           order.Currency,
		BalanceBeforeCents: available,
		BalanceAfterCents:  availableAfter,
		ReferenceType:      domain.LedgerReferenceTypePayoutOrder,
		ReferenceID:        order.ID,
		SourceEvent:        domain.LedgerSourceEventPayoutHold,
	}
	if _, err := insertLedgerEntry(ctx, tx, entry); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, err
	}
	order.MerchantCode = merchant.Code
	order.PayAccountName = beneficiary.PayAccountName
	order.PayCardNo = beneficiary.PayCardNo
	order.PayBankName = beneficiary.PayBankName
	order.PaySubBranch = beneficiary.PaySubBranch
	order.PaySubBranchCode = beneficiary.PaySubBranchCode
	order.PayCity = beneficiary.PayCity
	order.PayValidateID = beneficiary.PayValidateID
	order.PayCurrency = beneficiary.PayCurrency
	return order, nil
}

func (s *MySQLPayoutStore) FindPayoutOrderByPayoutNo(ctx context.Context, payoutNo string) (domain.PayoutOrder, error) {
	return s.findPayoutOrderByPayoutNoTx(ctx, nil, payoutNo, false)
}

func (s *MySQLPayoutStore) FindPayoutOrderByMerchantPayoutNo(ctx context.Context, merchantCode, merchantPayoutNo string) (domain.PayoutOrder, error) {
	return s.findPayoutOrderByMerchantPayoutNoTx(ctx, nil, merchantCode, merchantPayoutNo, false)
}

func (s *MySQLPayoutStore) ApprovePayoutOrder(ctx context.Context, payoutNo string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, payoutNo, true)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if order.Status != domain.PayoutOrderStatusPendingReview {
		return domain.PayoutOrder{}, fmt.Errorf("payout status %s cannot be approved", order.Status)
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE payout_orders
		SET status = ?, approved_at = ?, updated_at = ?
		WHERE id = ?
	`, string(domain.PayoutOrderStatusApproved), now, now, order.ID); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := insertPayoutReviewAuditLogTx(ctx, tx, order.MerchantID, order.ID, audit.withAction("approve")); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, err
	}
	order.Status = domain.PayoutOrderStatusApproved
	order.ApprovedAt = &now
	order.UpdatedAt = now
	return order, nil
}

func (s *MySQLPayoutStore) RejectPayoutOrder(ctx context.Context, payoutNo, reason string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, payoutNo, true)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if order.Status.IsTerminal() {
		return domain.PayoutOrder{}, fmt.Errorf("payout status %s cannot be rejected", order.Status)
	}
	if err := releasePayoutHoldTx(ctx, tx, order, "rejected", strings.TrimSpace(reason), 0); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := insertPayoutReviewAuditLogTx(ctx, tx, order.MerchantID, order.ID, audit.withAction("reject")); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, err
	}
	order.Status = domain.PayoutOrderStatusRejected
	order.FailureMessage = strings.TrimSpace(reason)
	order.UpdatedAt = time.Now()
	return order, nil
}

func (s *MySQLPayoutStore) CancelPayoutOrder(ctx context.Context, payoutNo, reason string, audit PayoutReviewAuditLog) (domain.PayoutOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, payoutNo, true)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	attemptCount, err := payoutAttemptCount(ctx, tx, order.ID)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if !canCancelPayoutOrder(order, attemptCount) {
		return domain.PayoutOrder{}, fmt.Errorf("payout status %s cannot be cancelled", order.Status)
	}
	if err := releasePayoutHoldTx(ctx, tx, order, "cancelled", strings.TrimSpace(reason), 0); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := insertPayoutReviewAuditLogTx(ctx, tx, order.MerchantID, order.ID, audit.withAction("cancel")); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, err
	}
	now := time.Now()
	order.Status = domain.PayoutOrderStatusCancelled
	order.FailureMessage = strings.TrimSpace(reason)
	order.CompletedAt = &now
	order.UpdatedAt = now
	return order, nil
}

func (s *MySQLPayoutStore) CreatePayoutReviewAuditLog(ctx context.Context, payoutNo string, audit PayoutReviewAuditLog) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, strings.TrimSpace(payoutNo), true)
	if err != nil {
		return err
	}
	if err := insertPayoutReviewAuditLogTx(ctx, tx, order.MerchantID, order.ID, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLPayoutStore) UpsertPayoutOperationalAlert(ctx context.Context, payoutNo string, alert PayoutOperationalAlertUpsert) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, strings.TrimSpace(payoutNo), true)
	if err != nil {
		return err
	}
	alert.MerchantID = order.MerchantID
	alert.PayoutOrderID = order.ID
	if err := upsertPayoutOperationalAlertTx(ctx, tx, alert, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLPayoutStore) ResolvePayoutOperationalAlert(ctx context.Context, alertID int64, resolve PayoutOperationalAlertResolve) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := resolvePayoutOperationalAlertTx(ctx, tx, alertID, resolve, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLPayoutStore) MarkPayoutSubmitted(ctx context.Context, payoutNo string, txAttempt domain.PayoutTransaction) (domain.PayoutOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, payoutNo, true)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	now := time.Now()
	providerID, err := ensureProvider(ctx, tx, normalizeProviderCode(order.Provider))
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if txAttempt.AttemptNo <= 0 {
		txAttempt.AttemptNo, err = nextPayoutAttemptNo(ctx, tx, order.ID)
		if err != nil {
			return domain.PayoutOrder{}, err
		}
	}
	requestPayload := nullableJSON(txAttempt.RequestPayload)
	responsePayload := nullableJSON(txAttempt.ResponsePayload)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO payout_transactions (
			payout_order_id, provider_id, attempt_no, provider_order_no, provider_trade_no, request_payload, response_payload, status, error_message, submitted_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.ID, providerID, txAttempt.AttemptNo, nullableString(txAttempt.ProviderOrderNo), nullableString(txAttempt.ProviderTradeNo), requestPayload, responsePayload, txAttempt.Status, nullableString(txAttempt.ErrorMessage), now, now, now)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	txID, err := result.LastInsertId()
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE payout_orders
		SET status = ?, provider_order_no = ?, provider_trade_no = ?, submitted_at = ?, updated_at = ?
		WHERE id = ?
	`, string(domain.PayoutOrderStatusProcessing), nullableString(txAttempt.ProviderOrderNo), nullableString(txAttempt.ProviderTradeNo), now, now, order.ID); err != nil {
		return domain.PayoutOrder{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, err
	}
	order.Status = domain.PayoutOrderStatusProcessing
	order.ProviderOrderNo = txAttempt.ProviderOrderNo
	order.ProviderTradeNo = txAttempt.ProviderTradeNo
	order.SubmittedAt = &now
	order.UpdatedAt = now
	_ = txID
	return order, nil
}

func (s *MySQLPayoutStore) MarkPayoutSubmissionFailure(ctx context.Context, payoutNo string, txAttempt domain.PayoutTransaction, retryable bool) (domain.PayoutOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByPayoutNoTx(ctx, tx, payoutNo, true)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	now := time.Now()
	providerID, err := ensureProvider(ctx, tx, normalizeProviderCode(order.Provider))
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	if txAttempt.AttemptNo <= 0 {
		txAttempt.AttemptNo, err = nextPayoutAttemptNo(ctx, tx, order.ID)
		if err != nil {
			return domain.PayoutOrder{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payout_transactions (
			payout_order_id, provider_id, attempt_no, provider_order_no, provider_trade_no, request_payload, response_payload, status, error_message, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.ID, providerID, txAttempt.AttemptNo, nullableString(txAttempt.ProviderOrderNo), nullableString(txAttempt.ProviderTradeNo), nullableJSON(txAttempt.RequestPayload), nullableJSON(txAttempt.ResponsePayload), txAttempt.Status, nullableString(txAttempt.ErrorMessage), now, now); err != nil {
		return domain.PayoutOrder{}, err
	}
	if retryable {
		if _, err := tx.ExecContext(ctx, `
			UPDATE payout_orders
			SET status = ?, failure_message = ?, updated_at = ?
			WHERE id = ?
		`, string(domain.PayoutOrderStatusApproved), nullableString(txAttempt.ErrorMessage), now, order.ID); err != nil {
			return domain.PayoutOrder{}, err
		}
		order.Status = domain.PayoutOrderStatusApproved
	} else {
		if err := releasePayoutHoldTx(ctx, tx, order, "failed", txAttempt.ErrorMessage, 0); err != nil {
			return domain.PayoutOrder{}, err
		}
		order.Status = domain.PayoutOrderStatusFailed
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, err
	}
	order.FailureMessage = txAttempt.ErrorMessage
	order.UpdatedAt = now
	if !retryable {
		order.CompletedAt = &now
	}
	return order, nil
}

func (s *MySQLPayoutStore) ApplyPayoutResult(ctx context.Context, result PayoutProviderResult) (domain.PayoutOrder, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PayoutOrder{}, false, err
	}
	defer rollback(tx)
	order, err := s.findPayoutOrderByMerchantPayoutNoTx(ctx, tx, "", result.MerchantPayoutNo, true)
	if err != nil {
		return domain.PayoutOrder{}, false, err
	}
	providerID, err := ensureProvider(ctx, tx, normalizeProviderCode(order.Provider))
	if err != nil {
		return domain.PayoutOrder{}, false, err
	}
	if result.EventKey != "" {
		var existingID int64
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM payout_callbacks WHERE provider_id = ? AND provider_event_key = ? LIMIT 1
		`, providerID, result.EventKey).Scan(&existingID)
		if err == nil {
			return order, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.PayoutOrder{}, false, err
		}
	}

	now := result.CompletedAt
	if now.IsZero() {
		now = time.Now()
	}
	transactionID, _ := findLatestPayoutTransactionID(ctx, tx, order.ID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payout_callbacks (
			provider_id, payout_order_id, payout_transaction_id, provider_order_no, provider_trade_no, provider_event_key, payload, status, error_message, received_at, processed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'processed', NULL, ?, ?)
	`, providerID, order.ID, nullableInt64Ptr(transactionID), nullableString(result.ProviderOrderNo), nullableString(result.ProviderTradeNo), nullableString(result.EventKey), result.Payload, now, now); err != nil {
		return domain.PayoutOrder{}, false, err
	}

	changed := false
	switch result.StatusCode {
	case "30000":
		if order.Status != domain.PayoutOrderStatusCompleted {
			if err := finalizePayoutHoldTx(ctx, tx, order, result.ProviderOrderNo, result.ProviderTradeNo, now); err != nil {
				return domain.PayoutOrder{}, false, err
			}
			order.Status = domain.PayoutOrderStatusCompleted
			order.CompletedAt = &now
			changed = true
		}
	case "40000":
		if order.Status == domain.PayoutOrderStatusCompleted {
			if err := ensurePayoutReversalMatchesCompletedTransaction(order, result); err != nil {
				return domain.PayoutOrder{}, false, err
			}
			if err := restorePayoutTx(ctx, tx, order, result.ProviderOrderNo, result.ProviderTradeNo, now); err != nil {
				return domain.PayoutOrder{}, false, err
			}
			order.Status = domain.PayoutOrderStatusReversed
			order.CompletedAt = &now
			changed = true
		} else if order.Status != domain.PayoutOrderStatusFailed && order.Status != domain.PayoutOrderStatusRejected {
			if err := releasePayoutHoldTx(ctx, tx, order, "failed", result.StatusMessage, transactionID); err != nil {
				return domain.PayoutOrder{}, false, err
			}
			order.Status = domain.PayoutOrderStatusFailed
			order.CompletedAt = &now
			changed = true
		}
	default:
		return domain.PayoutOrder{}, false, fmt.Errorf("unsupported payout status code: %s", result.StatusCode)
	}
	if err := tx.Commit(); err != nil {
		return domain.PayoutOrder{}, false, err
	}
	order.ProviderOrderNo = result.ProviderOrderNo
	order.ProviderTradeNo = result.ProviderTradeNo
	order.UpdatedAt = now
	order.FailureMessage = result.StatusMessage
	return order, changed, nil
}

func (s *MySQLPayoutStore) ListPayoutsForReconcile(ctx context.Context, statuses []domain.PayoutOrderStatus, before time.Time, limit int) ([]domain.PayoutOrder, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	holders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses)+2)
	for _, status := range statuses {
		holders = append(holders, "?")
		args = append(args, string(status))
	}
	query := `
		SELECT po.id, m.id, m.code, po.payout_no, po.merchant_payout_no, po.provider_code, COALESCE(po.provider_order_no, ''), COALESCE(po.provider_trade_no, ''),
		       po.amount_cents, po.fee_cents, po.total_debit_cents, po.currency, po.status, COALESCE(po.failure_code, ''), COALESCE(po.failure_message, ''),
		       COALESCE(po.callback_url, ''), COALESCE(pb.pay_account_name, ''), COALESCE(pb.pay_card_no, ''), COALESCE(pb.pay_bank_name, ''),
		       COALESCE(pb.pay_sub_branch, ''), COALESCE(pb.pay_sub_branch_code, ''), COALESCE(pb.pay_city, ''), COALESCE(pb.pay_validate_id, ''), COALESCE(pb.pay_currency, ''),
		       po.approved_at, po.submitted_at, po.completed_at, po.created_at, po.updated_at
		FROM payout_orders po
		JOIN merchants m ON m.id = po.merchant_id
		LEFT JOIN payout_beneficiaries pb ON pb.payout_order_id = po.id
		WHERE po.status IN (` + strings.Join(holders, ",") + `)
	`
	if !before.IsZero() {
		query += " AND po.updated_at <= ?"
		args = append(args, before)
	}
	query += " ORDER BY po.updated_at ASC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.PayoutOrder
	for rows.Next() {
		order, err := scanPayoutOrder(rows)
		if err != nil {
			return nil, err
		}
		order, err = s.decryptPayoutOrderSensitiveFields(order)
		if err != nil {
			return nil, err
		}
		result = append(result, order)
	}
	return result, rows.Err()
}

func (s *MySQLPayoutStore) CreateMerchantPayoutCallbackTask(ctx context.Context, task domain.MerchantPayoutCallbackTask) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO merchant_payout_callback_tasks (
			merchant_id, payout_order_id, callback_url, payload, status, retry_count, next_retry_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, task.MerchantID, task.PayoutOrderID, task.CallbackURL, task.Payload, "pending", 0, task.NextRetryAt)
	return err
}

func (s *MySQLPayoutStore) ClaimDueMerchantPayoutCallbackTasks(ctx context.Context, before, staleBefore time.Time, limit int) ([]domain.MerchantPayoutCallbackTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, merchant_id, payout_order_id, callback_url, payload, status, retry_count, next_retry_at, COALESCE(last_error, ''), sent_at, created_at, updated_at
		FROM merchant_payout_callback_tasks
		WHERE (status = 'pending' AND next_retry_at <= ?) OR (status = 'processing' AND claimed_at <= ?)
		ORDER BY next_retry_at ASC
		LIMIT ?
		FOR UPDATE
	`, before, staleBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []domain.MerchantPayoutCallbackTask
	for rows.Next() {
		var task domain.MerchantPayoutCallbackTask
		var sentAt sql.NullTime
		if err := rows.Scan(&task.ID, &task.MerchantID, &task.PayoutOrderID, &task.CallbackURL, &task.Payload, &task.Status, &task.RetryCount, &task.NextRetryAt, &task.LastError, &sentAt, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			task.SentAt = &sentAt.Time
		}
		task.ClaimToken = newCallbackClaimToken()
		if _, err := tx.ExecContext(ctx, `UPDATE merchant_payout_callback_tasks SET status = 'processing', claim_token = ?, claimed_at = ?, updated_at = ? WHERE id = ?`, task.ClaimToken, before, before, task.ID); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *MySQLPayoutStore) MarkMerchantPayoutCallbackTaskResult(ctx context.Context, taskID int64, claimToken string, success bool, nextRetryAt time.Time, errorMessage string) error {
	if success {
		_, err := s.db.ExecContext(ctx, `
			UPDATE merchant_payout_callback_tasks
			SET status = 'sent', sent_at = CURRENT_TIMESTAMP, last_error = NULL, claim_token = NULL, claimed_at = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status = 'processing' AND claim_token = ?
		`, taskID, claimToken)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE merchant_payout_callback_tasks
		SET status = CASE WHEN retry_count + 1 >= ? THEN 'dead_letter' ELSE 'pending' END,
			retry_count = retry_count + 1, next_retry_at = ?, last_error = ?, claim_token = NULL, claimed_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'processing' AND claim_token = ?
	`, maxMerchantPayoutCallbackAttempts, nextRetryAt, nullableString(errorMessage), taskID, claimToken)
	return err
}

func ensurePayoutReversalMatchesCompletedTransaction(order domain.PayoutOrder, result PayoutProviderResult) error {
	completedTransactionID := strings.TrimSpace(order.ProviderOrderNo)
	reversalTransactionID := strings.TrimSpace(result.ProviderOrderNo)
	if completedTransactionID == "" || reversalTransactionID == "" || completedTransactionID != reversalTransactionID {
		return errors.New("payout reversal transaction does not match completed payout")
	}
	return nil
}

func findMerchantForUpdate(ctx context.Context, tx *sql.Tx, code string) (domain.Merchant, error) {
	var merchant domain.Merchant
	err := tx.QueryRowContext(ctx, `
		SELECT id, code, name, api_key_hash, status, COALESCE(callback_url, ''), created_at, updated_at
		FROM merchants
		WHERE code = ?
		FOR UPDATE
	`, code).Scan(&merchant.ID, &merchant.Code, &merchant.Name, &merchant.APIKey, &merchant.Status, &merchant.CallbackURL, &merchant.CreatedAt, &merchant.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Merchant{}, ErrNotFound
	}
	return merchant, err
}

func ensureMerchantBalanceForUpdate(ctx context.Context, tx *sql.Tx, merchantID int64, currency string) (int64, int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchant_balances (merchant_id, currency, available_cents, pending_cents)
		VALUES (?, ?, 0, 0)
		ON DUPLICATE KEY UPDATE updated_at = CURRENT_TIMESTAMP
	`, merchantID, currency); err != nil {
		return 0, 0, err
	}
	var available, pending int64
	err := tx.QueryRowContext(ctx, `
		SELECT available_cents, pending_cents
		FROM merchant_balances
		WHERE merchant_id = ? AND currency = ?
		FOR UPDATE
	`, merchantID, currency).Scan(&available, &pending)
	return available, pending, err
}

func applyLedgerEntryAndBalanceUpdate(ctx context.Context, tx *sql.Tx, entry domain.LedgerEntry, availableDelta, pendingDelta int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE merchant_balances
		SET available_cents = available_cents + ?, pending_cents = pending_cents + ?, updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ? AND currency = ?
	`, availableDelta, pendingDelta, entry.MerchantID, entry.Currency); err != nil {
		return err
	}
	_, err := insertLedgerEntry(ctx, tx, entry)
	return err
}

func insertLedgerEntry(ctx context.Context, tx *sql.Tx, entry domain.LedgerEntry) (int64, error) {
	if strings.TrimSpace(entry.Direction) == "" {
		return 0, fmt.Errorf("ledger direction is required")
	}
	if strings.TrimSpace(entry.Type) == "" {
		return 0, fmt.Errorf("ledger type is required")
	}
	if strings.TrimSpace(entry.ReferenceType) == "" || entry.ReferenceID == 0 {
		return 0, fmt.Errorf("ledger reference is required")
	}
	if strings.TrimSpace(entry.SourceEvent) == "" {
		return 0, fmt.Errorf("ledger source event is required")
	}
	entryNo := buildLedgerEntryNo(entry)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (
			merchant_id, order_id, payout_order_id, provider_transaction_id, payout_transaction_id,
			entry_no, direction, type, amount_cents, currency,
			balance_before_cents, balance_after_cents, reference_type, reference_id, source_event, reversal_of_entry_id
		) VALUES (?, NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0))
	`, entry.MerchantID, entry.OrderID, entry.PayoutOrderID, entry.ProviderTransactionID, entry.PayoutTransactionID,
		entryNo, entry.Direction, entry.Type, entry.AmountCents, entry.Currency,
		entry.BalanceBeforeCents, entry.BalanceAfterCents, entry.ReferenceType, entry.ReferenceID, entry.SourceEvent, entry.ReversalOfEntryID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	entry.ID = id
	return id, nil
}

func buildLedgerEntryNo(entry domain.LedgerEntry) string {
	suffix := ""
	if entry.ReversalOfEntryID != 0 {
		suffix = fmt.Sprintf("R%d", entry.ReversalOfEntryID)
	} else if entry.ReferenceID != 0 {
		suffix = fmt.Sprintf("X%d", entry.ReferenceID)
	}
	switch {
	case entry.OrderID != 0:
		return fmt.Sprintf("LED%s%s%s", entry.OrderNo, strings.ToUpper(strings.ReplaceAll(entry.Type, "_", "")), suffix)
	case entry.PayoutOrderID != 0:
		return fmt.Sprintf("LEP%s%s%s", entry.PayoutNo, strings.ToUpper(strings.ReplaceAll(entry.Type, "_", "")), suffix)
	default:
		return fmt.Sprintf("LEM%d%s%s", entry.MerchantID, strings.ToUpper(strings.ReplaceAll(entry.Type, "_", "")), suffix)
	}
}

func payoutLedgerReferenceType(payoutTransactionID int64) string {
	if payoutTransactionID != 0 {
		return domain.LedgerReferenceTypePayoutTransaction
	}
	return domain.LedgerReferenceTypePayoutOrder
}

func payoutLedgerReferenceID(payoutOrderID, payoutTransactionID int64) int64 {
	if payoutTransactionID != 0 {
		return payoutTransactionID
	}
	return payoutOrderID
}

func releasePayoutHoldTx(ctx context.Context, tx *sql.Tx, order domain.PayoutOrder, targetStatus, failureMessage string, payoutTransactionID int64) error {
	available, pending, err := ensureMerchantBalanceForUpdate(ctx, tx, order.MerchantID, order.Currency)
	if err != nil {
		return err
	}
	availableAfter := available + order.TotalDebitCents
	pendingAfter := pending - order.TotalDebitCents
	if pendingAfter < 0 {
		pendingAfter = 0
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE merchant_balances
		SET available_cents = ?, pending_cents = ?, updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ? AND currency = ?
	`, availableAfter, pendingAfter, order.MerchantID, order.Currency); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE payout_orders
		SET status = ?, failure_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, targetStatus, nullableString(failureMessage), now, now, order.ID); err != nil {
		return err
	}
	holdEntryID, err := findLatestLedgerEntryID(ctx, tx, order.ID, domain.LedgerEntryTypePayoutHold)
	if err != nil {
		return err
	}
	entry := domain.LedgerEntry{
		MerchantID:          order.MerchantID,
		PayoutOrderID:       order.ID,
		PayoutTransactionID: payoutTransactionID,
		PayoutNo:            order.PayoutNo,
		AmountCents:         order.TotalDebitCents,
		Direction:           domain.LedgerDirectionCredit,
		Type:                domain.LedgerEntryTypePayoutRelease,
		Currency:            order.Currency,
		BalanceBeforeCents:  available,
		BalanceAfterCents:   availableAfter,
		ReferenceType:       payoutLedgerReferenceType(payoutTransactionID),
		ReferenceID:         payoutLedgerReferenceID(order.ID, payoutTransactionID),
		SourceEvent:         payoutReleaseSourceEvent(targetStatus),
		ReversalOfEntryID:   holdEntryID,
	}
	_, err = insertLedgerEntry(ctx, tx, entry)
	return err
}

func finalizePayoutHoldTx(ctx context.Context, tx *sql.Tx, order domain.PayoutOrder, providerOrderNo, providerTradeNo string, completedAt time.Time) error {
	available, pending, err := ensureMerchantBalanceForUpdate(ctx, tx, order.MerchantID, order.Currency)
	if err != nil {
		return err
	}
	pendingAfter := pending - order.TotalDebitCents
	if pendingAfter < 0 {
		pendingAfter = 0
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE merchant_balances
		SET pending_cents = ?, updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ? AND currency = ?
	`, pendingAfter, order.MerchantID, order.Currency); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE payout_orders
		SET status = ?, provider_order_no = ?, provider_trade_no = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, string(domain.PayoutOrderStatusCompleted), nullableString(providerOrderNo), nullableString(providerTradeNo), completedAt, completedAt, order.ID); err != nil {
		return err
	}
	txID, _ := findLatestPayoutTransactionID(ctx, tx, order.ID)
	entry := domain.LedgerEntry{
		MerchantID:          order.MerchantID,
		PayoutOrderID:       order.ID,
		PayoutTransactionID: txID,
		PayoutNo:            order.PayoutNo,
		AmountCents:         order.TotalDebitCents,
		Direction:           domain.LedgerDirectionDebit,
		Type:                domain.LedgerEntryTypePayoutComplete,
		Currency:            order.Currency,
		BalanceBeforeCents:  available,
		BalanceAfterCents:   available,
		ReferenceType:       payoutLedgerReferenceType(txID),
		ReferenceID:         payoutLedgerReferenceID(order.ID, txID),
		SourceEvent:         domain.LedgerSourceEventPayoutComplete,
	}
	_, err = insertLedgerEntry(ctx, tx, entry)
	return err
}

func restorePayoutTx(ctx context.Context, tx *sql.Tx, order domain.PayoutOrder, providerOrderNo, providerTradeNo string, completedAt time.Time) error {
	available, _, err := ensureMerchantBalanceForUpdate(ctx, tx, order.MerchantID, order.Currency)
	if err != nil {
		return err
	}
	availableAfter := available + order.TotalDebitCents
	if _, err := tx.ExecContext(ctx, `
		UPDATE merchant_balances
		SET available_cents = ?, updated_at = CURRENT_TIMESTAMP
		WHERE merchant_id = ? AND currency = ?
	`, availableAfter, order.MerchantID, order.Currency); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE payout_orders
		SET status = ?, provider_order_no = ?, provider_trade_no = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, string(domain.PayoutOrderStatusReversed), nullableString(providerOrderNo), nullableString(providerTradeNo), completedAt, completedAt, order.ID); err != nil {
		return err
	}
	txID, _ := findLatestPayoutTransactionID(ctx, tx, order.ID)
	completedEntryID, err := findLatestLedgerEntryID(ctx, tx, order.ID, domain.LedgerEntryTypePayoutComplete)
	if err != nil {
		return err
	}
	entry := domain.LedgerEntry{
		MerchantID:          order.MerchantID,
		PayoutOrderID:       order.ID,
		PayoutTransactionID: txID,
		PayoutNo:            order.PayoutNo,
		AmountCents:         order.TotalDebitCents,
		Direction:           domain.LedgerDirectionCredit,
		Type:                domain.LedgerEntryTypeReversal,
		Currency:            order.Currency,
		BalanceBeforeCents:  available,
		BalanceAfterCents:   availableAfter,
		ReferenceType:       payoutLedgerReferenceType(txID),
		ReferenceID:         payoutLedgerReferenceID(order.ID, txID),
		SourceEvent:         domain.LedgerSourceEventPayoutReverse,
		ReversalOfEntryID:   completedEntryID,
	}
	_, err = insertLedgerEntry(ctx, tx, entry)
	return err
}

func findLatestLedgerEntryID(ctx context.Context, tx *sql.Tx, payoutOrderID int64, entryType string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM ledger_entries
		WHERE payout_order_id = ? AND type = ?
		ORDER BY id DESC
		LIMIT 1
	`, payoutOrderID, entryType).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func payoutReleaseSourceEvent(targetStatus string) string {
	switch strings.TrimSpace(targetStatus) {
	case string(domain.PayoutOrderStatusRejected):
		return domain.LedgerSourceEventPayoutReject
	case string(domain.PayoutOrderStatusCancelled):
		return domain.LedgerSourceEventPayoutCancel
	default:
		return domain.LedgerSourceEventPayoutFail
	}
}

func nextPayoutAttemptNo(ctx context.Context, tx *sql.Tx, payoutOrderID int64) (int, error) {
	var next int
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(attempt_no), 0) + 1
		FROM payout_transactions
		WHERE payout_order_id = ?
	`, payoutOrderID).Scan(&next)
	return next, err
}

func payoutAttemptCount(ctx context.Context, tx *sql.Tx, payoutOrderID int64) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM payout_transactions
		WHERE payout_order_id = ?
	`, payoutOrderID).Scan(&count)
	return count, err
}

func findLatestPayoutTransactionID(ctx context.Context, tx *sql.Tx, payoutOrderID int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM payout_transactions
		WHERE payout_order_id = ?
		ORDER BY attempt_no DESC, id DESC
		LIMIT 1
	`, payoutOrderID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func canCancelPayoutOrder(order domain.PayoutOrder, attemptCount int) bool {
	switch order.Status {
	case domain.PayoutOrderStatusPendingReview:
		return true
	case domain.PayoutOrderStatusApproved:
		return attemptCount == 0 && order.SubmittedAt == nil &&
			strings.TrimSpace(order.ProviderOrderNo) == "" &&
			strings.TrimSpace(order.ProviderTradeNo) == ""
	default:
		return false
	}
}

func (s *MySQLPayoutStore) findPayoutOrderByPayoutNoTx(ctx context.Context, tx *sql.Tx, payoutNo string, forUpdate bool) (domain.PayoutOrder, error) {
	query := payoutOrderSelectQuery() + ` WHERE po.payout_no = ? LIMIT 1`
	if forUpdate {
		query = payoutOrderSelectQuery() + ` WHERE po.payout_no = ? FOR UPDATE`
	}
	row := rowGetter(s.db, tx).QueryRowContext(ctx, query, payoutNo)
	order, err := scanPayoutOrderFromRow(row)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return s.decryptPayoutOrderSensitiveFields(order)
}

func (s *MySQLPayoutStore) findPayoutOrderByMerchantPayoutNoTx(ctx context.Context, tx *sql.Tx, merchantCode, merchantPayoutNo string, forUpdate bool) (domain.PayoutOrder, error) {
	query := payoutOrderSelectQuery() + ` WHERE po.merchant_payout_no = ?`
	args := []any{merchantPayoutNo}
	if strings.TrimSpace(merchantCode) != "" {
		query += ` AND m.code = ?`
		args = append(args, merchantCode)
	}
	if forUpdate {
		query += ` FOR UPDATE`
	} else {
		query += ` LIMIT 1`
	}
	row := rowGetter(s.db, tx).QueryRowContext(ctx, query, args...)
	order, err := scanPayoutOrderFromRow(row)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return s.decryptPayoutOrderSensitiveFields(order)
}

func payoutOrderSelectQuery() string {
	return `
		SELECT po.id, po.merchant_id, m.code, po.payout_no, po.merchant_payout_no, COALESCE(po.provider_code, ''), COALESCE(po.provider_order_no, ''),
		       COALESCE(po.provider_trade_no, ''), po.amount_cents, po.fee_cents, po.total_debit_cents, po.currency, po.status,
		       COALESCE(po.failure_code, ''), COALESCE(po.failure_message, ''), COALESCE(po.callback_url, ''),
		       COALESCE(pb.pay_account_name, ''), COALESCE(pb.pay_card_no, ''), COALESCE(pb.pay_bank_name, ''), COALESCE(pb.pay_sub_branch, ''),
		       COALESCE(pb.pay_sub_branch_code, ''), COALESCE(pb.pay_city, ''), COALESCE(pb.pay_validate_id, ''), COALESCE(pb.pay_currency, ''),
		       po.approved_at, po.submitted_at, po.completed_at, po.created_at, po.updated_at
		FROM payout_orders po
		JOIN merchants m ON m.id = po.merchant_id
		LEFT JOIN payout_beneficiaries pb ON pb.payout_order_id = po.id
	`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPayoutOrder(rows scanner) (domain.PayoutOrder, error) {
	return scanPayoutOrderFromRow(rows)
}

func scanPayoutOrderFromRow(row scanner) (domain.PayoutOrder, error) {
	var order domain.PayoutOrder
	var status string
	var approvedAt, submittedAt, completedAt sql.NullTime
	err := row.Scan(
		&order.ID, &order.MerchantID, &order.MerchantCode, &order.PayoutNo, &order.MerchantPayoutNo, &order.Provider,
		&order.ProviderOrderNo, &order.ProviderTradeNo, &order.AmountCents, &order.FeeCents, &order.TotalDebitCents,
		&order.Currency, &status, &order.FailureCode, &order.FailureMessage, &order.CallbackURL, &order.PayAccountName,
		&order.PayCardNo, &order.PayBankName, &order.PaySubBranch, &order.PaySubBranchCode, &order.PayCity,
		&order.PayValidateID, &order.PayCurrency, &approvedAt, &submittedAt, &completedAt, &order.CreatedAt, &order.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PayoutOrder{}, ErrNotFound
	}
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	order.Status = domain.PayoutOrderStatus(status)
	if approvedAt.Valid {
		order.ApprovedAt = &approvedAt.Time
	}
	if submittedAt.Valid {
		order.SubmittedAt = &submittedAt.Time
	}
	if completedAt.Valid {
		order.CompletedAt = &completedAt.Time
	}
	return order, nil
}

func (s *MySQLPayoutStore) decryptPayoutOrderSensitiveFields(order domain.PayoutOrder) (domain.PayoutOrder, error) {
	var err error
	order.PayAccountName, err = s.cipher.DecryptIfEncrypted(order.PayAccountName)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	order.PayCardNo, err = s.cipher.DecryptIfEncrypted(order.PayCardNo)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	order.PayValidateID, err = s.cipher.DecryptIfEncrypted(order.PayValidateID)
	if err != nil {
		return domain.PayoutOrder{}, err
	}
	return order, nil
}

type queryable interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func rowGetter(db *sql.DB, tx *sql.Tx) queryable {
	if tx != nil {
		return tx
	}
	return db
}

func nullableJSON(raw string) any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	return raw
}

func nullableInt64Ptr(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
