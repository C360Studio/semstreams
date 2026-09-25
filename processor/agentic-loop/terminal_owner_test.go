package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// The single terminal owner (#1362, design § 5.7, D41/P6; owner ruling
// 2026-09-18 on #1330). One order for all three terminal lanes — marker
// Create, graph stamps, publish, entity Update — and three arms for a
// redelivered terminal:
//
//	(a) record already terminal → effect-free ACK (Q7; sits OUTSIDE the owner,
//	    at the lanes' own classification, and a lost compare-and-swap is a Retry
//	    that lands here on the redelivery);
//	(b) record not terminal, the marker exists → the Create is refused, the
//	    saved terminal is read back and adopted by loop ID + terminal kind, and
//	    a content difference is logged, never a disposition;
//	(c) no marker → Create, publish, Update, ACK.
//
// These run against the recording bucket with no NATS client, so publication
// is not observable here; what is observable is the KV order and what the
// record and the marker say afterwards. The broker half — the event really
// published before the record, and the adopted payload really republished —
// is terminal_tool_redelivery_integration_test.go.

const terminalOwnerLoopID = "5d1c7e2a-9b40-4f36-8a21-3c4d5e6f7a8b"

// terminalOwnerLoop is a process holding a loop at request R, able to write
// its record and unable to publish anything — so a publication step is a
// no-op and the KV writes are the whole observable effect.
func terminalOwnerLoop(t *testing.T) (*Component, *recordingLoopBucket, string, *bytes.Buffer) {
	t.Helper()
	published := looprequest.ID{LoopID: terminalOwnerLoopID, Iteration: 2, Retry: 0}.String()
	c, _ := replacementProcessHoldingALoop(t, published, terminalOwnerLoopID)
	c.natsClient = nil
	var logs bytes.Buffer
	c.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return c, c.loopsBucket.(*recordingLoopBucket), published, &logs
}

func completionFor(requestID, content string) agentic.AgentResponse {
	return agentic.AgentResponse{
		RequestID:    requestID,
		Status:       agentic.StatusComplete,
		FinishReason: "stop",
		Message:      agentic.ChatMessage{Role: "assistant", Content: content},
	}
}

func terminalMarkerOf(t *testing.T, bucket *recordingLoopBucket, loopID string) map[string]any {
	t.Helper()
	raw, ok := bucket.value("COMPLETE_" + loopID)
	require.True(t, ok, "no COMPLETE_ marker for the loop")
	var marker map[string]any
	require.NoError(t, json.Unmarshal(raw, &marker))
	return marker
}

