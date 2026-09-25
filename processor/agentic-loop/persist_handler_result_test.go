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

func (b failingLoopBucket) Create(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error) {
	return 0, b.err
}

func (b failingLoopBucket) Update(context.Context, string, []byte, uint64) (uint64, error) {
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

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
// A terminal whose publication failed is not committed, and since #1362
// (review H1) the terminal owner releases the loop on any failed commit: the
// error is returned AND nothing of the loop is held for a later delivery.
func TestPersistHandlerResultReleasesTheLoopWhenItsTerminalPublicationFails(t *testing.T) {
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
	require.Error(t, err, "a failed terminal commit kept the loop's transient state")
}

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestRequiredLoopStatePersistenceReturnsErrors(t *testing.T) {
	want := errors.New("kv unavailable")
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-persist", "general", "model", 3)
	require.NoError(t, err)
	c := &Component{handler: handler, loopsBucket: failingLoopBucket{err: want}}
	// The record's revision is what a birth or a cold read leaves behind. Seed
	// it directly so this test observes the WRITE failing, not the missing
	// observation that would refuse the write before it reached the bucket.
	c.rememberLoopRevision(loopID, 7)

	err = c.persistLoopState(t.Context(), loopID)
	require.ErrorIs(t, err, want)
	// The terminal marker is created, not Put (#1362): a Create that fails for
	// any reason but an existing key reports that failure.
	_, _, err = c.createTerminalMarker(t.Context(), loopID,
		terminalOutcome{completed: &agentic.LoopCompletedEvent{LoopID: loopID}})
	require.ErrorIs(t, err, want)
	_, _, err = c.createTerminalMarker(t.Context(), loopID,
		terminalOutcome{cancelled: &agentic.LoopCancelledEvent{LoopID: loopID}})
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

// TestAGateIsWrittenBeforeItIsPublished is task 1.6's gate branch at the
// carrier seam (docket OQ8): a result that CREATES an approval gate is written
// before its ApprovalPendingEvent is published — the shape decides, no lane
// can ask otherwise (#1376) — so an unpublishable client still leaves the gate
// durable.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAGateIsWrittenBeforeItIsPublished(t *testing.T) {
	c, bucket, loopID := carrierLoop(t)
	entity, err := c.handler.GetLoop(loopID)
	require.NoError(t, err)
	require.NoError(t, entity.BeginAwaitingApproval(
		"call-gate", "delete_rule", nil, agentic.ApprovalRequiredPrefix+"needs a human", time.Hour, ""))
	require.NoError(t, c.handler.UpdateLoop(entity))
	c.natsClient = unpublishableClient(t)

	err = c.persistHandlerResult(t.Context(), HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateAwaitingApproval,
		PublishedMessages: []PublishedMessage{{
			Subject: "agent.approval_pending." + loopID,
			Data:    []byte(`{"gate":true}`),
		}},
	})

	require.Error(t, err, "the unconnected publish must fail so the order is observable")
	require.Equal(t, []string{loopID}, bucket.written(),
		"the gate must be durable before its event is visible")
}

// TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged is task 1.6's
// conditional test, written first as ruled (#1362 issuecomment-5799118983,
// OQ-A), and kept as the recorded proof of the branch that shipped.
//
// The question: were a gate to take the uniform publish → Update order, a
// crash between its ApprovalPendingEvent and its record would leave the event
// visible and the record still running at the gate's request, with no gate on
// it. The human answers; the answer reaches a replacement. Would the approval
// lane's cold branch RETRY that answer until the gate became durable?
//
// It would not, and the test was first written asserting that it would and
// observed to fail ("An error is expected but got nil"): design § 5.5 step 1
// acknowledges an answer whose record is not awaiting_approval, because that
// is also the shape of an answer whose gate was already consumed (W3), and
// the record cannot tell the two apart. So the uniform order would lose the
// human's answer, and a gate keeps write → publish (variant A of the delta):
// the gate is durable before any human can see it, and no answer can outrun
// it. Adding an ahead-of-gate Retry arm is new scope and an owner question
// (OQ-A), not built.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovalAnswerThatOutrunsItsGateIsAcknowledged(t *testing.T) {
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		// The gate's write never landed: the record is as the tool lane's
		// dispatch left it, running at R with nothing applied.
		e.State = agentic.LoopStateExecuting
		e.StateBeforeApproval = ""
		e.PendingApproval = nil
		e.PendingToolResults = nil
	})

	decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision,
		"step 1 acknowledges an answer whose record is not awaiting approval: the answer is gone, "+
			"which is why a gate must be written before it is published")
	require.Empty(t, a.bucket.written(), "the acknowledged answer applied nothing")
	require.False(t, a.held())
}
