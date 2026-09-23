package agenticloop

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// toolDrops reads one reason label of the tool-result drop counter. A drop
// nobody can see is a drop an operator cannot act on, so every acknowledged
// tool result below is asserted on the counter as well as on the settlement.
//
// The counter set is a process-wide singleton, so every assertion below is a
// DELTA across one delivery. An absolute read would pass or fail on what other
// tests in this package happened to count first.
func toolDrops(c *Component, reason string) float64 {
	return testutil.ToFloat64(c.metrics.toolResultsDropped.WithLabelValues(reason))
}

// toolDropDelta captures the counter before a delivery and reports how far one
// reason moved once it has settled.
func toolDropDelta(c *Component, reason string) func() float64 {
	before := toolDrops(c, reason)
	return func() float64 { return toolDrops(c, reason) - before }
}

// warmToolLane builds the state a tool result is delivered into on a process
// that HOLDS the loop: a loop whose record names request 2 of iteration 1, one
// execution routed to it, and a client that cannot publish — so a delivery
// that reaches the handler and tries to advance the loop shows up as a failed
// settlement rather than passing for an acknowledgement.
func warmToolLane(t *testing.T) (*Component, *MessageHandler, string) {
	t.Helper()
	h := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, h)
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	c.loopsBucket = &recordingLoopBucket{}
	c.natsClient = unpublishableClient(t)
	c.requestEvidence = stubEvidenceReader{}

	loopID, err := h.loopManager.CreateLoop("task-tool-classification", "general", "model", 5)
	require.NoError(t, err)
	require.NoError(t, h.loopManager.IncrementIteration(loopID))
	require.NoError(t, h.loopManager.SetPublishedRequest(loopID,
		looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()))
	seedLoopRecord(t, c, loopID)
	return c, h, loopID
}

// routedToolResult builds a result correlated the way agentic-tools correlates
// one — request, execution and ordinal echoed from the call — and routes its
// execution to the loop, which is what makes the delivery warm.
func routedToolResult(t *testing.T, h *MessageHandler, loopID, requestID, callID string) agentic.ToolResult {
	t.Helper()
	executionID := deriveToolExecutionID(requestID, callID, 1)
	h.loopManager.TrackToolCall(executionID, loopID)
	return agentic.ToolResult{
		CallID: callID, Name: "search", Content: "result", LoopID: loopID,
		RequestID: requestID, ExecutionID: executionID, CallOrdinal: 1,
	}
}

func deliverToolResult(t *testing.T, c *Component, result agentic.ToolResult) (*loopDeliveryOwnerMsg, natsclient.DeliveryResult) {
	t.Helper()
	msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, &result)}
	delivered, admitted := deliverylane.Consume(t.Context(), msg,
		heartbeatPolicyForTest(t, "tool.result", c.handleToolResultMessage), deliverylane.NewAdmission(nil, nil))
	require.True(t, admitted)
	return msg, delivered
}

// TestRedeliveredToolResultIsClassifiedBeforeTheHandlerTouchesIt is the warm
// half of the tool lane's classification (#1330, design § 5.3).
//
// The damage it prevents is specific and silent: HandleToolResult stores a
// result into the applied set and acts on StopLoop BEFORE its own guards, so a
// result the loop has already moved past used to be stored and re-sent to the
// model as a duplicate tool message in the next turn's request. The
// classification runs at component entry, where nothing has been touched yet.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestRedeliveredToolResultIsClassifiedBeforeTheHandlerTouchesIt(t *testing.T) {
	t.Run("a result for an older request is acknowledged without effect", func(t *testing.T) {
		c, h, loopID := warmToolLane(t)
		older := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
		result := routedToolResult(t, h, loopID, older, "call-older")
		dropped := toolDropDelta(c, "older_request")

		msg, delivered := deliverToolResult(t, c, result)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a result the loop already applied cannot be applied again and must not be retried")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, float64(1), dropped())

		entity, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Empty(t, entity.PendingToolResults,
			"the stale result entered the applied set and will be re-sent to the model next turn")
		require.Empty(t, c.loopsBucket.(*recordingLoopBucket).written(),
			"an effect-free acknowledgement writes no record")
	})

	t.Run("a result for a request the record does not yet name is retried", func(t *testing.T) {
		c, h, loopID := warmToolLane(t)
		ahead := looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String()
		result := routedToolResult(t, h, loopID, ahead, "call-ahead")
		older := toolDropDelta(c, "older_request")
		terminal := toolDropDelta(c, "terminal_unproven")

		msg, delivered := deliverToolResult(t, c, result)

		require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision(),
			"a result that outran the record update is not yet observable, not stale")
		require.Equal(t, int32(1), msg.naks.Load())
		require.Zero(t, msg.acks.Load()+msg.terms.Load())
		require.Zero(t, older()+terminal(), "a retried result is not a dropped one")

		entity, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Empty(t, entity.PendingToolResults)
	})

	t.Run("a result naming another loop's request is quarantined", func(t *testing.T) {
		c, h, loopID := warmToolLane(t)
		foreign := looprequest.ID{
			LoopID: "9f8e7d6c-5b4a-4938-8271-6a5b4c3d2e10", Iteration: 2, Retry: 0,
		}.String()
		result := routedToolResult(t, h, loopID, foreign, "call-foreign")

		msg, delivered := deliverToolResult(t, c, result)

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, delivered.Decision(),
			"no later delivery makes another loop's request name this loop's work")
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(),
			"a quarantined delivery is settled by its owner, not by this callback")

		entity, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Empty(t, entity.PendingToolResults)
	})

	t.Run("a result for the current request is applied", func(t *testing.T) {
		c, h, loopID := warmToolLane(t)
		current := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		// Two calls in this batch, so applying one leaves the batch
		// incomplete: the loop stores the result and mints nothing, which is
		// what keeps this arm about classification rather than about the
		// advance.
		require.NoError(t, h.loopManager.AddPendingTool(loopID, "call-current"))
		require.NoError(t, h.loopManager.AddPendingTool(loopID, "call-sibling"))
		result := routedToolResult(t, h, loopID, current, "call-current")
		older := toolDropDelta(c, "older_request")
		terminal := toolDropDelta(c, "terminal_unproven")

		msg, delivered := deliverToolResult(t, c, result)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
		require.Zero(t, older()+terminal(),
			"the classification refused a result the loop was waiting for")

		entity, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Contains(t, entity.PendingToolResults, result.ExecutionID,
			"a current result must reach the applied set — otherwise the loop never advances")
	})
}

