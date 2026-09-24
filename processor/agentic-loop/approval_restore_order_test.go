package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/stretchr/testify/require"
)

// The approval lane's cold branch (#1362, design § 5.5, D35): an approval
// answer reaching a process that does not hold its loop is settled by the
// loop's RECORD, in this order — step 0 adopts any newer retained request,
// the record must still be awaiting THIS gate, I4 and the gated result are
// checked against the record step 0 left, the loop is rebuilt from its
// retained request and response, and only then is the answer applied. Before
// the branch existed every such answer was stale-dropped and acknowledged,
// and the human decision was lost.
//
// These run against the recording bucket and a stub evidence reader with no
// broker, so a publication is observable only through the order it forces
// (an unpublishable client stops whatever follows it). The broker half — the
// reject-minted W4 across a real crash — is
// terminal_tool_redelivery_integration_test.go.

const coldApprovalLoopID = "3d1f7a52-8b64-4c09-a1e2-5f6a7b8c9d01"

// coldApproval is a replacement process that does not hold the loop, over the
// record and the retained evidence its predecessor left: a two-call batch
// whose FIRST call gated the loop, with the second queued behind it and
// cleared by the gate.
type coldApproval struct {
	c       *Component
	bucket  *recordingLoopBucket
	request string
	gate    agentic.PendingApprovalState
	batch   agentic.AgentResponse
}

func coldGatedBatch(requestID string) (agentic.AgentResponse, []agentic.ToolCall) {
	batch := agentic.AgentResponse{
		RequestID:    requestID,
		Status:       agentic.StatusToolCall,
		FinishReason: "tool_calls",
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{
			{ID: "call-gated", Name: "delete_rule", Arguments: map[string]any{"rule_id": "rule-42"}},
			{ID: "call-sibling", Name: "search", Arguments: map[string]any{"q": "rules"}},
		}},
	}
	calls := append([]agentic.ToolCall(nil), batch.Message.ToolCalls...)
	if err := stampToolExecutionCorrelation(requestID, calls); err != nil {
		panic(err)
	}
	return batch, calls
}

// newColdApproval writes the gated record and wires the evidence the
// replacement reads. shape runs last over the record, so a subtest states only
// the one fact it varies.
func newColdApproval(t *testing.T, evidence loopEvidenceReader, shape func(*agentic.LoopEntity)) coldApproval {
	t.Helper()
	request := looprequest.ID{LoopID: coldApprovalLoopID, Iteration: 1, Retry: 0}.String()
	batch, calls := coldGatedBatch(request)
	gated := calls[0]
	gate := agentic.PendingApprovalState{
		RequestID:   request,
		ExecutionID: gated.ExecutionID,
		CallID:      gated.ID,
		CallOrdinal: gated.CallOrdinal,
		ToolName:    gated.Name,
		Arguments:   gated.Arguments,
		Reason:      agentic.ApprovalRequiredPrefix + "delete_rule requires human approval",
		RequestedAt: time.Now().UTC(),
		Timeout:     time.Hour,
	}

	handler := NewMessageHandler(DefaultConfig())
	c := releaseTestComponent(t, handler)
	bucket := &recordingLoopBucket{}
	c.loopsBucket = bucket
	if evidence == nil {
		evidence = stubEvidenceReader{requestID: request, response: &batch}
	}
	c.requestEvidence = evidence

	coldRecord(t, c, coldApprovalLoopID, func(e *agentic.LoopEntity) {
		e.State = agentic.LoopStateAwaitingApproval
		e.StateBeforeApproval = agentic.LoopStateExecuting
		e.PublishedRequestID = request
		e.Iterations = 0
		pending := gate
		e.PendingApproval = &pending
		e.PendingToolResults = map[string]agentic.ToolResult{
			gated.ExecutionID: {
				LoopID: coldApprovalLoopID, RequestID: request, ExecutionID: gated.ExecutionID,
				CallID: gated.ID, CallOrdinal: gated.CallOrdinal, Name: gated.Name,
				ErrorKind: agentic.ToolErrorPermission, Error: gate.Reason,
			},
		}
		if shape != nil {
			shape(e)
		}
	})
	bucket.resetWritten()
	return coldApproval{c: c, bucket: bucket, request: request, gate: gate, batch: batch}
}

func (a coldApproval) answer(decision string) agentic.ApprovalResponse {
	return agentic.ApprovalResponse{
		LoopID:      coldApprovalLoopID,
		CallID:      a.gate.CallID,
		ExecutionID: a.gate.ExecutionID,
		RequestID:   a.gate.RequestID,
		Decision:    decision,
		ApprovedBy:  "operator",
		Reason:      "reviewed",
		DecidedAt:   time.Now().UTC(),
	}
}

