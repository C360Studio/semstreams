//go:build integration

// Integration tests for MergeEntity — the merge-not-replace semantics
// the JetStream consumer path uses (handleMessage) to ingest
// Graphable arrivals without clobbering pre-existing triples written
// via the atomic mutation handlers (create_with_triples,
// update_with_triples, triple.add).
//
// gh#177 regression coverage: the canonical failure mode the issue
// captures is "Manager.Create stamps phase triple; subsequent
// mission-command Graphable arrives via jetstream; phase triple
// vanishes." TestIntegration_MergeEntity_HandleMessageDoesNotClobber
// pins that exact case.

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	payloadbuiltins "github.com/c360studio/semstreams/payloadbuiltins"
	payloadregistry "github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSeedEntity(id string, triples ...message.Triple) *graph.EntityState {
	now := time.Now()
	return &graph.EntityState{
		ID:          id,
		Triples:     triples,
		MessageType: message.Type{Domain: "test", Category: "seed", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}
}

// TestIntegration_MergeEntity_FirstWriteCreatesAtomically pins the
// fresh-entity case: MergeEntity on an absent ID writes the entity
// verbatim, like CreateEntity but without the upsert footgun.
func TestIntegration_MergeEntity_FirstWriteCreatesAtomically(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.merge.firstwrite.entity.001"
	now := time.Now()
	entity := newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "test.merge.kind", Object: "alpha", Timestamp: now, Confidence: 1.0},
	)

	require.NoError(t, c.MergeEntity(ctx, entity))

	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 1, nonProfileTripleCount(stored), "ADR-054 profile stamp excluded from the seed-triple count")
	assert.Equal(t, "test.merge.kind", stored.Triples[0].Predicate)
	assert.Equal(t, "alpha", stored.Triples[0].Object)
}

// TestIntegration_MergeEntity_SecondWriteMergesTriples pins the load-
// bearing fix: when an entity already exists, MergeEntity MERGES the new
// triples predicate-level (gh#466 — replace per (subject,predicate),
// preserving non-conflicting existing triples). Pre-fix code used Put
// (full-replace) and would have wiped the first set of triples. This test
// uses DISTINCT predicates across the two arrivals, so both survive under
// the merge; see TestIntegration_MergeEntity_SamePredicateReplaces for the
// same-predicate de-duplication.
func TestIntegration_MergeEntity_SecondWriteMergesTriples(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.merge.secondwrite.entity.001"
	now := time.Now()

	// First write: phase triple (simulates Manager.Create's stamp).
	first := newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "mission.state.phase", Object: "planning", Timestamp: now, Confidence: 1.0},
	)
	require.NoError(t, c.MergeEntity(ctx, first))

	// Second write: command triple (simulates a mission-command Graphable arrival).
	second := newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "mission.command.requested", Object: "launch", Timestamp: now, Confidence: 1.0},
	)
	second.MessageType = message.Type{Domain: "mission", Category: "command", Version: "v1"}
	require.NoError(t, c.MergeEntity(ctx, second))

	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, stored)

	// Both triples must be present — pre-fix this assertion failed
	// because the second MergeEntity (then CreateEntity → Put) wiped
	// mission.state.phase.
	predicates := make(map[string]any, len(stored.Triples))
	for _, tr := range stored.Triples {
		predicates[tr.Predicate] = tr.Object
	}
	assert.Equal(t, "planning", predicates["mission.state.phase"], "phase triple must survive the second arrival (gh#177)")
	assert.Equal(t, "launch", predicates["mission.command.requested"], "command triple must be present after the second arrival")

	// Metadata: latest-wins on MessageType, monotonic Version.
	assert.Equal(t, "mission", stored.MessageType.Domain)
	assert.Equal(t, "command", stored.MessageType.Category)
	assert.Equal(t, uint64(2), stored.Version, "Version should bump monotonically")
}

// TestIntegration_MergeEntity_SamePredicateReplaces is the gh#466 regression:
// republishing the same (subject, predicate) must REPLACE, not accumulate. A
// boid publishing position snapshots would otherwise grow flock.position.x
// without bound.
func TestIntegration_MergeEntity_SamePredicateReplaces(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.merge.samepred.entity.001"
	now := time.Now()

	for _, v := range []string{"1", "2", "3"} {
		require.NoError(t, c.MergeEntity(ctx, newSeedEntity(entityID,
			message.Triple{Subject: entityID, Predicate: "flock.position.x", Object: v, Timestamp: now, Confidence: 1.0})))
	}

	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)

	count := 0
	for _, tr := range stored.Triples {
		if tr.Predicate == "flock.position.x" {
			count++
		}
	}
	assert.Equal(t, 1, count, "gh#466: same-predicate re-arrivals must not accumulate duplicate triples")

	// Reader sees the newest value (MergeTriples puts newer first; GetPropertyValue
	// returns first-match — pre-fix append served the STALE first-written value).
	v, ok := graph.GetPropertyValue(stored, "flock.position.x")
	require.True(t, ok)
	assert.Equal(t, "3", v, "newest value wins after merge")
}

