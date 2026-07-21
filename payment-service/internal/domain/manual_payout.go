package domain

import "time"

type ManualPayoutStatus string

const (
	ManualPayoutPending       ManualPayoutStatus = "PENDING"
	ManualPayoutProcessing    ManualPayoutStatus = "PROCESSING"
	ManualPayoutPendingReview ManualPayoutStatus = "PAID_PENDING_REVIEW"
	ManualPayoutSuccess       ManualPayoutStatus = "SUCCESS"
	ManualPayoutFailed        ManualPayoutStatus = "FAILED"
	ManualPayoutCancelled     ManualPayoutStatus = "CANCELLED"
)

type ManualPayoutCase struct {
	ID            int64
	PayoutOrderID int64
	PayoutNo      string
	Status        ManualPayoutStatus
	OperatorID    string
	ConfirmedBy   string
	ConfirmedAt   *time.Time
	FailureReason string
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PayoutReceipt struct {
	ID                 int64
	ManualPayoutCaseID int64
	StorageKey         string
	OriginalFilename   string
	ContentType        string
	SizeBytes          int64
	SHA256             string
	UploadedBy         string
	CreatedAt          time.Time
}
