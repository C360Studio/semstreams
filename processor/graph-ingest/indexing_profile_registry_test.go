package graphingest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
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
		{message.Type{Domain: "operating_model", Category: "layer_approved", Version: "v1"}, vocabulary.IndexingProfileContent, true},
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
		{key(operatingmodel.Domain, operatingmodel.CategoryLayerApproved, operatingmodel.SchemaVersion), vocabulary.IndexingProfileContent},
		{key(operatingmodel.Domain, operatingmodel.CategoryProfileContext, operatingmodel.SchemaVersion), vocabulary.IndexingProfileContent},
	}
	for _, tc := range cases {
		got, ok := indexingProfileDefaults[tc.key]
		assert.True(t, ok, "registry must contain key %q (rebuilt from domain constants — drift if missing)", tc.key)
		assert.Equal(t, tc.want, got, "profile for %q", tc.key)
	}
}

// TestIndexingProfile_AddTripleAutoVivify_DoesNotStamp locks the ADR-055
// forward-compat decision: the triple.add auto-vivify path (which ADR-055 will
// DELETE) is NOT a stamp seam. An entity born via triple.add carries no profile
// triple — lenient consumers index it, and the floor metric never fires because
// reconcileIndexingProfile is not on this path. If a future change wires
// stamping into the auto-vivify branch (rather than removing it per ADR-055),
// this fails.
func TestIndexingProfile_AddTripleAutoVivify_DoesNotStamp(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	const id = "c360.platform.test.sys.widget.autovivify1"
	tr := message.Triple{Subject: id, Predicate: "evidence.note", Object: "v", Confidence: 1.0}
	require.NoError(t, comp.AddTriple(ctx, tr))

	es := storedEntity(t, comp, id)
	assert.Empty(t, profileValues(es),
		"triple.add auto-vivify must NOT stamp an indexing profile (ADR-055 deletes that create path)")
	assert.Equal(t, 1, nonProfileTripleCount(es), "the entity holds exactly the one added triple")
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
	req := graph.CreateEntityWithTriplesRequest{Entity: &graph.EntityState{ID: id, MessageType: mt}}
	data, _ := json.Marshal(req)
	_, err := comp.handleEntityCreateWithTriples(ctx, data)
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
	req := graph.CreateEntityWithTriplesRequest{Entity: &graph.EntityState{ID: id, MessageType: mt}}
	data, _ := json.Marshal(req)
	_, err := comp.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, err)

	es := storedEntity(t, comp, id)
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(es),
		"an unregistered type floors to control (fail-safe)")
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"an unregistered floor IS a gap → the default metric must fire exactly once")
}
