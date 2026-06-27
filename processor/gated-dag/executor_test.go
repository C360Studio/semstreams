package gateddagexec

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/gateddag"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fakes ---

type fakeReader struct {
	mu      sync.Mutex
	calls   int
	scripts [][]graph.EntityState // returned in order; last reused after exhausted
	err     error
	// concurrency probe (single-flight test)
	active     int32
	maxActive  int32
	blockEnter chan struct{} // if non-nil, ReadUnitSet blocks on it after entering
}

func (f *fakeReader) ReadUnitSet(_ context.Context) ([]graph.EntityState, error) {
	n := atomic.AddInt32(&f.active, 1)
	for {
		old := atomic.LoadInt32(&f.maxActive)
		if n <= old || atomic.CompareAndSwapInt32(&f.maxActive, old, n) {
			break
		}
	}
	defer atomic.AddInt32(&f.active, -1)
	if f.blockEnter != nil {
		<-f.blockEnter
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.scripts) == 0 {
		return nil, nil
	}
	idx := f.calls - 1
	if idx >= len(f.scripts) {
		idx = len(f.scripts) - 1
	}
	return f.scripts[idx], nil
}

type fakeClaimer struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeClaimer) Claim(_ context.Context, unitID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, unitID)
	return nil
}

type fakePublisher struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakePublisher) Dispatch(_ context.Context, unitID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, unitID)
	return nil
}

// recordingSubmit captures submitted unit IDs.
type recordingSubmit struct {
	mu   sync.Mutex
	jobs []string
}

func (r *recordingSubmit) fn(j dispatchJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, j.unitID)
	return nil
}

func (r *recordingSubmit) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.jobs))
	copy(out, r.jobs)
	return out
}

// newEvalExecutor builds an executor wired for reEvaluate-only tests (no live
// dispatcher; submit is the recorder).
func newEvalExecutor(cfg Config, reader graphReader, sub func(dispatchJob) error) *executor {
	return &executor{cfg: cfg, log: discardLogger(), reader: reader, submit: sub}
}

// fakeStall records edge-triggered stall publishes (#365.3).
type fakeStall struct {
	mu    sync.Mutex
	calls [][]string
}

func (f *fakeStall) PublishStall(_ context.Context, units []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(units))
	copy(cp, units)
	f.calls = append(f.calls, cp)
	return nil
}

func (f *fakeStall) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// --- claimThenDispatch ordering (invariant #2) ---

func TestClaimThenDispatch_ClaimBeforePublish(t *testing.T) {
	cl, pub := &fakeClaimer{}, &fakePublisher{}
	e := &executor{cfg: validCfg(), log: discardLogger(), claimer: cl, pub: pub}
	require.NoError(t, e.claimThenDispatch(context.Background(), "u1"))
	require.Equal(t, []string{"u1"}, cl.calls)
	require.Equal(t, []string{"u1"}, pub.calls)
}

func TestClaimThenDispatch_ClaimErrorSkipsPublish(t *testing.T) {
	cl := &fakeClaimer{err: errors.New("boom")}
	pub := &fakePublisher{}
	e := &executor{cfg: validCfg(), log: discardLogger(), claimer: cl, pub: pub}
	require.Error(t, e.claimThenDispatch(context.Background(), "u1"))
	require.Empty(t, pub.calls, "publish must not run when claim fails")
}

func TestClaimThenDispatch_PublishErrorAfterClaimDoesNotRollBack(t *testing.T) {
	cl := &fakeClaimer{}
	pub := &fakePublisher{err: errors.New("net down")}
	e := &executor{cfg: validCfg(), log: discardLogger(), claimer: cl, pub: pub}
	require.Error(t, e.claimThenDispatch(context.Background(), "u1"))
	require.Equal(t, []string{"u1"}, cl.calls, "claim stays committed (no rollback — avoids double-run window)")
}

func TestClaimThenDispatch_ClaimErrorClearsInflight(t *testing.T) {
	// The in-flight hint is set on submit; a claim failure in the worker must
	// clear it, or the unit is skipped on every subsequent pass (never terminal,
	// never dirtied) and wedges its whole subtree silently.
	cl := &fakeClaimer{err: errors.New("claim boom")}
	pub := &fakePublisher{}
	e := &executor{cfg: validCfg(), log: discardLogger(), claimer: cl, pub: pub, inflight: map[string]bool{"u1": true}}
	require.Error(t, e.claimThenDispatch(context.Background(), "u1"))
	require.False(t, e.inflight["u1"], "claim failure must clear the in-flight hint so the unit is re-selected")
	require.Empty(t, pub.calls, "no publish when the claim fails")
}

// --- reEvaluate selection ---

// presence builds a presence-marker triple (object value is irrelevant for
// completed/failed/dirtied/claim — extractGraph keys on the predicate).
func presence(pred string) message.Triple { return tri(pred, "x") }

