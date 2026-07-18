//go:build integration

// gh#562 — trusted RMW decode. graph-ingest's own read-modify-write reads
// use graph.UnmarshalEntityStateTrusted (no contract re-validation); the
// MarshalEntityState write gate remains the enforcement boundary.
//
// The load-bearing regression here is NO-LAUNDERING: resident noncanonical
// stored state (raw bytes written around MarshalEntityState — how poison
// arises in the field) must NOT ride through a valid mutation's merge into a
// committed write. The write gate validates every RMW output candidate, so
// the whole write is rejected and nothing is persisted. That guarantee never
// depended on read-side validation — these tests pass both before and after
// the trusted-decode swap.

package graphingest

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedResidentPoison writes a noncanonical EntityState's raw JSON directly
// into ENTITY_STATES via plain json.Marshal + Put, bypassing the
// MarshalEntityState write gate — the only way resident poison can exist.
// It asserts the fixture genuinely violates the contract before seeding.
func seedResidentPoison(t *testing.T, c *Component, state *graph.EntityState) []byte {
	t.Helper()
	ctx := t.Context()

	require.Error(t, graph.ValidateEntityStateContract(state),
		"fixture must be genuine poison — otherwise this test proves nothing")

	raw, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = c.entityBucket.Put(ctx, state.ID, raw)
	require.NoError(t, err)
	return raw
}

// requireStoredBytesUnchanged asserts the stored value and revision for id
// are exactly what they were at the captured baseline — no write committed
// (rejected mutation or true no-op).
func requireStoredBytesUnchanged(t *testing.T, c *Component, id string, wantBytes []byte, wantRevision uint64) {
	t.Helper()
	entry, err := c.entityBucket.Get(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, wantRevision, entry.Revision, "stored revision must not advance when no write commits")
	assert.True(t, bytes.Equal(wantBytes, entry.Value), "stored bytes must be untouched when no write commits")
}

// requireResetRequired asserts the mutation failure is attributed to the
// STORED state (graph-state-reset-required), not to the caller's valid
// candidate (invalid_request) — the gh#562 design-point-3 classification.
func requireResetRequired(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, ce.Code,
		"resident poison must classify as stored-state reset-required, not candidate-invalid")
}

func poisonPredicateState(id string) *graph.EntityState {
	now := time.Now()
	return &graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "test.valid.kind", Object: "sample", Timestamp: now, Confidence: 1.0},
			// Intentionally noncanonical predicate (upper-case segments) —
			// resident-poison fixture.
			{Subject: id, Predicate: "Test.Poison.Predicate", Object: "resident", Timestamp: now, Confidence: 1.0},
		},
		MessageType: message.Type{Domain: "test", Category: "poison", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}
}

// TestIntegration_RMWResidentPoison_WriteGateRejects drives VALID mutations
// through the production RMW lanes against an entity whose STORED state is
// poisoned. Every lane must reject the whole write (no laundering), persist
// nothing, and attribute the failure to the stored state.
func TestIntegration_RMWResidentPoison_WriteGateRejects(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	now := time.Now()

	t.Run("merge_entity", func(t *testing.T) {
		const id = "c360.test.rmw.poison.entity.001"
		raw := seedResidentPoison(t, c, poisonPredicateState(id))
		seeded, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		mergeErr := c.MergeEntity(ctx, &graph.EntityState{
			ID: id,
			Triples: []message.Triple{
				{Subject: id, Predicate: "test.merge.fresh", Object: "valid", Timestamp: now, Confidence: 1.0},
			},
			MessageType: message.Type{Domain: "test", Category: "poison", Version: "v1"},
		})
		requireResetRequired(t, mergeErr)
		requireStoredBytesUnchanged(t, c, id, raw, seeded.Revision)
	})

	t.Run("add_triple", func(t *testing.T) {
		const id = "c360.test.rmw.poison.entity.002"
		raw := seedResidentPoison(t, c, poisonPredicateState(id))
		seeded, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		addErr := c.AddTriple(ctx, message.Triple{
			Subject: id, Predicate: "test.add.fresh", Object: "valid", Timestamp: now, Confidence: 1.0,
		})
		requireResetRequired(t, addErr)
		requireStoredBytesUnchanged(t, c, id, raw, seeded.Revision)
	})

	t.Run("remove_triple", func(t *testing.T) {
		const id = "c360.test.rmw.poison.entity.005"
		raw := seedResidentPoison(t, c, poisonPredicateState(id))
		seeded, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		// Removing the CANONICAL predicate leaves the poisoned triple in the
		// candidate — the write gate rejects the whole write.
		removeErr := c.RemoveTriple(ctx, id, "test.valid.kind")
		requireResetRequired(t, removeErr)
		requireStoredBytesUnchanged(t, c, id, raw, seeded.Revision)
	})

	t.Run("add_triples_batch", func(t *testing.T) {
		const id = "c360.test.rmw.poison.entity.006"
		raw := seedResidentPoison(t, c, poisonPredicateState(id))
		seeded, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		written, failed, addErr := c.AddTriples(ctx, []message.Triple{
			{Subject: id, Predicate: "test.batch.fresh", Object: "valid", Timestamp: now, Confidence: 1.0},
		})
		require.Error(t, addErr)
		assert.Zero(t, written, "no triples may commit against a poisoned entity")
		// AddTriples' aggregate error stringifies per-subject causes into
		// FailedSubjects (pre-existing batch contract), so the reset-required
		// attribution is asserted on the per-subject detail rather than the
		// classified chain.
		require.Contains(t, failed, id)
		assert.Contains(t, failed[id], graph.ErrorCodeGraphStateResetRequired,
			"per-subject failure must attribute the reject to stored-state poison")
		requireStoredBytesUnchanged(t, c, id, raw, seeded.Revision)
	})

	t.Run("update_with_triples_handler", func(t *testing.T) {
		const id = "c360.test.rmw.poison.entity.003"
		raw := seedResidentPoison(t, c, poisonPredicateState(id))
		seeded, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		req := graph.UpdateEntityWithTriplesRequest{
			Entity: &graph.EntityState{
				ID:          id,
				MessageType: message.Type{Domain: "test", Category: "poison", Version: "v1"},
				Version:     2,
			},
			AddTriples: []message.Triple{
				{Subject: id, Predicate: "test.update.fresh", Object: "valid", Timestamp: now, Confidence: 1.0},
			},
			RequestID: "req-poison-update",
		}
		reqBytes, err := json.Marshal(req)
		require.NoError(t, err)

		respBytes, handlerErr := c.handleEntityUpdateWithTriples(ctx, reqBytes)
		requireClassifiedReject(t, respBytes, handlerErr, graph.ErrorCodeGraphStateResetRequired, "")
		requireStoredBytesUnchanged(t, c, id, raw, seeded.Revision)
	})
}

