package executors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// The fixtures below are built through graph.MarshalEntityState — the same
// write gate ENTITY_STATES enforces — so a triple these tests assert on is a
// triple the graph would actually store. A hand-written map would let a test
// assert over a record the authority would have rejected.

const (
	fixtureTempOne   = "acme.test.gcs.environmental.temperature.t1"
	fixtureTempTwo   = "acme.test.gcs.environmental.temperature.t2"
	fixtureTempThree = "acme.test.hvac.environmental.temperature.t3"
	fixtureDroneOne  = "acme.test.gcs.robotics.drone.d1"
	fixtureSiteOne   = "acme.test.gcs.facility.site.s1"
)

// entityFixture assembles one canonical EntityState and serializes it through
// the authoritative marshaller, failing the test when the contract rejects it.
func entityFixture(t *testing.T, id string, triples ...message.Triple) []byte {
	t.Helper()
	entity := &graph.EntityState{
		ID:          id,
		Triples:     triples,
		MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		UpdatedAt:   time.Unix(0, 0).UTC(),
	}
	data, err := graph.MarshalEntityState(entity)
	require.NoError(t, err, "fixture %s must satisfy the ENTITY_STATES contract", id)
	return data
}

func relationshipTriple(subject, predicate, object string) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Datatype:   message.EntityReferenceDatatype,
		Source:     "fixture",
		Timestamp:  time.Unix(0, 0).UTC(),
		Confidence: 1,
	}
}

func propertyTriple(subject, predicate string, object any) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Source:     "fixture",
		Timestamp:  time.Unix(0, 0).UTC(),
		Confidence: 1,
	}
}

// mockKVLister adds key listing to mockKVGetter. It is a SEPARATE type on
// purpose: mockKVGetter must NOT satisfy KVKeyLister, or
// TestQueryByType_WithoutKeyListerIsLoud would have nothing to observe.
type mockKVLister struct {
	*mockKVGetter
	// keys is returned VERBATIM for any pattern — deliberately unsorted and
	// deliberately wider than any one pattern selects, so the executor's own
	// sort and its own MatchEntityIDPattern filter are the things under test
	// rather than a mock that pre-answers the question.
	keys      []string
	patterns  []string
	callCount int
	err       error
}

func (m *mockKVLister) KeysByPattern(_ context.Context, pattern string) ([]string, error) {
	m.callCount++
	m.patterns = append(m.patterns, pattern)
	if m.err != nil {
		return nil, m.err
	}
	return append([]string(nil), m.keys...), nil
}

func decodeContent(t *testing.T, result agentic.ToolResult) map[string]any {
	t.Helper()
	require.Empty(t, result.Error, "expected a successful result")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &parsed), "content must be JSON: %s", result.Content)
	return parsed
}

func stringSlice(t *testing.T, raw any) []string {
	t.Helper()
	items, ok := raw.([]any)
	require.Truef(t, ok, "expected a JSON array, got %T", raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		require.Truef(t, ok, "expected a string element, got %T", item)
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------------------
// query_relationships
// ---------------------------------------------------------------------------

// TestQueryRelationships_FilteredAbsenceIsClassified is the blind spot this
// change exists to close: before it, a registered-but-absent predicate, an
// unregistered one, a typo, and one present only as a property all answered
// the identical `count: 0` with nothing to tell them apart.
//
// spec: agentic-tools / Direct graph-read tools classify an empty result and show the vocabulary they observed
func TestQueryRelationships_FilteredAbsenceIsClassified(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register("agent.lineage.parent", vocabulary.WithDescription("the loop that spawned this one"))
	vocabulary.Register("sensor.temperature.celsius", vocabulary.WithDescription("temperature reading"))

	kv := newMockKVGetter()
	kv.Put(fixtureTempOne, entityFixture(t, fixtureTempOne,
		propertyTriple(fixtureTempOne, "sensor.temperature.celsius", 48.2),
	))
	countingKV := &countingKVGetter{mockKVGetter: kv}
	executor := NewGraphQueryExecutor(countingKV)

	tests := []struct {
		name           string
		filter         string
		wantErrorKind  agentic.ToolErrorKind
		wantRegistered bool
	}{
		{name: "registered predicate absent on the entity", filter: "agent.lineage.parent", wantRegistered: true},
		{name: "unregistered predicate", filter: "agent.lineage.nonesuch", wantRegistered: false},
		{name: "present only as a property", filter: "sensor.temperature.celsius", wantRegistered: true},
		{name: "malformed filter", filter: "not-three-segments", wantErrorKind: agentic.ToolErrorInvalidArgs},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			countingKV.gets = 0
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID:   "call-" + tc.name,
				Name: "query_relationships",
				Arguments: map[string]any{
					"entity_id":         fixtureTempOne,
					"relationship_type": tc.filter,
				},
			})
			require.NoError(t, err)

			if tc.wantErrorKind != "" {
				assert.Equal(t, tc.wantErrorKind, result.ErrorKind)
				assert.Empty(t, result.Content, "a refused call must not emit a body")
				assert.Zero(t, countingKV.gets, "no entity is read for a filter the tool already knows is malformed")
				return
			}

			content := decodeContent(t, result)
			assert.Equal(t, float64(0), content["count"], "no relationship carries this predicate")
			assert.Equal(t, agentic.HintEmpty, result.ResultHint,
				"an empty success is classified, so the model is not left reading zero as an error")
			assert.Equal(t, tc.wantRegistered, content["filter_registered"],
				"filter_registered reports THIS process's registry, and nothing else")

			present, ok := content["predicates_present"].(map[string]any)
			require.True(t, ok, "predicates_present must be an object: %s", result.Content)
			celsius, ok := present["sensor.temperature.celsius"].(map[string]any)
			require.True(t, ok, "the predicate the entity DOES carry must be visible: %v", present)
			assert.Equal(t, "property", celsius["kind"],
				"a literal object is a property; showing its kind is what makes the zero count legible")
		})
	}
}

