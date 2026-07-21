//go:build integration

package graphingest

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_BatchQuery_ReportsMissingIDs closes the gh#597 drop path over the
// production handler: a requested ID whose KV read comes back not-found used to vanish
// from the response, leaving the caller a shorter list with no correspondence to what
// it asked for. Nothing downstream could tell "this entity does not exist" from "this
// read did not reach it", and the symptom surfaced several hops away as a missing
// search result.
//
// The response now names every unhydrated ID with a reason, and — this is the part
// worth pinning — the entity list is UNCHANGED for a fully-hydrated batch, so a
// consumer that only reads `entities` is not broken by the addition.
func TestIntegration_BatchQuery_ReportsMissingIDs(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	present := "c360.test.batchmissing.system.drone.001"
	alsoPresent := "c360.test.batchmissing.system.drone.002"
	absent := "c360.test.batchmissing.system.drone.404"
	alsoAbsent := "c360.test.batchmissing.system.drone.405"

	for _, id := range []string{present, alsoPresent} {
		require.NoError(t, c.CreateEntityStrict(ctx, &graph.EntityState{
			ID: id,
			Triples: []message.Triple{{
				Subject: id, Predicate: "core.identity.type", Object: "drone", Confidence: 1.0,
			}},
		}))
	}

	query := func(t *testing.T, ids []string) graph.EntityBatchResponse {
		t.Helper()
		body, err := json.Marshal(map[string]any{"ids": ids})
		require.NoError(t, err)
		raw, err := c.handleQueryBatchNATS(ctx, body)
		require.NoError(t, err, "a not-found ID must not fail the call — partiality is not the bug")
		var resp graph.EntityBatchResponse
		require.NoError(t, json.Unmarshal(raw, &resp))
		return resp
	}

	t.Run("missing IDs are named with a reason", func(t *testing.T) {
		resp := query(t, []string{present, absent, alsoPresent, alsoAbsent})

		assert.Len(t, resp.Entities, 2, "both existing entities must still hydrate")
		require.Len(t, resp.Missing, 2, "both absent IDs must be reported, not dropped")

		byID := map[string]graph.MissingReason{}
		for _, m := range resp.Missing {
			byID[m.ID] = m.Reason
		}
		assert.Equal(t, graph.MissingNotFound, byID[absent])
		assert.Equal(t, graph.MissingNotFound, byID[alsoAbsent])

		// Every requested ID is accounted for in exactly one of the two lists. This is
		// the property that makes the response reconcilable at all — without it a
		// caller is back to diffing sets and guessing.
		accounted := map[string]int{}
		for _, e := range resp.Entities {
			accounted[e.ID]++
		}
		for _, m := range resp.Missing {
			accounted[m.ID]++
		}
		for _, id := range []string{present, absent, alsoPresent, alsoAbsent} {
			assert.Equal(t, 1, accounted[id], "id %s must appear exactly once across entities+missing", id)
		}
	})

	t.Run("a fully hydrated batch omits the field entirely", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"ids": []string{present, alsoPresent}})
		require.NoError(t, err)
		raw, err := c.handleQueryBatchNATS(ctx, body)
		require.NoError(t, err)

		// Asserted on the RAW wire, not the decoded struct: the compatibility claim is
		// that the bytes an old consumer sees are unchanged, which a struct with an
		// empty slice would hide.
		var keyed map[string]any
		require.NoError(t, json.Unmarshal(raw, &keyed))
		_, present := keyed["missing"]
		assert.False(t, present, "a complete batch must not grow a missing key: %s", raw)
		assert.Contains(t, keyed, "entities", "the pre-existing field must keep its name")
	})

	t.Run("an all-missing batch is a success with an empty entity list", func(t *testing.T) {
		resp := query(t, []string{absent, alsoAbsent})
		assert.Empty(t, resp.Entities)
		assert.Len(t, resp.Missing, 2,
			"an all-missing batch must report every ID rather than looking like an empty graph")
	})
}
