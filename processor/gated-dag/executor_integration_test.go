//go:build integration

package gateddagexec

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
)

const (
	itestPrefix          = "itest.ops.plan.fanout.unit"
	itestDispatchSubject = "gateddag.itest.dispatch"
)

// startStack boots a real NATS testcontainer + graph-ingest + lifecycle.Manager
// + the gated-DAG executor wired through NewComponent (the production wire, not
// internal helpers). cfg's FanOutWorkflow/UnitEntityPrefix/DispatchSubject are
// pre-set by the caller; the rest defaults.
func startStack(t *testing.T, backstop string) (*graphingest.Component, *natsclient.Client, *Component) {
	t.Helper()
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	nc := tc.Client

	// graph-ingest serves graph.ingest.query.prefix + graph.mutation.entity.*
	giJSON, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	giDisc, err := graphingest.CreateGraphIngest(giJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	gi := giDisc.(*graphingest.Component)
	require.NoError(t, gi.Initialize())
	require.NoError(t, gi.Start(ctx))
	t.Cleanup(func() { _ = gi.Stop(5 * time.Second) })

	mgr := lifecycle.NewManager(nc, nil)

	cfg := Config{
		UnitEntityPrefix: itestPrefix,
		DispatchSubject:  itestDispatchSubject,
		BackstopInterval: backstop,
		// dispatch_dedupe_window must be >= backstop (ADR-070 B1); these tests use
		// artificial backstops (incl. a 10m sentinel), so match the window to it.
		DispatchDedupeWindow: backstop,
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	execDisc, err := NewComponent(cfgJSON, component.Dependencies{NATSClient: nc, LifecycleManager: mgr})
	require.NoError(t, err)
	require.NoError(t, execDisc.Initialize())

	time.Sleep(100 * time.Millisecond) // let graph-ingest subscriptions stabilise
	require.NoError(t, execDisc.Start(ctx))
	t.Cleanup(func() { _ = execDisc.Stop(5 * time.Second) })

	return gi, nc, execDisc
}

// seedUnit creates a unit entity with the given (predicate, object) triples via
// the real graph-ingest create path.
func seedUnit(t *testing.T, gi *graphingest.Component, id string, triples ...message.Triple) {
	t.Helper()
	for i := range triples {
		triples[i].Subject = id
		if triples[i].Timestamp.IsZero() {
			triples[i].Timestamp = time.Now()
		}
	}
	require.NoError(t, gi.CreateEntity(context.Background(), &graph.EntityState{
		ID: id, Triples: triples, Version: 1, UpdatedAt: time.Now(),
	}))
}

func unitID(suffix string) string { return itestPrefix + "." + suffix }

// dispatchCapture collects the unit IDs published to the dispatch subject.
type dispatchCapture struct {
	mu  sync.Mutex
	ids []string
	dec *message.Decoder
}

func subscribeDispatch(t *testing.T, nc *natsclient.Client) *dispatchCapture {
	t.Helper()
	reg := payloadregistry.New()
	require.NoError(t, RegisterPayloads(reg))
	dc := &dispatchCapture{dec: message.NewDecoder(reg)}
	sub, err := nc.Subscribe(context.Background(), itestDispatchSubject, func(_ context.Context, msg *nats.Msg) {
		base, derr := dc.dec.Decode(msg.Data)
		if derr != nil {
			return
		}
		dm, ok := base.Payload().(*DispatchMessage)
		if !ok {
			return
		}
		dc.mu.Lock()
		dc.ids = append(dc.ids, dm.UnitEntityID)
		dc.mu.Unlock()
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return dc
}

func (c *dispatchCapture) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.ids))
	copy(out, c.ids)
	return out
}

// TestIntegration_GatedDispatch_DependsOnAndDedup drives the whole wire: a
// prerequisite completes, its dependent is dispatched exactly once, the dispatch
// commits a durable claim, and a later backstop pass does not re-dispatch.
func TestIntegration_GatedDispatch_DependsOnAndDedup(t *testing.T) {
	gi, nc, exec := startStack(t, "300ms")
	c := exec
	capt := subscribeDispatch(t, nc)

	a, b := unitID("a"), unitID("b")
	seedUnit(t, gi, a, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "completed"), Object: true, Confidence: 1.0}) // a is Done
	seedUnit(t, gi, b, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: a, Confidence: 1.0})   // b depends on a → dispatchable

	// Wait for b to be dispatched (a backstop pass picks up the seeded units).
	require.Eventually(t, func() bool {
		ids := capt.snapshot()
		return len(ids) == 1 && ids[0] == b
	}, 5*time.Second, 50*time.Millisecond, "b should be dispatched exactly once; a (Done) never")

	// b must now carry the durable claim marker (read authoritatively).
	require.Eventually(t, func() bool {
		states := readPrefix(t, nc, itestPrefix)
		for _, s := range states {
			if s.ID == b && s.GetTriple(c.cfg.ClaimPredicate) != nil {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "b should carry the claim marker after dispatch")

	// Let several more backstop passes run; b must NOT be re-dispatched (dedup).
	time.Sleep(1 * time.Second)
	require.Equal(t, []string{b}, capt.snapshot(), "claim dedup prevents re-dispatch across passes")
}

// TestIntegration_BootReconcile proves restart recovery: seeding BEFORE the
// executor starts, the initial reconcile (boot read of authoritative KV)
// dispatches the ready-unclaimed dependent with no external trigger.
func TestIntegration_BootReconcile(t *testing.T) {
	// Long backstop so the dispatch we observe came from the boot reconcile, not
	// a backstop tick.
	gi, nc, exec := startStack(t, "10m")
	capt := subscribeDispatch(t, nc)

	a, b := unitID("a"), unitID("b")
	seedUnit(t, gi, a, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "completed"), Object: true, Confidence: 1.0})
	seedUnit(t, gi, b, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: a, Confidence: 1.0})

	// startStack already called Start. The initial reEvaluate fires on Start —
	// but seeding happened after Start here, so nudge a fresh reconcile by
	// restarting the executor against the now-seeded bucket.
	require.NoError(t, exec.Stop(5*time.Second))
	require.NoError(t, exec.Start(context.Background()))

	require.Eventually(t, func() bool {
		ids := capt.snapshot()
		return len(ids) >= 1 && ids[len(ids)-1] == b
	}, 5*time.Second, 50*time.Millisecond, "boot reconcile should dispatch b from authoritative KV with no backstop tick")
}

// readPrefix reads the authoritative unit set directly from graph-ingest.
func readPrefix(t *testing.T, nc *natsclient.Client, prefix string) []graph.EntityState {
	t.Helper()
	reqData, err := json.Marshal(graph.PrefixQueryRequest{Prefix: prefix, Limit: 100})
	require.NoError(t, err)
	respData, err := nc.RequestClassified(context.Background(), "graph.ingest.query.prefix", reqData, 5*time.Second)
	require.NoError(t, err)
	var resp graph.PrefixQueryResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	return resp.Entities
}
