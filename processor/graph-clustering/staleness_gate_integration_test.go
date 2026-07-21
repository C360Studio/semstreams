//go:build integration

package graphclustering

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests drive the PRODUCTION readiness wire end to end: a real GRAPH_STATUS KV
// bucket, the real graph/readiness watcher the component binds in Start, real
// consumer-local freshness, and the real canonical gate. Nothing is faked — the
// predecessor of this file stood up a fake `graph.index.query.status` responder, and
// that subject no longer exists (ADR-083 D1: readiness is watchable KV state).
//
// The fixture writes the envelopes itself rather than running graph-index, so the
// consumer contract is tested independently of the producer's publish path (which has
// its own producer-side test). What matters here is that whatever lands on the key
// reaches the gate through the real watcher.
//
// SYNCHRONIZATION: KV delivery is asynchronous, so every publish is followed by a
// wait on the OBSERVABLE state the watcher holds — keyed on a per-publish marker
// stamped into a gate-irrelevant envelope field — never on a fixed sleep. The one
// genuinely time-based assertion (a status feed going quiet must expire) waits on the
// freshness predicate itself, with the producer heartbeat shortened to milliseconds so
// it costs ~1s instead of the 15s a production heartbeat would.

// statusFixture owns the GRAPH_STATUS bucket and stamps each publish with a unique
// marker so a test can wait for the exact envelope it just wrote.
type statusFixture struct {
	t      *testing.T
	bucket jetstream.KeyValue
	seq    atomic.Uint64
}

func newStatusFixture(ctx context.Context, t *testing.T, nc *natsclient.Client) *statusFixture {
	t.Helper()
	bucket, err := nc.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      readiness.BucketGraphStatus,
		Description: "ADR-083 readiness envelopes",
		History:     readiness.BucketHistory,
	})
	require.NoError(t, err, "GRAPH_STATUS bucket")
	return &statusFixture{t: t, bucket: bucket}
}

// publish writes one envelope to graph-index's key and returns the marker that
// identifies it. LastSynced carries the marker: the gate never reads it, so stamping
// it cannot change any decision under test.
func (f *statusFixture) publish(ctx context.Context, status graph.IndexStatusResponse) string {
	f.t.Helper()
	marker := strconv.FormatUint(f.seq.Add(1), 10)
	status.LastSynced = marker

	data, err := json.Marshal(status)
	require.NoError(f.t, err)
	_, err = f.bucket.Put(ctx, readiness.KeyGraphIndex, data)
	require.NoError(f.t, err, "publish readiness envelope")
	return marker
}

// gateHarness is a component wired to the real watcher over the real bucket.
type gateHarness struct {
	t         *testing.T
	component *Component
	fixture   *statusFixture
}

// testHeartbeat is the producer heartbeat the harness declares. It sets the freshness
// window (3x heartbeat = 600ms), which is what keeps the feed-dies test at about a
// second of wall clock instead of the 15s a production 5s heartbeat would require.
const testHeartbeat = 200 * time.Millisecond

func newGateHarness(ctx context.Context, t *testing.T, nc *natsclient.Client, f *statusFixture, cfg Config) *gateHarness {
	t.Helper()
	cfg.Ports = &component.PortConfig{
		Inputs:  []component.PortDefinition{{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates}},
		Outputs: []component.PortDefinition{{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex}},
	}
	cfg.DetectionIntervalStr = "1s"
	cfg.MinCommunitySize = 2
	cfg.MaxIterations = 10
	cfg.ApplyDefaults()

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, err := CreateGraphClustering(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)

	c := comp.(*Component)
	c.statusHeartbeat = testHeartbeat
	// The same call Start makes; the detection loop is not needed to exercise the
	// gate, and driving it here keeps each case deterministic instead of racing a
	// ticker.
	require.NoError(t, c.startStatusWatcher(ctx))
	t.Cleanup(func() {
		if c.statusWatcher != nil {
			c.statusWatcher.Stop()
		}
	})
	return &gateHarness{t: t, component: c, fixture: f}
}

