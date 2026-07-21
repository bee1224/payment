package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	PaymentBaseURL        string
	CustomerID            string
	CustomerSecret        string
	MerchantID            string
	MerchantSecret        string
	APIKey                string
	CallbackKeyID         string
	CallbackSigningSecret string
	ListenAddr            string
	CallbackPath          string
	CallbackResponseMode  string
	TimeoutDelay          time.Duration
	RecordsPath           string
}

func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}
	delay, err := time.ParseDuration(value("MERCHANT_SANDBOX_TIMEOUT_DELAY", "35s"))
	if err != nil || delay <= 0 {
		return Config{}, fmt.Errorf("MERCHANT_SANDBOX_TIMEOUT_DELAY must be a positive duration")
	}
	c := Config{
		PaymentBaseURL:        strings.TrimRight(os.Getenv("PAYMENT_SANDBOX_BASE_URL"), "/"),
		CustomerID:            os.Getenv("PAYMENT_CUSTOMER_ID"),
		CustomerSecret:        os.Getenv("PAYMENT_CUSTOMER_SECRET"),
		MerchantID:            os.Getenv("PAYMENT_MERCHANT_ID"),
		MerchantSecret:        os.Getenv("PAYMENT_MERCHANT_SECRET"),
		APIKey:                os.Getenv("PAYMENT_API_KEY"),
		CallbackKeyID:         os.Getenv("MERCHANT_SANDBOX_CALLBACK_KEY_ID"),
		CallbackSigningSecret: os.Getenv("MERCHANT_SANDBOX_CALLBACK_SIGNING_SECRET"),
		ListenAddr:            value("MERCHANT_SANDBOX_LISTEN_ADDR", ":8081"),
		CallbackPath:          value("MERCHANT_SANDBOX_CALLBACK_PATH", "/callbacks/payment"),
		CallbackResponseMode:  value("MERCHANT_SANDBOX_CALLBACK_RESPONSE_MODE", "success"),
		TimeoutDelay:          delay,
		RecordsPath:           value("MERCHANT_SANDBOX_RECORDS_PATH", "var/callback-records.jsonl"),
	}
	if !strings.HasPrefix(c.CallbackPath, "/") {
		return Config{}, fmt.Errorf("MERCHANT_SANDBOX_CALLBACK_PATH must begin with /")
	}
	if c.CallbackResponseMode != "success" && c.CallbackResponseMode != "invalid_body" && c.CallbackResponseMode != "server_error" && c.CallbackResponseMode != "timeout" {
		return Config{}, fmt.Errorf("unsupported callback response mode %q", c.CallbackResponseMode)
	}
	if c.PaymentBaseURL != "" {
		u, err := url.Parse(c.PaymentBaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return Config{}, fmt.Errorf("PAYMENT_SANDBOX_BASE_URL must be an HTTPS URL")
		}
		if strings.EqualFold(u.Hostname(), "api.nnviopp.com") {
			return Config{}, fmt.Errorf("Production payment URL is not permitted")
		}
	}
	return c, nil
}

// loadDotEnv deliberately only fills unset process variables. It keeps Docker
// Compose and explicitly exported values authoritative while making the local
// CLI usable after copying .env.example to .env.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, rawValue, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return fmt.Errorf("invalid %s line %d", path, line)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value := strings.TrimSpace(rawValue)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s from %s: %w", key, path, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
