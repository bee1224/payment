package service

import "payment-service/internal/domain"

type DepositChannelService struct{}

func NewDepositChannelService() *DepositChannelService {
	return &DepositChannelService{}
}

func (s *DepositChannelService) EnabledDepositChannels() ([]domain.DepositChannel, error) {
	return nil, nil
}
