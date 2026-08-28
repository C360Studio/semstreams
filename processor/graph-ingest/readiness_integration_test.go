//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// readEnvelope reads the published graph-ingest readiness envelope off GRAPH_STATUS,
// returning the raw bytes too so wire-level absence can be asserted (omitempty is
// what keeps a field off the wire, so decoding into the struct cannot prove it).
func readEnvelope(ctx context.Context, t *testing.T, tc *natsclient.TestClient) (graph.IndexStatusResponse, []byte) {
	t.Helper()
	status, raw, ok := tryReadEnvelope(ctx, t, tc)
	require.True(t, ok, "graph-ingest readiness key not published yet; "+
		"use tryReadEnvelope from a polling loop")
	return status, raw
}

// tryReadEnvelope is readEnvelope for POLLING callers: it reports a
// not-yet-published key as ok=false instead of failing the test.
//
// A poll loop that tolerates "not the state I want yet" but not "the key does not
// exist yet" fails on its own first iteration, because the key appears on the
// producer's first status tick and the loop starts before it. That is what broke
// TestIntegration_ReadinessEnvelope_BacklogIsNotReady in CI: component start, then
// a read 0.56s later, then `nats: key not found` straight into require.NoError.
//
// Absent is not the same as negative — which is the distinction this very
// capability exists to enforce, so the test helper had better honour it too. Every
// OTHER bucket or decode error still fails hard: a missing key during startup is
// expected, a broken bucket is not.
func tryReadEnvelope(
	ctx context.Context, t *testing.T, tc *natsclient.TestClient,
) (graph.IndexStatusResponse, []byte, bool) {
	t.Helper()
	bucket, err := tc.Client.GetKeyValueBucket(ctx, readiness.BucketGraphStatus)
	require.NoError(t, err, "GRAPH_STATUS bucket")

	entry, err := bucket.Get(ctx, readiness.KeyGraphIngest)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return graph.IndexStatusResponse{}, nil, false
	}
	require.NoError(t, err, "graph-ingest readiness key")

	var status graph.IndexStatusResponse
	require.NoError(t, json.Unmarshal(entry.Value(), &status))
	return status, entry.Value(), true
}

func startIngestForReadiness(ctx context.Context, t *testing.T) (*natsclient.TestClient, *Component) {
	t.Helper()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	cfg := DefaultConfig()
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, testDependencies(t, tc.Client))
	require.NoError(t, err)

	c := comp.(*Component)
	// Tick fast so the test observes successive heartbeats without sleeping through
	// production cadence.
	c.statusInterval = 200 * time.Millisecond
	require.NoError(t, c.Initialize())
	// Register the test payload decoder BEFORE Start. Without it every published
	// message fails decode and is ack-DROPPED as poison — the backlog "drains"
	// without one entity reaching ENTITY_STATES, and a readiness assertion passes
	// vacuously.
	registerMergeTestPayload(t, c)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return tc, c
}

// publishEntity publishes an entity graph-ingest can actually DECODE.
//
// It wraps a Graphable in a BaseMessage, because that is what the consume closure's
// decodeEntity expects. Publishing a bare graph.EntityState — which an earlier version
// of this helper did — is not decodable: every message is ack-DROPPED as poison, so the
// backlog "drains" without a single entity ever reaching ENTITY_STATES. That made a
// readiness test pass vacuously (caught-up was true because everything was discarded),
// which is exactly the "a green new gate may have skipped everything" failure. The
// durability test below is what caught it.
func publishEntity(ctx context.Context, t *testing.T, tc *natsclient.TestClient, id string) {
	t.Helper()
	payload := &mergeTestGraphable{
		entityID: id,
		triples: []message.Triple{{
			Subject:    id,
			Predicate:  "core.identity.type",
			Object:     "drone",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		}},
	}
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "readiness-test"))
	require.NoError(t, err)
	_, err = tc.Client.PublishToStreamWithAck(ctx, "entity."+id, data)
	require.NoError(t, err)
}

// TestIntegration_ReadinessEnvelope_PublishedAndCatchesUp drives the whole producer:
// a real component, real consumers, a real GRAPH_STATUS bucket. It asserts the
// envelope reaches Ready with bootstrap complete on an idle stack — the gh#712 signal
// that did not exist.
func TestIntegration_ReadinessEnvelope_PublishedAndCatchesUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tc, _ := startIngestForReadiness(ctx, t)

	require.Eventually(t, func() bool {
		status, _, ok := tryReadEnvelope(ctx, t, tc)
		if !ok {
			return false
		}
		return status.Ready && status.BootstrapComplete && status.State == graph.IndexStateReady
	}, 30*time.Second, 200*time.Millisecond,
		"an idle stack must publish a caught-up envelope")

	status, raw := readEnvelope(ctx, t, tc)
	require.Zero(t, status.Lag, "idle stack reports no backlog")

	// Task 3.6 asserted on the WIRE: a stream sequence in a KV-revision field would
	// silently corrupt every read-your-writes check in the system, and omitempty is
	// what keeps these off the wire.
	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))
	for _, banned := range []string{"indexed_revision", "target_revision", "revision"} {
		require.NotContains(t, wire, banned,
			"a backlog producer must not publish %s: %s", banned, raw)
	}
}

