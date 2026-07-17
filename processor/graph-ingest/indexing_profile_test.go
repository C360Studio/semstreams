package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-054 Phase 1: graph-ingest stamps a single-valued entity.indexing.profile
// triple at every entity-CREATION seam (create_with_triples, Graphable arrival,
// stub→real upgrade), resolving via precedence envelope > Graphable
// IndexingProfiler > fallback floor (default control). It is NEVER stamped on a
// plain update (immutable after create) except via the explicit envelope
// override on update_with_triples. These tests drive the production mutation
// handlers and create seams against the mock KV bucket.

const testProfileEntityID = "c360.platform.test.sys.widget.001"

func testWidgetMessageType() message.Type {
	return message.Type{Domain: "test", Category: "widget", Version: "v1"}
}

func storedEntity(t *testing.T, comp *Component, id string) *graph.EntityState {
	t.Helper()
	entry, err := comp.entityBucket.Get(context.Background(), id)
	require.NoError(t, err, "entity %s should be stored", id)
	var es graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &es))
	return &es
}

// nonProfileTripleCount counts an entity's triples EXCLUDING the framework-
// stamped entity.indexing.profile (ADR-054). Count-based assertions that
// predate the stamp use it so they stay robust to the reserved triple instead
// of hard-coding "+1". Shared with the integration tests (same package).
func nonProfileTripleCount(es *graph.EntityState) int {
	n := 0
	for _, tr := range es.Triples {
		if tr.Predicate != vocabulary.EntityIndexingProfile {
			n++
		}
	}
	return n
}

// profileValues returns every entity.indexing.profile object on the entity, so
// tests can assert the single-valued invariant (len must be exactly 1).
func profileValues(es *graph.EntityState) []string {
	var out []string
	for _, tr := range es.Triples {
		if tr.Predicate == vocabulary.EntityIndexingProfile {
			if s, ok := tr.Object.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// --- create_with_triples (production handler) ---

func TestIndexingProfile_CreateWithTriples_DefaultsToControlFloor(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	req := graph.CreateEntityWithTriplesRequest{
		Entity:  &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()},
		Triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "robotics", "status", "armed"), Object: true, Timestamp: time.Now()}},
	}
	data, _ := json.Marshal(req)

	respData, err := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"undeclared entity must default to exactly one control-floor profile")
	assert.Equal(t, 1, nonProfileTripleCount(es), "the stamp must not displace the user triple")
}

func TestIndexingProfile_CreateWithTriples_EnvelopeProfileWins(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	req := graph.CreateEntityWithTriplesRequest{
		Entity:          &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()},
		Triples:         []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "doc", "body", "text"), Object: "hello", Timestamp: time.Now()}},
		IndexingProfile: vocabulary.IndexingProfileContent,
	}
	data, _ := json.Marshal(req)

	respData, err := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileContent}, profileValues(es),
		"explicit envelope profile must win over the floor")
}

func TestIndexingProfile_CreateWithTriples_InvalidEnvelopeFallsToFloor(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	req := graph.CreateEntityWithTriplesRequest{
		Entity:          &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()},
		IndexingProfile: "not-a-real-profile",
	}
	data, _ := json.Marshal(req)

	respData, err := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"an invalid envelope profile is treated as absent and falls to the floor")
}

// --- update_with_triples explicit override ---

func TestIndexingProfile_UpdateOverride_ReplacesProfile(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Create with the control floor.
	createReq := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()},
	}
	createData, _ := json.Marshal(createReq)
	_, err := comp.handleEntityCreateWithTriples(ctx, createData)
	require.NoError(t, err)
	require.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(storedEntity(t, comp, testProfileEntityID)))

	// Override to content via the explicit envelope channel.
	updateReq := graph.UpdateEntityWithTriplesRequest{
		Entity:          &graph.EntityState{ID: testProfileEntityID},
		IndexingProfile: vocabulary.IndexingProfileContent,
	}
	updateData, _ := json.Marshal(updateReq)
	respData, err := comp.handleEntityUpdateWithTriples(ctx, updateData)
	require.NoError(t, err)
	var resp graph.MutationResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileContent}, profileValues(es),
		"explicit override must REPLACE the profile (single-valued, no accumulation)")
}

func TestIndexingProfile_UpdateOverride_InvalidRejected(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	createReq := graph.CreateEntityWithTriplesRequest{Entity: &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()}}
	createData, _ := json.Marshal(createReq)
	_, err := comp.handleEntityCreateWithTriples(ctx, createData)
	require.NoError(t, err)

	updateReq := graph.UpdateEntityWithTriplesRequest{
		Entity:          &graph.EntityState{ID: testProfileEntityID},
		IndexingProfile: "bogus",
	}
	updateData, _ := json.Marshal(updateReq)
	// ADR-060: an invalid explicit override is rejected as a typed error
	// (invalid_request), not an in-body Success=false.
	respData, err := comp.handleEntityUpdateWithTriples(ctx, updateData)
	require.Error(t, err, "an invalid explicit override must be rejected (not silently dropped)")
	assert.Nil(t, respData, "a hard failure returns no body")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeInvalidRequest, ce.Code)
	assert.True(t, errs.IsInvalid(err))
}

