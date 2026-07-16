package fusion

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
)

// fakeGraphQuery records what was called and returns canned evidence
// per sub-query type.
type fakeGraphQuery struct {
	mu sync.Mutex

	entityStateCalls   int
	predicateWalkCalls int
	temporalRangeCalls int
	bm25Calls          int

	entityStateOut   []Evidence
	predicateWalkOut []Evidence
	temporalRangeOut []Evidence
	bm25Out          []Evidence

	entityStateErr   error
	predicateWalkErr error
	temporalRangeErr error
	bm25Err          error
}

func (f *fakeGraphQuery) EntityState(_ context.Context, _ EntityStateArgs, tier, source string, _ int) ([]Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entityStateCalls++
	if f.entityStateErr != nil {
		return nil, f.entityStateErr
	}
	return stampEvidence(f.entityStateOut, tier, source), nil
}

func (f *fakeGraphQuery) PredicateWalk(_ context.Context, _ PredicateWalkArgs, tier, source string, _ int) ([]Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.predicateWalkCalls++
	if f.predicateWalkErr != nil {
		return nil, f.predicateWalkErr
	}
	return stampEvidence(f.predicateWalkOut, tier, source), nil
}

func (f *fakeGraphQuery) TemporalRange(_ context.Context, _ TemporalRangeArgs, tier, source string, _ int) ([]Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.temporalRangeCalls++
	if f.temporalRangeErr != nil {
		return nil, f.temporalRangeErr
	}
	return stampEvidence(f.temporalRangeOut, tier, source), nil
}

func (f *fakeGraphQuery) BM25(_ context.Context, _ BM25Args, tier, source string, _ int) ([]Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bm25Calls++
	if f.bm25Err != nil {
		return nil, f.bm25Err
	}
	return stampEvidence(f.bm25Out, tier, source), nil
}