// TestQueryRelationships_LiteralObjectsAreNotRelationships pins the row filter:
// message.Triple.IsRelationship() decides, so a literal reading never arrives
// at the model dressed as an edge.
//
// spec: agentic-tools / Direct graph-read tools classify an empty result and show the vocabulary they observed
func TestQueryRelationships_LiteralObjectsAreNotRelationships(t *testing.T) {
	kv := newMockKVGetter()
	kv.Put(fixtureDroneOne, entityFixture(t, fixtureDroneOne,
		relationshipTriple(fixtureDroneOne, "robotics.fleet.member", fixtureSiteOne),
		propertyTriple(fixtureDroneOne, "robotics.battery.level", 82.5),
		propertyTriple(fixtureDroneOne, "robotics.fleet.label", "alpha"),
		// A literal whose TEXT has canonical entity-ID shape. The explicit
		// datatype is what keeps it a property, and it is the case a
		// shape-sniffing implementation gets wrong.
		message.Triple{
			Subject: fixtureDroneOne, Predicate: "robotics.fleet.note",
			Object: fixtureSiteOne, Datatype: "xsd:string",
			Source: "fixture", Timestamp: time.Unix(0, 0).UTC(), Confidence: 1,
		},
	))
	executor := NewGraphQueryExecutor(kv)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-literals", Name: "query_relationships",
		Arguments: map[string]any{"entity_id": fixtureDroneOne},
	})
	require.NoError(t, err)

	content := decodeContent(t, result)
	assert.Equal(t, float64(1), content["count"], "only the entity-reference triple is a relationship")
	rows := stringSliceOfField(t, content["relationships"], "type")
	assert.Equal(t, []string{"robotics.fleet.member"}, rows)

	present, ok := content["predicates_present"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "relationship", present["robotics.fleet.member"].(map[string]any)["kind"])
	assert.Equal(t, "property", present["robotics.battery.level"].(map[string]any)["kind"])
	assert.Equal(t, "property", present["robotics.fleet.note"].(map[string]any)["kind"],
		"an explicit non-reference datatype makes the object a literal even when its text looks like an ID")
}

// stringSliceOfField pulls one field out of every object in a JSON array.
func stringSliceOfField(t *testing.T, raw any, field string) []string {
	t.Helper()
	items, ok := raw.([]any)
	require.Truef(t, ok, "expected a JSON array, got %T", raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		require.Truef(t, ok, "expected an object element, got %T", item)
		value, _ := row[field].(string)
		out = append(out, value)
	}
	return out
}

// TestQueryRelationships_PredicatesPresentCarriesRegistryMetadata: the entity's
// vocabulary is reported with the registry's own words, so the model reads the
// names instead of guessing at a spelling.
//
// spec: agentic-tools / Direct graph-read tools classify an empty result and show the vocabulary they observed
func TestQueryRelationships_PredicatesPresentCarriesRegistryMetadata(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	vocabulary.Register("robotics.fleet.member",
		vocabulary.WithDescription("drone belongs to fleet"),
		vocabulary.WithRole(vocabulary.RoleIdentity),
		vocabulary.WithInverseOf("robotics.fleet.contains"))
	vocabulary.Register("robotics.fleet.contains",
		vocabulary.WithInverseOf("robotics.fleet.member"))

	kv := newMockKVGetter()
	kv.Put(fixtureDroneOne, entityFixture(t, fixtureDroneOne,
		relationshipTriple(fixtureDroneOne, "robotics.fleet.member", fixtureSiteOne),
		propertyTriple(fixtureDroneOne, "robotics.battery.level", 82.5),
	))
	executor := NewGraphQueryExecutor(kv)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-registry", Name: "query_relationships",
		Arguments: map[string]any{"entity_id": fixtureDroneOne},
	})
	require.NoError(t, err)

	present, ok := decodeContent(t, result)["predicates_present"].(map[string]any)
	require.True(t, ok)

	member, ok := present["robotics.fleet.member"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, member["registered"])
	assert.Equal(t, "drone belongs to fleet", member["description"])
	assert.Equal(t, string(vocabulary.RoleIdentity), member["role"])
	assert.Equal(t, "robotics.fleet.contains", member["inverse_of"])

	battery, ok := present["robotics.battery.level"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, battery["registered"],
		"an unregistered predicate is reported as unregistered, never omitted")
	assert.NotContains(t, battery, "role", "role and inverse_of are relationship-shaped metadata")
	assert.NotContains(t, battery, "inverse_of")
}

// TestQueryRelationships_UnservedDirectionIsRefused: the tool reads the
// entity's own record, so it can only answer outgoing. HintEmpty on `incoming`
// would have said "nothing exists" when the truth is "this tool cannot see it".
//
// spec: agentic-tools / query_relationships serves the direction it can read and names the owner of the one it cannot
func TestQueryRelationships_UnservedDirectionIsRefused(t *testing.T) {
	kv := newMockKVGetter()
	kv.Put(fixtureDroneOne, entityFixture(t, fixtureDroneOne,
		relationshipTriple(fixtureDroneOne, "robotics.fleet.member", fixtureSiteOne),
	))
	countingKV := &countingKVGetter{mockKVGetter: kv}
	executor := NewGraphQueryExecutor(countingKV)

	for _, direction := range []string{"incoming", "both"} {
		t.Run(direction, func(t *testing.T) {
			countingKV.gets = 0
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID: "call-" + direction, Name: "query_relationships",
				Arguments: map[string]any{"entity_id": fixtureDroneOne, "direction": direction},
			})
			require.NoError(t, err)
			assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)
			assert.Contains(t, result.Error, "INCOMING_INDEX",
				"the refusal must name the owner of the direction it cannot serve")
			assert.Contains(t, result.Error, "graph.query.relationships")
			assert.Zero(t, countingKV.gets, "no entity is read for a direction the tool cannot answer")
		})
	}

	t.Run("omitted direction is served as outgoing", func(t *testing.T) {
		countingKV.gets = 0
		result, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-omitted", Name: "query_relationships",
			Arguments: map[string]any{"entity_id": fixtureDroneOne},
		})
		require.NoError(t, err)
		content := decodeContent(t, result)
		assert.Equal(t, "outgoing", content["direction"],
			"the narrowing is echoed, so it is observable rather than silent")
		assert.Equal(t, float64(1), content["count"])
		assert.Equal(t, 1, countingKV.gets)
	})

	t.Run("unknown direction is refused rather than defaulted", func(t *testing.T) {
		result, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-sideways", Name: "query_relationships",
			Arguments: map[string]any{"entity_id": fixtureDroneOne, "direction": "sideways"},
		})
		require.NoError(t, err)
		assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)
	})
}

