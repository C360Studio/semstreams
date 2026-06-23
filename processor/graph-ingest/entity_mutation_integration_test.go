//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Round-trip integration tests for the entity-level mutation handlers
// landed for GH #98 (PR-A). Each handler is exercised through its
// NATS-callback shape (same surface natsclient.SubscribeForRequests
// delivers) to validate the wire envelope, success/error flagging,
// and the create-or-fail / must-exist / idempotent-delete semantics
// that CS API gateways (semconnect) need to map cleanly to HTTP
// status codes.
//
// PR-A semantics validated here:
//   - SubjectEntityCreate                : create-or-fail (409-shape on duplicate)
//   - SubjectEntityCreateWithTriples     : same as above + carries EntityState provenance
//   - SubjectEntityUpdate                : must-exist (404-shape when absent)
//   - SubjectEntityUpdateWithTriples     : must-exist + applies AddTriples/RemoveTriples delta
//   - SubjectEntityDelete                : idempotent (Deleted bool reflects prior presence)
//
// PR-B will close the partial-erasure window in the with_triples
// update path with a single CAS over the entity state; the existing
// tests below pin the SEMANTICS, not the implementation, so PR-B
// should not have to rewrite them.

// testMutationType is the synthetic Type used by these integration tests.
// Kept distinct from any production domain to make filtering / debugging
// easier.
var testMutationType = message.Type{
	Domain:   "test",
	Category: "mutation",
	Version:  "v1",
}

func newMutationTestEntity(id string) *graph.EntityState {
	now := time.Now()
	return &graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "test.kind", Object: "sample", Timestamp: now, Confidence: 1.0},
		},
		MessageType: testMutationType,
		Version:     1,
		UpdatedAt:   now,
	}
}

func TestIntegration_HandleEntityCreate_Success(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	entity := newMutationTestEntity("c360.test.entity.create.success.001")
	req := graph.CreateEntityRequest{Entity: entity, RequestID: "req-create-1"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityCreate(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.CreateEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.Equal(t, "req-create-1", resp.RequestID, "RequestID should round-trip")
	assert.NotZero(t, resp.KVRevision, "revision should be set after a successful write")
	require.NotNil(t, resp.Entity, "Entity should be populated in success response")
	assert.Equal(t, entity.ID, resp.Entity.ID)
}

func TestIntegration_HandleEntityCreate_Conflict(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	entity := newMutationTestEntity("c360.test.entity.create.conflict.001")
	require.NoError(t, c.CreateEntity(ctx, entity), "seed entity")

	req := graph.CreateEntityRequest{Entity: entity, RequestID: "req-create-conflict"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityCreate(ctx, reqBytes)
	// ADR-060: a duplicate create is a typed entity_already_exists reject
	// (invalid class), no longer an in-body Success=false. RequestID is no
	// longer echoed on the failure path (failures carry no body).
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeEntityExists, "already exists")
	assert.True(t, errs.IsInvalid(err), "conflict classifies invalid so semconnect can map to HTTP 409")
}

func TestIntegration_HandleEntityCreateWithTriples_PreservesProvenance(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.create_wt.provenance.001"
	now := time.Now()
	entity := &graph.EntityState{
		ID:          entityID,
		MessageType: testMutationType,
		Version:     7, // provenance: caller asserts this version
		UpdatedAt:   now,
		StorageRef: &message.StorageReference{
			StorageInstance: "test-bucket",
			Key:             "test/key",
			ContentType:     "application/json",
		},
		// req.Triples (below) is the canonical set per the
		// create_with_triples doc-comment. Entity.Triples here is a
		// distractor that should NOT survive — pinning the
		// replacement semantic, not append.
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "should.be.replaced", Object: "distractor", Timestamp: now, Confidence: 1.0},
		},
	}
	triples := []message.Triple{
		{Subject: entityID, Predicate: "sensorml.uid", Object: "urn:test:001", Timestamp: now, Confidence: 1.0},
		{Subject: entityID, Predicate: "sensorml.label", Object: "Test Sensor", Timestamp: now, Confidence: 1.0},
	}
	req := graph.CreateEntityWithTriplesRequest{
		Entity:    entity,
		Triples:   triples,
		RequestID: "req-create-wt-prov",
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityCreateWithTriples(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	// 2 request triples + the ADR-054 entity.indexing.profile stamp applied at
	// the create seam. TriplesAdded reflects the total stored on the entity.
	assert.Equal(t, 3, resp.TriplesAdded)

	// Verify provenance survived to storage — the load-bearing reason
	// for the *_with_triples variant over a plain add_batch upsert.
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &stored))
	assert.True(t, testMutationType.Equal(stored.MessageType), "MessageType provenance preserved")
	assert.NotNil(t, stored.StorageRef, "StorageRef preserved")
	assert.Equal(t, "test-bucket", stored.StorageRef.StorageInstance)
	assert.Equal(t, "test/key", stored.StorageRef.Key)
	assert.Equal(t, 2, nonProfileTripleCount(&stored), "req.Triples should REPLACE Entity.Triples, not append (ADR-054 profile stamp excluded)")
	for _, tr := range stored.Triples {
		assert.NotEqual(t, "should.be.replaced", tr.Predicate,
			"distractor predicate from req.Entity.Triples must not survive when req.Triples is non-empty")
	}
}