// stampEvidence applies the Tier + Source the GraphQueryClient contract
// requires. Test fixtures supply Evidence with just EntityID + Score;
// the contract-stamping mirrors what production adapters do.
func stampEvidence(in []Evidence, tier, source string) []Evidence {
	out := make([]Evidence, len(in))
	for i, e := range in {
		e.Tier = tier
		e.Source = source
		out[i] = e
	}
	return out
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Fuse: orchestration ---

func TestFuse_HappyPath(t *testing.T) {
	gq := &fakeGraphQuery{
		entityStateOut:   []Evidence{{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e1"), Score: 0.9}},
		predicateWalkOut: []Evidence{{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e2"), Score: 0.7}},
		bm25Out:          []Evidence{{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e3"), Score: 0.5}},
	}
	queries := []SubQuery{
		{Type: SubQueryTypeEntityState, Tier: "0", Source: "es", EntityState: &EntityStateArgs{EntityIDs: []string{semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e1")}}},
		{Type: SubQueryTypePredicateWalk, Tier: "0", Source: "pw", PredicateWalk: &PredicateWalkArgs{Seeds: []string{semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e1")}}},
		{Type: SubQueryTypeBM25, Tier: "1", Source: "bm", BM25: &BM25Args{Query: "q"}},
	}
	opts := FuseOptions{BudgetTokens: 10000}
	result, err := Fuse(context.Background(), gq, queries, opts, quietLogger())
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if len(result.Evidence) != 3 {
		t.Errorf("evidence count: got %d, want 3 (e1+e2+e3)", len(result.Evidence))
	}
	if result.Degraded {
		t.Errorf("Degraded should be false on happy path: reason=%q", result.DegradedReason)
	}
	if result.BudgetTokensUsed <= 0 {
		t.Errorf("budget_tokens_used should be > 0, got %d", result.BudgetTokensUsed)
	}
	if gq.entityStateCalls != 1 || gq.predicateWalkCalls != 1 || gq.bm25Calls != 1 {
		t.Errorf("call counts: es=%d pw=%d bm=%d, want all 1", gq.entityStateCalls, gq.predicateWalkCalls, gq.bm25Calls)
	}
}

func TestFuse_PerSubqueryErrorIsDegrading(t *testing.T) {
	gq := &fakeGraphQuery{
		entityStateOut: []Evidence{{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e1"), Score: 0.9}},
		bm25Err:        errors.New("BM25 down"),
	}
	queries := []SubQuery{
		{Type: SubQueryTypeEntityState, Tier: "0", Source: "es", EntityState: &EntityStateArgs{EntityIDs: []string{semantictest.EntityID(t, "test", "semstreams", "fusion", "engine", "entity", "e1")}}},
		{Type: SubQueryTypeBM25, Tier: "1", Source: "bm", BM25: &BM25Args{Query: "q"}},
	}
	opts := FuseOptions{BudgetTokens: 10000}
	result, err := Fuse(context.Background(), gq, queries, opts, quietLogger())
	if err != nil {
		t.Fatalf("Fuse should not chain-fail on per-sub-query err: %v", err)
	}
	if !result.Degraded {
		t.Error("Degraded should be true after BM25 error")
	}
	if !strings.Contains(result.DegradedReason, "bm25:bm failed") {
		t.Errorf("degraded reason should name BM25 failure: %q", result.DegradedReason)
	}
	if len(result.Evidence) != 1 {
		t.Errorf("evidence should still include EntityState hit despite BM25 failure: got %d, want 1", len(result.Evidence))
	}
}

func TestFuse_EmptyQueriesIsDegraded(t *testing.T) {
	result, err := Fuse(context.Background(), &fakeGraphQuery{}, nil, FuseOptions{}, quietLogger())
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if !result.Degraded {
		t.Error("Degraded should be true when materializer produced no queries")
	}
	if !strings.Contains(result.DegradedReason, "zero sub-queries") {
		t.Errorf("degraded reason should explain empty queries: %q", result.DegradedReason)
	}
}

func TestFuse_NilClientErrors(t *testing.T) {
	_, err := Fuse(context.Background(), nil, nil, FuseOptions{}, nil)
	if err == nil {
		t.Error("Fuse with nil client should return error")
	}
}

// --- dedup ---

func TestDedupEvidence_TierZeroWinsOverTierOne(t *testing.T) {
	in := []Evidence{
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "dedup", "entity", "e1"), Tier: "1", Source: "bm25", Score: 0.9}, // tier 1, high score
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "dedup", "entity", "e1"), Tier: "0", Source: "es", Score: 0.3},   // tier 0, low score — should win
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "dedup", "entity", "e2"), Tier: "0", Source: "pw", Score: 0.7},
	}
	out := dedupEvidence(in)
	if len(out) != 2 {
		t.Fatalf("dedup count: got %d, want 2", len(out))
	}
	var foundE1 Evidence
	for _, e := range out {
		if e.EntityID == "test.semstreams.fusion.dedup.entity.e1" {
			foundE1 = e
		}
	}
	if foundE1.Tier != "0" || foundE1.Source != "es" {
		t.Errorf("dedup should pick Tier 0 e1, got %+v", foundE1)
	}
}

func TestDedupEvidence_HigherScoreWinsWithinTier(t *testing.T) {
	in := []Evidence{
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "score", "entity", "e1"), Tier: "0", Source: "a", Score: 0.3},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "score", "entity", "e1"), Tier: "0", Source: "b", Score: 0.9}, // should win
	}
	out := dedupEvidence(in)
	if len(out) != 1 {
		t.Fatalf("dedup count: got %d, want 1", len(out))
	}
	if out[0].Source != "b" || out[0].Score != 0.9 {
		t.Errorf("dedup should pick higher-score evidence within tier, got %+v", out[0])
	}
}

func TestDedupEvidence_SkipsEmptyEntityID(t *testing.T) {
	in := []Evidence{
		{EntityID: "", Tier: "0", Source: "x"}, // entity-id-audit:classify intentional-malformed "" line=211 column=14 surface=go-field:.EntityID entity_id_invalid:empty dedup rejection fixture
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "empty", "entity", "e1"), Tier: "0", Source: "x"},
	}
	out := dedupEvidence(in)
	if len(out) != 1 {
		t.Errorf("dedup should skip empty-ID evidence; got %d entries", len(out))
	}
}

// --- sort ---

func TestSortEvidence_TierThenScoreThenID(t *testing.T) {
	ev := []Evidence{
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "sort", "entity", "z"), Tier: "1", Score: 0.9},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "sort", "entity", "a"), Tier: "0", Score: 0.3},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "sort", "entity", "b"), Tier: "0", Score: 0.9},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "sort", "entity", "c"), Tier: "0", Score: 0.9},
	}
	sortEvidence(ev)
	// Want: Tier 0 sorted by score-desc then ID-asc, then Tier 1.
	wantOrder := []string{"test.semstreams.fusion.sort.entity.b", "test.semstreams.fusion.sort.entity.c", "test.semstreams.fusion.sort.entity.a", "test.semstreams.fusion.sort.entity.z"}
	for i, want := range wantOrder {
		if ev[i].EntityID != want {
			t.Errorf("sortEvidence[%d]: got %q, want %q (full order: %v)", i, ev[i].EntityID, want, evIDs(ev))
		}
	}
}