// countingKVGetter counts reads so a test can prove a refusal happened BEFORE
// any entity was touched.
type countingKVGetter struct {
	*mockKVGetter
	gets int
}

func (c *countingKVGetter) Get(ctx context.Context, key string) (KVEntry, error) {
	c.gets++
	return c.mockKVGetter.Get(ctx, key)
}

// ---------------------------------------------------------------------------
// query_by_type
// ---------------------------------------------------------------------------

// listerFixture wires an executor whose lister returns every fixture key in a
// deliberately unsorted order, regardless of pattern.
func listerFixture(t *testing.T) (*GraphQueryExecutor, *mockKVLister) {
	t.Helper()
	kv := newMockKVGetter()
	lister := &mockKVLister{
		mockKVGetter: kv,
		keys: []string{
			fixtureTempThree,
			fixtureDroneOne,
			fixtureTempOne,
			fixtureSiteOne,
			fixtureTempTwo,
		},
	}
	return NewGraphQueryExecutor(lister), lister
}

// TestQueryByType_ListsIDsByTypeSegment: the advertised-but-never-served stub
// answers for real, at all three arities of the right-anchored grammar.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_ListsIDsByTypeSegment(t *testing.T) {
	tests := []struct {
		name        string
		entityType  string
		wantPattern string
		wantIDs     []string
	}{
		{
			name:        "one-token type",
			entityType:  "temperature",
			wantPattern: "*.*.*.*.temperature.*",
			wantIDs:     []string{fixtureTempOne, fixtureTempTwo, fixtureTempThree},
		},
		{
			name:        "two right-anchored tokens",
			entityType:  "environmental.temperature",
			wantPattern: "*.*.*.environmental.temperature.*",
			wantIDs:     []string{fixtureTempOne, fixtureTempTwo, fixtureTempThree},
		},
		{
			name:        "three right-anchored tokens",
			entityType:  "gcs.environmental.temperature",
			wantPattern: "*.*.gcs.environmental.temperature.*",
			wantIDs:     []string{fixtureTempOne, fixtureTempTwo},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executor, lister := listerFixture(t)
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID: "call-" + tc.name, Name: "query_by_type",
				Arguments: map[string]any{"entity_type": tc.entityType, "limit": float64(10)},
			})
			require.NoError(t, err)

			content := decodeContent(t, result)
			assert.Equal(t, tc.wantPattern, content["pattern"],
				"the pattern is reported so the model can see what was actually matched")
			assert.Equal(t, []string{tc.wantPattern}, lister.patterns,
				"the built pattern is what reaches the key listing")
			assert.Equal(t, tc.wantIDs, stringSlice(t, content["entity_ids"]))
			assert.Equal(t, float64(len(tc.wantIDs)), content["matched"])
			assert.Equal(t, float64(len(tc.wantIDs)), content["count"])
			assert.Equal(t, false, result.Metadata[agentic.MetadataKeyHasMore],
				"has_more is set on EVERY successful result of a paginated tool, false included")
			assert.NotContains(t, result.Metadata, agentic.MetadataKeyNextCursor)
			assert.NotContains(t, content, "truncated",
				"continuation belongs to the pagination contract; a second spelling in the body is what Q12 deleted")
			assert.Empty(t, result.ResultHint)
		})
	}

	t.Run("nothing matched is classified empty", func(t *testing.T) {
		executor, _ := listerFixture(t)
		result, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-empty", Name: "query_by_type",
			Arguments: map[string]any{"entity_type": "nonesuch"},
		})
		require.NoError(t, err)
		content := decodeContent(t, result)
		assert.Equal(t, float64(0), content["matched"])
		assert.Equal(t, agentic.HintEmpty, result.ResultHint)
		assert.Equal(t, false, result.Metadata[agentic.MetadataKeyHasMore])
	})
}

// TestQueryByType_SortsUnsortedListerOutput: natsclient.FilteredKeys appends in
// channel-arrival order and sorts nothing (natsclient/kv.go
// collectFilteredKeys), so the deterministic order the cursor rests on is the
// executor's own work. The mock returns scan order; the result must not.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_SortsUnsortedListerOutput(t *testing.T) {
	executor, lister := listerFixture(t)
	require.NotEqual(t, sortedCopy(lister.keys), lister.keys,
		"precondition: the mock must hand back an order that is NOT already sorted")

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-sort", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature"},
	})
	require.NoError(t, err)

	ids := stringSlice(t, decodeContent(t, result)["entity_ids"])
	assert.Equal(t, sortedCopy(ids), ids, "the returned identities are sorted")
	assert.Equal(t, []string{fixtureTempOne, fixtureTempTwo, fixtureTempThree}, ids)
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestQueryByType_PageOneSetsHasMoreAndCursor: a page that leaves matches
// behind sets the continuation metadata AND HintTooLarge — the composition the
// hint was written for, so the model is told both to narrow and that it may
// continue.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_PageOneSetsHasMoreAndCursor(t *testing.T) {
	executor, _ := listerFixture(t)
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-page1", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature", "limit": float64(2)},
	})
	require.NoError(t, err)

	content := decodeContent(t, result)
	assert.Equal(t, float64(3), content["matched"], "matched is the observed total, not the remainder")
	assert.Equal(t, float64(2), content["count"])
	assert.Equal(t, true, result.Metadata[agentic.MetadataKeyHasMore])
	assert.Equal(t, agentic.HintTooLarge, result.ResultHint)
	assert.NotContains(t, content, "truncated", "the result carries no separate truncation field")

	cursor, ok := result.Metadata[agentic.MetadataKeyNextCursor].(string)
	require.True(t, ok, "an intermediate page carries an opaque continuation token")
	assert.NotEmpty(t, cursor)
	assert.NotContains(t, result.Metadata, agentic.MetadataKeyNextOffset,
		"NextCursor and NextOffset are mutually exclusive in the contract")

	decoded, decodeErr := graph.DecodeCursor(cursor)
	require.NoError(t, decodeErr, "the token uses the graph package's own cursor encoding")
	assert.Equal(t, fixtureTempTwo, decoded, "the cursor is the last key on this page")
}

