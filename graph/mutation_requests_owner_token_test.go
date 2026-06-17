// Package graph — JSON round-trip tests for the ADR-056 PR-1 OwnerToken wire
// field added to CreateEntityWithTriplesRequest and UpdateEntityWithTriplesRequest.
//
// Tests verify:
//   - OwnerToken round-trips through JSON marshal→unmarshal.
//   - Empty OwnerToken is OMITTED from the marshaled JSON (omitempty).
//   - No other fields are disturbed by the addition of OwnerToken.
package graph

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
)

// TestCreateEntityWithTriplesRequest_OwnerToken_RoundTrip proves the wire field
// survives a marshal→unmarshal cycle and that omitempty suppresses it when empty.
func TestCreateEntityWithTriplesRequest_OwnerToken_RoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		req := CreateEntityWithTriplesRequest{
			Entity:     &EntityState{ID: "acme.ops.robotics.gcs.drone.001"},
			Triples:    []message.Triple{{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "status.phase", Object: "ready"}},
			OwnerToken: "mission-planner#deadbeef01234567",
			TraceID:    "trace-1",
		}
		data, err := json.Marshal(req)
		require.NoError(t, err, "marshal must succeed")

		var got CreateEntityWithTriplesRequest
		require.NoError(t, json.Unmarshal(data, &got), "unmarshal must succeed")

		assert.Equal(t, req.OwnerToken, got.OwnerToken, "OwnerToken must round-trip through JSON")
		assert.Equal(t, req.Entity.ID, got.Entity.ID, "Entity.ID must be preserved")
		assert.Equal(t, req.TraceID, got.TraceID, "TraceID must be preserved")
	})

	t.Run("empty_omitted", func(t *testing.T) {
		t.Parallel()
		req := CreateEntityWithTriplesRequest{
			Entity:     &EntityState{ID: "acme.ops.robotics.gcs.drone.001"},
			OwnerToken: "", // empty → must not appear in JSON (omitempty)
		}
		data, err := json.Marshal(req)
		require.NoError(t, err)

		// The JSON must not contain the "owner_token" key when empty.
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &raw))
		_, present := raw["owner_token"]
		assert.False(t, present, "empty OwnerToken must be omitted from JSON (omitempty)")
	})
}

// TestUpdateEntityWithTriplesRequest_OwnerToken_RoundTrip proves the wire field
// on the update request type survives a marshal→unmarshal cycle and omits when
// empty.
func TestUpdateEntityWithTriplesRequest_OwnerToken_RoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		req := UpdateEntityWithTriplesRequest{
			Entity:           &EntityState{ID: "acme.ops.robotics.gcs.drone.001"},
			AddTriples:       []message.Triple{{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "status.phase", Object: "running"}},
			RemoveTriples:    []string{"status.old"},
			ExpectedRevision: 42,
			OwnerToken:       "rule-pack.my-pack#cafebabe12345678",
			TraceID:          "trace-2",
			RequestID:        "req-2",
		}
		data, err := json.Marshal(req)
		require.NoError(t, err, "marshal must succeed")

		var got UpdateEntityWithTriplesRequest
		require.NoError(t, json.Unmarshal(data, &got), "unmarshal must succeed")

		assert.Equal(t, req.OwnerToken, got.OwnerToken, "OwnerToken must round-trip through JSON")
		assert.Equal(t, req.Entity.ID, got.Entity.ID, "Entity.ID must be preserved")
		assert.Equal(t, req.ExpectedRevision, got.ExpectedRevision, "ExpectedRevision must be preserved")
		assert.Equal(t, req.RemoveTriples, got.RemoveTriples, "RemoveTriples must be preserved")
		assert.Equal(t, req.TraceID, got.TraceID, "TraceID must be preserved")
		assert.Equal(t, req.RequestID, got.RequestID, "RequestID must be preserved")
	})

	t.Run("empty_omitted", func(t *testing.T) {
		t.Parallel()
		req := UpdateEntityWithTriplesRequest{
			Entity:     &EntityState{ID: "acme.ops.robotics.gcs.drone.001"},
			OwnerToken: "", // empty → must not appear in JSON (omitempty)
		}
		data, err := json.Marshal(req)
		require.NoError(t, err)

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &raw))
		_, present := raw["owner_token"]
		assert.False(t, present, "empty OwnerToken must be omitted from JSON (omitempty)")
	})

	t.Run("token_format", func(t *testing.T) {
		t.Parallel()
		// Verify the canonical "<owner>#<incarnation>" token shape survives round-trip.
		token := "rule-pack.sensor-pack#a1b2c3d4e5f60718"
		req := UpdateEntityWithTriplesRequest{
			Entity:     &EntityState{ID: "acme.ops.robotics.gcs.drone.001"},
			OwnerToken: token,
		}
		data, err := json.Marshal(req)
		require.NoError(t, err)
		var got UpdateEntityWithTriplesRequest
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, token, got.OwnerToken, "token format <owner>#<incarnation> must survive round-trip unmodified")
	})
}
