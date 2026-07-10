package domain

import "time"

type DepositOrderStatus string

const (
	DepositOrderStatusPending DepositOrderStatus = "pending"
	DepositOrderStatusPaid    DepositOrderStatus = "paid"
	DepositOrderStatusFailed  DepositOrderStatus = "failed"
)

type DepositOrder struct {
	ID              int64
	MerchantID      int64
	MerchantCode    string
	CallbackURL     string
	ChannelCode     string
	BankAccounts    []string
	StoreNumbers    []string
	OrderNo         string
	MerchantOrderNo string
	Provider        string
	ProviderTradeNo string
	AmountCents     int64
	Currency        string
	ItemDesc        string
	UserName        string
	BankID          string
	PayCurrency     string
	Mobile          string
	IDNo            string
	Status          DepositOrderStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
