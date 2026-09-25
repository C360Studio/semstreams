package agenticloop

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// A loop's terminal is counted where it becomes durable: the terminal owner
// (#1374). Before, each lane counted for itself ahead of the commit, and the
// lanes that commit a failure through the owner without a model response — the
// tool lane's handler-error branch, the approval lane's error-with-terminal
// branch and the approval-timeout sweeper's loop-deadline branch — counted
// nothing: `active_loops` stayed one too high for every such loop and
// `loops_failed_total` never saw it. A loop-deadline failure is `timeout` on
// every lane, the model lane included.
//
// getMetrics is a package singleton, so every observation here is a DELTA over
// a snapshot the test takes itself, never an absolute value. None of these
// tests is parallel, so no other test moves the counters inside the window.

// terminalCounts snapshots the loop-terminal series one test observes.
type terminalCounts struct {
	m *loopMetrics
	// failed is loops_failed_total by reason, for every reason a test reads.
	failed    map[string]float64
	completed float64
	active    float64
}

// terminalReasons is every reason label a test here reads: the ones a loop
// fails with in these fixtures, and the two a mis-classified timeout used to
// land on.
var terminalReasons = []string{
	"timeout", "handler_error", "unknown", "cancelled", "operator_abort", "max_iterations",
}

func snapshotTerminalCounts(m *loopMetrics) terminalCounts {
	counts := terminalCounts{m: m, failed: map[string]float64{}}
	for _, reason := range terminalReasons {
		counts.failed[reason] = testutil.ToFloat64(m.loopsFailed.WithLabelValues(reason))
	}
	counts.completed = testutil.ToFloat64(m.loopsCompleted)
	counts.active = testutil.ToFloat64(m.activeLoops)
	return counts
}

// requireOneTerminal asserts that exactly one terminal was counted since the
// snapshot — under reason for a failure, or as a completion when reason is "" —
// and that active_loops went down by exactly one. It is for a loop born in
// this process; a loop this process rebuilt from its record uses
// requireOneTerminalCounted.
func (before terminalCounts) requireOneTerminal(t *testing.T, reason string) {
	t.Helper()
	before.requireOneTerminalCounted(t, reason)
	after := snapshotTerminalCounts(before.m)
	require.Equal(t, -1.0, after.active-before.active, "active_loops must go down exactly once")
}

// requireOneTerminalCounted asserts the terminal counters only. A loop this
// process rebuilt from its record was never counted into active_loops here, so
// its terminal's decrement is the per-process drift recorded on #1242, not a
// property a test pins.
func (before terminalCounts) requireOneTerminalCounted(t *testing.T, reason string) {
	t.Helper()
	after := snapshotTerminalCounts(before.m)
	for _, label := range terminalReasons {
		want := 0.0
		if label == reason {
			want = 1
		}
		require.Equal(t, want, after.failed[label]-before.failed[label],
			"loops_failed_total{reason=%q} moved by the wrong amount", label)
	}
	wantCompleted := 0.0
	if reason == "" {
		wantCompleted = 1
	}
	require.Equal(t, wantCompleted, after.completed-before.completed, "loops_completed_total")
}

// requireNoTerminal asserts that nothing terminal was counted since the snapshot.
func (before terminalCounts) requireNoTerminal(t *testing.T) {
	t.Helper()
	after := snapshotTerminalCounts(before.m)
	for _, label := range terminalReasons {
		require.Zero(t, after.failed[label]-before.failed[label],
			"loops_failed_total{reason=%q} counted a terminal that is not durable", label)
	}
	require.Zero(t, after.completed-before.completed, "loops_completed_total")
	require.Zero(t, after.active-before.active, "active_loops moved for a terminal that is not durable")
}

// withLoopMetrics wires the singleton into the component and its handler, as
// the production constructor does.
func withLoopMetrics(c *Component) *loopMetrics {
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	c.handler.SetMetrics(c.metrics)
	return c.metrics
}

