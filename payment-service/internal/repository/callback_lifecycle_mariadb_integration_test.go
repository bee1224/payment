//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"payment-service/internal/domain"

	"github.com/golang-migrate/migrate/v4"
)

// These tests deliberately require a caller-provided, disposable MariaDB.
// Keeping the build tag prevents ordinary developer test runs from ever
// reaching a database.
const lifecycleDSNEnv = "PAYMENT_SERVICE_TEST_MARIADB_DSN"
const lifecycleMigrationsEnv = "PAYMENT_SERVICE_TEST_MIGRATIONS_PATH"

func TestMariaDBLegacyCallbackTaskCompatibility(t *testing.T) {
	h := newCallbackLifecycleHarness(t, true)
	ctx := context.Background()
	legacy := h.insertTask(t, "legacy")

	var beforeEventKey, beforeStatus string
	if err := h.db.QueryRow(`SELECT event_key, status FROM merchant_deposit_callback_tasks WHERE id=?`, legacy.ID).Scan(&beforeEventKey, &beforeStatus); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(h.dsn, h.migrations); err != nil { // production runner applies 000005
		t.Fatalf("apply 000005 through production runner: %v", err)
	}

	var attemptCount int
	var lease sql.NullTime
	var nullable, defaultValue, columnType string
	if err := h.db.QueryRow(`SELECT attempt_count, claim_expires_at FROM merchant_deposit_callback_tasks WHERE id=?`, legacy.ID).Scan(&attemptCount, &lease); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT is_nullable, column_default, column_type FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='merchant_deposit_callback_tasks' AND column_name='claim_expires_at'`).Scan(&nullable, &defaultValue, &columnType); err != nil {
		t.Fatal(err)
	}
	if attemptCount != 0 || lease.Valid || nullable != "YES" || !strings.Contains(columnType, "timestamp") || defaultValue != "NULL" {
		t.Fatalf("legacy migration values: attempts=%d lease.valid=%v nullable=%s default=%s type=%s", attemptCount, lease.Valid, nullable, defaultValue, columnType)
	}
	got, err := h.store.findMerchantDepositCallbackTaskByEventKey(ctx, beforeEventKey)
	if err != nil || got.ID != legacy.ID || got.Status != beforeStatus {
		t.Fatalf("production repository could not read legacy task: task=%+v err=%v", got, err)
	}
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(ctx, h.now, h.now.Add(-time.Minute), 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != legacy.ID {
		t.Fatalf("legacy task lifecycle claim: tasks=%+v err=%v", claimed, err)
	}
}

func TestMariaDBEnsureMerchantDepositCallbackTaskIdempotency(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task := h.task("ensure")
	first, recovered, err := h.store.EnsureMerchantDepositCallbackTask(context.Background(), task)
	if err != nil || recovered {
		t.Fatalf("first ensure: task=%+v recovered=%v err=%v", first, recovered, err)
	}
	second, recovered, err := h.store.EnsureMerchantDepositCallbackTask(context.Background(), task)
	if err != nil || !recovered || second.ID != first.ID {
		t.Fatalf("duplicate ensure: first=%+v second=%+v recovered=%v err=%v", first, second, recovered, err)
	}
	other := h.task("ensure-other")
	if _, recovered, err := h.store.EnsureMerchantDepositCallbackTask(context.Background(), other); err != nil || recovered {
		t.Fatalf("different event key: recovered=%v err=%v", recovered, err)
	}
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM merchant_deposit_callback_tasks WHERE event_key IN (?,?)`, task.EventKey, other.EventKey).Scan(&count); err != nil || count != 2 {
		t.Fatalf("unique constraint row count=%d err=%v", count, err)
	}
}

