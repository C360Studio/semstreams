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
	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-054 Phase 2: deriveIndexingProfileFloor consults a type-aware registry
// (indexingProfileDefaults) instead of always returning control. The floor is
// still LENIENT (no consumer acts on it); these tests lock the registry values
// + the metric-on-miss semantics.

func TestIndexingProfileRegistry_AllValuesValid(t *testing.T) {
	require.NotEmpty(t, indexingProfileDefaults, "registry seed must not be empty")
	for key, value := range indexingProfileDefaults {
		assert.True(t, vocabulary.IsValidIndexingProfile(value),
			"registry value for %q must be a valid indexing profile, got %q", key, value)
	}
}

func TestIndexingProfileFloorFor(t *testing.T) {
	cases := []struct {
		mt         message.Type
		want       string
		registered bool
	}{
		{message.Type{Domain: "agentic", Category: "request", Version: "v1"}, vocabulary.IndexingProfileTrace, true},
		{message.Type{Domain: "agentic", Category: "tool_result", Version: "v1"}, vocabulary.IndexingProfileTrace, true},
		{message.Type{Domain: "agentic", Category: "user_message", Version: "v1"}, vocabulary.IndexingProfileContent, true},
		{message.Type{Domain: "agentic", Category: "loop_completed", Version: "v1"}, vocabulary.IndexingProfileControl, true},
		{message.Type{Domain: "agentic", Category: "signal", Version: "v1"}, vocabulary.IndexingProfileSignal, true},
		{message.Type{Domain: "research", Category: "result", Version: "v1"}, vocabulary.IndexingProfileContent, true},
		// Misses fall to the control floor (fail-safe) and report registered=false.
		{message.Type{Domain: "unknown", Category: "thing", Version: "v1"}, vocabulary.IndexingProfileControl, false},
		{message.Type{}, vocabulary.IndexingProfileControl, false},
	}
	for _, tc := range cases {
		profile, registered := indexingProfileFloorFor(tc.mt)
		assert.Equal(t, tc.want, profile, "profile for %q", tc.mt.Key())
		assert.Equal(t, tc.registered, registered, "registered for %q", tc.mt.Key())
	}
}

// TestIndexingProfileRegistry_KeysTrackDomainVersionConstants is the
// version-drift guard for the string-keyed registry. The production registry
// can't import the domain packages (that would couple the generic graph-ingest
// layer to agentic/research/operating-model), but this TEST can — so it rebuilds
// canonical keys from the domain Domain/Category/SchemaVersion constants and
// asserts the registry contains them. If a domain bumps SchemaVersion (v1→v2)
// without updating the registry, the rebuilt key won't be found and this fails
// at CI time — catching the silent control-default drift the metric would
// otherwise only surface in production.
func TestIndexingProfileRegistry_KeysTrackDomainVersionConstants(t *testing.T) {
	key := func(domain, category, version string) string {
		return message.Type{Domain: domain, Category: category, Version: version}.Key()
	}
	cases := []struct {
		key  string
		want string
	}{
		{key(agentic.Domain, agentic.CategoryRequest, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(agentic.Domain, agentic.CategoryToolResult, agentic.SchemaVersion), vocabulary.IndexingProfileTrace},
		{key(agentic.Domain, agentic.CategoryUserMessage, agentic.SchemaVersion), vocabulary.IndexingProfileContent},
		{key(agentic.Domain, agentic.CategoryLoopCompleted, agentic.SchemaVersion), vocabulary.IndexingProfileControl},
		{key(agentic.Domain, agentic.CategorySignal, agentic.SchemaVersion), vocabulary.IndexingProfileSignal},
		{key(research.Domain, research.CategoryResult, research.SchemaVersion), vocabulary.IndexingProfileContent},
		{key(research.Domain, research.CategoryRouteDecision, research.SchemaVersion), vocabulary.IndexingProfileTrace},
	}
	for _, tc := range cases {
		got, ok := indexingProfileDefaults[tc.key]
		assert.True(t, ok, "registry must contain key %q (rebuilt from domain constants — drift if missing)", tc.key)
		assert.Equal(t, tc.want, got, "profile for %q", tc.key)
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

// End-to-end through the production create handler: a REGISTERED type floors to
// its registry profile (here trace, not the old always-control) and, because a
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
		"a registered type must floor to its registry profile (trace), not the old always-control")
	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001,
		"a registered floor is NOT a registry gap → the default metric must NOT fire")
}

// The complement: an UNREGISTERED type floors to control AND fires the gap
// metric (the ADR §5 "registry gap is visible" signal).
func TestIndexingProfile_RegistryFloor_UnregisteredFiresMetric(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	mt := message.Type{Domain: "gapdomain", Category: "gapcat", Version: "v1"}
	counter := getIndexingProfileDefaultMetric(nil).WithLabelValues(mt.Key())
	before := testutil.ToFloat64(counter)

	const id = "c360.platform.gapdomain.sys.gapcat.001"
	req := graph.CreateEntityRequest{Entity: &graph.EntityState{ID: id, MessageType: mt}}
	data, _ := json.Marshal(req)
	_, err := comp.handleCanonicalCreate(ctx, data)
	require.NoError(t, err)

	es := storedEntity(t, comp, id)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"an unregistered type floors to control (fail-safe)")
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"an unregistered floor IS a gap → the default metric must fire exactly once")
}
