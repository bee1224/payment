package gateway

import "testing"

func TestBuildHMACSignatureCallbackFixture(t *testing.T) {
	body := []byte(`{"customer_id":"M10001","order_id":"ORDER-001","status":"30000"}`)
	got, err := BuildHMACSignature(HMACRequestAuth{
		CustomerID: "M10001",
		Timestamp:  "1784030400",
		Nonce:      "fixed-callback-nonce",
		Method:     "POST",
		Path:       "/merchant/deposit/callback",
		Body:       body,
	}, "callback-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	const want = "9d44129330dce67064d24f78f15e560cf485aa66523b43fa1caf5f07c77e1940"
	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}
