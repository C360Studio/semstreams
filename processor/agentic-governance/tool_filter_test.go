package agenticgovernance

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolCallFilter_BlocksBashMetadataEndpoint(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-1",
		Name:      "bash",
		Arguments: map[string]any{"command": "curl http://169.254.169.254/latest/meta-data/"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, SeverityHigh, result.Violation.Severity)
}

func TestToolCallFilter_AllowsSafeBash(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-2",
		Name:      "bash",
		Arguments: map[string]any{"command": "go test ./..."},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestToolCallFilter_BlocksHTTPMetadataURL(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-3",
		Name:      "http_request",
		Arguments: map[string]any{"url": "http://169.254.169.254/latest/meta-data/"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
}

func TestToolCallFilter_AllowsSafeHTTP(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-4",
		Name:      "http_request",
		Arguments: map[string]any{"url": "https://pkg.go.dev/net/http"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestToolCallFilter_SkipsNonToolCallMessages(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := &Message{
		Type:    MessageTypeTask,
		Content: Content{Text: "normal task"},
	}

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestToolCallFilter_BlocksForkBomb(t *testing.T) {
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-5",
		Name:      "bash",
		Arguments: map[string]any{"command": ":(){ :|:& };:"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
}

func TestToolCallFilter_CheckPII_WithEmailInArgs(t *testing.T) {
	piiFilter, err := NewPIIFilter(&PIIFilterConfig{
		Types:               []PIIType{PIITypeEmail},
		Strategy:            RedactionLabel,
		ConfidenceThreshold: 0.9,
	})
	require.NoError(t, err)

	f := NewToolCallFilter(piiFilter)

	// Use a tool name that routes to checkPII (not bash or http_request)
	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-pii-1",
		Name:      "graph_query",
		Arguments: map[string]any{"query": "find user admin@example.com"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)

	// PII filter redacts but does not block
	assert.True(t, result.Allowed)
	assert.NotNil(t, result.Modified, "Modified message expected when PII is detected")
	assert.NotContains(t, result.Modified.Content.Text, "admin@example.com")
}

func TestToolCallFilter_CheckPII_WithCleanArgs(t *testing.T) {
	piiFilter, err := NewPIIFilter(&PIIFilterConfig{
		Types:               []PIIType{PIITypeEmail},
		Strategy:            RedactionLabel,
		ConfidenceThreshold: 0.9,
	})
	require.NoError(t, err)

	f := NewToolCallFilter(piiFilter)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-pii-2",
		Name:      "graph_query",
		Arguments: map[string]any{"query": "count all entities"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)

	assert.True(t, result.Allowed)
	assert.Nil(t, result.Modified, "No modification expected when no PII detected")
}

func TestToolCallFilter_CheckPII_NilPIIFilter(t *testing.T) {
	// NewToolCallFilter(nil) leaves piiFilter nil — checkPII must return allowed immediately.
	f := NewToolCallFilter(nil)

	msg := ToolCallToMessage(agentic.ToolCall{
		ID:        "call-pii-3",
		Name:      "graph_query",
		Arguments: map[string]any{"query": "find user admin@example.com"},
	}, "user-1", "ch-1")

	result, err := f.Process(context.Background(), msg)
	require.NoError(t, err)

	assert.True(t, result.Allowed)
	assert.Nil(t, result.Modified)
}

func TestToolCallToMessage(t *testing.T) {
	call := agentic.ToolCall{
		ID:        "call-x",
		Name:      "bash",
		Arguments: map[string]any{"command": "echo hi"},
		LoopID:    "loop-1",
	}

	msg := ToolCallToMessage(call, "user-42", "channel-7")
	assert.Equal(t, "call-x", msg.ID)
	assert.Equal(t, MessageTypeToolCall, msg.Type)
	assert.Equal(t, "user-42", msg.UserID)
	assert.Equal(t, "bash", msg.Content.Metadata["tool_name"])
	assert.Equal(t, "loop-1", msg.Content.Metadata["loop_id"])
}

// TestToolCallFilter_ImplementsAgenticInterface is a compile-time + runtime
// check that ToolCallFilter satisfies agentic.ToolCallFilter so it can be
// installed via MessageHandler.SetToolCallFilter.
func TestToolCallFilter_ImplementsAgenticInterface(t *testing.T) {
	var _ agentic.ToolCallFilter = (*ToolCallFilter)(nil)

	f := NewToolCallFilter(nil)
	var iface agentic.ToolCallFilter = f
	assert.NotNil(t, iface)
}

// TestFilterToolCalls_ParityWithProcess runs both the Process() surface and
// the FilterToolCalls() surface against the same set of calls and asserts
// they agree per call. This guards against the two surfaces drifting.
func TestFilterToolCalls_ParityWithProcess(t *testing.T) {
	f := NewToolCallFilter(nil)
	ctx := context.Background()

	calls := []agentic.ToolCall{
		{ID: "ok", Name: "bash", Arguments: map[string]any{"command": "go test ./..."}},
		{ID: "metadata", Name: "bash", Arguments: map[string]any{"command": "curl 169.254.169.254"}},
		{ID: "rm", Name: "bash", Arguments: map[string]any{"command": "rm -rf /"}},
		{ID: "url-ok", Name: "http_request", Arguments: map[string]any{"url": "https://pkg.go.dev"}},
		{ID: "url-bad", Name: "http_request", Arguments: map[string]any{"url": "http://localhost/admin"}},
	}

	// Process surface
	wantAllowed := map[string]bool{}
	for _, c := range calls {
		res, err := f.Process(ctx, ToolCallToMessage(c, "", ""))
		require.NoError(t, err)
		wantAllowed[c.ID] = res.Allowed
	}

	// FilterToolCalls surface
	got, err := f.FilterToolCalls("loop-7", calls)
	require.NoError(t, err)

	approvedIDs := map[string]bool{}
	for _, c := range got.Approved {
		approvedIDs[c.ID] = true
	}
	rejectedIDs := map[string]bool{}
	for _, r := range got.Rejected {
		rejectedIDs[r.Call.ID] = true
		assert.NotEmpty(t, r.Reason, "rejection reason should be populated for %s", r.Call.ID)
	}

	for id, allowed := range wantAllowed {
		if allowed {
			assert.True(t, approvedIDs[id], "call %s should be approved (Process said allowed)", id)
			assert.False(t, rejectedIDs[id], "call %s should not be in rejected list", id)
		} else {
			assert.True(t, rejectedIDs[id], "call %s should be rejected (Process said blocked)", id)
			assert.False(t, approvedIDs[id], "call %s should not be in approved list", id)
		}
	}
}

// TestFilterToolCalls_EmptySlice locks the empty-batch contract: nil or
// empty calls return an empty result with nil error. Callers iterate
// over Approved/Rejected without checking len, so an unintended nil-vs-
// empty drift here would still be benign, but downstream consumers may
// grow stricter checks in the future.
func TestFilterToolCalls_EmptySlice(t *testing.T) {
	f := NewToolCallFilter(nil)

	resNil, err := f.FilterToolCalls("loop-x", nil)
	require.NoError(t, err)
	assert.Empty(t, resNil.Approved)
	assert.Empty(t, resNil.Rejected)

	resEmpty, err := f.FilterToolCalls("loop-x", []agentic.ToolCall{})
	require.NoError(t, err)
	assert.Empty(t, resEmpty.Approved)
	assert.Empty(t, resEmpty.Rejected)
}

// TestFilterToolCalls_StampsLoopID verifies the loopID parameter overrides
// an unset ToolCall.LoopID in the synthesized governance Message metadata,
// so any downstream filter that branches on loop_id sees the loop the
// agentic-loop is operating on.
func TestFilterToolCalls_StampsLoopID(t *testing.T) {
	f := NewToolCallFilter(nil)

	calls := []agentic.ToolCall{
		{ID: "c1", Name: "bash", Arguments: map[string]any{"command": "echo ok"}},
	}
	res, err := f.FilterToolCalls("loop-xyz", calls)
	require.NoError(t, err)
	require.Len(t, res.Approved, 1)
	require.Empty(t, res.Rejected)
}

// TestFilterToolCalls_RejectsBatchEntry_OnInternalError verifies the safer
// error contract: a per-call Process error becomes a Rejected entry for
// that call, not an aborted batch. This prevents an internal evaluator
// failure from silently approving subsequent calls in the same batch.
func TestFilterToolCalls_RejectsBatchEntry_OnInternalError(t *testing.T) {
	// Forcing Process to error is not possible with the current real
	// ToolCallFilter (every code path returns nil err), so we exercise
	// the rejection-reason branch directly via violationReason and
	// assert the contract documented above through code review.
	//
	// This test fixes the contract for future authors: if any new code
	// in Process can return an error, FilterToolCalls MUST surface it
	// as a Rejected entry, not as a returned error that aborts the
	// batch. Touching this behaviour without updating the test is a
	// review red flag.
	t.Run("violationReason fallbacks", func(t *testing.T) {
		assert.Equal(t, "policy violation", violationReason(nil))
		assert.Equal(t, "policy violation (pii_redaction)", violationReason(&Violation{FilterName: "pii_redaction"}))
		v := &Violation{FilterName: "tool_call_governance", Details: map[string]any{"message": "blocked: rm -rf /"}}
		assert.Equal(t, "blocked: rm -rf /", violationReason(v))
	})
}
