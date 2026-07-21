package repository

import (
	"context"
	"testing"
	"time"

	"payment-service/internal/domain"
)

func TestInMemoryPayoutCallbackTaskDeadLettersAfterMaximumAttempts(t *testing.T) {
	store := NewInMemoryPayoutStore()
	if err := store.CreateMerchantPayoutCallbackTask(context.Background(), domain.MerchantPayoutCallbackTask{CallbackURL: "https://merchant.example/callback", Payload: `{}`}); err != nil {
		t.Fatal(err)
	}
	task := store.tasks[1]
	task.Status = "processing"
	task.ClaimToken = "claim-token"
	task.RetryCount = maxMerchantPayoutCallbackAttempts - 1
	store.tasks[task.ID] = task

	if err := store.MarkMerchantPayoutCallbackTaskResult(context.Background(), task.ID, task.ClaimToken, false, time.Now().Add(time.Minute), "callback failed"); err != nil {
		t.Fatal(err)
	}
	if got := store.tasks[task.ID]; got.Status != "dead_letter" || got.RetryCount != maxMerchantPayoutCallbackAttempts {
		t.Fatalf("unexpected terminal task state: %+v", got)
	}
}

func TestPayoutReversalRequiresCompletedTransactionMatch(t *testing.T) {
	completed := domain.PayoutOrder{ProviderOrderNo: "provider-transaction-001"}
	if err := ensurePayoutReversalMatchesCompletedTransaction(completed, PayoutProviderResult{ProviderOrderNo: "provider-transaction-001"}); err != nil {
		t.Fatalf("matching reversal should be accepted: %v", err)
	}
	if err := ensurePayoutReversalMatchesCompletedTransaction(completed, PayoutProviderResult{ProviderOrderNo: "provider-transaction-002"}); err == nil {
		t.Fatal("mismatched reversal transaction must be rejected")
	}
}