// markerFailure reads the failure the terminal owner saved in COMPLETE_<loopID>,
// which is the LoopFailedEvent the loop published.
func markerFailure(t *testing.T, bucket *recordingLoopBucket, loopID string) agentic.LoopFailedEvent {
	t.Helper()
	raw, ok := bucket.value(terminalMarkerKey(loopID))
	require.True(t, ok, "the failure did not pass through the terminal owner")
	var failure agentic.LoopFailedEvent
	require.NoError(t, json.Unmarshal(raw, &failure))
	return failure
}

// TestAToolLaneTimeoutCountsOneTimeoutFailure: the tool lane's handler-error
// branch (settleFailedToolResult) commits the timeout failure HandleToolResult
// returns with its error.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAToolLaneTimeoutCountsOneTimeoutFailure(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-tool-timeout", "general", "model", 3)
	require.NoError(t, err)
	callID, executionID := "call-timeout", "execution-timeout"
	handler.loopManager.TrackToolCall(executionID, loopID)
	require.NoError(t, handler.loopManager.AddPendingTool(loopID, callID))
	require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))
	c := releaseTestComponent(t, handler)
	m := withLoopMetrics(c)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	toolResult := &agentic.ToolResult{ExecutionID: executionID, CallID: callID, Name: "search", Content: "ran"}
	data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
	require.NoError(t, err)
	before := snapshotTerminalCounts(m)

	msg := &loopDeliveryOwnerMsg{data: data}
	result, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
	require.Equal(t, "timeout", markerFailure(t, bucket, loopID).Reason)
	before.requireOneTerminal(t, "timeout")
}

// TestAnApprovalLaneTimeoutCountsOneTimeoutFailure: the approval lane's
// error-with-terminal-result branch commits the loop's timeout failure.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAnApprovalLaneTimeoutCountsOneTimeoutFailure(t *testing.T) {
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		e.TimeoutAt = time.Now().Add(-time.Minute)
	})
	before := snapshotTerminalCounts(a.c.metrics)

	settled, err := a.deliver(t, a.answerOf(agentic.ApprovalDecisionApprove))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, settled)
	require.Equal(t, "timeout", markerFailure(t, a.bucket, coldApprovalLoopID).Reason)
	// Counters only: the loop is rebuilt from its record (active_loops drift, #1242).
	before.requireOneTerminalCounted(t, "timeout")
}

// TestTheApprovalSweepCountsOneTimeoutFailure: the approval-timeout sweeper's
// loop-deadline branch commits the loop's timeout failure.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTheApprovalSweepCountsOneTimeoutFailure(t *testing.T) {
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		e.TimeoutAt = time.Now().Add(-time.Minute)
		e.PendingApproval.RequestedAt = time.Now().Add(-2 * time.Hour)
	})
	rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), a.answer(agentic.ApprovalDecisionReject))
	require.NoError(t, err)
	require.True(t, rebuilt, "fixture: this process holds the gated loop and sweeps its deadline")
	before := snapshotTerminalCounts(a.c.metrics)

	a.c.sweepExpiredApprovals(t.Context())

	requireLoopTimedOut(t, a)
	require.Equal(t, "timeout", markerFailure(t, a.bucket, coldApprovalLoopID).Reason)
	// Counters only: the loop is rebuilt from its record (active_loops drift, #1242).
	before.requireOneTerminalCounted(t, "timeout")
}

// TestAModelLaneTimeoutIsATimeout: the model lane noticed the loop deadline in
// HandleModelResponse and failed the loop through handleLoopFailure under
// "handler_error", so watchers saw one cause under two reasons depending on
// which lane noticed it. It is "timeout" in the published failure and in
// loops_failed_total, as on every other lane.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAModelLaneTimeoutIsATimeout(t *testing.T) {
	c, bucket, loopID, _ := timedOutLoopWithASupersededRequest(t)
	m := withLoopMetrics(c)
	current := c.handler.loopManager.OutstandingRequest(loopID)
	before := snapshotTerminalCounts(m)

	msg := &loopDeliveryOwnerMsg{data: completionResponseBytes(t, current, "an answer past the deadline")}
	result, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), deliverylane.NewAdmission(nil, nil))

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
	require.Equal(t, agentic.LoopStateFailed, persistedLoop(t, bucket, loopID).State)
	require.Equal(t, "timeout", markerFailure(t, bucket, loopID).Reason,
		"the published failure must name the loop deadline, not a generic handler error")
	before.requireOneTerminal(t, "timeout")
}

