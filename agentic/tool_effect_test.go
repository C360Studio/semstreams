package agentic_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

func toolEffectRegistry(t *testing.T) *payloadregistry.Registry {
	t.Helper()
	return payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
}

// TestToolEffect_Canonical covers the fail-safe resolution rule from every
// direction: declared members return themselves, and BOTH undeclared (empty)
// and unrecognized values resolve to unknown — never to read_only.
//
// The "never read_only" assertion is explicit rather than implied. The whole
// point of gh#749 is that missing metadata must not imply safety, so a test
// that only asserted `== unknown` would still pass a future implementation that
// happened to spell its permissive default differently.
func TestToolEffect_Canonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input agentic.ToolEffect
		want  agentic.ToolEffect
	}{
		{"declared unknown", agentic.ToolEffectUnknown, agentic.ToolEffectUnknown},
		{"declared read_only", agentic.ToolEffectReadOnly, agentic.ToolEffectReadOnly},
		{"declared mutating", agentic.ToolEffectMutating, agentic.ToolEffectMutating},
		{"declared external_effect", agentic.ToolEffectExternal, agentic.ToolEffectExternal},
		{"undeclared (empty)", "", agentic.ToolEffectUnknown},
		{"unrecognized spelling", "destructive", agentic.ToolEffectUnknown},
		{"near-miss of a real member", "readonly", agentic.ToolEffectUnknown},
		{"case variant", "READ_ONLY", agentic.ToolEffectUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.input.Canonical()
			if got != tt.want {
				t.Errorf("ToolEffect(%q).Canonical() = %q, want %q", tt.input, got, tt.want)
			}
			if tt.want == agentic.ToolEffectUnknown && got == agentic.ToolEffectReadOnly {
				t.Errorf("ToolEffect(%q) resolved to read_only — absent or unrecognized metadata must never imply read-only", tt.input)
			}
		})
	}
}

// TestToolEffect_Known pins the asymmetry that registration depends on: empty is
// NOT known (it is undeclared, which registration accepts) while every declared
// member is. Collapsing those two states is exactly what would let a typo
// register silently.
func TestToolEffect_Known(t *testing.T) {
	t.Parallel()

	known := []agentic.ToolEffect{
		agentic.ToolEffectUnknown,
		agentic.ToolEffectReadOnly,
		agentic.ToolEffectMutating,
		agentic.ToolEffectExternal,
	}
	for _, e := range known {
		if !e.Known() {
			t.Errorf("ToolEffect(%q).Known() = false, want true", e)
		}
	}

	notKnown := []agentic.ToolEffect{"", "destructive", "read only", "External_Effect"}
	for _, e := range notKnown {
		if e.Known() {
			t.Errorf("ToolEffect(%q).Known() = true, want false", e)
		}
	}
}

