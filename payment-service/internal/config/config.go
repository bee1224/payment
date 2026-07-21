package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App             AppConfig             `yaml:"app"`
	Database        DatabaseConfig        `yaml:"database"`
	NewebpayDeposit NewebpayDepositConfig `yaml:"newebpay"`
	Gateway         GatewayConfig         `yaml:"gateway"`
	Merchant        MerchantConfig        `yaml:"merchant"`
}

type AppConfig struct {
	Name                        string              `yaml:"name"`
	Env                         string              `yaml:"env"`
	Port                        int                 `yaml:"port"`
	PayoutReviewToken           string              `yaml:"payout_review_token"`
	AdminSessionSecret          string              `yaml:"admin_session_secret"`
	AdminInitialUsername        string              `yaml:"admin_initial_username"`
	AdminInitialPasswordHash    string              `yaml:"admin_initial_password_hash"`
	ReceiptStoragePath          string              `yaml:"receipt_storage_path"`
	ReceiptMaxSizeMB            int64               `yaml:"receipt_max_size_mb"`
	CallbackWorkerInterval      int                 `yaml:"callback_worker_interval"`
	CallbackMaxAttempts         int                 `yaml:"callback_max_attempts"`
	CallbackTimeoutSeconds      int                 `yaml:"callback_timeout_seconds"`
	AdminAllowedOrigins         []string            `yaml:"admin_allowed_origins"`
	AdminCookieDomain           string              `yaml:"admin_cookie_domain"`
	AdminCookieSameSite         string              `yaml:"admin_cookie_same_site"`
	PayoutReviewActorAllowlist  []string            `yaml:"payout_review_actor_allowlist"`
	PayoutReviewActorRoles      map[string][]string `yaml:"payout_review_actor_roles"`
	MerchantSecretEncryptionKey string              `yaml:"merchant_secret_encryption_key"`
	TrustedProxyCIDRs           []string            `yaml:"trusted_proxy_cidrs"`
	ReadHeaderTimeoutSeconds    int                 `yaml:"read_header_timeout_seconds"`
	ReadTimeoutSeconds          int                 `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds         int                 `yaml:"write_timeout_seconds"`
	IdleTimeoutSeconds          int                 `yaml:"idle_timeout_seconds"`
	ShutdownTimeoutSeconds      int                 `yaml:"shutdown_timeout_seconds"`
	AlertWebhookURL             string              `yaml:"alert_webhook_url"`
	AlertWebhookTimeoutSeconds  int                 `yaml:"alert_webhook_timeout_seconds"`
	WorkerLeaseSeconds          int                 `yaml:"worker_lease_seconds"`
	TestDepositCallbacksEnabled bool                `yaml:"test_deposit_callbacks_enabled"`
	HMACDiagnosticsEnabled      bool                `yaml:"hmac_diagnostics_enabled"`
	MockProviderEnabled         bool                `yaml:"mock_provider_enabled"`
	PublicBaseURL               string              `yaml:"public_base_url"`
}

type DatabaseConfig struct {
	DSN string `yaml:"dsn"`
}

type NewebpayDepositConfig struct {
	Environment string `yaml:"environment"`
	MPGURL      string `yaml:"mpg_url"`
	MerchantID  string `yaml:"merchant_id"`
	HashKey     string `yaml:"hash_key"`
	HashIV      string `yaml:"hash_iv"`
	NotifyURL   string `yaml:"notify_url"`
	ReturnURL   string `yaml:"return_url"`
}

type GatewayConfig struct {
	HMACSecret               string   `yaml:"hmac_secret"`
	PreviousHMACSecret       string   `yaml:"previous_hmac_secret"`
	MaxSkewSeconds           int      `yaml:"max_skew_seconds"`
	BaseURL                  string   `yaml:"base_url"`
	CustomerID               string   `yaml:"customer_id"`
	PayoutNotifyURL          string   `yaml:"payout_notify_url"`
	DepositCallbackAllowlist []string `yaml:"deposit_callback_allowlist"`
	PayoutCallbackAllowlist  []string `yaml:"payout_callback_allowlist"`
	HTTPTimeoutSeconds       int      `yaml:"http_timeout_seconds"`
}

type MerchantConfig struct {
	Code                  string `yaml:"code"`
	Name                  string `yaml:"name"`
	APIKey                string `yaml:"api_key"`
	CallbackSigningKeyID  string `yaml:"callback_signing_key_id"`
	CallbackSigningSecret string `yaml:"callback_signing_secret"`
	CallbackURL           string `yaml:"callback_url"`
	InitialBalanceTWD     int64  `yaml:"initial_balance_twd"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	applyEnv(&cfg)
	if cfg.App.Port == 0 {
		cfg.App.Port = 8080
	}
	return &cfg, nil
}

