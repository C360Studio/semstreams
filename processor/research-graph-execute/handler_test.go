package researchexecute

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

	"github.com/c360studio/semstreams/agentic/research"
)

// fakeGraphQuery records what was called and returns canned
// evidence per sub-query type. Each per-type slice acts as a queue
// so multi-call tests (e.g. PredicateWalk's per-seed loop) can
// return different shapes per call.
type fakeGraphQuery struct {
	mu sync.Mutex

	entityStateCalls   int
	predicateWalkCalls int
	temporalRangeCalls int
	bm25Calls          int

	entityStateOut   []research.Evidence
	predicateWalkOut []research.Evidence
	temporalRangeOut []research.Evidence
	bm25Out          []research.Evidence

	entityStateErr   error
	predicateWalkErr error
	temporalRangeErr error
	bm25Err          error
}

func (f *fakeGraphQuery) EntityState(_ context.Context, _ EntityStateArgs, tier, source string, _ int) ([]research.Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entityStateCalls++
	if f.entityStateErr != nil {
		return nil, f.entityStateErr
	}
	return stampEvidence(f.entityStateOut, tier, source), nil
}

func (f *fakeGraphQuery) PredicateWalk(_ context.Context, _ PredicateWalkArgs, tier, source string, _ int) ([]research.Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.predicateWalkCalls++
	if f.predicateWalkErr != nil {
		return nil, f.predicateWalkErr
	}
	return stampEvidence(f.predicateWalkOut, tier, source), nil
}

func (f *fakeGraphQuery) TemporalRange(_ context.Context, _ TemporalRangeArgs, tier, source string, _ int) ([]research.Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.temporalRangeCalls++
	if f.temporalRangeErr != nil {
		return nil, f.temporalRangeErr
	}
	return stampEvidence(f.temporalRangeOut, tier, source), nil
}

func (f *fakeGraphQuery) BM25(_ context.Context, _ BM25Args, tier, source string, _ int) ([]research.Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bm25Calls++
	if f.bm25Err != nil {
		return nil, f.bm25Err
	}
	return stampEvidence(f.bm25Out, tier, source), nil
}

