package lifecycle_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// TestHarnessEntity_RoundTrip: the harness carrier is a verbatim Graphable —
// its triples survive the production decoder unchanged.
func TestHarnessEntity_RoundTrip(t *testing.T) {
	const id = "c360.platform1.gcs.lifecycle.mission.001"
	at := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	original := &lifecycle.HarnessEntity{
		ID: id,
		Facts: []message.Triple{
			{Subject: id, Predicate: "mission.lifecycle.phase", Object: "planning", Source: "lifecycle-harness", Timestamp: at, Confidence: 1},
			{Subject: id, Predicate: "mission.identity.owner-org-id", Object: "acme", Source: "lifecycle-harness", Timestamp: at, Confidence: 0.5},
		},
	}
	require.NoError(t, original.Validate())
	assert.Equal(t, "lifecycle.harness.v1", original.Schema().Key())
	assert.Equal(t, lifecycle.HarnessMessageType(), original.Schema())

	base := message.NewBaseMessage(original.Schema(), original, "test")
	data, err := json.Marshal(base)
	require.NoError(t, err)
	decoded, err := message.NewDecoder(payloadregistry.NewWithSubset(t, lifecycle.RegisterPayloads)).Decode(data)
	require.NoError(t, err)

	got, ok := decoded.Payload().(*lifecycle.HarnessEntity)
	require.Truef(t, ok, "decoded payload must be *lifecycle.HarnessEntity, got %T", decoded.Payload())
	assert.Equal(t, id, got.EntityID())
	assert.Equal(t, original.Triples(), got.Triples(), "verbatim triples survive decode")

	t.Run("an empty id fails validation", func(t *testing.T) {
		require.Error(t, (&lifecycle.HarnessEntity{}).Validate())
	})
}
