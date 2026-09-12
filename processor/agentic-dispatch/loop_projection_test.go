package agenticdispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / The shared view separates current authority from activity
func TestLoopProjectionClassifiesCurrentAuthority(t *testing.T) {
	c := terminalTestComponent(t)
	valid := agentic.LoopEntity{ID: admissionLoopA, State: agentic.LoopStateExecuting, MaxIterations: 20}
	data, err := json.Marshal(valid)
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		key     string
		data    []byte
		keep    bool
		invalid bool
	}{
		{"current", admissionLoopA, data, true, false},
		{"mismatched identity", admissionLoopB, data, false, true},
		{"invalid state", admissionLoopA, []byte(`{"id":"` + admissionLoopA + `","state":"bogus","max_iterations":20}`), false, true},
		{"retired paused state", admissionLoopA, []byte(`{"id":"` + admissionLoopA + `","state":"paused","max_iterations":20}`), false, true},
		{"malformed current value", admissionLoopA, []byte(`{`), false, true},
		{"noncanonical key with loop bytes", "not-a-loop", data, false, false},
		{"other producer record", "other.current." + admissionLoopA, []byte(`{`), false, false},
		{"research request", "research.request.received." + admissionLoopA, []byte(`{`), false, false},
		{"ordinary completion", "COMPLETE_" + admissionLoopA, loopCompletionJSON(t, admissionLoopA), true, false},
		{"completion key identity mismatch", "COMPLETE_" + admissionLoopB, loopCompletionJSON(t, admissionLoopA), false, true},
		{"noncanonical completion key", "COMPLETE_not-a-loop", loopCompletionJSON(t, admissionLoopA), false, true},
		{"malformed completion", "COMPLETE_" + admissionLoopA, []byte(`{`), false, true},
		{"invalid ordinary payload", "COMPLETE_" + admissionLoopA, []byte(`{"loop_id":"` + admissionLoopA + `","outcome":"success"}`), false, true},
		{"unsupported completion", "COMPLETE_" + admissionLoopA, []byte(`{"type":"other.result","payload":{}}`), false, true},
		{"registered nonterminal payload", "COMPLETE_" + admissionLoopA, terminalEnvelopeForDispatch(t, &agentic.LoopCreatedEvent{
			LoopID: admissionLoopA, TaskID: "task",
		}), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, keep, err := c.decodeActivityRecord(tc.key, tc.data, graphview.EntryMeta{})
			if tc.invalid {
				require.Error(t, err)
				require.False(t, keep)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.keep, keep)
			if tc.key == "COMPLETE_"+admissionLoopA {
				require.Nil(t, record.entity, "completion activity never supplies current authority")
				require.Equal(t, admissionLoopA, record.loop.LoopID)
			}
		})
	}
}

// spec: agentic-dispatch / The shared view separates current authority from activity
func TestLoopProjectionPreservesOrdinaryCompletionRepresentations(t *testing.T) {
	c := terminalTestComponent(t)
	at := time.Unix(1700000000, 0).UTC()
	for _, tc := range []struct {
		name    string
		payload message.Payload
		want    Loop
	}{
		{"success", &agentic.LoopCompletedEvent{
			LoopID: admissionLoopA, TaskID: "task", Outcome: agentic.OutcomeSuccess, Role: "assistant",
			Result: "answer", Prompt: "question", Iterations: 2, TokensIn: 11, TokensOut: 7,
			ParentLoopID: admissionLoopB, CompletedAt: at,
		}, Loop{LoopID: admissionLoopA, TaskID: "task", Outcome: "success", Role: "assistant",
			Result: "answer", Prompt: "question", Iterations: 2, TokensIn: 11, TokensOut: 7, ParentLoopID: admissionLoopB}},
		{"failure", &agentic.LoopFailedEvent{
			LoopID: admissionLoopA, TaskID: "task", Outcome: agentic.OutcomeFailed, Role: "assistant",
			Error: "failed work", Prompt: "question", Iterations: 3, TokensIn: 13, TokensOut: 9,
			ParentLoopID: admissionLoopB, FailedAt: at,
		}, Loop{LoopID: admissionLoopA, TaskID: "task", Outcome: "failed", Role: "assistant",
			Error: "failed work", Prompt: "question", Iterations: 3, TokensIn: 13, TokensOut: 9, ParentLoopID: admissionLoopB}},
		{"cancellation", &agentic.LoopCancelledEvent{
			LoopID: admissionLoopA, TaskID: "task", Outcome: agentic.OutcomeCancelled,
			ParentLoopID: admissionLoopB, CancelledAt: at,
		}, Loop{LoopID: admissionLoopA, TaskID: "task", Outcome: "cancelled", ParentLoopID: admissionLoopB}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			require.NoError(t, err)
			for name, wire := range map[string][]byte{"raw": raw, "registered": terminalEnvelopeForDispatch(t, tc.payload)} {
				t.Run(name, func(t *testing.T) {
					record, keep, err := c.decodeActivityRecord("COMPLETE_"+admissionLoopA, wire, graphview.EntryMeta{Created: at})
					require.NoError(t, err)
					require.True(t, keep)
					require.Nil(t, record.entity)
					require.Equal(t, tc.want, record.loop)
					require.Equal(t, at, record.createdAt)
					_, _, err = c.decodeActivityRecord("COMPLETE_"+admissionLoopB, wire, graphview.EntryMeta{})
					require.Error(t, err, "both representations require canonical key/payload identity agreement")
				})
			}
		})
	}
}

// spec: agentic-dispatch / The shared view separates current authority from activity
func TestLoopProjectionRejectsInvalidRegisteredCompletion(t *testing.T) {
	c := terminalTestComponent(t)
	wire := terminalEnvelopeForDispatch(t, &agentic.LoopCompletedEvent{
		LoopID: admissionLoopA, TaskID: "task", Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now().UTC(),
	})
	for _, field := range []string{"id", "task_id", "completed_at"} {
		t.Run(field, func(t *testing.T) {
			var envelope map[string]any
			require.NoError(t, json.Unmarshal(wire, &envelope))
			if field == "id" {
				delete(envelope, field)
			} else {
				delete(envelope["payload"].(map[string]any), field)
			}
			invalid, err := json.Marshal(envelope)
			require.NoError(t, err)
			record, keep, err := c.decodeActivityRecord("COMPLETE_"+admissionLoopA, invalid, graphview.EntryMeta{})
			require.Error(t, err)
			require.False(t, keep)
			require.Nil(t, record.entity)
		})
	}
}

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func TestLoopListDoesNotReportUnreadyAsEmpty(t *testing.T) {
	c := newTestComponent(t)
	r := httptest.NewRequest(http.MethodGet, "/loops", nil)
	w := httptest.NewRecorder()
	c.handleListLoops(w, r)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestLoopAdmissionValidatesPersistedAuthority(t *testing.T) {
	c := admissionTestComponent(t)
	withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopB, UserID: "user-a", State: agentic.LoopStateExecuting, MaxIterations: 20,
	}})
	_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
		Seam: seamChannelSubmission, Field: "reply_to", Operation: loopOpContinue,
		LoopID: admissionLoopA, Requester: "user-a",
	})
	require.Error(t, err, "a decodable but wrong-key record is not current loop authority")
	withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopA, UserID: "user-a", State: agentic.LoopStatePaused, MaxIterations: 20,
	}})
	_, err = c.loadPersistedLoop(t.Context(), admissionLoopA)
	require.Error(t, err, "the exact reader shares retired-state validation")
}
