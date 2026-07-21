package service

import (
	"context"
	"io"
	"os"
	"strings"

	"payment-service/internal/domain"
	"payment-service/internal/repository"
)

type ManualPayoutService struct {
	store   *repository.ManualPayoutStore
	storage *LocalReceiptStorage
}

func NewManualPayoutService(store *repository.ManualPayoutStore, storage *LocalReceiptStorage) *ManualPayoutService {
	return &ManualPayoutService{store: store, storage: storage}
}
func (s *ManualPayoutService) Start(ctx context.Context, payoutNo, operator, requestID string) (domain.ManualPayoutCase, error) {
	return s.store.Start(ctx, strings.TrimSpace(payoutNo), strings.TrimSpace(operator), strings.TrimSpace(requestID))
}
func (s *ManualPayoutService) Find(ctx context.Context, payoutNo string) (domain.ManualPayoutCase, error) {
	return s.store.Find(ctx, strings.TrimSpace(payoutNo))
}
func (s *ManualPayoutService) UploadReceipt(ctx context.Context, payoutNo, filename, contentType, operator, requestID string, body io.Reader) (domain.ManualPayoutCase, error) {
	receipt, err := s.storage.Save(filename, contentType, operator, body)
	if err != nil {
		return domain.ManualPayoutCase{}, err
	}
	caseRow, err := s.store.AttachReceipt(ctx, strings.TrimSpace(payoutNo), receipt, strings.TrimSpace(requestID))
	if err != nil {
		_ = s.storage.Delete(receipt.StorageKey)
		return domain.ManualPayoutCase{}, err
	}
	return caseRow, nil
}
func (s *ManualPayoutService) Confirm(ctx context.Context, payoutNo, reviewer, requestID string) (domain.ManualPayoutCase, error) {
	return s.store.Confirm(ctx, strings.TrimSpace(payoutNo), strings.TrimSpace(reviewer), strings.TrimSpace(requestID))
}
func (s *ManualPayoutService) Fail(ctx context.Context, payoutNo, reviewer, reason, requestID string) (domain.ManualPayoutCase, error) {
	return s.store.Fail(ctx, strings.TrimSpace(payoutNo), strings.TrimSpace(reviewer), strings.TrimSpace(reason), strings.TrimSpace(requestID))
}
func (s *ManualPayoutService) Cancel(ctx context.Context, payoutNo, actor, reason, requestID string) (domain.ManualPayoutCase, error) {
	return s.store.Cancel(ctx, strings.TrimSpace(payoutNo), strings.TrimSpace(actor), strings.TrimSpace(reason), strings.TrimSpace(requestID))
}
func (s *ManualPayoutService) OpenReceipt(ctx context.Context, payoutNo string, receiptID int64) (domain.PayoutReceipt, *os.File, error) {
	receipt, err := s.store.FindReceipt(ctx, strings.TrimSpace(payoutNo), receiptID)
	if err != nil {
		return domain.PayoutReceipt{}, nil, err
	}
	file, err := s.storage.Open(receipt.StorageKey)
	if err != nil {
		return domain.PayoutReceipt{}, nil, err
	}
	return receipt, file, nil
}

func (s *ManualPayoutService) OpenLatestReceipt(ctx context.Context, payoutNo string) (domain.PayoutReceipt, *os.File, error) {
	receipt, err := s.store.FindLatestReceipt(ctx, strings.TrimSpace(payoutNo))
	if err != nil {
		return domain.PayoutReceipt{}, nil, err
	}
	file, err := s.storage.Open(receipt.StorageKey)
	if err != nil {
		return domain.PayoutReceipt{}, nil, err
	}
	return receipt, file, nil
}
func (s *ManualPayoutService) ListCallbackAttempts(ctx context.Context, payoutNo string) ([]domain.ManualCallbackAttempt, error) {
	return s.store.ListCallbackAttempts(ctx, strings.TrimSpace(payoutNo))
}
func (s *ManualPayoutService) RetryCallback(ctx context.Context, payoutNo string) error {
	return s.store.RetryCallback(ctx, strings.TrimSpace(payoutNo))
}
