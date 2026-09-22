//go:build integration

package agenticloop

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// modelDropDelta captures the model-response drop counter for one reason
// before a delivery and reports how far it moved once that delivery settled.
// The counter set is a process-wide singleton, so an absolute read would pass
// or fail on what other tests in this binary happened to count first.
func modelDropDelta(c *Component, reason string) func() float64 {
	read := func() float64 {
		return testutil.ToFloat64(c.metrics.modelResponsesDropped.WithLabelValues(reason))
	}
	before := read()
	return func() float64 { return read() - before }
}

// compactingConfig lowers the compaction threshold so a loop reaches it with
// one block of context instead of seventy thousand tokens of filler. The
// threshold is what decides whether a length-truncated response may self-heal;
// moving it changes how much context the test has to build, not which branch
// the response takes.
func compactingConfig() Config {
	config := DefaultConfig()
	config.Context.CompactThreshold = 0.10
	return config
}

// fillContextAboveCompactThreshold puts enough conversation in the loop's
// context for a length-truncated response to be worth retrying.
func fillContextAboveCompactThreshold(t *testing.T, h *MessageHandler, loopID string) {
	t.Helper()
	cm := h.GetContextManager(loopID)
	require.NotNil(t, cm, "no context manager for loop %s", loopID)
	// Roughly four characters per token; one block of ~16K tokens clears a 10%
	// threshold on the default 128K limit and leaves the compactor something
	// to evict.
	require.NoError(t, cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role:    "user",
		Content: strings.Repeat("recovered context ", 16000*4/len("recovered context ")),
	}))
	require.GreaterOrEqual(t, cm.Utilization(), h.config.Context.CompactThreshold,
		"the self-heal branch is only reachable above the compaction threshold")
}

