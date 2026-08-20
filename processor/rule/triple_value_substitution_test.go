package rule

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

// gh#519 / rule-evaluation-completeness — the `.value` suffix completes
// the scalar-graceful family alongside `.length` (count) and `.triples`
// (JSON array). Unlike those two, `.value`'s suffix recognition is
// ARITY-DISAMBIGUATED via vocabulary.ParsePredicate rather than always
// applying: see applyTripleValueSubstitutions in execution_context.go
// for the full contract. These tests exercise the substitution layer
// directly (unit-level, mirrors triple_length/triple_triples style);
// TestExpressionRuleEvaluateEntityState_TripleValueSubstitution in
// expression_factory_test.go drives the production wire per house rule.

// TestTripleValueSubstitution_TruthTable covers the substitution-layer
// resolution semantics: present predicate → scalar object; absent
// predicate → empty string; non-string Object renders via the same
// scalar formatting the generic loop uses; multi-valued predicate →
// first match in entity.Triples order.
func TestTripleValueSubstitution_TruthTable(t *testing.T) {
	cases := []struct {
		name     string
		template string
		entity   *gtypes.EntityState
		want     string
	}{
		{
			name:     "present predicate resolves to scalar object value",
			template: "$entity.triple.openspec.change.revision.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "openspec", "change", "revision"), Object: "r42"},
				},
			},
			want: "r42",
		},
		{
			name:     "absent predicate resolves to empty string",
			template: "$entity.triple.openspec.change.revision.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "other", "unrelated", "field"), Object: "x"},
				},
			},
			want: "",
		},
		{
			name:     "non-string Object renders via canonical scalar formatting (fmt %v)",
			template: "$entity.triple.metrics.sample.count.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "metrics", "sample", "count"), Object: 42},
				},
			},
			want: "42",
		},
		{
			name:     "boolean Object renders via canonical scalar formatting",
			template: "$entity.triple.metrics.sample.flagged.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "metrics", "sample", "flagged"), Object: true},
				},
			},
			want: "true",
		},
		{
			name:     "multi-valued predicate resolves to first triple in entity.Triples order",
			template: "$entity.triple.gather.child.completed.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "loop_a"},
					{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "loop_b"},
				},
			},
			want: "loop_a",
		},
		{
			name:     "agent.lineage.<key>.value resolves (hyphenated property segment)",
			template: "$entity.triple.agent.lineage.research-reviewer.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "agent.lineage.research-reviewer", Object: "loop-abc-123"},
				},
			},
			want: "loop-abc-123",
		},
		{
			name:     "literal predicate ending in .value is not truncated (2-part prefix doesn't parse)",
			template: "$entity.triple.metrics.sample.value",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "metrics", "sample", "value"), Object: "7"},
				},
			},
			want: "7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ec := &ExecutionContext{Entity: tc.entity}
			got := ec.SubstituteVariables(context.Background(), tc.template)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestTripleValueSubstitution_NilEntityResolvesToEmpty verifies the
// message-path rule shape (no entity in scope) resolves to "" rather
// than tripping the unresolved-template warning — mirrors .length /
// .triples's nil-entity handling, except .value stays SILENT (no Debug
// log either): the entire point of .value is warn-free absence.
func TestTripleValueSubstitution_NilEntityResolvesToEmpty(t *testing.T) {
	ec := &ExecutionContext{} // Entity nil
	got := ec.SubstituteVariables(context.Background(), "$entity.triple.openspec.change.revision.value")
	assert.Equal(t, "", got)
}

// TestTripleValueSubstitution_RelatedNamespace mirrors the entity path
// for $related.triple.<predicate>.value on pair-rules.
func TestTripleValueSubstitution_RelatedNamespace(t *testing.T) {
	ec := &ExecutionContext{
		Related: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "test", "fixture", "tag"), Object: "primary"},
			},
		},
	}
	got := ec.SubstituteVariables(context.Background(), "$related.triple.test.fixture.tag.value")
	assert.Equal(t, "primary", got)
}

// TestTripleValueSubstitution_AbsenceDoesNotWarn is the acceptance test
// for the "absence is silent" spec scenario: substitution must resolve
// to "" AND must not fire the unresolved-template-variable WARN. The
// existing bare $entity.triple.<pred> form DOES warn on absence (by
// design, unchanged) — .value's whole purpose is to give rule authors a
// warn-free scalar read for optional predicates.
//
// Not t.Parallel(): slog.SetDefault mutates package-global state — see
// the same discipline at TestSubstituteVariables_Message_UnresolvedFieldWarns.
func TestTripleValueSubstitution_AbsenceDoesNotWarn(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "openspec", "validated", "flag"), Object: "r42"},
			},
		},
	}

	got := ec.SubstituteVariables(context.Background(), "$entity.triple.openspec.change.revision.value")
	assert.Equal(t, "", got)
	assert.Empty(t, buf.String(), "absent .value predicate must not log the unresolved-template warning")
}

