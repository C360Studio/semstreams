package service

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// TestOwnershipService_NilRegistry_DisabledPath verifies the R1 infallible-Start
// discipline: when no registry is attached (ownership disabled this boot) Start
// must return nil and the service must report StatusRunning (intentionally-idle,
// not crashed). Stop must also be clean.
func TestOwnershipService_NilRegistry_DisabledPath(t *testing.T) {
	t.Parallel()
	svc := NewOwnershipService(nil, nil, nil, nil)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start with nil registry must return nil (R1 infallible), got: %v", err)
	}

	if svc.Status() != StatusRunning {
		t.Errorf("Status after Start = %v, want StatusRunning", svc.Status())
	}

	if err := svc.Stop(time.Second); err != nil {
		t.Errorf("Stop on disabled path must be clean, got: %v", err)
	}
}

// TestOwnershipService_ReentrancyGuard verifies that a double-Start returns an
// error and does not launch duplicate goroutines. This is a BUG-CLASS guard, not
// an R1 soft failure.
func TestOwnershipService_ReentrancyGuard(t *testing.T) {
	t.Parallel()
	svc := NewOwnershipService(nil, nil, nil, nil)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("first Start must succeed: %v", err)
	}
	defer svc.Stop(time.Second) //nolint:errcheck

	// Second Start while already running must return an error.
	err := svc.Start(context.Background())
	if err == nil {
		t.Fatal("double-Start must return an error (re-entrancy guard)")
	}
}

// TestWireOwnershipShutdown_CancelsCtxAndJoinsCleanly proves the ADR-058 step-2
// drift-killer: the returned cleanup cancels the heartbeat ctx AND returns
// promptly when no registry is attached (WaitOwnership is a no-op — R3 symmetric
// independence). A hang would mean the join blocked on a goroutine never spawned.
func TestWireOwnershipShutdown_CancelsCtxAndJoinsCleanly(t *testing.T) {
	t.Parallel()
	mgr := lifecycle.NewManager(nil, slog.Default()) // no AttachOwnership → WaitOwnership no-op

	hbCtx, shutdown := WireOwnershipShutdown(context.Background(), mgr)
	if hbCtx.Err() != nil {
		t.Fatalf("hbCtx must be live before shutdown, got: %v", hbCtx.Err())
	}

	done := make(chan struct{})
	go func() { shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hung — WaitOwnership must be a no-op when no registry is attached (R3)")
	}

	if hbCtx.Err() == nil {
		t.Error("shutdown must cancel the heartbeat ctx (signal the Manager-internal heartbeater)")
	}
}

// TestWireOwnershipShutdown_ParentCancelPropagates proves the heartbeat ctx is
// derived from the caller's ctx — so the heartbeater stops on parent cancel too,
// not only via the returned cleanup (preserves the pre-refactor WithCancel(ctx)).
func TestWireOwnershipShutdown_ParentCancelPropagates(t *testing.T) {
	t.Parallel()
	mgr := lifecycle.NewManager(nil, slog.Default())
	parent, cancelParent := context.WithCancel(context.Background())

	hbCtx, shutdown := WireOwnershipShutdown(parent, mgr)
	defer shutdown()

	if hbCtx.Err() != nil {
		t.Fatalf("hbCtx must be live before parent cancel, got: %v", hbCtx.Err())
	}
	cancelParent()
	select {
	case <-hbCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("hbCtx must cancel when its parent cancels (derived context)")
	}
}
