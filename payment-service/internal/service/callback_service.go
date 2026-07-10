package service

import "payment-service/internal/domain"

type DepositCallbackService struct{}

func NewDepositCallbackService() *DepositCallbackService {
	return &DepositCallbackService{}
}

func (s *DepositCallbackService) RecordDepositCallback(callback domain.DepositCallback) error {
	return nil
}
