package domain

import "time"

type DepositCallback struct {
	ID        int64
	OrderID   int64
	Payload   []byte
	CreatedAt time.Time
}

type DepositNotifyTrace struct {
	Headers         map[string][]string
	SourceIP        string
	ProviderOrderNo string
	ProviderTradeNo string
}

type MerchantDepositCallbackTask struct {
	ID          int64
	MerchantID  int64
	OrderID     int64
	CallbackURL string
	Payload     string
	Status      string
	RetryCount  int
	NextRetryAt time.Time
	LastError   string
	SentAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