// TestIntegration_ReadinessEnvelope_BacklogIsNotReady is the gh#712 case itself: while
// entities are still being applied, the envelope must NOT read caught-up. Before this
// change a consumer could only see total_entities > 0 plus green health, which is
// exactly what read as settled while ingest was mid-flight.
func TestIntegration_ReadinessEnvelope_BacklogIsNotReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	// Publish BEFORE the component starts, so its consumer binds onto a real
	// backlog. DeliverPolicy "all" means it must catch up on all of it.
	const backlog = 200
	for i := 0; i < backlog; i++ {
		publishEntity(ctx, t, tc, "c360.test.readiness.backlog.drone."+string(rune('a'+i%26))+itoa(i))
	}

	cfg := DefaultConfig()
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, testDependencies(t, tc.Client))
	require.NoError(t, err)
	c := comp.(*Component)
	c.statusInterval = 100 * time.Millisecond
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c)
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(context.Background()) }()

	// FIRST assert the not-ready window this test is named for. Without it the test
	// only proved eventual readiness plus a non-zero scope — the gh#732 case — while
	// its name promised gh#712's "nonempty and healthy read as settled". A backlog
	// of 200 pre-bind messages cannot be drained instantly, so an envelope reporting
	// caught-up before any work was applied is the defect this must catch.
	sawNotReady := false
	notReadyDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(notReadyDeadline) {
		status, _, ok := tryReadEnvelope(ctx, t, tc)
		if !ok {
			// Key not published yet — the producer's first tick has not landed.
			// This is THE iteration that failed in CI: unpublished is not
			// not-ready, it is not-yet-observed, so keep polling.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if status.Lag > 0 && !status.Ready {
			sawNotReady = true
			break
		}
		if status.Ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, sawNotReady,
		"producer never reported outstanding work while draining a %d-message backlog", backlog)

	// ...and it must eventually catch up.
	require.Eventually(t, func() bool {
		status, _, ok := tryReadEnvelope(ctx, t, tc)
		if !ok {
			return false
		}
		return status.Ready && status.BootstrapComplete
	}, 60*time.Second, 100*time.Millisecond,
		"the producer must reach caught-up after draining the backlog")

	// ...and having caught up, its scope must record that there WAS an initial build.
	// This is the distinction gh#732 raises: complete && scope == 0 means
	// "authoritatively nothing to do", so a non-empty boot backlog must not report 0.
	status, _ := readEnvelope(ctx, t, tc)
	require.Positive(t, status.BootstrapScope,
		"a producer that bound onto a %d-message backlog must not report scope 0", backlog)
}

// TestIntegration_ReadinessEnvelope_NoStreamingPortIsHonestlyCaughtUp covers the
// mutation-only deployment shape. It is NOT an edge case: measured 2026-07-30, 9 of
// the 18 shipped graph-ingest instances declare their only input port as type "nats"
// (core NATS request/reply for graph_mutations), which setupSubscriptions skips — so
// they bind zero jetstream consumers and have no backlog to be behind on.
//
// Reporting degraded here would make half the shipped fleet permanently unready and
// every consumer folding on this key defer forever. The port shape below is copied
// from configs/flows/ops-agent.json rather than invented, so the test fails if that
// shape stops being reachable. (An ABSENT input list cannot be used: Config.Validate
// requires at least one input port.)
func TestIntegration_ReadinessEnvelope_NoStreamingPortIsHonestlyCaughtUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())

	cfg := DefaultConfig()
	cfg.Ports.Inputs = cfg.Ports.Inputs[1:]
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, testDependencies(t, tc.Client))
	require.NoError(t, err)
	c := comp.(*Component)
	c.statusInterval = 100 * time.Millisecond
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c)
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(context.Background()) }()

	require.Eventually(t, func() bool {
		status, _, ok := tryReadEnvelope(ctx, t, tc)
		if !ok {
			return false
		}
		return status.Ready && status.BootstrapComplete
	}, 20*time.Second, 100*time.Millisecond,
		"a deployment with no streaming input must be honestly caught up")

	status, _ := readEnvelope(ctx, t, tc)
	require.Zero(t, status.BootstrapScope,
		"nothing to do must report scope 0 — that is what makes it distinguishable")
	require.Equal(t, graph.IndexStateReady, status.State)
}

