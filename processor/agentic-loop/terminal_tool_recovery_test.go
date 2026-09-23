package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/require"
)

// TestTerminalLoopAcknowledgesAToolResultWithoutEffect is owner ruling Q7 on
// #1330: a loop that has settled cannot apply anything, so a tool result
// redelivered to it is acknowledged with no effect, counted, and named in an
// audit line — rather than retried to MaxDeliver or applied to a finished
// conversation.
//
// It is also docket OQ5's placement: the check sits at component entry, and
// LoopEntity.TransitionTo's same-state nil is left alone, because that nil is
// a legitimate no-op for the other callers of a state machine this lane does
// not own.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTerminalLoopAcknowledgesAToolResultWithoutEffect(t *testing.T) {
	t.Run("warm: the loop is terminal in memory and its record is complete", func(t *testing.T) {
		c, h, loopID := warmToolLane(t)
		require.NoError(t, h.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
		// The record matches the terminal loop this process holds: the
		// carrier wrote it when the loop settled, under compare-and-swap.
		require.NoError(t, c.persistLoopState(t.Context(), loopID))
		require.Equal(t, agentic.LoopStateComplete, decodeRecord(t, c, loopID).State)
		bucket := c.loopsBucket.(*recordingLoopBucket)
		bucket.resetWritten()

		// The duplicate of the very result that ended the loop, redelivered.
		current := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		result := routedToolResult(t, h, loopID, current, "call-stop")
		result.StopLoop = true
		dropped := toolDropDelta(c, "terminal_unproven")

		msg, delivered := deliverToolResult(t, c, result)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a terminal loop can apply nothing, so retrying this delivery only burns MaxDeliver")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, float64(1), dropped(),
			"the drop is deliberate, so it is counted where an operator can see it")
		require.Empty(t, bucket.written(),
			"an effect-free acknowledgement writes no record — the terminal one it already has stands")

		entity, err := h.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		require.Empty(t, entity.PendingToolResults,
			"the duplicate entered a settled loop's applied set")
	})

	t.Run("cold: the loop is gone from memory and its record is terminal", func(t *testing.T) {
		const loopID = "c1e8b2f3-3d5e-4b6c-9f70-2e3d4c5b6f71"
		c := evidenceComponent(t, "")
		c.metrics = getMetrics(metric.NewMetricsRegistry())
		entity := agentic.NewLoopEntity(loopID, "task-terminal-cold", "general", "model", 10)
		entity.State = agentic.LoopStateComplete
		entity.PublishedRequestID = looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
		writeRecord(t, c, entity)
		bucket := c.loopsBucket.(*recordingLoopBucket)
		bucket.resetWritten()
		dropped := toolDropDelta(c, "stale_execution")

		msg, delivered := deliverToolResult(t, c, agentic.ToolResult{
			CallID: loopID + ":tool:1", Name: "decide", Content: "done", LoopID: loopID,
			RequestID: looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String(),
			StopLoop:  true,
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, float64(1), dropped())
		require.Empty(t, bucket.written(), "a settled loop's record is not rewritten by a redelivery")
	})
}

// writeRecord puts an entity straight into the bucket, which is how a process
// meets a loop it never held.
func writeRecord(t *testing.T, c *Component, entity agentic.LoopEntity) {
	t.Helper()
	data, err := json.Marshal(entity)
	require.NoError(t, err)
	_, err = c.loopsBucket.Put(t.Context(), entity.ID, data)
	require.NoError(t, err)
}
