//go:build integration

// Review H1: a no-op graph mutation must not let the rule engine claim another
// writer's KV revision as its own.
//
// handleTripleAdd / handleTripleRemove report the entity's LIVE revision after
// the call. When the mutation was suppressed (an identical tuple already
// stored) or was a no-op removal (no predicate matched), that revision was
// produced by SOMEONE ELSE. trackRuleRevision records it against this rule, and
// shouldSkipRule then consumes it exactly once and returns true — so the rule's
// watcher silently DROPS the genuine external change that produced it.
//
// This drives the full production wire: the real tripleMutator over real NATS
// into a real graph-ingest component, with the real Processor as the tracker.

package rule

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
)

const revisionClaimEntityID = "c360.platform.robotics.mav1.drone.revclaim1"

type revisionClaimHarness struct {
	ctx     context.Context
	ingest  *graphingest.Component
	mutator TripleMutator
	tracker *Processor
	bucket  *natsclient.KVStore
}

func newRevisionClaimHarness(t *testing.T) *revisionClaimHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	testClient := natsclient.NewTestClient(
		t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}),
	)

	rawConfig, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(
		rawConfig,
		component.Dependencies{NATSClient: testClient.Client},
	)
	require.NoError(t, err)
	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(ctx))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	t.Cleanup(func() {
		_ = ingest.Stop(5 * time.Second)
		cancel()
	})

	bucket, err := graph.EnsureCatalogBucket(ctx, testClient.Client, graph.BucketEntityStates)
	require.NoError(t, err)

	tracker := &Processor{ownRevisions: make(map[ruleRevKey]map[uint64]time.Time)}
	return &revisionClaimHarness{
		ctx:     ctx,
		ingest:  ingest,
		mutator: newTripleMutator(testClient.Client, tracker),
		tracker: tracker,
		bucket:  testClient.Client.NewKVStore(bucket),
	}
}

func (h *revisionClaimHarness) liveRevision(t *testing.T) uint64 {
	t.Helper()
	entry, err := h.bucket.Get(h.ctx, revisionClaimEntityID)
	require.NoError(t, err)
	return entry.Revision
}

// ruleTriple is what actions.go builds for add_triple: Source "rule_engine",
// empty Context, and only Timestamp varying between assertions — all three of
// which are EXCLUDED from add-lane identity, so every re-assertion of the same
// (subject, predicate, object) is a suppression.
func ruleTriple(object string) message.Triple {
	return message.Triple{
		Subject:    revisionClaimEntityID,
		Predicate:  "rule.state.phase",
		Object:     object,
		Source:     "rule_engine",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}
}

func (h *revisionClaimHarness) seedEntity(t *testing.T) {
	t.Helper()
	require.NoError(t, h.ingest.CreateEntity(h.ctx, &graph.EntityState{
		ID: revisionClaimEntityID,
		Triples: []message.Triple{{
			Subject:   revisionClaimEntityID,
			Predicate: "entity.type.class",
			Object:    "robotics.drone",
			Timestamp: time.Now(),
		}},
		Version:   1,
		UpdatedAt: time.Now(),
	}))
}

// externalWrite advances the entity's revision from a writer that is NOT the
// rule under test — the change the rule's watcher must still see.
func (h *revisionClaimHarness) externalWrite(t *testing.T, object string) uint64 {
	t.Helper()
	require.NoError(t, h.ingest.AddTriple(h.ctx, message.Triple{
		Subject:    revisionClaimEntityID,
		Predicate:  "external.writer.mark",
		Object:     object,
		Source:     "some-other-component",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}))
	return h.liveRevision(t)
}

func TestTripleMutator_SuppressedAddDoesNotClaimAnotherWritersRevision(t *testing.T) {
	h := newRevisionClaimHarness(t)
	h.seedEntity(t)

	const ruleID = "rule-a"
	triple := ruleTriple("armed")

	// A real write by this rule IS this rule's revision, and must be tracked.
	firstRev, err := h.mutator.AddTriple(h.ctx, ruleID, triple)
	require.NoError(t, err)
	require.Positive(t, firstRev)
	require.True(t, h.tracker.shouldSkipRule(ruleID, revisionClaimEntityID, firstRev),
		"a revision this rule genuinely produced must be tracked")

	// Someone else writes. This is the change the rule's watcher must see.
	externalRev := h.externalWrite(t, "beacon-1")
	require.Greater(t, externalRev, firstRev, "the external write must move the revision")

	// The rule re-asserts the SAME triple. The add lane suppresses it, so
	// nothing is committed — but the handler still reports the live revision,
	// which now belongs to the external writer.
	secondRev, err := h.mutator.AddTriple(h.ctx, ruleID, ruleTriple("armed"))
	require.NoError(t, err, "a suppressed add is a success")
	require.Equal(t, externalRev, secondRev,
		"fixture sanity: the suppressed add reports the EXTERNAL writer's live revision")

	require.False(t, h.tracker.shouldSkipRule(ruleID, revisionClaimEntityID, externalRev),
		"a suppressed add committed nothing, so claiming the external writer's revision "+
			"makes the rule drop a genuine external change")
}

func TestTripleMutator_NoOpRemoveDoesNotClaimAnotherWritersRevision(t *testing.T) {
	h := newRevisionClaimHarness(t)
	h.seedEntity(t)

	const ruleID = "rule-b"
	const predicate = "rule.state.phase"
	_, err := h.mutator.AddTriple(h.ctx, "", ruleTriple("armed"))
	require.NoError(t, err)

	// A removal that actually removes something IS this rule's revision.
	firstRev, err := h.mutator.RemoveTriple(h.ctx, ruleID, revisionClaimEntityID, predicate)
	require.NoError(t, err)
	require.Positive(t, firstRev)
	require.True(t, h.tracker.shouldSkipRule(ruleID, revisionClaimEntityID, firstRev),
		"a revision this rule genuinely produced must be tracked")

	externalRev := h.externalWrite(t, "beacon-2")
	require.Greater(t, externalRev, firstRev, "the external write must move the revision")

	// Nothing matches the predicate any more, so this removal is a no-op.
	secondRev, err := h.mutator.RemoveTriple(h.ctx, ruleID, revisionClaimEntityID, predicate)
	require.NoError(t, err, "a no-op remove is a success")
	require.Equal(t, externalRev, secondRev,
		"fixture sanity: the no-op remove reports the EXTERNAL writer's live revision")

	require.False(t, h.tracker.shouldSkipRule(ruleID, revisionClaimEntityID, externalRev),
		"a no-op remove committed nothing, so claiming the external writer's revision "+
			"makes the rule drop a genuine external change")
}
