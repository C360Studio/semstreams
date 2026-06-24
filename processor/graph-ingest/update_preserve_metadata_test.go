package graphingest

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update_with_triples writes req.Entity wholesale; a caller sending a bare
// EntityState{ID} (just a triple delta) must NOT clobber the stored envelope.
// These tests lock preserveStoredEntityMetadata: MessageType (ADR-055 provenance
// + ADR-054 registry key), Version (optimistic-concurrency), and StorageRef
// (offloaded content) survive a metadata-less update, while an explicitly-set
// field on the request still wins (the pkg/lifecycle.Manager contract).

func bornEntity(t *testing.T, comp *Component, id string) (message.Type, *message.StorageReference, uint64) {
	t.Helper()
	mt := message.Type{Domain: "lifecycle", Category: "harness", Version: "v1"}
	sref := &message.StorageReference{StorageInstance: "objectstore", Key: "blobs/1", ContentType: "application/json"}
	createReq := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          id,
			MessageType: mt,
			Version:     1,
			StorageRef:  sref,
		},
		Triples: []message.Triple{{Subject: id, Predicate: "harness.phase", Object: "active", Confidence: 1.0}},
	}
	data, err := json.Marshal(createReq)
	require.NoError(t, err)
	respBytes, err := comp.handleEntityCreateWithTriples(t.Context(), data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
	return mt, sref, resp.KVRevision
}

func applyUpdate(t *testing.T, comp *Component, req graph.UpdateEntityWithTriplesRequest) {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	respBytes, err := comp.handleEntityUpdateWithTriples(t.Context(), data)
	require.NoError(t, err)
	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))
}

func TestUpdateWithTriples_BareEntityPreservesEnvelope_NonCAS(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const id = "c360.platform.lifecycle.sys.mission.001"

	mt, sref, _ := bornEntity(t, comp, id)

	// Bare update (no MessageType/Version/StorageRef), ExpectedRevision=0 → the
	// UpdateWithRetry merge path.
	applyUpdate(t, comp, graph.UpdateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: id},
		AddTriples: []message.Triple{{Subject: id, Predicate: "harness.phase", Object: "done", Confidence: 1.0}},
	})

	es := storedEntity(t, comp, id)
	assert.Equal(t, mt, es.MessageType, "MessageType envelope must survive a bare-entity update")
	require.NotNil(t, es.StorageRef, "StorageRef must survive a bare-entity update")
	assert.Equal(t, sref.Key, es.StorageRef.Key)
	assert.EqualValues(t, 1, es.Version, "Version must not reset to 0 on a bare-entity update")
	// The triple delta still applied (replace-by-predicate).
	phase, _ := es.GetPropertyValue("harness.phase")
	assert.Equal(t, "done", phase)
}

func TestUpdateWithTriples_BareEntityPreservesEnvelope_CAS(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const id = "c360.platform.lifecycle.sys.mission.002"

	mt, sref, rev := bornEntity(t, comp, id)

	// Bare update on the CAS-on-condition path (ExpectedRevision > 0).
	applyUpdate(t, comp, graph.UpdateEntityWithTriplesRequest{
		Entity:           &graph.EntityState{ID: id},
		AddTriples:       []message.Triple{{Subject: id, Predicate: "harness.phase", Object: "done", Confidence: 1.0}},
		ExpectedRevision: rev,
	})

	es := storedEntity(t, comp, id)
	assert.Equal(t, mt, es.MessageType, "CAS path must also preserve the MessageType envelope")
	require.NotNil(t, es.StorageRef)
	assert.Equal(t, sref.Key, es.StorageRef.Key)
	assert.EqualValues(t, 1, es.Version)
}

func TestUpdateWithTriples_ExplicitMetadataStillWins(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const id = "c360.platform.lifecycle.sys.mission.003"

	bornEntity(t, comp, id)

	// A caller that DOES set MessageType + Version (the pkg/lifecycle.Manager
	// shape) must have its explicit values written, not the stored ones —
	// otherwise the preserve logic would block legitimate envelope transitions.
	newType := message.Type{Domain: "lifecycle", Category: "harness", Version: "v2"}
	applyUpdate(t, comp, graph.UpdateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: id, MessageType: newType, Version: 7},
		AddTriples: []message.Triple{{Subject: id, Predicate: "harness.phase", Object: "done", Confidence: 1.0}},
	})

	es := storedEntity(t, comp, id)
	assert.Equal(t, newType, es.MessageType, "an explicitly-set MessageType must win over the stored one")
	assert.EqualValues(t, 7, es.Version, "an explicitly-set Version must win")
}