// TestQueryByType_CursorContinuesWithoutRepeats: over one unchanged key set,
// the pages partition the match exactly once.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_CursorContinuesWithoutRepeats(t *testing.T) {
	executor, _ := listerFixture(t)

	first, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-p1", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature", "limit": float64(2)},
	})
	require.NoError(t, err)
	firstIDs := stringSlice(t, decodeContent(t, first)["entity_ids"])
	cursor := first.Metadata[agentic.MetadataKeyNextCursor].(string)

	second, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-p2", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature", "limit": float64(2), "cursor": cursor},
	})
	require.NoError(t, err)
	secondContent := decodeContent(t, second)
	secondIDs := stringSlice(t, secondContent["entity_ids"])

	assert.Equal(t, []string{fixtureTempOne, fixtureTempTwo}, firstIDs)
	assert.Equal(t, []string{fixtureTempThree}, secondIDs)
	for _, id := range secondIDs {
		assert.Greater(t, id, firstIDs[len(firstIDs)-1], "every continued identity sorts after the cursor position")
	}
	assert.Equal(t, float64(3), secondContent["matched"],
		"matched stays the whole match count on every page")
	assert.Equal(t, false, second.Metadata[agentic.MetadataKeyHasMore], "the last page announces no more")
	assert.NotContains(t, second.Metadata, agentic.MetadataKeyNextCursor)
	assert.Empty(t, second.ResultHint)
}

// TestQueryByType_RejectsUndecodableCursor: a token this tool did not issue is
// refused, never quietly reset to page one — which would page the model over
// page one forever.
//
// The name says "undecodable" but the scenario's GIVEN is the wider set: NOT A
// TOKEN THIS TOOL ISSUED. Decoding is not validation — graph.DecodeCursor is
// base64.RawURLEncoding.DecodeString and nothing else — so the decode-failure
// arm alone leaves the dangerous half untested. The two decodable cases below
// are the ones that bite: "MQ" decodes to "1", which sorts BEFORE every
// canonical key and would return a full page one with a fresh next_cursor
// forever; "abcd" decodes past every key and would return an empty page
// alongside a non-zero `matched`, with no hint and no error at all.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_RejectsUndecodableCursor(t *testing.T) {
	cases := map[string]string{
		"not base64 at all":                   "not base64 url!!",
		"decodes, but sorts before every key": "MQ",
		"decodes, but sorts past every key":   "abcd",
	}
	for name, cursor := range cases {
		t.Run(name, func(t *testing.T) {
			executor, lister := listerFixture(t)
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID: "call-badcursor", Name: "query_by_type",
				Arguments: map[string]any{"entity_type": "temperature", "cursor": cursor},
			})
			require.NoError(t, err)
			assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind,
				"cursor %q is not a token this tool issued", cursor)
			assert.Empty(t, result.Content,
				"a refused cursor answers no page at all — an empty or restarted page is the silent reset")
			assert.Zero(t, lister.callCount, "a rejected cursor never costs a key scan")
		})
	}

	t.Run("an empty cursor is the first page, not a bad token", func(t *testing.T) {
		// graph.DecodeCursor returns ("", nil) for the empty cursor by
		// documented contract, and the advertised description says to omit the
		// argument for the first page. A caller that sends the optional
		// argument as "" must get page one, not a refusal telling it the
		// cursor "decodes to \"\"" — and not a refusal of the FIRST page.
		executor, lister := listerFixture(t)
		result, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-emptycursor", Name: "query_by_type",
			Arguments: map[string]any{"entity_type": "temperature", "cursor": ""},
		})
		require.NoError(t, err)
		assert.Empty(t, result.ErrorKind, "an empty cursor is omission, not a bad token")
		assert.NotEmpty(t, result.Content, "the first page is served")
		assert.Equal(t, 1, lister.callCount, "serving page one costs exactly one key scan")
	})

	t.Run("the tool's own cursor is still accepted", func(t *testing.T) {
		executor, _ := listerFixture(t)
		first, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-issued-1", Name: "query_by_type",
			Arguments: map[string]any{"entity_type": "temperature", "limit": float64(1)},
		})
		require.NoError(t, err)
		issued, ok := first.Metadata[agentic.MetadataKeyNextCursor].(string)
		require.True(t, ok, "precondition: page one issues a cursor")

		second, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-issued-2", Name: "query_by_type",
			Arguments: map[string]any{"entity_type": "temperature", "limit": float64(1), "cursor": issued},
		})
		require.NoError(t, err)
		assert.Empty(t, second.ErrorKind, "the validation refuses foreign tokens, not this tool's own")
	})
}

