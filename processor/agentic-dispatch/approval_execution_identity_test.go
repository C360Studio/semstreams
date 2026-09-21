package agenticdispatch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approvalPOST drives one approval submission through the PRODUCTION route
// table, not the handler function: the endpoint's behaviour includes the path
// pattern that binds {id}, and a test that calls the handler directly cannot
// see a body reaching the wrong route.
func approvalPOST(t *testing.T, c *Component, loopID, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	c.RegisterHTTPHandlers("/", mux)
	req := httptest.NewRequest(http.MethodPost, "/loops/"+loopID+"/approval", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// gatedRecord is the durable authority this endpoint reads on every decision:
// a loop awaiting approval, with the execution identity the human was shown.
func gatedRecord(loopID, callID, executionID string) *agentic.LoopEntity {
	return &agentic.LoopEntity{
		ID: loopID, TaskID: "task-" + loopID, UserID: "user-1",
		ChannelType: "http", ChannelID: "chan-1",
		State: agentic.LoopStateAwaitingApproval, MaxIterations: 3,
		PendingApproval: &agentic.PendingApprovalState{
			CallID: callID, ExecutionID: executionID, ToolName: "delete_rule",
		},
	}
}

// A human approves ONE call. The endpoint used to approve whichever call
// happened to be pending when the POST landed: the body carried no execution
// identity at all, so the handler read the current pending gate and stamped
// THAT execution onto the response it published. A decision about execution A —
// retried after A finished and B gated, which is ordinary at-least-once client
// behaviour — was therefore republished as an approval of B, and the loop's own
// matcher accepted it, because dispatch had relabelled it with B's identity.
//
// The body now names the execution the caller reviewed, and the refusals land
// before anything is published: 400 when it names none, 409 when it names one
// that is not the gate now pending. 409 rather than 200-and-ignore, because the
// caller's decision was about a call nobody is being asked about any more and
// only the caller can decide what to do with it.
//
// On this layer the gate is read from durable authority on every decision, so
// "the gate moved" is a record that changed, not a cache that diverged — which
// is the same test with one fewer thing to believe.
//
// The status is the publication assertion: this component has no NATS client,
// so any request that reached the publish step would answer 500. A 409 could
// not have published, and the record is re-read afterwards to prove the refusal
// also left the gate alone.
//
// spec: agentic-dispatch / An approval decision names the execution it answers
func TestStaleApprovalPOSTIsRefusedAgainstTheGateThatIsPending(t *testing.T) {
	comp := newTestComponent(t)
	records := map[string]*agentic.LoopEntity{
		seamTestLoopA: gatedRecord(seamTestLoopA, "call-a", "exec-call-a"),
	}
	withPersistedLoops(comp, records)
	body := `{"decision":"approve","execution_id":"exec-call-a"}`

	// While call-a is the gate of record, the decision is accepted as far as
	// the publish, which is where a component with no broker stops.
	require.Equal(t, http.StatusInternalServerError, approvalPOST(t, comp, seamTestLoopA, body).Code,
		"the decision for the pending gate must reach the publish step")

	// call-a finished; call-b is now the gate the human has NOT been asked about.
	records[seamTestLoopA] = gatedRecord(seamTestLoopA, "call-b", "exec-call-b")

	rec := approvalPOST(t, comp, seamTestLoopA, body)
	assert.Equal(t, http.StatusConflict, rec.Code,
		"the retried decision approved a call nobody reviewed")
	var refusal HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &refusal))
	assert.Contains(t, refusal.Content, "exec-call-a",
		"the refusal must name the execution the caller asked about")

	require.NotNil(t, records[seamTestLoopA].PendingApproval, "a refused decision must leave the gate pending")
	assert.Equal(t, "exec-call-b", records[seamTestLoopA].PendingApproval.ExecutionID,
		"the refused decision must not re-point or clear the gate of record")
}

// The field is required, and required means refused-at-the-edge rather than
// defaulted from whatever is gated — defaulting is precisely the bug. The loop
// is awaiting approval here, so 409 would be the wrong answer: nothing is wrong
// with the loop's state, the body is incomplete. The refusal also lands BEFORE
// the durable read, which is why the record is asserted untouched.
//
// spec: agentic-dispatch / An approval decision names the execution it answers
func TestApprovalWithoutAnExecutionIdentityIsRefused(t *testing.T) {
	comp := newTestComponent(t)
	record := gatedRecord(seamTestLoopA, "call-a", "exec-call-a")
	withPersistedLoops(comp, map[string]*agentic.LoopEntity{seamTestLoopA: record})
	before, err := json.Marshal(record)
	require.NoError(t, err)

	rec := approvalPOST(t, comp, seamTestLoopA, `{"decision":"approve"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var refusal HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &refusal))
	assert.Contains(t, refusal.Content, "execution_id",
		"the refusal must name the field the caller left out")

	after, err := json.Marshal(record)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after),
		"the refused decision must leave the gate exactly as it found it")
}