// spec: agentic-loop / The loop record names its outstanding request
func TestTerminalOwnerArms(t *testing.T) {
	t.Run("(c) no durable terminal: the marker is created before the record is written terminal", func(t *testing.T) {
		c, bucket, published, _ := terminalOwnerLoop(t)

		msg, delivered := deliverResponse(t, c, completionFor(published, "the answer"))

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, []string{"COMPLETE_" + terminalOwnerLoopID, terminalOwnerLoopID}, bucket.written(),
			"the terminal commits marker first and the loop record LAST: the record's terminal state is "+
				"the step a crash can lose, and the marker is what a redelivery adopts")
		require.Equal(t, agentic.OutcomeSuccess, terminalMarkerOf(t, bucket, terminalOwnerLoopID)["outcome"])
		require.Equal(t, agentic.LoopStateComplete, persistedLoop(t, bucket, terminalOwnerLoopID).State)
	})

	t.Run("(b) the loop's durable terminal exists: it is adopted, not overwritten", func(t *testing.T) {
		c, bucket, published, logs := terminalOwnerLoop(t)
		saved := agentic.LoopCompletedEvent{
			LoopID:      terminalOwnerLoopID,
			TaskID:      "task-replacement",
			Outcome:     agentic.OutcomeSuccess,
			Result:      "the answer the predecessor published",
			CompletedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		}
		savedBytes, err := json.Marshal(&saved)
		require.NoError(t, err)
		_, err = bucket.Create(t.Context(), "COMPLETE_"+terminalOwnerLoopID, savedBytes)
		require.NoError(t, err)
		markerRevision := bucket.revisionOf("COMPLETE_" + terminalOwnerLoopID)
		bucket.resetWritten()

		msg, delivered := deliverResponse(t, c, completionFor(published, "an answer that differs in content"))

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
			"a content difference from the durable terminal is never a disposition")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, markerRevision, bucket.revisionOf("COMPLETE_"+terminalOwnerLoopID),
			"the durable terminal was overwritten: a Put replaced the one terminal the loop already published")
		raw, _ := bucket.value("COMPLETE_" + terminalOwnerLoopID)
		require.JSONEq(t, string(savedBytes), string(raw))
		require.Equal(t, []string{terminalOwnerLoopID}, bucket.written(),
			"adoption writes only the loop record")

		record := persistedLoop(t, bucket, terminalOwnerLoopID)
		require.Equal(t, agentic.LoopStateComplete, record.State)
		require.Equal(t, saved.Result, record.Result,
			"the record is written to match the adopted terminal, not this delivery's candidate")

		require.Contains(t, logs.String(), "adopted the loop's durable terminal")
		require.Contains(t, logs.String(), `"content_differs":true`,
			"the content difference is logged at the audit line")
	})

	t.Run("(b) a durable terminal of another kind is refused, not adopted", func(t *testing.T) {
		c, bucket, published, _ := terminalOwnerLoop(t)
		saved := agentic.LoopCancelledEvent{
			LoopID: terminalOwnerLoopID, TaskID: "task-replacement", Outcome: agentic.OutcomeCancelled,
			CancelledBy: "operator",
		}
		savedBytes, err := json.Marshal(&saved)
		require.NoError(t, err)
		_, err = bucket.Create(t.Context(), "COMPLETE_"+terminalOwnerLoopID, savedBytes)
		require.NoError(t, err)
		markerRevision := bucket.revisionOf("COMPLETE_" + terminalOwnerLoopID)
		recordRevision := bucket.revisionOf(terminalOwnerLoopID)

		_, delivered := deliverResponse(t, c, completionFor(published, "the answer"))

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, delivered.Decision(),
			"a terminal whose identity (loop ID + kind) does not match the candidate's is conflicting "+
				"evidence, and conflicting evidence fails closed")
		require.Equal(t, markerRevision, bucket.revisionOf("COMPLETE_"+terminalOwnerLoopID))
		require.Equal(t, recordRevision, bucket.revisionOf(terminalOwnerLoopID),
			"nothing is written behind a refused adoption")
	})

	t.Run("(a) a lost compare-and-swap retries, and the redelivery finds the record terminal", func(t *testing.T) {
		c, bucket, published, _ := terminalOwnerLoop(t)
		// A foreign writer commits the loop terminal first.
		foreign, err := json.Marshal(agentic.LoopEntity{
			ID: terminalOwnerLoopID, TaskID: "task-replacement", State: agentic.LoopStateComplete,
			Role: "general", Model: "model", PublishedRequestID: published,
		})
		require.NoError(t, err)
		_, err = bucket.Put(t.Context(), terminalOwnerLoopID, foreign)
		require.NoError(t, err)

		_, delivered := deliverResponse(t, c, completionFor(published, "the answer"))
		require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision(),
			"a revision conflict alone is a Retry: the re-read decides")
		_, heldErr := c.handler.GetLoop(terminalOwnerLoopID)
		require.Error(t, heldErr, "the loser released the loop so the redelivery re-reads the record")

		recordRevision := bucket.revisionOf(terminalOwnerLoopID)
		markerRevision := bucket.revisionOf("COMPLETE_" + terminalOwnerLoopID)
		bucket.resetWritten()

		msg, redelivered := deliverResponse(t, c, completionFor(published, "the answer"))

		require.Equal(t, natsclient.DeliveryDecisionAck, redelivered.Decision(),
			"a terminal record is settled: the redelivery is acknowledged without effect")
		require.Equal(t, int32(1), msg.acks.Load())
		require.Empty(t, bucket.written(), "an effect-free ACK writes nothing")
		require.Equal(t, recordRevision, bucket.revisionOf(terminalOwnerLoopID))
		require.Equal(t, markerRevision, bucket.revisionOf("COMPLETE_"+terminalOwnerLoopID))
	})
}

