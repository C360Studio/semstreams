//go:build integration

// Full-stack composed validation for the gated-DAG dispatch executor
// (ADR-046 Phase 2, Stage E). Boots a real NATS testcontainer + graph-ingest +
// the rule engine + a lifecycle Manager + the gated-DAG executor + a demo
// consumer that completes (or fails) units on dispatch, then drives the four
// ADR Stage-E scenarios end-to-end:
//
//  1. depends_on respected under concurrent completion arrival.
//  2. reset survives an evicted terminal row — exercises Stage C (executor
//     re-dispatches the recreated unit) AND Stage D (the rule engine clears the
//     evicted entity's $state) together.
//  3. a failed node holds its dependents while independent branches keep flowing.
//  4. a depends_on cycle surfaces as a stall (logged), dispatching nothing.
//
// External test package: composes graph-ingest + rule + lifecycle + the
// executor's exported API; no internal access needed (the Stage D assertion
// reads the RULE_STATE bucket directly).
package gateddagexec_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	gateddagexec "github.com/c360studio/semstreams/processor/gated-dag"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/processor/rule"
)

// Predicate vocabulary — set explicitly on the executor config so the test and
// the executor agree (the defaults are unexported).
const (
	pCompleted = "gateddag.unit.completed"
	pFailed    = "gateddag.unit.failed"
	pDirtied   = "gateddag.unit.dirtied"
	pDependsOn = "gateddag.unit.depends-on"
	pClaim     = "gateddag.unit.claim"
)

const fsBackstop = "250ms"

// fsEventually bounds the full-stack Eventually waits. These tests drive a real
// multi-component pipeline (NATS → graph-ingest mutation+query → executor →
// demo consumer doing mutation-with-retry), so a single DAG level is several
// serial NATS round-trips; a diamond is three of them. The 250ms fsBackstop
// guarantees forward progress the moment a marker lands, so this window only
// needs to absorb upstream pipeline latency under contention — it does not mask
// a logic stall (a genuinely stuck DAG never advances regardless of wait).
//
// The window is CI-contention-aware (gh#404). Locally the whole pipeline settles
// in ~5s, so 30s is ~6x headroom and surfaces a real stall fast in dev. CI runs
// dozens of testcontainer-booting integration packages under unbounded
// `go test -tags=integration ./...` parallelism (no -p cap), so the shared
// Docker host is CPU/IO-starved and every round-trip in this 6-component stack
// slows down. gh#404 measured this test time out at 30.65s on CI while
// completing in 5.2s locally — confirmed SLOWNESS, not a stall (the prior
// "a re-flake here means a real stall" prediction was thereby falsified). So CI
// gets a generous 90s budget (~18x the local baseline) while local stays tight.
//
// Earlier attempts: #374 widened 10s→30s; #386 made marker writes reliable; the
// 90s bump above absorbed more contention but still flaked (gh#511). The systemic
// lever — cap integration `-p` so fewer NATS containers run at once — is now
// applied: CI and `task test:integration` run with `-p 2` (ci.yml / taskfiles/
// test.yml), roughly halving concurrent testcontainer packages on the shared
// Docker host. The 90s window is kept as a safety margin on top of the cap.
var fsEventually = fsEventuallyWindow()

// fsEventuallyWindow returns the Eventually budget: tight locally for fast stall
// detection, generous on CI to absorb testcontainer-contention slowness (gh#404).
// GitHub Actions (and most CI) sets CI=true.
func fsEventuallyWindow() time.Duration {
	if os.Getenv("CI") != "" {
		return 90 * time.Second
	}
	return 30 * time.Second
}

// dispatchLog is the thread-safe ordered record of units the executor dispatched
// (decoded off the dispatch subject by the demo consumer).
type dispatchLog struct {
	mu  sync.Mutex
	ids []string
}

func (d *dispatchLog) add(id string) {
	d.mu.Lock()
	d.ids = append(d.ids, id)
	d.mu.Unlock()
}

func (d *dispatchLog) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.ids))
	copy(out, d.ids)
	return out
}

func (d *dispatchLog) count(id string) int {
	n := 0
	for _, v := range d.snapshot() {
		if v == id {
			n++
		}
	}
	return n
}

// fullStack is a booted composed stack for one scenario.
type fullStack struct {
	nc         *natsclient.Client
	gi         *graphingest.Component
	mgr        *lifecycle.Manager
	prefix     string
	dispatched *dispatchLog
}

