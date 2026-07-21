package repository

import (
	"strings"
	"testing"

	"payment-service/internal/domain"
)

func TestValidateDepositNotificationState(t *testing.T) {
	tests := []struct {
		name              string
		orderStatus       domain.DepositOrderStatus
		notificationState string
		transactionStatus string
		storedTradeNo     string
		notificationTrade string
		ledgerCount       int64
		wantIdempotent    bool
		wantError         string
	}{
		{name: "identical paid retry", orderStatus: domain.DepositOrderStatusPaid, notificationState: "SUCCESS", transactionStatus: "paid", storedTradeNo: "T-001", notificationTrade: "T-001", ledgerCount: 1, wantIdempotent: true},
		{name: "different trade number", orderStatus: domain.DepositOrderStatusPaid, notificationState: "SUCCESS", transactionStatus: "paid", storedTradeNo: "T-001", notificationTrade: "T-002", ledgerCount: 1, wantError: "does not match"},
		{name: "paid without ledger", orderStatus: domain.DepositOrderStatusPaid, notificationState: "SUCCESS", transactionStatus: "paid", storedTradeNo: "T-001", notificationTrade: "T-001", ledgerCount: 0, wantError: "0 deposit paid ledger"},
		{name: "ledger with non-paid order", orderStatus: domain.DepositOrderStatusPending, notificationState: "SUCCESS", transactionStatus: "pending", storedTradeNo: "", notificationTrade: "T-001", ledgerCount: 1, wantError: "conflicts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDepositNotificationState(tt.orderStatus, tt.notificationState, tt.transactionStatus, tt.storedTradeNo, tt.notificationTrade, tt.ledgerCount)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil || got != tt.wantIdempotent {
				t.Fatalf("idempotent=%t error=%v", got, err)
			}
		})
	}
}