func TestIntegration_HandleEntityUpdate_NotFound(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	entity := newMutationTestEntity("c360.test.entity.update.notfound.001")
	req := graph.UpdateEntityRequest{Entity: entity, RequestID: "req-update-404"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityUpdate(ctx, reqBytes)
	// ADR-060: must-exist update on an absent entity is a typed entity_not_found
	// reject (invalid class), so semconnect can map to HTTP 404.
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeEntityNotFound, "not found")
	assert.True(t, errs.IsInvalid(err))
}

func TestIntegration_HandleEntityUpdate_Success(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.update.success.001"
	original := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, original), "seed entity")

	updated := newMutationTestEntity(entityID)
	updated.Triples = append(updated.Triples, message.Triple{
		Subject:    entityID,
		Predicate:  "test.added",
		Object:     "by-update",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	})
	updated.Version = 2

	req := graph.UpdateEntityRequest{Entity: updated, RequestID: "req-update-ok"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityUpdate(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.UpdateEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.Equal(t, int64(2), resp.Version)
}

func TestIntegration_HandleEntityUpdateWithTriples_AddAndRemove(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.update_wt.delta.001"
	now := time.Now()
	seed := &graph.EntityState{
		ID:          entityID,
		MessageType: testMutationType,
		Version:     1,
		UpdatedAt:   now,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "test.keep", Object: "keep-me", Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: "test.drop", Object: "drop-me", Timestamp: now, Confidence: 1.0},
		},
	}
	require.NoError(t, c.CreateEntity(ctx, seed), "seed entity")

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:        seed, // metadata flows through
		AddTriples:    []message.Triple{{Subject: entityID, Predicate: "test.added", Object: "added-me", Timestamp: now, Confidence: 1.0}},
		RemoveTriples: []string{"test.drop"},
		RequestID:     "req-update-wt-delta",
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityUpdateWithTriples(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.Equal(t, 1, resp.TriplesAdded)
	assert.Equal(t, 1, resp.TriplesRemoved)

	// Verify the stored triples reflect the delta.
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &stored))

	predicates := make(map[string]bool, len(stored.Triples))
	for _, tr := range stored.Triples {
		predicates[tr.Predicate] = true
	}
	assert.True(t, predicates["test.keep"], "test.keep should survive")
	assert.False(t, predicates["test.drop"], "test.drop should be removed")
	assert.True(t, predicates["test.added"], "test.added should be appended")
}

// applyUpdateWT runs one update_with_triples against entityID via the
// requested handler path — the default UpdateWithRetry path (useCAS=false,
// ExpectedRevision=0) or the CAS-on-condition path (useCAS=true,
// ExpectedRevision=current rev, the path pkg/lifecycle.Manager uses) —
// and returns the entity's stored triples. Fails the test on any error.
func applyUpdateWT(t *testing.T, ctx context.Context, c *Component, entityID string, add []message.Triple, remove []string, useCAS bool) []message.Triple {
	t.Helper()
	req := graph.UpdateEntityWithTriplesRequest{
		Entity:        &graph.EntityState{ID: entityID, MessageType: testMutationType, Version: 1, UpdatedAt: time.Now()},
		AddTriples:    add,
		RemoveTriples: remove,
	}
	if useCAS {
		_, rev, err := c.fetchEntityState(ctx, entityID)
		require.NoError(t, err, "fetch current revision for CAS path")
		req.ExpectedRevision = rev
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)
	respBytes, err := c.handleEntityUpdateWithTriples(ctx, reqBytes)
	require.NoError(t, err)
	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err, "read back stored entity")
	return stored.Triples
}

// objectsForPredicate returns the Objects of every triple matching predicate.
func objectsForPredicate(triples []message.Triple, predicate string) []any {
	out := []any{}
	for _, tr := range triples {
		if tr.Predicate == predicate {
			out = append(out, tr.Object)
		}
	}
	return out
}