// fsOpts parameterizes setupFullStack.
type fsOpts struct {
	prefix, subject  string
	failUnits        map[string]bool
	logger           *slog.Logger
	backstop         string // default fsBackstop when empty
	fanOutInstanceID string // #364: framework-owned instance lifecycle
	stallSubject     string // #365.3: edge-triggered stall event
}

// captureHandler records slog messages so a scenario can assert a stall was
// surfaced (Stage E requirement #8).
type captureHandler struct {
	mu      sync.Mutex
	records []string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Message)
	h.mu.Unlock()
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }
func (h *captureHandler) sawContaining(sub string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.records {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// setupFullStack boots NATS + graph-ingest + rule engine + lifecycle Manager +
// the executor + the demo consumer for one scenario, all wired through the
// production constructors. failUnits names units the consumer should fail
// (write a failed marker) instead of complete. logger is the executor's logger
// (pass a capturing logger for the stall scenario; nil → discard).
func setupFullStack(t *testing.T, opts fsOpts) *fullStack {
	t.Helper()
	ctx := context.Background()
	prefix, subject, failUnits := opts.prefix, opts.subject, opts.failUnits
	logger := opts.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discard{}, nil))
	}
	backstop := opts.backstop
	if backstop == "" {
		backstop = fsBackstop
	}

	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV(), natsclient.WithStreams(streams...))
	nc := tc.Client

	// graph-ingest: mutation + query handlers, ENTITY_STATES.
	giJSON, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	giDisc, err := graphingest.CreateGraphIngest(giJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	gi := giDisc.(*graphingest.Component)
	require.NoError(t, gi.Initialize())
	require.NoError(t, gi.Start(ctx))
	t.Cleanup(func() { _ = gi.Stop(5 * time.Second) })

	// Rule engine (no rules) — present so its DELETED-branch cleanup runs for the
	// Stage D assertion; harmless no-op for the other scenarios.
	rcfg := rule.DefaultConfig()
	rcfg.EntityWatchBuckets = map[string][]string{gtypes.BucketEntityStates: {"fs.test.*.*.*.*"}}
	rproc, err := rule.NewProcessor(nc, &rcfg)
	require.NoError(t, err)
	require.NoError(t, rproc.Initialize())
	require.NoError(t, rproc.Start(ctx))
	t.Cleanup(func() { _ = rproc.Stop(5 * time.Second) })

	mgr := lifecycle.NewManager(nc, nil)

	dispatched := &dispatchLog{}

	// markerCtx scopes the demo consumer's marker-write goroutines; cancelled at
	// teardown (registered after NewTestClient so it runs BEFORE client.Close in
	// LIFO cleanup) so no retry loop outlives the test or races the closing client.
	markerCtx, cancelMarkers := context.WithCancel(context.Background())
	t.Cleanup(cancelMarkers)

	// Demo consumer: on each dispatch, complete the unit (or fail it). This is the
	// work-completion loop a real consumer (semspec) provides; here it writes the
	// terminal marker so the executor's next backstop advances the DAG. The marker
	// write is synchronous in the callback (dispatch is recorded first, so the
	// dispatch assertions are unaffected). A retrying write head-of-line-blocks
	// sibling units on this single-threaded subscription callback, but bounded at
	// addMarker's 25s — under fsEventually even in the worst case — and a
	// goroutine-per-marker alternative measurably worsened scheduling contention
	// under constrained CPU, so synchronous is the right trade here.
	// These terminal markers are part of the test consumer's fixed graph
	// contract, not caller options. Keeping them here makes every composed
	// scenario—including future ones—incapable of emitting an empty predicate.
	completedMarker := message.Triple{Predicate: "gateddag.unit.completed", Object: true, Confidence: 1.0}
	failedMarker := message.Triple{Predicate: "gateddag.unit.failed", Object: true, Confidence: 1.0}
	dec := newDispatchDecoder(t)
	sub, err := nc.Subscribe(ctx, subject, func(_ context.Context, msg *nats.Msg) {
		unitID, ok := dec(msg.Data)
		if !ok {
			return
		}
		dispatched.add(unitID)
		marker := completedMarker
		if failUnits[unitID] {
			marker = failedMarker
		}
		marker.Subject = unitID
		marker.Timestamp = time.Now()
		addMarker(markerCtx, nc, marker)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Executor.
	cfg := gateddagexec.Config{
		UnitEntityPrefix: prefix,
		DispatchSubject:  subject,
		BackstopInterval: backstop,
		// dispatch_dedupe_window must be >= backstop (ADR-070 B1). These are
		// artificial test backstops (incl. a 10m sentinel), so match the window to
		// the backstop to keep the config valid.
		DispatchDedupeWindow: backstop,
		CompletedPredicate:   pCompleted,
		FailedPredicate:      pFailed,
		DirtiedPredicate:     pDirtied,
		DependsOnPredicate:   pDependsOn,
		ClaimPredicate:       pClaim,
		FanOutInstanceID:     opts.fanOutInstanceID,
		StallSubject:         opts.stallSubject,
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	exec, err := gateddagexec.NewComponent(cfgJSON, component.Dependencies{NATSClient: nc, LifecycleManager: mgr, Logger: logger})
	require.NoError(t, err)
	require.NoError(t, exec.Initialize())
	require.NoError(t, exec.Start(ctx))
	t.Cleanup(func() { _ = exec.Stop(5 * time.Second) })

	return &fullStack{nc: nc, gi: gi, mgr: mgr, prefix: prefix, dispatched: dispatched}
}

// discard is an io.Writer sink for the default discard logger.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// newDispatchDecoder returns a decoder for the dispatch envelope keyed only on
// the gated-DAG payload (no payloadbuiltins dependency).
func newDispatchDecoder(t *testing.T) func([]byte) (string, bool) {
	t.Helper()
	reg := payloadregistry.New()
	require.NoError(t, gateddagexec.RegisterPayloads(reg))
	dec := message.NewDecoder(reg)
	return func(data []byte) (string, bool) {
		base, err := dec.Decode(data)
		if err != nil {
			return "", false
		}
		dm, ok := base.Payload().(*gateddagexec.DispatchMessage)
		if !ok {
			return "", false
		}
		return dm.UnitEntityID, true
	}
}

// seedUnit creates a unit entity with the given depends_on prerequisite IDs.
func (fs *fullStack) seedUnit(t *testing.T, id string, dependsOn ...string) {
	t.Helper()
	triples := make([]message.Triple, 0, len(dependsOn))
	for _, dep := range dependsOn {
		triples = append(triples, message.Triple{
			Subject: id, Predicate: pDependsOn, Object: dep, Timestamp: time.Now(), Confidence: 1.0,
		})
	}
	require.NoError(t, fs.gi.CreateEntity(context.Background(), &gtypes.EntityState{
		ID: id, Triples: triples, Version: 1, UpdatedAt: time.Now(),
	}))
}

// deleteEntity evicts a unit via the production delete mutation (triggers the
// rule engine's DELETED-branch $state cleanup).
func (fs *fullStack) deleteEntity(t *testing.T, id string) {
	t.Helper()
	data, err := json.Marshal(gtypes.DeleteEntityRequest{EntityID: id})
	require.NoError(t, err)
	_, err = fs.nc.RequestWithRetryClassified(context.Background(), "graph.mutation.entity.delete", data, 5*time.Second, natsclient.DefaultRetryConfig())
	require.NoError(t, err)
}

// unitHasPredicate reports whether the unit carries a triple with the predicate,
// read authoritatively via the prefix query.
func (fs *fullStack) unitHasPredicate(t *testing.T, id, predicate string) bool {
	t.Helper()
	data, err := json.Marshal(gtypes.PrefixQueryRequest{Prefix: fs.prefix, Limit: 100})
	require.NoError(t, err)
	resp, err := fs.nc.RequestClassified(context.Background(), "graph.ingest.query.prefix", data, 5*time.Second)
	require.NoError(t, err)
	var pr gtypes.PrefixQueryResponse
	require.NoError(t, json.Unmarshal(resp, &pr))
	for i := range pr.Entities {
		if pr.Entities[i].ID == id {
			return pr.Entities[i].GetTriple(predicate) != nil
		}
	}
	return false
}

// addMarker writes a terminal marker, retrying until the mutation lands, a 25s
// deadline elapses, ctx is cancelled, or the error is permanent. A real work
// consumer must write terminal markers reliably (at-least-once); the prior
// fire-and-forget (single RequestWithRetry, error DISCARDED) silently LOST the
// marker whenever a transport-level failure — graph-ingest's mutation subject
// momentarily slow/unready under CI Docker contention — blew RequestWithRetry's
// transport-retry budget. A lost marker leaves the unit dispatched-but-never-
// terminal, stalling the dependent assertions for the full Eventually window
// (gh#373, the real root cause behind the "flake"). 25s sits under fsEventually
// (30s local / 90s CI) so a genuinely-failing mutation still surfaces as the
// assertion failure rather than being masked by the wait.
//
// ctx is the test-scoped marker context, cancelled at teardown so no loop
// outlives the test or races client.Close. Synchronous in the consumer callback
// (see setupFullStack for the head-of-line-blocking trade-off). Must not touch
// *testing.T.
//
// NOTE: this makes the TEST consumer infallible. Production has no such
// guarantee and a stranded claimed-but-unmarked unit is not auto-recovered —
// tracked separately as a production gap (gh#373 follow-up).
func addMarker(ctx context.Context, nc *natsclient.Client, triple message.Triple) {
	req := gtypes.AddTripleRequest{Triple: triple}
	data, err := json.Marshal(req)
	if err != nil {
		return
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		_, err := nc.RequestWithRetryClassified(ctx, "graph.mutation.triple.add", data, 5*time.Second, natsclient.DefaultRetryConfig())
		if err == nil || errs.IsInvalid(err) || time.Now().After(deadline) {
			return // success, permanent rejection (won't pass on retry), or deadline
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func fsUnit(scenario, suffix string) string {
	return "fs.test." + scenario + ".fanout.unit." + suffix
}

// --- Scenario 1: depends_on respected under concurrent completion arrival ---

func TestFullStack_DependsOnOrderingAndDedup(t *testing.T) {
	const subject = "fs.dispatch.s1"
	prefix := "fs.test.s1.fanout.unit"
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject})

	// Diamond: a -> {b,c} -> d. The consumer completes each on dispatch.
	a, b, c, d := fsUnit("s1", "a"), fsUnit("s1", "b"), fsUnit("s1", "c"), fsUnit("s1", "d")
	fs.seedUnit(t, a)
	fs.seedUnit(t, b, a)
	fs.seedUnit(t, c, a)
	fs.seedUnit(t, d, b, c)

	// All four eventually dispatch, each exactly once (claim dedup).
	require.Eventually(t, func() bool {
		s := fs.dispatched.snapshot()
		return len(s) == 4
	}, fsEventually, 100*time.Millisecond, "all four units should dispatch; got %v", fs.dispatched.snapshot())

	for _, id := range []string{a, b, c, d} {
		require.Equalf(t, 1, fs.dispatched.count(id), "unit %s dispatched exactly once (dedup)", id)
	}

	// Dependency order: a before b/c; b and c before d.
	order := fs.dispatched.snapshot()
	pos := func(id string) int {
		for i, v := range order {
			if v == id {
				return i
			}
		}
		return -1
	}
	require.Less(t, pos(a), pos(b), "a must dispatch before b")
	require.Less(t, pos(a), pos(c), "a must dispatch before c")
	require.Less(t, pos(b), pos(d), "b must dispatch before d")
	require.Less(t, pos(c), pos(d), "c must dispatch before d")
}

// --- Scenario 2: reset survives an evicted terminal row (Stage C + Stage D) ---

func TestFullStack_ResetSurvivesEvictedRow(t *testing.T) {
	const subject = "fs.dispatch.s2"
	prefix := "fs.test.s2.fanout.unit"
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject})

	x := fsUnit("s2", "x")
	fs.seedUnit(t, x)

	// First dispatch + completion + durable claim.
	require.Eventually(t, func() bool { return fs.dispatched.count(x) == 1 }, fsEventually, 100*time.Millisecond,
		"x should dispatch once initially")
	require.Eventually(t, func() bool { return fs.unitHasPredicate(t, x, pClaim) }, fsEventually, 100*time.Millisecond,
		"x should carry the durable claim after dispatch")

	// Stage D setup: simulate a rule that exhausted its retry budget on x by
	// writing an exhausted MatchState directly into the RULE_STATE bucket.
	const ruleID = "fs-retry-rule"
	stateKey := ruleID + "." + x // buildStateKey(ruleID, entityID)
	fsSetRuleState(t, fs.nc, stateKey, x, ruleID)
	require.True(t, fsRuleStateExists(t, fs.nc, stateKey), "precondition: rule $state seeded for x")

	// RESET via eviction: delete x's entity. The rule engine's DELETED branch
	// cleans x's $state (Stage D); the executor's next read sees x gone.
	fs.deleteEntity(t, x)

	// Stage D: x's rule $state is cleared (no stale, exhausted budget survives).
	require.Eventually(t, func() bool { return !fsRuleStateExists(t, fs.nc, stateKey) }, fsEventually, 100*time.Millisecond,
		"Stage D: evicting x must clear its rule $state")

	// Recreate x fresh (no markers, no claim) and assert Stage C re-dispatch.
	fs.seedUnit(t, x)
	require.Eventually(t, func() bool { return fs.dispatched.count(x) >= 2 }, fsEventually, 100*time.Millisecond,
		"Stage C: recreated x must be re-dispatched (evicted-then-recreated read fresh, not idle-skipped)")
}

// --- Scenario 3: failed node holds dependents; independent branches flow ---

func TestFullStack_FailedNodeHoldsDependents(t *testing.T) {
	const subject = "fs.dispatch.s3"
	prefix := "fs.test.s3.fanout.unit"
	a, b, indep := fsUnit("s3", "a"), fsUnit("s3", "b"), fsUnit("s3", "indep")
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject, failUnits: map[string]bool{a: true}}) // a fails on dispatch

	fs.seedUnit(t, a)     // fails
	fs.seedUnit(t, b, a)  // depends on the failing a
	fs.seedUnit(t, indep) // independent branch

	// a and the independent unit both dispatch; a then fails, indep completes.
	require.Eventually(t, func() bool {
		return fs.dispatched.count(a) == 1 && fs.dispatched.count(indep) == 1
	}, fsEventually, 100*time.Millisecond, "a and indep should dispatch")
	require.Eventually(t, func() bool { return fs.unitHasPredicate(t, a, pFailed) }, fsEventually, 100*time.Millisecond,
		"a should carry the failed marker")

	// b stays held behind its failed prerequisite — give several backstop passes
	// to prove it is NOT dispatched.
	time.Sleep(2 * time.Second)
	require.Equal(t, 0, fs.dispatched.count(b), "b must stay held behind failed prerequisite a")
	require.Equal(t, 1, fs.dispatched.count(indep), "independent branch flowed exactly once")
}

