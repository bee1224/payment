package config

import (
	"strings"
	"testing"
)

func validTestConfig() *Config {
	return &Config{
		App:      AppConfig{Port: 8080, MerchantSecretEncryptionKey: "test-merchant-secret-key", TrustedProxyCIDRs: []string{"10.0.0.0/8"}},
		Database: DatabaseConfig{DSN: "user:pass@tcp(localhost:3306)/payment_service"},
		NewebpayDeposit: NewebpayDepositConfig{
			MPGURL:     "https://core.newebpay.com/MPG/mpg_gateway",
			MerchantID: "MS123456789",
			HashKey:    "hash-key",
			HashIV:     "hash-iv",
			NotifyURL:  "https://payment.example.com/notify/newebpay",
			ReturnURL:  "https://payment.example.com/deposits/return",
		},
		Gateway: GatewayConfig{
			HMACSecret:               "sign-key",
			PreviousHMACSecret:       "previous-sign-key",
			CustomerID:               "RIG001",
			BaseURL:                  "https://upstream.example.com",
			PayoutNotifyURL:          "https://payment.example.com/api/payments/callback",
			DepositCallbackAllowlist: []string{"35.220.239.87"},
			PayoutCallbackAllowlist:  []string{"35.220.239.87"},
			HTTPTimeoutSeconds:       15,
		},
	}
}

func TestValidateWarnsOnSelfReferentialGatewayBaseURL(t *testing.T) {
	cfg := validTestConfig()
	cfg.Gateway.BaseURL = "https://payment.example.com"

	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected warning only, got error %v", err)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, " "), "matches GATEWAY_PAYOUT_NOTIFY_URL host") {
		t.Fatalf("expected self-referential gateway base URL warning, got %v", warnings)
	}
}

func TestValidateRejectsLocalNotifyURL(t *testing.T) {
	cfg := validTestConfig()
	cfg.NewebpayDeposit.NotifyURL = "http://127.0.0.1/notify/newebpay"

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "NEWEBPAY_NOTIFY_URL must not target a private or loopback address") {
		t.Fatalf("expected localhost notify URL error, got %v", err)
	}
}

func TestValidateRejectsNonHTTPSAlertWebhook(t *testing.T) {
	cfg := validTestConfig()
	cfg.App.AlertWebhookURL = "http://alerts.example.com/webhook"

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ALERT_WEBHOOK_URL must use HTTPS") {
		t.Fatalf("expected non-HTTPS webhook error, got %v", err)
	}
}

func TestValidateRejectsInvalidPayoutCallbackAllowlistEntry(t *testing.T) {
	cfg := validTestConfig()
	cfg.Gateway.PayoutCallbackAllowlist = []string{"not-an-ip"}

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "GATEWAY_PAYOUT_CALLBACK_ALLOWLIST entry") {
		t.Fatalf("expected invalid allowlist entry error, got %v", err)
	}
}

func TestValidateRejectsInvalidDepositCallbackAllowlistEntry(t *testing.T) {
	cfg := validTestConfig()
	cfg.Gateway.DepositCallbackAllowlist = []string{"not-an-ip"}

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "GATEWAY_DEPOSIT_CALLBACK_ALLOWLIST entry") {
		t.Fatalf("expected invalid deposit allowlist entry error, got %v", err)
	}
}

func TestValidateRequiresDepositCallbackAllowlist(t *testing.T) {
	cfg := validTestConfig()
	cfg.Gateway.DepositCallbackAllowlist = nil

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "GATEWAY_DEPOSIT_CALLBACK_ALLOWLIST must contain at least one IP or CIDR") {
		t.Fatalf("expected missing deposit callback allowlist error, got %v", err)
	}
}

func TestValidateRequiresPayoutCallbackAllowlist(t *testing.T) {
	cfg := validTestConfig()
	cfg.Gateway.PayoutCallbackAllowlist = []string{"  "}

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "GATEWAY_PAYOUT_CALLBACK_ALLOWLIST must contain at least one IP or CIDR") {
		t.Fatalf("expected missing payout callback allowlist error, got %v", err)
	}
}

func TestValidateRejectsInvalidTrustedProxyEntry(t *testing.T) {
	cfg := validTestConfig()
	cfg.App.TrustedProxyCIDRs = []string{"not-a-cidr"}

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "APP_TRUSTED_PROXY_CIDRS entry") {
		t.Fatalf("expected invalid trusted proxy entry error, got %v", err)
	}
}

