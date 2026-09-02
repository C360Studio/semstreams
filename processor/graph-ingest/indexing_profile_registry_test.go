package graphingest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-054 Phase 2 / ADR-103: the indexing-profile floor is an attribute
// registered with the type (Registration.IndexingProfile), read through the
// registry graph-ingest already holds. The floor is still LENIENT (no consumer
// acts on it); these tests lock the registered values + the metric semantics —
// every expectation the retired string-keyed table carried is kept.

// floorTestRegistry holds the registration sets whose floors the retired table
// enumerated: agentic (15 + the mutation-lane types) and graph research. The
// dispatch-local control-signal type the table also carried was retired with
// its whole registration — one producer, zero consumers — so the floor is 21
// keys, not 22.
func floorTestRegistry(t *testing.T) *payloadregistry.Registry {
	t.Helper()
	return payloadregistry.NewWithSubset(t, agentic.RegisterPayloads, research.RegisterPayloads)
}

// floorTestComponent is a Component holding only the floor registry, for the
// registeredIndexingProfile helper.
func floorTestComponent(t *testing.T) *Component {
	t.Helper()
	return &Component{payloadRegistry: floorTestRegistry(t)}
}

func TestIndexingProfileRegistry_AllValuesValid(t *testing.T) {
	reg := floorTestRegistry(t)
	listed := reg.List()
	require.NotEmpty(t, listed, "registry must not be empty")
	for key, registration := range listed {
		assert.True(t, registration.IndexingProfile == "" || vocabulary.IsValidIndexingProfile(registration.IndexingProfile),
			"registered floor for %q must be empty or a valid indexing profile, got %q", key, registration.IndexingProfile)
	}
}

func TestIndexingProfileFloorFor(t *testing.T) {
	c := floorTestComponent(t)
	cases := []struct {
		mt      message.Type
		want    string
		floored bool
	}{
		{message.Type{Domain: "agentic", Category: "request", Version: "v1"}, vocabulary.IndexingProfileTrace, true},
		{message.Type{Domain: "agentic", Category: "tool_result", Version: "v1"}, vocabulary.IndexingProfileTrace, true},
		{message.Type{Domain: "agentic", Category: "user_message", Version: "v1"}, vocabulary.IndexingProfileContent, true},
		{message.Type{Domain: "agentic", Category: "loop_completed", Version: "v1"}, vocabulary.IndexingProfileControl, true},
		{message.Type{Domain: "agentic", Category: "signal", Version: "v1"}, vocabulary.IndexingProfileSignal, true},
		{message.Type{Domain: "research", Category: "result", Version: "v1"}, vocabulary.IndexingProfileContent, true},
		// A type the registry does not hold falls to the control floor (fail-safe)
		// and reports floored=false; the create seams refuse it before this runs.
		{message.Type{Domain: "unknown", Category: "thing", Version: "v1"}, vocabulary.IndexingProfileControl, false},
		{message.Type{}, vocabulary.IndexingProfileControl, false},
	}
	for _, tc := range cases {
		profile, floored := c.registeredIndexingProfile(tc.mt)
		assert.Equal(t, tc.want, profile, "profile for %q", tc.mt.Key())
		assert.Equal(t, tc.floored, floored, "floored for %q", tc.mt.Key())
	}

	t.Run("a component without a registry answers control and unfloored", func(t *testing.T) {
		profile, floored := (&Component{}).registeredIndexingProfile(message.Type{Domain: "agentic", Category: "request", Version: "v1"})
		assert.Equal(t, vocabulary.IndexingProfileControl, profile)
		assert.False(t, floored)
	})
}

