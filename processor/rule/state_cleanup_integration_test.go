//go:build integration

package rule

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// TestEntityWatcher_DeletedEntityCleansRuleState is the gh#358 regression lock:
// when an entity is deleted, the watcher cleans up its per-(rule,entity) $state,
// so an entity recreated/reused at the same ID does not inherit a stale,
// exhausted max_iterations budget (which would silently never re-fire). It also
// proves the boundary-aware match: a prefix-sibling's state survives.
func TestEntityWatcher_DeletedEntityCleansRuleState(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	t.Cleanup(func() { _ = testClient.Terminate() })
	nc := testClient.Client
	ctx := context.Background()

	js, err := nc.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(ctx, "ENTITY_STATES")
	if err != nil {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "ENTITY_STATES"})
		require.NoError(t, err)
	}

	config := DefaultConfig()
	config.EntityWatchBuckets = map[string][]string{gtypes.BucketEntityStates: {"gh358.test.*.*.*.*"}}
	proc, err := NewProcessor(nc, &config)
	require.NoError(t, err)
	require.NoError(t, proc.Initialize())
	require.NoError(t, proc.Start(ctx))
	t.Cleanup(func() { _ = proc.Stop(5 * time.Second) })
	require.NotNil(t, proc.stateTracker, "state tracker must initialize")

	const ruleID = "retry-rule"
	const x = "gh358.test.dom.sys.unit.1"
	const x10 = "gh358.test.dom.sys.unit.10"   // dotted prefix sibling of x
	const xUnd = "gh358.test.dom.sys.unit.1_a" // underscore prefix sibling of x (live entity)

	siblings := []string{x, x10, xUnd}

	// Seed all entities into ENTITY_STATES so the watcher has live keys.
	for _, id := range siblings {
		data, merr := json.Marshal(gtypes.EntityState{ID: id})
		require.NoError(t, merr)
		_, perr := kv.Put(ctx, id, data)
		require.NoError(t, perr)
	}

	// Pre-set an EXHAUSTED retry budget (Iteration == MaxIterations) for each —
	// the state a prior run would have left behind.
	for _, id := range siblings {
		require.NoError(t, proc.stateTracker.Set(ctx, MatchState{
			RuleID: ruleID, EntityKey: id, IsMatching: true,
			Iteration: 3, MaxIterations: 3, LastChecked: time.Now(),
		}))
	}

	// Delete x — the watcher's DELETED branch must clean x's rule state.
	require.NoError(t, kv.Delete(ctx, x))

	// x's state is cleaned up (the watcher processes the delete asynchronously).
	require.Eventually(t, func() bool {
		_, gerr := proc.stateTracker.Get(ctx, ruleID, x)
		return errors.Is(gerr, ErrStateNotFound)
	}, 5*time.Second, 50*time.Millisecond, "deleted entity's rule state must be cleaned up")

	// The prefix siblings survive — the exact suffix match did not over-delete.
	// x10 guards the dotted-prefix case; xUnd guards the underscore-prefix case
	// ("_" is legal in an instance segment, so "unit.1" must not match "unit.1_a").
	for _, sib := range []string{x10, xUnd} {
		got, gerr := proc.stateTracker.Get(ctx, ruleID, sib)
		require.NoErrorf(t, gerr, "prefix-sibling %q rule state must NOT be deleted", sib)
		require.Equalf(t, 3, got.Iteration, "sibling %q budget untouched", sib)
	}
}
