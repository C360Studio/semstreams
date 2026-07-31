package natsclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// errPortNotFound is the shape Docker returns when a container's host-side
// mapping is not (yet) resolvable — the failure gh#736 saw at 0.47s.
var errPortNotFound = errors.New(`port "4222" not found`)

// failNTimes returns a portResolver that fails n times, then succeeds.
func failNTimes(n int, port string) (portResolver, *int) {
	calls := 0
	return func(context.Context, string) (string, error) {
		calls++
		if calls <= n {
			return "", errPortNotFound
		}
		return port, nil
	}, &calls
}

// TestResolveMappedPort_RetriesTransientFailure is the regression guard for
// gh#736's fast-fail class.
//
// Before this fix, MappedPort was called ONCE. A mapping that was momentarily
// unresolvable under Docker API pressure failed the whole test instantly, which
// is why the failure appeared at 0.47s rather than as the 120-180s timeout that
// issue documents.
func TestResolveMappedPort_RetriesTransientFailure(t *testing.T) {
	t.Parallel()

	resolve, calls := failNTimes(3, "54321")

	got, err := resolveMappedPort(context.Background(), resolve, "4222")
	if err != nil {
		t.Fatalf("expected the transient failures to be retried through, got: %v", err)
	}
	if got != "54321" {
		t.Errorf("port = %q, want %q", got, "54321")
	}
	// 3 failures + 1 success. Asserting the count — not just the outcome —
	// so a change that accidentally stops retrying cannot pass by having the
	// first call happen to succeed.
	if *calls != 4 {
		t.Errorf("resolver called %d times, want 4 (3 transient failures then success)", *calls)
	}
}

// TestResolveMappedPort_SucceedsFirstTryWithoutSleeping guards the common path:
// the retry must cost nothing when there is nothing to retry. A budget-shaped
// delay on every container start would be a real regression across 337 call
// sites.
func TestResolveMappedPort_SucceedsFirstTryWithoutSleeping(t *testing.T) {
	t.Parallel()

	resolve, calls := failNTimes(0, "4222")

	start := time.Now()
	got, err := resolveMappedPort(context.Background(), resolve, "4222")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "4222" {
		t.Errorf("port = %q, want %q", got, "4222")
	}
	if *calls != 1 {
		t.Errorf("resolver called %d times, want exactly 1", *calls)
	}
	if elapsed >= mappedPortRetryInterval {
		t.Errorf("first-try success slept %s; the retry must not add latency to the happy path", elapsed)
	}
}

// TestResolveMappedPort_GivesUpAndStaysDiagnosable covers the case the budget
// exists for: the container is GONE, not slow. The retry must not hide that —
// it must surface the underlying error, the attempt count, and the budget, so a
// dead container is distinguishable from a slow mapping.
func TestResolveMappedPort_GivesUpAndStaysDiagnosable(t *testing.T) {
	t.Parallel()

	// Always fails. Bounded by a context rather than the 10s budget so the
	// test stays fast while exercising the same give-up path.
	alwaysFails := func(context.Context, string) (string, error) { return "", errPortNotFound }
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := resolveMappedPort(ctx, alwaysFails, "4222")
	if err == nil {
		t.Fatal("expected an error when the mapping never resolves")
	}
	// The ORIGINAL cause must survive, or a dead container reads as a generic
	// timeout and the next person re-debugs it from scratch.
	if !errors.Is(err, errPortNotFound) {
		t.Errorf("underlying lookup error was swallowed: %v", err)
	}
	if !strings.Contains(err.Error(), "attempt(s)") {
		t.Errorf("error should report how many attempts were made, got: %v", err)
	}
}

// TestResolveMappedPort_StopsOnContextCancellation ensures the retry cannot
// outlive its caller — a test that has already failed or timed out must not be
// held open for the remainder of the budget.
func TestResolveMappedPort_StopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	alwaysFails := func(context.Context, string) (string, error) { return "", errPortNotFound }
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first attempt

	start := time.Now()
	_, err := resolveMappedPort(ctx, alwaysFails, "4222")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error on a cancelled context")
	}
	if elapsed > time.Second {
		t.Errorf("took %s to notice cancellation; the retry must not outlive its caller", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation should be reported as such, got: %v", err)
	}
}
