package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"payment-service/internal/domain"
)

type PayoutAlertNotifier interface {
	Notify(context.Context, domain.PayoutOperationalAlert) error
}

// WebhookPayoutAlertNotifier emits Slack-compatible JSON without making alert
// persistence depend on an external system.
type WebhookPayoutAlertNotifier struct {
	url    string
	client *http.Client
}

func NewWebhookPayoutAlertNotifier(url string, timeout time.Duration) *WebhookPayoutAlertNotifier {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &WebhookPayoutAlertNotifier{url: strings.TrimSpace(url), client: &http.Client{Timeout: timeout}}
}
func (n *WebhookPayoutAlertNotifier) Notify(ctx context.Context, a domain.PayoutOperationalAlert) error {
	if n == nil || n.url == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"text": "payment-service payout alert", "payout_no": a.PayoutNo, "category": a.Category, "severity": a.Severity, "summary": a.Summary, "details": a.Details, "occurrence_count": a.OccurrenceCount})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &webhookStatusError{status: resp.StatusCode}
	}
	return nil
}

type webhookStatusError struct{ status int }

func (e *webhookStatusError) Error() string {
	return "alert webhook returned HTTP " + http.StatusText(e.status)
}
