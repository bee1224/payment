package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

type HMACRequestAuth struct {
	CustomerID string
	Timestamp  string
	Nonce      string
	Signature  string
	Method     string
	Path       string
	Body       []byte
}

func BuildHMACSignature(auth HMACRequestAuth, secret string) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", errors.New("gateway hmac secret is required")
	}
	if strings.TrimSpace(auth.CustomerID) == "" {
		return "", errors.New("customer_id is required")
	}
	if strings.TrimSpace(auth.Timestamp) == "" || strings.TrimSpace(auth.Nonce) == "" {
		return "", errors.New("timestamp and nonce are required")
	}
	bodyHash := sha256.Sum256(auth.Body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{
		strings.TrimSpace(auth.CustomerID),
		strings.TrimSpace(auth.Timestamp),
		strings.TrimSpace(auth.Nonce),
		strings.ToUpper(strings.TrimSpace(auth.Method)),
		strings.TrimSpace(auth.Path),
		hex.EncodeToString(bodyHash[:]),
	}, "\n")))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func VerifyHMACRequest(auth HMACRequestAuth, secret string, now time.Time, maxSkew time.Duration) error {
	if strings.TrimSpace(auth.Timestamp) == "" || strings.TrimSpace(auth.Nonce) == "" || strings.TrimSpace(auth.Signature) == "" {
		return errors.New("timestamp, nonce and signature are required")
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(auth.Timestamp), 10, 64)
	if err != nil {
		return errors.New("timestamp must be a valid unix timestamp")
	}
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	requestTime := time.Unix(ts, 0)
	if requestTime.Before(now.Add(-maxSkew)) || requestTime.After(now.Add(maxSkew)) {
		return errors.New("timestamp exceeded allowed time skew")
	}
	expected, err := BuildHMACSignature(auth, secret)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(expected), []byte(strings.TrimSpace(auth.Signature))) {
		return errors.New("signature verification failed")
	}
	return nil
}