// --- Scenario 4: a depends_on cycle surfaces as a stall, dispatching nothing ---

func TestFullStack_CycleSurfacesStall(t *testing.T) {
	const subject = "fs.dispatch.s4"
	prefix := "fs.test.s4.fanout.unit"
	logCap := &captureHandler{}
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject, logger: slog.New(logCap)})

	a, b := fsUnit("s4", "a"), fsUnit("s4", "b")
	fs.seedUnit(t, a, b) // a depends on b
	fs.seedUnit(t, b, a) // b depends on a → cycle

	// The stall is surfaced (logged) within a couple of backstop passes...
	require.Eventually(t, func() bool { return logCap.sawContaining("stalled") }, fsEventually, 100*time.Millisecond,
		"a depends_on cycle must surface as a stall, not silent idle")
	// ...and nothing is ever dispatched.
	require.Empty(t, fs.dispatched.snapshot(), "a cycle dispatches nothing")
}

// --- Stage D RULE_STATE helpers (direct KV; the MatchState shape is the rule
// engine's public JSON contract) ---

func ruleStateBucket(t *testing.T, nc *natsclient.Client) jetstream.KeyValue {
	t.Helper()
	js, err := nc.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(context.Background(), "RULE_STATE")
	require.NoError(t, err)
	return kv
}

