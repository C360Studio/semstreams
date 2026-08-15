//go:build integration

package rule_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/stretchr/testify/require"
)

// TestIntegration_EmptyPatternStillDeliversTheNilSentinel is task 4.6, and it is the
// load-bearing assumption of the whole rule producer.
//
// `bootstrap_complete && bootstrap_scope == 0` is defined to mean "authoritatively
// nothing to replay". That is only true if NATS delivers its end-of-replay nil
// sentinel EVEN WHEN THE PATTERN MATCHES NOTHING. If it did not, an empty pattern
// would sit forever mid-replay and the producer would report never-bootstrapped on a
// perfectly healthy deployment.
//
// Unit coverage exists (entity_watcher_atomic_bootstrap_test.go), but the behavior
// belongs to the SERVER and depends on the watch being created without UpdatesOnly.
// The task text says verify, don't assume — so this drives a real bucket on a real
// server rather than a fake.
func TestIntegration_EmptyPatternStillDeliversTheNilSentinel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	bucket, err := tc.CreateKVBucket(ctx, gtypes.BucketEntityStates)
	require.NoError(t, err, "create ENTITY_STATES")

	// Watch a pattern that matches nothing at all in an empty bucket.
	watcher, err := bucket.Watch(ctx, "nothing.matches.this.*.*.*")
	require.NoError(t, err)
	defer func() { _ = watcher.Stop() }()

	select {
	case entry, ok := <-watcher.Updates():
		require.True(t, ok, "updates channel closed instead of delivering the sentinel")
		require.Nil(t, entry,
			"expected the nil end-of-replay sentinel as the FIRST delivery on an empty pattern")
	case <-time.After(20 * time.Second):
		t.Fatal("no nil sentinel arrived for an empty pattern — " +
			"`complete && scope == 0` would never be reachable and every consumer " +
			"would defer forever on a healthy deployment")
	}
}

// TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo drives the
// real processor: with a configured pattern that matches no existing entity, it must
// publish bootstrap_complete with scope 0 — the exact distinction gh#732 asked for and
// which was not recoverable from the wire before this change.
func TestIntegration_RuleReadiness_EmptyReplayIsAuthoritativelyNothingToDo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	_, err := tc.CreateKVBucket(ctx, gtypes.BucketEntityStates)
	require.NoError(t, err)

	config, cerr := rule.NewConfig("readiness-empty-replay")
	require.NoError(t, cerr)
	config.EntityWatchBuckets = map[string][]string{
		gtypes.BucketEntityStates: {"c360.readiness.empty.*.*.*"},
	}

	processor, err := rule.NewProcessorWithMetrics(tc.Client, &config, nil)
	require.NoError(t, err)
	require.NoError(t, processor.Initialize())
	require.NoError(t, processor.Start(ctx))
	t.Cleanup(func() { _ = processor.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		status, ok := readRuleEnvelope(ctx, t, tc)
		return ok && status.BootstrapComplete
	}, 30*time.Second, 200*time.Millisecond,
		"an empty replay must still reach bootstrap_complete")

	status, _ := readRuleEnvelope(ctx, t, tc)
	require.Zero(t, status.BootstrapScope,
		"an empty replay must report scope 0 — that is what makes it distinguishable "+
			"from a replay that had work")
	require.Equal(t, gtypes.IndexStateReady, status.State)
	require.True(t, status.Ready)
}

// TestIntegration_RuleReadiness_NonEmptyReplayReportsScope is the other half of the
// distinction: a pattern that matches existing entities must report a NON-zero scope,
// so a consumer can tell "there was nothing to do" from "there was work and it is done".
func TestIntegration_RuleReadiness_NonEmptyReplayReportsScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	bucket, err := tc.CreateKVBucket(ctx, gtypes.BucketEntityStates)
	require.NoError(t, err)

	// Seed entities BEFORE the processor starts, so its watcher replays them.
	const seeded = 3
	for i := 0; i < seeded; i++ {
		id := "c360.readiness.scope.sensor.unit." + string(rune('a'+i))
		entity := gtypes.EntityState{ID: id, Version: 1, UpdatedAt: time.Now()}
		payload, merr := json.Marshal(entity)
		require.NoError(t, merr)
		_, perr := bucket.Put(ctx, id, payload)
		require.NoError(t, perr)
	}

	config, cerr := rule.NewConfig("readiness-scope")
	require.NoError(t, cerr)
	config.EntityWatchBuckets = map[string][]string{
		gtypes.BucketEntityStates: {"c360.readiness.scope.*.*.*"},
	}

	processor, err := rule.NewProcessorWithMetrics(tc.Client, &config, nil)
	require.NoError(t, err)
	require.NoError(t, processor.Initialize())
	require.NoError(t, processor.Start(ctx))
	t.Cleanup(func() { _ = processor.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		status, ok := readRuleEnvelope(ctx, t, tc)
		return ok && status.BootstrapComplete
	}, 30*time.Second, 200*time.Millisecond, "replay must finish")

	status, _ := readRuleEnvelope(ctx, t, tc)
	require.EqualValues(t, seeded, status.BootstrapScope,
		"scope must count the values replayed before the sentinel")
}

func readRuleEnvelope(
	ctx context.Context, t *testing.T, tc *natsclient.TestClient,
) (gtypes.IndexStatusResponse, bool) {
	t.Helper()
	bucket, err := tc.GetKVBucket(ctx, readiness.BucketGraphStatus)
	if err != nil {
		return gtypes.IndexStatusResponse{}, false
	}
	entry, err := bucket.Get(ctx, readiness.KeyRule)
	if err != nil {
		return gtypes.IndexStatusResponse{}, false
	}
	var status gtypes.IndexStatusResponse
	if err := json.Unmarshal(entry.Value(), &status); err != nil {
		return gtypes.IndexStatusResponse{}, false
	}
	return status, true
}