func (c *Config) Validate() ([]string, error) {
	var warnings []string
	var problems []string

	require := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, fmt.Sprintf("%s is required", name))
		}
	}

	require("DATABASE_DSN", c.Database.DSN)
	require("NEWEBPAY_MPG_URL", c.NewebpayDeposit.MPGURL)
	require("NEWEBPAY_MERCHANT_ID", c.NewebpayDeposit.MerchantID)
	require("NEWEBPAY_HASH_KEY", c.NewebpayDeposit.HashKey)
	require("NEWEBPAY_HASH_IV", c.NewebpayDeposit.HashIV)
	require("NEWEBPAY_NOTIFY_URL", c.NewebpayDeposit.NotifyURL)
	require("NEWEBPAY_RETURN_URL", c.NewebpayDeposit.ReturnURL)
	require("GATEWAY_HMAC_SECRET", c.Gateway.HMACSecret)
	require("GATEWAY_CUSTOMER_ID", c.Gateway.CustomerID)
	require("GATEWAY_BASE_URL", c.Gateway.BaseURL)
	require("GATEWAY_PAYOUT_NOTIFY_URL", c.Gateway.PayoutNotifyURL)
	if strings.TrimSpace(c.Database.DSN) != "" {
		require("MERCHANT_SECRET_ENCRYPTION_KEY", c.App.MerchantSecretEncryptionKey)
	}
	if strings.TrimSpace(c.Merchant.Code) != "" {
		require("MERCHANT_CALLBACK_SIGNING_KEY_ID", c.Merchant.CallbackSigningKeyID)
		require("MERCHANT_CALLBACK_SIGNING_SECRET", c.Merchant.CallbackSigningSecret)
	}

	checkURL := func(name, raw string, publicOnly bool) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			problems = append(problems, fmt.Sprintf("%s must be an absolute HTTP(S) URL", name))
			return
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			problems = append(problems, fmt.Sprintf("%s must be an absolute HTTP(S) URL", name))
			return
		}
		if !publicOnly {
			return
		}
		host := strings.TrimSpace(parsed.Hostname())
		if host == "" {
			problems = append(problems, fmt.Sprintf("%s host is required", name))
			return
		}
		if strings.EqualFold(host, "localhost") {
			problems = append(problems, fmt.Sprintf("%s must not target localhost", name))
			return
		}
		if ip := net.ParseIP(host); ip != nil {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
				problems = append(problems, fmt.Sprintf("%s must not target a private or loopback address", name))
			}
		}
	}

	checkURL("NEWEBPAY_MPG_URL", c.NewebpayDeposit.MPGURL, false)
	checkURL("NEWEBPAY_NOTIFY_URL", c.NewebpayDeposit.NotifyURL, true)
	checkURL("NEWEBPAY_RETURN_URL", c.NewebpayDeposit.ReturnURL, true)
	checkURL("GATEWAY_BASE_URL", c.Gateway.BaseURL, false)
	checkURL("GATEWAY_PAYOUT_NOTIFY_URL", c.Gateway.PayoutNotifyURL, true)
	checkURL("ALERT_WEBHOOK_URL", c.App.AlertWebhookURL, true)
	if strings.TrimSpace(c.App.AlertWebhookURL) != "" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.App.AlertWebhookURL)), "https://") {
		problems = append(problems, "ALERT_WEBHOOK_URL must use HTTPS")
	}
	validateRequiredAllowlistEntries("GATEWAY_DEPOSIT_CALLBACK_ALLOWLIST", c.Gateway.DepositCallbackAllowlist, &problems)
	validateRequiredAllowlistEntries("GATEWAY_PAYOUT_CALLBACK_ALLOWLIST", c.Gateway.PayoutCallbackAllowlist, &problems)
	validateAllowlistEntries("APP_TRUSTED_PROXY_CIDRS", c.App.TrustedProxyCIDRs, &problems)
	validateActorRoles(c.App.PayoutReviewActorRoles, &problems)
	validateEnvironmentIsolation(c, &problems)

	baseHost := hostOnly(c.Gateway.BaseURL)
	if baseHost != "" {
		for _, compare := range []struct {
			name string
			raw  string
		}{
			{name: "NEWEBPAY_NOTIFY_URL", raw: c.NewebpayDeposit.NotifyURL},
			{name: "NEWEBPAY_RETURN_URL", raw: c.NewebpayDeposit.ReturnURL},
			{name: "GATEWAY_PAYOUT_NOTIFY_URL", raw: c.Gateway.PayoutNotifyURL},
		} {
			if compareHost := hostOnly(compare.raw); compareHost != "" && strings.EqualFold(baseHost, compareHost) {
				warnings = append(warnings, fmt.Sprintf("GATEWAY_BASE_URL host matches %s host; this usually means upstream calls are pointed back to this service", compare.name))
			}
		}
	}

	if c.Gateway.HTTPTimeoutSeconds <= 0 {
		warnings = append(warnings, "GATEWAY_HTTP_TIMEOUT_SECONDS is not set; default client timeout behavior will be used")
	}
	if c.App.Port <= 0 {
		warnings = append(warnings, "APP_PORT is not set; defaulting to 8080")
	}
	if c.App.ReadHeaderTimeoutSeconds <= 0 {
		warnings = append(warnings, "APP_READ_HEADER_TIMEOUT_SECONDS is not set; default hardening timeout will be used")
	}
	if c.App.ReadTimeoutSeconds <= 0 {
		warnings = append(warnings, "APP_READ_TIMEOUT_SECONDS is not set; default hardening timeout will be used")
	}
	if c.App.WriteTimeoutSeconds <= 0 {
		warnings = append(warnings, "APP_WRITE_TIMEOUT_SECONDS is not set; default hardening timeout will be used")
	}
	if c.App.IdleTimeoutSeconds <= 0 {
		warnings = append(warnings, "APP_IDLE_TIMEOUT_SECONDS is not set; default hardening timeout will be used")
	}
	if c.App.ShutdownTimeoutSeconds <= 0 {
		warnings = append(warnings, "APP_SHUTDOWN_TIMEOUT_SECONDS is not set; default graceful shutdown timeout will be used")
	}

	if len(problems) > 0 {
		return warnings, fmt.Errorf(strings.Join(problems, "; "))
	}
	return warnings, nil
}

