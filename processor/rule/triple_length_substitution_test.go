package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// #149 — .length suffix substitution for list-typed triples. Closes
// the dynamic-counter gap so rule 03 in configs/rules/example-fan-out/
// doesn't need a hardcoded N per decomposer fan-out width.

// TestTripleLengthSubstitution_TruthTable covers every shape the
// resolver handles, mirroring the coerceTripleObjectToStrings shape
// truth-table plus the missing-predicate and entity-nil paths.
func TestTripleLengthSubstitution_TruthTable(t *testing.T) {
	cases := []struct {
		name     string
		template string
		entity   *gtypes.EntityState
		want     string
	}{
		{
			name:     "native []string of 3 → 3",
			template: "$entity.triple.subtopics.length",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "subtopics", Object: []string{"a", "b", "c"}},
				},
			},
			want: "3",
		},
		{
			name:     "[]any of 5 → 5 (JSON-unmarshal shape)",
			template: "$entity.triple.subtopics.length",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "subtopics", Object: []any{"a", "b", "c", "d", "e"}},
				},
			},
			want: "5",
		},
		{
			name:     "JSON-encoded string list → integer count (decide tool wire shape)",
			template: "$entity.triple.coordinator.decision.subtopics.length",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "coordinator.decision.subtopics", Object: `["hydraulics","pneumatics","electrics","sensors"]`},
				},
			},
			want: "4",
		},
		{
			name:     "empty list → 0",
			template: "$entity.triple.subtopics.length",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "subtopics", Object: []any{}},
				},
			},
			want: "0",
		},
		{
			name:     "missing predicate → 0",
			template: "$entity.triple.subtopics.length",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "other.predicate", Object: "x"},
				},
			},
			want: "0",
		},
		{
			name:     "entity nil → 0",
			template: "$entity.triple.subtopics.length",
			entity:   nil,
			want:     "0",
		},
		{
			name:     "deep predicate path (dots) → count",
			template: "$entity.triple.coordinator.decision.subtopics.length",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "coordinator.decision.subtopics", Object: []string{"a", "b"}},
				},
			},
			want: "2",
		},
		{
			name:     "interpolated into a larger string",
			template: "expected=$entity.triple.subtopics.length children",
			entity: &gtypes.EntityState{
				Triples: []message.Triple{
					{Predicate: "subtopics", Object: []string{"a", "b", "c"}},
				},
			},
			want: "expected=3 children",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ec := &ExecutionContext{
				EntityID: "e.1",
				Entity:   tc.entity,
			}
			got := ec.SubstituteVariables(tc.template)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestTripleLengthSubstitution_NonListShapeEmitsErrorSentinel
// covers the loudness contract: when a predicate is found but its
// Object isn't list-shaped (operator typo, predicate IS a scalar),
// the token is replaced with a self-describing error sentinel that:
//
//	(a) survives subsequent substitution passes (no $ prefix so the
//	    generic triple-substitution loop can't mangle it),
//	(b) is visible in any prompt/subject the operator inspects,
//	(c) errors downstream coerceToInt rather than silently parsing
//	    as 0.
//
// The sentinel shape `[ERROR_LENGTH_NOT_LIST:<predicate>]` is a
// behaviour contract — operator tooling that pattern-matches on this
// shape can surface a structured diagnosis.
func TestTripleLengthSubstitution_NonListShapeEmitsErrorSentinel(t *testing.T) {
	ec := &ExecutionContext{
		EntityID: "e.1",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				// agent.role is a scalar string, not a list — author
				// shouldn't be using .length against it.
				{Predicate: "agent.role", Object: "researcher"},
			},
		},
	}
	got := ec.SubstituteVariables("$entity.triple.agent.role.length")
	assert.Equal(t, "[ERROR_LENGTH_NOT_LIST:agent.role]", got,
		"non-list Object must replace the .length token with the error sentinel so author errors don't silently parse as 0 downstream")
}

// TestTripleLengthSubstitution_RelatedNamespace verifies the
// $related.triple.<predicate>.length path resolves against
// ec.Related — symmetric with the $entity case.
func TestTripleLengthSubstitution_RelatedNamespace(t *testing.T) {
	ec := &ExecutionContext{
		EntityID:  "e.1",
		RelatedID: "r.1",
		Related: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "items", Object: []string{"x", "y"}},
			},
		},
	}
	got := ec.SubstituteVariables("$related.triple.items.length")
	assert.Equal(t, "2", got)
}

// TestTripleLengthSubstitution_NotInterferingWithOrdinarySubstitution
// closes the regression where a careless implementation could
// stringify the list first (via the generic triple-substitution loop)
// and then strand the .length suffix. The pre-pass ordering matters.
func TestTripleLengthSubstitution_NotInterferingWithOrdinarySubstitution(t *testing.T) {
	ec := &ExecutionContext{
		EntityID: "e.1",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "subtopics", Object: []string{"a", "b", "c"}},
				{Predicate: "agent.role", Object: "coordinator"},
			},
		},
	}
	// Both forms in one template: ordinary triple substitution AND
	// .length substitution. Both must resolve.
	got := ec.SubstituteVariables("role=$entity.triple.agent.role count=$entity.triple.subtopics.length")
	assert.Equal(t, "role=coordinator count=3", got)
}

func TestTripleLengthSubstitution_CanonicalHyphenatedPredicate(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{Entity: &gtypes.EntityState{Triples: []message.Triple{
		{Predicate: "coordinator.decision.next-actions", Object: []string{"review", "merge"}},
	}}}

	got := ec.SubstituteVariables("$entity.triple.coordinator.decision.next-actions.length")
	assert.Equal(t, "2", got)
}
