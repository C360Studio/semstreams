package clustering

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeSummaryStore is an in-memory SummaryStore for worker unit tests. It also
// records whether Put was ever called so a test can assert "no write happened".
type fakeSummaryStore struct {
	mu      sync.Mutex
	records map[string]*CommunitySummaryRecord
	getErr  error
	puts    int
}

func newFakeSummaryStore() *fakeSummaryStore {
	return &fakeSummaryStore{records: make(map[string]*CommunitySummaryRecord)}
}

func (f *fakeSummaryStore) GetSummary(_ context.Context, level int, hash string) (*CommunitySummaryRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	rec, ok := f.records[SummaryKey(level, hash)]
	if !ok {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

func (f *fakeSummaryStore) PutSummary(_ context.Context, rec *CommunitySummaryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	cp := *rec
	f.records[SummaryKey(rec.Level, rec.MembershipHash)] = &cp
	return nil
}

// PutFailedUnlessEnhanced mirrors the NATS store's contract: it never overwrites an
// existing llm-enhanced record with a failure. (The fake is single-process, so it
// enforces the guarantee with the same-mutex read+write; the CAS race-safety over
// real NATS is proved by the store integration test.)
func (f *fakeSummaryStore) PutFailedUnlessEnhanced(_ context.Context, rec *CommunitySummaryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.records[SummaryKey(rec.Level, rec.MembershipHash)]; ok && existing.Status == SummaryStatusEnhanced {
		return nil // a success already won — never downgrade it to llm-failed
	}
	f.puts++
	cp := *rec
	f.records[SummaryKey(rec.Level, rec.MembershipHash)] = &cp
	return nil
}

func (f *fakeSummaryStore) CountSummaries(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records), nil
}

// countingLLMClient records how many times the LLM was actually invoked.
type countingLLMClient struct {
	mu    sync.Mutex
	calls int
}

func (c *countingLLMClient) ChatCompletion(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return &llm.ChatResponse{Content: "an autonomous drone fleet summary", Model: "test-model", FinishReason: "stop"}, nil
}
func (c *countingLLMClient) Model() string { return "test-model" }
func (c *countingLLMClient) Close() error  { return nil }
func (c *countingLLMClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// stubQuerier returns one entity per requested ID so SummarizeCommunity has a
// non-empty entity list.
type stubQuerier struct{}

func (stubQuerier) GetEntities(_ context.Context, ids []string) ([]*gtypes.EntityState, error) {
	out := make([]*gtypes.EntityState, 0, len(ids))
	for _, id := range ids {
		out = append(out, &gtypes.EntityState{ID: id})
	}
	return out, nil
}

func newTestWorker(t *testing.T, store SummaryStore, client llm.Client) *EnhancementWorker {
	t.Helper()
	summarizer, err := NewLLMSummarizer(LLMSummarizerConfig{Client: client, MaxTokens: 64})
	require.NoError(t, err)
	return &EnhancementWorker{
		llm:                summarizer,
		querier:            stubQuerier{},
		summaries:          store,
		llmTimeout:         2 * time.Second,
		failedRetryBackoff: time.Hour,
		logger:             discardLogger(),
		ctx:                context.Background(),
	}
}

func themedCommunity() *Community {
	return &Community{
		ID:                 "acme.ops.robotics.gcs.drone.001",
		Level:              0,
		Members:            []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.drone.002"},
		StatisticalSummary: "statistical baseline",
		SummaryStatus:      "statistical",
	}
}

// An unchanged membership with a stored llm-enhanced record is a cache hit: the
// worker serves it and performs NO LLM call.
func TestEnhanceCommunity_SkipOnEnhancedHit_NoLLMCall(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()
	store := newFakeSummaryStore()
	hash := MembershipHash(comm.Members)
	store.records[SummaryKey(comm.Level, hash)] = &CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          comm.Level,
		LLMSummary:     "already enhanced",
		Status:         SummaryStatusEnhanced,
		GeneratedAt:    time.Now(),
	}

	client := &countingLLMClient{}
	w := newTestWorker(t, store, client)

	w.enhanceCommunity(comm)

	assert.Equal(t, 0, client.count(), "an llm-enhanced hit must not invoke the LLM")
	assert.Equal(t, 0, store.puts, "an llm-enhanced hit must not write the store")
}

// A failed record within the backoff window is not retried (no LLM call); after
// the backoff elapses a subsequent trigger does retry.
func TestEnhanceCommunity_FailedRetryBackoff(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()
	store := newFakeSummaryStore()
	hash := MembershipHash(comm.Members)
	store.records[SummaryKey(comm.Level, hash)] = &CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          comm.Level,
		Status:         SummaryStatusFailed,
		GeneratedAt:    time.Now(), // fresh failure
	}

	client := &countingLLMClient{}
	w := newTestWorker(t, store, client)
	w.failedRetryBackoff = time.Hour

	// Within backoff → no retry.
	w.enhanceCommunity(comm)
	assert.Equal(t, 0, client.count(), "a fresh llm-failed record must not be retried within the backoff")

	// Age the failure past the backoff → retry (LLM invoked, record upgraded).
	store.mu.Lock()
	store.records[SummaryKey(comm.Level, hash)].GeneratedAt = time.Now().Add(-2 * time.Hour)
	store.mu.Unlock()

	w.enhanceCommunity(comm)
	assert.Equal(t, 1, client.count(), "an llm-failed record past the backoff must be retried")
	got, _ := store.GetSummary(context.Background(), comm.Level, hash)
	require.NotNil(t, got)
	assert.Equal(t, SummaryStatusEnhanced, got.Status, "a successful retry upgrades the record to llm-enhanced")
}