// --- Graphable arrival (channel a) via MergeEntity ---

type testGraphablePayload struct {
	id      string
	triples []message.Triple
	profile string // empty => does not implement a meaningful profile
}

func (p *testGraphablePayload) EntityID() string             { return p.id }
func (p *testGraphablePayload) Triples() []message.Triple    { return p.triples }
func (p *testGraphablePayload) IndexingProfile() string      { return p.profile }
func (p *testGraphablePayload) Validate() error              { return nil }
func (p *testGraphablePayload) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (p *testGraphablePayload) UnmarshalJSON([]byte) error   { return nil }
func (p *testGraphablePayload) Schema() message.Type {
	return message.Type{Domain: "test", Category: "graphable", Version: "v1"}
}

func TestIndexingProfile_Graphable_IndexingProfilerWins(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	payload := &testGraphablePayload{
		id:      testProfileEntityID,
		triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "doc", "body", "text"), Object: "content here", Timestamp: time.Now()}},
		profile: vocabulary.IndexingProfileContent,
	}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	entity, err := comp.extractEntityFromMessage(msg)
	require.NoError(t, err)
	require.NoError(t, comp.MergeEntity(context.Background(), entity))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileContent}, profileValues(es),
		"a Graphable IndexingProfiler declaration must be stamped at the merge create seam")
}

func TestIndexingProfile_Graphable_NoProfilerFallsToFloor(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// Payload returns "" from IndexingProfile() => treated as absent.
	payload := &testGraphablePayload{id: testProfileEntityID, triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "test", "fixture", "value"), Object: "v", Timestamp: time.Now()}}}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	entity, err := comp.extractEntityFromMessage(msg)
	require.NoError(t, err)
	require.NoError(t, comp.MergeEntity(context.Background(), entity))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"a Graphable that declares no profile falls to the control floor")
}

// --- single-valued invariant: re-arrival must not accumulate or re-profile ---

func TestIndexingProfile_ReArrival_DoesNotAccumulateOrReProfile(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// First arrival: declares content.
	first := &testGraphablePayload{id: testProfileEntityID, triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "doc", "body", "text"), Object: "v1", Timestamp: time.Now()}}, profile: vocabulary.IndexingProfileContent}
	e1, err := comp.extractEntityFromMessage(message.NewBaseMessage(first.Schema(), first, "test"))
	require.NoError(t, err)
	require.NoError(t, comp.MergeEntity(ctx, e1))

	// Second arrival of the SAME entity declaring a DIFFERENT profile (trace):
	// the profile is immutable after create, so this must NOT re-profile and
	// must NOT add a second profile triple.
	second := &testGraphablePayload{id: testProfileEntityID, triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "doc", "body", "text"), Object: "v2", Timestamp: time.Now()}}, profile: vocabulary.IndexingProfileTrace}
	e2, err := comp.extractEntityFromMessage(message.NewBaseMessage(second.Schema(), second, "test"))
	require.NoError(t, err)
	require.NoError(t, comp.MergeEntity(ctx, e2))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileContent}, profileValues(es),
		"re-arrival must keep the create-time profile and stay single-valued")
}

// --- stub → real upgrade ---

func TestIndexingProfile_StubUpgrade_StampsOnRealArrival(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Simulate a referential-integrity stub already present (no profile).
	stub := &graph.EntityState{
		ID:      testProfileEntityID,
		Version: 1,
		Triples: []message.Triple{{Subject: testProfileEntityID, Predicate: "core.identity.stub", Object: true, Timestamp: time.Now()}},
	}
	stubData, _ := json.Marshal(stub)
	_, err := comp.entityBucket.Put(ctx, testProfileEntityID, stubData)
	require.NoError(t, err)
	require.Empty(t, profileValues(storedEntity(t, comp, testProfileEntityID)), "stub starts with no profile")

	// Real producer arrives and merges into the stub: this is the entity's true
	// birth, so the profile must be stamped now (floor, since none declared).
	realArrival := &testGraphablePayload{id: testProfileEntityID, triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "doc", "body", "text"), Object: "real", Timestamp: time.Now()}}}
	entity, err := comp.extractEntityFromMessage(message.NewBaseMessage(realArrival.Schema(), realArrival, "test"))
	require.NoError(t, err)
	require.NoError(t, comp.MergeEntity(ctx, entity))

	es := storedEntity(t, comp, testProfileEntityID)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"a real producer merging into a profile-less stub must stamp the profile")
}

// --- ADR-054 §1 invariant: the structural graph is NEVER gated by profile ---