// TestIntegration_MergeEntity_MultiValuedPredicateFullSetReplace pins the
// full-set-replace contract for a multi-valued relationship predicate: a new
// flock.relation.neighbor set replaces the prior set (not a union). Producers own
// publishing the complete set per arrival (gh#466 design).
func TestIntegration_MergeEntity_MultiValuedPredicateFullSetReplace(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.merge.multivalue.entity.001"
	now := time.Now()

	// First arrival: neighbor set {b, c}.
	require.NoError(t, c.MergeEntity(ctx, newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "flock.relation.neighbor", Object: "b", Timestamp: now, Confidence: 1.0},
		message.Triple{Subject: entityID, Predicate: "flock.relation.neighbor", Object: "c", Timestamp: now, Confidence: 1.0})))
	// Second arrival: neighbor set {c, d} — must fully replace the prior set.
	require.NoError(t, c.MergeEntity(ctx, newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "flock.relation.neighbor", Object: "c", Timestamp: now, Confidence: 1.0},
		message.Triple{Subject: entityID, Predicate: "flock.relation.neighbor", Object: "d", Timestamp: now, Confidence: 1.0})))

	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)

	neighbors := map[any]bool{}
	for _, tr := range stored.Triples {
		if tr.Predicate == "flock.relation.neighbor" {
			neighbors[tr.Object] = true
		}
	}
	assert.Equal(t, map[any]bool{"c": true, "d": true}, neighbors,
		"full-set replace: the new {c,d} set replaces prior {b,c}; the prior-only member b is gone")
}

// TestIntegration_HandleMessage_DoesNotClobber is the gh#177
// regression test driving through the actual jetstream-consumer entry
// point. Seeds the entity via the atomic mutation handler shape
// (analogous to Manager.Create), then drives a Graphable through
// handleMessage and asserts the seeded triple survives.
//
// Why this test: TestIntegration_MergeEntity_SecondWriteMergesTriples
// proves the helper merges; this test proves the production wire
// actually calls the helper. The bug was specifically that the wire
// called the wrong helper.
func TestIntegration_HandleMessage_DoesNotClobber(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const entityID = "c360.test.handlemsg.gh177.entity.001"
	now := time.Now()

	// Step 1: seed the entity through the atomic mutation handler
	// path — same shape Manager.Create produces.
	seed := newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "mission.state.phase", Object: "planning", Timestamp: now, Confidence: 1.0},
	)
	require.NoError(t, c.CreateEntityStrict(ctx, seed))

	// Step 2: build a BaseMessage carrying a Graphable that stamps
	// one extra triple (mission.command.requested=launch). This is the shape
	// the mission-command processor publishes to mission.processed.entity.
	graphablePayload := &mergeTestGraphable{
		entityID: entityID,
		triples: []message.Triple{
			{Subject: entityID, Predicate: "mission.command.requested", Object: "launch", Timestamp: now, Confidence: 1.0},
		},
	}
	registerMergeTestPayload(t, c)
	baseMsg := message.NewBaseMessage(graphablePayload.Schema(), graphablePayload, "test-source")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	// Step 3: drive the message through handleMessage — the exact
	// production entry point the JetStream consumer uses.
	c.handleMessage(ctx, "test.subject", data)

	// Step 4: assert both triples are present. Pre-fix, only the
	// second arrival's triple survived; the seeded mission.state.phase
	// vanished. Post-fix, MergeEntity preserves both.
	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	predicates := make(map[string]any, len(stored.Triples))
	for _, tr := range stored.Triples {
		predicates[tr.Predicate] = tr.Object
	}
	assert.Equal(t, "planning", predicates["mission.state.phase"],
		"seeded phase triple must survive a subsequent jetstream-consumer arrival (gh#177)")
	assert.Equal(t, "launch", predicates["mission.command.requested"],
		"new Graphable's triple must also be present")
}

