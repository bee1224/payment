package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"payment-service/internal/repository"
)

const MerchantCallbackSignatureVersion = "hmac-sha256-v1"

func BuildMerchantCallbackHeaders(key repository.CallbackSigningKey, method, rawURL string, body []byte, now time.Time) (map[string][]string, error) {
	if strings.TrimSpace(key.MerchantCode) == "" || strings.TrimSpace(key.KeyID) == "" || strings.TrimSpace(key.Secret) == "" {
		return nil, fmt.Errorf("merchant callback signing key is incomplete")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.EscapedPath() == "" {
		return nil, fmt.Errorf("invalid callback URL")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("callback nonce generation failed")
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := hex.EncodeToString(nonceBytes)
	signature := ComputeMerchantCallbackSignature(key.MerchantCode, key.KeyID, timestamp, nonce, method, u.EscapedPath(), body, key.Secret)
	return map[string][]string{
		"Content-Type":                 {"application/json"},
		"X-Callback-Merchant-Id":       {key.MerchantCode},
		"X-Callback-Key-Id":            {key.KeyID},
		"X-Callback-Timestamp":         {timestamp},
		"X-Callback-Nonce":             {nonce},
		"X-Callback-Signature-Version": {MerchantCallbackSignatureVersion},
		"X-Callback-Signature":         {signature},
	}, nil
}

func ComputeMerchantCallbackSignature(merchantID, keyID, timestamp, nonce, method, path string, body []byte, secret string) string {
	bodyHash := sha256.Sum256(body)
	canonical := strings.Join([]string{merchantID, keyID, timestamp, nonce, strings.ToUpper(method), path, hex.EncodeToString(bodyHash[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