func TestReEvaluate_DiamondSelection(t *testing.T) {
	cfg := validCfg()
	// a -> {b,c} -> d. Only a complete: b,c dispatch; d held.
	states := []graph.EntityState{
		unit("a", presence(cfg.CompletedPredicate)),
		unit("b", tri(cfg.DependsOnPredicate, "a")),
		unit("c", tri(cfg.DependsOnPredicate, "a")),
		unit("d", tri(cfg.DependsOnPredicate, "b"), tri(cfg.DependsOnPredicate, "c")),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.reEvaluate(context.Background(), true)
	require.ElementsMatch(t, []string{"b", "c"}, sub.ids())
}

func TestReEvaluate_InFlightDedup(t *testing.T) {
	cfg := validCfg()
	// b is Ready (a complete) but already claimed → must NOT re-dispatch.
	states := []graph.EntityState{
		unit("a", presence(cfg.CompletedPredicate)),
		unit("b", tri(cfg.DependsOnPredicate, "a"), presence(cfg.ClaimPredicate)),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.reEvaluate(context.Background(), true)
	require.Empty(t, sub.ids())
}

func TestReEvaluate_DirtiedOverridesStaleClaim(t *testing.T) {
	cfg := validCfg()
	// b carries a stale claim AND a dirtied marker (reset that forgot to clear
	// the claim) → V4: dirtied overrides claim, so b re-dispatches.
	states := []graph.EntityState{
		unit("a", presence(cfg.CompletedPredicate)),
		unit("b",
			tri(cfg.DependsOnPredicate, "a"),
			presence(cfg.ClaimPredicate),
			presence(cfg.DirtiedPredicate)),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.reEvaluate(context.Background(), true)
	require.Equal(t, []string{"b"}, sub.ids())
}

func TestReEvaluate_BackstopPicksUpLateDependent(t *testing.T) {
	cfg := validCfg()
	// Pass 1: c is in-flight (claimed, not complete) → deduped; d held (c not
	// done). Pass 2: c completes → d becomes dispatchable. A later pass (the
	// backstop) re-reads authoritative state and releases the dependent.
	pass1 := []graph.EntityState{
		unit("c", presence(cfg.ClaimPredicate)),
		unit("d", tri(cfg.DependsOnPredicate, "c")),
	}
	pass2 := []graph.EntityState{
		unit("c", presence(cfg.CompletedPredicate)),
		unit("d", tri(cfg.DependsOnPredicate, "c")),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{pass1, pass2}}, sub.fn)
	e.reEvaluate(context.Background(), true) // pass 1: c claimed (deduped), d held
	require.Empty(t, sub.ids())
	e.reEvaluate(context.Background(), true) // pass 2 (backstop): d now ready
	require.Equal(t, []string{"d"}, sub.ids())
}

func TestReEvaluate_ReadErrorIsResilient(t *testing.T) {
	cfg := validCfg()
	sub := &recordingSubmit{}
	r := &fakeReader{err: errors.New("store unavailable")}
	e := newEvalExecutor(cfg, r, sub.fn)
	require.NotPanics(t, func() { e.reEvaluate(context.Background(), true) })
	require.Empty(t, sub.ids())
}

func TestReEvaluate_StopOnFirstFailureHoldsIndependentBranch(t *testing.T) {
	cfg := validCfg()
	cfg.FailurePolicy = FailurePolicyStopOnFirstFailure
	// a failed; independent ready unit z. continue_others would dispatch z;
	// stop_on_first_failure holds it.
	states := []graph.EntityState{
		unit("a", presence(cfg.FailedPredicate)),
		unit("z"),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.reEvaluate(context.Background(), true)
	require.Empty(t, sub.ids(), "stop_on_first_failure holds all new dispatch")

	// continue_others (default) dispatches the independent branch.
	cfg.FailurePolicy = FailurePolicyContinueOthers
	sub2 := &recordingSubmit{}
	e2 := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub2.fn)
	e2.reEvaluate(context.Background(), true)
	require.Equal(t, []string{"z"}, sub2.ids())
}

func TestReEvaluate_SingleFlightSerializes(t *testing.T) {
	cfg := validCfg()
	r := &fakeReader{
		scripts:    [][]graph.EntityState{{unit("a")}},
		blockEnter: make(chan struct{}),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, r, sub.fn)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.reEvaluate(context.Background(), true) }()
	}
	// Let both goroutines reach ReadUnitSet; release them.
	time.Sleep(20 * time.Millisecond)
	close(r.blockEnter)
	wg.Wait()
	require.Equal(t, int32(1), r.maxActive, "evalMu must serialize passes (never two concurrent reads)")
}

// --- stall ---

func TestReEvaluate_StallSurfacedNothingDispatched(t *testing.T) {
	cfg := validCfg()
	// Cycle a<->b: nothing dispatchable, both held-ready ⇒ stall.
	states := []graph.EntityState{
		unit("a", tri(cfg.DependsOnPredicate, "b")),
		unit("b", tri(cfg.DependsOnPredicate, "a")),
	}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.reEvaluate(context.Background(), true)
	require.Empty(t, sub.ids(), "a cycle dispatches nothing")
}