func (a coldApproval) deliver(t *testing.T, response agentic.ApprovalResponse) (natsclient.DeliveryDecision, error) {
	t.Helper()
	return a.c.handleApprovalResponseMessage(t.Context(), baseMessageBytes(t, &response))
}

func (a coldApproval) held() bool {
	_, err := a.c.handler.GetLoop(coldApprovalLoopID)
	return err == nil
}

// unreadableEvidence is a stream that cannot be read, as opposed to one that
// was read and holds nothing.
type unreadableEvidence struct{}

func (unreadableEvidence) ReadRetainedRequest(context.Context, string, string) ([]byte, bool, error) {
	return nil, false, errors.New("stream unavailable")
}

func (unreadableEvidence) ReadRetainedResponse(context.Context, string, string) ([]byte, bool, error) {
	return nil, false, errors.New("stream unavailable")
}

// TestAColdApprovalAnswerIsSettledByTheRecord walks the branch's refusals, in
// the order it takes them. Every one leaves the loop unheld, and every one
// but the confirmed-absence arm writes nothing.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdApprovalAnswerIsSettledByTheRecord(t *testing.T) {
	t.Run("an absent record is acknowledged", func(t *testing.T) {
		a := newColdApproval(t, nil, nil)
		a.bucket = &recordingLoopBucket{}
		a.c.loopsBucket = a.bucket

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision,
			"nothing any process can apply an answer to: acknowledged (D35)")
		require.Empty(t, a.bucket.written())
		require.False(t, a.held())
	})

	t.Run("a terminal record is acknowledged", func(t *testing.T) {
		a := newColdApproval(t, nil, nil)
		terminal := agentic.NewLoopEntity(coldApprovalLoopID, "task-cold", "general", "model", 10)
		terminal.State = agentic.LoopStateFailed
		data, err := json.Marshal(terminal)
		require.NoError(t, err)
		_, err = a.bucket.Put(t.Context(), coldApprovalLoopID, data)
		require.NoError(t, err)
		a.bucket.resetWritten()

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.Empty(t, a.bucket.written())
		require.False(t, a.held())
	})

	t.Run("a live record no longer awaiting approval is acknowledged as inapplicable", func(t *testing.T) {
		// W3: the gate was cleared and the answer's acknowledgement lost.
		a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
			e.State = agentic.LoopStateExecuting
			e.StateBeforeApproval = ""
			e.PendingApproval = nil
		})

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.Empty(t, a.bucket.written(), "an inapplicable answer writes nothing")
		require.False(t, a.held(), "an inapplicable answer rebuilds nothing")
	})

	t.Run("an answer for another gate is acknowledged as inapplicable", func(t *testing.T) {
		a := newColdApproval(t, nil, nil)
		other := a.answer(agentic.ApprovalDecisionApprove)
		other.ExecutionID = deriveToolExecutionID(a.request, "call-other", 3)

		decision, err := a.deliver(t, other)

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.Empty(t, a.bucket.written())
		require.False(t, a.held())
	})

	t.Run("a gate naming another request than the record is quarantined (I4)", func(t *testing.T) {
		a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
			e.PendingApproval.RequestID = looprequest.ID{LoopID: coldApprovalLoopID, Iteration: 1, Retry: 1}.String()
		})

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision,
			"a record that breaks I4 is conflicting evidence, never a guess")
		require.Empty(t, a.bucket.written())
		require.False(t, a.held())
	})

	t.Run("a gate whose result the record does not carry is quarantined", func(t *testing.T) {
		a := newColdApproval(t, nil, func(e *agentic.LoopEntity) {
			e.PendingToolResults = nil
		})

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionReject))

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		require.Empty(t, a.bucket.written())
		require.False(t, a.held())
	})

	t.Run("an unreadable stream is retried", func(t *testing.T) {
		a := newColdApproval(t, unreadableEvidence{}, nil)

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionRetry, decision,
			"transient uncertainty is not confirmed absence (owner ruling 2026-09-13 on #1146)")
		require.Empty(t, a.bucket.written())
		require.False(t, a.held())
	})
}