func TestMariaDBClaimTokenAndLease(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task := h.insertTask(t, "claim")
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now, h.now.Add(-time.Minute), 1)
	if err != nil || len(claimed) != 1 || claimed[0].ClaimToken == "" {
		t.Fatalf("claim: tasks=%+v err=%v", claimed, err)
	}
	if claimed[0].ClaimExpiresAt == nil || !claimed[0].ClaimExpiresAt.Equal(h.now.Add(2*time.Minute)) {
		t.Fatalf("deterministic lease=%v", claimed[0].ClaimExpiresAt)
	}
	again, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now.Add(time.Minute), h.now, 1)
	if err != nil || len(again) != 0 {
		t.Fatalf("unexpired task re-claimed: tasks=%+v err=%v", again, err)
	}
	var status, token string
	var lease time.Time
	if err := h.db.QueryRow(`SELECT status, claim_token, claim_expires_at FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&status, &token, &lease); err != nil || status != domain.CallbackTaskProcessing || token != claimed[0].ClaimToken || !lease.Equal(h.now.Add(2*time.Minute)) {
		t.Fatalf("reload: status=%s tokenMatch=%v lease=%s err=%v", status, token == claimed[0].ClaimToken, lease, err)
	}
}

func TestMariaDBBeginAttemptLifecycle(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	h.insertTask(t, "attempt")
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now, h.now, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	first, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), claimed[0].ID, claimed[0].ClaimToken, h.now)
	if err != nil || first.AttemptNo != 1 || first.Status != domain.CallbackAttemptRunning || first.TaskID != claimed[0].ID {
		t.Fatalf("first attempt=%+v err=%v", first, err)
	}
	if _, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), claimed[0].ID, claimed[0].ClaimToken, h.now); !errors.Is(err, ErrCallbackClaimLost) {
		t.Fatalf("second BeginAttempt with an existing running attempt err=%v", err)
	}
	if _, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), claimed[0].ID, "wrong-token", h.now); !errors.Is(err, ErrCallbackClaimLost) {
		t.Fatalf("invalid BeginAttempt err=%v", err)
	}
	var attempts, attemptCount int
	var status string
	if err := h.db.QueryRow(`SELECT attempt_count, status FROM merchant_deposit_callback_tasks WHERE id=?`, claimed[0].ID).Scan(&attemptCount, &status); err != nil || attemptCount != 1 || status != domain.CallbackTaskProcessing {
		t.Fatalf("task after attempts: count=%d status=%s err=%v", attemptCount, status, err)
	}
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM merchant_deposit_callback_attempts WHERE task_id=? AND status='running'`, claimed[0].ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempt rows=%d err=%v", attempts, err)
	}
}

func TestMariaDBStaleClaimTokenRejected(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task := h.insertTask(t, "stale")
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now, h.now, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim A: %v %v", claimed, err)
	}
	// This models the durable state after a reclaimer has assigned token B;
	// this test intentionally does not test recovery behaviour itself.
	tokenB := "replacement-claim-token"
	if _, err := h.db.Exec(`UPDATE merchant_deposit_callback_tasks SET claim_token=?, claim_expires_at=? WHERE id=?`, tokenB, h.now.Add(2*time.Minute), task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), task.ID, claimed[0].ClaimToken, h.now); !errors.Is(err, ErrCallbackClaimLost) {
		t.Fatalf("stale token A was not rejected: %v", err)
	}
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM merchant_deposit_callback_attempts WHERE task_id=?`, task.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale token partially changed attempts=%d err=%v", count, err)
	}
	valid, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), task.ID, tokenB, h.now)
	if err != nil || valid.AttemptNo != 1 {
		t.Fatalf("token B valid attempt=%+v err=%v", valid, err)
	}
}

func TestMariaDBFinalizeSuccessAtomicity(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task, attempt := h.runningTask(t, "success")
	result := domain.DeliveryResult{HTTPStatus: 204, ResponseSummary: "OK", Elapsed: 37 * time.Millisecond}
	if err := h.store.FinalizeMerchantDepositCallbackSuccess(context.Background(), task.ID, task.ClaimToken, attempt.ID, result, h.now); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, task.ID, "sent", 0, "", true)
	h.assertAttempt(t, attempt.ID, "success", 204, "", true)
	if err := h.store.FinalizeMerchantDepositCallbackSuccess(context.Background(), task.ID, task.ClaimToken, attempt.ID, result, h.now); !errors.Is(err, ErrCallbackClaimLost) {
		t.Fatalf("duplicate success finalize err=%v", err)
	}
	h.assertAttempt(t, attempt.ID, "success", 204, "", true)
}

func TestMariaDBFinalizeSuccessRollback(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task, attempt := h.runningTask(t, "success-rollback")
	h.failTaskUpdate(t)
	err := h.store.FinalizeMerchantDepositCallbackSuccess(context.Background(), task.ID, task.ClaimToken, attempt.ID, domain.DeliveryResult{HTTPStatus: 200, ResponseSummary: "OK"}, h.now)
	if err == nil {
		t.Fatal("expected task update trigger failure")
	}
	h.assertTask(t, task.ID, "processing", 0, task.ClaimToken, false)
	h.assertAttempt(t, attempt.ID, "running", 0, "", false)
}

func TestMariaDBFinalizeFailureAtomicityAndDeadLetter(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task, attempt := h.runningTask(t, "failure")
	next := h.now.Add(time.Minute)
	result := domain.DeliveryResult{HTTPStatus: 503, ResponseSummary: "unavailable", ErrorCode: "delivery_timeout", Elapsed: 12 * time.Millisecond, Retryable: true}
	if err := h.store.FinalizeMerchantDepositCallbackFailure(context.Background(), task.ID, task.ClaimToken, attempt.ID, result, next, h.now); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, task.ID, "pending", 1, "", true)
	h.assertAttempt(t, attempt.ID, "failed", 503, "delivery_timeout", true)
	var storedNext time.Time
	var lastError string
	if err := h.db.QueryRow(`SELECT next_retry_at, last_error FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&storedNext, &lastError); err != nil || !storedNext.Equal(next) || lastError != "delivery_timeout" {
		t.Fatalf("failure retry fields: next=%s error=%s err=%v", storedNext, lastError, err)
	}

	dead := h.insertTask(t, "dead-letter")
	if _, err := h.db.Exec(`UPDATE merchant_deposit_callback_tasks SET retry_count=7 WHERE id=?`, dead.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now, h.now, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim dead letter task: %v %v", claimed, err)
	}
	deadAttempt, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), dead.ID, claimed[0].ClaimToken, h.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.FinalizeMerchantDepositCallbackFailure(context.Background(), dead.ID, claimed[0].ClaimToken, deadAttempt.ID, domain.DeliveryResult{ErrorCode: "permanent", Error: errors.New("permanent")}, next, h.now); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, dead.ID, "dead_letter", 8, "", true)
	h.assertAttempt(t, deadAttempt.ID, "failed", 0, "permanent", true)
}