// The cancel lane takes the same owner: its marker now precedes its event
// (OQ-E), and the terminal transition clears a pending approval gate — L3's
// deferred item (archived L3 design :50), landed by the owner that writes the
// terminal record.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestCancelTakesTheTerminalOwnerAndClearsAPendingApproval(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	entity, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	require.NoError(t, entity.BeginAwaitingApproval("call-gated", "delete_rule", nil,
		"approval_required: destructive", time.Minute, ""))
	entity.PendingApproval.RequestID = published
	require.NoError(t, c.handler.UpdateLoop(entity))

	require.NoError(t, c.handleCancelSignal(t.Context(), agentic.UserSignal{
		SignalID: "signal-1", Type: agentic.SignalCancel, LoopID: terminalOwnerLoopID, UserID: "operator",
	}))

	require.Equal(t, []string{"COMPLETE_" + terminalOwnerLoopID, terminalOwnerLoopID}, bucket.written(),
		"cancel's marker is created before the record is written terminal, like every terminal")
	require.Equal(t, agentic.OutcomeCancelled, terminalMarkerOf(t, bucket, terminalOwnerLoopID)["outcome"])
	record := persistedLoop(t, bucket, terminalOwnerLoopID)
	require.Equal(t, agentic.LoopStateCancelled, record.State)
	require.Nil(t, record.PendingApproval,
		"a terminal record carrying a pending approval gate names a human decision nothing will apply")
	require.Empty(t, record.StateBeforeApproval)
}

// exhaustedTerminalOwnerLoop is terminalOwnerLoop with its iteration budget
// spent, so the next response fails the loop through handleLoopFailure.
func exhaustedTerminalOwnerLoop(t *testing.T) (*Component, *recordingLoopBucket, string) {
	t.Helper()
	c, bucket, published, _ := terminalOwnerLoop(t)
	entity, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	entity.Iterations = entity.MaxIterations
	require.NoError(t, c.handler.UpdateLoop(entity))
	return c, bucket, published
}

// The loop-failure lane takes the terminal owner's order, driven through the
// response lane (#1362 review M5): marker first, record last; a lost
// compare-and-swap is a Retry; and a failed step stops every step after it.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestLoopFailureTakesTheTerminalOwnersOrder(t *testing.T) {
	t.Run("marker first, record last", func(t *testing.T) {
		c, bucket, published := exhaustedTerminalOwnerLoop(t)

		msg, delivered := deliverResponse(t, c, completionFor(published, "one answer too many"))

		require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision())
		require.Equal(t, int32(1), msg.acks.Load())
		require.Equal(t, []string{"COMPLETE_" + terminalOwnerLoopID, terminalOwnerLoopID}, bucket.written(),
			"the failed record was written ahead of its durable terminal")
		marker := terminalMarkerOf(t, bucket, terminalOwnerLoopID)
		require.Equal(t, agentic.OutcomeFailed, marker["outcome"])
		require.Equal(t, "max_iterations", marker["reason"])
		require.Equal(t, agentic.LoopStateFailed, persistedLoop(t, bucket, terminalOwnerLoopID).State)
	})

	t.Run("a lost compare-and-swap on the record retries", func(t *testing.T) {
		c, bucket, published := exhaustedTerminalOwnerLoop(t)
		_, err := bucket.Put(t.Context(), terminalOwnerLoopID, []byte(`{"id":"moved"}`))
		require.NoError(t, err)
		bucket.resetWritten()

		_, delivered := deliverResponse(t, c, completionFor(published, "one answer too many"))

		require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision(),
			"a revision conflict alone is transient: the redelivery re-reads the record")
		require.Equal(t, []string{"COMPLETE_" + terminalOwnerLoopID}, bucket.written())
		_, heldErr := c.handler.GetLoop(terminalOwnerLoopID)
		require.Error(t, heldErr)
	})

	t.Run("a failed step stops every step after it", func(t *testing.T) {
		c, bucket, published := exhaustedTerminalOwnerLoop(t)
		bucket.fail = errKVUnavailable
		bucket.failPrefix = "COMPLETE_"

		_, delivered := deliverResponse(t, c, completionFor(published, "one answer too many"))

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, delivered.Decision())
		require.Empty(t, bucket.written(),
			"the record was written although the durable terminal before it did not land")
		_, heldErr := c.handler.GetLoop(terminalOwnerLoopID)
		require.Error(t, heldErr, "a failed terminal commit left the loop terminal in memory")
	})
}

