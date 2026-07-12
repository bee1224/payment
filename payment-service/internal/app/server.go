package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"payment-service/internal/config"
	delivery "payment-service/internal/delivery/http"
	"payment-service/internal/provider"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/provider/newebpay"
	"payment-service/internal/repository"
	"payment-service/internal/service"
)

type Server struct {
	cfg *config.Config
}

func NewServer(cfg *config.Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Run() error {
	newebpayDepositGateway := newebpay.NewDepositClient(
		s.cfg.NewebpayDeposit.MPGURL,
		s.cfg.NewebpayDeposit.MerchantID,
		s.cfg.NewebpayDeposit.HashKey,
		s.cfg.NewebpayDeposit.HashIV,
		s.cfg.NewebpayDeposit.NotifyURL,
		s.cfg.NewebpayDeposit.ReturnURL,
	)
	depositGateways := map[string]provider.DepositGateway{
		"newebpay": newebpayDepositGateway,
	}
	depositChannelProviders := map[string]string{
		"CREDIT":    "newebpay",
		"APPLEPAY":  "newebpay",
		"GOOGLEPAY": "newebpay",
		"WEBATM":    "newebpay",
		"VACC":      "newebpay",
		"CVS":       "newebpay",
		"BARCODE":   "newebpay",
	}
	ledger := service.NewLedgerService()
	depositService := service.NewDepositService(depositGateways, depositChannelProviders, ledger)
	inMemoryPayoutStore := repository.NewInMemoryPayoutStore()
	payoutStore := repository.PayoutStore(inMemoryPayoutStore)
	merchantBootstrap := repository.MerchantBootstrap{
		Code:              s.cfg.Merchant.Code,
		Name:              s.cfg.Merchant.Name,
		APIKey:            s.cfg.Merchant.APIKey,
		CallbackURL:       s.cfg.Merchant.CallbackURL,
		InitialBalanceTWD: s.cfg.Merchant.InitialBalanceTWD,
	}
	if merchantBootstrap.Enabled() {
		inMemoryPayoutStore.SeedMerchant(merchantBootstrap.Merchant(), merchantBootstrap.InitialBalanceTWD*100)
	}
	sqlDB, err := repository.Open(s.cfg.Database.DSN)
	if err != nil {
		return err
	}
	if sqlDB != nil {
		defer sqlDB.Close()
		if err := repository.SeedMerchantInDB(context.Background(), sqlDB, merchantBootstrap); err != nil {
			return err
		}
		depositService = service.NewPersistentDepositService(
			depositGateways,
			depositChannelProviders,
			ledger,
			repository.NewMySQLDepositStore(sqlDB),
		)
		payoutStore = repository.NewMySQLPayoutStore(sqlDB)
	}
	payoutClient := providerGateway.NewPayoutClient(
		s.cfg.Gateway.BaseURL,
		s.cfg.Gateway.CustomerID,
		s.cfg.Gateway.SignKey,
		s.cfg.Gateway.PayoutNotifyURL,
		time.Duration(s.cfg.Gateway.HTTPTimeoutSeconds)*time.Second,
	)
	merchantSecrets := map[string]string{}
	if merchantBootstrap.Enabled() {
		merchantSecrets[merchantBootstrap.Code] = merchantBootstrap.APIKey
	}
	payoutService := service.NewPayoutServiceWithSecrets(payoutStore, payoutClient, merchantSecrets)
	go runPayoutLoops(payoutService)
	router := delivery.NewRouter(depositService, payoutService, s.cfg.App, s.cfg.Gateway)
	addr := fmt.Sprintf(":%d", s.cfg.App.Port)
	return http.ListenAndServe(addr, router)
}

func runPayoutLoops(payoutService *service.PayoutService) {
	reconcileTicker := time.NewTicker(30 * time.Second)
	callbackTicker := time.NewTicker(15 * time.Second)
	defer reconcileTicker.Stop()
	defer callbackTicker.Stop()
	for {
		select {
		case <-reconcileTicker.C:
			_ = payoutService.ReconcilePendingPayouts(context.Background(), 20)
		case <-callbackTicker.C:
			_ = payoutService.RetryMerchantCallbacks(context.Background(), 20)
		}
	}
}