func fsSetRuleState(t *testing.T, nc *natsclient.Client, key, entityID, ruleID string) {
	t.Helper()
	// Mirror rule.MatchState's JSON shape with an exhausted budget.
	state := map[string]any{
		"rule_id": ruleID, "entity_key": entityID, "is_matching": true,
		"iteration": 3, "max_iterations": 3, "last_checked": time.Now(),
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = ruleStateBucket(t, nc).Put(context.Background(), key, data)
	require.NoError(t, err)
}

func fsRuleStateExists(t *testing.T, nc *natsclient.Client, key string) bool {
	t.Helper()
	_, err := ruleStateBucket(t, nc).Get(context.Background(), key)
	return err == nil
}

// --- Phase 2.1 contract-completion scenarios (GH #363/#364/#365.3) ---

// TestFullStack_EventDrivenDispatchBeatsBackstop (#363) proves completions drive
// dispatch via the unit-prefix watch, not the backstop: with a deliberately long
// 30s backstop, a depth-3 chain completes in seconds (backstop-bound would be
// up to ~90s) — and each unit dispatches exactly once (no double-dispatch).
func TestFullStack_EventDrivenDispatchBeatsBackstop(t *testing.T) {
	const subject = "fs.dispatch.s5"
	prefix := "fs.test.s5.fanout.unit"
	// A 10m backstop provably cannot fire within the assertion window, so any
	// dispatch inside it proves the event-driven unit-watch path — not the
	// backstop — advanced the chain. The window is CI-contention-aware
	// (fsEventually: 30s local / 90s CI, gh#404); this test previously hardcoded
	// 12s and was the one full-stack test the gh#404 sweep missed, so it flaked
	// under testcontainer contention when the 3-unit chain's serial NATS
	// round-trips legitimately exceeded 12s.
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject, backstop: "10m"})

	a, b, c := fsUnit("s5", "a"), fsUnit("s5", "b"), fsUnit("s5", "c")
	fs.seedUnit(t, a)
	fs.seedUnit(t, b, a)
	fs.seedUnit(t, c, b)

	start := time.Now()
	require.Eventually(t, func() bool {
		return fs.dispatched.count(a) == 1 && fs.dispatched.count(b) == 1 && fs.dispatched.count(c) == 1
	}, fsEventually, 100*time.Millisecond, "chain should dispatch once each via the unit watch, not the 10m backstop")
	require.Less(t, time.Since(start), fsEventually, "dispatch completes within the window, well under the 10m backstop")
}

