package rule

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestApplyMessageSubstitutions_FlatFields(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"loop_id":   "loop-abc",
		"call_id":   "call-001",
		"tool_name": "bash",
	}

	in := "loop=$message.loop_id call=$message.call_id tool=$message.tool_name"
	want := "loop=loop-abc call=call-001 tool=bash"

	if got := applyMessageSubstitutions(in, data); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Deep paths must resolve through nested maps. Mirrors the $entity.triple.X
// precedent — the substitution layer is the only place where tool-call
// payloads with nested `tool_args` can be templated into rule subjects/reasons.
func TestApplyMessageSubstitutions_DeepPath(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"tool_name": "bash",
		"tool_args": map[string]any{
			"command": "rm -rf /tmp/danger",
			"timeout": 30,
		},
	}

	in := "tool=$message.tool_name cmd=$message.tool_args.command timeout=$message.tool_args.timeout"
	want := "tool=bash cmd=rm -rf /tmp/danger timeout=30"

	if got := applyMessageSubstitutions(in, data); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Nil/empty data MUST leave every token literal so unresolvedTemplateVarRe
// surfaces the silent-pass. Authors get a warning instead of empty strings
// landing in downstream subjects/keys. Catches new event shapes that forgot
// to populate the field — the bug class the unresolved-warning regression
// guard exists for.
func TestApplyMessageSubstitutions_NilOrEmptyData(t *testing.T) {
	t.Parallel()

	in := "loop=$message.loop_id"

	if got := applyMessageSubstitutions(in, nil); got != in {
		t.Errorf("nil data: got %q, want unchanged %q", got, in)
	}
	if got := applyMessageSubstitutions(in, map[string]any{}); got != in {
		t.Errorf("empty data: got %q, want unchanged %q", got, in)
	}
}