func TestMariaDBFinalizeFailureRollback(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task, attempt := h.runningTask(t, "failure-rollback")
	h.failTaskUpdate(t)
	err := h.store.FinalizeMerchantDepositCallbackFailure(context.Background(), task.ID, task.ClaimToken, attempt.ID, domain.DeliveryResult{HTTPStatus: 500, ErrorCode: "transport"}, h.now.Add(time.Minute), h.now)
	if err == nil {
		t.Fatal("expected task update trigger failure")
	}
	h.assertTask(t, task.ID, "processing", 0, task.ClaimToken, false)
	h.assertAttempt(t, attempt.ID, "running", 0, "", false)
}

func TestMariaDBClaimOnlyRecovery(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task := h.claimTask(t, "claim-only")
	if err := h.store.RecoverStaleMerchantDepositCallbacks(context.Background(), h.now.Add(3*time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, task.ID, "pending", 1, "", true)
	var attempts, attemptCount int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM merchant_deposit_callback_attempts WHERE task_id=?`, task.ID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("claim-only attempts=%d err=%v", attempts, err)
	}
	if err := h.db.QueryRow(`SELECT attempt_count FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&attemptCount); err != nil || attemptCount != 0 {
		t.Fatalf("claim-only attempt_count=%d err=%v", attemptCount, err)
	}
	if err := h.store.RecoverStaleMerchantDepositCallbacks(context.Background(), h.now.Add(4*time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, task.ID, "pending", 1, "", true)
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now.Add(4*time.Minute), h.now, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ClaimToken == task.ClaimToken {
		t.Fatalf("reclaim: tasks=%+v err=%v", claimed, err)
	}
}

func TestMariaDBNonExpiredClaimAndRunningRecoveryUntouched(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	claimOnly := h.claimTask(t, "fresh-claim")
	_, running := h.runningTask(t, "fresh-running")
	if err := h.store.RecoverStaleMerchantDepositCallbacks(context.Background(), h.now.Add(time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, claimOnly.ID, "processing", 0, claimOnly.ClaimToken, false)
	h.assertAttempt(t, running.ID, "running", 0, "", false)
}

func TestMariaDBRunningAttemptRecovery(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task, attempt := h.runningTask(t, "running-recovery")
	recoverAt := h.now.Add(3 * time.Minute)
	if err := h.store.RecoverStaleMerchantDepositCallbacks(context.Background(), recoverAt, 10); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, task.ID, "pending", 1, "", true)
	h.assertAttempt(t, attempt.ID, "abandoned", 0, "worker_lease_expired", true)
	if err := h.store.FinalizeMerchantDepositCallbackSuccess(context.Background(), task.ID, task.ClaimToken, attempt.ID, domain.DeliveryResult{HTTPStatus: 200}, recoverAt); !errors.Is(err, ErrCallbackClaimLost) {
		t.Fatalf("stale finalize err=%v", err)
	}
	if err := h.store.RecoverStaleMerchantDepositCallbacks(context.Background(), recoverAt.Add(time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	h.assertTask(t, task.ID, "pending", 1, "", true)
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), recoverAt.Add(time.Minute), h.now, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("reclaim after recovery: %v %v", claimed, err)
	}
	next, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), task.ID, claimed[0].ClaimToken, recoverAt.Add(time.Minute))
	if err != nil || next.AttemptNo != 2 {
		t.Fatalf("new attempt after recovery=%+v err=%v", next, err)
	}
}