// TestFullStack_FanOutInstanceAutoCompletes (#364) proves the framework creates
// the FanOut instance in dispatching and auto-transitions it to completed once
// every unit is Done.
func TestFullStack_FanOutInstanceAutoCompletes(t *testing.T) {
	const subject = "fs.dispatch.s6"
	prefix := "fs.test.s6.fanout.unit"
	instID := "fs.test.gateddag.fanout.instance.s6"
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject, fanOutInstanceID: instID})

	ctx := context.Background()
	require.Eventually(t, func() bool {
		inst, err := fs.mgr.Get(ctx, gateddagexec.FanOutWorkflow, instID)
		return err == nil && inst.Phase() == "dispatching"
	}, fsEventually, 100*time.Millisecond, "FanOut instance should be created in dispatching")

	a, b := fsUnit("s6", "a"), fsUnit("s6", "b")
	fs.seedUnit(t, a)
	fs.seedUnit(t, b, a)

	require.Eventually(t, func() bool {
		inst, err := fs.mgr.Get(ctx, gateddagexec.FanOutWorkflow, instID)
		return err == nil && inst.Phase() == "completed"
	}, fsEventually, 100*time.Millisecond, "FanOut instance must auto-complete once every unit is Done")
}

// TestFullStack_StallEventPublished (#365.3) proves a depends_on cycle publishes
// an edge-triggered StallEvent to the configured stall subject.
func TestFullStack_StallEventPublished(t *testing.T) {
	const subject = "fs.dispatch.s7"
	const stallSubject = "fs.stall.s7"
	prefix := "fs.test.s7.fanout.unit"
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject, stallSubject: stallSubject})

	reg := payloadregistry.New()
	require.NoError(t, gateddagexec.RegisterPayloads(reg))
	dec := message.NewDecoder(reg)
	var got struct {
		sync.Mutex
		units []string
	}
	sub, err := fs.nc.Subscribe(context.Background(), stallSubject, func(_ context.Context, msg *nats.Msg) {
		base, derr := dec.Decode(msg.Data)
		if derr != nil {
			return
		}
		if se, ok := base.Payload().(*gateddagexec.StallEvent); ok {
			got.Lock()
			got.units = se.StalledUnits
			got.Unlock()
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	a, b := fsUnit("s7", "a"), fsUnit("s7", "b")
	fs.seedUnit(t, a, b)
	fs.seedUnit(t, b, a)

	require.Eventually(t, func() bool {
		got.Lock()
		defer got.Unlock()
		return len(got.units) == 2
	}, fsEventually, 100*time.Millisecond, "a cycle must publish a StallEvent naming both held units")
	require.Empty(t, fs.dispatched.snapshot(), "a cycle dispatches nothing")
}
