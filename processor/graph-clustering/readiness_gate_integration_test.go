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
// production projection so Ready/State/Lag/StalenessMs carry the values the real
// producer emits rather than a hand-assembled shape that could drift from
// ComputeIndexStatus — which matters more now than it did under a tolerance, because
// the staleness these fixtures carry is the value the gate must be proven to IGNORE.
//
// building projects a HEALTHY producer's envelope: a real graph-index stamps
// bootstrap_complete once its initial build finished. The cutover case gets preBootstrap
// below, so the one remaining health "not yet" stays visibly separate in the fixtures
// the way it is in the gate.
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

// TestIntegration_ReadinessGate_HealthOnly covers the spec scenarios over the real
// wire: a healthy index clusters at ANY view age, while every index-health fault defers
// with its own attributable reason.
func TestIntegration_ReadinessGate_HealthOnly(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	h := newGateHarness(ctx, t, nc, fixture, Config{})

	tests := []struct {
		name        string
		status      graph.IndexStatusResponse
		wantProceed bool
		wantReason  graph.DeferReason
	}{
		{
			name:        "continuous write clusters",
			status:      building(450, 500, 6*time.Second),
			wantProceed: true,
		},
		{
			// The inverse of the retired "a view older than the bound defers" row.
			// Nothing about age withholds a run: detection re-derives the whole
			// partition every cycle, so a minute-old view yields a minute-old partition
			// that the next cycle overwrites — strictly better than republishing
			// nothing and leaving an even older one standing.
			name:        "a minute-old view still clusters",
			status:      building(350, 500, 60*time.Second),
			wantProceed: true,
		},
		{
			name:        "a day-old view still clusters",
			status:      building(350, 500, 24*time.Hour),
			wantProceed: true,
		},
		{
			name:        "caught up clusters with zero staleness",
			status:      building(500, 500, 0),
			wantProceed: true,
		},
		{
			name:        "degraded is a hard stop",
			status:      graph.IndexStatusResponse{State: graph.IndexStateDegraded, BootstrapComplete: true, TargetRevision: 500, Lag: 5, StalenessMs: 100},
			wantProceed: false,
			wantReason:  graph.DeferHardStop,
		},
		{
			name:        "reset_required is a hard stop",
			status:      graph.IndexStatusResponse{State: graph.IndexStateResetRequired, BootstrapComplete: true, TargetRevision: 500, StalenessMs: 100},
			wantProceed: false,
			wantReason:  graph.DeferHardStop,
		},
		{
			// The gh#474 guard, and the ONLY remaining "not yet". A half-built index is
			// not "a bit stale": the keyset is partially materialised, so detection
			// would derive a partition from topology that does not exist yet.
			name:        "an unbootstrapped index defers",
			status:      preBootstrap(490, 500, 2*time.Second),
			wantProceed: false,
			wantReason:  graph.DeferBootstrapIncomplete,
		},
		{
			// Version skew reaches the wire as a state this consumer cannot interpret.
			// Allow-list, not deny-list: it must not read as "not degraded, therefore
			// healthy".
			name: "an uninterpretable state defers",
			status: graph.IndexStatusResponse{State: "rebuilding", BootstrapComplete: true,
				TargetRevision: 500, StalenessMs: 100},
			wantProceed: false,
			wantReason:  graph.DeferUnrecognizedState,
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
			// The inverse of the retired "no computed staleness defers" row. The
			// PRESENCE encoding still holds on the wire (0 means "no age to report",
			// not "0ms old") — it just no longer blocks anything, because nothing reads
			// the age to decide. A consumer that SURFACES it still owes the encoding.
			name: "a not-ready envelope with no computed staleness proceeds",
			status: graph.IndexStatusResponse{State: graph.IndexStateBuilding,
				BootstrapComplete: true, IndexedRevision: 400, TargetRevision: 500, Lag: 100},
			wantProceed: true,
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

// TestIntegration_ReadinessGate_CoverageIsInert replaces the retired
// "DefaultIsTheExactGate" pin, and inverts it. That test asserted proceed == Ready for
// every envelope in a battery — the exact coupling this change deletes. What must hold
// now is the opposite: Ready is COVERAGE and the gate never consults it, so across the
// same battery the verdict tracks HEALTH and nothing else.
func TestIntegration_ReadinessGate_CoverageIsInert(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	h := newGateHarness(ctx, t, nc, fixture, Config{})

	battery := []struct {
		status      graph.IndexStatusResponse
		wantProceed bool
	}{
		{building(100, 100, 0), true},                   // caught up   → Ready true
		{building(60, 100, 500*time.Millisecond), true}, // lagging     → Ready false, healthy
		{building(60, 100, 30*time.Second), true},       // far behind  → Ready false, healthy
		{preBootstrap(0, 0, 0), false},                  // still building
		{graph.IndexStatusResponse{State: graph.IndexStateDegraded, BootstrapComplete: true,
			TargetRevision: 100, Lag: 10, StalenessMs: 50}, false},
		{graph.IndexStatusResponse{State: graph.IndexStateResetRequired, BootstrapComplete: true}, false},
	}
	for i, tc := range battery {
		t.Run(fmt.Sprintf("case_%d_%s", i, tc.status.State), func(t *testing.T) {
			h.publishAndAwait(ctx, tc.status)

			got := h.component.evaluateReadiness()

			assert.Equal(t, tc.wantProceed, got.proceed,
				"the verdict must follow health, not Ready=%v, for %+v", tc.status.Ready, tc.status)
		})
	}

	// The direct statement: two of those envelopes are healthy-but-not-Ready (battery[1]
	// and battery[2]; battery[0] is building at 100/100, which computes Ready=true) and
	// both cleared, so coverage licensed nothing and withheld nothing.
	require.False(t, battery[2].status.Ready, "fixture drifted: case 2 must be a not-Ready envelope")
}

// TestIntegration_ReadinessGate_StatusFeedDies is the gh#590 regression, end to end: a
// consumer holding a last-known READY envelope whose producer goes quiet must flip to
// UNKNOWN and fail closed, attributed to the transport — NOT to index state. Before
// ADR-083 this case was a request timeout wearing not-ready's log line, which is what
// cost three investigation cycles.
//
// This is the ENVELOPE-liveness question, and it is the one "freshness" that survived
// the view-age deletion. The distinction the test proves: the held envelope's CONTENTS
// still say Ready, and are ignored, because the consumer can no longer vouch for them.
func TestIntegration_ReadinessGate_StatusFeedDies(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := newStatusFixture(ctx, t, nc)
	h := newGateHarness(ctx, t, nc, fixture, Config{})

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
	assert.False(t, dead.proceed, "an unknown status must fail closed")
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

// TestIntegration_ReadinessGate_UnknownStatusHonorsUngatedEscape: a standalone
// deployment (clustering with no graph-index, so no bucket key ever appears) keeps the
// pre-ADR-083 allow_ungated_reads escape, and a run taken that way is marked ungated
// so it cannot publish a fabricated "verified caught up" staleness.
func TestIntegration_ReadinessGate_UnknownStatusHonorsUngatedEscape(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// No bucket is created at all: this is the "producer was never deployed here"
	// shape, which must not be a startup failure.
	fixture := &statusFixture{t: t}

	closed := newGateHarness(ctx, t, nc, fixture, Config{})
	escaped := newGateHarness(ctx, t, nc, fixture, Config{AllowUngatedReads: true})

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

// TestIntegration_ReadinessGate_WiredByComponentStart closes the helper-direct gap:
// the readiness watcher must be bound by the component's real Start, not only by the
// harness above. Without this, every test here could pass while production never
// bound a watcher at all.
func TestIntegration_ReadinessGate_WiredByComponentStart(t *testing.T) {
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
