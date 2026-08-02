//go:build integration

package clustering

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Finding 2 (HIGH): an llm-failed write must NEVER replace an existing llm-enhanced
// record, and it must be race-safe against a concurrent success landing inside the
// read→write window (a plain read-then-write would be TOCTOU).
//
// Two orderings are proved deterministically over real NATS:
//
//	A. Late failure — a success is already committed; a subsequent failure for the
//	   same membership must be a no-op (the read sees llm-enhanced and skips).
//	B. TOCTOU — a stale llm-failed record exists (so the failed path takes the CAS
//	   Update branch); a concurrent llm-enhanced success is injected INSIDE the
//	   read→write window via the afterFailedRead hook. Without CAS the failed write
//	   clobbers the success; with CAS the Update conflicts, the retry re-reads the
//	   enhanced record, and skips.
func TestIntegration_SummaryStore_FailedNeverReplacesEnhanced(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "COMMUNITY_SUMMARIES_CAS_TEST",
	})
	require.NoError(t, err)
	purgeKeys := func() {
		keys, _ := kv.Keys(ctx)
		for _, k := range keys {
			_ = kv.Delete(ctx, k)
		}
	}
	purgeKeys()
	defer purgeKeys()

	store := NewNATSSummaryStore(natsClient, kv)

	// Scenario A — late failure after a committed success.
	membersA := []string{"acme.ops.a.gcs.x.001", "acme.ops.a.gcs.x.002"}
	hashA := MembershipHash(membersA)
	require.NoError(t, store.PutSummary(ctx, &CommunitySummaryRecord{
		MembershipHash: hashA, Level: 0, LLMSummary: "committed enhanced prose",
		Status: SummaryStatusEnhanced, MemberCount: len(membersA), GeneratedAt: time.Now(),
	}))
	require.NoError(t, store.PutFailedUnlessEnhanced(ctx, &CommunitySummaryRecord{
		MembershipHash: hashA, Level: 0, Status: SummaryStatusFailed,
		MemberCount: len(membersA), GeneratedAt: time.Now(),
	}))
	gotA, err := store.GetSummary(ctx, 0, hashA)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	assert.Equal(t, SummaryStatusEnhanced, gotA.Status,
		"a late failure must not replace a committed llm-enhanced record")
	assert.Equal(t, "committed enhanced prose", gotA.LLMSummary)

	// Scenario B — a success races into the CAS window of a failed write.
	membersB := []string{"acme.ops.b.gcs.y.001", "acme.ops.b.gcs.y.002"}
	hashB := MembershipHash(membersB)
	// Seed a stale llm-failed record so the failed path takes the CAS Update branch.
	require.NoError(t, store.PutSummary(ctx, &CommunitySummaryRecord{
		MembershipHash: hashB, Level: 0, Status: SummaryStatusFailed,
		MemberCount: len(membersB), GeneratedAt: time.Now().Add(-time.Hour),
	}))

	casStore := NewNATSSummaryStore(natsClient, kv)
	var once sync.Once
	casStore.afterFailedRead = func() {
		once.Do(func() {
			// The concurrent success lands mid-window via a separate handle, same bucket.
			require.NoError(t, store.PutSummary(ctx, &CommunitySummaryRecord{
				MembershipHash: hashB, Level: 0, LLMSummary: "the racing winner",
				Status: SummaryStatusEnhanced, MemberCount: len(membersB), GeneratedAt: time.Now(),
			}))
		})
	}

	require.NoError(t, casStore.PutFailedUnlessEnhanced(ctx, &CommunitySummaryRecord{
		MembershipHash: hashB, Level: 0, Status: SummaryStatusFailed,
		MemberCount: len(membersB), GeneratedAt: time.Now(),
	}))

	gotB, err := store.GetSummary(ctx, 0, hashB)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	assert.Equal(t, SummaryStatusEnhanced, gotB.Status,
		"a failure racing a success inside the CAS window must not downgrade the enhanced record")
	assert.Equal(t, "the racing winner", gotB.LLMSummary)
}

// Finding 3 (MEDIUM): the summaries-size gauge must be initialized from the store's
// count at Start, so a restart onto a populated store where every trigger is a cache
// hit (no writes) reports the real count instead of a stuck 0.
func TestIntegration_EnhancementWorker_Start_InitializesSizeGaugeFromStore(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	communityKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "COMMUNITY_INDEX_GAUGE_TEST",
	})
	require.NoError(t, err)
	summaryKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "COMMUNITY_SUMMARIES_GAUGE_TEST",
	})
	require.NoError(t, err)
	purge := func(kv jetstream.KeyValue) {
		keys, _ := kv.Keys(ctx)
		for _, k := range keys {
			_ = kv.Delete(ctx, k)
		}
	}
	purge(summaryKV)
	purge(communityKV)
	defer func() { purge(summaryKV); purge(communityKV) }()

	// Pre-populate the summary store BEFORE the worker starts (simulating a restart
	// onto a populated store).
	store := NewNATSSummaryStore(natsClient, summaryKV)
	const wantCount = 3
	for i := 0; i < wantCount; i++ {
		members := []string{
			fmt.Sprintf("acme.ops.g.gcs.z.%03d", i),
			fmt.Sprintf("acme.ops.g.gcs.z.%03d", i+100),
		}
		require.NoError(t, store.PutSummary(ctx, &CommunitySummaryRecord{
			MembershipHash: MembershipHash(members), Level: 0, LLMSummary: "pre-existing",
			Status: SummaryStatusEnhanced, MemberCount: len(members), GeneratedAt: time.Now(),
		}))
	}

	registry := metric.NewMetricsRegistry()
	summarizer, err := NewLLMSummarizer(LLMSummarizerConfig{Client: &countingLLMClient{}, MaxTokens: 64})
	require.NoError(t, err)
	worker, err := NewEnhancementWorker(&EnhancementWorkerConfig{
		LLMSummarizer:   summarizer,
		Querier:         stubQuerier{},
		NATSClient:      natsClient,
		CommunityBucket: communityKV,
		SummaryBucket:   summaryKV,
		Registry:        registry,
		Logger:          discardLogger(),
	})
	require.NoError(t, err)

	// Before Start: the gauge has never been set → 0.
	require.Equal(t, float64(0), testutil.ToFloat64(worker.metrics.summariesSize),
		"gauge must be 0 before Start")

	require.NoError(t, worker.Start(ctx))
	defer func() { _ = worker.Stop() }()

	// After Start — no new writes (empty COMMUNITY_INDEX → no triggers) — the gauge
	// must reflect the real store count. Start initializes it synchronously, so this
	// holds immediately.
	assert.Equal(t, float64(wantCount), testutil.ToFloat64(worker.metrics.summariesSize),
		"Start must initialize the size gauge from CountSummaries, not leave it at 0")
}