// TestIntegration_HandleEntityUpdateWithTriples_ReplaceByPredicate locks the
// gh#244 fix: the AddTriples leg replaces by (subject,predicate) via
// MergeTriples — matching the DataManager UpdateEntityWithTriples contract —
// rather than appending. Run against BOTH handler paths.
//
// Before the fix, the single-valued sub-case accumulated duplicate predicates
// (status [active, completed]) whenever the caller didn't defensively name
// the predicate in RemoveTriples — the silent re-bloat semspec measured in
// gh#242. update_with_triples is upsert/replace; incremental append is
// triple.add_batch's job.
func TestIntegration_HandleEntityUpdateWithTriples_ReplaceByPredicate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		idtok  string
		useCAS bool
	}{
		{"UpdateWithRetry", "uwr", false},
		{"CAS", "cas", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, c := startBatchTestComponent(t)
			entityID := "c360.test.entity.replacepred." + tc.idtok + ".001"

			now := time.Now()
			seed := &graph.EntityState{
				ID: entityID, MessageType: testMutationType, Version: 1, UpdatedAt: now,
				Triples: []message.Triple{
					{Subject: entityID, Predicate: "test.status", Object: "active", Timestamp: now, Confidence: 1.0},
					{Subject: entityID, Predicate: "test.keep", Object: "keep-me", Timestamp: now, Confidence: 1.0},
					{Subject: entityID, Predicate: "test.member", Object: "a", Timestamp: now, Confidence: 1.0},
					{Subject: entityID, Predicate: "test.member", Object: "b", Timestamp: now, Confidence: 1.0},
				},
			}
			require.NoError(t, c.CreateEntity(ctx, seed), "seed entity")

			// (1) Single-valued replace WITHOUT RemoveTriples: status active→completed.
			//     Pre-fix this appended, leaving BOTH active and completed.
			triples := applyUpdateWT(t, ctx, c, entityID, []message.Triple{
				{Subject: entityID, Predicate: "test.status", Object: "completed", Timestamp: time.Now(), Confidence: 1.0},
			}, nil, tc.useCAS)
			assert.Equal(t, []any{"completed"}, objectsForPredicate(triples, "test.status"),
				"single-valued predicate must be REPLACED (exactly one value), not accumulated")

			// (3) Untouched predicate survives.
			assert.Equal(t, []any{"keep-me"}, objectsForPredicate(triples, "test.keep"),
				"a predicate absent from AddTriples must survive untouched")

			// (2) Multi-valued replace: send the FULL new set {c,d} → replaces {a,b}.
			triples = applyUpdateWT(t, ctx, c, entityID, []message.Triple{
				{Subject: entityID, Predicate: "test.member", Object: "c", Timestamp: time.Now(), Confidence: 1.0},
				{Subject: entityID, Predicate: "test.member", Object: "d", Timestamp: time.Now(), Confidence: 1.0},
			}, nil, tc.useCAS)
			assert.ElementsMatch(t, []any{"c", "d"}, objectsForPredicate(triples, "test.member"),
				"multi-valued predicate replaced by the full new set — prior members dropped, not accumulated")

			// (4) Idempotent re-persist: applying the same value again must not duplicate.
			triples = applyUpdateWT(t, ctx, c, entityID, []message.Triple{
				{Subject: entityID, Predicate: "test.status", Object: "completed", Timestamp: time.Now(), Confidence: 1.0},
			}, nil, tc.useCAS)
			assert.Equal(t, []any{"completed"}, objectsForPredicate(triples, "test.status"),
				"re-persisting the same value must stay idempotent (no duplicate)")
		})
	}
}

// TestIntegration_HandleEntityUpdateWithTriples_RemoveOnlyPureDelete confirms
// RemoveTriples still deletes a predicate's values when AddTriples does not
// re-add it (pure delete — unchanged by the gh#244 replace-by-predicate fix,
// which only governs the add leg).
func TestIntegration_HandleEntityUpdateWithTriples_RemoveOnlyPureDelete(t *testing.T) {
	ctx, c := startBatchTestComponent(t)
	const entityID = "c360.test.entity.puredelete.del.001"
	now := time.Now()
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: entityID, MessageType: testMutationType, Version: 1, UpdatedAt: now,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "test.status", Object: "active", Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: "test.keep", Object: "keep-me", Timestamp: now, Confidence: 1.0},
		},
	}), "seed entity")

	triples := applyUpdateWT(t, ctx, c, entityID, nil, []string{"test.status"}, false)
	assert.Empty(t, objectsForPredicate(triples, "test.status"), "remove-only must delete the predicate's values")
	assert.Equal(t, []any{"keep-me"}, objectsForPredicate(triples, "test.keep"), "unrelated predicate untouched")
}

