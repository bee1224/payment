package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	merchantSecretCipher := repository.NewMerchantSecretCipher(s.cfg.App.MerchantSecretEncryptionKey)
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
	reconciliationStore := repository.ReconciliationStore(repository.NewInMemoryReconciliationStore())
	replayNonceStore := repository.ReplayNonceStore(repository.NewInMemoryReplayNonceStore())
	workerLeases := repository.WorkerLeaseStore(repository.NewInMemoryWorkerLeaseStore())
	var manualPayoutService *service.ManualPayoutService
	var manualPayoutStore *repository.ManualPayoutStore
	var adminUsers *repository.AdminUserStore
	merchantBootstrap := repository.MerchantBootstrap{
		Code:                  s.cfg.Merchant.Code,
		Name:                  s.cfg.Merchant.Name,
		APIKey:                s.cfg.Merchant.APIKey,
		CallbackSigningKeyID:  s.cfg.Merchant.CallbackSigningKeyID,
		CallbackSigningSecret: s.cfg.Merchant.CallbackSigningSecret,
		CallbackURL:           s.cfg.Merchant.CallbackURL,
		InitialBalanceTWD:     s.cfg.Merchant.InitialBalanceTWD,
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
		if err := repository.SeedMerchantInDB(context.Background(), sqlDB, merchantBootstrap, merchantSecretCipher); err != nil {
			return err
		}
		if err := repository.SeedMerchantCallbackSigningKey(context.Background(), sqlDB, merchantBootstrap.Code, merchantBootstrap.CallbackSigningKeyID, merchantBootstrap.CallbackSigningSecret, merchantSecretCipher); err != nil {
			return err
		}
		depositService = service.NewPersistentDepositService(
			depositGateways,
			depositChannelProviders,
			ledger,
			repository.NewMySQLDepositStore(sqlDB),
		)
		payoutStore = repository.NewMySQLPayoutStore(sqlDB, merchantSecretCipher)
		reconciliationStore = repository.NewMySQLReconciliationStore(sqlDB)
		replayNonceStore = repository.NewMySQLReplayNonceStore(sqlDB)
		workerLeases = repository.NewMySQLWorkerLeaseStore(sqlDB)
		storage, storageErr := service.NewLocalReceiptStorage(s.cfg.App.ReceiptStoragePath, s.cfg.App.ReceiptMaxSizeMB)
		if storageErr != nil {
			return storageErr
		}
		manualPayoutStore = repository.NewManualPayoutStore(sqlDB)
		manualPayoutService = service.NewManualPayoutService(manualPayoutStore, storage)
		adminUsers = repository.NewAdminUserStore(sqlDB)
		if err := adminUsers.SeedInitial(context.Background(), s.cfg.App.AdminInitialUsername, s.cfg.App.AdminInitialPasswordHash); err != nil {
			return err
		}
	}
	payoutClient := providerGateway.NewPayoutClient(
		s.cfg.Gateway.BaseURL,
		s.cfg.Gateway.CustomerID,
		s.cfg.Gateway.HMACSecret,
		s.cfg.Gateway.PayoutNotifyURL,
		time.Duration(s.cfg.Gateway.HTTPTimeoutSeconds)*time.Second,
	)
	merchantSecrets := map[string]string{}
	if merchantBootstrap.Enabled() {
		merchantSecrets[merchantBootstrap.Code] = merchantBootstrap.APIKey
	}
	payoutService := service.NewPayoutServiceWithSecrets(payoutStore, payoutClient, merchantSecrets)
	if sqlDB != nil {
		callbackSigningKeys := repository.NewMySQLCallbackSigningKeyStore(sqlDB, merchantSecretCipher)
		depositService.SetCallbackSigningKeyResolver(callbackSigningKeys)
		payoutService.SetCallbackSigningKeyResolver(callbackSigningKeys)
	}
	payoutService.SetReconciliationService(service.NewReconciliationService(reconciliationStore))
	payoutService.SetMerchantAuthMaxSkew(time.Duration(s.cfg.Gateway.MaxSkewSeconds) * time.Second)
	payoutService.SetReplayNonceStore(replayNonceStore)
	if s.cfg.App.AlertWebhookURL != "" {
		payoutService.SetAlertNotifier(service.NewWebhookPayoutAlertNotifier(s.cfg.App.AlertWebhookURL, time.Duration(s.cfg.App.AlertWebhookTimeoutSeconds)*time.Second))
	}
	var manualCallbackWorker *service.ManualCallbackWorker
	if manualPayoutStore != nil {
		manualCallbackWorker = service.NewManualCallbackWorker(manualPayoutStore, payoutService.ResolveMerchantCallbackSigningKey, time.Duration(s.cfg.App.CallbackTimeoutSeconds)*time.Second, s.cfg.App.CallbackMaxAttempts, "payment-api")
	}
	router := delivery.NewRouterWithOperations(depositService, payoutService, manualPayoutService, adminUsers, s.cfg.App, s.cfg.Gateway, replayNonceStore)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	holder, _ := os.Hostname()
	if holder == "" {
		holder = "payment-api"
	}
	holder = fmt.Sprintf("%s-%d", holder, os.Getpid())
	leaseTTL := configuredDuration(s.cfg.App.WorkerLeaseSeconds, 45*time.Second)
	go runDepositLoops(ctx, depositService, workerLeases, holder, leaseTTL)
	go runPayoutLoops(ctx, payoutService, workerLeases, holder, leaseTTL)
	go runReconciliationLoop(ctx, payoutService, workerLeases, holder, leaseTTL)
	if manualCallbackWorker != nil {
		go runManualCallbackLoop(ctx, manualCallbackWorker, s.cfg.App.CallbackWorkerInterval, workerLeases, holder, leaseTTL)
	}
	addr := fmt.Sprintf(":%d", s.cfg.App.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: configuredDuration(s.cfg.App.ReadHeaderTimeoutSeconds, 5*time.Second),
		ReadTimeout:       configuredDuration(s.cfg.App.ReadTimeoutSeconds, 15*time.Second),
		WriteTimeout:      configuredDuration(s.cfg.App.WriteTimeoutSeconds, 15*time.Second),
		IdleTimeout:       configuredDuration(s.cfg.App.IdleTimeoutSeconds, 60*time.Second),
		MaxHeaderBytes:    1 << 20,
	}
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), configuredDuration(s.cfg.App.ShutdownTimeoutSeconds, 15*time.Second))
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func runManualCallbackLoop(ctx context.Context, worker *service.ManualCallbackWorker, intervalSeconds int, leases repository.WorkerLeaseStore, holder string, ttl time.Duration) {
	interval := configuredDuration(intervalSeconds, 15*time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if acquired, _ := leases.TryAcquire(ctx, "manual-callback", holder, ttl); acquired {
			_ = worker.RunOnce(ctx, 20)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

const depositCallbackWorkerLeaseName = "deposit-callback"
const depositCallbackWorkerBatchSize = 20

type depositLoopService interface {
	RetryDepositCallbacks(context.Context, int) error
	ExpireDueDeposits(context.Context, int) error
}

type depositCallbackRecoveryService interface {
	RecoverMissingDepositCallbacks(context.Context, int) error
}

type printfLogger interface {
	Printf(string, ...any)
}

func runDepositLoops(ctx context.Context, depositService depositLoopService, leases repository.WorkerLeaseStore, holder string, ttl time.Duration) {
	callbackTicker := time.NewTicker(15 * time.Second)
	expireTicker := time.NewTicker(1 * time.Minute)
	defer callbackTicker.Stop()
	defer expireTicker.Stop()
	runDepositLoopsWithTicks(ctx, depositService, leases, holder, ttl, callbackTicker.C, expireTicker.C, log.Default())
}

// runDepositLoopsWithTicks keeps the production loop small while allowing
// deterministic worker-observability tests without sleeping.
func runDepositLoopsWithTicks(ctx context.Context, depositService depositLoopService, leases repository.WorkerLeaseStore, holder string, ttl time.Duration, callbackTicks, expireTicks <-chan time.Time, logger printfLogger) {
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("component=deposit_callback_worker operation=start lease=%s worker_id=%s poll_interval=%s batch_size=%d worker_lease_ttl=%s", depositCallbackWorkerLeaseName, holder, 15*time.Second, depositCallbackWorkerBatchSize, ttl)
	for {
		select {
		case <-ctx.Done():
			logger.Printf("component=deposit_callback_worker operation=stop lease=%s worker_id=%s reason=context_canceled", depositCallbackWorkerLeaseName, holder)
			return
		case <-callbackTicks:
			if acquired, err := leases.TryAcquire(ctx, depositCallbackWorkerLeaseName, holder, ttl); err != nil {
				logger.Printf("component=deposit_callback_worker operation=acquire_lease_failed lease=%s worker_id=%s poll_interval=%s error_class=lease_acquire_failed error_type=%T", depositCallbackWorkerLeaseName, holder, 15*time.Second, err)
			} else if acquired {
				if err := depositService.RetryDepositCallbacks(ctx, depositCallbackWorkerBatchSize); err != nil && !errors.Is(err, context.Canceled) {
					logger.Printf("component=deposit_callback_worker operation=retry_deposit_callbacks lease=%s worker_id=%s poll_interval=%s error_class=retry_deposit_callbacks_failed error_type=%T", depositCallbackWorkerLeaseName, holder, 15*time.Second, err)
				}
			}
		case <-expireTicks:
			if acquired, _ := leases.TryAcquire(ctx, "deposit-expiry", holder, ttl); acquired {
				_ = depositService.ExpireDueDeposits(ctx, 100)
				if recovery, ok := depositService.(depositCallbackRecoveryService); ok {
					if err := recovery.RecoverMissingDepositCallbacks(ctx, 100); err != nil && !errors.Is(err, context.Canceled) {
						logger.Printf("component=deposit_callback_worker operation=recover_missing_callbacks error_class=recovery_failed error_type=%T", err)
					}
				}
			}
		}
	}
}

func runPayoutLoops(ctx context.Context, payoutService *service.PayoutService, leases repository.WorkerLeaseStore, holder string, ttl time.Duration) {
	callbackTicker := time.NewTicker(15 * time.Second)
	reconcileTicker := time.NewTicker(20 * time.Second)
	defer callbackTicker.Stop()
	defer reconcileTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-callbackTicker.C:
			if acquired, _ := leases.TryAcquire(ctx, "payout-callback", holder, ttl); acquired {
				_ = payoutService.RetryMerchantCallbacks(ctx, 20)
			}
		case <-reconcileTicker.C:
			if acquired, _ := leases.TryAcquire(ctx, "payout-dispatch-reconcile", holder, ttl); acquired {
				_ = payoutService.ReconcilePendingPayouts(ctx, 20)
			}
		}
	}
}

func runReconciliationLoop(ctx context.Context, payoutService *service.PayoutService, leases repository.WorkerLeaseStore, holder string, ttl time.Duration) {
	if acquired, _ := leases.TryAcquire(ctx, "daily-reconciliation", holder, ttl); acquired {
		_, _ = payoutService.RunReconciliation(ctx, service.RunReconciliationRequest{})
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if acquired, _ := leases.TryAcquire(ctx, "daily-reconciliation", holder, ttl); acquired {
				_, _ = payoutService.RunReconciliation(ctx, service.RunReconciliationRequest{})
			}
		}
	}
}

func configuredDuration(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