// A miss triggers exactly one LLM call and writes an llm-enhanced record keyed by
// the membership hash. The worker holds NO CommunityStorage, so it structurally
// cannot write COMMUNITY_INDEX (single-writer invariant).
func TestEnhanceCommunity_MissSummarizesAndWritesSummaryStoreOnly(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()
	store := newFakeSummaryStore()
	client := &countingLLMClient{}
	w := newTestWorker(t, store, client)

	w.enhanceCommunity(comm)

	assert.Equal(t, 1, client.count(), "a miss must invoke the LLM exactly once")
	hash := MembershipHash(comm.Members)
	got, err := store.GetSummary(context.Background(), comm.Level, hash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, SummaryStatusEnhanced, got.Status)
	assert.Equal(t, "an autonomous drone fleet summary", got.LLMSummary)
	assert.Equal(t, hash, got.MembershipHash)
	assert.Equal(t, "test-model", got.Model)
}

// A late failure for a membership that ALREADY has an llm-enhanced record must not
// downgrade it: markFailed routes through PutFailedUnlessEnhanced, so the enhanced
// record wins. This is the worker-level guarantee; the CAS race-safety over real
// NATS is proved by TestIntegration_SummaryStore_FailedNeverReplacesEnhanced.
func TestMarkFailed_NeverDowngradesEnhanced(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()
	store := newFakeSummaryStore()
	hash := MembershipHash(comm.Members)
	store.records[SummaryKey(comm.Level, hash)] = &CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          comm.Level,
		LLMSummary:     "the winning enhanced prose",
		Status:         SummaryStatusEnhanced,
		GeneratedAt:    time.Now(),
	}

	w := newTestWorker(t, store, &countingLLMClient{})

	// A late failure for the same membership must be a no-op on the enhanced record.
	w.markFailed(comm, hash, comm.Level)

	got, err := store.GetSummary(context.Background(), comm.Level, hash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, SummaryStatusEnhanced, got.Status, "a late failure must never downgrade an llm-enhanced record")
	assert.Equal(t, "the winning enhanced prose", got.LLMSummary)
}

// A transient summary-store read error skips the trigger rather than stampeding
// the LLM.
func TestEnhanceCommunity_ReadErrorSkips(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()
	store := newFakeSummaryStore()
	store.getErr = errors.New("kv transient")
	client := &countingLLMClient{}
	w := newTestWorker(t, store, client)

	w.enhanceCommunity(comm)

	assert.Equal(t, 0, client.count(), "a read error must not trigger an LLM call")
	assert.Equal(t, 0, store.puts, "a read error must not write the store")
}
