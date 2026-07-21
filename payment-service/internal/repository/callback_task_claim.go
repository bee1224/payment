package repository

import (
	"crypto/rand"
	"encoding/hex"
)

func newCallbackClaimToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "callback-claim-unavailable"
	}
	return hex.EncodeToString(buf)
}
