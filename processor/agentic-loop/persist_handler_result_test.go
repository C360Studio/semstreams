package agenticloop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunWithBudget_ReturnsCompletedFalseWhenFnReturnsFast asserts the
// happy path: when fn returns well within the budget, runWithBudget
// reports timedOut=false. This is the case persistHandlerResult relies
// on for the publish-after-stamp ordering — the publish proceeds with
// the graph triple guaranteed visible.
func TestRunWithBudget_ReturnsCompletedFalseWhenFnReturnsFast(t *testing.T) {
	var ran atomic.Bool
	timedOut := runWithBudget(context.Background(), 100*time.Millisecond, func(_ context.Context) {
		ran.Store(true)
	})
	if timedOut {
		t.Errorf("expected timedOut=false for fast fn, got true")
	}
	if !ran.Load() {
		t.Errorf("expected fn to have run")
	}
}

// TestRunWithBudget_ReturnsTimedOutTrueWhenFnExceedsBudget asserts that the
// child context expires and cooperative work joins before timeout is reported.
func TestRunWithBudget_ReturnsTimedOutTrueWhenFnExceedsBudget(t *testing.T) {
	var bctxCancelled atomic.Bool
	timedOut := runWithBudget(context.Background(), 20*time.Millisecond, func(bctx context.Context) {
		select {
		case <-bctx.Done():
			bctxCancelled.Store(true)
		case <-time.After(500 * time.Millisecond):
			t.Errorf("fn ran past 500ms — bctx should have been cancelled at 20ms")
		}
	})
	if !timedOut {
		t.Errorf("expected timedOut=true when fn exceeds budget")
	}
	if !bctxCancelled.Load() {
		t.Errorf("expected fn's bctx to have been cancelled when budget expired")
	}
}

// TestRunWithBudget_ParentContextCancellationPropagates asserts that a
// caller-side ctx cancellation (e.g. component shutdown) reaches fn.
// runWithBudget's bctx is derived from ctx, so cancelling ctx cancels
// bctx, which cancels fn. timedOut is true (since bctx.Done fired),
// matching the contract: any reason for not completing returns true.
//
// fnObserved proves the synchronous work saw the exact derived cancellation.
func TestRunWithBudget_ParentContextCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before runWithBudget so bctx is born cancelled
	fnObserved := make(chan error, 1)
	timedOut := runWithBudget(ctx, 1*time.Second, func(bctx context.Context) {
		<-bctx.Done()
		fnObserved <- bctx.Err()
	})
	if !timedOut {
		t.Errorf("expected timedOut=true on parent-ctx cancellation")
	}
	select {
	case err := <-fnObserved:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected fn to see context.Canceled, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Errorf("fn never observed bctx cancellation")
	}
}

// spec: agentic-loop / Delivery work joins before settlement
// scenario: Delivery work exceeds its budget
func TestRunWithBudget_DoesNotReturnBeforeCooperativeWorkJoins(t *testing.T) {
	observedCancellation := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan bool, 1)
	go func() {
		returned <- runWithBudget(t.Context(), 10*time.Millisecond, func(workCtx context.Context) {
			<-workCtx.Done()
			close(observedCancellation)
			<-release
		})
	}()

	<-observedCancellation
	select {
	case <-returned:
		t.Fatal("runWithBudget returned before cooperative child joined")
	default:
	}
	close(release)
	if timedOut := <-returned; !timedOut {
		t.Fatal("expected timedOut=true after the work budget expired")
	}
}

// TestGraphWritePublishBudget_IsReasonable is a guard against
// accidentally setting the budget to zero or to a value so large it
// defeats the bounded-wait property. 2s is the chosen value (see
// const doc); this test fires if someone changes the constant without
// thinking. Tighten/widen here when changing the constant.
func TestGraphWritePublishBudget_IsReasonable(t *testing.T) {
	if graphWritePublishBudget < 100*time.Millisecond {
		t.Errorf("graphWritePublishBudget too tight (%v); healthy graph-gateway will trip the timeout under normal load", graphWritePublishBudget)
	}
	if graphWritePublishBudget > 10*time.Second {
		t.Errorf("graphWritePublishBudget too wide (%v); defeats the bounded-wait property — publish can be delayed by a degraded graph-gateway", graphWritePublishBudget)
	}
}