// TestAColdApprovalWhoseEvidenceIsGoneFailsTheLoop is OQ1 (owner ruling
// 2026-09-13 on #1146; OQ-C on #1362): the retained request, or the response
// that carries the gated batch, is confirmed absent, so the loop cannot be
// continued and fails with continuation_unavailable — through the terminal
// owner, so COMPLETE_<loopID> is created before the record is written
// terminal, and the delivery is acknowledged only after both.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdApprovalWhoseEvidenceIsGoneFailsTheLoop(t *testing.T) {
	for name, evidence := range map[string]func(request string) loopEvidenceReader{
		"the retained response is gone": func(request string) loopEvidenceReader {
			return stubEvidenceReader{requestID: request}
		},
		"the retained request is gone": func(string) loopEvidenceReader {
			return stubEvidenceReader{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := looprequest.ID{LoopID: coldApprovalLoopID, Iteration: 1, Retry: 0}.String()
			a := newColdApproval(t, evidence(request), nil)

			decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision,
				"the failure is durable, so the answer is settled")
			require.Equal(t, []string{"COMPLETE_" + coldApprovalLoopID, coldApprovalLoopID}, a.bucket.written(),
				"the durable terminal precedes the terminal record (terminal owner)")
			marker := terminalMarkerOf(t, a.bucket, coldApprovalLoopID)
			require.Equal(t, agentic.OutcomeFailed, marker["outcome"])
			require.Equal(t, "continuation_unavailable", marker["reason"])
			record := persistedLoop(t, a.bucket, coldApprovalLoopID)
			require.Equal(t, agentic.LoopStateFailed, record.State)
			require.Nil(t, record.PendingApproval, "a terminal record names no human decision")
			require.False(t, a.held(), "the failed loop is released once its terminal is committed")
		})
	}
}

// TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt: a live gated record and
// the evidence to rebuild from. The answer is applied exactly as the process
// that gated the loop would have applied it.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdApprovalAnswerRebuildsTheLoopAndAppliesIt(t *testing.T) {
	t.Run("an approval dispatches the gated call and clears the gate", func(t *testing.T) {
		a := newColdApproval(t, nil, nil)

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.Equal(t, []string{coldApprovalLoopID}, a.bucket.written())
		record := persistedLoop(t, a.bucket, coldApprovalLoopID)
		require.Equal(t, agentic.LoopStateExecuting, record.State)
		require.Nil(t, record.PendingApproval)
		require.Equal(t, a.request, record.PublishedRequestID, "an approval mints no request")
		require.True(t, a.held(), "the rebuilt loop is this process's now")
		require.Equal(t, coldApprovalLoopID, a.c.findLoopIDForToolCall(a.gate.ExecutionID),
			"the approved call's result must route to the rebuilt loop")
		require.Equal(t, []string{a.gate.CallID}, a.c.handler.loopManager.GetPendingTools(coldApprovalLoopID),
			"the approved call is the one in flight, and nothing else")
	})

	t.Run("a rejection completes the batch the gate cleared and advances the loop", func(t *testing.T) {
		// The gate cleared the call queued behind it (gateForApproval). A
		// rebuild that re-queued it from the retained batch would dispatch a
		// call the process that gated the loop never would have.
		a := newColdApproval(t, nil, nil)

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionReject))

		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		record := persistedLoop(t, a.bucket, coldApprovalLoopID)
		require.Equal(t, looprequest.ID{LoopID: coldApprovalLoopID, Iteration: 2, Retry: 0}.String(),
			record.PublishedRequestID, "the rejection completes the batch and mints the next request")
		require.Equal(t, 1, record.Iterations)
		require.Empty(t, record.PendingToolResults)
		require.Nil(t, record.PendingApproval)
		require.Empty(t, a.c.handler.loopManager.GetPendingTools(coldApprovalLoopID),
			"the sibling the gate cleared was dispatched by the rebuilt loop")
	})

	t.Run("the rebuilt answer publishes before it writes", func(t *testing.T) {
		// The approval lane takes publish → compare-and-swap (task 1.4): a
		// publication that did not land leaves the record as it was read, so
		// the redelivery finds the gate still there.
		a := newColdApproval(t, nil, nil)
		a.c.natsClient = unpublishableClient(t)

		decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionApprove))

		require.Error(t, err)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		require.Empty(t, a.bucket.written(), "the record was written ahead of the publication")
	})
}

// TestAColdReplacementAdoptsPastARejectionAndAcknowledgesTheApproval is the
// reject-minted W4 without a broker: the predecessor published R(N+1) from a
// rejection and died before its record update. Step 0 adopts R(N+1) and clears
// the gate in the same compare-and-swap, the answer finds no gate, and it is
// acknowledged with nothing republished or re-applied.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdReplacementAdoptsPastARejectionAndAcknowledgesTheApproval(t *testing.T) {
	next := looprequest.ID{LoopID: coldApprovalLoopID, Iteration: 2, Retry: 0}.String()
	a := newColdApproval(t, stubEvidenceReader{requestID: next}, nil)

	decision, err := a.deliver(t, a.answer(agentic.ApprovalDecisionReject))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, []string{coldApprovalLoopID}, a.bucket.written(), "step 0's adopt is the only write")
	record := persistedLoop(t, a.bucket, coldApprovalLoopID)
	require.Equal(t, next, record.PublishedRequestID)
	require.Equal(t, 1, record.Iterations)
	require.Equal(t, agentic.LoopStateExecuting, record.State)
	require.Nil(t, record.PendingApproval)
	require.False(t, a.held(), "an inapplicable answer rebuilds nothing")
}