// Missing terminal key, missing intermediate, and descent through a
// non-map intermediate must all leave the token literal. This is the
// class of silent-pass bug `$entity.triple.X` already defends against.
func TestApplyMessageSubstitutions_UnresolvedPathLeavesToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data map[string]any
		in   string
		want string
	}{
		{
			name: "missing terminal key",
			data: map[string]any{"loop_id": "loop-abc"},
			in:   "call=$message.call_id",
			want: "call=$message.call_id",
		},
		{
			name: "missing intermediate",
			data: map[string]any{"tool_name": "bash"},
			in:   "cmd=$message.tool_args.command",
			want: "cmd=$message.tool_args.command",
		},
		{
			name: "intermediate is not a map (string)",
			data: map[string]any{"tool_args": "not-a-map"},
			in:   "cmd=$message.tool_args.command",
			want: "cmd=$message.tool_args.command",
		},
		{
			name: "intermediate is not a map (number)",
			data: map[string]any{"tool_args": 42},
			in:   "cmd=$message.tool_args.command",
			want: "cmd=$message.tool_args.command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := applyMessageSubstitutions(tt.in, tt.data); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// Tokens with no resolution alongside resolved tokens must NOT eat into
// each other — partial-resolution is fine, but leftovers stay literal so
// the warning fires. Catches a regression where a greedy
// ReplaceAllString might overwrite preceding successful substitutions on
// failure.
func TestApplyMessageSubstitutions_MixedResolution(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"loop_id": "loop-abc",
	}

	in := "loop=$message.loop_id call=$message.call_id"
	want := "loop=loop-abc call=$message.call_id"

	if got := applyMessageSubstitutions(in, data); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Template with no $message.* tokens is unchanged regardless of data.
// Guards a future regex refactor against false-positive matches.
func TestApplyMessageSubstitutions_NoTokens(t *testing.T) {
	t.Parallel()

	data := map[string]any{"loop_id": "loop-abc"}
	in := "entity=$entity.id"
	if got := applyMessageSubstitutions(in, data); got != in {
		t.Errorf("got %q, want unchanged %q", got, in)
	}
}

// End-to-end through ExecutionContext.SubstituteVariables. Confirms the
// $message.* pass coexists with $entity.id and $caller.id resolution in
// the same template — the use case for ADR-039 tool-call governance
// reasons that splice caller + tool-call payload + entity.
func TestSubstituteVariables_Message_FullPipeline(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID: "c360.osh.agentic-loop.agent.execution.uuid-001",
		MessageData: map[string]any{
			"loop_id":   "loop-abc",
			"call_id":   "call-001",
			"tool_name": "bash",
			"tool_args": map[string]any{
				"command": "ls /tmp",
			},
		},
	}

	in := "rule fired on $entity.id for loop=$message.loop_id call=$message.call_id cmd=$message.tool_args.command"
	want := "rule fired on c360.osh.agentic-loop.agent.execution.uuid-001 for loop=loop-abc call=call-001 cmd=ls /tmp"

	if got := ec.SubstituteVariables(context.Background(), in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// $message.* in a verdict subject template — the canonical ADR-039 use
// case. Pins that the existing publish/action subject substitution
// pipeline correctly resolves $message.* tokens for routing.
func TestSubstituteVariables_Message_VerdictSubject(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		MessageData: map[string]any{
			"loop_id": "loop-abc",
			"call_id": "call-001",
		},
	}

	in := "agent.toolcall.rejected.$message.loop_id.$message.call_id"
	want := "agent.toolcall.rejected.loop-abc.call-001"

	if got := ec.SubstituteVariables(context.Background(), in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Unresolved $message.<missing> MUST fire the unresolvedTemplateVarRe
// warning via the same path as missing $entity.triple.X. This is the
// silent-pass guard the ADR specifically called out — catches new event
// shapes that forgot to populate a field at publish time.
//
// Not t.Parallel(): slog.SetDefault mutates package-global state, and
// any other parallel test that captures slog.Default() at setup races
// against our SetDefault/Cleanup window. See the same discipline at
// TestExecutionContext_SubstituteVariables_WarnsOnUnresolved in
// actions_test.go. Caught on the beta.72 Go-bump CI run when scheduler
// timing shifted enough to surface the latent race.
func TestSubstituteVariables_Message_UnresolvedFieldWarns(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ec := &ExecutionContext{
		MessageData: map[string]any{"loop_id": "loop-abc"},
	}

	in := "loop=$message.loop_id missing=$message.absent_field"
	want := "loop=loop-abc missing=$message.absent_field"

	got := ec.SubstituteVariables(context.Background(), in)
	if got != want {
		t.Errorf("substitution: got %q, want %q", got, want)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "Unresolved template variables") {
		t.Errorf("expected unresolved-template warning, got log: %s", logOutput)
	}
	if !strings.Contains(logOutput, "$message.absent_field") {
		t.Errorf("expected $message.absent_field in warning, got log: %s", logOutput)
	}
}

// $message.* alongside $entity.* alongside $caller.* — all three
// namespaces resolve independently in the same template. Coexistence
// property a "reorder the substitution passes" refactor would silently
// break.
func TestSubstituteVariables_Message_CoexistsWithOtherNamespaces(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID: "c360.osh.agentic-loop.agent.execution.uuid-001",
		Caller: &CallerContext{
			ID:   "alice",
			Role: "operator",
		},
		MessageData: map[string]any{"tool_name": "bash"},
	}

	in := "caller=$caller.id role=$caller.role entity=$entity.instance tool=$message.tool_name"
	want := "caller=alice role=operator entity=uuid-001 tool=bash"

	if got := ec.SubstituteVariables(context.Background(), in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Confirms unresolvedTemplateVarRe matches $message.<field> so the
// warning surfaces missing tokens. Mirror of the entity-parts regex
// guard test in entity_substitution_test.go.
func TestSubstituteVariables_Message_RegexMatchesTokens(t *testing.T) {
	t.Parallel()

	in := "loop=$message.loop_id deep=$message.tool_args.command"
	leftovers := unresolvedTemplateVarRe.FindAllString(in, -1)
	if len(leftovers) != 2 {
		t.Fatalf("expected 2 $message.* matches, got %d: %v", len(leftovers), leftovers)
	}
	if leftovers[0] != "$message.loop_id" {
		t.Errorf("first match: got %q want %q", leftovers[0], "$message.loop_id")
	}
	if leftovers[1] != "$message.tool_args.command" {
		t.Errorf("second match: got %q want %q", leftovers[1], "$message.tool_args.command")
	}
}