func TestIntegration_HandleEntityDelete_Existing(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.delete.existing.001"
	require.NoError(t, c.CreateEntity(ctx, newMutationTestEntity(entityID)), "seed entity")

	req := graph.DeleteEntityRequest{EntityID: entityID, RequestID: "req-delete-1"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityDelete(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.DeleteEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.True(t, resp.Deleted, "Deleted should be true when the entity was present")

	exists, err := c.entityExists(ctx, entityID)
	require.NoError(t, err)
	assert.False(t, exists, "entity should be gone after delete")
}

func TestIntegration_HandleEntityDelete_Idempotent(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	req := graph.DeleteEntityRequest{
		EntityID:  "c360.test.entity.delete.absent.001",
		RequestID: "req-delete-idempotent",
	}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityDelete(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.DeleteEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.False(t, resp.Deleted, "Deleted should be false when entity was already absent")
}

// TestIntegration_HandleEntityUpdate_DeleteBeforeUpdate pins the
// must-exist contract on the *handler* level when the entity is
// already gone by the time the handler runs. This exercises the
// fetchEntityState → ErrKVKeyNotFound path (mutations.go:430),
// NOT the CAS branch (which can only be reached if the delete
// races between fetchEntityState and updateEntityAtRevision).
//
// The CAS branch itself is covered by
// TestIntegration_UpdateEntityAtRevision_StaleRevisionFromConcurrentDelete
// below — that test invokes the CAS primitive directly with a
// stale revision, which is the only deterministic way to exercise
// the post-fetch race window without a mock bucket.
func TestIntegration_HandleEntityUpdate_DeleteBeforeUpdate(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.update.delbefore.001"
	original := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, original), "seed entity")
	require.NoError(t, c.DeleteEntity(ctx, entityID), "delete before handler runs")

	updated := newMutationTestEntity(entityID)
	updated.Version = 2
	req := graph.UpdateEntityRequest{Entity: updated, RequestID: "req-delbefore"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityUpdate(ctx, reqBytes)
	// ADR-060: a concurrent delete surfaces as a typed entity_not_found reject
	// (invalid class) rather than silently resurrecting the entity.
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeEntityNotFound, "not found")
	assert.True(t, errs.IsInvalid(err))

	exists, err := c.entityExists(ctx, entityID)
	require.NoError(t, err)
	assert.False(t, exists, "entity must remain absent after the failed update")
}

// TestIntegration_UpdateEntityAtRevision_StaleRevisionFromConcurrentDelete
// pins the CAS primitive's behavior under the actual race the
// must-exist contract protects against: a delete lands between
// the handler's read (which captured revision R1) and the
// handler's write (CAS attempt at R1). The tombstone bumps the
// stream sequence, so CAS at R1 fails with ErrKVRevisionMismatch
// instead of silently resurrecting the entity via Put.
//
// Tests the primitive directly (not through the handler) because
// the handler's fetchEntityState would catch the delete first if
// we deleted before invoking it — only by calling
// updateEntityAtRevision with a captured stale revision can we
// hit the post-fetch, pre-write window deterministically.
func TestIntegration_UpdateEntityAtRevision_StaleRevisionFromConcurrentDelete(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.cas.staledel.001"
	original := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, original), "seed entity")

	// Capture the revision while the entity exists — this models the
	// state the handler holds after fetchEntityState succeeds.
	_, capturedRev, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotZero(t, capturedRev)

	// Out-of-band delete: the tombstone moves the KV sequence past
	// capturedRev, so any subsequent CAS at capturedRev must fail.
	require.NoError(t, c.DeleteEntity(ctx, entityID), "delete between read and write")

	updated := newMutationTestEntity(entityID)
	updated.Version = 2
	err = c.updateEntityAtRevision(ctx, updated, capturedRev)
	require.Error(t, err, "CAS at stale revision after concurrent delete must fail")
	assert.True(t, errors.Is(err, natsclient.ErrKVRevisionMismatch),
		"stale-revision CAS after delete must produce ErrKVRevisionMismatch; got: %v", err)

	exists, err := c.entityExists(ctx, entityID)
	require.NoError(t, err)
	assert.False(t, exists, "failed CAS must not resurrect the deleted entity")
}

// TestIntegration_HandleEntityUpdate_KVRevisionIncrements pins the
// cache-invalidation / monotonic-revision contract downstream readers
// (semconnect ETags, change-detection consumers) depend on.
func TestIntegration_HandleEntityUpdate_KVRevisionIncrements(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.update.revmono.001"
	require.NoError(t, c.CreateEntity(ctx, newMutationTestEntity(entityID)), "seed entity")

	// First update: capture revision.
	first := newMutationTestEntity(entityID)
	first.Version = 2
	r1, err := c.handleEntityUpdate(ctx, mustJSON(t, graph.UpdateEntityRequest{Entity: first}))
	require.NoError(t, err)
	var resp1 graph.UpdateEntityResponse
	require.NoError(t, json.Unmarshal(r1, &resp1))

	// Second update: revision must be strictly greater.
	second := newMutationTestEntity(entityID)
	second.Version = 3
	r2, err := c.handleEntityUpdate(ctx, mustJSON(t, graph.UpdateEntityRequest{Entity: second}))
	require.NoError(t, err)
	var resp2 graph.UpdateEntityResponse
	require.NoError(t, json.Unmarshal(r2, &resp2))

	assert.Greater(t, resp2.KVRevision, resp1.KVRevision,
		"sequential updates must produce strictly increasing KV revisions")
}

