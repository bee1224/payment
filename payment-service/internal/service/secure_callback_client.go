package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"payment-service/internal/domain"
)

type callbackIPResolver func(context.Context, string) ([]net.IPAddr, error)

type PublicHTTPSCallbackDeliveryEngine struct{ Timeout time.Duration }

func (e PublicHTTPSCallbackDeliveryEngine) Deliver(ctx context.Context, request domain.CallbackDeliveryRequest) domain.DeliveryResult {
	started := time.Now()
	headers := make(http.Header, len(request.Headers))
	for name, values := range request.Headers {
		headers[name] = append([]string(nil), values...)
	}
	resp, err := PostPublicHTTPSCallback(ctx, request.URL, request.Body, e.Timeout, headers)
	result := domain.DeliveryResult{Elapsed: time.Since(started)}
	if err != nil {
		result.Error = err
		result.ErrorCode, result.Retryable = callbackDeliveryErrorCode(err)
		return result
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
	result.HTTPStatus = resp.StatusCode
	result.ResponseSummary = domain.SanitizeCallbackResponseSummary(body)
	if readErr != nil {
		result.Error, result.ErrorCode, result.Retryable = readErr, "response_read_error", true
		return result
	}
	if len(body) > 4096 {
		result.Error = errors.New("callback response body too large")
		result.ErrorCode = "response_body_too_large"
		result.Retryable = true
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.ErrorCode = "http_4xx"
		result.Retryable = false
		if resp.StatusCode >= 500 {
			result.ErrorCode, result.Retryable = "http_5xx", true
		}
		result.Error = errors.New("callback returned non-2xx status")
	} else if !isSuccessfulMerchantCallbackResponse(resp.StatusCode, body) {
		result.ErrorCode, result.Retryable = "response_not_ok", true
		result.Error = errors.New("callback response was not OK")
	}
	return result
}

func isSuccessfulMerchantCallbackResponse(status int, body []byte) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices && string(body) == "OK"
}

func callbackDeliveryErrorCode(err error) (string, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "network_timeout", true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network_timeout", true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if _, ok := urlErr.Err.(tls.RecordHeaderError); ok {
			return "tls_error", false
		}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "callback url") || strings.Contains(message, "blocked ip") || strings.Contains(message, "could not be resolved") {
		return "invalid_callback_url", false
	}
	if strings.Contains(message, "tls") || strings.Contains(message, "x509") {
		return "tls_error", false
	}
	return "connection_error", true
}

// PostPublicHTTPSCallback resolves a callback host once and pins the outbound
// connection to the selected public IP. It rejects non-HTTPS targets and never
// follows redirects, so a callback cannot be redirected to an internal host.
func PostPublicHTTPSCallback(ctx context.Context, rawURL string, body []byte, timeout time.Duration, headers http.Header) (*http.Response, error) {
	parsed, target, err := resolvePublicHTTPSCallbackTarget(ctx, rawURL, net.DefaultResolver.LookupIPAddr)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	client := newPinnedPublicHTTPSCallbackClient(parsed.Hostname(), target, port, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	return client.Do(req)
}

func newPinnedPublicHTTPSCallbackClient(host string, target net.IP, port string, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(target.String(), port))
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func resolvePublicHTTPSCallbackTarget(ctx context.Context, rawURL string, resolve callbackIPResolver) (*url.URL, net.IP, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, nil, errors.New("callback URL must be an absolute HTTPS URL")
	}
	ips, err := resolve(ctx, parsed.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, nil, errors.New("callback host could not be resolved")
	}
	for _, candidate := range ips {
		if publicCallbackIP(candidate.IP) {
			return parsed, candidate.IP, nil
		}
	}
	return nil, nil, errors.New("callback host resolves to a blocked IP")
}
