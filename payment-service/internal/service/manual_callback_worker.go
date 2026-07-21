package service

import (
	"context"
	"errors"
	"net"
	"time"

	"payment-service/internal/domain"
	"payment-service/internal/repository"
)

type ManualCallbackWorker struct {
	store         *repository.ManualPayoutStore
	resolveKey    func(context.Context, string) (repository.CallbackSigningKey, error)
	clientTimeout time.Duration
	maxAttempts   int
	workerID      string
	engine        domain.CallbackDeliveryEngine
}

func NewManualCallbackWorker(store *repository.ManualPayoutStore, resolveKey func(context.Context, string) (repository.CallbackSigningKey, error), timeout time.Duration, maxAttempts int, workerID string) *ManualCallbackWorker {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	return &ManualCallbackWorker{store: store, resolveKey: resolveKey, clientTimeout: timeout, maxAttempts: maxAttempts, workerID: workerID, engine: PublicHTTPSCallbackDeliveryEngine{Timeout: timeout}}
}

func (w *ManualCallbackWorker) RunOnce(ctx context.Context, limit int) error {
	jobs, err := w.store.ClaimDueCallbackJobs(ctx, w.workerID, limit)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		w.deliver(ctx, job)
	}
	return nil
}
func (w *ManualCallbackWorker) deliver(ctx context.Context, job domain.ManualCallbackJob) {
	body := []byte(job.Payload)
	var err error
	attempt := domain.ManualCallbackAttempt{RequestBody: string(body)}
	if err == nil {
		if w.resolveKey == nil {
			err = errors.New("merchant callback signing key resolver is unavailable")
		} else {
			key, keyErr := w.resolveKey(ctx, job.MerchantCode)
			if keyErr != nil {
				err = keyErr
			} else {
				headers, headerErr := BuildMerchantCallbackHeaders(key, "POST", job.CallbackURL, body, time.Now().UTC())
				if headerErr != nil {
					err = headerErr
				} else {
					headers["Idempotency-Key"] = []string{job.IdempotencyKey}
					result := w.engine.Deliver(ctx, domain.CallbackDeliveryRequest{URL: job.CallbackURL, Body: body, Headers: headers})
					attempt.ResponseStatus = result.HTTPStatus
					attempt.ResponseBody = result.ResponseSummary
					if result.HTTPStatus < 200 || result.HTTPStatus > 299 || result.ResponseSummary != "OK" {
						err = result.Error
					}
				}
			}
		}
	}
	if err != nil {
		attempt.ErrorMessage = err.Error()
	}
	count := job.AttemptCount + 1
	_ = w.store.FinishCallbackJob(ctx, job.ID, err == nil, count >= w.maxAttempts, time.Now().UTC().Add(callbackRetryDelay(count)), attempt)
}

func publicCallbackIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
func callbackRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}