// TestQueryByType_RejectsNonSegmentTokens: the grammar is enforced with a typed
// refusal rather than a silent zero, and never at the cost of a full key scan.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_RejectsNonSegmentTokens(t *testing.T) {
	cases := map[string]string{
		"bare wildcard":     "*",
		"embedded wildcard": "environmental.*",
		"nats greater":      ">",
		"empty segment":     "environmental..temperature",
		"leading dot":       ".temperature",
		"four tokens":       "acme.gcs.environmental.temperature",
		"illegal byte":      "temp/erature",
	}
	for name, entityType := range cases {
		t.Run(name, func(t *testing.T) {
			executor, lister := listerFixture(t)
			result, err := executor.Execute(context.Background(), agentic.ToolCall{
				ID: "call-" + name, Name: "query_by_type",
				Arguments: map[string]any{"entity_type": entityType},
			})
			require.NoError(t, err)
			assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind, "entity_type %q must be refused", entityType)
			assert.Zero(t, lister.callCount, "the key lister is not invoked for a rejected entity_type")
		})
	}
}

// TestQueryByType_WithoutKeyListerIsLoud: a binding that cannot list keys
// cannot answer this question, and reporting zero matches would be a positive
// signal ("nothing of that type exists") the tool never established.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_WithoutKeyListerIsLoud(t *testing.T) {
	kv := newMockKVGetter()
	var _ KVGetter = kv
	_, isLister := any(kv).(KVKeyLister)
	require.False(t, isLister, "precondition: the plain getter must not satisfy KVKeyLister")

	executor := NewGraphQueryExecutor(kv)
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-nolister", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature"},
	})
	require.Error(t, err, "the executor contract carries the failure beside the result")
	assert.Equal(t, agentic.ToolErrorInternal, result.ErrorKind)
	assert.Contains(t, result.Error, "KVKeyLister", "the failure names the missing binding capability")
	assert.Contains(t, result.Error, "mockKVGetter", "and the binding that lacks it")
	assert.Empty(t, result.Content, "a classified internal error, never an empty listing")
}

// TestQueryByType_ListerFailureIsNotAnEmptyListing: a transient listing failure
// fails the call. The alternative — an empty result — is the fail-open shape.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_ListerFailureIsNotAnEmptyListing(t *testing.T) {
	executor, lister := listerFixture(t)
	lister.err = errors.New("kv unavailable")

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-listerr", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature"},
	})
	require.Error(t, err)
	assert.Equal(t, agentic.ToolErrorNetwork, result.ErrorKind)
	assert.NotEqual(t, agentic.HintEmpty, result.ResultHint)
}

// TestQueryByType_NonCanonicalKeyFailsClosed: a key in ENTITY_STATES that is
// not a canonical entity ID is authoritative-state corruption, and the listing
// refuses rather than skipping it. A skip would drop a row from a set the
// result reports as the complete match.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestQueryByType_NonCanonicalKeyFailsClosed(t *testing.T) {
	lister := &mockKVLister{
		mockKVGetter: newMockKVGetter(),
		// Five segments where a canonical entity ID has six. It carries no
		// entity-id-audit classify annotation on purpose: the audit extracts no
		// candidate from a bare slice element (measured 2026-09-09 — the audit
		// is green at 1317 candidates with this fixture present), and an
		// annotation that matches no candidate is itself an audit failure.
		keys: []string{fixtureTempOne, "acme.test.gcs.environmental.temperature"},
	}
	executor := NewGraphQueryExecutor(lister)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-badkey", Name: "query_by_type",
		Arguments: map[string]any{"entity_type": "temperature"},
	})
	require.Error(t, err, "the executor contract carries the failure beside the result")
	assert.Equal(t, agentic.ToolErrorInternal, result.ErrorKind)
	assert.Empty(t, result.Content, "no partial listing is emitted beside the refusal")
	assert.NotEqual(t, agentic.HintEmpty, result.ResultHint)
}

// ---------------------------------------------------------------------------
// query_neighbors
// ---------------------------------------------------------------------------

// fixtureNeighborDepth is the tool's ADVERTISED default, and these tests pass
// it explicitly so that a future change to the default cannot silently move
// what they exercise.
//
// It was 2 until the depth off-by-one was fixed (owner ruling 2026-09-09,
// #1261): the traversal spent ring 0 on the source itself, so the advertised
// default returned an empty neighbor map and the fixtures had to ask for one
// more hop than they meant. They no longer do — a test that has to over-ask
// to see a record is a test agreeing with a defect.
const fixtureNeighborDepth = float64(1)

// TestQueryNeighbors_DefaultDepthReturnsDirectNeighbors: the walk used to
// spend its entire budget on the seeding ring, so the ADVERTISED default
// answered "no neighbors" for an entity whose neighbor was one edge away.
//
// This calls the tool the way its schema documents it — with NO depth
// argument — because that is the shape the defect hid in: every other test
// here passed an explicit depth, and quietly passing 2 to mean 1 is what let
// it survive four review rounds.
//
// The hint assertion is the reason it was fixed rather than filed. Before
// this change the empty answer was merely uninformative; classified as
// HintEmpty it tells the model through a typed contract that the
// neighborhood is empty and the filter should be broadened. A silent
// under-answer is recoverable by a model that keeps looking. A confident
// false negative is not.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_DefaultDepthReturnsDirectNeighbors(t *testing.T) {
	kv := newMockKVGetter()
	kv.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne,
		relationshipTriple(fixtureSiteOne, "facility.site.holds", fixtureTempOne),
	))
	kv.Put(fixtureTempOne, entityFixture(t, fixtureTempOne,
		propertyTriple(fixtureTempOne, "sensor.temperature.celsius", 20.0)))
	executor := NewGraphQueryExecutor(kv)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-default-depth", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne},
	})
	require.NoError(t, err)
	require.Empty(t, result.Error)

	content := decodeContent(t, result)
	assert.Equal(t, float64(1), content["depth"],
		"precondition: an omitted depth is the advertised default of 1")
	assert.Equal(t, float64(1), content["count"])
	neighbors, ok := content["neighbors"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, neighbors, fixtureTempOne,
		"one edge out from the source is what depth 1 means")
	assert.NotEqual(t, agentic.HintEmpty, result.ResultHint,
		"an entity with a neighbor is never an empty neighborhood")
}

