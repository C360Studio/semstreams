//go:build integration

package fusionnats

import (
	"context"
	"encoding/json"
	"errors"
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
		return json.Marshal(graph.EntityState{ID: req.ID, Triples: []message.Triple{
			{Subject: req.ID, Predicate: "dc.terms.title", Object: "Widget"},
		}})
	})
	subscribe(t, ctx, nc, subjectBatch, func(_ []byte) ([]byte, error) {
		return json.Marshal(map[string]any{"entities": []graph.EntityState{
			{ID: "a.b.c.d.e.1"}, {ID: "a.b.c.d.e.2"},
		}})
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
			return statusErr != nil && st == fusion.IndexStatus{}
		}, 10*time.Second, 20*time.Millisecond,
			"an unpublished readiness key must not read as a status")
	})

	t.Run("Resolve_symbol", func(t *testing.T) {
		ids, err := c.Resolve(ctx, fusion.ResolveQuery{Query: "Widget", Mode: fusion.ResolveModeSymbol, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"a.b.c.d.e.1", "a.b.c.d.e.2"}, ids)
	})

	t.Run("Resolve_prefix", func(t *testing.T) {
		ids, err := c.Resolve(ctx, fusion.ResolveQuery{Query: "a.b.c", Mode: fusion.ResolveModePrefix, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"a.b.c.d.e.1"}, ids)
	})

	t.Run("Resolve_nl", func(t *testing.T) {
		ids, err := c.Resolve(ctx, fusion.ResolveQuery{Query: "find a widget", Mode: fusion.ResolveModeNL, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, []string{"a.b.c.d.e.9"}, ids)
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
		require.Len(t, ents, 2)
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
