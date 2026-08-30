//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// queryEntityViaPrefix reads a single entity back through the cached
// graph.ingest.query.prefix path (the read path the gated-DAG executor and other
// whole-set readers use). Returns nil if absent.
func queryEntityViaPrefix(t *testing.T, ctx context.Context, nc *natsclient.Client, id string) *graph.EntityState {
	t.Helper()
	reqData, err := json.Marshal(graph.PrefixQueryRequest{Prefix: id, Limit: 10})
	require.NoError(t, err)
	respData, err := nc.RequestClassified(ctx, "graph.ingest.query.prefix", reqData, 5*time.Second)
	require.NoError(t, err)
	var resp graph.PrefixQueryResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	for i := range resp.Entities {
		if resp.Entities[i].ID == id {
			return &resp.Entities[i]
		}
	}
	return nil
}

// TestIntegration_CacheCoherence_AppendVisibleAfterPrefixRead is the
// regression lock for the read-after-write gap the gated-DAG executor surfaced:
// the entity-query cache (30s TTL) was NOT invalidated by the NATS mutation
// handlers, so a query within the TTL after an append
// returned the pre-write entity. The executor read a unit's claim marker as
// absent right after committing it and re-dispatched on every pass.
func TestIntegration_CacheCoherence_AppendVisibleAfterPrefixRead(t *testing.T) {
	ctx := context.Background()
	c, nc := startPrefixTestComponent(t, withAuthority("coh", "ops"))

	const id = "coh.ops.dom.sys.type.entity1"
	seedPrefixEntity(t, ctx, c, id)

	// Prime the cache: read once via the prefix path.
	got := queryEntityViaPrefix(t, ctx, nc, id)
	require.NotNil(t, got)
	require.Nil(t, got.GetTriple("test.coherence.marker"), "marker not present yet")

	// Commit a new predicate via canonical append.
	updReq := graph.AppendTriplesRequest{Triples: []message.Triple{{Subject: id, Predicate: "test.coherence.marker", Object: "v1", Timestamp: time.Now(), Confidence: 1.0}}}
	updData, err := json.Marshal(updReq)
	require.NoError(t, err)
	_, err = nc.RequestClassified(ctx, "graph.mutation.triple.append", updData, 5*time.Second)
	require.NoError(t, err)

	// The very next prefix read MUST reflect the new marker (no stale cache).
	got = queryEntityViaPrefix(t, ctx, nc, id)
	require.NotNil(t, got)
	require.NotNil(t, got.GetTriple("test.coherence.marker"), "append must be visible on the next prefix read (cache invalidated)")

	// Same contract for a second append.
	addReq := graph.AppendTriplesRequest{Triples: []message.Triple{{Subject: id, Predicate: "test.coherence.added", Object: "v2", Timestamp: time.Now(), Confidence: 1.0}}}
	addData, err := json.Marshal(addReq)
	require.NoError(t, err)
	_, err = nc.RequestClassified(ctx, "graph.mutation.triple.append", addData, 5*time.Second)
	require.NoError(t, err)

	got = queryEntityViaPrefix(t, ctx, nc, id)
	require.NotNil(t, got)
	require.NotNil(t, got.GetTriple("test.coherence.added"), "append must be visible on the next prefix read (cache invalidated)")
}
