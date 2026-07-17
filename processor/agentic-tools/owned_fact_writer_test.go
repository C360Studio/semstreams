package agentictools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

func TestOwnedPredicates(t *testing.T) {
	entityID := semantictest.EntityID(t, "test", "agentic-tools", "owned-facts", "reader", "run", "001")
	tests := []struct {
		name    string
		triples []message.Triple
		prefix  string
		want    []string
	}{
		{
			name: "prefix scopes to owned package, sibling predicates excluded",
			triples: []message.Triple{
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-1"), Object: "v"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-2"), Object: "v"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "core", "identity", "type"), Object: "v"},     // other owner — must NOT be returned
				{Subject: entityID, Predicate: semantictest.Predicate(t, "lifecycle", "status", "phase"), Object: "v"}, // lifecycle — must NOT be returned
			},
			prefix: "change.abc.",
			want:   []string{"change.abc.task-1", "change.abc.task-2"},
		},
		{
			name: "distinct: multi-valued predicate collapses to one entry",
			triples: []message.Triple{
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "tag"), Object: "a"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "tag"), Object: "b"},
			},
			prefix: "change.abc.",
			want:   []string{"change.abc.tag"},
		},
		{
			name: "sorted output regardless of triple order",
			triples: []message.Triple{
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-3"), Object: "v"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-1"), Object: "v"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-2"), Object: "v"},
			},
			prefix: "change.abc.",
			want:   []string{"change.abc.task-1", "change.abc.task-2", "change.abc.task-3"},
		},
		{
			name: "empty prefix returns every predicate (single-owner escape hatch)",
			triples: []message.Triple{
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-1"), Object: "v"},
				{Subject: entityID, Predicate: semantictest.Predicate(t, "core", "identity", "type"), Object: "v"},
			},
			prefix: "",
			want:   []string{"change.abc.task-1", "core.identity.type"},
		},
		{
			name: "empty predicate skipped",
			triples: []message.Triple{
				{Subject: entityID, Predicate: "", Object: "v"}, // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
				{Subject: entityID, Predicate: semantictest.Predicate(t, "change", "abc", "task-1"), Object: "v"},
			},
			prefix: "change.abc.",
			want:   []string{"change.abc.task-1"},
		},
		{
			name:    "no match returns empty, not nil-panic",
			triples: []message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "other", "fact", "predicate"), Object: "v"}},
			prefix:  "change.abc.",
			want:    []string{},
		},
		{
			name:    "no triples returns empty",
			triples: nil,
			prefix:  "change.abc.",
			want:    []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ownedPredicates(tc.triples, tc.prefix)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestReadOwnedPredicates_RejectsEmptyPrefix locks the typed guard: an unscoped
// read is refused BEFORE any RPC (nil client is never dereferenced), so a tool
// can never accidentally clear predicates it does not own on a shared entity.
func TestReadOwnedPredicates_RejectsEmptyPrefix(t *testing.T) {
	w := &natsOwnedFactWriter{client: nil}
	_, err := w.ReadOwnedPredicates(context.Background(), "acme.spec.run.change.001", "")
	require.Error(t, err, "empty ownedPrefix must be rejected")
	assert.Contains(t, err.Error(), "non-empty", "error must explain the scoping requirement")
}
