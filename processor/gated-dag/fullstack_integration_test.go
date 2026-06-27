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
	"github.com/c360studio/semstreams/pkg/lifecycle"
	gateddagexec "github.com/c360studio/semstreams/processor/gated-dag"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/processor/rule"
)

// Predicate vocabulary — set explicitly on the executor config so the test and
// the executor agree (the defaults are unexported).
const (
	pCompleted = "gateddag.completed"
	pFailed    = "gateddag.failed"
	pDirtied   = "gateddag.dirtied"
	pDependsOn = "gateddag.depends_on"
	pClaim     = "gateddag.claim"
)

const fsBackstop = "250ms"

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
	prefix     string
	dispatched *dispatchLog
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
func setupFullStack(t *testing.T, prefix, subject string, failUnits map[string]bool, logger *slog.Logger) *fullStack {
	t.Helper()
	ctx := context.Background()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discard{}, nil))
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
	rcfg.EntityWatchPatterns = []string{"fs.test.>"}
	rproc, err := rule.NewProcessor(nc, &rcfg)
	require.NoError(t, err)
	require.NoError(t, rproc.Initialize())
	require.NoError(t, rproc.Start(ctx))
	t.Cleanup(func() { _ = rproc.Stop(5 * time.Second) })

	mgr := lifecycle.NewManager(nc, nil)

	dispatched := &dispatchLog{}

	// Demo consumer: on each dispatch, complete the unit (or fail it). This is
	// the work-completion loop a real consumer (semspec) provides; here it just
	// writes the terminal marker so the executor's next backstop advances the DAG.
	dec := newDispatchDecoder(t)
	sub, err := nc.Subscribe(ctx, subject, func(_ context.Context, msg *nats.Msg) {
		unitID, ok := dec(msg.Data)
		if !ok {
			return
		}
		dispatched.add(unitID)
		pred := pCompleted
		if failUnits[unitID] {
			pred = pFailed
		}
		addMarker(nc, unitID, pred)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Executor.
	cfg := gateddagexec.Config{
		UnitEntityPrefix:   prefix,
		DispatchSubject:    subject,
		BackstopInterval:   fsBackstop,
		CompletedPredicate: pCompleted,
		FailedPredicate:    pFailed,
		DirtiedPredicate:   pDirtied,
		DependsOnPredicate: pDependsOn,
		ClaimPredicate:     pClaim,
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	exec, err := gateddagexec.NewComponent(cfgJSON, component.Dependencies{NATSClient: nc, LifecycleManager: mgr, Logger: logger})
	require.NoError(t, err)
	require.NoError(t, exec.Initialize())
	require.NoError(t, exec.Start(ctx))
	t.Cleanup(func() { _ = exec.Stop(5 * time.Second) })

	return &fullStack{nc: nc, gi: gi, prefix: prefix, dispatched: dispatched}
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

// addMarker appends a presence marker (best-effort; called from the consumer
// goroutine so it must not touch *testing.T).
func addMarker(nc *natsclient.Client, unitID, predicate string) {
	req := gtypes.AddTripleRequest{Triple: message.Triple{
		Subject: unitID, Predicate: predicate, Object: true, Timestamp: time.Now(), Confidence: 1.0,
	}}
	data, err := json.Marshal(req)
	if err != nil {
		return
	}
	_, _ = nc.RequestWithRetryClassified(context.Background(), "graph.mutation.triple.add", data, 5*time.Second, natsclient.DefaultRetryConfig())
}

func fsUnit(scenario, suffix string) string {
	return "fs.test." + scenario + ".fanout.unit." + suffix
}

// --- Scenario 1: depends_on respected under concurrent completion arrival ---

func TestFullStack_DependsOnOrderingAndDedup(t *testing.T) {
	const subject = "fs.dispatch.s1"
	prefix := "fs.test.s1.fanout.unit"
	fs := setupFullStack(t, prefix, subject, nil, nil)

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
	}, 10*time.Second, 100*time.Millisecond, "all four units should dispatch; got %v", fs.dispatched.snapshot())

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
	fs := setupFullStack(t, prefix, subject, nil, nil)

	x := fsUnit("s2", "x")
	fs.seedUnit(t, x)

	// First dispatch + completion + durable claim.
	require.Eventually(t, func() bool { return fs.dispatched.count(x) == 1 }, 10*time.Second, 100*time.Millisecond,
		"x should dispatch once initially")
	require.Eventually(t, func() bool { return fs.unitHasPredicate(t, x, pClaim) }, 5*time.Second, 100*time.Millisecond,
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
	require.Eventually(t, func() bool { return !fsRuleStateExists(t, fs.nc, stateKey) }, 5*time.Second, 100*time.Millisecond,
		"Stage D: evicting x must clear its rule $state")

	// Recreate x fresh (no markers, no claim) and assert Stage C re-dispatch.
	fs.seedUnit(t, x)
	require.Eventually(t, func() bool { return fs.dispatched.count(x) >= 2 }, 10*time.Second, 100*time.Millisecond,
		"Stage C: recreated x must be re-dispatched (evicted-then-recreated read fresh, not idle-skipped)")
}

// --- Scenario 3: failed node holds dependents; independent branches flow ---

func TestFullStack_FailedNodeHoldsDependents(t *testing.T) {
	const subject = "fs.dispatch.s3"
	prefix := "fs.test.s3.fanout.unit"
	a, b, indep := fsUnit("s3", "a"), fsUnit("s3", "b"), fsUnit("s3", "indep")
	fs := setupFullStack(t, prefix, subject, map[string]bool{a: true}, nil) // a fails on dispatch

	fs.seedUnit(t, a)     // fails
	fs.seedUnit(t, b, a)  // depends on the failing a
	fs.seedUnit(t, indep) // independent branch

	// a and the independent unit both dispatch; a then fails, indep completes.
	require.Eventually(t, func() bool {
		return fs.dispatched.count(a) == 1 && fs.dispatched.count(indep) == 1
	}, 10*time.Second, 100*time.Millisecond, "a and indep should dispatch")
	require.Eventually(t, func() bool { return fs.unitHasPredicate(t, a, pFailed) }, 5*time.Second, 100*time.Millisecond,
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
	fs := setupFullStack(t, prefix, subject, nil, slog.New(logCap))

	a, b := fsUnit("s4", "a"), fsUnit("s4", "b")
	fs.seedUnit(t, a, b) // a depends on b
	fs.seedUnit(t, b, a) // b depends on a → cycle

	// The stall is surfaced (logged) within a couple of backstop passes...
	require.Eventually(t, func() bool { return logCap.sawContaining("stalled") }, 5*time.Second, 100*time.Millisecond,
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
