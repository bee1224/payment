package http

import (
	"bytes"
	"log"
	"strconv"
	"strings"
	"testing"
	"time"

	providerGateway "payment-service/internal/provider/gateway"
)

func TestSandboxHMACDiagnosticRedactsSensitiveValues(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	secret := "sandbox-secret-must-not-be-logged"
	rawBody := []byte(`{"email":"person@example.test","amount":"100"}`)
	auth := providerGateway.HMACRequestAuth{
		CustomerID: "SANDBOX-MERCHANT",
		Timestamp:  strconv.FormatInt(time.Now().Unix(), 10),
		Nonce:      "diagnostic-nonce-001",
		Signature:  "received-full-signature-must-not-be-logged",
		Method:     "POST",
		Path:       "/api/pay_order",
		Body:       rawBody,
	}
	err := authenticateGatewayRequest(nil, auth, GatewaySecurityConfig{
		CustomerID:             "SANDBOX-MERCHANT",
		HMACSecret:             secret,
		MaxSkewSeconds:         300,
		HMACDiagnosticsEnabled: true,
	}, nil, time.Now())
	if err == nil || err.Error() != "signature verification failed" {
		t.Fatalf("expected signature verification failure, got %v", err)
	}

	logged := output.String()
	for _, prohibited := range []string{secret, string(rawBody), "person@example.test", auth.Signature} {
		if strings.Contains(logged, prohibited) {
			t.Fatalf("diagnostic output leaked sensitive value %q: %s", prohibited, logged)
		}
	}
	for _, required := range []string{"component=gateway_hmac_diagnostic", "raw_body_sha256=", "received_signature_fp=", "expected_signature_fp=", "canonical_fields="} {
		if !strings.Contains(logged, required) {
			t.Fatalf("diagnostic output missing %q: %s", required, logged)
		}
	}
}
