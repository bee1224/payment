package callback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestCallbackHMACGoldenContract(t *testing.T) {
	for _, tc := range []struct{ body, nonce, want string }{
		{`{"event":"deposit_result","transaction_id":"TX-GOLDEN-001","status":"30000","note":"測試"}`, "nonce-deposit-golden", "0e27538b66c0b3d1f0984c6447c1859c846461ac7f0905f3ec4d98e07b518606"},
		{`{"event":"payout_result","payout_no":"PO-GOLDEN-001","status":"completed","memo":null,"remark":""}`, "nonce-payout-golden", "3c2d085dda16ba493eb8470aaa21d3c5988457e78628aa4c1999e8c5a77144d9"},
	} {
		hash := sha256.Sum256([]byte(tc.body))
		canonical := strings.Join([]string{"M-GOLDEN", "cb-v1", "1700000000", tc.nonce, "POST", "/callbacks/payment", hex.EncodeToString(hash[:])}, "\n")
		mac := hmac.New(sha256.New, []byte("golden-callback-secret"))
		_, _ = mac.Write([]byte(canonical))
		if got := hex.EncodeToString(mac.Sum(nil)); got != tc.want {
			t.Fatalf("golden signature=%s", got)
		}
	}
}