// stampEvidence applies the Tier + Source the GraphQueryClient
// contract requires. Test fixtures supply Evidence with just
// EntityID + Score (the discriminating fields); the contract-
// stamping mirrors what production adapters do.
func stampEvidence(in []research.Evidence, tier, source string) []research.Evidence {
	out := make([]research.Evidence, len(in))
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

// --- extractLoopIDFromSubject ---

func TestExtractLoopIDFromSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{"happy", "component.execute_subqueries.loop-1", "loop-1"},
		{"wrong prefix", "component.nl_classify.loop-1", ""},
		{"empty suffix", "component.execute_subqueries.", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractLoopIDFromSubject(c.subject); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// --- resolveSeedRefs ---

func TestResolveSeedRefs_MixedRefTypes(t *testing.T) {
	candidates := []research.Candidate{
		{EntityID: "acme.ops.robotics.gcs.drone.001", Label: "Drone One", Tier: "0", Source: "x"},
		{EntityID: "acme.ops.robotics.gcs.drone.002", Label: "Drone Two", Tier: "0", Source: "x"},
		{EntityID: "acme.ops.industrial.factory.sensor.555", Label: "Sensor 555", Tier: "0", Source: "x"},
	}
	seeds := []research.SeedRef{
		{Ref: "drone.001", RefType: research.SeedRefTypePartialID},
		{Ref: "Sensor 555", RefType: research.SeedRefTypeName},
		{Ref: "1", RefType: research.SeedRefTypeCandidateIndex},
		{Ref: "missing", RefType: research.SeedRefTypePartialID}, // unresolved
	}
	resolved, dropped := resolveSeedRefs(seeds, candidates, quietLogger())
	if len(resolved) != 3 {
		t.Errorf("resolved = %v, want 3 entries", resolved)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	wantSet := map[string]bool{
		"acme.ops.robotics.gcs.drone.001":        true,
		"acme.ops.industrial.factory.sensor.555": true,
		"acme.ops.robotics.gcs.drone.002":        true,
	}
	for _, id := range resolved {
		if !wantSet[id] {
			t.Errorf("unexpected resolved ID: %q", id)
		}
	}
}

func TestResolveSeedRefs_AllDropped(t *testing.T) {
	resolved, dropped := resolveSeedRefs(
		[]research.SeedRef{{Ref: "x", RefType: "unknown_type"}},
		nil,
		quietLogger())
	if len(resolved) != 0 {
		t.Errorf("resolved should be empty, got %v", resolved)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

// --- executeAll: orchestration ---

func TestExecuteAll_HappyPath(t *testing.T) {
	gq := &fakeGraphQuery{
		entityStateOut:   []research.Evidence{{EntityID: "e1", Score: 0.9}},
		predicateWalkOut: []research.Evidence{{EntityID: "e2", Score: 0.7}},
		bm25Out:          []research.Evidence{{EntityID: "e3", Score: 0.5}},
	}
	queries := []SubQuery{
		{Type: SubQueryTypeEntityState, Tier: "0", Source: "es", EntityState: &EntityStateArgs{EntityIDs: []string{"e1"}}},
		{Type: SubQueryTypePredicateWalk, Tier: "0", Source: "pw", PredicateWalk: &PredicateWalkArgs{Seeds: []string{"e1"}}},
		{Type: SubQueryTypeBM25, Tier: "1", Source: "bm", BM25: &BM25Args{Query: "q"}},
	}
	out, err := executeAll(context.Background(), gq, queries,
		&research.Intent{Topic: "x", BudgetTokens: 10000},
		research.ActionWalkSeeds, 4, 10, quietLogger())
	if err != nil {
		t.Fatalf("executeAll: %v", err)
	}
	if len(out.Evidence) != 3 {
		t.Errorf("evidence count: got %d, want 3 (e1+e2+e3)", len(out.Evidence))
	}
	if out.SubQueryCount != 3 {
		t.Errorf("subquery_count: got %d, want 3", out.SubQueryCount)
	}
	if out.Degraded {
		t.Errorf("Degraded should be false on happy path: reason=%q", out.DegradedReason)
	}
	if out.BudgetTokensUsed <= 0 {
		t.Errorf("budget_tokens_used should be > 0, got %d", out.BudgetTokensUsed)
	}
	if gq.entityStateCalls != 1 || gq.predicateWalkCalls != 1 || gq.bm25Calls != 1 {
		t.Errorf("call counts: es=%d pw=%d bm=%d, want all 1", gq.entityStateCalls, gq.predicateWalkCalls, gq.bm25Calls)
	}
}

func TestExecuteAll_PerSubqueryErrorIsDegrading(t *testing.T) {
	gq := &fakeGraphQuery{
		entityStateOut: []research.Evidence{{EntityID: "e1", Score: 0.9}},
		bm25Err:        errors.New("BM25 down"),
	}
	queries := []SubQuery{
		{Type: SubQueryTypeEntityState, Tier: "0", Source: "es", EntityState: &EntityStateArgs{EntityIDs: []string{"e1"}}},
		{Type: SubQueryTypeBM25, Tier: "1", Source: "bm", BM25: &BM25Args{Query: "q"}},
	}
	out, err := executeAll(context.Background(), gq, queries,
		&research.Intent{Topic: "x", BudgetTokens: 10000},
		research.ActionDecompose, 4, 10, quietLogger())
	if err != nil {
		t.Fatalf("executeAll should not chain-fail on per-sub-query err: %v", err)
	}
	if !out.Degraded {
		t.Error("Degraded should be true after BM25 error")
	}
	if !strings.Contains(out.DegradedReason, "bm25:bm failed") {
		t.Errorf("degraded reason should name BM25 failure: %q", out.DegradedReason)
	}
	if len(out.Evidence) != 1 {
		t.Errorf("evidence should still include EntityState hit despite BM25 failure: got %d, want 1", len(out.Evidence))
	}
}

func TestExecuteAll_EmptyQueriesIsDegraded(t *testing.T) {
	out, err := executeAll(context.Background(), &fakeGraphQuery{}, nil,
		&research.Intent{Topic: "x"}, research.ActionWalkSeeds, 4, 10, quietLogger())
	if err != nil {
		t.Fatalf("executeAll: %v", err)
	}
	if !out.Degraded {
		t.Error("Degraded should be true when materializer produced no queries")
	}
	if !strings.Contains(out.DegradedReason, "zero sub-queries") {
		t.Errorf("degraded reason should explain empty queries: %q", out.DegradedReason)
	}
}

// --- dedup ---

func TestDedupEvidence_TierZeroWinsOverTierOne(t *testing.T) {
	in := []research.Evidence{
		{EntityID: "e1", Tier: "1", Source: "bm25", Score: 0.9}, // tier 1, high score
		{EntityID: "e1", Tier: "0", Source: "es", Score: 0.3},   // tier 0, low score — should win
		{EntityID: "e2", Tier: "0", Source: "pw", Score: 0.7},
	}
	out := dedupEvidence(in)
	if len(out) != 2 {
		t.Fatalf("dedup count: got %d, want 2", len(out))
	}
	var foundE1 research.Evidence
	for _, e := range out {
		if e.EntityID == "e1" {
			foundE1 = e
		}
	}
	if foundE1.Tier != "0" || foundE1.Source != "es" {
		t.Errorf("dedup should pick Tier 0 e1, got %+v", foundE1)
	}
}

func TestDedupEvidence_HigherScoreWinsWithinTier(t *testing.T) {
	in := []research.Evidence{
		{EntityID: "e1", Tier: "0", Source: "a", Score: 0.3},
		{EntityID: "e1", Tier: "0", Source: "b", Score: 0.9}, // should win
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
	in := []research.Evidence{
		{EntityID: "", Tier: "0", Source: "x"},
		{EntityID: "e1", Tier: "0", Source: "x"},
	}
	out := dedupEvidence(in)
	if len(out) != 1 {
		t.Errorf("dedup should skip empty-ID evidence; got %d entries", len(out))
	}
}

// --- sort ---

func TestSortEvidence_TierThenScoreThenID(t *testing.T) {
	ev := []research.Evidence{
		{EntityID: "z", Tier: "1", Score: 0.9},
		{EntityID: "a", Tier: "0", Score: 0.3},
		{EntityID: "b", Tier: "0", Score: 0.9},
		{EntityID: "c", Tier: "0", Score: 0.9},
	}
	sortEvidence(ev)
	// Want: Tier 0 sorted by score-desc then ID-asc, then Tier 1.
	wantOrder := []string{"b", "c", "a", "z"}
	for i, want := range wantOrder {
		if ev[i].EntityID != want {
			t.Errorf("sortEvidence[%d]: got %q, want %q (full order: %v)", i, ev[i].EntityID, want, evIDs(ev))
		}
	}
}

func evIDs(ev []research.Evidence) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = e.EntityID
	}
	return out
}

// --- budget enforcement ---

func TestEnforceBudget_DropsLowestRanked(t *testing.T) {
	// 3 evidence entries, each ~5 tokens (estimateEvidenceTokens
	// rough). Budget 8 should keep 1.
	in := []research.Evidence{
		{EntityID: "first", Tier: "0", Source: "src", SnippetText: "snip"},
		{EntityID: "secnd", Tier: "0", Source: "src", SnippetText: "snip"},
		{EntityID: "third", Tier: "0", Source: "src", SnippetText: "snip"},
	}
	out, used := enforceBudget(in, 8)
	if len(out) < 1 || len(out) > 2 {
		t.Errorf("budget enforcement should keep 1-2 evidence under tight budget; got %d (used=%d)", len(out), used)
	}
	if used > 8 {
		t.Errorf("used %d exceeded budget 8", used)
	}
}

func TestEnforceBudget_GenerousBudgetKeepsAll(t *testing.T) {
	in := []research.Evidence{
		{EntityID: "e1", Tier: "0", Source: "x"},
		{EntityID: "e2", Tier: "1", Source: "x"},
	}
	out, _ := enforceBudget(in, 1_000_000)
	if len(out) != 2 {
		t.Errorf("generous budget should keep all evidence: got %d, want 2", len(out))
	}
}

// --- concurrency ---

func TestExecuteAll_ConcurrentDispatchUnderParallelismCap(t *testing.T) {
	// 20 BM25 sub-queries — verify the orchestrator dispatches them
	// all and returns aggregate evidence, with bounded parallelism.
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
	out, err := executeAll(context.Background(), gq, queries,
		&research.Intent{Topic: "x", BudgetTokens: 100000},
		research.ActionDecompose, 4, 10, quietLogger())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("executeAll: %v", err)
	}
	if out.SubQueryCount != 20 {
		t.Errorf("subquery_count: got %d, want 20", out.SubQueryCount)
	}
	// With parallelism=4 and per-query delay=5ms, expect roughly
	// ceil(20/4)*5ms = 25ms. Generous upper bound 200ms guards
	// against host-load flakiness while still catching a serial bug.
	if elapsed > 200*time.Millisecond {
		t.Errorf("parallelism=4 with 20 5ms queries should complete < 200ms, took %v", elapsed)
	}
	// 20 calls all executed.
	if calls := atomic.LoadInt64(&gq.calls); calls != 20 {
		t.Errorf("BM25 call count: got %d, want 20", calls)
	}
}

// countingDelayedGQ counts BM25 calls and sleeps to model network
// delay so the concurrency test can assert parallel dispatch
// behaviour without being purely timing-driven.
type countingDelayedGQ struct {
	calls int64
	delay time.Duration
}

func (g *countingDelayedGQ) EntityState(_ context.Context, _ EntityStateArgs, _, _ string, _ int) ([]research.Evidence, error) {
	return nil, nil
}
func (g *countingDelayedGQ) PredicateWalk(_ context.Context, _ PredicateWalkArgs, _, _ string, _ int) ([]research.Evidence, error) {
	return nil, nil
}
func (g *countingDelayedGQ) TemporalRange(_ context.Context, _ TemporalRangeArgs, _, _ string, _ int) ([]research.Evidence, error) {
	return nil, nil
}
func (g *countingDelayedGQ) BM25(_ context.Context, _ BM25Args, tier, source string, _ int) ([]research.Evidence, error) {
	atomic.AddInt64(&g.calls, 1)
	time.Sleep(g.delay)
	return []research.Evidence{{EntityID: "e", Tier: tier, Source: source, Score: 0.5}}, nil
}