// A response delivered on a context that ended before the handler touched
// anything is retried, not turned into the loop's failure (#1362 review M4,
// owner ruling 3): failing it would create the loop's create-once terminal on
// a shutdown.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestAResponseCancelledBeforeMutationRetries(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	before, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = c.handleResponseMessage(ctx, baseMessageBytes(t, &agentic.AgentResponse{
		RequestID: published, Status: agentic.StatusComplete,
		Message: agentic.ChatMessage{Role: "assistant", Content: "the answer"},
	}))

	require.ErrorIs(t, err, errCancelledBeforeMutation)
	require.False(t, errs.IsFatal(err), "a pre-mutation cancellation is not a partial effect")
	require.Empty(t, bucket.written(), "a retried delivery wrote the loop's terminal")
	after, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err, "a pre-mutation cancellation released the loop")
	require.Equal(t, before.State, after.State, "a cancelled delivery context failed the loop")
}

// The warm variant of review H1: a response meets a loop that is terminal in
// memory because another lane's terminal commit is in flight — here a cancel
// transitioned and not yet committed. The handler's terminal guard does
// nothing; the carrier must not then render that entity into the record, which
// would commit a cancelled record outside the terminal owner, possibly beside
// a marker that says something else.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAResponseMeetingAnUncommittedTerminalWritesNothing(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	_, err := c.handler.CancelLoop(terminalOwnerLoopID, "operator")
	require.NoError(t, err)

	_, delivered := deliverResponse(t, c, completionFor(published, "an answer racing the cancel"))

	require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision(),
		"the terminal belongs to the lane committing it; this delivery re-reads once it settles")
	require.Empty(t, bucket.written(), "the carrier wrote a terminal record outside the terminal owner")
}

// A terminal in memory is a commit in flight whose outcome the delivery cannot
// see, so a terminal-guard result is decided by the RECORD (#1362 re-review
// M1): a terminal record is acknowledged without effect, with the drop
// counted.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAResponseMeetingACommittedTerminalIsAcknowledged(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	require.NoError(t, c.handler.loopManager.TransitionLoop(terminalOwnerLoopID, agentic.LoopStateFailed))
	committed, err := json.Marshal(agentic.LoopEntity{
		ID: terminalOwnerLoopID, TaskID: "task-replacement", State: agentic.LoopStateFailed,
		Role: "general", Model: "model", PublishedRequestID: published,
	})
	require.NoError(t, err)
	_, err = bucket.Put(t.Context(), terminalOwnerLoopID, committed)
	require.NoError(t, err)
	bucket.resetWritten()
	dropped := testutil.ToFloat64(c.metrics.modelResponsesDropped.WithLabelValues("stale_request_id"))

	msg, delivered := deliverResponse(t, c, completionFor(published, "an answer after the loop settled"))

	require.Equal(t, natsclient.DeliveryDecisionAck, delivered.Decision(),
		"a terminal record is settled: retrying would run the delivery to MaxDeliver")
	require.Equal(t, int32(1), msg.acks.Load())
	require.Empty(t, bucket.written())
	require.Equal(t, dropped+1, testutil.ToFloat64(c.metrics.modelResponsesDropped.WithLabelValues("stale_request_id")))
}