// awaitEnvelope blocks until the component's watcher holds the envelope identified by
// marker. This is explicit synchronization on real observable state, not a sleep: the
// condition is "the thing I published has been delivered and applied".
func (h *gateHarness) awaitEnvelope(marker string) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		r := h.component.statusWatcher.Read()
		return r.Known && r.Status.LastSynced == marker
	}, 10*time.Second, 5*time.Millisecond, "watcher never received the published envelope %s", marker)
}

// publishAndAwait writes an envelope and returns once the gate would see it.
func (h *gateHarness) publishAndAwait(ctx context.Context, status graph.IndexStatusResponse) {
	h.t.Helper()
	h.awaitEnvelope(h.fixture.publish(ctx, status))
}

// building is the envelope graph-index publishes while catching up. Built through the
// production projection so Ready/State/Lag carry the values the real producer emits
// rather than a hand-assembled shape that could drift from ComputeIndexStatus.
// building projects a HEALTHY producer's envelope: a real graph-index stamps
// bootstrap_complete once its initial build finished, and every case in these tables is
// about freshness, not about health. The cutover case gets preBootstrap below, so the
// two questions stay visibly separate in the fixtures the way they are in the gate.
func building(indexed, target uint64, staleness time.Duration) graph.IndexStatusResponse {
	now := time.Now()
	status := graph.ComputeIndexStatus(graph.IndexStatusInputs{
		Indexed:   indexed,
		Target:    target,
		IndexedAt: now.Add(-staleness),
		Now:       now,
	})
	status.BootstrapComplete = true
	return status
}

// preBootstrap projects an index still doing its initial build (the gh#474 cutover
// window): plausibly small lag, half-materialised keyset, bootstrap_complete false.
func preBootstrap(indexed, target uint64, staleness time.Duration) graph.IndexStatusResponse {
	status := building(indexed, target, staleness)
	status.Ready = false
	status.State = graph.IndexStateBuilding
	status.BootstrapComplete = false
	return status
}

// TestIntegration_StalenessGate_BoundedStaleness covers the spec scenarios for the
// bounded-staleness mode over the real wire: a continuously-written graph within the
// bound clusters, while every hard stop and an over-stale view defer under the same
// generous tolerance.
func TestIntegration_StalenessGate_BoundedStaleness(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	h := newGateHarness(ctx, t, nc, fixture, Config{MaxStalenessStr: "3s"})

	tests := []struct {
		name        string
		status      graph.IndexStatusResponse
		wantProceed bool
		wantReason  graph.DeferReason
	}{
		{
			name:        "continuous write within the staleness bound clusters",
			status:      building(450, 500, 1200*time.Millisecond),
			wantProceed: true,
		},
		{
			name:        "a view older than the bound defers",
			status:      building(350, 500, 12*time.Second),
			wantProceed: false,
			wantReason:  graph.DeferOverStaleness,
		},
		{
			name:        "caught up clusters with zero staleness",
			status:      building(500, 500, 0),
			wantProceed: true,
		},
		{
			name:        "degraded is a hard stop at any tolerance",
			status:      graph.IndexStatusResponse{State: graph.IndexStateDegraded, TargetRevision: 500, Lag: 5, StalenessMs: 100},
			wantProceed: false,
			wantReason:  graph.DeferHardStop,
		},
		{
			name:        "reset_required is a hard stop at any tolerance",
			status:      graph.IndexStatusResponse{State: graph.IndexStateResetRequired, TargetRevision: 500, StalenessMs: 100},
			wantProceed: false,
			wantReason:  graph.DeferHardStop,
		},
		{
			// Health outranks freshness: a half-built index is not "a bit stale", and
			// no tolerance may wave it through (ADR-084 D1).
			name:        "an unbootstrapped index defers under a generous tolerance",
			status:      preBootstrap(490, 500, 100*time.Millisecond),
			wantProceed: false,
			wantReason:  graph.DeferBootstrapIncomplete,
		},
		{
			// The counterpart the old TargetRevision==0 rule got wrong: 0/0 after
			// enumeration is a COMPLETED build, and detection over an empty graph is a
			// valid (empty) result rather than something to defer forever.
			name:        "an authoritatively empty graph proceeds",
			status:      graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady, BootstrapComplete: true},
			wantProceed: true,
		},
		{
			// A not-ready envelope whose staleness is merely ABSENT (the presence
			// encoding) must not be read as "0ms stale" and waved through.
			name:        "a not-ready envelope with no computed staleness defers",
			status:      graph.IndexStatusResponse{State: graph.IndexStateBuilding, IndexedRevision: 400, TargetRevision: 500, Lag: 100},
			wantProceed: false,
			wantReason:  graph.DeferOverStaleness,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.publishAndAwait(ctx, tt.status)

			got := h.component.evaluateReadiness()

			assert.Equal(t, tt.wantProceed, got.proceed, "gate decision over the real wire")
			assert.Equal(t, tt.wantReason, got.reason, "defer reason must be attributable")
			assert.False(t, got.ungated, "a fresh status never takes the ungated path")
			assert.True(t, got.reading.Fresh, "a just-published envelope must read as fresh")
		})
	}
}

