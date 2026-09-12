package agenticloop

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type failingLoopBucket struct {
	jetstream.KeyValue
	err error
}

type finalMarkerFailBucket struct {
	jetstream.KeyValue
	loopID string
	values map[string][]byte
	err    error
}

func (b *finalMarkerFailBucket) Put(_ context.Context, key string, value []byte) (uint64, error) {
	if key == b.loopID {
		return 0, b.err
	}
	b.values[key] = append([]byte(nil), value...)
	return 1, nil
}

func (b failingLoopBucket) Put(context.Context, string, []byte) (uint64, error) {
	return 0, b.err
}

func (b failingLoopBucket) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, jetstream.ErrKeyNotFound
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
func TestPersistHandlerResultReturnsPublicationFailureAndDiscardsSpeculativeTerminalState(t *testing.T) {
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
	require.Error(t, err, "failed terminal attempt retained speculative process state")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTerminalLoopEntityIsFinalAppliedMarker(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-final-marker", "general", "model", 3)
	require.NoError(t, err)
	_, err = handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)
	require.NoError(t, handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
	require.NoError(t, handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, "done", ""))
	bucket := &finalMarkerFailBucket{
		loopID: loopID, values: make(map[string][]byte), err: errors.New("final marker unavailable"),
	}
	c := &Component{handler: handler, loopsBucket: bucket, logger: slog.Default()}
	completion := &agentic.LoopCompletedEvent{
		LoopID: loopID, TaskID: "task-final-marker", Outcome: agentic.OutcomeSuccess,
		Role: "general", Model: "model", Result: "done",
	}

	err = c.persistHandlerResult(t.Context(), HandlerResult{
		LoopID: loopID, State: agentic.LoopStateComplete, CompletionState: completion,
	})

	require.ErrorIs(t, err, bucket.err)
	require.Contains(t, bucket.values, "COMPLETE_"+loopID,
		"settlement-required effects did not run before the final marker")
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "failed final marker retained speculative terminal process state")
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

// spec: agentic-loop / Observed audit loss MUST be readable from the loop entity as a classified condition
func TestCompletionGraphWriteFailureRemainsNonblocking(t *testing.T) {
	failures := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_graph_evidence_failures_total"}, []string{"state", "reason"})
	c := &Component{
		logger:  slog.Default(),
		metrics: &loopMetrics{graphEvidenceFailures: failures},
		graphWriter: &graphWriter{
			natsClient: &natsclient.Client{},
			platform:   types.PlatformMeta{Org: "acme", Platform: "ops"},
			logger:     slog.Default(),
		},
	}

	err := c.stampLoopCompletionWithBudget(t.Context(), "loop-1", &agentic.LoopCompletedEvent{
		LoopID: "loop-1", Role: "general", Model: "model",
	})

	require.NoError(t, err)
	require.Equal(t, float64(1), testutil.ToFloat64(failures.WithLabelValues("complete", "write_error")))
}

// spec: agentic-loop / Observed audit loss MUST be readable from the loop entity as a classified condition
func TestFailureGraphWriteFailureRemainsNonblocking(t *testing.T) {
	failures := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_graph_evidence_failures_total"}, []string{"state", "reason"})
	c := &Component{
		logger:  slog.Default(),
		metrics: &loopMetrics{graphEvidenceFailures: failures},
		graphWriter: &graphWriter{
			natsClient: &natsclient.Client{},
			platform:   types.PlatformMeta{Org: "acme", Platform: "ops"},
			logger:     slog.Default(),
		},
	}

	err := c.stampLoopFailureWithBudget(t.Context(), "loop-1", &agentic.LoopFailedEvent{
		LoopID: "loop-1", Role: "general", Model: "model", Reason: "provider_failure",
	})

	require.NoError(t, err)
	require.Equal(t, float64(1), testutil.ToFloat64(failures.WithLabelValues("failure", "write_error")))
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestRequiredSyntheticGraphWriteFailureReturnsError(t *testing.T) {
	c := &Component{
		logger: slog.Default(),
		graphWriter: &graphWriter{
			natsClient: &natsclient.Client{},
			platform:   types.PlatformMeta{Org: "acme", Platform: "ops"},
			logger:     slog.Default(),
		},
	}

	err := c.stampSyntheticDecideWithBudget(t.Context(), &SyntheticDecideRequest{
		LoopID: "loop-1", Reason: "done",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "synthetic decide graph stamp")
}

// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestFailureLoopEntityIsFinalAppliedMarker(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-failure-marker", "general", "model", 3)
	require.NoError(t, err)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	bucket := &finalMarkerFailBucket{
		loopID: loopID, values: make(map[string][]byte), err: errors.New("final marker unavailable"),
	}
	c := &Component{handler: handler, loopsBucket: bucket, logger: slog.Default()}

	err = c.handleLoopFailure(t.Context(), loopID, entity, "provider_failure", errors.New("provider unavailable"))

	require.ErrorIs(t, err, bucket.err)
	require.Contains(t, bucket.values, "COMPLETE_"+loopID,
		"failure completion did not commit before the final marker")
	_, lookupErr := handler.GetLoop(loopID)
	require.Error(t, lookupErr, "failed final marker retained speculative failure state")
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestImpossibleFailureTransitionIsQuarantined(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-impossible-transition", "general", "model", 3)
	require.NoError(t, err)
	require.NoError(t, handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	c := &Component{handler: handler, logger: slog.Default()}

	err = c.handleLoopFailure(t.Context(), loopID, entity, "handler_error", errors.New("late failure"))

	require.Error(t, err)
	require.True(t, errs.IsFatal(err), "impossible transition must map to Quarantine, not Retry")
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

// TestGraphWritePublishBudget_IsReasonable guards the cooperative deadline
// against zero or a value so large that cancellation is signalled too late.
// It does not claim a hard return-time cap: runWithBudget joins the dependency,
// which must honor the supplied context. Tighten/widen with the const doc.
func TestGraphWritePublishBudget_IsReasonable(t *testing.T) {
	if graphWritePublishBudget < 100*time.Millisecond {
		t.Errorf("graphWritePublishBudget too tight (%v); healthy graph-gateway will trip the timeout under normal load", graphWritePublishBudget)
	}
	if graphWritePublishBudget > 10*time.Second {
		t.Errorf("graphWritePublishBudget too wide (%v); cooperative cancellation would be signalled too late", graphWritePublishBudget)
	}
}
