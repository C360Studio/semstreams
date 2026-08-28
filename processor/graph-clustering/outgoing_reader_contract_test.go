package graphclustering

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKVProviderOutgoingReaderRejectsRowsWithoutCanonicalTarget(t *testing.T) {
	entityID := "acme.ops.robotics.gcs.drone.001"
	canonicalSource := "acme.ops.robotics.gcs.sensor.001"
	tests := []struct {
		name string
		row  relationshipEntry
	}{
		{
			name: "incoming-shaped from-only row",
			row: relationshipEntry{
				Predicate:    "robotics.assigned.mission",
				FromEntityID: canonicalSource,
			},
		},
		{
			name: "malformed outgoing target",
			row: relationshipEntry{
				Predicate:  "robotics.assigned.mission",
				ToEntityID: "malformed-target", // entity-id-audit:classify intentional-malformed "malformed-target" line=31 column=17 surface=go-field:relationshipEntry.ToEntityID entity_id_invalid:arity malformed relationship target rejection fixture
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := newMockKVBucket()
			encoded, err := json.Marshal([]relationshipEntry{tt.row})
			require.NoError(t, err)
			bucket.data[entityID] = encoded
			provider := newKVProvider(newMockKVBucket(), bucket, newMockKVBucket(), slog.Default())

			neighbors, err := provider.GetNeighbors(context.Background(), entityID, "outgoing")
			require.Error(t, err)
			assert.Nil(t, neighbors, "poisoned OUTGOING_INDEX rows must not become LPA neighbors")
		})
	}
}

func TestKVProviderOutgoingReaderUsesOnlyCanonicalTarget(t *testing.T) {
	entityID := "acme.ops.robotics.gcs.drone.001"
	targetID := "acme.ops.robotics.gcs.mission.001"
	bucket := newMockKVBucket()
	encoded, err := json.Marshal([]relationshipEntry{{
		Predicate:    "robotics.assigned.mission",
		ToEntityID:   targetID,
		FromEntityID: "acme.ops.robotics.gcs.sensor.001",
	}})
	require.NoError(t, err)
	bucket.data[entityID] = encoded
	provider := newKVProvider(newMockKVBucket(), bucket, newMockKVBucket(), slog.Default())

	neighbors, err := provider.GetNeighbors(context.Background(), entityID, "outgoing")
	require.NoError(t, err)
	assert.Equal(t, []string{targetID}, neighbors)
}