func hostOnly(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

func applyEnv(cfg *Config) {
	setString(&cfg.App.Env, "APP_ENV")
	setInt(&cfg.App.Port, "APP_PORT")
	setString(&cfg.App.PayoutReviewToken, "PAYOUT_REVIEW_TOKEN")
	setString(&cfg.App.AdminSessionSecret, "ADMIN_SESSION_SECRET")
	setString(&cfg.App.AdminInitialUsername, "ADMIN_INITIAL_USERNAME")
	setString(&cfg.App.AdminInitialPasswordHash, "ADMIN_INITIAL_PASSWORD_HASH")
	setString(&cfg.App.ReceiptStoragePath, "RECEIPT_STORAGE_PATH")
	setInt64(&cfg.App.ReceiptMaxSizeMB, "RECEIPT_MAX_SIZE_MB")
	setInt(&cfg.App.CallbackWorkerInterval, "CALLBACK_WORKER_INTERVAL")
	setInt(&cfg.App.CallbackMaxAttempts, "CALLBACK_MAX_ATTEMPTS")
	setInt(&cfg.App.CallbackTimeoutSeconds, "CALLBACK_TIMEOUT_SECONDS")
	setCSV(&cfg.App.AdminAllowedOrigins, "ADMIN_ALLOWED_ORIGINS")
	setString(&cfg.App.AdminCookieDomain, "ADMIN_COOKIE_DOMAIN")
	setString(&cfg.App.AdminCookieSameSite, "ADMIN_COOKIE_SAME_SITE")
	setCSV(&cfg.App.PayoutReviewActorAllowlist, "PAYOUT_REVIEW_ACTOR_ALLOWLIST")
	setActorRoles(&cfg.App.PayoutReviewActorRoles, "PAYOUT_REVIEW_ACTOR_ROLES")
	setString(&cfg.App.MerchantSecretEncryptionKey, "MERCHANT_SECRET_ENCRYPTION_KEY")
	setCSV(&cfg.App.TrustedProxyCIDRs, "APP_TRUSTED_PROXY_CIDRS")
	setInt(&cfg.App.ReadHeaderTimeoutSeconds, "APP_READ_HEADER_TIMEOUT_SECONDS")
	setInt(&cfg.App.ReadTimeoutSeconds, "APP_READ_TIMEOUT_SECONDS")
	setInt(&cfg.App.WriteTimeoutSeconds, "APP_WRITE_TIMEOUT_SECONDS")
	setInt(&cfg.App.IdleTimeoutSeconds, "APP_IDLE_TIMEOUT_SECONDS")
	setInt(&cfg.App.ShutdownTimeoutSeconds, "APP_SHUTDOWN_TIMEOUT_SECONDS")
	setString(&cfg.App.AlertWebhookURL, "ALERT_WEBHOOK_URL")
	setInt(&cfg.App.AlertWebhookTimeoutSeconds, "ALERT_WEBHOOK_TIMEOUT_SECONDS")
	setInt(&cfg.App.WorkerLeaseSeconds, "WORKER_LEASE_SECONDS")
	setBool(&cfg.App.TestDepositCallbacksEnabled, "TEST_DEPOSIT_CALLBACKS_ENABLED")
	setBool(&cfg.App.HMACDiagnosticsEnabled, "HMAC_DIAGNOSTICS_ENABLED")
	setBool(&cfg.App.MockProviderEnabled, "MOCK_PROVIDER_ENABLED")
	setString(&cfg.App.PublicBaseURL, "PUBLIC_BASE_URL")
	setString(&cfg.Database.DSN, "DATABASE_DSN")
	setString(&cfg.NewebpayDeposit.MPGURL, "NEWEBPAY_MPG_URL")
	setString(&cfg.NewebpayDeposit.Environment, "NEWEBPAY_ENV")
	setString(&cfg.NewebpayDeposit.MerchantID, "NEWEBPAY_MERCHANT_ID")
	setString(&cfg.NewebpayDeposit.HashKey, "NEWEBPAY_HASH_KEY")
	setString(&cfg.NewebpayDeposit.HashIV, "NEWEBPAY_HASH_IV")
	setString(&cfg.NewebpayDeposit.NotifyURL, "NEWEBPAY_NOTIFY_URL")
	setString(&cfg.NewebpayDeposit.ReturnURL, "NEWEBPAY_RETURN_URL")
	setString(&cfg.Gateway.HMACSecret, "GATEWAY_HMAC_SECRET")
	setString(&cfg.Gateway.PreviousHMACSecret, "GATEWAY_PREVIOUS_HMAC_SECRET")
	setInt(&cfg.Gateway.MaxSkewSeconds, "GATEWAY_MAX_SKEW_SECONDS")
	setString(&cfg.Gateway.BaseURL, "GATEWAY_BASE_URL")
	setString(&cfg.Gateway.CustomerID, "GATEWAY_CUSTOMER_ID")
	setString(&cfg.Gateway.PayoutNotifyURL, "GATEWAY_PAYOUT_NOTIFY_URL")
	setCSV(&cfg.Gateway.DepositCallbackAllowlist, "GATEWAY_DEPOSIT_CALLBACK_ALLOWLIST")
	setCSV(&cfg.Gateway.PayoutCallbackAllowlist, "GATEWAY_PAYOUT_CALLBACK_ALLOWLIST")
	setInt(&cfg.Gateway.HTTPTimeoutSeconds, "GATEWAY_HTTP_TIMEOUT_SECONDS")
	setString(&cfg.Merchant.Code, "MERCHANT_CODE")
	setString(&cfg.Merchant.Name, "MERCHANT_NAME")
	setString(&cfg.Merchant.APIKey, "MERCHANT_API_KEY")
	setString(&cfg.Merchant.CallbackSigningKeyID, "MERCHANT_CALLBACK_SIGNING_KEY_ID")
	setString(&cfg.Merchant.CallbackSigningSecret, "MERCHANT_CALLBACK_SIGNING_SECRET")
	setString(&cfg.Merchant.CallbackURL, "MERCHANT_CALLBACK_URL")
	setInt64(&cfg.Merchant.InitialBalanceTWD, "MERCHANT_INITIAL_BALANCE_TWD")
}

func validateEnvironmentIsolation(c *Config, problems *[]string) {
	env := strings.ToLower(strings.TrimSpace(c.App.Env))
	newebpayEnv := strings.ToLower(strings.TrimSpace(c.NewebpayDeposit.Environment))
	newebpayHost := strings.ToLower(hostOnly(c.NewebpayDeposit.MPGURL))
	publicHost := strings.ToLower(hostOnly(c.App.PublicBaseURL))
	notifyHost := strings.ToLower(hostOnly(c.NewebpayDeposit.NotifyURL))
	returnHost := strings.ToLower(hostOnly(c.NewebpayDeposit.ReturnURL))
	payoutCallbackHost := strings.ToLower(hostOnly(c.Gateway.PayoutNotifyURL))
	dsn := strings.ToLower(c.Database.DSN)

	if env != "production" && env != "sandbox" {
		return
	}
	if publicHost == "" {
		*problems = append(*problems, "PUBLIC_BASE_URL is required when APP_ENV is production or sandbox")
	}
	if notifyHost != "" && publicHost != "" && notifyHost != publicHost {
		*problems = append(*problems, "NEWEBPAY_NOTIFY_URL host must match PUBLIC_BASE_URL host")
	}
	if returnHost != "" && publicHost != "" && returnHost != publicHost {
		*problems = append(*problems, "NEWEBPAY_RETURN_URL host must match PUBLIC_BASE_URL host")
	}
	if payoutCallbackHost != "" && publicHost != "" && payoutCallbackHost != publicHost {
		*problems = append(*problems, "GATEWAY_PAYOUT_NOTIFY_URL host must match PUBLIC_BASE_URL host")
	}

	switch env {
	case "production":
		if c.App.HMACDiagnosticsEnabled {
			*problems = append(*problems, "HMAC_DIAGNOSTICS_ENABLED must be false in production")
		}
		if c.App.TestDepositCallbacksEnabled {
			*problems = append(*problems, "TEST_DEPOSIT_CALLBACKS_ENABLED must be false in production")
		}
		if c.App.MockProviderEnabled {
			*problems = append(*problems, "MOCK_PROVIDER_ENABLED must be false in production")
		}
		if newebpayEnv != "production" {
			*problems = append(*problems, "NEWEBPAY_ENV must be production when APP_ENV is production")
		}
		if newebpayHost != "core.newebpay.com" {
			*problems = append(*problems, "production NEWEBPAY_MPG_URL must target core.newebpay.com")
		}
		if publicHost != "api.nnviopp.com" {
			*problems = append(*problems, "production PUBLIC_BASE_URL must target api.nnviopp.com")
		}
		if strings.Contains(dsn, "sandbox") || strings.Contains(dsn, "_test") || strings.Contains(dsn, "-test") {
			*problems = append(*problems, "production DATABASE_DSN must not reference a sandbox or test database")
		}
	case "sandbox":
		if !c.App.TestDepositCallbacksEnabled {
			*problems = append(*problems, "TEST_DEPOSIT_CALLBACKS_ENABLED must be true in sandbox")
		}
		if newebpayEnv != "sandbox" {
			*problems = append(*problems, "NEWEBPAY_ENV must be sandbox when APP_ENV is sandbox")
		}
		if !c.App.MockProviderEnabled && newebpayHost != "ccore.newebpay.com" {
			*problems = append(*problems, "sandbox NEWEBPAY_MPG_URL must target ccore.newebpay.com unless MOCK_PROVIDER_ENABLED is true")
		}
		if publicHost != "sandbox-api.nnviopp.com" {
			*problems = append(*problems, "sandbox PUBLIC_BASE_URL must target sandbox-api.nnviopp.com")
		}
		if !strings.Contains(dsn, "sandbox") {
			*problems = append(*problems, "sandbox DATABASE_DSN must reference a database name containing sandbox")
		}
	}
}

func setString(target *string, key string) {
	if value := os.Getenv(key); value != "" {
		*target = value
	}
}

func setInt(target *int, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	parsed, err := strconv.Atoi(value)
	if err == nil {
		*target = parsed
	}
}

func setInt64(target *int64, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		*target = parsed
	}
}