// TestModelResponseRedeliveredToAReplacementProcess is task 4.2's response
// lane over a real broker, in the same two crash windows as the tool lane.
//
// W4 exists here only on the truncation-retry path, because that is the one
// place a model response mints a request of its own: the self-heal re-asks the
// same iteration under the next retry ordinal. The ordinal is what makes it
// worth a real broker — R and R' differ in their LAST component, so a
// classification that compared anything coarser than the full identity would
// read them as the same request and re-apply a response the loop moved past.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestModelResponseRedeliveredToAReplacementProcess(t *testing.T) {
	client := newLoopNATS(t)

	t.Run("W4: the truncation retry is retained and the record never learned its name", func(t *testing.T) {
		predecessor, handler := startLoopProcess(t, client, compactingConfig())
		loopID, firstRequest := bornLoop(t, predecessor, handler, "task-response-w4")
		requestSubject := "agent.request." + loopID
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))
		fillContextAboveCompactThreshold(t, handler, loopID)

		truncated := agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusLengthTruncated,
			FinishReason: agentic.FinishReasonLength,
			Message:      agentic.ChatMessage{Role: "assistant", Content: "partial output"},
			TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
		}

		// From here the predecessor is dying: it compacts, publishes the retry
		// and never records the name it published under.
		predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
		_, died := deliverResponse(t, predecessor, truncated)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, died.Decision(),
			"a record write of unknown durability is not an acknowledgement")

		retryRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 1}.String()
		require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
			"the self-heal re-asks the same iteration under the next retry ordinal")
		crashed := loopRecordOf(t, predecessor, loopID)
		require.Equal(t, firstRequest, crashed.entity.PublishedRequestID)
		require.Equal(t, 0, crashed.entity.Iterations,
			"a within-iteration retry does not advance the loop")

		replacement, _ := startLoopProcess(t, client, compactingConfig())
		superseded := modelDropDelta(replacement, "superseded_request")
		stale := modelDropDelta(replacement, "stale_request_id")

		msg, delivered := deliverResponse(t, replacement, truncated)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"after adoption the response answers an older request than the record, and nobody is owed it")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, float64(1), superseded())
		require.Zero(t, stale(), "the loop is live; this is a superseded response, not a missing loop")

		adopted := loopRecordOf(t, replacement, loopID)
		require.Equal(t, retryRequest, adopted.entity.PublishedRequestID,
			"the record must name the retry the stream retains before anything is classified against it")
		require.Equal(t, 0, adopted.entity.Iterations,
			"adopting a retry ordinal must not advance the iteration it retries")
		require.False(t, adopted.entity.State.IsTerminal())
		require.Equal(t, uint64(2), messagesOn(t, client, requestSubject),
			"adoption must not put a second copy of the retry on the stream")
	})

	t.Run("W2: the dispatch landed and the record never learned it", func(t *testing.T) {
		predecessor, handler := startLoopProcess(t, client, DefaultConfig())
		loopID, firstRequest := bornLoop(t, predecessor, handler, "task-response-w2")
		requestSubject := "agent.request." + loopID

		// A tool_call response mints no request: its effect is the dispatch.
		toolCall := agentic.AgentResponse{
			RequestID:    firstRequest,
			Status:       agentic.StatusToolCall,
			FinishReason: "tool_calls",
			Message: agentic.ChatMessage{
				Role:      "assistant",
				ToolCalls: []agentic.ToolCall{{ID: "call-response-w2", Name: "response_w2_tool"}},
			},
		}

		predecessor.loopsBucket = crashedBeforeRecordUpdate{KeyValue: predecessor.loopsBucket}
		_, died := deliverResponse(t, predecessor, toolCall)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, died.Decision())

		executeSubject := "tool.execute.response_w2_tool"
		require.Equal(t, uint64(1), messagesOn(t, client, executeSubject),
			"the dispatch went out before the record write was attempted")
		crashed := loopRecordOf(t, predecessor, loopID)
		require.Equal(t, firstRequest, crashed.entity.PublishedRequestID)
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject),
			"a tool_call response mints no request")

		// Nothing newer than the record is on the stream, so step 0 has nothing
		// to adopt and the response is still the loop's current one. Before L4a
		// that response was owed to a process that no longer exists: Retry, to
		// MaxDeliver, then the dead-letter. The replacement now rebuilds the
		// loop from the record and its retained request — the response IS the
		// delivery, so there is no second read to do — and applies it.
		replacement, replacementHandler := startLoopProcess(t, client, DefaultConfig())
		superseded := modelDropDelta(replacement, "superseded_request")
		stale := modelDropDelta(replacement, "stale_request_id")

		msg, delivered := deliverResponse(t, replacement, toolCall)

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a response the rebuilt loop applied is settled, not owed to a process that will never exist")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Zero(t, superseded())
		require.Zero(t, stale())

		rebuilt, err := replacementHandler.loopManager.GetLoop(loopID)
		require.NoError(t, err, "the replacement must HOLD the loop it rebuilt")
		require.False(t, rebuilt.State.IsTerminal())

		after := loopRecordOf(t, replacement, loopID)
		require.Greater(t, after.revision, crashed.revision,
			"the dispatch the crash lost is durable only if the rebuilt holder can write the record")
		require.Equal(t, firstRequest, after.entity.PublishedRequestID,
			"a tool_call response mints no request, so the record still names the first one")
		require.Equal(t, uint64(1), messagesOn(t, client, requestSubject))

		// The re-applied response dispatches the batch again, under the SAME
		// execution identity: it is derived from the request and the call
		// rather than minted (L2), so agentic-tools' TOOL_CALL_OUTCOMES sees a
		// replay of one execution, not two executions of one call.
		require.Equal(t, uint64(2), messagesOn(t, client, executeSubject),
			"the rebuilt loop must dispatch the batch its response asked for")
		execution := deriveToolExecutionID(firstRequest, "call-response-w2", 1)
		routed, held := replacementHandler.loopManager.GetLoopForToolCall(execution)
		require.True(t, held,
			"the re-dispatched call must carry the identity its first dispatch derived")
		require.Equal(t, loopID, routed)
	})
}