// TestIntegration_StalenessGate_DefaultIsTheExactGate pins the contract-preserving
// default: with max_staleness unset the gate proceeds exactly when the envelope's own
// Ready bit is set, for every state, target, and staleness — bit-for-bit the
// pre-ADR-083 behavior.
func TestIntegration_StalenessGate_DefaultIsTheExactGate(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	h := newGateHarness(ctx, t, nc, fixture, Config{}) // max_staleness absent → 0
	require.Equal(t, time.Duration(0), h.component.config.MaxStaleness(), "the default must be the exact gate")

	battery := []graph.IndexStatusResponse{
		building(100, 100, 0),                   // caught up  → Ready true
		building(60, 100, 500*time.Millisecond), // lagging, fresh view → Ready false
		building(60, 100, 30*time.Second),       // lagging, ancient view → Ready false
		preBootstrap(0, 0, 0),                   // pre-enumeration → Ready false
		{State: graph.IndexStateDegraded, TargetRevision: 100, Lag: 10, StalenessMs: 50},
		{State: graph.IndexStateResetRequired},
	}
	for i, status := range battery {
		t.Run(fmt.Sprintf("case_%d_%s", i, status.State), func(t *testing.T) {
			h.publishAndAwait(ctx, status)

			got := h.component.evaluateReadiness()

			assert.Equal(t, status.Ready, got.proceed,
				"max_staleness 0 must equal exact Ready for %+v", status)
		})
	}
}

// TestIntegration_StalenessGate_StatusFeedDies is the gh#590 regression, end to end: a
// consumer holding a last-known READY envelope whose producer goes quiet must flip to
// UNKNOWN and fail closed, attributed to the transport — NOT to index state, and NOT
// rescued by a generous tolerance. Before ADR-083 this case was a request timeout
// wearing not-ready's log line, which is what cost three investigation cycles.
func TestIntegration_StalenessGate_StatusFeedDies(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	// A deliberately GENEROUS tolerance: it must never be evaluated against unknown
	// state, so it cannot rescue a dead feed.
	h := newGateHarness(ctx, t, nc, fixture, Config{MaxStalenessStr: "10m"})

	// The producer is alive and the index is caught up: detection runs.
	h.publishAndAwait(ctx, building(500, 500, 0))
	live := h.component.evaluateReadiness()
	require.True(t, live.proceed, "a fresh ready envelope must clear the gate")
	require.True(t, live.reading.Fresh)

	// The producer dies. No further publishes; the held envelope still says Ready.
	// Waiting on the freshness PREDICATE (not a fixed sleep) is the assertion: the
	// held status must expire on its own after 3x the declared heartbeat.
	require.Eventually(t, func() bool {
		return !h.component.evaluateReadiness().proceed
	}, 10*time.Second, 10*time.Millisecond, "a quiet status feed must expire and fail closed")

	dead := h.component.evaluateReadiness()
	assert.False(t, dead.proceed, "an unknown status must fail closed under any tolerance")
	assert.Equal(t, graph.DeferStatusUnknown, dead.reason,
		"a dead feed must be attributed to the transport, never to index state")
	assert.False(t, dead.reading.Fresh, "the held envelope must be marked stale")
	assert.True(t, dead.reading.Known, "the last-known envelope survives for diagnostics")
	assert.True(t, dead.reading.Status.Ready,
		"the stale envelope still SAYS ready — which is exactly why freshness, not its contents, decides")
	assert.GreaterOrEqual(t, dead.reading.Age, readiness.FreshnessMultiplier*testHeartbeat,
		"the reading must carry the age that justifies the unknown verdict")

	// And the producer coming back makes it fresh again on the next delivery — a
	// transient blip must not become a permanent status_unknown.
	h.publishAndAwait(ctx, building(600, 600, 0))
	recovered := h.component.evaluateReadiness()
	assert.True(t, recovered.proceed, "a recovered feed must clear the gate again")
	assert.Equal(t, graph.DeferNone, recovered.reason)
}

