package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/domain"
)

var ErrReplayRequest = errors.New("request has already been used")

type MerchantRequestAuth struct {
	MerchantID string
	APIKey     string
	Timestamp  string
	Nonce      string
	Signature  string
	Method     string
	Path       string
	Body       []byte
}

func (s *PayoutService) AuthenticateMerchantRequest(ctx context.Context, auth MerchantRequestAuth) (domain.Merchant, error) {
	if strings.TrimSpace(auth.Signature) != "" || strings.TrimSpace(auth.Timestamp) != "" || strings.TrimSpace(auth.Nonce) != "" {
		return s.authenticateMerchantSignature(ctx, auth)
	}
	return s.authenticateMerchant(ctx, auth.MerchantID, auth.APIKey)
}

func (s *PayoutService) authenticateMerchantSignature(ctx context.Context, auth MerchantRequestAuth) (domain.Merchant, error) {
	merchantCode := strings.TrimSpace(auth.MerchantID)
	if merchantCode == "" {
		return domain.Merchant{}, errors.New("merchant_id is required")
	}
	if strings.TrimSpace(auth.Timestamp) == "" || strings.TrimSpace(auth.Nonce) == "" || strings.TrimSpace(auth.Signature) == "" {
		return domain.Merchant{}, errors.New("timestamp, nonce and signature are required")
	}
	merchant, err := s.store.FindMerchantByCode(ctx, merchantCode)
	if err != nil {
		return domain.Merchant{}, err
	}
	if strings.ToLower(merchant.Status) == "disabled" {
		return domain.Merchant{}, ErrMerchantAuthFailed
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(auth.Timestamp), 10, 64)
	if err != nil {
		return domain.Merchant{}, errors.New("timestamp must be a valid unix timestamp")
	}
	now := s.now()
	requestTime := time.Unix(ts, 0)
	if requestTime.Before(now.Add(-s.authMaxSkew)) || requestTime.After(now.Add(s.authMaxSkew)) {
		return domain.Merchant{}, errors.New("timestamp is outside the allowed skew")
	}
	nonce := strings.TrimSpace(auth.Nonce)
	if nonce == "" {
		return domain.Merchant{}, errors.New("nonce is required")
	}
	secret := s.resolveMerchantRequestSigningSecret(ctx, merchant)
	if strings.TrimSpace(secret) == "" {
		return domain.Merchant{}, ErrMerchantAuthFailed
	}
	expected := computeMerchantRequestSignature(secret, MerchantRequestSignaturePayload{
		MerchantID: merchant.Code,
		Timestamp:  strings.TrimSpace(auth.Timestamp),
		Nonce:      nonce,
		Method:     strings.ToUpper(strings.TrimSpace(auth.Method)),
		Path:       strings.TrimSpace(auth.Path),
		BodyHash:   hashRequestBody(auth.Body),
	})
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(expected)), []byte(strings.ToLower(strings.TrimSpace(auth.Signature)))) != 1 {
		return domain.Merchant{}, ErrMerchantAuthFailed
	}
	if s.nonceStore == nil {
		return merchant, nil
	}
	allowed, err := s.nonceStore.Use(ctx, "merchant_request:"+merchant.Code, nonce, requestTime.Add(s.authMaxSkew), now)
	if err != nil {
		return domain.Merchant{}, err
	}
	if !allowed {
		return domain.Merchant{}, ErrReplayRequest
	}
	return merchant, nil
}

type MerchantRequestSignaturePayload struct {
	MerchantID string
	Timestamp  string
	Nonce      string
	Method     string
	Path       string
	BodyHash   string
}

func computeMerchantRequestSignature(secret string, payload MerchantRequestSignaturePayload) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(secret)))
	mac.Write([]byte(strings.Join([]string{
		strings.TrimSpace(payload.MerchantID),
		strings.TrimSpace(payload.Timestamp),
		strings.TrimSpace(payload.Nonce),
		strings.ToUpper(strings.TrimSpace(payload.Method)),
		strings.TrimSpace(payload.Path),
		strings.TrimSpace(payload.BodyHash),
	}, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashRequestBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