// TestTripleValueSubstitution_LiteralFallbackAbsenceStillWarns proves the
// bare-form behavior is genuinely unchanged: when the arity
// disambiguation falls back to treating ".value" as part of a literal
// (non-3-part-prefixed) predicate name, and that literal predicate is
// ALSO absent, the existing unresolved-template warning still fires —
// exactly as it would for any other unresolved literal predicate today.
//
// Not t.Parallel(): slog.SetDefault mutates package-global state.
func TestTripleValueSubstitution_LiteralFallbackAbsenceStillWarns(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// "a.b" is only 2 segments — does not parse as a canonical 3-part
	// predicate, so the whole sequence "a.b.value" is the literal
	// predicate under test. It is not present on the entity, so this
	// must behave exactly like any other unresolved literal predicate.
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "other", "unrelated", "field"), Object: "x"},
			},
		},
	}

	got := ec.SubstituteVariables(context.Background(), "$entity.triple.a.b.value")
	assert.Equal(t, "$entity.triple.a.b.value", got, "non-3-part prefix must not be treated as a .value suffix")
	assert.Contains(t, buf.String(), "Unresolved template variables")
}

// TestTripleValueSubstitution_SuffixIsTheFinalDotValue documents the
// regex-capture semantics called out in tripleValueRe's doc comment: for
// a token containing ".value" mid-string as well as at the end, the
// SUFFIX split point is the FINAL ".value" — the arity check runs
// against everything before it, not the first occurrence.
func TestTripleValueSubstitution_SuffixIsTheFinalDotValue(t *testing.T) {
	// "a.value.b" is a legal 3-part predicate (domain="a", category=
	// "value", property="b"); the trailing ".value" is the actual
	// suffix, so the candidate under arity test is "a.value.b", not "a".
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "a.value.b", Object: "resolved"},
			},
		},
	}
	got := ec.SubstituteVariables(context.Background(), "$entity.triple.a.value.b.value")
	assert.Equal(t, "resolved", got)
}

// TestTripleValueSubstitution_RunsBeforeGenericSubstitution verifies the
// pre-pass discipline: the .value suffix is resolved before the generic
// $entity.triple.<predicate> loop stringifies the Object, otherwise the
// suffix would be orphaned. Mirrors the equivalent .length/.triples
// tests for the same hazard.
func TestTripleValueSubstitution_RunsBeforeGenericSubstitution(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "openspec", "change", "revision"), Object: "r42"},
			},
		},
	}
	got := ec.SubstituteVariables(context.Background(), "plain=$entity.triple.openspec.change.revision value=$entity.triple.openspec.change.revision.value")
	assert.Equal(t, "plain=r42 value=r42", got)
}

// TestTripleValueSubstitution_MultipleTokensInOneTemplate verifies
// regex-replace handles N occurrences in a single template, mixing
// present and absent predicates.
func TestTripleValueSubstitution_MultipleTokensInOneTemplate(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "openspec", "change", "revision"), Object: "r42"},
			},
		},
	}
	tmpl := "rev=$entity.triple.openspec.change.revision.value missing=$entity.triple.openspec.change.author.value"
	got := ec.SubstituteVariables(context.Background(), tmpl)
	assert.Equal(t, "rev=r42 missing=", got)
}

// TestTripleValueSubstitution_BareFormUnchanged is a regression guard:
// the bare $entity.triple.<predicate> form (no .value suffix) must
// continue to warn on absence exactly as before — this feature is
// strictly additive.
//
// Not t.Parallel(): slog.SetDefault mutates package-global state.
func TestTripleValueSubstitution_BareFormUnchanged(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "other", "unrelated", "field"), Object: "x"},
			},
		},
	}

	got := ec.SubstituteVariables(context.Background(), "$entity.triple.openspec.change.revision")
	assert.Equal(t, "$entity.triple.openspec.change.revision", got)
	if !strings.Contains(buf.String(), "Unresolved template variables") {
		t.Errorf("expected bare-form absence to still warn, got log: %s", buf.String())
	}
}
