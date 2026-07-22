package domain

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const CallbackResponseSummaryMaxRunes = 512

type CallbackDeliveryRequest struct {
	URL     string
	Body    []byte
	Headers map[string][]string
}

type DeliveryResult struct {
	HTTPStatus      int
	ResponseSummary string
	Elapsed         time.Duration
	Error           error
	ErrorCode       string
	Retryable       bool
}

type CallbackDeliveryEngine interface {
	Deliver(context.Context, CallbackDeliveryRequest) DeliveryResult
}

// IsSuccessfulMerchantCallbackResponse is the frozen external callback contract.
func IsSuccessfulMerchantCallbackResponse(status int, body []byte) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices && bytes.Equal(body, []byte("OK"))
}

type MerchantDepositCallbackAttempt struct {
	ID              int64
	TaskID          int64
	AttemptNo       int
	Stage           string
	Status          string
	StartedAt       time.Time
	FinishedAt      *time.Time
	HTTPStatus      int
	ResponseSummary string
	ErrorCode       string
	ElapsedMS       int64
	NextRetryAt     *time.Time
}

const (
	CallbackTaskPending      = "pending"
	CallbackTaskProcessing   = "processing"
	CallbackTaskSent         = "sent"
	CallbackTaskDeadLetter   = "dead_letter"
	CallbackAttemptRunning   = "running"
	CallbackAttemptSuccess   = "success"
	CallbackAttemptFailed    = "failed"
	CallbackAttemptAbandoned = "abandoned"
)

func SanitizeCallbackResponseSummary(body []byte) string {
	text := strings.TrimSpace(string(body))
	var b strings.Builder
	space := false
	runes := 0
	for _, r := range text {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
		runes++
		if runes >= CallbackResponseSummaryMaxRunes {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