// TestIntegration_HandleEntity_NilEntity pins the nil-body guard
// across all four entity-shape handlers — the guards exist in the
// code; this test ensures a future refactor that drops them surfaces
// the regression.
func TestIntegration_HandleEntity_NilEntity(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	cases := []struct {
		name    string
		payload any
		handler func([]byte) ([]byte, error)
	}{
		{"create", graph.CreateEntityRequest{Entity: nil}, func(b []byte) ([]byte, error) { return c.handleEntityCreate(ctx, b) }},
		{"create_with_triples", graph.CreateEntityWithTriplesRequest{Entity: nil}, func(b []byte) ([]byte, error) { return c.handleEntityCreateWithTriples(ctx, b) }},
		{"update", graph.UpdateEntityRequest{Entity: nil}, func(b []byte) ([]byte, error) { return c.handleEntityUpdate(ctx, b) }},
		{"update_with_triples", graph.UpdateEntityWithTriplesRequest{Entity: nil}, func(b []byte) ([]byte, error) { return c.handleEntityUpdateWithTriples(ctx, b) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			respBytes, err := tc.handler(mustJSON(t, tc.payload))
			// ADR-060: the nil-body guard rejects with a typed invalid_request error.
			requireClassifiedReject(t, respBytes, err, graph.ErrorCodeInvalidRequest, "entity cannot be nil")
		})
	}
}

// TestIntegration_HandleEntityDelete_EmptyID pins the empty-EntityID
// guard. Without it, the handler would forward an empty key to
// entityBucket.Delete and the failure mode would depend on the
// underlying KV (likely a different error class than "invalid
// request"). Pin the classification at the handler boundary.
func TestIntegration_HandleEntityDelete_EmptyID(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	req := graph.DeleteEntityRequest{EntityID: "", RequestID: "req-delete-empty"}
	respBytes, err := c.handleEntityDelete(ctx, mustJSON(t, req))
	// ADR-060: the empty-EntityID guard rejects with a typed invalid_request error.
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeInvalidRequest, "entity_id cannot be empty")
}

// TestIntegration_HandleEntityUpdateWithTriples_UnknownRemovePredicate
// pins the "remove if present" semantic: naming a predicate in
// RemoveTriples that doesn't exist on the entity is NOT an error,
// it's a no-op. Downstream gateways (semconnect cs-api PATCH
// /systems/{id}) rely on this — they emit RemoveTriples blindly
// when they don't know which predicates were previously written.
func TestIntegration_HandleEntityUpdateWithTriples_UnknownRemovePredicate(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.update_wt.unknown_remove.001"
	require.NoError(t, c.CreateEntity(ctx, newMutationTestEntity(entityID)), "seed entity")

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:        newMutationTestEntity(entityID),
		RemoveTriples: []string{"never.set.predicate"},
		RequestID:     "req-update-wt-unknown",
	}
	respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
	require.NoError(t, err)

	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.Equal(t, 0, resp.TriplesRemoved, "no triples should be removed when the named predicate isn't present")
}

// mustJSON is a test convenience: marshals or fails the test.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// requireClassifiedReject asserts the ADR-060 typed-error reject contract: a
// hard mutation failure returns (nil body, *errs.ClassifiedError) carrying the
// expected stable Code, with the handler's message preserved in err.Error().
// Replaces the pre-ADR-060 "no Go error, Success=false + ErrorCode in the body"
// assertions across the graph-ingest mutation integration tests.
func requireClassifiedReject(t *testing.T, respBytes []byte, err error, wantCode, wantMsgSubstr string) {
	t.Helper()
	require.Error(t, err)
	require.Nil(t, respBytes, "ADR-060: a hard failure returns no body")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, wantCode, ce.Code, "classified error Code")
	if wantMsgSubstr != "" {
		assert.Contains(t, err.Error(), wantMsgSubstr)
	}
}

