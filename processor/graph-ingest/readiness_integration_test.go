//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// readEnvelope reads the published graph-ingest readiness envelope off GRAPH_STATUS,
// returning the raw bytes too so wire-level absence can be asserted (omitempty is
// what keeps a field off the wire, so decoding into the struct cannot prove it).
func readEnvelope(ctx context.Context, t *testing.T, tc *natsclient.TestClient) (graph.IndexStatusResponse, []byte) {
	t.Helper()
	bucket, err := tc.Client.GetKeyValueBucket(ctx, readiness.BucketGraphStatus)
	require.NoError(t, err, "GRAPH_STATUS bucket")
	entry, err := bucket.Get(ctx, readiness.KeyGraphIngest)
	require.NoError(t, err, "graph-ingest readiness key")

	var status graph.IndexStatusResponse
	require.NoError(t, json.Unmarshal(entry.Value(), &status))
	return status, entry.Value()
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

	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)

	c := comp.(*Component)
	// Tick fast so the test observes successive heartbeats without sleeping through
	// production cadence.
	c.statusInterval = 200 * time.Millisecond
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })
	return tc, c
}

func publishEntity(ctx context.Context, t *testing.T, tc *natsclient.TestClient, id string) {
	t.Helper()
	entity := &graph.EntityState{
		ID: id,
		Triples: []message.Triple{{
			Subject:    id,
			Predicate:  "core.identity.type",
			Object:     "drone",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		}},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	payload, err := json.Marshal(entity)
	require.NoError(t, err)
	_, err = tc.Client.PublishToStreamWithAck(ctx, "entity."+id, payload)
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
		status, _ := readEnvelope(ctx, t, tc)
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
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	c.statusInterval = 100 * time.Millisecond
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()

	// It must eventually catch up...
	require.Eventually(t, func() bool {
		status, _ := readEnvelope(ctx, t, tc)
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
	cfg.Ports.Inputs = []component.PortDefinition{{
		Name:    "graph_mutations",
		Type:    "nats",
		Subject: "graph.mutation.>",
	}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	c.statusInterval = 100 * time.Millisecond
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()

	require.Eventually(t, func() bool {
		status, _ := readEnvelope(ctx, t, tc)
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
