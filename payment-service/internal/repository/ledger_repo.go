package repository

import "payment-service/internal/domain"

type LedgerRepository interface {
	Create(entry domain.LedgerEntry) error
}