func TestIntegration_HandleEntity_InvalidJSON(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	cases := []struct {
		name    string
		handler func([]byte) ([]byte, error)
	}{
		{"create", func(b []byte) ([]byte, error) { return c.handleEntityCreate(ctx, b) }},
		{"create_with_triples", func(b []byte) ([]byte, error) { return c.handleEntityCreateWithTriples(ctx, b) }},
		{"update", func(b []byte) ([]byte, error) { return c.handleEntityUpdate(ctx, b) }},
		{"update_with_triples", func(b []byte) ([]byte, error) { return c.handleEntityUpdateWithTriples(ctx, b) }},
		{"delete", func(b []byte) ([]byte, error) { return c.handleEntityDelete(ctx, b) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			respBytes, err := tc.handler([]byte("not json"))
			// ADR-060: malformed JSON rejects with a typed invalid_request error
			// (handler returns (nil, *errs.ClassifiedError), not an in-body flag).
			requireClassifiedReject(t, respBytes, err, graph.ErrorCodeInvalidRequest, "invalid request")
		})
	}
}

// --- PR-B tests: atomic create + UpdateWithRetry for update_with_triples ---

// TestIntegration_CreateEntityStrict_AtomicConflict pins the
// fundamental contract of the new atomic-create primitive: a second
// create for the same ID surfaces natsclient.ErrKVKeyExists, and
// the original entity content is untouched (no upsert).
//
// Tests the primitive directly so the contract is independent of
// handler wiring; the handler-level mapping to "entity already
// exists" + HTTP 409 is verified by
// TestIntegration_HandleEntityCreate_Conflict (PR-A test, still
// green because the semantic survives the refactor).
func TestIntegration_CreateEntityStrict_AtomicConflict(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.atomic.create.001"
	first := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntityStrict(ctx, first), "first create on fresh ID should succeed")

	second := newMutationTestEntity(entityID)
	second.Triples = []message.Triple{
		{Subject: entityID, Predicate: "test.marker", Object: "second", Timestamp: time.Now(), Confidence: 1.0},
	}
	err := c.CreateEntityStrict(ctx, second)
	require.Error(t, err, "second create on the same ID must fail")
	assert.True(t, errors.Is(err, natsclient.ErrKVKeyExists),
		"second create must surface ErrKVKeyExists sentinel so handlers can branch on it; got: %v", err)

	// First entity's content survives — no upsert.
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &stored))
	for _, tr := range stored.Triples {
		assert.NotEqual(t, "test.marker", tr.Predicate,
			"second create's distinct triple must not be present — atomic create must not overwrite")
	}
}

// TestIntegration_CreateEntityStrict_ConcurrentRaceOneWinner pins the
// concurrency guarantee CreateEntityStrict provides over the prior
// exists-check + Put pattern: with N goroutines all trying to create
// the same ID, exactly one wins and N-1 see ErrKVKeyExists. The
// PR-A pattern would allow last-writer-wins with all N reporting
// success — a 409-Conflict contract violation.
func TestIntegration_CreateEntityStrict_ConcurrentRaceOneWinner(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.atomic.race.001"
	const N = 5

	var wg sync.WaitGroup
	results := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entity := newMutationTestEntity(entityID)
			entity.Triples = append(entity.Triples, message.Triple{
				Subject:    entityID,
				Predicate:  "test.creator",
				Object:     idx,
				Timestamp:  time.Now(),
				Confidence: 1.0,
			})
			results[idx] = c.CreateEntityStrict(ctx, entity)
		}(i)
	}
	wg.Wait()

	winners := 0
	conflicts := 0
	for i, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, natsclient.ErrKVKeyExists):
			conflicts++
		default:
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	assert.Equal(t, 1, winners, "exactly one concurrent create must win")
	assert.Equal(t, N-1, conflicts, "all other concurrent creates must surface ErrKVKeyExists")
	assert.Equal(t, N, winners+conflicts,
		"every outcome must be either winner or ErrKVKeyExists — no unaccounted-for failures")
}