func setBool(target *bool, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	parsed, err := strconv.ParseBool(value)
	if err == nil {
		*target = parsed
	}
}

func setCSV(target *[]string, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	*target = items
}

func setActorRoles(target *map[string][]string, key string) {
	value := os.Getenv(key)
	if value == "" {
		return
	}
	result := make(map[string][]string)
	actors := strings.Split(value, ",")
	for _, actorEntry := range actors {
		actorEntry = strings.TrimSpace(actorEntry)
		if actorEntry == "" {
			continue
		}
		parts := strings.SplitN(actorEntry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		actor := strings.TrimSpace(parts[0])
		if actor == "" {
			continue
		}
		roleParts := strings.Split(parts[1], "|")
		roles := make([]string, 0, len(roleParts))
		for _, role := range roleParts {
			role = strings.TrimSpace(role)
			if role != "" {
				roles = append(roles, role)
			}
		}
		if len(roles) > 0 {
			result[actor] = roles
		}
	}
	if len(result) > 0 {
		*target = result
	}
}

func validateAllowlistEntries(name string, entries []string, problems *[]string) {
	for _, raw := range entries {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err == nil {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			continue
		}
		*problems = append(*problems, fmt.Sprintf("%s entry %q must be a valid IP or CIDR", name, value))
	}
}

func validateRequiredAllowlistEntries(name string, entries []string, problems *[]string) {
	for _, raw := range entries {
		if strings.TrimSpace(raw) != "" {
			validateAllowlistEntries(name, entries, problems)
			return
		}
	}
	*problems = append(*problems, fmt.Sprintf("%s must contain at least one IP or CIDR", name))
}

func validateActorRoles(actorRoles map[string][]string, problems *[]string) {
	for actor, roles := range actorRoles {
		if strings.TrimSpace(actor) == "" {
			*problems = append(*problems, "APP payout review actor roles contain an empty actor")
			continue
		}
		if len(roles) == 0 {
			*problems = append(*problems, fmt.Sprintf("APP payout review actor %q must have at least one role", actor))
		}
	}
}
