package agentictools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// globalRejectText is the pre-existing global-allowlist rejection message.
// semdev's gh#551 acceptance requires the per-loop rejection be DISTINGUISHABLE
// from this text, so its routing rules can tell "not in this deployment" apart
// from "not advertised to this loop".
const globalRejectText = "is not allowed"

// perLoopRejectText is the distinct per-loop (advertised-set) rejection marker.
const perLoopRejectText = "is not permitted for this loop (advertised tool set)"

// newAdvertisedTestComponent builds a component whose GLOBAL AllowedTools
// includes both "decide" and "create_change" (the gh#551 setup: one executor
// multiplexing several roles), with mock executors registered for both.
func newAdvertisedTestComponent(t *testing.T) interface {
	Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
} {
	t.Helper()

	config := agentictools.DefaultConfig()
	config.AllowedTools = []string{"decide", "create_change"}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	comp, err := agentictools.NewComponent(rawConfig, component.Dependencies{NATSClient: nil})
	if err != nil {
		t.Fatalf("NewComponent() failed: %v", err)
	}

	registrar, ok := comp.(interface {
		RegisterToolExecutor(executor agentictools.ToolExecutor) error
	})
	if !ok {
		t.Fatal("Component should implement RegisterToolExecutor")
	}
	for _, name := range []string{"decide", "create_change"} {
		if err := registrar.RegisterToolExecutor(&mockToolExecutor{
			name:          name,
			returnContent: name + " executed",
		}); err != nil {
			t.Fatalf("RegisterToolExecutor(%s) failed: %v", name, err)
		}
	}

	executor, ok := comp.(interface {
		Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
	})
	if !ok {
		t.Fatal("Component should implement Execute")
	}
	return executor
}

// TestExecute_AdvertisedTools_RejectsUnadvertised is the gh#551 acceptance
// case verbatim: global AllowedTools includes both "decide" and
// "create_change"; a loop advertised only ["decide"] (as []any — the
// JSON-decoded wire shape) emits "create_change" → REJECTED, with error text
// distinct from the global-allowlist rejection.
func TestExecute_AdvertisedTools_RejectsUnadvertised(t *testing.T) {
	executor := newAdvertisedTestComponent(t)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID:     "call-unadv",
		Name:   "create_change",
		LoopID: "loop-coordinator-1",
		Metadata: map[string]any{
			agentic.MetadataKeyAdvertisedTools: []any{"decide"},
		},
	})
	if err == nil {
		t.Fatal("Execute() must reject a tool outside the loop's advertised set")
	}
	if result.Error == "" {
		t.Fatal("Result.Error should not be empty for unadvertised tool")
	}
	if !strings.Contains(result.Error, perLoopRejectText) {
		t.Errorf("Result.Error = %q, want per-loop rejection containing %q", result.Error, perLoopRejectText)
	}
	if strings.Contains(result.Error, globalRejectText) {
		t.Errorf("per-loop rejection %q must be distinguishable from global rejection %q", result.Error, globalRejectText)
	}
}

// TestExecute_AdvertisedTools_AbsentAdmits confirms back-compat: the same
// call with NO advertised-set key is admitted (global allowlist only).
func TestExecute_AdvertisedTools_AbsentAdmits(t *testing.T) {
	executor := newAdvertisedTestComponent(t)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-absent",
		Name: "create_change",
	})
	if err != nil {
		t.Fatalf("Execute() with no advertised set should admit, got error: %v", err)
	}
	if result.Content != "create_change executed" {
		t.Errorf("Result.Content = %q, want %q", result.Content, "create_change executed")
	}
}

// TestExecute_AdvertisedTools_AdvertisedAdmits: a tool inside the advertised
// set is admitted (still subject to the global allowlist).
func TestExecute_AdvertisedTools_AdvertisedAdmits(t *testing.T) {
	executor := newAdvertisedTestComponent(t)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-adv-ok",
		Name: "decide",
		Metadata: map[string]any{
			agentic.MetadataKeyAdvertisedTools: []any{"decide"},
		},
	})
	if err != nil {
		t.Fatalf("Execute() with advertised tool should admit, got error: %v", err)
	}
	if result.Content != "decide executed" {
		t.Errorf("Result.Content = %q, want %q", result.Content, "decide executed")
	}
}

