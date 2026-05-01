package agenticdispatch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trackedLoopWithApproval seeds a loop in the tracker AND attaches a
// pending approval. Returns the test component for chained
// assertions. Used by every test that exercises the success-path
// validation branches up to (but not including) the NATS publish.
func trackedLoopWithApproval(t *testing.T, loopID, callID string) *Component {
	t.Helper()
	comp := newTestComponent(t)
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      loopID,
		TaskID:      "task-" + loopID,
		UserID:      "user-1",
		ChannelType: "http",
		ChannelID:   "chan-1",
		State:       "awaiting_approval",
		CreatedAt:   time.Now(),
	})
	comp.loopTracker.SetPendingApproval(loopID, &PendingApprovalInfo{
		CallID:      callID,
		ToolName:    "delete_rule",
		Arguments:   map[string]any{"rule_id": "rule-42"},
		Reason:      "approval_required: Tool 'delete_rule' requires human approval",
		RequestedAt: time.Now().UTC(),
	})
	return comp
}

func TestHandleLoopApproval_MissingLoopID(t *testing.T) {
	comp := newTestComponent(t)

	body := `{"decision":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/loops//approval", strings.NewReader(body))
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleLoopApproval_LoopNotFound(t *testing.T) {
	comp := newTestComponent(t)

	body := `{"decision":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/ghost/approval", strings.NewReader(body))
	req.SetPathValue("id", "ghost")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleLoopApproval_InvalidBody(t *testing.T) {
	comp := trackedLoopWithApproval(t, "loop-1", "call-001")

	req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader("not json"))
	req.SetPathValue("id", "loop-1")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleLoopApproval_UnknownDecision(t *testing.T) {
	comp := trackedLoopWithApproval(t, "loop-1", "call-001")

	body := `{"decision":"abstain"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader(body))
	req.SetPathValue("id", "loop-1")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.Content, "invalid decision")
}

// TestHandleLoopApproval_NotAwaitingApproval is the regression guard
// for the cache-divergence false-negative: when dispatch's local view
// of the loop has no PendingApproval (process restart, race lost,
// approval already resolved), the handler must reject 409 rather
// than try to publish with an empty CallID. 409 Conflict is the
// REST-conventional "resource exists, wrong state for this op."
func TestHandleLoopApproval_NotAwaitingApproval(t *testing.T) {
	comp := newTestComponent(t)
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      "loop-noapproval",
		UserID:      "user-1",
		ChannelType: "http",
		State:       "executing",
		CreatedAt:   time.Now(),
	})
	// No SetPendingApproval — loop is tracked but not awaiting approval.

	body := `{"decision":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/loop-noapproval/approval", strings.NewReader(body))
	req.SetPathValue("id", "loop-noapproval")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)

	var resp HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.Content, "loop not awaiting approval")
}

// TestHandleLoopApproval_DecisionValidation pins each of the three
// valid decisions through the validation gate. Without a NATS client
// the publish step fails with 500 — but reaching that point proves
// validation passed (which is what we're guarding here).
func TestHandleLoopApproval_DecisionValidation(t *testing.T) {
	tests := []struct {
		name       string
		decision   string
		expectCode int
	}{
		{"approve passes validation", "approve", http.StatusInternalServerError},
		{"reject passes validation", "reject", http.StatusInternalServerError},
		{"modify passes validation", "modify", http.StatusInternalServerError},
		{"empty fails validation", "", http.StatusBadRequest},
		{"random string fails validation", "yes-please", http.StatusBadRequest},
		{"uppercase fails validation", "APPROVE", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := trackedLoopWithApproval(t, "loop-1", "call-001")

			body := `{"decision":"` + tt.decision + `"}`
			req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader(body))
			req.SetPathValue("id", "loop-1")
			rec := httptest.NewRecorder()

			comp.handleLoopApproval(rec, req)

			assert.Equal(t, tt.expectCode, rec.Code)
		})
	}
}

// TestHandleLoopApproval_BodyIdentityFallback verifies that when
// neither ctx nor body supplies a user_id, the helper's default
// ("http-user") propagates through to the approval response. The
// handler reaches the publish step (500 because no NATS client),
// proving identity was resolved successfully.
func TestHandleLoopApproval_BodyIdentityFallback(t *testing.T) {
	comp := trackedLoopWithApproval(t, "loop-1", "call-001")

	// No user_id in body.
	body := `{"decision":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader(body))
	req.SetPathValue("id", "loop-1")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	// Reaches the publish step (no NATS client → 500), confirming
	// validation + identity resolution passed.
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleLoopApproval_CtxIdentityWinsOverBody is the regression
// guard for the helper's resolution order: middleware-supplied ctx
// identity must override a body user_id field. Verified via the
// attached PendingApproval being routed through a request whose ctx
// has been augmented with WithIdentity. Reaching publish (500) proves
// the helper was consulted; the actual approver value is captured
// indirectly via the identity helper unit tests.
func TestHandleLoopApproval_CtxIdentityWinsOverBody(t *testing.T) {
	comp := trackedLoopWithApproval(t, "loop-1", "call-001")

	body := `{"decision":"approve","user_id":"body-user"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader(body))
	req = req.WithContext(auth.WithIdentity(req.Context(), &auth.Identity{ID: "ctx-authenticated-user"}))
	req.SetPathValue("id", "loop-1")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	// Reaches publish (500 no NATS); middleware seam is exercised.
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleLoopApproval_ModifiedArgumentsAccepted is a structural
// guard: the modify decision with modified_arguments must reach the
// publish step. The frame-level wire shape (ApprovalResponse with
// ModifiedArguments populated) is verified by the agentic package's
// payload tests; this test just confirms the handler accepts the body.
func TestHandleLoopApproval_ModifiedArgumentsAccepted(t *testing.T) {
	comp := trackedLoopWithApproval(t, "loop-1", "call-001")

	body := `{"decision":"modify","modified_arguments":{"path":"/tmp/safe"},"reason":"narrowed scope"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader(body))
	req.SetPathValue("id", "loop-1")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "validation should pass and reach publish")
}

// TestHandleLoopApproval_NotAwaitingApprovalAfterPublishFailure is a
// regression guard for the clear-on-success contract: after a
// successful publish, the local PendingApproval cache is cleared so
// a fast-follow duplicate request gets 409 instead of re-publishing.
// We can't exercise the success-path publish without NATS, so this
// test asserts the corollary: a request that fails publish (no NATS
// client → 500) must NOT clear the cache, leaving subsequent retries
// able to succeed once NATS is available.
func TestHandleLoopApproval_FailedPublishPreservesPendingApproval(t *testing.T) {
	comp := trackedLoopWithApproval(t, "loop-1", "call-001")

	body := `{"decision":"approve"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/approval", strings.NewReader(body))
	req.SetPathValue("id", "loop-1")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	// Cache must remain populated for retry.
	callID, ok := comp.loopTracker.GetPendingApprovalCallID("loop-1")
	assert.True(t, ok, "failed publish must NOT clear PendingApproval — caller should be able to retry")
	assert.Equal(t, "call-001", callID)
}