// TestColdToolResultIsClassifiedAgainstTheAdoptedRecord is the cold half: the
// process holds no loop at all, so the record is the only authority. Step 0
// adopts the newest retained request first, and the result is then ordered
// against THAT — which is what turns the W4 crash window from a redelivery
// storm into one acknowledgement.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestColdToolResultIsClassifiedAgainstTheAdoptedRecord(t *testing.T) {
	const loopID = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
	retained := looprequest.ID{LoopID: loopID, Iteration: 3, Retry: 0}.String()

	t.Run("a result for the request the adoption superseded is acknowledged", func(t *testing.T) {
		c := evidenceComponent(t, retained)
		c.metrics = getMetrics(metric.NewMetricsRegistry())
		coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
			e.Iterations = 1
		})

		// The last result of request 2's batch, redelivered to a replacement
		// process: its predecessor published request 3 and died before the
		// record update.
		dropped := toolDropDelta(c, "older_request")
		msg, delivered := deliverToolResult(t, c, agentic.ToolResult{
			CallID: loopID + ":tool:1", Name: "search", Content: "result", LoopID: loopID,
			RequestID: looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String(),
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"after adoption the result is older than the record, so no process can apply it")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, float64(1), dropped())
		require.Equal(t, retained, decodeRecord(t, c, loopID).PublishedRequestID,
			"the classification ran without adopting first")
	})

	t.Run("a result naming a request of another loop is quarantined", func(t *testing.T) {
		c := evidenceComponent(t, retained)
		c.metrics = getMetrics(metric.NewMetricsRegistry())
		coldRecord(t, c, loopID, func(e *agentic.LoopEntity) {
			e.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
			e.Iterations = 1
		})

		msg, delivered := deliverToolResult(t, c, agentic.ToolResult{
			CallID: loopID + ":tool:1", Name: "search", Content: "result", LoopID: loopID,
			RequestID: "9f8e7d6c-5b4a-4938-8271-6a5b4c3d2e10:req:2:0",
		})

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, delivered.Decision())
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())
	})
}

// TestASkippedClassificationSaysSoInTheLog: the classification at component
// entry is skipped when the loop was released between the routing lookup and
// the read, and continuing there is safe — HandleToolResult answers the race
// exactly as it did before the check existed. Safe is not the same as silent.
// An operator reading a result applied to a loop nothing guarded needs the
// line saying the guard did not run.
//
// spec: agentic-loop / A loop absent from process memory is settled from its record
func TestASkippedClassificationSaysSoInTheLog(t *testing.T) {
	const loopID = "6d5e4f3a-2b1c-4098-8877-665544332211"
	const executionID = "execution-orphan-route"

	var logged bytes.Buffer
	handler := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, handler)
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	handler.SetMetrics(c.metrics)
	c.logger = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
	degraded := degradationDelta(c, "tool_result_classification")
	// A routing entry with no loop behind it: exactly what a release between
	// the lookup and the read leaves.
	handler.loopManager.TrackToolCall(executionID, loopID)

	_, delivered := deliverToolResult(t, c, agentic.ToolResult{
		CallID: "call-orphan", ExecutionID: executionID, Name: "search",
		Content: "the executor really did this work", LoopID: loopID,
	})
	require.NotEqual(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
		"the fixture must reach the handler, which is what the skipped guard let through")

	require.Contains(t, logged.String(), "Tool result not classified against the loop",
		"the delivery skipped its classification without saying so")
	require.Contains(t, logged.String(), executionID)
	require.Equal(t, float64(1), degraded(),
		"a log line is not something an operator can alert on; a declared degrade carries both")
}

// degradationDelta reports how far recovery_degradations_total has moved for
// one site since it was called.
func degradationDelta(c *Component, site string) func() float64 {
	before := testutil.ToFloat64(c.metrics.recoveryDegradations.WithLabelValues(site))
	return func() float64 {
		return testutil.ToFloat64(c.metrics.recoveryDegradations.WithLabelValues(site)) - before
	}
}