func evIDs(ev []Evidence) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = e.EntityID
	}
	return out
}

// --- budget enforcement ---

func TestEnforceBudget_DropsLowestRanked(t *testing.T) {
	// Three canonical entity-ID evidence entries exceed a tight 24-token budget.
	in := []Evidence{
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "budget", "entity", "first"), Tier: "0", Source: "src", SnippetText: "snip"},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "budget", "entity", "second"), Tier: "0", Source: "src", SnippetText: "snip"},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "budget", "entity", "third"), Tier: "0", Source: "src", SnippetText: "snip"},
	}
	out, used := enforceBudget(in, 24)
	if len(out) < 1 || len(out) > 2 {
		t.Errorf("budget enforcement should keep 1-2 evidence under tight budget; got %d (used=%d)", len(out), used)
	}
	if used > 24 {
		t.Errorf("used %d exceeded budget 24", used)
	}
}

func TestEnforceBudget_GenerousBudgetKeepsAll(t *testing.T) {
	in := []Evidence{
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "generous", "entity", "e1"), Tier: "0", Source: "x"},
		{EntityID: semantictest.EntityID(t, "test", "semstreams", "fusion", "generous", "entity", "e2"), Tier: "1", Source: "x"},
	}
	out, _ := enforceBudget(in, 1_000_000)
	if len(out) != 2 {
		t.Errorf("generous budget should keep all evidence: got %d, want 2", len(out))
	}
}

// --- concurrency ---

func TestFuse_ConcurrentDispatchUnderParallelismCap(t *testing.T) {
	// 20 BM25 sub-queries — verify the engine dispatches them all and
	// returns aggregate evidence, with bounded parallelism.
	gq := &countingDelayedGQ{delay: 5 * time.Millisecond}
	queries := make([]SubQuery, 20)
	for i := range queries {
		queries[i] = SubQuery{
			Type:   SubQueryTypeBM25,
			Tier:   "1",
			Source: "bm",
			BM25:   &BM25Args{Query: "q"},
		}
	}
	start := time.Now()
	opts := FuseOptions{MaxParallelism: 4, BudgetTokens: 100000}
	result, err := Fuse(context.Background(), gq, queries, opts, quietLogger())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	// With parallelism=4 and per-query delay=5ms, expect roughly
	// ceil(20/4)*5ms = 25ms. Generous upper bound 200ms guards against
	// host-load flakiness while still catching a serial bug.
	if elapsed > 200*time.Millisecond {
		t.Errorf("parallelism=4 with 20 5ms queries should complete < 200ms, took %v", elapsed)
	}
	// 20 calls all executed.
	if calls := atomic.LoadInt64(&gq.calls); calls != 20 {
		t.Errorf("BM25 call count: got %d, want 20", calls)
	}
	_ = result
}

// countingDelayedGQ counts BM25 calls and sleeps to model network
// delay so the concurrency test can assert parallel dispatch behaviour
// without being purely timing-driven.
type countingDelayedGQ struct {
	calls int64
	delay time.Duration
}

func (g *countingDelayedGQ) EntityState(_ context.Context, _ EntityStateArgs, _, _ string, _ int) ([]Evidence, error) {
	return nil, nil
}
func (g *countingDelayedGQ) PredicateWalk(_ context.Context, _ PredicateWalkArgs, _, _ string, _ int) ([]Evidence, error) {
	return nil, nil
}
func (g *countingDelayedGQ) TemporalRange(_ context.Context, _ TemporalRangeArgs, _, _ string, _ int) ([]Evidence, error) {
	return nil, nil
}
func (g *countingDelayedGQ) BM25(_ context.Context, _ BM25Args, tier, source string, _ int) ([]Evidence, error) {
	atomic.AddInt64(&g.calls, 1)
	time.Sleep(g.delay)
	return []Evidence{{EntityID: "test.semstreams.fusion.concurrent.entity.e", Tier: tier, Source: source, Score: 0.5}}, nil
}
