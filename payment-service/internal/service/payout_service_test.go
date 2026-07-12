package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"payment-service/internal/domain"
	providerGateway "payment-service/internal/provider/gateway"
	"payment-service/internal/repository"
)

func TestCreatePayoutOrderAcceptsHashedMerchantAPIKey(t *testing.T) {
	sum := sha256.Sum256([]byte("merchant-secret"))
	store := repository.NewInMemoryPayoutStore()
	store.SeedMerchant(domain.Merchant{
		Code:        "M10001",
		Name:        "Merchant 1",
		APIKey:      hex.EncodeToString(sum[:]),
		Status:      "active",
		CallbackURL: "https://merchant.example/payout-callback",
	}, 500000)

	service := NewPayoutServiceWithSecrets(
		store,
		providerGateway.NewPayoutClient("", "50000", "sign-key", "https://payment-service.example/api/payments/callback", time.Second),
		map[string]string{"M10001": "merchant-secret"},
	)

	order, err := service.CreatePayoutOrder(context.Background(), CreatePayoutOrderRequest{
		MerchantID:       "M10001",
		APIKey:           "merchant-secret",
		MerchantPayoutNo: "HASH-001",
		Amount:           "100.00",
		Currency:         "TWD",
		PayAccountName:   "Tester",
		PayCardNo:        "202008372239",
		PayBankName:      "013",
	})
	if err != nil {
		t.Fatalf("CreatePayoutOrder() error = %v", err)
	}
	if order.MerchantCode != "M10001" {
		t.Fatalf("CreatePayoutOrder() merchant = %s, want M10001", order.MerchantCode)
	}
}
