package agentictools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// allEffectDeclarations is every state a tool definition's Effect can be in:
// the four declared members plus undeclared. Enforcement outcomes must be
// identical across all five.
var allEffectDeclarations = []agentic.ToolEffect{
	"", // undeclared
	agentic.ToolEffectUnknown,
	agentic.ToolEffectReadOnly,
	agentic.ToolEffectMutating,
	agentic.ToolEffectExternal,
}

// newEffectBlindnessComponent builds a component whose single registered tool
// declares the supplied effect, with the global allowlist and the
// approval-required set configured by name.
func newEffectBlindnessComponent(t *testing.T, toolName string, effect agentic.ToolEffect, allowed, approvalRequired []string) interface {
	Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
} {
	t.Helper()

	config := agentictools.DefaultConfig()
	config.AllowedTools = allowed
	config.ApprovalRequired = approvalRequired

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	comp, err := agentictools.NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	registrar, ok := comp.(interface {
		RegisterToolExecutor(executor agentictools.ToolExecutor) error
	})
	if !ok {
		t.Fatal("component does not implement RegisterToolExecutor")
	}
	if err := registrar.RegisterToolExecutor(&effectExecutor{defs: []agentic.ToolDefinition{def(toolName, effect)}}); err != nil {
		t.Fatalf("RegisterToolExecutor: %v", err)
	}
	executor, ok := comp.(interface {
		Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
	})
	if !ok {
		t.Fatal("component does not implement Execute")
	}
	return executor
}

// TestApprovalGating_IsBlindToEffect is the "approval enforcement is not
// weakened" acceptance test, asserted in BOTH directions.
//
// Proving the field changes nothing IS this increment's contract: effect
// metadata is descriptive, and the configured approval-required NAME set stays
// the sole authority. A one-directional test (only that a dangerous tool still
// gates) would miss the more likely regression — a `read_only` declaration
// quietly buying a tool out of a gate an operator configured deliberately.
func TestApprovalGating_IsBlindToEffect(t *testing.T) {
	t.Parallel()

	t.Run("a read_only tool named in the approval set still gates", func(t *testing.T) {
		t.Parallel()
		filter := agentictools.NewApprovalFilter([]string{"gated_tool"})
		result := filter.FilterToolCalls("loop-1", []agentic.ToolCall{{ID: "c1", Name: "gated_tool"}})

		if len(result.Rejected) != 1 {
			t.Fatalf("rejected %d calls, want 1 — a read_only declaration must not buy a tool out of a configured gate", len(result.Rejected))
		}
		if len(result.Approved) != 0 {
			t.Errorf("approved %d calls, want 0", len(result.Approved))
		}
	})

	t.Run("an external_effect tool not named is not gated", func(t *testing.T) {
		t.Parallel()
		filter := agentictools.NewApprovalFilter([]string{"some_other_tool"})
		result := filter.FilterToolCalls("loop-1", []agentic.ToolCall{{ID: "c1", Name: "dangerous_tool"}})

		if len(result.Approved) != 1 {
			t.Fatalf("approved %d calls, want 1 — an external_effect declaration must not add a gate the operator did not configure", len(result.Approved))
		}
		if len(result.Rejected) != 0 {
			t.Errorf("rejected %d calls, want 0", len(result.Rejected))
		}
	})
}

// TestAdmission_IsBlindToEffect runs the same call through the production
// admission seam (Component.Execute → admitToolCall: global allowlist + per-loop
// advertised set) once per effect declaration, and requires every outcome to be
// identical.
//
// The matrix covers admitted, globally-refused, and per-loop-refused, so a
// hypothetical effect-derived branch could not hide in one arm.
func TestAdmission_IsBlindToEffect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		allowed    []string
		call       agentic.ToolCall
		wantErr    bool
		wantResult string
	}{
		{
			name:       "admitted: on the allowlist, no advertised set",
			allowed:    []string{"probe_tool"},
			call:       agentic.ToolCall{ID: "c1", Name: "probe_tool"},
			wantErr:    false,
			wantResult: "ok",
		},
		{
			name:    "refused: not on the global allowlist",
			allowed: []string{"some_other_tool"},
			call:    agentic.ToolCall{ID: "c2", Name: "probe_tool"},
			wantErr: true,
		},
		{
			name:    "refused: outside the loop's advertised set",
			allowed: []string{"probe_tool"},
			call: agentic.ToolCall{
				ID: "c3", Name: "probe_tool", LoopID: "loop-1",
				Metadata: map[string]any{agentic.MetadataKeyAdvertisedTools: []any{"another_tool"}},
			},
			wantErr: true,
		},
		{
			name:    "admitted: inside the loop's advertised set",
			allowed: []string{"probe_tool"},
			call: agentic.ToolCall{
				ID: "c4", Name: "probe_tool", LoopID: "loop-1",
				Metadata: map[string]any{agentic.MetadataKeyAdvertisedTools: []any{"probe_tool"}},
			},
			wantErr:    false,
			wantResult: "ok",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			type outcome struct {
				errored bool
				errText string
				content string
			}
			outcomes := make(map[agentic.ToolEffect]outcome, len(allEffectDeclarations))

			for _, effect := range allEffectDeclarations {
				executor := newEffectBlindnessComponent(t, "probe_tool", effect, tc.allowed, nil)
				result, err := executor.Execute(context.Background(), tc.call)
				outcomes[effect] = outcome{errored: err != nil, errText: result.Error, content: result.Content}
			}

			// Every declaration must produce the expected outcome...
			for effect, got := range outcomes {
				if got.errored != tc.wantErr {
					t.Errorf("effect %q: errored = %v, want %v (error text: %q)", effect, got.errored, tc.wantErr, got.errText)
				}
				if !tc.wantErr && got.content != tc.wantResult {
					t.Errorf("effect %q: content = %q, want %q", effect, got.content, tc.wantResult)
				}
			}

			// ...and, more strongly, all outcomes must be identical to each
			// other, so a future effect-derived branch cannot pass by changing
			// every arm consistently with the expectation above.
			baseline := outcomes[""]
			for effect, got := range outcomes {
				if got != baseline {
					t.Errorf("effect %q produced a different admission outcome than an undeclared tool:\n  got      %+v\n  baseline %+v",
						effect, got, baseline)
				}
			}
		})
	}
}
