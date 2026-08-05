package graphingest

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reconcile is deliberately predicate-scoped. It changes graph facts without
// accepting a replacement entity envelope, so message identity and offloaded
// content metadata cannot be clobbered by a mutation caller.
func TestCanonicalReconcilePreservesEntityEnvelope(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	const id = "c360.platform.lifecycle.sys.mission.001"
	messageType := message.Type{Domain: "lifecycle", Category: "harness", Version: "v1"}
	storageRef := &message.StorageReference{
		StorageInstance: "objectstore",
		Key:             "blobs/1",
		ContentType:     "application/json",
	}

	createData, err := json.Marshal(graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID:          id,
			MessageType: messageType,
			Version:     7,
			StorageRef:  storageRef,
		},
		Triples: []message.Triple{{
			Subject: id, Predicate: "lifecycle.state.phase", Object: "active", Confidence: 1,
		}},
	})
	require.NoError(t, err)
	createBody, err := comp.handleCanonicalCreate(t.Context(), createData)
	require.NoError(t, err)
	var created graph.CreateEntityResponse
	require.NoError(t, json.Unmarshal(createBody, &created))
	require.Equal(t, graph.MutationApplied, created.Outcome)

	reconcileData, err := json.Marshal(graph.ReconcilePredicatesRequest{
		EntityID:         id,
		ExpectedRevision: created.KVRevision,
		Predicates:       []string{"lifecycle.state.phase"},
		Desired: []message.Triple{{
			Subject: id, Predicate: "lifecycle.state.phase", Object: "done", Confidence: 1,
		}},
	})
	require.NoError(t, err)
	reconcileBody, err := comp.handleCanonicalReconcile(t.Context(), reconcileData)
	require.NoError(t, err)
	var reconciled graph.ReconcilePredicatesResponse
	require.NoError(t, json.Unmarshal(reconcileBody, &reconciled))
	require.Equal(t, graph.MutationApplied, reconciled.Outcome)

	stored := storedEntity(t, comp, id)
	assert.Equal(t, messageType, stored.MessageType)
	require.NotNil(t, stored.StorageRef)
	assert.Equal(t, storageRef.Key, stored.StorageRef.Key)
	assert.EqualValues(t, 8, stored.Version, "reconcile advances the entity version from its stored value")
	phase, ok := stored.GetPropertyValue("lifecycle.state.phase")
	require.True(t, ok)
	assert.Equal(t, "done", phase)
}