func TestMariaDBClaimConcurrencySingleTask(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task := h.insertTask(t, "claim-concurrency-single")
	results := h.concurrentClaims(t, h.now, 1, 2)
	if got := h.claimedIDs(results); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("claimed ids=%v", got)
	}
	var status string
	if err := h.db.QueryRow(`SELECT status FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&status); err != nil || status != "processing" {
		t.Fatalf("final task status=%s err=%v", status, err)
	}
	var token string
	var lease time.Time
	if err := h.db.QueryRow(`SELECT claim_token, claim_expires_at FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&token, &lease); err != nil || token == "" || !lease.Equal(h.now.Add(2*time.Minute)) {
		t.Fatalf("final claim token/lease tokenPresent=%v lease=%s err=%v", token != "", lease, err)
	}
}

func TestMariaDBClaimConcurrencyMultipleTasks(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	for i := 0; i < 4; i++ {
		h.insertTask(t, fmt.Sprintf("claim-concurrency-many-%d", i))
	}
	results := h.concurrentClaims(t, h.now, 2, 2)
	seen := map[int64]bool{}
	for _, result := range results {
		if len(result.tasks) != 2 {
			t.Fatalf("worker tasks=%v err=%v", result.tasks, result.err)
		}
		for _, task := range result.tasks {
			if seen[task.ID] {
				t.Fatalf("overlapping claim task id=%d", task.ID)
			}
			seen[task.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("claimed distinct tasks=%d", len(seen))
	}
	var processing, tokens int
	if err := h.db.QueryRow(`SELECT COUNT(*), COUNT(claim_token) FROM merchant_deposit_callback_tasks WHERE status='processing'`).Scan(&processing, &tokens); err != nil || processing != 4 || tokens != 4 {
		t.Fatalf("final multi-claim processing=%d tokens=%d err=%v", processing, tokens, err)
	}
}

func TestMariaDBClaimConcurrencyActiveAndExpiredLease(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	active := h.claimTask(t, "claim-concurrency-active")
	results := h.concurrentClaims(t, h.now.Add(time.Minute), 1, 2)
	if got := h.claimedIDs(results); len(got) != 0 {
		t.Fatalf("active lease was claimed: %v", got)
	}
	if err := h.store.RecoverStaleMerchantDepositCallbacks(context.Background(), h.now.Add(3*time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	results = h.concurrentClaims(t, h.now.Add(4*time.Minute), 1, 2)
	if got := h.claimedIDs(results); len(got) != 1 || got[0] != active.ID {
		t.Fatalf("recovered task claim ids=%v", got)
	}
}

func TestMariaDBBeginAttemptConcurrency(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task := h.claimTask(t, "begin-concurrency")
	results := h.concurrentBegins(t, task.ID, task.ClaimToken, h.now, 2)
	success := 0
	lost := 0
	for _, result := range results {
		if result.err == nil {
			success++
			if result.attempt.AttemptNo != 1 {
				t.Fatalf("attempt=%+v", result.attempt)
			}
		} else if errors.Is(result.err, ErrCallbackClaimLost) {
			lost++
		} else {
			t.Fatalf("unexpected begin error=%v", result.err)
		}
	}
	if success != 1 || lost != 1 {
		t.Fatalf("begin results success=%d lost=%d %+v", success, lost, results)
	}
	var attempts, running, count int
	var status string
	if err := h.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(status='running'),0) FROM merchant_deposit_callback_attempts WHERE task_id=?`, task.ID).Scan(&attempts, &running); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT attempt_count,status FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&count, &status); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || running != 1 || count != 1 || status != "processing" {
		t.Fatalf("final begin attempts=%d running=%d count=%d status=%s", attempts, running, count, status)
	}
}

func TestMariaDBAttemptNumberConcurrency(t *testing.T) {
	h := newCallbackLifecycleHarness(t, false)
	task, first := h.runningTask(t, "attempt-number-concurrency")
	next := h.now.Add(time.Minute)
	if err := h.store.FinalizeMerchantDepositCallbackFailure(context.Background(), task.ID, task.ClaimToken, first.ID, domain.DeliveryResult{Error: errors.New("retry"), ErrorCode: "retry", Retryable: true}, next, h.now); err != nil {
		t.Fatal(err)
	}
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), next, h.now, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("reclaim: %v %v", claimed, err)
	}
	results := h.concurrentBegins(t, task.ID, claimed[0].ClaimToken, next, 2)
	success, lost := 0, 0
	for _, result := range results {
		if result.err == nil {
			success++
			if result.attempt.AttemptNo != 2 {
				t.Fatalf("new attempt=%+v", result.attempt)
			}
		} else if errors.Is(result.err, ErrCallbackClaimLost) {
			lost++
		} else {
			t.Fatalf("unexpected err=%v", result.err)
		}
	}
	if success != 1 || lost != 1 {
		t.Fatalf("attempt #2 results success=%d lost=%d", success, lost)
	}
	rows, err := h.db.Query(`SELECT attempt_no FROM merchant_deposit_callback_attempts WHERE task_id=? ORDER BY attempt_no`, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var numbers []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		numbers = append(numbers, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := h.db.QueryRow(`SELECT attempt_count FROM merchant_deposit_callback_tasks WHERE id=?`, task.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(numbers) != "[1 2]" || count != 2 {
		t.Fatalf("attempt numbers=%v attempt_count=%d", numbers, count)
	}
}

type concurrentClaimResult struct {
	tasks []domain.MerchantDepositCallbackTask
	err   error
}
type concurrentBeginResult struct {
	attempt domain.MerchantDepositCallbackAttempt
	err     error
}

func (h *callbackLifecycleHarness) concurrentClaims(t *testing.T, now time.Time, limit, workers int) []concurrentClaimResult {
	t.Helper()
	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(workers)
	results := make(chan concurrentClaimResult, workers)
	for i := 0; i < workers; i++ {
		store, closeDB := h.independentStore(t)
		go func() {
			defer closeDB()
			ready.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tasks, err := store.ClaimDueMerchantDepositCallbackTasks(ctx, now, now, limit)
			results <- concurrentClaimResult{tasks, err}
		}()
	}
	ready.Wait()
	close(start)
	out := make([]concurrentClaimResult, 0, workers)
	for i := 0; i < workers; i++ {
		select {
		case result := <-results:
			out = append(out, result)
		case <-time.After(6 * time.Second):
			t.Fatal("claim concurrency timeout")
		}
	}
	for _, result := range out {
		if result.err != nil {
			t.Fatalf("claim worker error=%v", result.err)
		}
	}
	return out
}

func (h *callbackLifecycleHarness) concurrentBegins(t *testing.T, taskID int64, token string, now time.Time, workers int) []concurrentBeginResult {
	t.Helper()
	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(workers)
	results := make(chan concurrentBeginResult, workers)
	for i := 0; i < workers; i++ {
		store, closeDB := h.independentStore(t)
		go func() {
			defer closeDB()
			ready.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			attempt, err := store.BeginMerchantDepositCallbackAttempt(ctx, taskID, token, now)
			results <- concurrentBeginResult{attempt, err}
		}()
	}
	ready.Wait()
	close(start)
	out := make([]concurrentBeginResult, 0, workers)
	for i := 0; i < workers; i++ {
		select {
		case result := <-results:
			out = append(out, result)
		case <-time.After(6 * time.Second):
			t.Fatal("begin concurrency timeout")
		}
	}
	return out
}

func (h *callbackLifecycleHarness) independentStore(t *testing.T) (*MySQLDepositStore, func()) {
	t.Helper()
	db, err := sql.Open("mysql", h.dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return NewMySQLDepositStore(db), func() { _ = db.Close() }
}

func (h *callbackLifecycleHarness) claimedIDs(results []concurrentClaimResult) []int64 {
	var ids []int64
	for _, result := range results {
		for _, task := range result.tasks {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func (h *callbackLifecycleHarness) claimTask(t *testing.T, name string) domain.MerchantDepositCallbackTask {
	t.Helper()
	h.insertTask(t, name)
	claimed, err := h.store.ClaimDueMerchantDepositCallbackTasks(context.Background(), h.now, h.now, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim %s: tasks=%+v err=%v", name, claimed, err)
	}
	return claimed[0]
}

func (h *callbackLifecycleHarness) runningTask(t *testing.T, name string) (domain.MerchantDepositCallbackTask, domain.MerchantDepositCallbackAttempt) {
	t.Helper()
	task := h.claimTask(t, name)
	attempt, err := h.store.BeginMerchantDepositCallbackAttempt(context.Background(), task.ID, task.ClaimToken, h.now)
	if err != nil {
		t.Fatalf("begin %s: %v", name, err)
	}
	return task, attempt
}

// A disposable-database trigger fails only the second SQL statement in a
// finalize transaction.  It proves MariaDB rolls back the already-successful
// attempt update; no application code path or production configuration can
// enable this fixture.
func (h *callbackLifecycleHarness) failTaskUpdate(t *testing.T) {
	t.Helper()
	if _, err := h.db.Exec(`CREATE TRIGGER it_fail_callback_task_update BEFORE UPDATE ON merchant_deposit_callback_tasks FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'integration finalize task update failure'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.db.Exec(`DROP TRIGGER IF EXISTS it_fail_callback_task_update`) })
}

func (h *callbackLifecycleHarness) assertTask(t *testing.T, id int64, wantStatus string, wantRetries int, wantToken string, wantCleared bool) {
	t.Helper()
	var status, token string
	var retries int
	var lease sql.NullTime
	var lastError sql.NullString
	var next sql.NullTime
	if err := h.db.QueryRow(`SELECT status,retry_count,COALESCE(claim_token,''),claim_expires_at,last_error,next_retry_at FROM merchant_deposit_callback_tasks WHERE id=?`, id).Scan(&status, &retries, &token, &lease, &lastError, &next); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || retries != wantRetries || token != wantToken {
		t.Fatalf("task id=%d status=%s retries=%d tokenMatch=%v", id, status, retries, token == wantToken)
	}
	if wantCleared && (lease.Valid || token != "") {
		t.Fatalf("task id=%d claim not cleared lease=%v tokenPresent=%v", id, lease.Valid, token != "")
	}
}

func (h *callbackLifecycleHarness) assertAttempt(t *testing.T, id int64, wantStatus string, wantHTTP int, wantCode string, wantFinished bool) {
	t.Helper()
	var status, code string
	var httpStatus sql.NullInt64
	var finished sql.NullTime
	if err := h.db.QueryRow(`SELECT status,COALESCE(http_status,0),COALESCE(error_code,''),finished_at FROM merchant_deposit_callback_attempts WHERE id=?`, id).Scan(&status, &httpStatus, &code, &finished); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || int(httpStatus.Int64) != wantHTTP || code != wantCode || finished.Valid != wantFinished {
		t.Fatalf("attempt id=%d status=%s http=%d code=%s finished=%v", id, status, httpStatus.Int64, code, finished.Valid)
	}
}

type callbackLifecycleHarness struct {
	t               *testing.T
	db              *sql.DB
	dsn, migrations string
	store           *MySQLDepositStore
	now             time.Time
	prefix          string
	sequence        int
}

func newCallbackLifecycleHarness(t *testing.T, stopAtFour bool) *callbackLifecycleHarness {
	t.Helper()
	dsn := os.Getenv(lifecycleDSNEnv)
	if dsn == "" {
		t.Skip(lifecycleDSNEnv + " is not set")
	}
	migrations := os.Getenv(lifecycleMigrationsEnv)
	if migrations == "" {
		t.Skip(lifecycleMigrationsEnv + " is not set")
	}
	migrations, err := filepath.Abs(migrations)
	if err != nil {
		t.Fatalf("resolve %s: %v", lifecycleMigrationsEnv, err)
	}
	releaseIntegrationFixtureLock(t, dsn)
	prefix := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(t.Name(), "TestMariaDB"), "/", "-"))
	h := &callbackLifecycleHarness{t: t, dsn: dsn, migrations: migrations, now: time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC), prefix: prefix}
	if stopAtFour {
		h.migrateTo(t, 4)
	} else if err := Migrate(dsn, migrations); err != nil {
		t.Fatalf("production Migrate: %v", err)
	}
	h.db, err = sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.db.Close() })
	h.store = NewMySQLDepositStore(h.db)
	// The supplied database is disposable but shared by the test process.  Keep
	// each case independent without changing any production repository method.
	for _, statement := range []string{
		"DELETE FROM merchant_deposit_callback_attempts",
		"DELETE FROM merchant_deposit_callback_tasks",
		"DELETE FROM orders",
		"DELETE FROM merchants",
	} {
		if _, err := h.db.Exec(statement); err != nil {
			t.Fatalf("reset lifecycle fixture with %q: %v", statement, err)
		}
	}
	return h
}

