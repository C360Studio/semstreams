package agenticloop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type failingLoopBucket struct {
	jetstream.KeyValue
	err error
}

func (b failingLoopBucket) Put(context.Context, string, []byte) (uint64, error) {
	return 0, b.err
}

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

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestPersistHandlerResultReturnsPublicationFailureBeforeTerminalRelease(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID := "publish-failure-loop"
	_, err := handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)
	c := &Component{handler: handler, natsClient: &natsclient.Client{}}

	err = c.persistHandlerResult(t.Context(), HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateComplete,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.complete." + loopID,
			Data:    []byte(`{"complete":true}`),
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "publish result")
	_, err = handler.trajectoryManager.getTrajectory(loopID)
	require.NoError(t, err, "failed required publication released terminal transient state")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestRequiredLoopStatePersistenceReturnsErrors(t *testing.T) {
	want := errors.New("kv unavailable")
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-persist", "general", "model", 3)
	require.NoError(t, err)
	c := &Component{handler: handler, loopsBucket: failingLoopBucket{err: want}}

	err = c.persistLoopState(t.Context(), loopID)
	require.ErrorIs(t, err, want)
	err = c.persistCompletionState(t.Context(), loopID, &agentic.LoopCompletedEvent{LoopID: loopID})
	require.ErrorIs(t, err, want)
	err = c.persistCancellationState(t.Context(), loopID, &agentic.LoopCancelledEvent{LoopID: loopID})
	require.ErrorIs(t, err, want)
}

// TestRunWithBudget_ReturnsTimedOutTrueWhenFnExceedsBudget asserts the
// degraded-graph-gateway path: when fn exceeds the budget,
// its bounded context is cancelled and cooperative work joins before
// runWithBudget reports timedOut=true.
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
		t.Errorf("expected joined work to observe cancellation when budget expired")
	}
}

// TestRunWithBudget_ParentContextCancellationPropagates asserts that a
// caller-side ctx cancellation (e.g. component shutdown) reaches fn.
// runWithBudget's bctx is derived from ctx, so cancelling ctx cancels
// bctx, which cancels fn. timedOut is true (since bctx.Done fired),
// matching the contract: any reason for not completing returns true.
//
// fnObserved proves the joined work saw the exact derived cancellation.
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
func TestRunWithBudgetWaitsForCooperativeWorkToJoinAfterCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan bool, 1)
	go func() {
		returned <- runWithBudget(context.Background(), 10*time.Millisecond, func(bctx context.Context) {
			close(started)
			<-bctx.Done()
			close(cancelObserved)
			<-release
		})
	}()
	<-started
	<-cancelObserved

	returnedBeforeJoin := false
	select {
	case <-returned:
		returnedBeforeJoin = true
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if returnedBeforeJoin {
		t.Fatal("runWithBudget returned while delivery-derived graph work remained live")
	}
	select {
	case timedOut := <-returned:
		if !timedOut {
			t.Error("expected timedOut=true after budget cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("runWithBudget did not return after joined work was released")
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
