package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDepositWorkerLogsPollErrorAndContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time, 2)
	ticks <- time.Now()
	ticks <- time.Now()
	service := &depositLoopServiceStub{retryErrors: []error{errors.New("secret=do-not-log signature=do-not-log body=do-not-log token=do-not-log dsn=do-not-log"), nil}, calls: make(chan struct{}, 2)}
	logger := &workerLogBuffer{}
	done := make(chan struct{})
	go func() {
		runDepositLoopsWithTicks(ctx, service, alwaysLease{}, "worker-test", time.Minute, ticks, make(chan time.Time), logger)
		close(done)
	}()
	for range 2 {
		select {
		case <-service.calls:
		case <-time.After(time.Second):
			t.Fatal("worker did not continue to next poll")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after context cancellation")
	}
	logs := logger.String()
	for _, want := range []string{"component=deposit_callback_worker", "operation=retry_deposit_callbacks", "error_class=retry_deposit_callbacks_failed", "error_type=*errors.errorString", "operation=start", "operation=stop"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing log field %q: %s", want, logs)
		}
	}
	for _, forbidden := range []string{"secret=do-not-log", "signature=do-not-log", "body=do-not-log", "token=do-not-log", "dsn=do-not-log"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("sensitive error content leaked: %s", logs)
		}
	}
}

func TestDepositWorkerEmptyPollIsLowNoise(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	service := &depositLoopServiceStub{calls: make(chan struct{}, 1)}
	logger := &workerLogBuffer{}
	done := make(chan struct{})
	go func() {
		runDepositLoopsWithTicks(ctx, service, alwaysLease{}, "worker-test", time.Minute, ticks, make(chan time.Time), logger)
		close(done)
	}()
	select {
	case <-service.calls:
	case <-time.After(time.Second):
		t.Fatal("worker did not poll")
	}
	cancel()
	<-done
	logs := logger.String()
	if strings.Contains(logs, "operation=retry_deposit_callbacks") || strings.Contains(logs, "operation=delivery_finalized") {
		t.Fatalf("empty poll logged work activity: %s", logs)
	}
	if strings.Count(logs, "component=deposit_callback_worker") != 2 {
		t.Fatalf("empty poll should only log start/stop: %s", logs)
	}
}

type depositLoopServiceStub struct {
	retryErrors []error
	calls       chan struct{}
	mu          sync.Mutex
	index       int
}

func (s *depositLoopServiceStub) RetryDepositCallbacks(context.Context, int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.index < len(s.retryErrors) {
		err = s.retryErrors[s.index]
	}
	s.index++
	s.calls <- struct{}{}
	return err
}
func (*depositLoopServiceStub) ExpireDueDeposits(context.Context, int) error { return nil }

type alwaysLease struct{}

func (alwaysLease) TryAcquire(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

type workerLogBuffer struct {
	mu    sync.Mutex
	lines []string
}

func (b *workerLogBuffer) Printf(format string, args ...any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, strings.TrimSpace(formatLog(format, args...)))
}
func (b *workerLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}
func formatLog(format string, args ...any) string { return fmt.Sprintf(format, args...) }