// TestIntegration_HandleEntityUpdateWithTriples_ConcurrentUpdatesSurvive
// pins the closing of the partial-erasure window semconnect Stage 27
// flagged. N goroutines each call handleEntityUpdateWithTriples to
// add a distinct triple; with PR-B's UpdateWithRetry path, every
// goroutine eventually succeeds (CAS-conflicts retry against the
// fresh state) and all N triples land on the entity. The PR-A path
// (single CAS, fail-on-conflict) would have surfaced revision
// mismatches and the loser goroutines' triples would be missing
// from the final state.
//
// This is the load-bearing test for PR-B's semantic guarantee:
// concurrent updates compose, they don't silently erase each other.
//
// Flake risk: if UpdateWithRetry's MaxAttempts is exhausted under
// extreme load (CI + race detector + N goroutines all retrying
// against each other), one goroutine could surface
// ErrKVMaxRetriesExceeded. N=5 is well within the per-bucket retry
// budget under normal conditions. If this test flakes, the
// remediation is t.Skip + an issue tracking why the retry budget
// is insufficient, NOT bumping MaxAttempts to hide the symptom.
func TestIntegration_HandleEntityUpdateWithTriples_ConcurrentUpdatesSurvive(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.entity.concurrent.upd.001"
	require.NoError(t, c.CreateEntity(ctx, newMutationTestEntity(entityID)), "seed entity")

	const N = 5
	var wg sync.WaitGroup
	results := make([]string, N) // empty on success, error msg on failure

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := graph.UpdateEntityWithTriplesRequest{
				Entity: newMutationTestEntity(entityID),
				AddTriples: []message.Triple{
					{
						Subject:    entityID,
						Predicate:  fmt.Sprintf("test.concurrent.%d", idx),
						Object:     idx,
						Timestamp:  time.Now(),
						Confidence: 1.0,
					},
				},
				RequestID: fmt.Sprintf("req-conc-%d", idx),
			}
			respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
			if err != nil {
				results[idx] = fmt.Sprintf("handler returned err: %v", err)
				return
			}
			var resp graph.UpdateEntityWithTriplesResponse
			if err := json.Unmarshal(respBytes, &resp); err != nil {
				results[idx] = fmt.Sprintf("unmarshal: %v", err)
				return
			}
			_ = resp
		}(i)
	}
	wg.Wait()

	for i, msg := range results {
		assert.Empty(t, msg,
			"concurrent update %d must succeed via UpdateWithRetry; PR-A semantics would have surfaced a revision mismatch here", i)
	}

	// All N concurrent triples must be present on the final entity —
	// none were silently erased by another goroutine's race.
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &stored))

	predicates := make(map[string]bool, len(stored.Triples))
	for _, tr := range stored.Triples {
		predicates[tr.Predicate] = true
	}
	for i := 0; i < N; i++ {
		p := fmt.Sprintf("test.concurrent.%d", i)
		assert.True(t, predicates[p],
			"concurrent triple %q must survive on the final entity (PR-B retry semantics)", p)
	}

	// Metadata fields from each caller's req.Entity must also flow through
	// the merge loop — testMutationType is what every caller sent, so the
	// stored entity must carry it. If a retry path silently dropped
	// metadata (e.g., wrote only the merged triples without the carrier's
	// metadata), this assertion fails. Pins the contract claimed in
	// handleEntityUpdateWithTriples's doc-comment.
	assert.True(t, testMutationType.Equal(stored.MessageType),
		"req.Entity.MessageType must survive across UpdateWithRetry attempts")
}

// --- ADR-049 PR 1: UpdateEntityWithTriplesRequest.ExpectedRevision ---
//
// The CAS-on-condition path: when ExpectedRevision is non-zero, the
// handler reads the entity's current revision, rejects on mismatch
// (single-pass — no internal retry), applies the delta, and writes
// at the expected rev. A concurrent writer between read and write
// trips the CAS at write time. This is the primitive
// pkg/lifecycle.Manager.Transition uses to preserve state-machine
// correctness under concurrent writes.

func TestIntegration_UpdateEntityWithTriples_ExpectedRevisionSuccess(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.casupd.success.entity.001"
	seed := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, seed), "seed entity")

	// Capture the seed's revision so we can pass it as ExpectedRevision.
	_, seedRev, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotZero(t, seedRev)

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:           seed,
		AddTriples:       []message.Triple{{Subject: entityID, Predicate: "test.cas.added", Object: "v1", Timestamp: time.Now(), Confidence: 1.0}},
		ExpectedRevision: seedRev,
		RequestID:        "req-update-wt-cas-success",
	}
	respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
	require.NoError(t, err)

	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.Equal(t, 1, resp.TriplesAdded)
	assert.Greater(t, resp.KVRevision, seedRev, "revision should increment after write")
}

