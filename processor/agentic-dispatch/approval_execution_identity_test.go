package agenticdispatch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// gateOn re-points the loop's pending approval at another execution, which is
// what happens when the approved call finishes and the next one gates.
func gateOn(c *Component, loopID, callID string) {
	c.loopTracker.SetPendingApproval(loopID, &PendingApprovalInfo{
		CallID:      callID,
		ExecutionID: approvalExecutionID(callID),
		ToolName:    "delete_rule",
		Arguments:   map[string]any{"rule_id": "rule-42"},
		Reason:      "approval_required: Tool 'delete_rule' requires human approval",
		RequestedAt: time.Now().UTC(),
	})
}

// A human approves ONE call. The endpoint used to approve whichever call
// happened to be pending when the POST landed: the body carried no execution
// identity at all, so the handler read the current pending record and stamped
// THAT execution onto the response it published. A decision made about
// execution A — retried after A finished and B gated, which is ordinary
// at-least-once client behaviour — was therefore republished as an approval of
// B, and the loop's own matcher accepted it, because dispatch had relabelled
// it with B's identity. The review drove the production mux and watched one
// identical body emit exec-a and then exec-b, both 200.
//
// The body now names the execution the caller reviewed, and the refusals land
// before anything is published: 400 when it names none, 409 when it names one
// that is not the gate now pending. 409 rather than 200-and-ignore, because the
// caller's decision was about a call nobody is being asked about any more and
// only the caller can decide what to do with it.
//
// The status is the publication assertion: this component has no NATS client,
// so any request that reached the publish step would answer 500. A 409 could
// not have published, and the pending record is re-read afterwards to prove
// the refusal also left the gate alone.
//
// spec: agentic-dispatch / An approval decision names the execution it answers
func TestStaleApprovalPOSTIsRefusedAgainstTheGateThatIsPending(t *testing.T) {
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-a")
	body := `{"decision":"approve","execution_id":"` + approvalExecutionID("call-a") + `"}`

	// While call-a is the pending gate, the decision is accepted as far as the
	// publish, which is where a component with no broker stops.
	require.Equal(t, http.StatusInternalServerError, approvalPOST(t, comp, seamTestLoopA, body).Code,
		"the decision for the pending gate must reach the publish step")

	// call-a finished; call-b is now the gate the human has NOT been asked about.
	gateOn(comp, seamTestLoopA, "call-b")

	rec := approvalPOST(t, comp, seamTestLoopA, body)
	assert.Equal(t, http.StatusConflict, rec.Code,
		"the retried decision approved a call nobody reviewed")
	var refusal HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &refusal))
	assert.Contains(t, refusal.Content, approvalExecutionID("call-a"),
		"the refusal must name the execution the caller asked about")

	pending, awaiting := comp.loopTracker.GetPendingApproval(seamTestLoopA)
	require.True(t, awaiting, "a refused decision must leave the gate pending")
	assert.Equal(t, approvalExecutionID("call-b"), pending.ExecutionID,
		"the refused decision must not re-point or clear the pending gate")
}

// The field is required, and required means refused-at-the-edge rather than
// defaulted from whatever is pending — defaulting is precisely the bug. The
// loop is awaiting approval here, so 409 would be the wrong answer: nothing is
// wrong with the loop's state, the body is incomplete.
//
// spec: agentic-dispatch / An approval decision names the execution it answers
func TestApprovalWithoutAnExecutionIdentityIsRefused(t *testing.T) {
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-a")

	rec := approvalPOST(t, comp, seamTestLoopA, `{"decision":"approve"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var refusal HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &refusal))
	assert.Contains(t, refusal.Content, "execution_id",
		"the refusal must name the field the caller left out")

	pending, awaiting := comp.loopTracker.GetPendingApproval(seamTestLoopA)
	require.True(t, awaiting)
	assert.Equal(t, approvalExecutionID("call-a"), pending.ExecutionID,
		"the refused decision must leave the gate exactly as it found it")
}

// The symmetry that keeps an execution-less gate approvable. The loop's own
// matcher compares only when the pending approval HAS an identity
// (processor/agentic-loop/state.go:545); the endpoint does the same, so a gate
// that carries none is answered rather than made permanently unapprovable by a
// comparison it can never satisfy. The body's field stays required either way.
//
// spec: agentic-dispatch / An approval decision names the execution it answers
func TestApprovalAgainstAGateWithNoExecutionIdentityIsAccepted(t *testing.T) {
	comp := newTestComponent(t)
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      seamTestLoopA,
		TaskID:      "task-" + seamTestLoopA,
		UserID:      "user-1",
		ChannelType: "http",
		ChannelID:   "chan-1",
		State:       "awaiting_approval",
		CreatedAt:   time.Now(),
	})
	comp.loopTracker.SetPendingApproval(seamTestLoopA, &PendingApprovalInfo{
		CallID:      "call-legacy",
		ToolName:    "delete_rule",
		Reason:      "approval_required: Tool 'delete_rule' requires human approval",
		RequestedAt: time.Now().UTC(),
	})

	rec := approvalPOST(t, comp, seamTestLoopA,
		`{"decision":"approve","execution_id":"exec-anything"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"a gate with no identity to compare must still be answerable: this reached the publish step")
}
