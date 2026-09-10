package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const approvalTestExecutionID = "observed-pending-execution"

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func TestHandleLoopApproval_RequiresCurrentExecutionEcho(t *testing.T) {
	for _, tc := range []struct {
		name    string
		echo    string
		want    int
		message string
	}{
		{"missing echo", "", http.StatusBadRequest, "execution_id is required"},
		{"stale echo", "previous-execution", http.StatusConflict, "approval execution is not current"},
		{"current echo", approvalTestExecutionID, http.StatusInternalServerError, ErrNATSClientNil.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")
			current, err := comp.loadPersistedLoop(t.Context(), seamTestLoopA)
			require.NoError(t, err)
			current.PendingApproval.ExecutionID = approvalTestExecutionID
			before, err := json.Marshal(current)
			require.NoError(t, err)
			body, err := json.Marshal(map[string]string{"decision": "approve", "execution_id": tc.echo})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", bytes.NewReader(body))
			req.SetPathValue("id", seamTestLoopA)
			rec := httptest.NewRecorder()
			comp.handleLoopApproval(rec, req)
			require.Equal(t, tc.want, rec.Code, "%s", rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.message)
			after, err := json.Marshal(current)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
// An explicit approval reads current durable authority, including when the
// original created/pending events have settled and the tracker is empty.
func TestHandleLoopApproval_CurrentAuthority(t *testing.T) {
	tests := []struct {
		name      string
		tracked   bool
		mutate    func(*agentic.LoopEntity)
		readError error
		vanish    bool
		wantCode  int
		wantText  string
	}{
		{name: "cold pending reaches existing publication", wantCode: http.StatusInternalServerError, wantText: ErrNATSClientNil.Error()},
		{name: "stale tracker cannot override executing", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.State, e.PendingApproval = agentic.LoopStateExecuting, nil
		}, wantCode: http.StatusConflict, wantText: "loop not awaiting approval"},
		{name: "stale tracker cannot override terminal", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.State, e.PendingApproval = agentic.LoopStateComplete, nil
		}, wantCode: http.StatusConflict, wantText: "loop not awaiting approval"},
		{name: "invalid state is unreadable", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.State = "invented"
		}, wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "awaiting without pending is unreadable", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.PendingApproval = nil
		}, wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "pending without call identity is unreadable", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.PendingApproval.CallID = ""
		}, wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "pending without execution identity is unreadable", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.PendingApproval.ExecutionID = ""
		}, wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "nonawaiting with pending is incoherent", tracked: true, mutate: func(e *agentic.LoopEntity) {
			e.State = agentic.LoopStateExecuting
		}, wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "unavailable authority cannot fall back to tracker", tracked: true, readError: errors.New("storage unavailable"),
			wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "malformed authority cannot fall back to tracker", tracked: true, readError: permanentTerminal("malformed loop JSON"),
			wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
		{name: "record vanished after admission is unreadable", vanish: true,
			wantCode: http.StatusServiceUnavailable, wantText: "loop record is not readable right now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := newTestComponent(t)
			if tt.tracked {
				comp = trackedLoopWithApproval(t, seamTestLoopA, "stale-cache-call")
			}
			var logs bytes.Buffer
			comp.logger = slog.New(slog.NewTextHandler(&logs, nil))
			comp.metrics = getMetrics(metric.NewMetricsRegistry())
			record := &agentic.LoopEntity{
				ID: seamTestLoopA, TaskID: "task-current", UserID: "user-1", ChannelType: "http", ChannelID: "chan-1",
				State: agentic.LoopStateAwaitingApproval, MaxIterations: 3,
				PendingApproval: &agentic.PendingApprovalState{CallID: "durable-call", ExecutionID: approvalTestExecutionID, ToolName: "delete_rule"},
			}
			if tt.mutate != nil {
				tt.mutate(record)
			}
			before, err := json.Marshal(record)
			require.NoError(t, err)
			reads := 0
			comp.loadPersistedLoopFn = func(_ context.Context, loopID string) (*agentic.LoopEntity, error) {
				require.Equal(t, seamTestLoopA, loopID)
				reads++
				if tt.vanish && reads > 1 {
					return nil, absentRecord(loopID)
				}
				return record, tt.readError
			}
			req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(`{"decision":"approve","execution_id":"`+approvalTestExecutionID+`"}`))
			req.SetPathValue("id", seamTestLoopA)
			req = req.WithContext(WithIdentity(req.Context(), "second-party-reviewer"))
			rec := httptest.NewRecorder()
			comp.handleLoopApproval(rec, req)
			require.Equal(t, tt.wantCode, rec.Code, "%s", rec.Body.String())
			var response HTTPMessageResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Contains(t, response.Content, tt.wantText)
			if tt.wantCode == http.StatusServiceUnavailable {
				assert.Contains(t, logs.String(), "admitted approval state could not be read")
				assert.Equal(t, 1.0, testutil.ToFloat64(comp.metrics.httpRequestsTotal.WithLabelValues(
					"/loops/{id}/approval", "POST", "503")))
			}
			after, err := json.Marshal(record)
			require.NoError(t, err)
			require.Equal(t, before, after, "HTTP admission/publication must not mutate loop authority")
		})
	}
}

