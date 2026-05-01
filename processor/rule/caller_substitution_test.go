package rule

import (
	"testing"
)

func TestApplyCallerSubstitutions_Populated(t *testing.T) {
	t.Parallel()

	cc := &CallerContext{
		ID:   "alice",
		Role: "viewer",
		Org:  "acme",
	}

	in := "$caller.id $caller.role $caller.org"
	want := "alice viewer acme"
	if got := applyCallerSubstitutions(in, cc); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Partial CallerContext: populated ID, empty Role and Org.
// Empty fields should render as the empty string, not the token literal.
func TestApplyCallerSubstitutions_Partial(t *testing.T) {
	t.Parallel()

	cc := &CallerContext{ID: "alice"}
	in := "$caller.id is $caller.role"
	want := "alice is "
	if got := applyCallerSubstitutions(in, cc); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Nil CallerContext is the no-op path — expression rules and cron rules
// that have no caller in scope should not panic, and the token must survive
// substitution intact so the unresolved-template warning in
// execution_context.go fires.
func TestApplyCallerSubstitutions_Nil(t *testing.T) {
	t.Parallel()

	in := "$caller.id"
	if got := applyCallerSubstitutions(in, nil); got != in {
		t.Errorf("got %q, want unchanged %q (nil CallerContext must not panic or substitute)", got, in)
	}
}

// Template with no $caller.* tokens must pass through unchanged.
func TestApplyCallerSubstitutions_NoTokens(t *testing.T) {
	t.Parallel()

	cc := &CallerContext{ID: "alice", Role: "admin", Org: "acme"}
	in := "entity=$entity.id state=$state.iteration"
	if got := applyCallerSubstitutions(in, cc); got != in {
		t.Errorf("got %q, want unchanged %q (no $caller.* tokens should be a no-op)", got, in)
	}
}

// Full path through ExecutionContext.SubstituteVariables with a populated Caller.
// Confirms the substitution fires at the right point in the pipeline
// alongside the $entity.* namespace.
func TestSubstituteVariables_CallerNamespace(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID: "acme.ops.robotics.gcs.drone.001",
		Caller: &CallerContext{
			ID:   "alice",
			Role: "viewer",
			Org:  "acme",
		},
	}

	in := "entity=$entity.id caller=$caller.id role=$caller.role org=$caller.org"
	want := "entity=acme.ops.robotics.gcs.drone.001 caller=alice role=viewer org=acme"
	if got := ec.SubstituteVariables(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// An expression rule (Caller == nil) that mistakenly references
// $caller.* tokens must surface them via the unresolved-template
// regex, NOT silently render as empty. Regression guard for
// cross-namespace leakage.
func TestSubstituteVariables_CallerNilTokenSurvives(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID: "acme.ops.robotics.gcs.drone.001",
		// Caller deliberately nil — we're a non-caller-aware rule
	}

	in := "$caller.id"
	out := ec.SubstituteVariables(in)
	if out != in {
		t.Errorf("got %q, want unchanged %q (caller-only $caller.* must not silently empty in non-caller-aware path)", out, in)
	}
}

// $caller.unknown_field must match unresolvedTemplateVarRe.
// This is the lint gate that catches author typos at warn-time instead of
// silently passing garbage into NATS subjects or KV keys.
func TestUnresolvedTemplateVarRe_CatchesCallerTypos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "unknown caller field triggers regex",
			input:   "$caller.unknown_field",
			matches: true,
		},
		{
			name:    "another unknown caller field",
			input:   "$caller.trust_score",
			matches: true,
		},
		{
			name:    "deep unknown caller path",
			input:   "$caller.some.nested.field",
			matches: true,
		},
		// Real $caller.* fields should also match the regex — they survive
		// substitution only when Caller is nil, which is the correct warn-on-nil
		// path (author put a $caller.* token in a rule that will never have
		// a CallerContext populated).
		{
			name:    "valid caller id field matches when unresolved",
			input:   "$caller.id",
			matches: true,
		},
		{
			name:    "valid caller role field matches when unresolved",
			input:   "$caller.role",
			matches: true,
		},
		{
			name:    "valid caller org field matches when unresolved",
			input:   "$caller.org",
			matches: true,
		},
		{
			name:    "non-caller namespace not matched",
			input:   "$now",
			matches: false,
		},
		{
			name:    "plain string not matched",
			input:   "no template here",
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := unresolvedTemplateVarRe.MatchString(tt.input)
			if got != tt.matches {
				t.Errorf("unresolvedTemplateVarRe.MatchString(%q) = %v, want %v", tt.input, got, tt.matches)
			}
		})
	}
}