// TestIndexingProfileRegistry_KeysTrackDomainVersionConstants keeps the
// registered floors pinned to the domain constants: every key the retired
// table carried is registered with the same value.
func TestIndexingProfileRegistry_KeysTrackDomainVersionConstants(t *testing.T) {
	reg := floorTestRegistry(t)
	key := func(domain, category, version string) string {
		return message.Type{Domain: domain, Category: category, Version: version}.Key()
	}
	cases := []struct {
		key  string
		want string
	}{
		{key(agentic.Domain, agentic.CategoryUserMessage, agentic.SchemaVersion), vocabulary.IndexingProfileContent},
		{key(agentic.Domain, agentic.CategoryUserResponse, agentic.SchemaVersion), vocabulary.IndexingProfileContent},
		{key(research.Domain, research.CategoryResult, research.SchemaVersion), vocabulary.IndexingProfileContent},
		{key(agentic.Domain, agentic.CategoryTask, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategoryLoopCreated, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategoryLoopCompleted, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategoryLoopFailed, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategoryLoopCancelled, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategoryApprovalPending, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategoryApprovalResponse, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(research.Domain, research.CategoryIntent, research.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategorySignal, agentic.SchemaVersion), vocabulary.IndexingProfileSignal},
		{key(agentic.Domain, agentic.CategoryRequest, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(agentic.Domain, agentic.CategoryResponse, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(agentic.Domain, agentic.CategoryToolCall, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(agentic.Domain, agentic.CategoryToolResult, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(agentic.Domain, agentic.CategoryContextEvent, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(research.Domain, research.CategoryRouteDecision, research.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(research.Domain, research.CategoryClassifierOutput, research.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(research.Domain, research.CategoryExecutionOutput, research.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(research.Domain, research.CategoryAssessmentOutput, research.SchemaVersion), vocabulary.IndexingProfileTrace},
	}
	require.Len(t, cases, 21, "the retired table carried 22 keys; the retired dispatch control-signal key leaves 21")
	for _, tc := range cases {
		got, registered := reg.IndexingProfileFor(tc.key)
		assert.True(t, registered, "registry must hold %q (rebuilt from domain constants — drift if missing)", tc.key)
		assert.Equal(t, tc.want, got, "floor for %q", tc.key)
	}
}

// TestIndexingProfile_Append_DoesNotStamp locks the indexing invariant:
// append is NOT a stamp seam. An entity updated via append carries no
// additional profile triple — reconcileIndexingProfile is not on this path.
// The entity must be pre-created (ADR-055 deleted the auto-vivify path);
// Appending to a pre-existing entity must NOT re-stamp indexing metadata.
func TestIndexingProfile_Append_DoesNotStamp(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	const id = "c360.platform.test.sys.widget.addtriple1"
	// Pre-create the entity via the create seam so it exists in ENTITY_STATES.
	req := graph.CreateEntityRequest{Entity: &graph.EntityState{ID: id, MessageType: testWidgetMessageType()}}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	_, err = comp.handleCanonicalCreate(ctx, data)
	require.NoError(t, err)

	// Record the profile triples stamped at create time so we can assert
	// that append does NOT add more.
	esBefore := storedEntity(t, comp, id)
	profilesBefore := profileValues(esBefore)

	// Now add a user triple via the append path.
	tr := message.Triple{Subject: id, Predicate: "evidence.note.value", Object: "v", Confidence: 1.0}
	appendData, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{tr}})
	require.NoError(t, err)
	_, err = comp.handleCanonicalAppend(ctx, appendData)
	require.NoError(t, err)

	esAfter := storedEntity(t, comp, id)
	assert.Equal(t, profilesBefore, profileValues(esAfter),
		"append must NOT stamp or change the indexing profile (only the create seam stamps)")
	assert.Equal(t, nonProfileTripleCount(esBefore)+1, nonProfileTripleCount(esAfter),
		"the entity holds exactly one additional user triple after append")
}

// End-to-end through the production create handler: a type with a REGISTERED
// floor takes it (here trace, not the old always-control) and, because a
// registered floor is a deliberate classification rather than an operator gap,
// the default-fallback metric must NOT fire.
func TestIndexingProfile_RegistryFloor_RegisteredTypeNoMetric(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	mt := message.Type{Domain: "agentic", Category: "request", Version: "v1"}
	counter := getIndexingProfileDefaultMetric(nil).WithLabelValues(mt.Key())
	before := testutil.ToFloat64(counter)

	const id = "c360.platform.agentic.sys.request.001"
	req := graph.CreateEntityRequest{Entity: &graph.EntityState{ID: id, MessageType: mt}}
	data, _ := json.Marshal(req)
	_, err := comp.handleCanonicalCreate(ctx, data)
	require.NoError(t, err)

	es := storedEntity(t, comp, id)
	assert.Equal(t, []string{vocabulary.IndexingProfileTrace}, profileValues(es),
		"a type with a registered floor must take it (trace), not the old always-control")
	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001,
		"a registered floor is NOT a gap → the default metric must NOT fire")
}

// The complement: a REGISTERED type that declares no floor falls to control AND
// fires the metric — its new meaning under ADR-103: the label names a
// Registration literal whose IndexingProfile is empty.
func TestIndexingProfile_RegistryFloor_RegisteredNoFloorFiresMetric(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	mt := message.Type{Domain: "test", Category: "nofloor", Version: "v1"}
	counter := getIndexingProfileDefaultMetric(nil).WithLabelValues(mt.Key())
	before := testutil.ToFloat64(counter)

	const id = "c360.platform.test.sys.nofloor.001"
	req := graph.CreateEntityRequest{Entity: &graph.EntityState{ID: id, MessageType: mt}}
	data, _ := json.Marshal(req)
	_, err := comp.handleCanonicalCreate(ctx, data)
	require.NoError(t, err)

	es := storedEntity(t, comp, id)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"a registered type with no floor falls to control (fail-safe)")
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"a registered type with no floor IS the metered gap → the default metric must fire exactly once")
}
