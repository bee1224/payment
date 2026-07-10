package repository

import "payment-service/internal/domain"

type MerchantRepository interface {
	FindByAPIKey(apiKey string) (domain.Merchant, error)
}
