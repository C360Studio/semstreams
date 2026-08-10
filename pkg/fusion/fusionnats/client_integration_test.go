//go:build integration

package fusionnats

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// TestIntegration_RetrievalClient_RealWire drives every RetrievalClient method
// over a REAL NATS round-trip via RequestClassified, against thin handlers that
// return the production response shapes. This is the consumer-side gate the
// semstreams-reviewer required when PR #402 (B1) landed without it: it locks the
// subject strings AND the RequestClassified decode/mapping that a unit test with
// a fake requester cannot exercise. A wrong subject or a classify mismatch ships
// red here instead of green (feedback_integration_tests_must_drive_production_wire).
func TestIntegration_RetrievalClient_RealWire(t *testing.T) {
	ctx := context.Background()
	// KV is required now that readiness is KV state rather than a subject.
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	// Readiness is NOT a subject after ADR-083: seed the real GRAPH_STATUS bucket
	// through the shared producer-side helper, so this test locks the same bucket,
	// key, and value encoding the producers write and every consumer reads. A drift
	// between the two sides ships red here rather than as a permanently-unknown
	// readiness in production.
	statusBucket, err := readiness.EnsureBucket(ctx, nc)
	require.NoError(t, err)
	envelope, err := json.Marshal(graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady})
	require.NoError(t, err)
	_, err = statusBucket.Put(ctx, readiness.KeyGraphIndex, envelope)
	require.NoError(t, err)

	// Register a handler on each public subject the client maps onto, returning
	// the exact production response shape. SubscribeForRequests applies the
	// ADR-060 classified-error wrapper, so a returned ClassifiedError round-trips
	// its Code on the wire — which is what Entity's not-found translation needs.
	subscribe(t, ctx, nc, subjectByName, func(_ []byte) ([]byte, error) {
		return json.Marshal(graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{
			{EntityID: "a.b.c.d.e.1", MatchedName: "Widget"},
			{EntityID: "a.b.c.d.e.2", MatchedName: "Gadget"},
		}}))
	})
	subscribe(t, ctx, nc, subjectPrefix, func(_ []byte) ([]byte, error) {
		return json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: "a.b.c.d.e.1"}}})
	})
	subscribe(t, ctx, nc, subjectSemantic, func(_ []byte) ([]byte, error) {
		return json.Marshal(map[string]any{"results": []map[string]any{
			{"entity_id": "a.b.c.d.e.9", "similarity": 0.9},
		}})
	})
	subscribe(t, ctx, nc, subjectEntity, func(data []byte) ([]byte, error) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(data, &req)
		if req.ID == "missing" {
			return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, errors.New("not found"))
		}
		return json.Marshal(graph.ExactEntity{
			Entity: &graph.EntityState{ID: req.ID, Triples: []message.Triple{
				{Subject: req.ID, Predicate: "dc.terms.title", Object: "Widget"},
			}},
			KVRevision: 7,
		})
	})
	// The batch responder MODELS the real handler rather than echoing a fixed pair:
	// it answers only for IDs it knows, reports the rest as missing, and — like
	// graph-ingest — returns them in an order that is NOT the request order. A fake
	// that ignored the request would let both the reconciliation and the order
	// restore pass while testing neither.
	known := map[string]bool{"a.b.c.d.e.1": true, "a.b.c.d.e.2": true}
	subscribe(t, ctx, nc, subjectBatch, func(data []byte) ([]byte, error) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		resp := graph.EntityBatchResponse{}
		for _, id := range req.IDs {
			if known[id] {
				resp.Entities = append(resp.Entities, graph.EntityState{ID: id})
				continue
			}
			resp.Missing = append(resp.Missing, graph.MissingEntity{
				ID: id, Reason: graph.MissingNotFound,
			})
		}
		// Reverse, standing in for graph-ingest's cache-hits-first ordering.
		for i, j := 0, len(resp.Entities)-1; i < j; i, j = i+1, j-1 {
			resp.Entities[i], resp.Entities[j] = resp.Entities[j], resp.Entities[i]
		}
		return json.Marshal(resp)
	})
	subscribe(t, ctx, nc, subjectRelationships, func(_ []byte) ([]byte, error) {
		return json.Marshal([]map[string]any{
			{"from_entity_id": "a.b.c.d.e.seed", "to_entity_id": "a.b.c.d.e.callee", "edge_type": "code.calls"},
		})
	})

	c := New(nc, 5*time.Second)

	t.Run("Status", func(t *testing.T) {
		st, err := c.Status(ctx)
		require.NoError(t, err)
		require.True(t, st.Ready)
		require.Equal(t, fusion.StateReady, st.State)
	})

	// The fail-closed half of the same real wire: a producer that stops publishing
	// leaves its key behind, and the client must refuse to serve it as live rather
	// than let a frozen index keep licensing authoritative-absence claims. Deleting
	// the key is the sharper form of the same condition (a stale key needs a clock to
	// age) and exercises the tombstone sentinel against real NATS.
	//
	// The client CONVERGES on the tombstone rather than seeing it synchronously: it
	// holds state fed by a watch, so the delete has to travel to the watcher. That
	// eventual convergence is the honest contract of distributing readiness as state
	// (ADR-083) — the guarantee is that a deleted key stops reading as live within a
	// delivery, not that it does so within the Delete call. Asserting it without a
	// wait would be asserting a synchronous read the transport no longer performs.
	t.Run("Status_absent_key_is_an_error", func(t *testing.T) {
		require.NoError(t, statusBucket.Delete(ctx, readiness.KeyGraphIndex))
		t.Cleanup(func() {
			_, putErr := statusBucket.Put(ctx, readiness.KeyGraphIndex, envelope)
			require.NoError(t, putErr)
		})

		require.Eventually(t, func() bool {
			st, statusErr := c.Status(ctx)
			return statusErr != nil && reflect.DeepEqual(st, fusion.IndexStatus{})
		}, 10*time.Second, 20*time.Millisecond,
			"an unpublished readiness key must not read as a status")
	})

	// The three resolve modes over the real wire, asserted as SEEDS: identity plus
	// whether the mode reports a score. Symbol and prefix must report none — emitting
	// a zero would advertise a perfect non-match those wires never claimed — while NL
	// must carry the similarity the semantic reply actually sent, which this decode
	// silently dropped before ADR-084 D5.
	t.Run("Resolve_symbol", func(t *testing.T) {
		seeds, err := c.Resolve(ctx, fusion.ResolveQuery{Query: "Widget", Mode: fusion.ResolveModeSymbol, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"a.b.c.d.e.1", "a.b.c.d.e.2"}, fusion.SeedIDs(seeds))
		for _, s := range seeds {
			require.False(t, s.HasSimilarity, "the byName wire carries no score")
		}
	})

	t.Run("Resolve_prefix", func(t *testing.T) {
		seeds, err := c.Resolve(ctx, fusion.ResolveQuery{Query: "a.b.c", Mode: fusion.ResolveModePrefix, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"a.b.c.d.e.1"}, fusion.SeedIDs(seeds))
		require.False(t, seeds[0].HasSimilarity, "prefix resolve is an enumeration, not a ranking")
	})

	t.Run("Resolve_nl", func(t *testing.T) {
		seeds, err := c.Resolve(ctx, fusion.ResolveQuery{Query: "find a widget", Mode: fusion.ResolveModeNL, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"a.b.c.d.e.9"}, fusion.SeedIDs(seeds))
		require.True(t, seeds[0].HasSimilarity, "the semantic wire reports a score and it must survive the decode")
		require.InDelta(t, 0.9, seeds[0].Similarity, 1e-9)
	})

	t.Run("Entity_found", func(t *testing.T) {
		ent, err := c.Entity(ctx, "a.b.c.d.e.1")
		require.NoError(t, err)
		require.NotNil(t, ent)
		require.Equal(t, "Widget", ent.First("dc.terms.title"))
	})

	t.Run("Entity_not_found_is_absence", func(t *testing.T) {
		ent, err := c.Entity(ctx, "missing")
		require.NoError(t, err, "entity_not_found must decode to (nil,nil) over the real wire")
		require.Nil(t, ent)
	})

	t.Run("Entities", func(t *testing.T) {
		ents, err := c.Entities(ctx, []string{"a.b.c.d.e.1", "a.b.c.d.e.2"})
		require.NoError(t, err)
		require.Len(t, ents.Entities, 2)
		require.Empty(t, ents.Unhydrated)
		// Request order over the real wire — the engine ranks by position, so this is
		// a relevance contract, not a presentation one.
		require.Equal(t, []string{"a.b.c.d.e.1", "a.b.c.d.e.2"},
			[]string{ents.Entities[0].ID, ents.Entities[1].ID})
	})

	t.Run("Entities_reports_unhydrated", func(t *testing.T) {
		// gh#597 end to end: an ID the handler cannot hydrate comes back NAMED, not
		// silently dropped from a shorter list.
		ents, err := c.Entities(ctx, []string{"a.b.c.d.e.1", "a.b.c.d.e.404"})
		require.NoError(t, err, "a partial batch is a success, not a fault")
		require.Len(t, ents.Entities, 1)
		require.Len(t, ents.Unhydrated, 1)
		require.Equal(t, "a.b.c.d.e.404", ents.Unhydrated[0].Handle)
	})

	t.Run("Neighbors", func(t *testing.T) {
		edges, err := c.Neighbors(ctx, "a.b.c.d.e.seed", []string{"code.calls"}, fusion.Outgoing)
		require.NoError(t, err)
		require.Len(t, edges, 1)
		require.Equal(t, "a.b.c.d.e.callee", edges[0].Target)
		require.Equal(t, "code.calls", edges[0].Predicate)
	})

	t.Run("Names", func(t *testing.T) {
		names, err := c.Names(ctx, "Wid", 5)
		require.NoError(t, err)
		require.Equal(t, []string{"Widget", "Gadget"}, names)
	})
}

// subscribe registers a request handler on subject, failing the test on error
// and cleaning up the subscription when the test ends.
func subscribe(t *testing.T, ctx context.Context, nc *natsclient.Client, subject string, fn func(data []byte) ([]byte, error)) {
	t.Helper()
	sub, err := nc.SubscribeForRequests(ctx, subject, func(_ context.Context, data []byte) ([]byte, error) {
		return fn(data)
	})
	require.NoError(t, err, "subscribe %s", subject)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}
