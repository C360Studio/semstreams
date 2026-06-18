package service

import (
	"context"
	"testing"
	"time"
)

// TestOwnershipService_NilRegistry_DisabledPath verifies the R1 infallible-Start
// discipline: when no registry is attached (ownership disabled this boot) Start
// must return nil and the service must report StatusRunning (intentionally-idle,
// not crashed). Stop must also be clean.
func TestOwnershipService_NilRegistry_DisabledPath(t *testing.T) {
	t.Parallel()
	svc := NewOwnershipService(nil, nil, nil)

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
	svc := NewOwnershipService(nil, nil, nil)

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