func TestIntegration_UpdateEntityWithTriples_ExpectedRevisionMismatchAtRead(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.casupd.mismatch.entity.001"
	seed := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, seed), "seed entity")

	_, seedRev, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)

	// Advance the revision via an unrelated write so the caller's
	// (stale) ExpectedRevision no longer matches.
	intermediate := graph.UpdateEntityWithTriplesRequest{
		Entity:     seed,
		AddTriples: []message.Triple{{Subject: entityID, Predicate: "test.intermediate", Object: "advance", Timestamp: time.Now(), Confidence: 1.0}},
		RequestID:  "req-intermediate-advance",
	}
	intermediateResp, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, intermediate))
	require.NoError(t, err)
	var iResp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(intermediateResp, &iResp))

	// Now caller submits with the STALE rev. CAS should reject at the
	// read-time check.
	req := graph.UpdateEntityWithTriplesRequest{
		Entity:           seed,
		AddTriples:       []message.Triple{{Subject: entityID, Predicate: "test.cas.late", Object: "should-not-land", Timestamp: time.Now(), Confidence: 1.0}},
		ExpectedRevision: seedRev, // stale!
		RequestID:        "req-update-wt-cas-stale",
	}
	respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
	// ADR-060: a stale CAS is the revision_mismatch control-flow sentinel, so a
	// caller can errors.Is(err, errs.ErrRevisionMismatch) and re-read + re-validate.
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeRevisionMismatch, "revision mismatch")
	assert.True(t, errors.Is(err, errs.ErrRevisionMismatch), "stale CAS surfaces the revision-mismatch sentinel")

	// Verify the stale delta did NOT land.
	_, currentRev, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	assert.Equal(t, iResp.KVRevision, currentRev,
		"revision must not have advanced after a CAS-rejected request")
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &stored))
	for _, tr := range stored.Triples {
		assert.NotEqual(t, "test.cas.late", tr.Predicate,
			"the late triple from the stale request must not have landed")
	}
}

func TestIntegration_UpdateEntityWithTriples_ExpectedRevisionZeroKeepsExistingBehavior(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.casupd.zerorev.entity.001"
	seed := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, seed), "seed entity")

	// ExpectedRevision == 0 means "use the existing UpdateWithRetry
	// path." Every existing caller passes zero; the field is opt-in.
	req := graph.UpdateEntityWithTriplesRequest{
		Entity:     seed,
		AddTriples: []message.Triple{{Subject: entityID, Predicate: "test.zero_rev.added", Object: "via-retry-path", Timestamp: time.Now(), Confidence: 1.0}},
		// ExpectedRevision deliberately omitted (zero value)
		RequestID: "req-update-wt-zero-rev",
	}
	respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
	require.NoError(t, err)

	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.Equal(t, 1, resp.TriplesAdded)
}

func TestIntegration_UpdateEntityWithTriples_ExpectedRevisionAbsentEntity(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.casupd.absent.entity.001"
	// Note: no Create — entity does not exist.

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:           &graph.EntityState{ID: entityID, MessageType: testMutationType},
		AddTriples:       []message.Triple{{Subject: entityID, Predicate: "test.absent.added", Object: "should-fail", Timestamp: time.Now(), Confidence: 1.0}},
		ExpectedRevision: 42, // arbitrary non-zero
		RequestID:        "req-update-wt-cas-absent",
	}
	respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
	// ADR-060: CAS against an absent entity is entity_not_found (not
	// revision_mismatch), so callers can disambiguate from a CAS conflict.
	requireClassifiedReject(t, respBytes, err, graph.ErrorCodeEntityNotFound, "entity not found")
	assert.False(t, errors.Is(err, errs.ErrRevisionMismatch), "absent entity is not the revision-mismatch sentinel")
}

func TestIntegration_UpdateEntityWithTriples_ExpectedRevisionRaceAtWrite(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.casupd.race.entity.001"
	seed := newMutationTestEntity(entityID)
	require.NoError(t, c.CreateEntity(ctx, seed), "seed entity")

	// Two callers concurrently read at the SAME revision then both
	// try to write with ExpectedRevision=thatRev. Exactly one wins;
	// the other gets a revision-mismatch at write-time CAS even
	// though the read-time check passed (because the winner moved
	// the revision in between).
	_, sharedRev, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotZero(t, sharedRev)

	const N = 8
	var (
		successCount int
		mismatchErrs int
		mu           sync.Mutex
		wg           sync.WaitGroup
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := graph.UpdateEntityWithTriplesRequest{
				Entity:           seed,
				AddTriples:       []message.Triple{{Subject: entityID, Predicate: fmt.Sprintf("test.race.%d", idx), Object: "via-cas", Timestamp: time.Now(), Confidence: 1.0}},
				ExpectedRevision: sharedRev,
				RequestID:        fmt.Sprintf("req-race-%d", idx),
			}
			respBytes, err := c.handleEntityUpdateWithTriples(ctx, mustJSON(t, req))
			if err != nil {
				// ADR-060: a CAS loser now surfaces as the revision_mismatch
				// sentinel error (no in-body Success=false).
				if errors.Is(err, errs.ErrRevisionMismatch) {
					mu.Lock()
					mismatchErrs++
					mu.Unlock()
				}
				return
			}
			var winner graph.UpdateEntityWithTriplesResponse
			if err := json.Unmarshal(respBytes, &winner); err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			successCount++
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 1, successCount, "exactly one CAS race winner expected from %d racers", N)
	assert.Equal(t, N-1, mismatchErrs, "all other racers must observe revision-mismatch errors")
}