// TestExecute_AdvertisedTools_PresentButEmptyRejects: key present but empty
// (or coercing to empty) FAILS CLOSED — the IsKnownFilesystemPolicy precedent:
// a malformed value on a security control must not degrade to permissive.
func TestExecute_AdvertisedTools_PresentButEmptyRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
	}{
		{name: "empty list", raw: []any{}},
		{name: "non-list value", raw: "decide"},
		{name: "list of non-strings", raw: []any{42, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor := newAdvertisedTestComponent(t)

			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID:     "call-empty",
				Name:   "decide",
				LoopID: "loop-coordinator-2",
				Metadata: map[string]any{
					agentic.MetadataKeyAdvertisedTools: tc.raw,
				},
			})
			if err == nil {
				t.Fatal("Execute() with present-but-empty advertised set must fail closed")
			}
			if !strings.Contains(result.Error, perLoopRejectText) {
				t.Errorf("Result.Error = %q, want per-loop rejection containing %q", result.Error, perLoopRejectText)
			}
		})
	}
}

// TestExecute_AdvertisedTools_GlobalCheckStillApplies: a tool NOT in the
// global AllowedTools is rejected with the GLOBAL error text even when the
// loop advertised it — the per-loop set narrows, never widens.
func TestExecute_AdvertisedTools_GlobalCheckStillApplies(t *testing.T) {
	executor := newAdvertisedTestComponent(t)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-global",
		Name: "bash",
		Metadata: map[string]any{
			agentic.MetadataKeyAdvertisedTools: []any{"bash"},
		},
	})
	if err == nil {
		t.Fatal("Execute() must reject a tool outside the global allowlist")
	}
	if !strings.Contains(result.Error, globalRejectText) {
		t.Errorf("Result.Error = %q, want global rejection containing %q", result.Error, globalRejectText)
	}
	if strings.Contains(result.Error, perLoopRejectText) {
		t.Errorf("global rejection must not carry the per-loop marker: %q", result.Error)
	}
}

// TestExecute_AdvertisedTools_EnforcesAfterWireRoundTrip proves enforcement on
// the PRODUCTION wire shape: a ToolCall stamped with a native []string is
// marshalled through its BaseMessage envelope and decoded with the production
// decoder — landing the advertised set as []any — and the executor still
// rejects an unadvertised tool on the decoded call.
func TestExecute_AdvertisedTools_EnforcesAfterWireRoundTrip(t *testing.T) {
	call := &agentic.ToolCall{
		ID:     "call-wire",
		Name:   "create_change",
		LoopID: "loop-coordinator-3",
		Metadata: map[string]any{
			agentic.MetadataKeyAdvertisedTools: []string{"decide"},
		},
	}
	baseMsg := message.NewBaseMessage(call.Schema(), call, "advertised-tools-test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("marshal BaseMessage: %v", err)
	}

	dec := payloadbuiltins.NewTestDecoder(t)
	decoded, err := dec.Decode(data)
	if err != nil {
		t.Fatalf("production decode: %v", err)
	}
	decodedCall, ok := decoded.Payload().(*agentic.ToolCall)
	if !ok {
		t.Fatalf("decoded payload type = %T, want *agentic.ToolCall", decoded.Payload())
	}
	// Sanity: the wire lands the list as []any — the shape enforcement must handle.
	if _, ok := decodedCall.Metadata[agentic.MetadataKeyAdvertisedTools].([]any); !ok {
		t.Fatalf("expected []any after production decode, got %T",
			decodedCall.Metadata[agentic.MetadataKeyAdvertisedTools])
	}

	executor := newAdvertisedTestComponent(t)
	result, err := executor.Execute(context.Background(), *decodedCall)
	if err == nil {
		t.Fatal("Execute() must reject the decoded unadvertised call")
	}
	if !strings.Contains(result.Error, perLoopRejectText) {
		t.Errorf("Result.Error = %q, want per-loop rejection containing %q", result.Error, perLoopRejectText)
	}
}