// TestAHandleLoopFailureCountsOnlyTheCommittedTerminal: handleLoopFailure used
// to count its failure BEFORE the commit, so a commit that did not land was
// counted, and the redelivery that did land it was counted again. The failure
// is counted once, by the commit that makes it durable.
func TestAHandleLoopFailureCountsOnlyTheCommittedTerminal(t *testing.T) {
	t.Run("a committed failure is counted once under its reason", func(t *testing.T) {
		h := NewMessageHandler(DefaultConfig())
		c := releaseTestComponent(t, h)
		m := withLoopMetrics(c)
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		loopID := populatedLoop(t, h)
		seedLoopRecord(t, c, loopID)
		before := snapshotTerminalCounts(m)

		require.NoError(t, c.handleLoopFailure(t.Context(), loopID, "operator_abort", errors.New("boom")))

		require.Equal(t, "operator_abort", markerFailure(t, bucket, loopID).Reason)
		before.requireOneTerminal(t, "operator_abort")
	})

	t.Run("a failure whose commit did not land is not counted", func(t *testing.T) {
		h := NewMessageHandler(DefaultConfig())
		c := releaseTestComponent(t, h)
		m := withLoopMetrics(c)
		bucket := &recordingLoopBucket{}
		c.loopsBucket = bucket
		loopID := populatedLoop(t, h)
		seedLoopRecord(t, c, loopID)
		bucket.arm(errKVUnavailable)
		before := snapshotTerminalCounts(m)

		require.Error(t, c.handleLoopFailure(t.Context(), loopID, "operator_abort", errors.New("boom")))

		before.requireNoTerminal(t)
	})
}

// TestARetriedTerminalCommitIsCountedOnce is the exactly-once half across a
// retry: a lost compare-and-swap counts nothing and releases the loop, and the
// redelivery that adopts the durable marker counts the terminal once.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARetriedTerminalCommitIsCountedOnce(t *testing.T) {
	a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
		e.TimeoutAt = time.Now().Add(-time.Minute)
	})
	response := a.answer(agentic.ApprovalDecisionApprove)
	rebuilt, err := a.c.settleApprovalResponseWithoutLoop(t.Context(), response)
	require.NoError(t, err)
	require.True(t, rebuilt, "fixture: this process holds the expired loop")
	// A foreign writer moves the record past the revision this process read.
	foreign, ok := a.bucket.value(coldApprovalLoopID)
	require.True(t, ok)
	_, err = a.bucket.Put(t.Context(), coldApprovalLoopID, foreign)
	require.NoError(t, err)
	before := snapshotTerminalCounts(a.c.metrics)

	settled, err := a.deliver(t, response)
	require.ErrorIs(t, err, natsclient.ErrKVRevisionMismatch)
	require.Equal(t, natsclient.DeliveryDecisionRetry, settled)
	before.requireNoTerminal(t)

	settled, err = a.deliver(t, response)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, settled)
	requireLoopTimedOut(t, a)
	// Counters only: the loop is rebuilt from its record (active_loops drift, #1242).
	before.requireOneTerminalCounted(t, "timeout")
}

// TestACancelIsCountedOnceAtItsCommit: the cancel lane counted its terminal
// ahead of the commit too. It is counted by the commit, under "cancelled".
func TestACancelIsCountedOnceAtItsCommit(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	m := withLoopMetrics(c)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	loopID := populatedLoop(t, h)
	seedLoopRecord(t, c, loopID)
	before := snapshotTerminalCounts(m)

	require.NoError(t, c.handleCancelSignal(t.Context(), agentic.UserSignal{LoopID: loopID, UserID: "operator"}))

	require.Equal(t, agentic.LoopStateCancelled, persistedLoop(t, bucket, loopID).State)
	before.requireOneTerminal(t, "cancelled")
}

