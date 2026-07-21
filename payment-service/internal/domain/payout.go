package domain

import "time"

type PayoutOrderStatus string

const (
	PayoutOrderStatusPendingReview PayoutOrderStatus = "pending_review"
	PayoutOrderStatusApproved      PayoutOrderStatus = "approved"
	PayoutOrderStatusSubmitting    PayoutOrderStatus = "submitting"
	PayoutOrderStatusProcessing    PayoutOrderStatus = "processing"
	PayoutOrderStatusCompleted     PayoutOrderStatus = "completed"
	PayoutOrderStatusFailed        PayoutOrderStatus = "failed"
	PayoutOrderStatusReversed      PayoutOrderStatus = "reversed"
	PayoutOrderStatusRejected      PayoutOrderStatus = "rejected"
	PayoutOrderStatusCancelled     PayoutOrderStatus = "cancelled"
)

func (s PayoutOrderStatus) IsTerminal() bool {
	switch s {
	case PayoutOrderStatusCompleted, PayoutOrderStatusFailed, PayoutOrderStatusReversed, PayoutOrderStatusRejected, PayoutOrderStatusCancelled:
		return true
	default:
		return false
	}
}

type PayoutOrder struct {
	ID               int64
	MerchantID       int64
	MerchantCode     string
	PayoutNo         string
	MerchantPayoutNo string
	Provider         string
	ProviderOrderNo  string
	ProviderTradeNo  string
	AmountCents      int64
	FeeCents         int64
	TotalDebitCents  int64
	Currency         string
	Status           PayoutOrderStatus
	FailureCode      string
	FailureMessage   string
	CallbackURL      string
	PayAccountName   string
	PayCardNo        string
	PayBankName      string
	PaySubBranch     string
	PaySubBranchCode string
	PayCity          string
	PayValidateID    string
	PayCurrency      string
	ApprovedAt       *time.Time
	SubmittedAt      *time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PayoutBeneficiary struct {
	ID               int64
	PayoutOrderID    int64
	PayAccountName   string
	PayCardNo        string
	PayBankName      string
	PaySubBranch     string
	PaySubBranchCode string
	PayCity          string
	PayValidateID    string
	PayCurrency      string
	CreatedAt        time.Time
}

type PayoutTransaction struct {
	ID              int64
	PayoutOrderID   int64
	ProviderID      int64
	AttemptNo       int
	ProviderOrderNo string
	ProviderTradeNo string
	Status          string
	ErrorMessage    string
	RequestPayload  string
	ResponsePayload string
	SubmittedAt     *time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PayoutCallback struct {
	ID                  int64
	ProviderID          int64
	PayoutOrderID       int64
	PayoutTransactionID int64
	ProviderOrderNo     string
	ProviderTradeNo     string
	ProviderEventKey    string
	Payload             string
	Status              string
	ErrorMessage        string
	ReceivedAt          time.Time
	ProcessedAt         *time.Time
}

type MerchantPayoutCallbackTask struct {
	ID            int64
	MerchantID    int64
	PayoutOrderID int64
	CallbackURL   string
	Payload       string
	Status        string
	RetryCount    int
	NextRetryAt   time.Time
	LastError     string
	ClaimToken    string
	SentAt        *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
