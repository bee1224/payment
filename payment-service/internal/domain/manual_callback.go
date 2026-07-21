package domain

import "time"

type ManualCallbackJob struct {
	ID             int64
	ManualCaseID   int64
	MerchantCode   string
	CallbackURL    string
	Payload        string
	AttemptCount   int
	IdempotencyKey string
}

type ManualCallbackAttempt struct {
	RequestBody    string
	ResponseStatus int
	ResponseBody   string
	ErrorMessage   string
	CreatedAt      time.Time
}
