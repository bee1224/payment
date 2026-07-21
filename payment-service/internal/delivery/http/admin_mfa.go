package http

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

func newTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func validTOTP(secret, code string, now time.Time) bool {
	secret = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(key) < 16 || len(strings.TrimSpace(code)) != 6 {
		return false
	}
	for offset := int64(-1); offset <= 1; offset++ {
		expected := []byte(totpCode(key, now.Unix()/30+offset))
		if hmac.Equal([]byte(strings.TrimSpace(code)), expected) {
			return true
		}
	}
	return false
}

func totpCode(key []byte, counter int64) string {
	var rawCounter [8]byte
	binary.BigEndian.PutUint64(rawCounter[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(rawCounter[:])
	sum := mac.Sum(nil)
	pos := sum[len(sum)-1] & 0x0f
	value := (int(sum[pos]&0x7f)<<24 | int(sum[pos+1])<<16 | int(sum[pos+2])<<8 | int(sum[pos+3])) % 1000000
	return string([]byte{byte('0' + value/100000), byte('0' + value/10000%10), byte('0' + value/1000%10), byte('0' + value/100%10), byte('0' + value/10%10), byte('0' + value%10)})
}

var errMFAUnavailable = errors.New("MFA enrollment requires the persistent admin user store")