func TestValidateRejectsProductionTestCallback(t *testing.T) {
	cfg := validTestConfig()
	cfg.App.Env = "production"
	cfg.App.PublicBaseURL = "https://api.nnviopp.com"
	cfg.App.TestDepositCallbacksEnabled = true
	cfg.NewebpayDeposit.Environment = "production"
	cfg.NewebpayDeposit.NotifyURL = "https://api.nnviopp.com/notify/newebpay"
	cfg.NewebpayDeposit.ReturnURL = "https://api.nnviopp.com/deposits/return"
	cfg.Gateway.PayoutNotifyURL = "https://api.nnviopp.com/api/payments/callback"

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "TEST_DEPOSIT_CALLBACKS_ENABLED must be false in production") {
		t.Fatalf("expected production test callback rejection, got %v", err)
	}
}

func TestValidateRejectsProductionHMACDiagnostics(t *testing.T) {
	cfg := validTestConfig()
	cfg.App.Env = "production"
	cfg.App.PublicBaseURL = "https://api.nnviopp.com"
	cfg.App.HMACDiagnosticsEnabled = true
	cfg.NewebpayDeposit.Environment = "production"
	cfg.NewebpayDeposit.NotifyURL = "https://api.nnviopp.com/notify/newebpay"
	cfg.NewebpayDeposit.ReturnURL = "https://api.nnviopp.com/deposits/return"
	cfg.Gateway.PayoutNotifyURL = "https://api.nnviopp.com/api/payments/callback"

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "HMAC_DIAGNOSTICS_ENABLED must be false in production") {
		t.Fatalf("expected production HMAC diagnostic rejection, got %v", err)
	}
}

func TestValidateRejectsSandboxProductionNewebPay(t *testing.T) {
	cfg := validTestConfig()
	cfg.App.Env = "sandbox"
	cfg.App.PublicBaseURL = "https://sandbox-api.nnviopp.com"
	cfg.App.TestDepositCallbacksEnabled = true
	cfg.Database.DSN = "user:pass@tcp(localhost:3306)/payment_sandbox"
	cfg.NewebpayDeposit.Environment = "sandbox"
	cfg.NewebpayDeposit.NotifyURL = "https://sandbox-api.nnviopp.com/notify/newebpay"
	cfg.NewebpayDeposit.ReturnURL = "https://sandbox-api.nnviopp.com/deposits/return"
	cfg.Gateway.PayoutNotifyURL = "https://sandbox-api.nnviopp.com/api/payments/callback"

	_, err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "sandbox NEWEBPAY_MPG_URL must target ccore.newebpay.com") {
		t.Fatalf("expected sandbox production NewebPay rejection, got %v", err)
	}
}

func TestApplyEnvParsesPayoutReviewActorAllowlist(t *testing.T) {
	t.Setenv("PAYOUT_REVIEW_ACTOR_ALLOWLIST", "ops.alice, ops.bob")

	cfg := &Config{}
	applyEnv(cfg)
	if len(cfg.App.PayoutReviewActorAllowlist) != 2 {
		t.Fatalf("expected 2 payout review actors, got %v", cfg.App.PayoutReviewActorAllowlist)
	}
	if cfg.App.PayoutReviewActorAllowlist[0] != "ops.alice" || cfg.App.PayoutReviewActorAllowlist[1] != "ops.bob" {
		t.Fatalf("unexpected payout review actors: %v", cfg.App.PayoutReviewActorAllowlist)
	}
}

func TestApplyEnvParsesPayoutReviewActorRoles(t *testing.T) {
	t.Setenv("PAYOUT_REVIEW_ACTOR_ROLES", "ops.alice=reviewer|auditor,ops.bob=admin|security")

	cfg := &Config{}
	applyEnv(cfg)
	if len(cfg.App.PayoutReviewActorRoles) != 2 {
		t.Fatalf("expected 2 actor role entries, got %v", cfg.App.PayoutReviewActorRoles)
	}
	if len(cfg.App.PayoutReviewActorRoles["ops.alice"]) != 2 || cfg.App.PayoutReviewActorRoles["ops.alice"][0] != "reviewer" {
		t.Fatalf("unexpected roles for ops.alice: %v", cfg.App.PayoutReviewActorRoles["ops.alice"])
	}
	if len(cfg.App.PayoutReviewActorRoles["ops.bob"]) != 2 || cfg.App.PayoutReviewActorRoles["ops.bob"][1] != "security" {
		t.Fatalf("unexpected roles for ops.bob: %v", cfg.App.PayoutReviewActorRoles["ops.bob"])
	}
}