func TestIndexingProfile_StructuralGraphNeverGated_TraceEntityStaysQueryable(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	req := graph.CreateEntityWithTriplesRequest{
		Entity:          &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()},
		Triples:         []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "audit", "event", "kind"), Object: "trace-line", Timestamp: time.Now()}},
		IndexingProfile: vocabulary.IndexingProfileTrace,
	}
	createData, _ := json.Marshal(req)
	_, err := comp.handleEntityCreateWithTriples(ctx, createData)
	require.NoError(t, err)

	// A 'trace' entity is excluded from EMBEDDING (Phase 3) but must remain
	// fully queryable/traversable: the query path must not filter on profile.
	queryReq, _ := json.Marshal(map[string]any{"prefix": "c360", "limit": 10})
	respData, err := comp.handleQueryPrefixNATS(ctx, queryReq)
	require.NoError(t, err)
	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.Len(t, resp.Entities, 1, "trace entity must still be returned by structural query")
	assert.Equal(t, []string{vocabulary.IndexingProfileTrace}, profileValues(&resp.Entities[0]))
}

// --- helper-level unit tests ---

func TestReconcileIndexingProfile_DedupesToFirst(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	es := &graph.EntityState{ID: testProfileEntityID, MessageType: testWidgetMessageType()}
	es.Triples = []message.Triple{
		{Subject: es.ID, Predicate: semantictest.Predicate(t, "entity", "indexing", "profile"), Object: vocabulary.IndexingProfileContent},
		{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "test", "fixture", "value"), Object: "v", Timestamp: time.Now()},
		{Subject: es.ID, Predicate: semantictest.Predicate(t, "entity", "indexing", "profile"), Object: vocabulary.IndexingProfileTrace},
	}
	comp.reconcileIndexingProfile(es)
	assert.Equal(t, []string{vocabulary.IndexingProfileContent}, profileValues(es),
		"reconcile keeps the FIRST profile and drops duplicates (single-valued)")
}

// TestIndexingProfile_FloorMetric_FiresExactlyOnFloor proves the operator-
// load-bearing indexing_profile_default_total counter increments EXACTLY when
// the floor is applied (no producer declaration) and NOT when a profile is
// declared or on a re-arrival of an already-profiled entity. The Phase-3
// strict-exclusion flip is gated on a cost-ledger built from this counter, so
// an unconditional or mislabeled increment must fail a test, not ship silently.
func TestIndexingProfile_FloorMetric_FiresExactlyOnFloor(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// A unique message_type isolates this counter label from any other test
	// touching the process-global vec.
	mt := message.Type{Domain: "metrictest", Category: "widget", Version: "v1"}
	counter := getIndexingProfileDefaultMetric(nil).WithLabelValues(mt.Key())

	create := func(id, profile string) {
		req := graph.CreateEntityWithTriplesRequest{
			Entity:          &graph.EntityState{ID: id, MessageType: mt},
			IndexingProfile: profile,
		}
		data, _ := json.Marshal(req)
		_, err := comp.handleEntityCreateWithTriples(ctx, data)
		require.NoError(t, err)
	}

	// (a) No declaration → floor applied → metric increments by exactly 1.
	before := testutil.ToFloat64(counter)
	create("c360.platform.metrictest.sys.widget.001", "")
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"floor fallback must increment indexing_profile_default_total exactly once")

	// (b) Explicit declaration → floor NOT applied → metric unchanged.
	before = testutil.ToFloat64(counter)
	create("c360.platform.metrictest.sys.widget.002", vocabulary.IndexingProfileContent)
	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001,
		"a declared profile must NOT increment the default-floor metric")

	// (c) Re-arrival of the floor-stamped entity (001) → profile already present
	//     → keep-first, no floor → metric unchanged (immutability is not a default).
	before = testutil.ToFloat64(counter)
	payload := &testGraphablePayload{id: "c360.platform.metrictest.sys.widget.001", triples: []message.Triple{{Subject: testProfileEntityID, Predicate: semantictest.Predicate(t, "test", "fixture", "value"), Object: "v2", Timestamp: time.Now()}}}
	entity, err := comp.extractEntityFromMessage(message.NewBaseMessage(mt, payload, "test"))
	require.NoError(t, err)
	require.NoError(t, comp.MergeEntity(ctx, entity))
	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001,
		"re-arrival of an already-profiled entity must NOT increment the default-floor metric")
}

func TestIndexingProfileMetricLabel(t *testing.T) {
	cases := []struct {
		name string
		mt   message.Type
		want string
	}{
		{"zero", message.Type{}, "unknown"},
		{"missing-domain", message.Type{Category: "widget", Version: "v1"}, "unknown"},
		{"missing-version", message.Type{Domain: "test", Category: "widget"}, "unknown"},
		{"complete", message.Type{Domain: "test", Category: "widget", Version: "v1"}, "test.widget.v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, indexingProfileMetricLabel(tc.mt))
		})
	}
}

func TestStampExplicitIndexingProfile_IgnoresInvalid(t *testing.T) {
	es := &graph.EntityState{ID: testProfileEntityID}
	stampExplicitIndexingProfile(es, "garbage")
	assert.Empty(t, profileValues(es), "invalid explicit profile must be ignored (fall through to floor)")

	stampExplicitIndexingProfile(es, vocabulary.IndexingProfileSignal)
	assert.Equal(t, []string{vocabulary.IndexingProfileSignal}, profileValues(es))

	// Re-stamping replaces (single-valued).
	stampExplicitIndexingProfile(es, vocabulary.IndexingProfileContent)
	assert.Equal(t, []string{vocabulary.IndexingProfileContent}, profileValues(es))
}