// TestIntegration_MergeEntity_HierarchyDoesNotDuplicate is the
// targeted regression test for the go-reviewer concern 5 finding on
// gh#177's first cut: hierarchy inference is deterministic per
// entityID; running it on every merge would APPEND the same edges
// repeatedly. The fix gates hierarchy fetch to the first-write arm
// (probed via a KV existence check before the CAS callback, with the
// callback's len(current) == 0 branch as the actual gate).
//
// Setup: spin up a component with EnableHierarchy=true; call
// MergeEntity twice with the same entity ID; assert hierarchy
// triples appear exactly once in the final merged state.
func TestIntegration_MergeEntity_HierarchyDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	cfg := DefaultConfig()
	cfg.EnableHierarchy = true
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(cfgJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	time.Sleep(100 * time.Millisecond)

	const entityID = "c360.test.merge.hier.entity.001"
	now := time.Now()

	// First merge — entity is fresh; hierarchy inference runs and stamps edges.
	first := newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "robotics.state.status", Object: "armed", Timestamp: now, Confidence: 1.0},
	)
	require.NoError(t, c.MergeEntity(ctx, first))

	stored, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)
	tripleCountAfterFirst := len(stored.Triples)
	require.Greater(t, tripleCountAfterFirst, 1,
		"after first merge, expected hierarchy triples to land (got %d total)", tripleCountAfterFirst)

	// Snapshot hierarchy triples (anything not the caller-supplied robotics.state.status).
	hierarchyTripleCount := 0
	for _, tr := range stored.Triples {
		if tr.Predicate != "robotics.state.status" {
			hierarchyTripleCount++
		}
	}
	require.Greater(t, hierarchyTripleCount, 0, "hierarchy edges must be stamped on first merge")

	// Second merge — same entity ID, one new caller-supplied triple
	// (distinct predicate). Pre-fix the hierarchy block ran again, doubling
	// the hierarchy triple count; the fix skips re-applying hierarchy and
	// merges predicate-level, so the count holds.
	second := newSeedEntity(entityID,
		message.Triple{Subject: entityID, Predicate: "robotics.command.requested", Object: "land", Timestamp: now, Confidence: 1.0},
	)
	require.NoError(t, c.MergeEntity(ctx, second))

	stored2, _, err := c.fetchEntityState(ctx, entityID)
	require.NoError(t, err)

	// Final state: first-call payload + hierarchy + second-call payload.
	// Hierarchy count must MATCH the snapshot, not double.
	hierarchyAfterSecond := 0
	for _, tr := range stored2.Triples {
		if tr.Predicate != "robotics.state.status" && tr.Predicate != "robotics.command.requested" {
			hierarchyAfterSecond++
		}
	}
	assert.Equal(t, hierarchyTripleCount, hierarchyAfterSecond,
		"hierarchy triples must NOT duplicate on subsequent merges (gh#177 reviewer concern 5)")
}

// mergeTestGraphable is a minimal Graphable payload that stamps a
// caller-supplied triple set on an entity ID. Used by the
// HandleMessage regression test above to exercise the merge wire.
type mergeTestGraphable struct {
	entityID string
	triples  []message.Triple
}

func (g *mergeTestGraphable) EntityID() string          { return g.entityID }
func (g *mergeTestGraphable) Triples() []message.Triple { return g.triples }
func (g *mergeTestGraphable) Schema() message.Type {
	return message.Type{Domain: "test", Category: "merge", Version: "v1"}
}

func (g *mergeTestGraphable) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		EntityID string           `json:"entity_id"`
		Triples  []message.Triple `json:"triples"`
	}{g.entityID, g.triples})
}

func (g *mergeTestGraphable) UnmarshalJSON(data []byte) error {
	var v struct {
		EntityID string           `json:"entity_id"`
		Triples  []message.Triple `json:"triples"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	g.entityID = v.EntityID
	g.triples = v.Triples
	return nil
}

func (g *mergeTestGraphable) Validate() error { return nil }

func registerMergeTestPayload(t *testing.T, c *Component) {
	t.Helper()
	reg := payloadregistry.New()
	require.NoError(t, payloadbuiltins.Register(reg))
	require.NoError(t, reg.Register(&payloadregistry.Registration{
		Domain:      "test",
		Category:    "merge",
		Version:     "v1",
		Description: "merge-entity integration-test payload",
		Factory:     func() any { return &mergeTestGraphable{} },
	}))
	c.decoder = message.NewDecoder(reg)
}
