package agentictools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
)

// approvalProbeExecutor advertises definitions with caller-chosen effects.
type approvalProbeExecutor struct {
	defs []agentic.ToolDefinition
}

func (e *approvalProbeExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "ok"}, nil
}

func (e *approvalProbeExecutor) ListTools() []agentic.ToolDefinition { return e.defs }

func probeDef(name string, effect agentic.ToolEffect) agentic.ToolDefinition {
	return agentic.ToolDefinition{
		Name:        name,
		Description: "approval probe tool",
		Parameters:  map[string]any{"type": "object"},
		Effect:      effect,
	}
}

func newApprovalProbeComponent(t *testing.T, approvalRequired []string) *Component {
	t.Helper()

	cfg := DefaultConfig()
	cfg.ApprovalRequired = approvalRequired
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	created, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp, ok := created.(*Component)
	if !ok {
		t.Fatalf("NewComponent returned %T, want *Component", created)
	}
	return comp
}

// TestApprovalFilter_IsBlindToEffect_ThroughTheComponent asserts that the
// component's approval gate is derived from the configured NAME set alone, and
// that registering tools of any effect never changes it.
//
// This test is IN-PACKAGE on purpose. The approval filter runs inside
// handleToolCall — a NATS-message-driven, unexported seam that an external test
// cannot reach — so the observable that matters is the component's own
// approvalFilter after registration. An external test that builds an
// ApprovalFilter directly and calls FilterToolCalls cannot fail this way,
// because ToolCall carries no effect and no ToolDefinition is involved: it
// asserts a property it never varies. That earlier version was caught by
// review, mutation-proved incapable, and replaced by this.
//
// The mutation this must catch is the shape of the DEFERRED follow-up (gh#808):
// rebuilding approvalFilter at registration from the configured names plus every
// non-read_only tool. If that ever lands unintentionally, this fails.
func TestApprovalFilter_IsBlindToEffect_ThroughTheComponent(t *testing.T) {
	t.Parallel()

	allEffects := []agentic.ToolEffect{
		"", // undeclared
		agentic.ToolEffectUnknown,
		agentic.ToolEffectReadOnly,
		agentic.ToolEffectMutating,
		agentic.ToolEffectExternal,
	}

	t.Run("no approval configured stays no approval, whatever the effects", func(t *testing.T) {
		t.Parallel()
		comp := newApprovalProbeComponent(t, nil)
		if comp.approvalFilter != nil {
			t.Fatal("component built with no approval_required already has an approval filter")
		}

		defs := make([]agentic.ToolDefinition, 0, len(allEffects))
		for i, effect := range allEffects {
			defs = append(defs, probeDef(toolNameForIndex(i), effect))
		}
		if err := comp.RegisterToolExecutor(&approvalProbeExecutor{defs: defs}); err != nil {
			t.Fatalf("RegisterToolExecutor: %v", err)
		}

		// The sharp assertion: an operator who configured no approval gate must
		// still have none after registering an external_effect tool. Effect is
		// descriptive; it does not manufacture a control.
		if comp.approvalFilter != nil {
			t.Error("registering tools created an approval filter the operator did not configure — " +
				"effect metadata must not derive enforcement in this increment (see gh#808)")
		}
	})

	t.Run("the configured name set is exactly what gates", func(t *testing.T) {
		t.Parallel()
		const gated = "gated_probe_tool"
		comp := newApprovalProbeComponent(t, []string{gated})
		if comp.approvalFilter == nil {
			t.Fatal("component built with approval_required has no approval filter")
		}

		// gated_probe_tool declares read_only — the permissive direction. It
		// must still gate. Every other tool declares something more severe and
		// must NOT gate, because no operator named it.
		defs := []agentic.ToolDefinition{probeDef(gated, agentic.ToolEffectReadOnly)}
		for i, effect := range allEffects {
			defs = append(defs, probeDef(toolNameForIndex(i), effect))
		}
		if err := comp.RegisterToolExecutor(&approvalProbeExecutor{defs: defs}); err != nil {
			t.Fatalf("RegisterToolExecutor: %v", err)
		}

		for _, def := range defs {
			result := comp.approvalFilter.FilterToolCalls("loop-1", []agentic.ToolCall{{ID: "c1", Name: def.Name}})
			wantGated := def.Name == gated
			gotGated := len(result.Rejected) == 1
			if gotGated != wantGated {
				t.Errorf("tool %q (effect %q): gated = %v, want %v — the approval gate must follow the configured name set, not the declared effect",
					def.Name, def.Effect, gotGated, wantGated)
			}
		}
	})
}

// toolNameForIndex gives each effect variant a distinct registrable name;
// RegisterExecutor refuses duplicates.
func toolNameForIndex(i int) string {
	return "probe_tool_" + string(rune('a'+i))
}
