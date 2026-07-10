package repository

import "payment-service/internal/domain"

type DepositOrderRepository interface {
	CreateDepositOrder(order domain.DepositOrder) (domain.DepositOrder, error)
	FindDepositOrderByOrderNo(orderNo string) (domain.DepositOrder, error)
}