func TestStallAfterInflight(t *testing.T) {
	mk := func(completed, failed, dirtied, claimed []string) graphView {
		v := graphView{claimed: map[string]bool{}}
		for _, id := range claimed {
			v.claimed[id] = true
		}
		v.markers = gateddag.NewMarkerSet(completed, failed, dirtied)
		return v
	}

	t.Run("no stalled units → nil", func(t *testing.T) {
		require.Nil(t, stallAfterInflight(nil, mk(nil, nil, nil, nil)))
	})
	t.Run("stalled and nothing in-flight → reported", func(t *testing.T) {
		got := stallAfterInflight([]string{"a", "b"}, mk(nil, nil, nil, nil))
		require.Equal(t, []string{"a", "b"}, got)
	})
	t.Run("stalled but a claimed non-terminal unit is in-flight → suppressed", func(t *testing.T) {
		got := stallAfterInflight([]string{"a"}, mk(nil, nil, nil, []string{"c"}))
		require.Nil(t, got, "running work is not a stall")
	})
	t.Run("claimed unit is terminal → not in-flight, stall reported", func(t *testing.T) {
		got := stallAfterInflight([]string{"a"}, mk([]string{"c"}, nil, nil, []string{"c"}))
		require.Equal(t, []string{"a"}, got)
	})
	t.Run("claimed unit is dirtied → reset, not in-flight, stall reported", func(t *testing.T) {
		got := stallAfterInflight([]string{"a"}, mk(nil, nil, []string{"c"}, []string{"c"}))
		require.Equal(t, []string{"a"}, got)
	})
}

// --- #364 / #365.3 contract-completion additions ---

func TestAllUnitsDone(t *testing.T) {
	done := func(ids ...string) []gateddag.Decision {
		out := make([]gateddag.Decision, len(ids))
		for i, id := range ids {
			out[i] = gateddag.Decision{UnitID: id, Status: gateddag.StatusDone}
		}
		return out
	}
	require.False(t, allUnitsDone(nil), "empty set is NOT complete (vacuous all-done)")
	require.True(t, allUnitsDone(done("a", "b")))

	mixed := append(done("a"), gateddag.Decision{UnitID: "b", Status: gateddag.StatusReady})
	require.False(t, allUnitsDone(mixed), "a non-Done unit means not complete")

	blocked := append(done("a"), gateddag.Decision{UnitID: "b", Status: gateddag.StatusBlocked})
	require.False(t, allUnitsDone(blocked), "a Blocked (failed) unit is not 'done' — consumer-driven failure")
}

func TestReEvaluate_StallEventEdgeTriggered(t *testing.T) {
	cfg := validCfg()
	// Cycle a<->b → persistent stall on every pass.
	states := []graph.EntityState{
		unit("a", tri(cfg.DependsOnPredicate, "b")),
		unit("b", tri(cfg.DependsOnPredicate, "a")),
	}
	sub := &recordingSubmit{}
	fs := &fakeStall{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.stall = fs

	// Three passes, all stalled: the event fires once on the 0→non-zero edge.
	e.reEvaluate(context.Background(), true)
	e.reEvaluate(context.Background(), true)
	e.reEvaluate(context.Background(), true)
	require.Equal(t, 1, fs.count(), "stall event is edge-triggered, not re-emitted every pass")
	require.Empty(t, sub.ids(), "a cycle dispatches nothing")
}

// TestReEvaluate_InMemoryInflightDedup proves the submit→claim-commit window is
// closed: a unit submitted in one pass is NOT re-submitted by a later pass even
// when its durable claim is not yet visible in the read (the latent double-
// dispatch the #363 unit watch exposed).
func TestReEvaluate_InMemoryInflightDedup(t *testing.T) {
	cfg := validCfg()
	states := []graph.EntityState{unit("a")} // a: no deps, Ready, never shows a claim
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{states}}, sub.fn)
	e.reEvaluate(context.Background(), true) // submits a, marks it in-flight
	e.reEvaluate(context.Background(), true) // claim still not visible, but in-flight → NOT re-submitted
	e.reEvaluate(context.Background(), true)
	require.Equal(t, []string{"a"}, sub.ids(), "in-memory in-flight prevents double-dispatch before the claim is visible")
}

// TestReEvaluate_DirtiedClearsInflight proves a reset re-dispatches even if the
// in-memory in-flight hint is stale (dirtied overrides it, same as the claim).
func TestReEvaluate_DirtiedClearsInflight(t *testing.T) {
	cfg := validCfg()
	clean := []graph.EntityState{unit("a")}
	reset := []graph.EntityState{unit("a", presence(cfg.DirtiedPredicate))}
	sub := &recordingSubmit{}
	e := newEvalExecutor(cfg, &fakeReader{scripts: [][]graph.EntityState{clean, reset}}, sub.fn)
	e.reEvaluate(context.Background(), true) // submits a (in-flight)
	e.reEvaluate(context.Background(), true) // a now dirtied → in-flight cleared, re-dispatched
	require.Equal(t, []string{"a", "a"}, sub.ids(), "a dirtied unit re-dispatches despite a stale in-flight hint")
}
