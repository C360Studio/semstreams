//go:build integration

package graphingest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
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
	require.True(t, resp.Success, "create on fresh ID should succeed; err=%q", resp.Error)
	assert.Empty(t, resp.Error)
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
	require.NoError(t, err)

	var resp graph.CreateEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.False(t, resp.Success, "duplicate create should not succeed")
	assert.Contains(t, resp.Error, "already exists",
		"error body should signal conflict so semconnect can map to HTTP 409")
	assert.Equal(t, "req-create-conflict", resp.RequestID)
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
	require.True(t, resp.Success, "create_with_triples on fresh ID should succeed; err=%q", resp.Error)
	assert.Equal(t, 2, resp.TriplesAdded)

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
	assert.Len(t, stored.Triples, 2)
}

func TestIntegration_HandleEntityUpdate_NotFound(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	entity := newMutationTestEntity("c360.test.entity.update.notfound.001")
	req := graph.UpdateEntityRequest{Entity: entity, RequestID: "req-update-404"}
	reqBytes, err := json.Marshal(req)
	require.NoError(t, err)

	respBytes, err := c.handleEntityUpdate(ctx, reqBytes)
	require.NoError(t, err)

	var resp graph.UpdateEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	assert.False(t, resp.Success, "update on absent entity should not succeed")
	assert.Contains(t, resp.Error, "not found",
		"error body should signal absence so semconnect can map to HTTP 404")
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
	require.True(t, resp.Success, "update on existing entity should succeed; err=%q", resp.Error)
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
	require.True(t, resp.Success, "update_with_triples should succeed; err=%q", resp.Error)
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
	require.True(t, resp.Success, "delete of existing entity should succeed; err=%q", resp.Error)
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
	assert.True(t, resp.Success, "delete of absent entity should succeed (idempotent)")
	assert.False(t, resp.Deleted, "Deleted should be false when entity was already absent")
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
			require.NoError(t, err, "handler should never return a non-nil err — error lives in the response body per feedback_natsclient_error_payload_convention")

			// Each shape has a MutationResponse embedded; we can unmarshal
			// to the base type just to read the Success/Error flags.
			var base struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(respBytes, &base))
			assert.False(t, base.Success)
			assert.Contains(t, base.Error, "invalid request")
		})
	}
}
