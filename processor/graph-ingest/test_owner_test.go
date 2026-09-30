package graphingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type graphIngestTestDrain struct {
	called atomic.Int32
	drain  func(context.Context) error
}

func (d *graphIngestTestDrain) Drain(ctx context.Context) error {
	d.called.Add(1)
	return d.drain(ctx)
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphIngestTestOwnerControlledStopPreservesAuthority(t *testing.T) {
	opCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	var startCtx context.Context
	drain := &graphIngestTestDrain{drain: func(stopCtx context.Context) error {
		if _, finite := stopCtx.Deadline(); !finite {
			return errors.New("Stop received no finite deadline")
		}
		if err := startCtx.Err(); err != nil {
			return errors.New("accepted Start authority ended before Stop")
		}
		return nil
	}}
	component := &Component{logger: slog.Default(), lifecycleUsed: true, running: true,
		subscriptions: []graphIngestCoreSubscription{drain}}
	owner := newGraphIngestTestOwner(component)
	startCtx = owner.startContext(opCtx)
	if err := owner.stop(opCtx); err != nil {
		t.Fatalf("controlled Stop: %v", err)
	}
	if err := startCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start authority after Stop = %v, want canceled", err)
	}
	if err := opCtx.Err(); err != nil {
		t.Fatalf("operation authority ended before assertions: %v", err)
	}
	owner.finish(opCtx, t)
	if got := drain.called.Load(); got != 1 {
		t.Fatalf("concrete Drain attempts = %d, want 1", got)
	}
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphIngestTestOwnerFailedAttemptIsNotRetried(t *testing.T) {
	opCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	want := errors.New("subscription drain failed")
	drain := &graphIngestTestDrain{drain: func(context.Context) error { return want }}
	component := &Component{logger: slog.Default(), lifecycleUsed: true, cleanupPending: true,
		subscriptions: []graphIngestCoreSubscription{drain}}
	owner := newGraphIngestTestOwner(component)
	if err := owner.stop(opCtx); !errors.Is(err, want) {
		t.Fatalf("concrete Stop error = %v, want %v", err, want)
	}
	owner.finish(opCtx, t)
	if got := drain.called.Load(); got != 1 {
		t.Fatalf("failed-start Drain attempts = %d, want 1", got)
	}
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphIngestTestOwnerCanceledOperationKeepsFreshTerminalAuthority(t *testing.T) {
	operationCtx, cancelOperation := context.WithCancel(t.Context())
	want := errors.New("core subscription cleanup failed")
	drain := &graphIngestTestDrain{drain: func(stopCtx context.Context) error {
		if stopCtx.Err() != nil {
			return errors.New("fresh Stop context was canceled with operation authority")
		}
		if _, finite := stopCtx.Deadline(); !finite {
			return errors.New("fresh Stop context has no deadline")
		}
		return want
	}}
	component := &Component{logger: slog.Default(), lifecycleUsed: true, running: true,
		subscriptions: []graphIngestCoreSubscription{drain}}
	owner := newGraphIngestTestOwner(component)
	owner.startContext(operationCtx)
	cancelOperation()
	err := owner.stop(operationCtx)
	if !errors.Is(err, want) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled-operation Stop error = %v, want concrete and operation causes", err)
	}
	if got := drain.called.Load(); got != 1 {
		t.Fatalf("concrete Stop attempts = %d, want 1", got)
	}
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphIngestTestOwnerWaitsForObservedClosure(t *testing.T) {
	operationCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	consumer := &graphIngestLifecycleConsumeContext{
		closed: make(chan struct{}), drainSeen: make(chan struct{}),
	}
	component := &Component{logger: slog.Default(), lifecycleUsed: true, running: true,
		consumers: []graphIngestConsumerBinding{{handle: consumer}}}
	owner := newGraphIngestTestOwner(component)
	startCtx := owner.startContext(operationCtx)
	stopDone := make(chan struct{})
	var stopErr error
	go func() {
		stopErr = owner.stop(operationCtx)
		close(stopDone)
	}()
	var releaseOnce sync.Once
	releaseClosed := func() { releaseOnce.Do(func() { close(consumer.closed) }) }
	t.Cleanup(func() {
		releaseClosed()
		joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancelJoin()
		select {
		case <-stopDone:
		case <-joinCtx.Done():
			t.Errorf("incomplete cleanup: concrete Stop did not join after native Closed release: %v", joinCtx.Err())
		}
	})
	select {
	case <-consumer.drainSeen:
	case <-operationCtx.Done():
		t.Fatalf("native drain was not entered: %v", operationCtx.Err())
	}
	select {
	case <-stopDone:
		t.Fatalf("Stop returned before owned Closed observation: %v", stopErr)
	default:
	}
	if err := startCtx.Err(); err != nil {
		t.Fatalf("Start authority ended while native close was pending: %v", err)
	}
	releaseClosed()
	select {
	case <-stopDone:
		if stopErr != nil {
			t.Fatalf("Stop after observed Closed: %v", stopErr)
		}
	case <-operationCtx.Done():
		t.Fatalf("Stop did not return after owned closure: %v", operationCtx.Err())
	}
	if !errors.Is(startCtx.Err(), context.Canceled) {
		t.Fatalf("Start authority after joined Stop = %v, want canceled", startCtx.Err())
	}
}
