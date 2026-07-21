package domain

import (
	"fmt"
	"strings"
	"time"
)

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
	ID             int64
	MerchantID     int64
	OrderID        int64
	EventKey       string
	CallbackURL    string
	Payload        string
	Status         string
	RetryCount     int
	NextRetryAt    time.Time
	LastError      string
	ClaimToken     string
	ClaimedAt      *time.Time
	ClaimExpiresAt *time.Time
	AttemptCount   int
	SentAt         *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func MerchantDepositCallbackEventKey(transactionID string, status string) (string, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return "", fmt.Errorf("transaction_id is required for callback event key")
	}
	event := map[DepositOrderStatus]string{
		DepositOrderStatusPaid:    "deposit.paid",
		DepositOrderStatusFailed:  "deposit.failed",
		DepositOrderStatusExpired: "deposit.expired",
	}[DepositOrderStatus(status)]
	if event == "" {
		return "", fmt.Errorf("unsupported callback event status %q", status)
	}
	return "merchant.deposit:" + transactionID + ":" + event, nil
}