// itoa avoids pulling strconv in for one call site in a test helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestIntegration_ReadyImpliesTheWritesAreDurable is task 8.1: the durability
// guarantee, asserted on the quantity production actually consults.
//
// The envelope's soundness rests on acknowledgement being the TERMINAL step of the
// ingest success path — the graph write, its derived writes, and the durable guard
// stamp all complete before Ack, and every failure path Naks or Terms without acking.
// The observable consequence is this: at the instant the producer first reports
// caught-up, every published entity must already be readable in ENTITY_STATES.
//
// DEVIATION FROM THE TASK TEXT, with its reason MEASURED. Task 8.1 asked to "force a
// write failure and assert NumPending + NumAckPending stays > 0 while it fails". That
// assumes write failures are RETRYABLE. They are mostly not: probed 2026-07-30 with
// poisoned resident state and with the RMW target deleted, both produce TERMINAL
// dispositions (Term), so the message leaves both counters within milliseconds and
// outstanding work correctly returns to zero. Asserting "stays > 0" would assert
// something false about this component. It also observes the documented honesty
// boundary live: lag == 0 means no outstanding work, NOT that every message was
// applied — a terminated message is simply gone (gh#742 owns that visibility).
//
// IT DELIBERATELY DOES NOT ASSERT ON THE ACK FLOOR. §D0 measured it unusable, nothing
// here reads it, and it would pass for the wrong reason under MaxDeliver exhaustion.
func TestIntegration_ReadyImpliesTheWritesAreDurable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	defer func() { _ = tc.Terminate() }()

	// Publish BEFORE start so the consumer binds onto a real backlog: a single
	// message is applied faster than any status tick can sample, so only a backlog
	// makes the not-ready window observable at all.
	const published = 150
	ids := make([]string, 0, published)
	for i := 0; i < published; i++ {
		id := "c360.test.durable.sensor.unit." + itoa(i)
		ids = append(ids, id)
		publishEntity(ctx, t, tc, id)
	}

	cfg := DefaultConfig()
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, testDependencies(t, tc.Client))
	require.NoError(t, err)
	c := comp.(*Component)
	c.statusInterval = 50 * time.Millisecond
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c)
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(context.Background()) }()

	bucket, err := tc.GetKVBucket(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		status, _, ok := tryReadEnvelope(ctx, t, tc)
		if !ok {
			return false
		}
		return status.Ready && status.BootstrapComplete
	}, 60*time.Second, 50*time.Millisecond, "producer must reach caught-up")

	// THE ASSERTION: caught-up was reported, so every write must already be durable.
	// If ack ran before the write, or before a derived write, some of these are absent.
	missing := 0
	for _, id := range ids {
		if _, gerr := bucket.Get(ctx, id); gerr != nil {
			missing++
		}
	}
	require.Zero(t, missing,
		"producer reported caught-up with %d of %d entities not yet durable — "+
			"acknowledgement is not the terminal step of the success path", missing, published)
}

// TestIntegration_ReadinessEnvelope_AbsentKeyIsNotAFailure pins the branch that
// broke CI, deterministically rather than by hoping the startup race recurs.
//
// The original helper called require.NoError on bucket.Get, so a key that had not
// been published yet failed the test outright — and every polling loop in this file
// called it on its first iteration, before the producer's first status tick. The
// class was five loops wide, not one.
//
// Deleting the key after it exists reproduces the same sentinel branch on demand:
// tryReadEnvelope must report not-observed, and readEnvelope must still fail hard,
// because absent is a legitimate answer only to a poller.
func TestIntegration_ReadinessEnvelope_AbsentKeyIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tc, comp := startIngestForReadiness(ctx, t)

	require.Eventually(t, func() bool {
		_, _, ok := tryReadEnvelope(ctx, t, tc)
		return ok
	}, 30*time.Second, 100*time.Millisecond, "the producer should publish its key")
	// Fence the producer before the fixture-admin purge. Otherwise the 200ms
	// heartbeat can legitimately republish between Purge and Get, making this a
	// scheduler race rather than a test of the absent-key decoder branch.
	require.NoError(t, comp.Stop(ctx))

	bucket, err := tc.Client.GetKeyValueBucket(ctx, readiness.BucketGraphStatus)
	require.NoError(t, err)
	require.NoError(t, bucket.Purge(ctx, readiness.KeyGraphIngest))

	// The key is now absent. A poller must be told "not observed", not failed.
	_, _, ok := tryReadEnvelope(ctx, t, tc)
	require.False(t, ok,
		"an absent readiness key must report not-observed to a polling caller; "+
			"failing here is what broke TestIntegration_ReadinessEnvelope_BacklogIsNotReady in CI")
}