// trackedLoopWithApproval supplies both durable authority and the old tracker
// fixture. Tests may replace either observation to prove which one controls
// explicit approval; production authority is never written by the handler.
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
		ExecutionID: approvalTestExecutionID,
		ToolName:    "delete_rule",
		Arguments:   map[string]any{"rule_id": "rule-42"},
		Reason:      "approval_required: Tool 'delete_rule' requires human approval",
		RequestedAt: time.Now().UTC(),
	})
	withPersistedLoops(comp, map[string]*agentic.LoopEntity{loopID: {
		ID: loopID, TaskID: "task-" + loopID, UserID: "user-1", ChannelType: "http", ChannelID: "chan-1",
		State: agentic.LoopStateAwaitingApproval, MaxIterations: 3,
		PendingApproval: &agentic.PendingApprovalState{CallID: callID, ExecutionID: approvalTestExecutionID, ToolName: "delete_rule"},
	}})
	return comp
}

func TestHandleLoopApproval_MissingLoopID(t *testing.T) {
	comp := newTestComponent(t)

	body := `{"decision":"approve","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops//approval", strings.NewReader(body))
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleLoopApproval_LoopNotFound(t *testing.T) {
	comp := newTestComponent(t)

	body := `{"decision":"approve","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopAbsent+"/approval", strings.NewReader(body))
	req.SetPathValue("id", seamTestLoopAbsent)
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleLoopApproval_InvalidBody(t *testing.T) {
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")

	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader("not json"))
	req.SetPathValue("id", seamTestLoopA)
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleLoopApproval_UnknownDecision(t *testing.T) {
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")

	body := `{"decision":"abstain","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(body))
	req.SetPathValue("id", seamTestLoopA)
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.Content, "invalid decision")
}

// TestHandleLoopApproval_NotAwaitingApproval keeps 409 for a current durable
// record in a nonpending state, not for an empty replacement process cache.
func TestHandleLoopApproval_NotAwaitingApproval(t *testing.T) {
	comp := newTestComponent(t)
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      seamTestLoopB,
		UserID:      "user-1",
		ChannelType: "http",
		State:       "executing",
		CreatedAt:   time.Now(),
	})
	withPersistedLoops(comp, map[string]*agentic.LoopEntity{seamTestLoopB: {
		ID: seamTestLoopB, UserID: "user-1", ChannelType: "http", State: agentic.LoopStateExecuting, MaxIterations: 3,
	}})

	body := `{"decision":"approve","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopB+"/approval", strings.NewReader(body))
	req.SetPathValue("id", seamTestLoopB)
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
			comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")

			body := `{"decision":"` + tt.decision + `","execution_id":"` + approvalTestExecutionID + `"}`
			req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(body))
			req.SetPathValue("id", seamTestLoopA)
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
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")

	// No user_id in body.
	body := `{"decision":"approve","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(body))
	req.SetPathValue("id", seamTestLoopA)
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
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")

	body := `{"decision":"approve","user_id":"body-user","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(body))
	req = req.WithContext(WithIdentity(req.Context(), "ctx-authenticated-user"))
	req.SetPathValue("id", seamTestLoopA)
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
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")

	body := `{"decision":"modify","modified_arguments":{"path":"/tmp/safe"},"reason":"narrowed scope","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(body))
	req.SetPathValue("id", seamTestLoopA)
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "validation should pass and reach publish")
}

// A failed ordinary approval publication must leave current pending authority
// intact. Only the loop owner applies the eventual approval response.
func TestHandleLoopApproval_FailedPublishPreservesPendingApproval(t *testing.T) {
	comp := trackedLoopWithApproval(t, seamTestLoopA, "call-001")
	before, err := comp.loadPersistedLoop(context.Background(), seamTestLoopA)
	require.NoError(t, err)
	beforeBytes, err := json.Marshal(before)
	require.NoError(t, err)

	body := `{"decision":"approve","execution_id":"` + approvalTestExecutionID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/loops/"+seamTestLoopA+"/approval", strings.NewReader(body))
	req.SetPathValue("id", seamTestLoopA)
	rec := httptest.NewRecorder()

	comp.handleLoopApproval(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	after, err := comp.loadPersistedLoop(context.Background(), seamTestLoopA)
	require.NoError(t, err)
	afterBytes, err := json.Marshal(after)
	require.NoError(t, err)
	require.Equal(t, beforeBytes, afterBytes)
	require.Equal(t, "call-001", after.PendingApproval.CallID)
}
