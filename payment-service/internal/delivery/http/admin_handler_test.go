package http

import (
	"encoding/base32"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminPasswordHash(t *testing.T) {
	hash, err := NewAdminPasswordHash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyAdminPassword("correct horse battery staple", hash) {
		t.Fatal("valid password was rejected")
	}
	if !verifyAdminPassword("CORRECT HORSE BATTERY STAPLE", hash) {
		t.Fatal("uppercase password was rejected")
	}
	if verifyAdminPassword("wrong", hash) {
		t.Fatal("invalid password was accepted")
	}
	if verifyAdminPassword("correct horse battery staple", "sha256:deadbeef") {
		t.Fatal("legacy SHA256 hash was accepted")
	}
}

func TestAdminRolePermissionsAreLeastPrivilege(t *testing.T) {
	if !adminPermissionAllowed("OPERATOR", "manual_payout.start") {
		t.Fatal("operator should be allowed to start manual payout")
	}
	if adminPermissionAllowed("OPERATOR", "manual_payout.confirm") {
		t.Fatal("operator must not confirm a payout")
	}
	if !adminPermissionAllowed("REVIEWER", "manual_payout.confirm") {
		t.Fatal("reviewer should confirm a payout")
	}
	if adminPermissionAllowed("AUDITOR", "manual_payout.cancel") {
		t.Fatal("auditor must be read-only")
	}
}

func TestTOTPValidation(t *testing.T) {
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !validTOTP(secret, totpCode(decoded, time.Unix(59, 0).Unix()/30), time.Unix(59, 0)) {
		t.Fatal("known RFC-compatible TOTP code was rejected")
	}
	if validTOTP(secret, "282761", time.Unix(0, 0)) {
		t.Fatal("invalid TOTP code was accepted")
	}
	generated, err := newTOTPSecret()
	if err != nil || len(generated) < 16 || strings.Contains(generated, "=") {
		t.Fatalf("invalid generated TOTP secret: %q, %v", generated, err)
	}
}

func TestAdminLoginFailureRateLimitAndReset(t *testing.T) {
	h := &AdminHandler{loginFailures: make(map[string]adminLoginFailure)}
	now := time.Now()
	request := httptest.NewRequest("POST", "/api/admin/auth/login", nil)
	request.RemoteAddr = "198.51.100.10:4321"
	key := adminLoginKey(request, "ADMIN")
	for range adminLoginFailureLimit {
		h.recordLoginFailure(key, now)
	}
	if !h.loginRateLimited(key, now.Add(time.Minute)) {
		t.Fatal("expected failed logins to be rate limited")
	}
	h.clearLoginFailures(key)
	if h.loginRateLimited(key, now.Add(time.Minute)) {
		t.Fatal("expected a successful login reset to clear the rate limit")
	}
	for range adminLoginFailureLimit {
		h.recordLoginFailure(key, now)
	}
	if h.loginRateLimited(key, now.Add(adminLoginFailureWindow)) {
		t.Fatal("expected the rate limit window to expire")
	}
}
