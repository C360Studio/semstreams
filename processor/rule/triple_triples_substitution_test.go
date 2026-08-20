package rule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

// ADR-048 PR 4 — .triples suffix substitution for list enumeration.
// Closes the GH #151 consumer-side enumeration gap so fan-out join
// rules can thread sibling-loop IDs into a synthesizer's properties
// without a per-role tool surface (the agent receives a JSON array
// and parses it via the canonical persona-prose template).

// TestTripleTriplesSubstitution_TruthTable covers Pattern A
// (multiple triples, scalar Objects) + Pattern B (single triple,
// list Object) + edge cases. Verifies the JSON-array output format
// decided in ADR-048 (never CSV — commas-in-values is a latent
// production-bug class the framework refuses to introduce).
func TestTripleTriplesSubstitution_TruthTable(t *testing.T) {
	cases := []struct {
		name     string
		template string
		entity   *gtypes.EntityState
		want     string
	}{
		{
			name:     "Pattern A — N triples with scalar Objects → JSON array of N strings",
			template: "$entity.triple.gather.child.completed.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "loop_a"},
					{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "loop_b"},
					{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "loop_c"},
				},
			},
			want: `["loop_a","loop_b","loop_c"]`,
		},
		{
			name:     "Pattern B — single triple with []string Object → JSON array of elements",
			template: "$entity.triple.test.fixture.subtopics.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "test", "fixture", "subtopics"), Object: []string{"hydraulics", "pneumatics", "electrics"}},
				},
			},
			want: `["hydraulics","pneumatics","electrics"]`,
		},
		{
			name:     "Pattern B — single triple with []any Object (JSON-unmarshal shape)",
			template: "$entity.triple.test.fixture.subtopics.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "test", "fixture", "subtopics"), Object: []any{"a", "b"}},
				},
			},
			want: `["a","b"]`,
		},
		{
			name:     "Pattern B — JSON-encoded string list (decide tool wire shape)",
			template: "$entity.triple.coordinator.decision.subtopics.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "coordinator", "decision", "subtopics"), Object: `["alpha","beta","gamma"]`},
				},
			},
			want: `["alpha","beta","gamma"]`,
		},
		{
			name:     "Predicate not present → empty JSON array",
			template: "$entity.triple.test.never.set.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "test", "other", "predicate"), Object: "value"},
				},
			},
			want: "[]",
		},
		{
			name:     "Single Pattern A triple → JSON array of one element",
			template: "$entity.triple.gather.child.completed.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "only_child"},
				},
			},
			want: `["only_child"]`,
		},
		{
			name:     "Empty Pattern B list → empty JSON array",
			template: "$entity.triple.test.fixture.subtopics.triples",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "test", "fixture", "subtopics"), Object: []string{}},
				},
			},
			want: "[]",
		},
		{
			name:     "Mixed Pattern A + non-string scalar → stringified into array",
			template: "$entity.triple.test.fixture.counts.triples",
			entity: &gtypes.EntityState{
				// Numeric scalar Objects get stringified via %v —
				// same path that []any non-string elements take in
				// coerceTripleObjectToStrings.
				Triples: []message.Triple{
					{Predicate: semantictest.Predicate(t, "test", "fixture", "counts"), Object: 42},
					{Predicate: semantictest.Predicate(t, "test", "fixture", "counts"), Object: 17},
				},
			},
			want: `["42","17"]`,
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

// TestTripleTriplesSubstitution_NilEntityResolvesToEmpty verifies
// the message-path rule shape (no entity in scope) resolves to "[]"
// rather than tripping the unresolved-template warning.
func TestTripleTriplesSubstitution_NilEntityResolvesToEmpty(t *testing.T) {
	ec := &ExecutionContext{} // Entity nil
	got := ec.SubstituteVariables(context.Background(), "$entity.triple.test.fixture.anything.triples")
	assert.Equal(t, "[]", got)
}

// TestTripleTriplesSubstitution_RelatedNamespace mirrors the entity
// path for $related.triple.<predicate>.triples on pair-rules.
func TestTripleTriplesSubstitution_RelatedNamespace(t *testing.T) {
	ec := &ExecutionContext{
		Related: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "test", "fixture", "tags"), Object: "x"},
				{Predicate: semantictest.Predicate(t, "test", "fixture", "tags"), Object: "y"},
			},
		},
	}
	got := ec.SubstituteVariables(context.Background(), "$related.triple.test.fixture.tags.triples")
	assert.Equal(t, `["x","y"]`, got)
}

// TestTripleTriplesSubstitution_RunsBeforeGenericSubstitution verifies
// the pre-pass discipline: the .triples suffix is resolved before the
// generic $entity.triple.<predicate> loop stringifies the Object,
// otherwise the suffix would be orphaned and trip the unresolved-
// template warning. Mirrors the .length test for the same hazard.
func TestTripleTriplesSubstitution_RunsBeforeGenericSubstitution(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "test", "fixture", "subtopics"), Object: []string{"a", "b", "c"}},
			},
		},
	}
	// Template uses both the plain triple reference AND the .triples
	// suffix. If the generic substitution ran first, the .triples
	// suffix would be orphaned on the stringified `[a b c]` value.
	got := ec.SubstituteVariables(context.Background(), "plain=$entity.triple.test.fixture.subtopics triples=$entity.triple.test.fixture.subtopics.triples")
	// Plain reference stringifies the list (existing behavior);
	// .triples renders as JSON array.
	assert.Contains(t, got, `triples=["a","b","c"]`)
}

// TestTripleTriplesSubstitution_MultipleTokensInOneTemplate verifies
// regex-replace handles N occurrences in a single template — the
// realistic shape is a prompt (natural-language) string with
// multiple .triples references threaded in.
//
// JSON-template substitution (e.g. "{\"k\":\"$entity.triples...\"}")
// produces nested-JSON-as-string, NOT a flattened embedded array.
// That's the contract per ADR-048: substitution does verbatim text
// replacement; the receiving agent gets the JSON array as a string
// property value and parses it via the canonical persona-prose
// template.
func TestTripleTriplesSubstitution_MultipleTokensInOneTemplate(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "test", "fixture", "first"), Object: "a"},
				{Predicate: semantictest.Predicate(t, "test", "fixture", "first"), Object: "b"},
				{Predicate: semantictest.Predicate(t, "test", "fixture", "second"), Object: "x"},
			},
		},
	}
	prompt := "Sibling IDs: $entity.triple.test.fixture.first.triples; Subtopics: $entity.triple.test.fixture.second.triples"
	got := ec.SubstituteVariables(context.Background(), prompt)
	assert.Equal(t, `Sibling IDs: ["a","b"]; Subtopics: ["x"]`, got)
}

func TestTripleTriplesSubstitution_CanonicalHyphenatedPredicate(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{Entity: &gtypes.EntityState{Triples: []message.Triple{
		{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "child-a"},
		{Predicate: semantictest.Predicate(t, "gather", "child", "completed"), Object: "child-b"},
	}}}

	got := ec.SubstituteVariables(context.Background(), "$entity.triple.gather.child.completed.triples")
	assert.Equal(t, `["child-a","child-b"]`, got)
}
