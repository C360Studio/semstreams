//go:build integration

package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
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

// TestIntegration_QueryByType_ListsFromEntityStates is the RC-6 walked path for
// KVKeyLister: the new exported surface driven end to end against a real NATS
// through the production registration wire and the production catalog-reader
// adapter, not a mock.
//
// It asserts the three things the unit tests cannot: that a fixed-position
// wildcard filter is a shape real NATS actually serves over ENTITY_STATES,
// that the executor's own sort produces a deterministic order over whatever
// scan order the server hands back, and that a cursor issued by one page
// continues correctly on the next. The cancelled-context arm mirrors the
// precedent this listing shares its collector with
// (processor/graph-index/owner_filter_integration_test.go): a partial key list
// is never returned as success.
func TestIntegration_QueryByType_ListsFromEntityStates(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	// The owner provisions ENTITY_STATES the way graph-ingest does.
	_, err := graph.EnsureCatalogBucket(ctx, client, graph.BucketEntityStates)
	require.NoError(t, err)
	bucket, err := client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	// Two systems, one domain, two types. Written through MarshalEntityState,
	// so every fixture is a record the authority would accept.
	sensors := []string{
		"acme.itest.gcs.environmental.temperature.s3",
		"acme.itest.gcs.environmental.temperature.s1",
		"acme.itest.hvac.environmental.temperature.s2",
	}
	others := []string{
		"acme.itest.gcs.robotics.drone.d1",
		"acme.itest.gcs.environmental.humidity.h1",
	}
	for _, id := range append(append([]string{}, sensors...), others...) {
		entity := &graph.EntityState{
			ID: id,
			Triples: []message.Triple{{
				Subject: id, Predicate: "sensor.reading.value", Object: 1.0,
				Source: "itest", Timestamp: time.Unix(0, 0).UTC(), Confidence: 1,
			}},
			MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
			UpdatedAt:   time.Unix(0, 0).UTC(),
		}
		data, marshalErr := graph.MarshalEntityState(entity)
		require.NoError(t, marshalErr)
		_, putErr := bucket.Put(ctx, id, data)
		require.NoError(t, putErr)
	}

	reg, err := registerGraphQueryTools(ctx, t, client)
	require.NoError(t, err)

	sortedSensors := append([]string{}, sensors...)
	sort.Strings(sortedSensors)

	call := func(t *testing.T, id, entityType string, limit int, cursor string) agentic.ToolResult {
		t.Helper()
		args := map[string]any{"entity_type": entityType, "limit": float64(limit)}
		if cursor != "" {
			args["cursor"] = cursor
		}
		result, execErr := reg.Execute(ctx, agentic.ToolCall{ID: id, Name: "query_by_type", Arguments: args})
		require.NoError(t, execErr)
		require.Empty(t, result.Error, "query_by_type must succeed against a provisioned bucket")
		return result
	}

	page := func(t *testing.T, result agentic.ToolResult) (map[string]any, []string) {
		t.Helper()
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(result.Content), &parsed))
		raw, ok := parsed["entity_ids"].([]any)
		require.True(t, ok, "entity_ids must be an array: %s", result.Content)
		ids := make([]string, 0, len(raw))
		for _, item := range raw {
			ids = append(ids, item.(string))
		}
		return parsed, ids
	}

	t.Run("the type segment selects, and the order is deterministic", func(t *testing.T) {
		result := call(t, "itest-all", "temperature", 10, "")
		body, ids := page(t, result)
		assert.Equal(t, "*.*.*.*.temperature.*", body["pattern"])
		assert.Equal(t, sortedSensors, ids,
			"real NATS returns scan order; the sorted order is the executor's own work")
		assert.Equal(t, float64(len(sensors)), body["matched"])
		assert.Equal(t, false, result.Metadata[agentic.MetadataKeyHasMore])
	})

	t.Run("three right-anchored tokens narrow to one system", func(t *testing.T) {
		result := call(t, "itest-three", "gcs.environmental.temperature", 10, "")
		body, ids := page(t, result)
		assert.Equal(t, "*.*.gcs.environmental.temperature.*", body["pattern"])
		assert.Equal(t, []string{
			"acme.itest.gcs.environmental.temperature.s1",
			"acme.itest.gcs.environmental.temperature.s3",
		}, ids)
	})

	t.Run("one cursor continuation across two pages", func(t *testing.T) {
		first := call(t, "itest-p1", "temperature", 2, "")
		_, firstIDs := page(t, first)
		require.Equal(t, sortedSensors[:2], firstIDs)
		require.Equal(t, true, first.Metadata[agentic.MetadataKeyHasMore])
		require.Equal(t, agentic.HintTooLarge, first.ResultHint)
		cursor, ok := first.Metadata[agentic.MetadataKeyNextCursor].(string)
		require.True(t, ok, "an intermediate page must carry a continuation token")

		second := call(t, "itest-p2", "temperature", 2, cursor)
		secondBody, secondIDs := page(t, second)
		assert.Equal(t, sortedSensors[2:], secondIDs, "the pages partition the match exactly once")
		assert.Equal(t, float64(len(sensors)), secondBody["matched"],
			"matched stays the whole match count on every page")
		assert.Equal(t, false, second.Metadata[agentic.MetadataKeyHasMore])
		assert.NotContains(t, second.Metadata, agentic.MetadataKeyNextCursor)
	})

	t.Run("nothing of that type is classified empty, not absent", func(t *testing.T) {
		result := call(t, "itest-none", "nonesuch", 10, "")
		body, ids := page(t, result)
		assert.Empty(t, ids)
		assert.Equal(t, float64(0), body["matched"])
		assert.Equal(t, agentic.HintEmpty, result.ResultHint)
	})

	t.Run("a cancelled context yields the context error and no partial list", func(t *testing.T) {
		// The NATS KeyLister closes its channel on cancellation without a
		// terminal error, so a collector that returned what it had would turn
		// a partial snapshot into an authoritative answer. This is the arm
		// processor/graph-index/owner_filter_integration_test.go pins on the
		// same collector; the adapter inherits it through
		// natsclient.FilteredKeys.
		cancelled, cancelNow := context.WithCancel(ctx)
		cancelNow()
		adapter := &graphQueryKVAdapter{natsClient: client}
		keys, listErr := adapter.KeysByPattern(cancelled, "*.*.*.*.temperature.*")
		require.Error(t, listErr)
		assert.ErrorIs(t, listErr, context.Canceled)
		assert.Nil(t, keys, "a partial key list is never returned as success")
	})

	t.Run("a rejected entity_type never reaches NATS", func(t *testing.T) {
		result, execErr := reg.Execute(ctx, agentic.ToolCall{
			ID: "itest-wild", Name: "query_by_type",
			Arguments: map[string]any{"entity_type": "*"},
		})
		require.NoError(t, execErr)
		assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind,
			fmt.Sprintf("a wildcard entity_type must be refused, got %q", result.Content))
	})
}