// TestIntegration_StalenessGate_UnknownStatusHonorsUngatedEscape: a standalone
// deployment (clustering with no graph-index, so no bucket key ever appears) keeps the
// pre-ADR-083 allow_ungated_reads escape, and a run taken that way is marked ungated
// so it cannot publish a fabricated "verified caught up" staleness.
func TestIntegration_StalenessGate_UnknownStatusHonorsUngatedEscape(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// No bucket is created at all: this is the "producer was never deployed here"
	// shape, which must not be a startup failure.
	fixture := &statusFixture{t: t}

	closed := newGateHarness(ctx, t, nc, fixture, Config{MaxStalenessStr: "10m"})
	escaped := newGateHarness(ctx, t, nc, fixture, Config{MaxStalenessStr: "10m", AllowUngatedReads: true})

	require.Eventually(t, func() bool {
		return closed.component.statusWatcher.Read().Err != nil
	}, 10*time.Second, 10*time.Millisecond, "the absent bucket must be recorded for the defer log")

	failClosed := closed.component.evaluateReadiness()
	assert.False(t, failClosed.proceed, "an absent readiness feed must fail closed by default")
	assert.Equal(t, graph.DeferStatusUnknown, failClosed.reason)
	assert.False(t, failClosed.reading.Known, "nothing may be fabricated with no producer")
	assert.Error(t, failClosed.reading.Err, "the bucket error must reach the structured defer log")

	ungated := escaped.component.evaluateReadiness()
	assert.True(t, ungated.proceed, "allow_ungated_reads must keep its standalone escape")
	assert.True(t, ungated.ungated, "the escape must be marked so no staleness is claimed")
}

// TestIntegration_StalenessGate_WiredByComponentStart closes the helper-direct gap:
// the readiness watcher must be bound by the component's real Start, not only by the
// harness above. Without this, every test here could pass while production never
// bound a watcher at all.
func TestIntegration_StalenessGate_WiredByComponentStart(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	marker := fixture.publish(ctx, building(500, 500, 0))

	js, err := nc.JetStream()
	require.NoError(t, err)
	for _, bucket := range []string{graph.BucketEntityStates, graph.BucketOutgoingIndex, graph.BucketIncomingIndex} {
		_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket})
		require.NoError(t, err)
	}

	config := Config{
		MaxStalenessStr:      "3s",
		DetectionIntervalStr: "1h", // long: this test is about Start's wiring, not ticks
		MinCommunitySize:     2,
		MaxIterations:        10,
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates}},
			Outputs: []component.PortDefinition{{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex}},
		},
	}
	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphClustering(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(10 * time.Second) })

	require.NotNil(t, c.statusWatcher, "Start must bind the readiness watcher")
	require.Eventually(t, func() bool {
		r := c.statusWatcher.Read()
		return r.Known && r.Status.LastSynced == marker
	}, 10*time.Second, 5*time.Millisecond, "the watcher bound by Start must receive the published envelope")

	decision := c.evaluateReadiness()
	assert.True(t, decision.proceed, "the production-wired gate must clear on a fresh ready envelope")

	// Stop must release the watcher so a later Start can bind a fresh one (a stopped
	// Watcher cannot be restarted).
	require.NoError(t, c.Stop(10*time.Second))
	assert.Nil(t, c.statusWatcher, "Stop must detach the watcher")
}
