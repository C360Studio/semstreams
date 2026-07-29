//go:build integration

package executors

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
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
// graph_query group only, returning the registry for tool-presence assertions.
func registerGraphQueryTools(ctx context.Context, t *testing.T, client *natsclient.Client) (*agentictools.ExecutorRegistry, error) {
	t.Helper()
	reg := agentictools.NewExecutorRegistry()
	err := RegisterBuiltins(ctx, reg, ToolDependencies{
		NATSClient:   client,
		SkipBuiltins: skipAllBut("graph_query"),
	})
	return reg, err
}

// TestIntegration_GraphQueryTools_LazyBindSurvivesCleanBoot is the clean-boot
// production-order contract: both mains call RegisterBuiltins BEFORE
// Manager.StartAll, so on a first install ENTITY_STATES does not exist yet
// when the graph-query tools register (graph-ingest provisions it inside its
// component Start). The five query tools MUST still be registered — the
// registry is built once per process, so a registration-time skip is a
// PERMANENT loss of the tools — and each execution must resolve the bucket
// lazily: classified not-ready (naming the owner) until graph-ingest has
// provisioned it, then working reads with NO re-registration.
func TestIntegration_GraphQueryTools_LazyBindSurvivesCleanBoot(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	// Clean NATS: ENTITY_STATES absent (the first-install shape).
	_, err := client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound)

	// Production-order registration: BEFORE the owner exists.
	reg, err := registerGraphQueryTools(ctx, t, client)
	require.NoError(t, err)

	// The tools ARE registered despite the absent bucket.
	require.NotNil(t, reg.GetTool("query_entity"),
		"query_entity must be registered on a clean boot — the registry is built once per process, "+
			"so skipping here loses the tool for the process lifetime")

	// Executing before the owner provisions → classified not-ready naming the
	// owner; and the reader must STILL not have created the bucket.
	result, execErr := reg.Execute(ctx, agentic.ToolCall{
		ID: "call-1", Name: "query_entity",
		Arguments: map[string]any{"entity_id": "acme.test.graph.kv.entity.001"},
	})
	require.Error(t, execErr, "executing against a not-yet-provisioned bucket must error")
	assert.Contains(t, result.Error, "not ready", "the tool result must carry the not-ready shape")
	assert.Contains(t, result.Error, "graph-ingest", "the not-ready error must name the catalog owner")
	_, err = client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"lazy resolution must never create the bucket (#714 holds at execution time too)")

	// The owner provisions ENTITY_STATES the way graph-ingest does — through
	// the catalog seam.
	_, err = graph.EnsureCatalogBucket(ctx, client, graph.BucketEntityStates)
	require.NoError(t, err)

	// The SAME registration now serves reads: a missing entity comes back as
	// a clean not-found TOOL RESULT (nil error), proving the bind succeeded
	// with no re-registration.
	result, execErr = reg.Execute(ctx, agentic.ToolCall{
		ID: "call-2", Name: "query_entity",
		Arguments: map[string]any{"entity_id": "acme.test.graph.kv.entity.001"},
	})
	require.NoError(t, execErr,
		"after the owner provisions, the lazily-bound tool must execute without re-registration")
	assert.Equal(t, agentic.ToolErrorNotFound, result.ErrorKind,
		"a missing entity on a live bucket is a not-found result, not a bind failure")
}

// TestIntegration_RegisterBuiltins_GraphQueryNeverCreatesEntityStates is the
// #714-closing reader-creates assertion driven through the production
// RegisterBuiltins wire: registering the graph-query tools against a NATS with
// no ENTITY_STATES bucket must NOT create the bucket — a reader that creates is
// an emitter of divergent configuration (the F1 History race). The tools are
// REGISTERED (lazy bind, not-ready until the owner provisions — see the
// clean-boot test); the bucket stays absent for its owner (graph-ingest) to
// create through the catalog seam.
func TestIntegration_RegisterBuiltins_GraphQueryNeverCreatesEntityStates(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	// Precondition: ENTITY_STATES is absent.
	_, err := client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"precondition: ENTITY_STATES must not exist before tool registration")

	reg, err := registerGraphQueryTools(ctx, t, client)
	require.NoError(t, err, "an absent bucket must not fail registration")
	require.NotNil(t, reg.GetTool("query_entity"),
		"the tools must be registered (lazy bind), not skipped for the process lifetime")

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
		_, err = registerGraphQueryTools(ctx, t, client)
		require.NoError(t, err)
		require.Equal(t, declared, entityStatesHistory(ctx, t, client),
			"tool registration must not perturb the owner's declared bucket config")
	})
}
