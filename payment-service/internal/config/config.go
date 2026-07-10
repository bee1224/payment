package config

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App             AppConfig             `yaml:"app"`
	Database        DatabaseConfig        `yaml:"database"`
	NewebpayDeposit NewebpayDepositConfig `yaml:"newebpay"`
	RY              RYConfig              `yaml:"ry"`
	Merchant        MerchantConfig        `yaml:"merchant"`
}

type AppConfig struct {
	Name              string `yaml:"name"`
	Env               string `yaml:"env"`
	Port              int    `yaml:"port"`
	PayoutReviewToken string `yaml:"payout_review_token"`
}

type DatabaseConfig struct {
	DSN string `yaml:"dsn"`
}

type NewebpayDepositConfig struct {
	MPGURL     string `yaml:"mpg_url"`
	MerchantID string `yaml:"merchant_id"`
	HashKey    string `yaml:"hash_key"`
	HashIV     string `yaml:"hash_iv"`
	NotifyURL  string `yaml:"notify_url"`
	ReturnURL  string `yaml:"return_url"`
}

type RYConfig struct {
	SignKey            string `yaml:"sign_key"`
	MaxSkewSeconds     int    `yaml:"max_skew_seconds"`
	BaseURL            string `yaml:"base_url"`
	CustomerID         string `yaml:"customer_id"`
	PayoutNotifyURL    string `yaml:"payout_notify_url"`
	HTTPTimeoutSeconds int    `yaml:"http_timeout_seconds"`
}

type MerchantConfig struct {
	Code              string `yaml:"code"`
	Name              string `yaml:"name"`
	APIKey            string `yaml:"api_key"`
	CallbackURL       string `yaml:"callback_url"`
	InitialBalanceTWD int64  `yaml:"initial_balance_twd"`
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

func applyEnv(cfg *Config) {
	setString(&cfg.App.Env, "APP_ENV")
	setInt(&cfg.App.Port, "APP_PORT")
	setString(&cfg.App.PayoutReviewToken, "PAYOUT_REVIEW_TOKEN")
	setString(&cfg.Database.DSN, "DATABASE_DSN")
	setString(&cfg.NewebpayDeposit.MPGURL, "NEWEBPAY_MPG_URL")
	setString(&cfg.NewebpayDeposit.MerchantID, "NEWEBPAY_MERCHANT_ID")
	setString(&cfg.NewebpayDeposit.HashKey, "NEWEBPAY_HASH_KEY")
	setString(&cfg.NewebpayDeposit.HashIV, "NEWEBPAY_HASH_IV")
	setString(&cfg.NewebpayDeposit.NotifyURL, "NEWEBPAY_NOTIFY_URL")
	setString(&cfg.NewebpayDeposit.ReturnURL, "NEWEBPAY_RETURN_URL")
	setString(&cfg.RY.SignKey, "RY_SIGN_KEY")
	setInt(&cfg.RY.MaxSkewSeconds, "RY_MAX_SKEW_SECONDS")
	setString(&cfg.RY.BaseURL, "RY_BASE_URL")
	setString(&cfg.RY.CustomerID, "RY_CUSTOMER_ID")
	setString(&cfg.RY.PayoutNotifyURL, "RY_PAYOUT_NOTIFY_URL")
	setInt(&cfg.RY.HTTPTimeoutSeconds, "RY_HTTP_TIMEOUT_SECONDS")
	setString(&cfg.Merchant.Code, "MERCHANT_CODE")
	setString(&cfg.Merchant.Name, "MERCHANT_NAME")
	setString(&cfg.Merchant.APIKey, "MERCHANT_API_KEY")
	setString(&cfg.Merchant.CallbackURL, "MERCHANT_CALLBACK_URL")
	setInt64(&cfg.Merchant.InitialBalanceTWD, "MERCHANT_INITIAL_BALANCE_TWD")
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
