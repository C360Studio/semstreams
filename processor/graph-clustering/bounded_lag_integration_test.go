//go:build integration

package graphclustering

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_GraphIndexReady_BoundedLagGate drives the REAL detection gate
// (graphIndexReady → RequestClassified → JSON decode → ReadyWithinLag(tolerance))
// against a fake graph.index.query.status responder serving fabricated statuses, so
// the config tolerance is exercised through the production request/decode path rather
// than a reimplementation (ADR-082 task 2.3). A known-incomplete index (failedCount>0)
// is not a separate case here: it surfaces to this consumer AS degraded (task 2.1b),
// which the degraded case covers.
func TestIntegration_GraphIndexReady_BoundedLagGate(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var mu sync.Mutex
	var current graph.IndexStatusResponse
	setStatus := func(s graph.IndexStatusResponse) {
		mu.Lock()
		defer mu.Unlock()
		current = s
	}
	_, err := nc.SubscribeForRequests(ctx, "graph.index.query.status",
		func(_ context.Context, _ []byte) ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()
			return json.Marshal(current)
		})
	require.NoError(t, err)

	newComp := func(tol uint64) *Component {
		config := Config{
			IndexLagTolerance: tol,
			Ports: &component.PortConfig{
				Inputs:  []component.PortDefinition{{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates}},
				Outputs: []component.PortDefinition{{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex}},
			},
			DetectionIntervalStr: "1s",
			MinCommunitySize:     2,
			MaxIterations:        10,
		}
		config.ApplyDefaults()
		configJSON, err := json.Marshal(config)
		require.NoError(t, err)
		comp, err := CreateGraphClustering(configJSON, component.Dependencies{NATSClient: nc})
		require.NoError(t, err)
		return comp.(*Component)
	}

	// building constructs the status graph-index would publish while lagging.
	building := func(target, indexed uint64) graph.IndexStatusResponse {
		return graph.ComputeIndexStatus(indexed, target, false, "ts")
	}

	t.Run("tolerance 100", func(t *testing.T) {
		c := newComp(100)
		cases := []struct {
			name      string
			status    graph.IndexStatusResponse
			wantReady bool
			wantLag   uint64
		}{
			{"bounded lag within tolerance runs", building(500, 450), true, 50},
			{"exactly at the tolerance boundary runs", building(500, 400), true, 100},
			{"lag beyond tolerance defers", building(500, 350), false, 150},
			{"degraded defers at any tolerance", graph.IndexStatusResponse{State: graph.IndexStateDegraded, TargetRevision: 500, Lag: 5}, false, 5},
			{"reset_required defers at any tolerance", graph.IndexStatusResponse{State: graph.IndexStateResetRequired}, false, 0},
			{"empty graph defers (target 0)", building(0, 0), false, 0},
			{"caught up runs", building(500, 500), true, 0},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				setStatus(tc.status)
				ready, lag, statusKnown := c.graphIndexReady(ctx)
				assert.Equal(t, tc.wantReady, ready, "readiness decision")
				assert.Equal(t, tc.wantLag, lag, "reported lag")
				assert.True(t, statusKnown, "a parsed status is always known")
			})
		}
	})

	t.Run("tolerance 0 is bit-identical to the exact Ready gate", func(t *testing.T) {
		c := newComp(0)
		battery := []graph.IndexStatusResponse{
			building(100, 100), // caught up → Ready true
			building(100, 60),  // lagging → Ready false
			building(0, 0),     // empty → Ready false
			{State: graph.IndexStateDegraded, TargetRevision: 100, Lag: 10},
			{State: graph.IndexStateResetRequired},
		}
		for _, s := range battery {
			setStatus(s)
			ready, _, _ := c.graphIndexReady(ctx)
			assert.Equal(t, s.Ready, ready, "tolerance 0 must equal exact Ready for %+v", s)
		}
	})
}
