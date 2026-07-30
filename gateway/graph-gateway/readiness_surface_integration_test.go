//go:build integration

package graphgateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// publishStatus writes one producer's envelope to its GRAPH_STATUS key.
func publishStatus(
	ctx context.Context, t *testing.T, tc *natsclient.TestClient,
	key string, status graph.IndexStatusResponse,
) {
	t.Helper()
	bucket, err := tc.CreateKVBucket(ctx, readiness.BucketGraphStatus)
	require.NoError(t, err)
	payload, err := json.Marshal(status)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, key, payload)
	require.NoError(t, err)
}

func readSurface(t *testing.T, c *Component) readinessSurfaceResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	c.handleReadiness(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body readinessSurfaceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// TestIntegration_ReadinessSurface_QuietFeedIsDistinguishableFromNotReady is the
// requirement's load-bearing scenario, driven over a real bucket.
//
// One producer publishes a NOT-READY envelope; another publishes nothing at all. The
// operator must be able to tell those apart FROM THE SURFACE ALONE — no log
// correlation. That distinction is the reason `known` and `fresh` are separate fields:
// conflating "never saw this producer" with "saw it, then it went quiet" cost three
// investigation cycles once already (gh#590).
func TestIntegration_ReadinessSurface_QuietFeedIsDistinguishableFromNotReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	// graph-ingest published a real, honest not-ready envelope.
	publishStatus(ctx, t, tc, readiness.KeyGraphIngest, graph.IndexStatusResponse{
		State:             graph.IndexStateBuilding,
		Ready:             false,
		Lag:               42,
		BootstrapComplete: true,
		BootstrapScope:    900,
	})
	// rule published NOTHING.

	cfg := DefaultConfig()
	cfg.ReadinessKeys = []string{readiness.KeyGraphIngest, readiness.KeyRule}
	c := &Component{
		config:     cfg,
		logger:     slog.New(slog.DiscardHandler),
		natsClient: tc.Client,
	}
	require.NoError(t, c.startReadinessSet(ctx))
	defer c.stopReadinessSet()

	var body readinessSurfaceResponse
	require.Eventually(t, func() bool {
		body = readSurface(t, c)
		if len(body.Producers) != 2 {
			return false
		}
		return body.Producers[0].Known
	}, 20*time.Second, 100*time.Millisecond,
		"the published producer must become known on the surface")

	// Rows are in deterministic (sorted) key order.
	require.Equal(t, readiness.KeyGraphIngest, body.Producers[0].Key)
	require.Equal(t, readiness.KeyRule, body.Producers[1].Key)

	// The producer that published: KNOWN, and shown not-ready with its real numbers.
	ingest := body.Producers[0]
	require.True(t, ingest.Known, "a producer that published must be known")
	require.False(t, ingest.Ready, "it published a not-ready envelope")
	require.Equal(t, graph.IndexStateBuilding, ingest.State)
	require.EqualValues(t, 42, ingest.Lag, "the surface must echo the real backlog")
	require.EqualValues(t, 900, ingest.BootstrapScope)

	// The producer that never published: UNKNOWN. This is the distinction the whole
	// scenario exists for — it is NOT "ready", and it is NOT "not ready with lag 0".
	rule := body.Producers[1]
	require.False(t, rule.Known,
		"a producer that never published must be shown unknown, not as a healthy zero")
	require.False(t, rule.Fresh, "an unknown producer cannot be fresh")
	require.False(t, rule.Ready)
}

// TestIntegration_ReadinessSurface_EchoesEveryConfiguredKey guards against the surface
// quietly dropping a declared key — an operator reading it must see every producer
// they configured, including ones that are perfectly healthy.
func TestIntegration_ReadinessSurface_EchoesEveryConfiguredKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	keys := []string{readiness.KeyGraphIngest, readiness.KeyGraphIndex, readiness.KeyRule}
	for _, k := range keys {
		publishStatus(ctx, t, tc, k, graph.IndexStatusResponse{
			State:             graph.IndexStateReady,
			Ready:             true,
			BootstrapComplete: true,
		})
	}

	cfg := DefaultConfig()
	cfg.ReadinessKeys = keys
	c := &Component{
		config:     cfg,
		logger:     slog.New(slog.DiscardHandler),
		natsClient: tc.Client,
	}
	require.NoError(t, c.startReadinessSet(ctx))
	defer c.stopReadinessSet()

	require.Eventually(t, func() bool {
		body := readSurface(t, c)
		if len(body.Producers) != len(keys) {
			return false
		}
		for _, p := range body.Producers {
			if !p.Known || !p.Ready {
				return false
			}
		}
		return true
	}, 20*time.Second, 100*time.Millisecond,
		"every configured key must appear and report its published state")

	// Still no aggregate verdict, even when every producer is ready — that is exactly
	// the case where someone would be tempted to add one.
	rec := httptest.NewRecorder()
	c.handleReadiness(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))
	var wire map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wire))
	for _, banned := range []string{"ready", "proceed", "ok", "covered", "verdict"} {
		require.NotContains(t, wire, banned,
			"aggregate verdict %q leaked when all producers were ready: %s", banned, rec.Body.String())
	}
}
