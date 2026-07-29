//go:build integration

package executors

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// entityStatesHistory reads the live History (stream MaxMsgsPerSubject) of
// ENTITY_STATES.
func entityStatesHistory(ctx context.Context, t *testing.T, client *natsclient.Client) int64 {
	t.Helper()
	bucket, err := client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	status, err := bucket.Status(ctx)
	require.NoError(t, err)
	return status.History()
}

// skipAllBut returns a SkipBuiltins list covering every builtin group except
// keep, so a test can drive exactly one register_* function through the
// production RegisterBuiltins wire.
func skipAllBut(keep string) []string {
	skip := make([]string, 0, len(BuiltinGroupKeys))
	for _, k := range BuiltinGroupKeys {
		if k != keep {
			skip = append(skip, k)
		}
	}
	return skip
}

// registerGraphQueryTools drives the production tool-registration path for the
// graph_query group only.
func registerGraphQueryTools(ctx context.Context, t *testing.T, client *natsclient.Client) error {
	t.Helper()
	reg := agentictools.NewExecutorRegistry()
	return RegisterBuiltins(ctx, reg, ToolDependencies{
		NATSClient:   client,
		SkipBuiltins: skipAllBut("graph_query"),
	})
}

// TestIntegration_RegisterBuiltins_GraphQueryNeverCreatesEntityStates is the
// #714-closing reader-creates assertion driven through the production
// RegisterBuiltins wire: registering the graph-query tools against a NATS with
// no ENTITY_STATES bucket must NOT create the bucket — a reader that creates is
// an emitter of divergent configuration (the F1 History race). The tools are
// warn-and-skipped; the bucket stays absent for its owner (graph-ingest) to
// create through the catalog seam.
func TestIntegration_RegisterBuiltins_GraphQueryNeverCreatesEntityStates(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	// Precondition: ENTITY_STATES is absent.
	_, err := client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"precondition: ENTITY_STATES must not exist before tool registration")

	require.NoError(t, registerGraphQueryTools(ctx, t, client),
		"a missing bucket is a warn-and-skip, not a registration error")

	// The #714 closure: the reader path must not have created the bucket.
	_, err = client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"graph-query tool registration must NEVER create ENTITY_STATES (reader-creates class, #714)")
}

// TestIntegration_EntityStatesHistory_NoLongerDecidedByBootOrder is the F1
// regression: ENTITY_STATES History used to be a boot race — the (retired)
// tool-registration create stamped History 3 while graph-ingest stamped
// History 1, and whoever landed first won, with adoption never comparing.
// Now the graph's actual History equals the catalog declaration (1) in BOTH
// orders: an adopted divergent bucket is reconciled at the owner's seam
// acquisition, and the tool path never creates at all.
func TestIntegration_EntityStatesHistory_NoLongerDecidedByBootOrder(t *testing.T) {
	entityStatesSpec, ok := graph.SpecFor(graph.BucketEntityStates)
	require.True(t, ok)
	declared := int64(entityStatesSpec.History)
	require.Equal(t, int64(1), declared, "owner decision 2026-07-28: ENTITY_STATES History = 1")

	t.Run("legacy tool-path create first, then the owner acquires", func(t *testing.T) {
		ctx := context.Background()
		tc := natsclient.NewTestClient(t, natsclient.WithKV())
		client := tc.Client

		// The pre-catalog tool-registration create (History 3) having won the
		// race — the adopted-divergent shape a live deploy may carry.
		_, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
			Bucket:  graph.BucketEntityStates,
			History: 3,
		})
		require.NoError(t, err)
		require.Equal(t, int64(3), entityStatesHistory(ctx, t, client), "precondition: divergent adopt")

		// The owner (graph-ingest's acquisition path) acquires through the seam.
		_, err = graph.EnsureCatalogBucket(ctx, client, graph.BucketEntityStates)
		require.NoError(t, err)
		require.Equal(t, declared, entityStatesHistory(ctx, t, client),
			"the owner's acquisition must reconcile the adopted History to the catalog")
	})

	t.Run("owner first, then the tool path registers", func(t *testing.T) {
		ctx := context.Background()
		tc := natsclient.NewTestClient(t, natsclient.WithKV())
		client := tc.Client

		_, err := graph.EnsureCatalogBucket(ctx, client, graph.BucketEntityStates)
		require.NoError(t, err)
		require.Equal(t, declared, entityStatesHistory(ctx, t, client))

		// The production tool registration binds read-only and registers tools.
		require.NoError(t, registerGraphQueryTools(ctx, t, client))
		require.Equal(t, declared, entityStatesHistory(ctx, t, client),
			"tool registration must not perturb the owner's declared bucket config")
	})
}
