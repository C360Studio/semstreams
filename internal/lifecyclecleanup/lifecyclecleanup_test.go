package lifecyclecleanup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type contextKey string

func TestFailedStartRollbackTimeoutIsFrameworkOwned(t *testing.T) {
	if failedStartRollbackTimeout != 5*time.Second {
		t.Fatalf("failedStartRollbackTimeout = %v, want 5s", failedStartRollbackTimeout)
	}
}

func TestRollbackFailedStartRejectsNilParentBeforeCallback(t *testing.T) {
	called := false
	err := RollbackFailedStart(nil, func(context.Context) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("RollbackFailedStart(nil, callback) returned nil")
	}
	if called {
		t.Fatal("rollback callback ran with a nil parent")
	}
}

func TestRollbackFailedStartNilRollback(t *testing.T) {
	if err := RollbackFailedStart(t.Context(), nil); err != nil {
		t.Fatalf("RollbackFailedStart(ctx, nil) = %v, want nil", err)
	}
}

func TestRollbackFailedStartDetachesCancellationPreservesValuesAndBoundsWork(t *testing.T) {
	const key contextKey = "owner"
	tests := []struct {
		name   string
		parent func() context.Context
	}{
		{name: "canceled", parent: func() context.Context {
			parent, cancel := context.WithCancel(context.WithValue(t.Context(), key, "research"))
			cancel()
			return parent
		}},
		{name: "expired deadline", parent: func() context.Context {
			parent, cancel := context.WithDeadline(context.WithValue(t.Context(), key, "research"), time.Unix(1, 0))
			t.Cleanup(cancel)
			return parent
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			err := RollbackFailedStart(test.parent(), func(ctx context.Context) error {
				called = true
				if got := ctx.Value(key); got != "research" {
					t.Fatalf("rollback context value = %v, want research", got)
				}
				if err := ctx.Err(); err != nil {
					t.Fatalf("rollback context inherited parent completion: %v", err)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("rollback context has no deadline")
				}
				return nil
			})
			if err != nil {
				t.Fatalf("RollbackFailedStart() = %v", err)
			}
			if !called {
				t.Fatal("rollback callback did not run synchronously")
			}
		})
	}
}

func TestRollbackFailedStartJoinsCallbackAndExpiryErrors(t *testing.T) {
	callbackErr := errors.New("rollback failed")
	err := rollbackFailedStart(t.Context(), time.Nanosecond, func(ctx context.Context) error {
		<-ctx.Done()
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("rollback error = %v, want callback error", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rollback error = %v, want deadline exceeded", err)
	}
}
