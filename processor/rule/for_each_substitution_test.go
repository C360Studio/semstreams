package rule

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// TestSubstituteVariablesWithIterVar_BindsOverlay verifies the
// per-iteration overlay (#134 / ADR-046 Phase 1). $<varName> in the
// template resolves to the bound value; framework-namespace tokens
// still resolve as usual. Empty varName degenerates to the standard
// SubstituteVariables path.
func TestSubstituteVariablesWithIterVar_BindsOverlay(t *testing.T) {
	ec := &ExecutionContext{EntityID: "acme.ops.robot.gcs.drone.001"}

	got := ec.SubstituteVariablesWithIterVar(context.Background(), "subtopic=$subtopic for $entity.id", "subtopic", "hydraulics")
	assert.Equal(t, "subtopic=hydraulics for acme.ops.robot.gcs.drone.001", got)
}

// TestSubstituteVariablesWithIterVar_EmptyVarNameDegenerates verifies
// that calling with an empty varName behaves exactly like
// SubstituteVariables (no overlay applied). Lets the for_each loop's
// non-iter path share a single call site.
func TestSubstituteVariablesWithIterVar_EmptyVarNameDegenerates(t *testing.T) {
	ec := &ExecutionContext{EntityID: "acme.ops.robot.gcs.drone.001"}

	got := ec.SubstituteVariablesWithIterVar(context.Background(), "hello $entity.id", "", "ignored")
	assert.Equal(t, "hello acme.ops.robot.gcs.drone.001", got)
}

// TestResolveListValue_TripleObjectShapes verifies the three concrete
// triple-Object shapes the for_each resolver handles: native []string,
// []any (typical JSON unmarshal shape), and JSON-encoded string. All
// must produce the same []string output so authors don't have to
// reason about how the list was stored upstream.
func TestResolveListValue_TripleObjectShapes(t *testing.T) {
	cases := []struct {
		name string
		obj  any
		want []string
	}{
		{
			name: "native []string",
			obj:  []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "[]any (JSON unmarshal shape)",
			obj:  []any{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "JSON-encoded string (decide tool wire shape)",
			obj:  `["a","b","c"]`,
			want: []string{"a", "b", "c"},
		},
		{
			name: "JSON-encoded with whitespace",
			obj:  `  ["a", "b", "c"]  `,
			want: []string{"a", "b", "c"},
		},
		{
			name: "empty list still resolves",
			obj:  []any{},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ec := &ExecutionContext{
				Entity: &gtypes.EntityState{
					Triples: []message.Triple{
						{Predicate: "coordinator.decision.subtopics", Object: tc.obj, Timestamp: time.Now()},
					},
				},
			}
			got, ok := ec.ResolveListValue("$entity.triple.coordinator.decision.subtopics")
			require.True(t, ok, "should resolve list")
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveListValue_NonListReturnsFalse verifies that a reference
// pointing at a scalar-typed triple Object resolves but returns
// ok=false — the for_each caller logs and degenerates to a single
// dispatch with the iter-var unbound (author-error-loud, not silent).
func TestResolveListValue_NonListReturnsFalse(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "test.agent.role", Object: "researcher", Timestamp: time.Now()},
			},
		},
	}
	got, ok := ec.ResolveListValue("$entity.triple.test.agent.role")
	assert.False(t, ok, "scalar string should not resolve as list")
	assert.Nil(t, got)
}

// TestResolveListValue_MissingPredicateReturnsFalse covers the case
// where the operator referenced a predicate that doesn't exist on
// the entity at fire time (race with late-arriving triples, typo, etc.).
func TestResolveListValue_MissingPredicateReturnsFalse(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "test.agent.role", Object: "researcher", Timestamp: time.Now()},
			},
		},
	}
	got, ok := ec.ResolveListValue("$entity.triple.test.nonexistent.predicate")
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestResolveListValue_UnsupportedNamespaceReturnsFalse ensures Phase
// 1's narrow surface — only $entity.triple.* and $related.triple.*
// are supported list references. $message.* / $state.* deliberately
// don't resolve as lists today.
func TestResolveListValue_UnsupportedNamespaceReturnsFalse(t *testing.T) {
	ec := &ExecutionContext{
		MessageData: map[string]any{
			"items": []any{"a", "b"},
		},
	}
	got, ok := ec.ResolveListValue("$message.items")
	assert.False(t, ok, "$message.* list refs not supported in Phase 1")
	assert.Nil(t, got)
}

// TestResolveListValue_MalformedJSONReturnsFalse verifies that a
// string-typed Object that LOOKS like a JSON list but doesn't parse
// degenerates to ok=false. The author sees a Warn from the for_each
// caller; doesn't silently iterate a garbled list.
func TestResolveListValue_MalformedJSONReturnsFalse(t *testing.T) {
	ec := &ExecutionContext{
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "test.broken.list", Object: `["a", "b" "c]`, Timestamp: time.Now()},
			},
		},
	}
	got, ok := ec.ResolveListValue("$entity.triple.test.broken.list")
	assert.False(t, ok, "malformed JSON should not resolve as list")
	assert.Nil(t, got)
}

// TestCoerceTripleObjectToStrings_JSONRoundTrip closes the wire-path
// contract: a list emitted as []string → JSON-marshaled → stored as a
// string-typed triple Object → resolves back to the same []string.
// Mirrors the decide tool's emission path verbatim.
func TestCoerceTripleObjectToStrings_JSONRoundTrip(t *testing.T) {
	original := []string{"hydraulics", "pneumatics", "electrics"}
	encoded, err := json.Marshal(original)
	require.NoError(t, err)

	got, ok := coerceTripleObjectToStrings(string(encoded))
	require.True(t, ok)
	assert.Equal(t, original, got)
}
