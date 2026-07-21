package http

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	nethttp "net/http"
	"strings"
	"time"

	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
)

var errGatewayReplayRequest = errors.New("request has already been used")

func buildGatewayRequestAuth(r *nethttp.Request, customerID string, body []byte) providerGateway.HMACRequestAuth {
	return providerGateway.HMACRequestAuth{
		CustomerID: firstNonEmpty([]string{strings.TrimSpace(r.Header.Get("X-Customer-Id")), strings.TrimSpace(customerID)}),
		Timestamp:  strings.TrimSpace(r.Header.Get("X-Timestamp")),
		Nonce:      strings.TrimSpace(r.Header.Get("X-Nonce")),
		Signature:  strings.TrimSpace(r.Header.Get("X-Signature")),
		Method:     r.Method,
		Path:       r.URL.Path,
		Body:       append([]byte(nil), body...),
	}
}

func ensureConsistentGatewayCustomerID(fieldName, primary, secondary string) error {
	primary = strings.TrimSpace(primary)
	secondary = strings.TrimSpace(secondary)
	if primary != "" && secondary != "" && primary != secondary {
		return errors.New(fieldName + " does not match X-Customer-Id")
	}
	return nil
}

func authenticateGatewayRequest(r *nethttp.Request, auth providerGateway.HMACRequestAuth, cfg GatewaySecurityConfig, nonceStore repository.ReplayNonceStore, now time.Time) error {
	if expected := strings.TrimSpace(cfg.CustomerID); expected != "" && strings.TrimSpace(auth.CustomerID) != expected {
		return errors.New("customer_id does not match configured gateway customer")
	}
	maxSkew := time.Duration(cfg.MaxSkewSeconds) * time.Second
	if err := verifyGatewayRequestWithSecrets(auth, gatewayRequestSecrets(cfg), now, maxSkew); err != nil {
		if cfg.HMACDiagnosticsEnabled && err.Error() == "signature verification failed" {
			logGatewayHMACDiagnostic(auth, gatewayRequestSecrets(cfg))
		}
		return err
	}
	if nonceStore == nil {
		return nil
	}
	expiry := now.Add(maxSkew)
	if maxSkew <= 0 {
		expiry = now.Add(5 * time.Minute)
	}
	allowed, err := nonceStore.Use(r.Context(), "gateway_request:"+strings.TrimSpace(auth.CustomerID), strings.TrimSpace(auth.Nonce), expiry, now)
	if err != nil {
		return err
	}
	if !allowed {
		return errGatewayReplayRequest
	}
	return nil
}

// logGatewayHMACDiagnostic is deliberately limited to Sandbox-only use by the
// router. It never emits a secret, raw request body, or a full signature.
func logGatewayHMACDiagnostic(auth providerGateway.HMACRequestAuth, secrets []string) {
	bodyHash := sha256.Sum256(auth.Body)
	expected := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		signature, err := providerGateway.BuildHMACSignature(auth, secret)
		if err == nil {
			expected = append(expected, signatureFingerprint(signature))
		}
	}
	log.Printf("component=gateway_hmac_diagnostic outcome=signature_mismatch customer_id=%q timestamp=%q nonce=%q method=%q path=%q raw_body_sha256=%s received_signature_fp=%s expected_signature_fp=%q canonical_fields=%q", strings.TrimSpace(auth.CustomerID), strings.TrimSpace(auth.Timestamp), strings.TrimSpace(auth.Nonce), strings.ToUpper(strings.TrimSpace(auth.Method)), strings.TrimSpace(auth.Path), hex.EncodeToString(bodyHash[:]), signatureFingerprint(auth.Signature), expected, "customer_id,timestamp,nonce,uppercase_method,path,raw_body_sha256")
}

func signatureFingerprint(signature string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(signature)))
	return hex.EncodeToString(digest[:])[:12]
}

func gatewayRequestSecrets(cfg GatewaySecurityConfig) []string {
	secrets := make([]string, 0, 2)
	for _, secret := range []string{cfg.HMACSecret, cfg.PreviousHMACSecret} {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		duplicate := false
		for _, existing := range secrets {
			if existing == secret {
				duplicate = true
				break
			}
		}
		if !duplicate {
			secrets = append(secrets, secret)
		}
	}
	return secrets
}

func verifyGatewayRequestWithSecrets(auth providerGateway.HMACRequestAuth, secrets []string, now time.Time, maxSkew time.Duration) error {
	if len(secrets) == 0 {
		return errors.New("gateway hmac secret is required")
	}
	var signatureErr error
	for _, secret := range secrets {
		err := providerGateway.VerifyHMACRequest(auth, secret, now, maxSkew)
		if err == nil {
			return nil
		}
		if err.Error() == "signature verification failed" {
			signatureErr = err
			continue
		}
		return err
	}
	if signatureErr != nil {
		return signatureErr
	}
	return errors.New("signature verification failed")
}
