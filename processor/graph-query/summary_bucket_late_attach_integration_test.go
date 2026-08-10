//go:build integration

package graphquery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// Finding 1 (HIGH): graph-query must independently watch/retry the COMMUNITY_SUMMARIES
// bucket. On a rolling upgrade COMMUNITY_INDEX already exists (old version) so GraphRAG
// starts, but COMMUNITY_SUMMARIES is created LATER by the enhancement worker. The old
// single-attempt-at-GraphRAG-start behavior missed the late bucket and never
// re-attached, stranding the component on the statistical floor until restart.
//
// This test drives the REAL component: COMMUNITY_INDEX present at Start,
// COMMUNITY_SUMMARIES absent. It then creates COMMUNITY_SUMMARIES and writes an
// enhanced record, and asserts the cache attaches WatchSummaries and SummaryFor starts
// surfacing the summary WITHOUT a restart.
func TestIntegration_GraphQuery_SummaryBucketCreatedLate_Attaches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// COMMUNITY_INDEX present at start; COMMUNITY_SUMMARIES intentionally ABSENT.
	communityKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: gtypes.BucketCommunityIndex})
	require.NoError(t, err)

	comm := &clustering.Community{
		ID:                 entDrone1,
		Level:              0,
		Members:            []string{entDrone1, entDrone2},
		StatisticalSummary: "statistical floor",
		Keywords:           []string{"drone"},
	}
	require.NoError(t, clustering.NewNATSCommunityStorage(communityKV).SaveCommunity(ctx, comm))

	// Build the real component with a fast recheck so the late-bucket attach is quick.
	cfg := DefaultConfig()
	cfg.RecheckInterval = 200 * time.Millisecond
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphQuery(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	gq, ok := comp.(*Component)
	require.True(t, ok)
	require.NoError(t, gq.Initialize())
	require.NoError(t, gq.Start(ctx))
	defer func() { _ = gq.Stop(5 * time.Second) }()

	// Sanity: with COMMUNITY_SUMMARIES still absent, SummaryFor misses (statistical floor).
	_, ok = gq.communityCache.summaryFor(comm)
	require.False(t, ok, "with no summary bucket yet, SummaryFor must miss")

	// LATE: the enhancement worker creates COMMUNITY_SUMMARIES and writes the record.
	summaryKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: gtypes.BucketCommunitySummaries})
	require.NoError(t, err)

	const wantSummary = "a coordinated drone survey"
	hash := clustering.MembershipHash(comm.Members)
	require.NoError(t, clustering.NewNATSSummaryStore(summaryKV).PutSummary(ctx, &clustering.CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          comm.Level,
		LLMSummary:     wantSummary,
		Status:         clustering.SummaryStatusEnhanced,
		MemberCount:    len(comm.Members),
		GeneratedAt:    time.Now(),
	}))

	// The independent summary resource watcher must detect the late bucket, attach
	// WatchSummaries, and surface the summary — all without a component restart.
	require.Eventually(t, func() bool {
		got, ok := gq.communityCache.summaryFor(comm)
		return ok && got == wantSummary
	}, 15*time.Second, 200*time.Millisecond,
		"COMMUNITY_SUMMARIES created after Start must attach without a restart and surface via SummaryFor")

	// And resolveCommunitySummary must now prefer the LLM summary over the statistical floor.
	require.Equal(t, wantSummary, gq.resolveCommunitySummary(comm))
}
