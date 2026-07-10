package repository

import "payment-service/internal/domain"

type DepositChannelRepository interface {
	ListEnabledDepositChannels() ([]domain.DepositChannel, error)
}
