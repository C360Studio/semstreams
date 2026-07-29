//go:build integration

package graphquery

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSummaryLLMClient returns a fixed summary for every ChatCompletion so the
// test can assert the EXACT text propagates all the way from the worker's LLM
// call to graph-query's read join. A stub (not a live endpoint) keeps this a
// deterministic wire test — the question it answers is "does the production join
// work", not "is the model good".
type mockSummaryLLMClient struct {
	summary string
}

func (m *mockSummaryLLMClient) ChatCompletion(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: m.summary, Model: "mock-summary-model", FinishReason: "stop"}, nil
}
func (m *mockSummaryLLMClient) Model() string { return "mock-summary-model" }
func (m *mockSummaryLLMClient) Close() error  { return nil }

// oneEntityPerIDQuerier returns a single bare EntityState per requested member so
// LLMSummarizer.SummarizeCommunity clears its only hard precondition (a non-empty
// entity list). Content is irrelevant here — the mock client ignores the prompt.
type oneEntityPerIDQuerier struct{}

func (oneEntityPerIDQuerier) GetEntities(_ context.Context, ids []string) ([]*gtypes.EntityState, error) {
	out := make([]*gtypes.EntityState, 0, len(ids))
	for _, id := range ids {
		out = append(out, &gtypes.EntityState{ID: id})
	}
	return out, nil
}

// TestIntegration_EnhancementWorker_WiresThroughToGraphQuerySummary is the
// DEFINITIVE end-to-end proof of the B3 ownership split (ADR-087) over REAL NATS.
// It is the A-vs-B answer: if it passes, the earlier e2e "enhanced=0" report was a
// measurement gap in the observability (it read the old COMMUNITY_INDEX location),
// not a real worker/join defect.
//
// It drives the PRODUCTION trigger path — a COMMUNITY_INDEX write, seen by the
// worker's real WatchAll — rather than calling enhanceCommunity directly, and it
// drives graph-query's real WatchSummaries loop over NATS rather than calling
// handleSummaryUpdate directly. That closes the reviewer's MEDIUM: the
// WatchSummaries-over-real-NATS seam was previously covered only by the e2e tier.
func TestIntegration_EnhancementWorker_WiresThroughToGraphQuerySummary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// COMMUNITY_INDEX = detector-owned trigger bucket; COMMUNITY_SUMMARIES =
	// worker-owned content-addressed store. Two distinct buckets is the whole point
	// of the split.
	communityKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: gtypes.BucketCommunityIndex})
	require.NoError(t, err)
	summaryKV, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: gtypes.BucketCommunitySummaries})
	require.NoError(t, err)

	const mockSummary = "an autonomous drone fleet coordinating a joint area survey"

	summarizer, err := clustering.NewLLMSummarizer(clustering.LLMSummarizerConfig{
		Client:    &mockSummaryLLMClient{summary: mockSummary},
		MaxTokens: 128,
	})
	require.NoError(t, err)

	worker, err := clustering.NewEnhancementWorker(&clustering.EnhancementWorkerConfig{
		LLMSummarizer:   summarizer,
		Querier:         oneEntityPerIDQuerier{},
		CommunityBucket: communityKV,
		SummaryBucket:   summaryKV,
		Logger:          logger,
	})
	require.NoError(t, err)

	// Start the worker: this establishes the REAL COMMUNITY_INDEX WatchAll that is
	// the production trigger. Nothing is enhanced until a community is written.
	require.NoError(t, worker.Start(ctx))
	defer func() { _ = worker.Stop() }()

	// Seed COMMUNITY_INDEX through the real detector storage — the Put on the
	// community key is what fires the worker's watch.
	comm := &clustering.Community{
		ID:                 entDrone1,
		Level:              0,
		Members:            []string{entDrone1, entDrone2},
		StatisticalSummary: "statistical baseline about two drones",
		Keywords:           []string{"drone"},
	}
	storage := clustering.NewNATSCommunityStorage(communityKV)
	require.NoError(t, storage.SaveCommunity(ctx, comm))

	hash := clustering.MembershipHash(comm.Members)
	wantKey := clustering.SummaryKey(comm.Level, hash)

	// (1) The worker enhances end-to-end: an llm-enhanced record lands in the real
	// COMMUNITY_SUMMARIES bucket at exactly {level}.{membership_hash}.
	store := clustering.NewNATSSummaryStore(summaryKV)
	require.Eventually(t, func() bool {
		rec, gerr := store.GetSummary(ctx, comm.Level, hash)
		return gerr == nil && rec != nil && rec.Status == clustering.SummaryStatusEnhanced
	}, 30*time.Second, 200*time.Millisecond,
		"enhancement worker never wrote an llm-enhanced record to COMMUNITY_SUMMARIES — this would be a REAL worker/trigger defect")

	rec, err := store.GetSummary(ctx, comm.Level, hash)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, clustering.SummaryStatusEnhanced, rec.Status)
	assert.Equal(t, mockSummary, rec.LLMSummary, "stored summary must be the mock LLM's exact output")
	assert.Equal(t, hash, rec.MembershipHash)
	assert.Equal(t, comm.Level, rec.Level)
	assert.Equal(t, "mock-summary-model", rec.Model)

	// The record must live at the {level}.{hash} key — the exact join contract the
	// read path reconstructs. A different key would mean the store and reader can
	// never meet.
	entry, err := summaryKV.Get(ctx, wantKey)
	require.NoError(t, err, "summary must be stored at the {level}.{membership_hash} key")
	assert.NotEmpty(t, entry.Value())

	// (2) graph-query's community cache, driven by its REAL WatchAndSync +
	// WatchSummaries loops over NATS, joins the summary to the community by
	// membership hash and surfaces it.
	cache := NewCommunityCache(logger)
	go func() { _ = cache.WatchAndSync(ctx, communityKV) }()
	go func() { _ = cache.WatchSummaries(ctx, summaryKV) }()

	require.Eventually(t, func() bool {
		got, ok := cache.SummaryFor(comm)
		return ok && got == mockSummary
	}, 30*time.Second, 200*time.Millisecond,
		"graph-query cache never surfaced the worker's summary via WatchSummaries over real NATS — REAL read-join defect")

	got, ok := cache.SummaryFor(comm)
	require.True(t, ok)
	assert.Equal(t, mockSummary, got)

	// resolveCommunitySummary must surface the joined LLM summary, NOT the
	// statistical floor — proving the tiered read path prefers the store hit.
	comp := &Component{communityCache: cache}
	resolved := comp.resolveCommunitySummary(comm)
	assert.Equal(t, mockSummary, resolved, "resolveCommunitySummary must return the LLM summary, not the statistical floor")
	assert.NotEqual(t, comm.StatisticalSummary, resolved)
}