// TestQueryNeighbors_FilterTypeReadsIDSegment: before this, filter_type
// compared a `type` key the graph authority never writes, so the filter was
// advertised and inert.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_FilterTypeReadsIDSegment(t *testing.T) {
	kv := newMockKVGetter()
	kv.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne,
		relationshipTriple(fixtureSiteOne, "facility.site.holds", fixtureTempOne),
		relationshipTriple(fixtureSiteOne, "facility.site.holds", fixtureDroneOne),
	))
	kv.Put(fixtureTempOne, entityFixture(t, fixtureTempOne,
		propertyTriple(fixtureTempOne, "sensor.temperature.celsius", 20.0)))
	// The drone is filtered OUT of the answer and still carries an edge, so
	// the walk's expansion through it is observable rather than merely
	// asserted in a comment: the temperature it holds is two hops past the
	// filtered node.
	kv.Put(fixtureDroneOne, entityFixture(t, fixtureDroneOne,
		propertyTriple(fixtureDroneOne, "robotics.battery.level", 82.5),
		relationshipTriple(fixtureDroneOne, "robotics.drone.carries", fixtureTempTwo),
	))
	kv.Put(fixtureTempTwo, entityFixture(t, fixtureTempTwo,
		propertyTriple(fixtureTempTwo, "sensor.temperature.celsius", 31.5)))
	executor := NewGraphQueryExecutor(kv)

	unfiltered, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-nofilter", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
	})
	require.NoError(t, err)
	assert.Equal(t, float64(2), decodeContent(t, unfiltered)["count"], "precondition: both neighbors are reachable")

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-filter", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth, "filter_type": "temperature"},
	})
	require.NoError(t, err)
	content := decodeContent(t, result)
	assert.Equal(t, "*.*.*.*.temperature.*", content["pattern"],
		"filter_type is built by the same builder as entity_type — one grammar, not two")
	neighbors, ok := content["neighbors"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, neighbors, 1)
	assert.Contains(t, neighbors, fixtureTempOne)
	assert.NotContains(t, neighbors, fixtureDroneOne)

	t.Run("the walk expands THROUGH a filtered-out neighbor", func(t *testing.T) {
		// filter_type narrows the answer; it does not shorten the graph. The
		// drone is excluded from the result and is still traversed, so the
		// temperature it carries — reachable only through it — comes back.
		// Depth 2 is what the graph actually needs: the drone is one hop from
		// the site and the temperature it carries is two.
		deep, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-through", Name: "query_neighbors",
			Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": float64(2), "filter_type": "temperature"},
		})
		require.NoError(t, err)
		reached, ok := decodeContent(t, deep)["neighbors"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, reached, fixtureTempTwo,
			"the only path to this entity runs through the filtered-out drone; if the walk stopped at "+
				"the filter it would be unreachable")
		assert.NotContains(t, reached, fixtureDroneOne, "and the drone itself is still not returned")
	})

	t.Run("an unusable filter_type is refused", func(t *testing.T) {
		bad, err := executor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-badfilter", Name: "query_neighbors",
			Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth, "filter_type": "*"},
		})
		require.NoError(t, err)
		assert.Equal(t, agentic.ToolErrorInvalidArgs, bad.ErrorKind)
	})
}

// TestQueryNeighbors_UnresolvedTargetsAreReported: a target absent from
// ENTITY_STATES is named, never omitted — an omission reads to the model as
// "this edge does not exist". A transient read failure is a different class and
// fails the call.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_UnresolvedTargetsAreReported(t *testing.T) {
	const missing = "acme.test.gcs.environmental.temperature.gone"
	kv := newMockKVGetter()
	kv.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne,
		relationshipTriple(fixtureSiteOne, "facility.site.holds", fixtureTempOne),
		relationshipTriple(fixtureSiteOne, "facility.site.holds", missing),
	))
	kv.Put(fixtureTempOne, entityFixture(t, fixtureTempOne,
		propertyTriple(fixtureTempOne, "sensor.temperature.celsius", 20.0)))
	executor := NewGraphQueryExecutor(kv)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-unresolved", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
	})
	require.NoError(t, err)
	content := decodeContent(t, result)
	assert.Equal(t, []string{missing}, stringSlice(t, content["unresolved"]))
	neighbors, ok := content["neighbors"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, neighbors, missing, "unresolved and neighbors are disjoint")
	assert.Contains(t, neighbors, fixtureTempOne, "the resolvable side of the same walk is still returned")

	t.Run("all targets unresolved is not an empty neighborhood", func(t *testing.T) {
		// Zero neighbors WITH an unresolved list is the third state, and it is
		// not the empty one: HintEmpty would tell the model "nothing here,
		// broaden your filter" when the truth is "these targets are not
		// resident". The edges exist; their records do not.
		const alsoMissing = "acme.test.gcs.environmental.temperature.vanished"
		barren := newMockKVGetter()
		barren.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne,
			relationshipTriple(fixtureSiteOne, "facility.site.holds", missing),
			relationshipTriple(fixtureSiteOne, "facility.site.holds", alsoMissing),
		))
		result, err := NewGraphQueryExecutor(barren).Execute(context.Background(), agentic.ToolCall{
			ID: "call-allmissing", Name: "query_neighbors",
			Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
		})
		require.NoError(t, err)
		content := decodeContent(t, result)
		assert.Equal(t, float64(0), content["count"], "precondition: no target resolved")
		assert.ElementsMatch(t, []string{missing, alsoMissing}, stringSlice(t, content["unresolved"]),
			"precondition: and every one of them is named")
		assert.NotEqual(t, agentic.HintEmpty, result.ResultHint,
			"targets that are not resident are not an absence of targets")
	})

	t.Run("a genuinely empty neighborhood IS empty", func(t *testing.T) {
		// The counterpart, so the fix above cannot be "never classify empty":
		// an entity with no outgoing edges at all has an empty neighborhood
		// and says so.
		lonely := newMockKVGetter()
		lonely.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne,
			propertyTriple(fixtureSiteOne, "facility.site.label", "west")))
		result, err := NewGraphQueryExecutor(lonely).Execute(context.Background(), agentic.ToolCall{
			ID: "call-lonely", Name: "query_neighbors",
			Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
		})
		require.NoError(t, err)
		content := decodeContent(t, result)
		assert.Equal(t, float64(0), content["count"])
		assert.Empty(t, stringSlice(t, content["unresolved"]))
		assert.Equal(t, agentic.HintEmpty, result.ResultHint)
	})

	t.Run("a transient read failure fails the call", func(t *testing.T) {
		flaky := &flakyKVGetter{mockKVGetter: kv, failOn: fixtureTempOne, err: errors.New("nats: connection closed")}
		flakyExecutor := NewGraphQueryExecutor(flaky)
		bad, err := flakyExecutor.Execute(context.Background(), agentic.ToolCall{
			ID: "call-flaky", Name: "query_neighbors",
			Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
		})
		require.Error(t, err, "a transient failure is not a smaller graph reported as complete")
		assert.Equal(t, agentic.ToolErrorNetwork, bad.ErrorKind)
	})
}