// TestARetriedCancelIsCountedOnceAtItsAdoption: a terminal loop record has two
// writers, commitTerminal and adoptDurableCancel's writeRecordCancelled, and
// both count. The first cancel creates the marker and loses its record write
// to a foreign writer, counting nothing and releasing the loop; the
// redelivered cancel finds no held loop, adopts the marker, writes the record
// cancelled and counts it once.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestARetriedCancelIsCountedOnceAtItsAdoption(t *testing.T) {
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	m := withLoopMetrics(c)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	loopID := populatedLoop(t, h)
	seedLoopRecord(t, c, loopID)
	foreign, ok := bucket.value(loopID)
	require.True(t, ok)
	_, err := bucket.Put(t.Context(), loopID, foreign)
	require.NoError(t, err)
	signal := agentic.UserSignal{LoopID: loopID, UserID: "operator"}
	before := snapshotTerminalCounts(m)

	require.ErrorIs(t, c.handleCancelSignal(t.Context(), signal), natsclient.ErrKVRevisionMismatch)
	before.requireNoTerminal(t)
	_, heldErr := h.GetLoop(loopID)
	require.Error(t, heldErr, "fixture: the lost compare-and-swap released the loop")

	require.NoError(t, c.handleCancelSignal(t.Context(), signal))

	require.Equal(t, agentic.LoopStateCancelled, persistedLoop(t, bucket, loopID).State)
	before.requireOneTerminal(t, "cancelled")
}

// TestAModelLaneCompletionCountsOneCompletion: a completion is counted by the
// terminal owner as loops_completed_total, and under no failure reason.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestAModelLaneCompletionCountsOneCompletion(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-completes", "general", "model", 5)
	require.NoError(t, err)
	requestID := handler.loopManager.GenerateRequestID(loopID)
	handler.loopManager.TrackRequest(requestID, loopID)
	require.NoError(t, handler.loopManager.SetPublishedRequest(loopID, requestID))
	c := releaseTestComponent(t, handler)
	m := withLoopMetrics(c)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	before := snapshotTerminalCounts(m)

	msg := &loopDeliveryOwnerMsg{data: completionResponseBytes(t, requestID, "done")}
	result, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "agent.response", c.handleResponseMessage), deliverylane.NewAdmission(nil, nil))

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
	require.Equal(t, agentic.LoopStateComplete, persistedLoop(t, bucket, loopID).State)
	before.requireOneTerminal(t, "")
}

// TestAToolResultThatExhaustsTheBudgetCountsOneMaxIterations: the tool lane's
// success path, where handleToolsComplete fails the loop on its iteration
// budget, is counted under the event's reason "max_iterations". The branch this
// replaced counted every non-budget terminal on that path as "unknown".
//
// spec: agentic-loop / Iteration exhaustion publishes one uniform reason
func TestAToolResultThatExhaustsTheBudgetCountsOneMaxIterations(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-exhausts", "general", "model", 1)
	require.NoError(t, err)
	callID := "call-exhausts"
	_, err = handler.HandleModelResponse(t.Context(), loopID, agentic.AgentResponse{
		RequestID: "request-exhausts", Status: "tool_call",
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{ID: callID, Name: "search"}}},
	})
	require.NoError(t, err)
	// Spend the one iteration, so the increment the arriving result drives is
	// the one past the budget.
	require.NoError(t, handler.loopManager.IncrementIteration(loopID))
	c := releaseTestComponent(t, handler)
	m := withLoopMetrics(c)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	seedLoopRecord(t, c, loopID)
	executionID := dispatchedExecutionID(t, handler.loopManager, loopID)
	toolResult := &agentic.ToolResult{ExecutionID: executionID, CallID: callID, Name: "search", Content: "ran"}
	data, err := json.Marshal(message.NewBaseMessage(toolResult.Schema(), toolResult, "test"))
	require.NoError(t, err)
	before := snapshotTerminalCounts(m)

	msg := &loopDeliveryOwnerMsg{data: data}
	result, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
	require.Equal(t, "max_iterations", markerFailure(t, bucket, loopID).Reason)
	before.requireOneTerminal(t, "max_iterations")
}