func releaseIntegrationFixtureLock(t *testing.T, dsn string) {
	t.Helper()
	lockDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := lockDB.Conn(context.Background())
	if err != nil {
		_ = lockDB.Close()
		t.Fatal(err)
	}
	var acquired int
	if err := conn.QueryRowContext(context.Background(), `SELECT GET_LOCK('payment-service-integration-fixture', 30)`).Scan(&acquired); err != nil || acquired != 1 {
		_ = conn.Close()
		_ = lockDB.Close()
		t.Fatalf("acquire integration fixture lock: acquired=%d err=%v", acquired, err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DO RELEASE_LOCK('payment-service-integration-fixture')`)
		_ = conn.Close()
		_ = lockDB.Close()
	})
}
func (h *callbackLifecycleHarness) migrateTo(t *testing.T, version uint) {
	t.Helper()
	path, cleanup, err := isolatedMigrationPath(h.migrations)
	if err != nil {
		t.Fatalf("isolate production migration path %q: %v", h.migrations, err)
	}
	defer cleanup()
	m, err := migrate.New(migrationSourceURL(path), "mysql://"+h.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Migrate(version); err != nil {
		t.Fatalf("migrate to %d using migration path %q (isolated %q): %v", version, h.migrations, path, err)
	}
}
func (h *callbackLifecycleHarness) task(s string) domain.MerchantDepositCallbackTask {
	h.sequence++
	key := fmt.Sprintf("it:%s:%s:%d", h.prefix, s, h.sequence)
	return domain.MerchantDepositCallbackTask{MerchantID: h.merchant(), OrderID: h.order(), EventKey: key, CallbackURL: "https://callback.invalid/" + key, Payload: `{"event":"deposit.paid"}`, NextRetryAt: h.now}
}
func (h *callbackLifecycleHarness) insertTask(t *testing.T, s string) domain.MerchantDepositCallbackTask {
	t.Helper()
	task := h.task(s)
	got, _, err := h.store.EnsureMerchantDepositCallbackTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func (h *callbackLifecycleHarness) merchant() int64 {
	var id int64
	code := fmt.Sprintf("it-m-%s-%d", h.prefix, h.sequence)
	if err := h.db.QueryRow(`INSERT INTO merchants(code,name,api_key_hash) VALUES (?, 'integration', 'hash') RETURNING id`, code).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}
func (h *callbackLifecycleHarness) order() int64 {
	var id int64
	merchant := h.sequence
	if err := h.db.QueryRow(`SELECT id FROM merchants ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	var orderID int64
	no := fmt.Sprintf("it-order-%s-%d", h.prefix, merchant)
	if err := h.db.QueryRow(`INSERT INTO orders(merchant_id,order_no,merchant_order_no,amount_cents,currency,status,item_desc) VALUES (?, ?, ?, 100, 'TWD','pending','integration') RETURNING id`, id, no, no).Scan(&orderID); err != nil {
		h.t.Fatal(err)
	}
	return orderID
}