// TestQueryNeighbors_AbsentSourceIsNotAnEmptyNeighborhood: classifying the
// zero this used to answer would have made it actively wrong — HintEmpty says
// "try a broader filter" for an entity that does not exist. It takes the same
// not-found classification query_entity and query_relationships give the same
// input.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_AbsentSourceIsNotAnEmptyNeighborhood(t *testing.T) {
	executor := NewGraphQueryExecutor(newMockKVGetter())
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-nosource", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
	})
	require.NoError(t, err)
	assert.Equal(t, agentic.ToolErrorNotFound, result.ErrorKind)
	assert.NotEqual(t, agentic.HintEmpty, result.ResultHint,
		"an absent entity is not an empty result set")
	assert.Empty(t, result.Content)
}

type flakyKVGetter struct {
	*mockKVGetter
	failOn string
	err    error
}

func (f *flakyKVGetter) Get(ctx context.Context, key string) (KVEntry, error) {
	if key == f.failOn {
		return nil, f.err
	}
	return f.mockKVGetter.Get(ctx, key)
}

// neighborBudgetFixture builds a hub whose neighbor records are each about a
// quarter of the budget, so three fit and the fourth does not.
func neighborBudgetFixture(t *testing.T) (*GraphQueryExecutor, []string) {
	t.Helper()
	const recordBytes = neighborMaxContentBytes / 4
	kv := newMockKVGetter()
	targets := make([]string, 0, 5)
	hubTriples := make([]message.Triple, 0, 5)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("acme.test.gcs.environmental.temperature.wide-%d", i)
		targets = append(targets, id)
		hubTriples = append(hubTriples, relationshipTriple(fixtureSiteOne, "facility.site.holds", id))
		kv.Put(id, entityFixture(t, id,
			propertyTriple(id, "sensor.temperature.note", strings.Repeat("x", recordBytes))))
	}
	kv.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne, hubTriples...))
	return NewGraphQueryExecutor(kv), targets
}

// TestQueryNeighbors_BudgetTruncatesWithHint: the walk measures the real bytes
// of the real records it assembled and stops before the next one crosses the
// cap — it does not predict a limit it would get wrong.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_BudgetTruncatesWithHint(t *testing.T) {
	executor, targets := neighborBudgetFixture(t)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-budget", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
	})
	require.NoError(t, err)

	content := decodeContent(t, result)
	neighbors, ok := content["neighbors"].(map[string]any)
	require.True(t, ok)
	assert.Less(t, len(neighbors), len(targets), "the walk stopped short of the whole frontier")
	assert.Equal(t, true, content["truncated"])
	assert.Equal(t, agentic.HintTooLarge, result.ResultHint)

	remaining, ok := content["frontier_remaining"].(float64)
	require.True(t, ok)
	assert.Greater(t, remaining, float64(0),
		"a truncated result always names at least the identity it could not fit")
	assert.Equal(t, float64(len(targets)-len(neighbors)), remaining)

	assert.LessOrEqual(t, len(result.Content), neighborMaxContentBytes,
		"the budget bounds the string the model receives")
}

// neighborWideFixture builds a hub with MANY SMALL neighbor records. Their raw
// bytes together stay under the budget — so the walk's read bound admits every
// one of them — and only the emitted, indented result crosses it.
//
// That is precisely the shape a proxy meter gets wrong. Indentation is nearly
// free on one long string (neighborBudgetFixture above) and expensive on many
// short structural lines, so metering the compact KV values would report this
// set as comfortably inside a cap the model receives it far outside of.
func neighborWideFixture(t *testing.T) (*GraphQueryExecutor, []string, int) {
	t.Helper()
	const wideRecords = 150
	kv := newMockKVGetter()
	targets := make([]string, 0, wideRecords)
	hubTriples := make([]message.Triple, 0, wideRecords)
	rawTotal := 0
	for i := 0; i < wideRecords; i++ {
		id := fmt.Sprintf("acme.test.gcs.environmental.temperature.small-%03d", i)
		targets = append(targets, id)
		hubTriples = append(hubTriples, relationshipTriple(fixtureSiteOne, "facility.site.holds", id))
		record := entityFixture(t, id, propertyTriple(id, "sensor.temperature.celsius", float64(i)))
		rawTotal += len(record)
		kv.Put(id, record)
	}
	kv.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne, hubTriples...))
	require.Less(t, rawTotal, neighborMaxContentBytes,
		"precondition: the raw records all fit, so any truncation this fixture produces "+
			"can only have come from measuring the emitted result")
	return NewGraphQueryExecutor(kv), targets, rawTotal
}

