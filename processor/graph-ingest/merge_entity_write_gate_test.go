package graphingest

// merge_entity_write_gate_test.go — gh#562 write-cost follow-up. MergeEntity
// no longer runs its own full contract pass over the incoming candidate; the
// MarshalEntityState write gate inside the CAS closure is the single
// authoritative full-contract pass per committed candidate (both branches
// marshal a superset of the incoming triples). These tests pin the preserved
// invariant: an invalid candidate NEVER commits, on either closure branch, and
// the failure blames the CALLER's candidate (not resident state — no
// graph-state-reset classification, no poison latch) when the stored entity is
// canonical.

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

func TestMergeEntity_InvalidCandidateNeverCommits(t *testing.T) {
	validID := "acme.ops.test.system.widget.001"
	validPredicate := semantictest.Predicate(t, "test", "state", "value")

	badPredicateTriples := []message.Triple{
		{Subject: validID, Predicate: validPredicate, Object: "ok"},
		{Subject: validID, Predicate: "not-canonical", Object: "x"}, // predicate-audit:invalid {"kind":"candidate-predicate","value":"not-canonical","reason":"arity"}
	}
	badSubjectTriples := []message.Triple{
		{Subject: validID, Predicate: validPredicate, Object: "ok"},
		{Subject: "bad", Predicate: validPredicate, Object: "x"},
	}

	cases := []struct {
		name    string
		triples []message.Triple
	}{
		{name: "noncanonical predicate", triples: badPredicateTriples},
		{name: "noncanonical subject", triples: badSubjectTriples},
	}

	for _, tc := range cases {
		t.Run("merge branch "+tc.name, func(t *testing.T) {
			c, bucket := createTestComponentWithMockKVBucket(t)
			resident := &graph.EntityState{ID: validID, Version: 1, Triples: []message.Triple{
				{Subject: validID, Predicate: validPredicate, Object: "resident"},
			}}
			require.NoError(t, c.MergeEntity(context.Background(), resident))
			storedBefore, ok := bucket.data[validID]
			require.True(t, ok, "resident seed must exist")

			err := c.MergeEntity(context.Background(), &graph.EntityState{
				ID: validID, Triples: tc.triples,
			})
			require.Error(t, err, "invalid candidate must not merge")
			require.True(t, errs.IsInvalid(err), "candidate rejection must classify invalid, got: %v", err)
			var stateErr *graph.StateContractError
			require.False(t, errors.As(err, &stateErr),
				"canonical resident state must not be blamed (no reset classification)")
			require.Equal(t, int64(0), c.entityPoisonSize.Load(),
				"caller-invalid candidate must not be inventoried as poison")

			storedAfter := bucket.data[validID]
			require.Equal(t, storedBefore.revision, storedAfter.revision, "no write may commit")
			require.Equal(t, storedBefore.value, storedAfter.value, "stored bytes must be untouched")
		})

		t.Run("create branch "+tc.name, func(t *testing.T) {
			c, bucket := createTestComponentWithMockKVBucket(t)

			err := c.MergeEntity(context.Background(), &graph.EntityState{
				ID: validID, Triples: tc.triples,
			})
			require.Error(t, err, "invalid candidate must not create")
			require.True(t, errs.IsInvalid(err), "candidate rejection must classify invalid, got: %v", err)

			_, exists := bucket.data[validID]
			require.False(t, exists, "rejected create branch must leave the key absent")
		})
	}
}