// TestToolDefinition_EffectJSONRoundTrip round-trips every effect value through
// the PRODUCTION decoder (payload registry + message.NewDecoder), not an
// anonymous json.Unmarshal into the same struct — a cast-based round-trip proves
// the struct is symmetric with itself, which is not the property that matters on
// the wire.
//
// The absent case is the load-bearing one: `omitempty` means an undeclared tool
// serializes with no `effect` key at all, and the decoded value must resolve to
// unknown rather than being mistaken for a classification.
func TestToolDefinition_EffectJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		effect       agentic.ToolEffect
		wantDecoded  agentic.ToolEffect
		wantResolved agentic.ToolEffect
	}{
		{"read_only survives", agentic.ToolEffectReadOnly, agentic.ToolEffectReadOnly, agentic.ToolEffectReadOnly},
		{"mutating survives", agentic.ToolEffectMutating, agentic.ToolEffectMutating, agentic.ToolEffectMutating},
		{"external_effect survives", agentic.ToolEffectExternal, agentic.ToolEffectExternal, agentic.ToolEffectExternal},
		{"explicit unknown survives", agentic.ToolEffectUnknown, agentic.ToolEffectUnknown, agentic.ToolEffectUnknown},
		{"absent decodes empty and resolves unknown", "", "", agentic.ToolEffectUnknown},
		{"unrecognized is preserved on the struct and resolves unknown", "destructive", "destructive", agentic.ToolEffectUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := &agentic.TaskMessage{
				TaskID: "task-effect-1",
				Role:   "general",
				Model:  "fast",
				Prompt: "p",
				Tools: []agentic.ToolDefinition{{
					Name:        "probe_tool",
					Description: "a tool used to pin the effect wire shape",
					Parameters:  map[string]any{"type": "object"},
					Effect:      tt.effect,
				}},
			}

			wire, err := message.NewBaseMessage(original.Schema(), original, "tool-effect-test").MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}

			dec := message.NewDecoder(toolEffectRegistry(t))
			decoded, err := dec.Decode(wire)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}

			got, ok := decoded.Payload().(*agentic.TaskMessage)
			if !ok {
				t.Fatalf("payload = %T, want *agentic.TaskMessage", decoded.Payload())
			}
			if len(got.Tools) != 1 {
				t.Fatalf("decoded %d tools, want 1", len(got.Tools))
			}

			if got.Tools[0].Effect != tt.wantDecoded {
				t.Errorf("decoded Effect = %q, want %q", got.Tools[0].Effect, tt.wantDecoded)
			}
			if resolved := got.Tools[0].Effect.Canonical(); resolved != tt.wantResolved {
				t.Errorf("resolved Effect = %q, want %q", resolved, tt.wantResolved)
			}
			if tt.wantResolved == agentic.ToolEffectUnknown && got.Tools[0].Effect.Canonical() == agentic.ToolEffectReadOnly {
				t.Error("an absent or unrecognized effect resolved to read_only over the wire")
			}
		})
	}
}

// TestToolDefinition_AbsentEffectOmitsTheKey proves the `omitempty` claim the
// round-trip above depends on. If the tag were dropped, an undeclared tool would
// serialize `"effect":""`, and a consumer distinguishing absent from empty would
// silently change behavior.
func TestToolDefinition_AbsentEffectOmitsTheKey(t *testing.T) {
	t.Parallel()

	def := &agentic.TaskMessage{
		TaskID: "task-omit",
		Role:   "general",
		Model:  "fast",
		Prompt: "p",
		Tools:  []agentic.ToolDefinition{{Name: "probe_tool", Parameters: map[string]any{"type": "object"}}},
	}
	wire, err := message.NewBaseMessage(def.Schema(), def, "tool-effect-test").MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if got := string(wire); strings.Contains(got, `"effect"`) {
		t.Errorf("undeclared tool serialized an effect key: %s", got)
	}
}

// TestToolDefinition_ValidateRejectsUnknownEffect pins that Validate stays
// truthful about the enum. Note this method has no production caller today —
// registration enforces the same rule directly, because Validate additionally
// requires Parameters and registration deliberately does not.
func TestToolDefinition_ValidateRejectsUnknownEffect(t *testing.T) {
	t.Parallel()

	base := func(effect agentic.ToolEffect) agentic.ToolDefinition {
		return agentic.ToolDefinition{
			Name:       "probe_tool",
			Parameters: map[string]any{"type": "object"},
			Effect:     effect,
		}
	}

	if err := base("destructive").Validate(); err == nil {
		t.Error("Validate accepted an unrecognized effect")
	}
	if err := base("").Validate(); err != nil {
		t.Errorf("Validate rejected an undeclared (empty) effect: %v", err)
	}
	for _, e := range []agentic.ToolEffect{
		agentic.ToolEffectUnknown, agentic.ToolEffectReadOnly,
		agentic.ToolEffectMutating, agentic.ToolEffectExternal,
	} {
		if err := base(e).Validate(); err != nil {
			t.Errorf("Validate rejected declared member %q: %v", e, err)
		}
	}
}