// TestQueryNeighbors_OverBudgetIsAlwaysSignalled covers the seam between the
// emitted-size trim and the unresolved/empty split. When every target is an
// edge but none is resident, nothing is ever admitted, so fitEmitted returns
// on its "nothing left to give back" arm without setting truncated, and the
// HintEmpty guard rightly declines because unresolved is non-empty. Without an
// explicit size check that leaves an over-cap body reported as fine — the same
// failure the budget exists to prevent, at a rarer input.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_OverBudgetIsAlwaysSignalled(t *testing.T) {
	const wideTargets = 2000
	kv := newMockKVGetter()
	hubTriples := make([]message.Triple, 0, wideTargets)
	for i := 0; i < wideTargets; i++ {
		// Named but never Put: an edge whose target is not resident.
		hubTriples = append(hubTriples, relationshipTriple(fixtureSiteOne, "facility.site.holds",
			fmt.Sprintf("acme.test.gcs.environmental.temperature.absent-%04d", i)))
	}
	kv.Put(fixtureSiteOne, entityFixture(t, fixtureSiteOne, hubTriples...))

	executor := NewGraphQueryExecutor(kv)
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-overbudget", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": float64(2)},
	})
	require.NoError(t, err)
	require.Empty(t, result.ErrorKind, "the walk succeeded; the source is resident")

	var content map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &content))
	require.Empty(t, content["neighbors"], "precondition: nothing was resident, so nothing was admitted")
	require.NotEmpty(t, content["unresolved"], "precondition: every target is reported unresolved")
	require.Greater(t, len(result.Content), neighborMaxContentBytes,
		"precondition: the unresolved envelope alone exceeds the cap, which is what makes this the seam")

	assert.Equal(t, agentic.HintTooLarge, result.ResultHint,
		"an over-budget body is signalled even when nothing was given back")
	assert.NotEqual(t, agentic.HintEmpty, result.ResultHint,
		"unresolved targets are not an empty neighborhood")
}

// TestQueryNeighbors_BudgetMetersTheEmittedResult: the cap bounds the string
// the model receives, not a proxy for it. The result ships as indented JSON
// that re-indents every embedded record, so a set measured on its compact
// bytes can be reported as inside a budget it is ~50% outside of.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_BudgetMetersTheEmittedResult(t *testing.T) {
	executor, targets, rawTotal := neighborWideFixture(t)

	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-emitted", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
	})
	require.NoError(t, err)

	assert.LessOrEqual(t, len(result.Content), neighborMaxContentBytes,
		"%d raw bytes of records — all of which fit the raw budget — emitted %d bytes",
		rawTotal, len(result.Content))

	content := decodeContent(t, result)
	neighbors, ok := content["neighbors"].(map[string]any)
	require.True(t, ok)
	assert.Less(t, len(neighbors), len(targets),
		"the emitted measurement gave records back; the raw one would have kept all %d", len(targets))
	assert.Equal(t, true, content["truncated"])
	assert.Equal(t, agentic.HintTooLarge, result.ResultHint)
	assert.Equal(t, float64(len(targets)-len(neighbors)), content["frontier_remaining"],
		"every record not returned is named as still owed, however it was dropped")
}

// TestQueryNeighbors_NeverAnnouncesContinuation: a traversal frontier is not a
// resumable position, so this tool reports width without ever claiming the
// caller can continue. A has_more with no token the model can pass back is
// worse than silence — the loop would render "pass the continuation token"
// for a token that does not exist.
//
// spec: agentic-tools / query_neighbors bounds its content by a model-facing budget and reports unresolved targets
func TestQueryNeighbors_NeverAnnouncesContinuation(t *testing.T) {
	executor, _ := neighborBudgetFixture(t)
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call-nocont", Name: "query_neighbors",
		Arguments: map[string]any{"entity_id": fixtureSiteOne, "depth": fixtureNeighborDepth},
	})
	require.NoError(t, err)
	require.Equal(t, agentic.HintTooLarge, result.ResultHint, "precondition: this result IS truncated")

	assert.NotContains(t, result.Metadata, agentic.MetadataKeyHasMore)
	assert.NotContains(t, result.Metadata, agentic.MetadataKeyNextCursor)
	assert.NotContains(t, result.Metadata, agentic.MetadataKeyNextOffset)

	for _, def := range NewGraphQueryExecutor(newMockKVGetter()).ListTools() {
		if def.Name == "query_neighbors" {
			assert.False(t, def.Paginated, "query_neighbors must not declare a contract it cannot honour")
		}
		if def.Name == "query_by_type" {
			assert.True(t, def.Paginated, "query_by_type does declare it, and sets has_more on every result")
		}
	}
}

// ---------------------------------------------------------------------------
// pattern builder
// ---------------------------------------------------------------------------

// TestBuildTypePattern_RightAnchorsSegments pins the position arithmetic
// directly, so a shift in the ADR-102 order is a named failure rather than a
// wrong result set two tools down.
//
// spec: agentic-tools / query_by_type lists entity identities by the ADR-102 type segment through the existing filtered key listing
func TestBuildTypePattern_RightAnchorsSegments(t *testing.T) {
	cases := map[string]string{
		"temperature":                   "*.*.*.*.temperature.*",
		"environmental.temperature":     "*.*.*.environmental.temperature.*",
		"gcs.environmental.temperature": "*.*.gcs.environmental.temperature.*",
	}
	for input, want := range cases {
		got, err := buildTypePattern(input)
		require.NoErrorf(t, err, "entity_type %q", input)
		assert.Equal(t, want, got)
		require.NoError(t, semtypes.ValidateEntityIDPattern(got))
	}
}