// TestIntegration_RemoveTriple_NoOpCommitsNoWrite pins RemoveTriple's no-op
// branch as a TRUE no-op (gh#562 review M1): when no triple matches the
// predicate, the CAS closure exits via sentinel BEFORE any KV write — no
// identity rewrite, no revision bump, no watcher re-fire. This holds for both
// healthy and poisoned stored state; for poison it also preserves the
// invariant that the closure's only WRITE exit is a MarshalEntityState-
// validated candidate.
func TestIntegration_RemoveTriple_NoOpCommitsNoWrite(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	now := time.Now()

	t.Run("healthy_entity", func(t *testing.T) {
		const id = "c360.test.rmw.noop.entity.001"
		require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{
			ID: id,
			Triples: []message.Triple{
				{Subject: id, Predicate: "test.valid.kind", Object: "sample", Timestamp: now, Confidence: 1.0},
			},
			MessageType: message.Type{Domain: "test", Category: "noop", Version: "v1"},
		}))
		before, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		require.NoError(t, c.RemoveTriple(ctx, id, "test.absent.predicate"),
			"removing an absent predicate is silent success")
		requireStoredBytesUnchanged(t, c, id, before.Value, before.Revision)
	})

	t.Run("poisoned_entity", func(t *testing.T) {
		const id = "c360.test.rmw.noop.entity.002"
		raw := seedResidentPoison(t, c, poisonPredicateState(id))
		seeded, err := c.entityBucket.Get(ctx, id)
		require.NoError(t, err)

		// The trusted read admits the poison; nothing matches the predicate,
		// so nothing is written — silent success with the stored poison bytes
		// untouched (no laundering, no poison revision appended, no watcher
		// re-fire).
		require.NoError(t, c.RemoveTriple(ctx, id, "test.absent.predicate"),
			"no-op remove on a poisoned entity is silent success with no write")
		requireStoredBytesUnchanged(t, c, id, raw, seeded.Revision)
	})
}

// TestIntegration_RMWTrustedDecode_ReadAdmitsPoison pins the post-gh#562
// behavior: the OWNER's own RMW read no longer errors on resident poison —
// the trusted decode admits it and the MarshalEntityState write gate is what
// catches (or, for a removal that eliminates the poison, releases) it.
//
// The poison here is a triple with a canonical predicate but a noncanonical
// SUBJECT, so a canonical RemoveTriple by that predicate produces a fully
// canonical candidate: the write commits and the graph is clean again. Before
// the swap this exact call failed on the read — this test is the behavioral
// proof the read-side validation is gone from the RMW lane.
func TestIntegration_RMWTrustedDecode_ReadAdmitsPoison(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	const id = "c360.test.rmw.poison.entity.004"
	now := time.Now()

	seedResidentPoison(t, c, &graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "test.valid.kind", Object: "sample", Timestamp: now, Confidence: 1.0},
			// Intentionally noncanonical 2-part subject — resident-poison
			// fixture with a canonical predicate, so a canonical RemoveTriple
			// can eliminate it.
			{Subject: "bad.subject", Predicate: "test.poison.kind", Object: "resident", Timestamp: now, Confidence: 1.0},
		},
		MessageType: message.Type{Domain: "test", Category: "poison", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	})

	// The exact seam the RMW closures decode through: the stored poisoned
	// bytes are admitted by the trusted decoder and refused by the
	// validating decoder.
	entry, err := c.entityBucket.Get(ctx, id)
	require.NoError(t, err)
	var trusted graph.EntityState
	require.NoError(t, graph.UnmarshalEntityStateTrusted(entry.Value, &trusted),
		"trusted decode must admit resident poison on the owner's own RMW read")
	var validated graph.EntityState
	require.Error(t, graph.UnmarshalEntityState(entry.Value, &validated),
		"validating decoder must keep refusing the same bytes")

	// A canonical removal that eliminates the poisoned triple commits: the
	// read admitted the poison, and the write-gate candidate (poison removed)
	// is canonical.
	require.NoError(t, c.RemoveTriple(ctx, id, "test.poison.kind"),
		"pre-swap the RMW read rejected this call; post-swap it must commit")

	after, err := c.entityBucket.Get(ctx, id)
	require.NoError(t, err)
	var clean graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(after.Value, &clean),
		"stored state must be fully canonical after the poisoned triple is removed")
	for _, triple := range clean.Triples {
		assert.NotEqual(t, "test.poison.kind", triple.Predicate, "poisoned triple must be gone")
	}
}