// A loop that is terminal in memory and past its deadline gets a late
// response: the terminal guard runs before the timeout arm, so the in-flight
// terminal's Outcome and Error are not rewritten to a timeout failure and no
// failure is committed for it (#1362 re-review M2).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestALateResponseToATimedOutCancelledLoopTouchesNothing(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	require.NoError(t, c.handler.loopManager.SetTimeout(terminalOwnerLoopID, -time.Second))
	_, err := c.handler.CancelLoop(terminalOwnerLoopID, "operator")
	require.NoError(t, err)

	_, delivered := deliverResponse(t, c, completionFor(published, "a late answer"))

	require.Equal(t, natsclient.DeliveryDecisionRetry, delivered.Decision(),
		"the record is live: the cancel's commit is in flight and decides")
	require.Empty(t, bucket.written(), "a timeout failure was committed over an in-flight cancel")
	entity, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	require.Equal(t, agentic.OutcomeCancelled, entity.Outcome,
		"the timeout arm rewrote the in-flight cancel's outcome")
}

// The tool handler's terminal guard refuses before it touches anything — the
// stored result, the pending set, the trajectory, the timeout arm (#1362
// re-review M2, M4) — and the carrier then leaves the terminal to its owner.
// The tool LANE settles a terminal loop earlier (Q7's warm classification);
// this guard is reached by the approval lane's synthesized rejection.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAToolResultForATerminalLoopTouchesNothing(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	executionID := deriveToolExecutionID(published, "call-late", 1)
	c.handler.loopManager.TrackToolCall(executionID, terminalOwnerLoopID)
	require.NoError(t, c.handler.loopManager.AddPendingTool(terminalOwnerLoopID, "call-late"))
	require.NoError(t, c.handler.loopManager.SetTimeout(terminalOwnerLoopID, -time.Second))
	_, err := c.handler.CancelLoop(terminalOwnerLoopID, "operator")
	require.NoError(t, err)
	pendingBefore := c.handler.loopManager.GetPendingTools(terminalOwnerLoopID)

	result, err := c.handler.HandleToolResult(t.Context(), terminalOwnerLoopID, agentic.ToolResult{
		RequestID: published, ExecutionID: executionID, CallID: "call-late", CallOrdinal: 1,
		Name: "search", Content: "a result for a loop that has ended",
	})

	require.NoError(t, err)
	require.True(t, result.terminalOwnedElsewhere, "the terminal guard did not answer")
	require.Equal(t, pendingBefore, c.handler.loopManager.GetPendingTools(terminalOwnerLoopID),
		"the guard ran after the result was already applied to the pending set")
	entity, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	require.Equal(t, agentic.OutcomeCancelled, entity.Outcome, "the timeout arm rewrote the cancel")

	persistErr := c.persistHandlerResult(t.Context(), result)
	require.Error(t, persistErr)
	require.False(t, errs.IsFatal(persistErr), "an in-flight terminal is retried, not quarantined")
	require.Empty(t, bucket.written(), "the carrier wrote a terminal outside the owner")
}

// The approval lane honours a transient carrier failure: a lost
// compare-and-swap has released the loop and is retried, never quarantined
// (#1362 re-review M3).
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestAnApprovalWhoseRecordMovedIsRetried(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	executionID := deriveToolExecutionID(published, "call-gated", 1)
	entity, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	require.NoError(t, entity.BeginAwaitingApproval("call-gated", "delete_rule", nil,
		"approval_required: destructive", time.Minute, ""))
	entity.PendingApproval.RequestID = published
	entity.PendingApproval.ExecutionID = executionID
	entity.PendingApproval.CallOrdinal = 1
	require.NoError(t, c.handler.UpdateLoop(entity))
	_, err = bucket.Put(t.Context(), terminalOwnerLoopID, []byte(`{"id":"moved"}`))
	require.NoError(t, err)
	bucket.resetWritten()

	decision, err := c.handleApprovalResponseMessage(t.Context(), baseMessageBytes(t, &agentic.ApprovalResponse{
		LoopID: terminalOwnerLoopID, CallID: "call-gated", ExecutionID: executionID, RequestID: published,
		Decision: agentic.ApprovalDecisionApprove, ApprovedBy: "operator", DecidedAt: time.Now().UTC(),
	}))

	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision,
		"a lost compare-and-swap is transient; quarantining it latches the lane on a benign race")
	require.Empty(t, bucket.written())
}
