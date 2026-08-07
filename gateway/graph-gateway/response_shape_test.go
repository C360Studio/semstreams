package graphgateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/stretchr/testify/require"
)

// These helpers return RAW JSON on purpose. The wrapped and unwrapped shapes
// both unmarshal cleanly into any permissive target, so a test that decodes
// into a struct before asserting passes under the defect AND under the fix —
// which is precisely how gh#762 survived for weeks. Every assertion below is
// over JSON keys.
//
// Not every subject has a GraphQL field mapping: subjectToGraphQLField returns
// "" for unmapped subjects (graph.query.byName among them), and the payload is
// then spread directly under `data` rather than under a named field. This
// returns the raw `data` member so both cases are handled without assuming a
// wrapper that may not exist.
func projectData(t *testing.T, subject string, reply []byte) []byte {
	t.Helper()

	comp := createTestGateway(t)
	recorder := httptest.NewRecorder()
	comp.handleNATSResponse(recorder, subject, reply)

	var body struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body),
		"gateway response was not valid GraphQL JSON: %s", recorder.Body.String())
	require.NotEmpty(t, body.Data, "no data member: %s", recorder.Body.String())
	return body.Data
}

// projectField returns the single named GraphQL field's raw JSON, for subjects
// that HAVE a field mapping.
func projectField(t *testing.T, subject string, reply []byte) (fieldName string, fieldJSON []byte) {
	t.Helper()

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projectData(t, subject, reply), &fields))
	require.Len(t, fields, 1, "expected exactly one projected field")

	for name, raw := range fields {
		return name, raw
	}
	return "", nil
}

// requireNoRepeatedDataHop asserts the ABSENCE of the defect, not merely the
// presence of the expected leaf. The double-nested shape ALSO makes the leaf
// reachable — one hop deeper — so asserting reachability alone cannot tell the
// defect from the fix.
func requireNoRepeatedDataHop(t *testing.T, fieldJSON []byte) {
	t.Helper()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(fieldJSON, &fields); err != nil {
		// A non-object projection cannot carry a repeated hop, but every
		// current caller passes an object — so a regression that turned a
		// field into an array or scalar would satisfy this guard by DEFAULT
		// rather than by assertion. Fail instead of returning quietly.
		t.Fatalf("expected an object projection to check for a repeated `data` hop; got %s", fieldJSON)
	}
	require.NotContains(t, fields, "data",
		"projected field still carries a `data` hop — this is the gh#762 double-nesting: %s", fieldJSON)
	require.NotContains(t, fields, "timestamp",
		"projected field still carries the envelope's `timestamp`: %s", fieldJSON)
}

// TestGateway_SummaryProjectsUnwrapped is gh#762's motivating instance: a
// caller must read data.graphSummary.total_entities, never
// data.graphSummary.data.total_entities.
//
// It is a test case, not the acceptance criterion — the criterion is that the
// decision is made from the reply (TestGateway_UnwrapIsSubjectInvariant), over
// the full reachable inventory (TestGateway_EveryRoutedSubjectHasAShapeCase).
func TestGateway_SummaryProjectsUnwrapped(t *testing.T) {
	t.Parallel()

	reply, err := json.Marshal(graph.NewQueryResponse(graph.SummaryData{TotalEntities: 42}))
	require.NoError(t, err)

	field, projected := projectField(t, "graph.query.summary", reply)
	require.Equal(t, "graphSummary", field)
	requireNoRepeatedDataHop(t, projected)

	var summary map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projected, &summary))
	require.Contains(t, summary, "total_entities",
		"total_entities must be at the top level of the field: %s", projected)
}

// TestGateway_UnwrapIsSubjectInvariant asserts the CONTRACT the fix rests on:
// one reply projects identically no matter which subject served it.
//
// SCOPE, stated honestly (Codex review, 2026-07-31). An earlier version of this
// test claimed to exercise a live production proxy path via
// `graph.query.byName`. It does not, and cannot: `graph.query.byName` is NOT
// reachable through the GraphQL gateway — `mapGraphQLQueryToNATSSubject` has no
// byName branch, `subjectToGraphQLField` has no case, and the schema exposes no
// such field (it IS served over NATS, but NATS callers bypass this gateway
// entirely). The subjects below are therefore driven through the projection
// helper SYNTHETICALLY. This is subject-invariance coverage, not proof of a
// reachable proxy instance, and no such reachable instance exists today —
// see TestGateway_EveryRoutedSubjectHasAShapeCase for the reachable inventory.
//
// It is still the test that falsifies a subject-keyed implementation: under the
// old prefix gate these two projections differ by exactly one `data` hop. The
// live evidence that the families do not partition by envelope usage is
// `graph.query.summary`, which is reachable, is served by graph-query's OWN
// handler (not a proxy), and returns the envelope — covered above.
func TestGateway_UnwrapIsSubjectInvariant(t *testing.T) {
	t.Parallel()

	reply, err := json.Marshal(graph.NewQueryResponse(graph.NameData{
		Matches: []graph.NameMatch{{EntityID: "acme.ops.test.system.widget.001"}},
	}))
	require.NoError(t, err)

	viaIndexFamily := projectData(t, "graph.index.query.byName", reply)
	viaQueryFamily := projectData(t, "graph.query.byName", reply)

	requireNoRepeatedDataHop(t, viaIndexFamily)
	requireNoRepeatedDataHop(t, viaQueryFamily)

	require.JSONEq(t, string(viaIndexFamily), string(viaQueryFamily),
		"the same envelope projected differently depending on which subject served it — "+
			"the unwrap decision is still coming from the subject, not the reply")

	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(viaQueryFamily, &payload))
	require.Contains(t, payload, "matches",
		"payload must be the NameData itself: %s", viaQueryFamily)
}

// routedSubjectShape is one gateway-REACHABLE subject and the marshalled shape
// its producer actually returns.
type routedSubjectShape struct {
	subject string
	// producer cites where the shape comes from, so a reader can audit the
	// case rather than trust it.
	producer string
	// reply is the producer's marshalled output. Real Go types are used where
	// the type is exported and constructible here; where the producer builds an
	// anonymous map or slice, the shape is written out with its citation.
	reply []byte
	// expectUnwrap is whether this reply IS the QueryResponse envelope.
	expectUnwrap bool
}

func routedSubjectShapes() []routedSubjectShape {
	env := func(v any) []byte {
		b, err := json.Marshal(graph.NewQueryResponse(v))
		if err != nil {
			panic(err)
		}
		return b
	}
	raw := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		return b
	}

	return []routedSubjectShape{
		// --- ENVELOPE producers -------------------------------------------
		{"graph.query.summary", "processor/graph-query/summary.go:118 NewQueryResponse(SummaryData)",
			env(graph.SummaryData{TotalEntities: 3}), true},
		{"graph.index.query.predicateList", "processor/graph-index/query.go:539 NewQueryResponse(PredicateListData)",
			env(graph.PredicateListData{}), true},
		{"graph.index.query.predicateStats", "processor/graph-index/query.go:605 NewQueryResponse(PredicateStatsData)",
			env(graph.PredicateStatsData{}), true},
		{"graph.index.query.predicate", "processor/graph-index/query.go:390 NewQueryResponse(PredicateData)",
			env(graph.PredicateData{}), true},
		{"graph.index.query.predicateCompound", "processor/graph-index/query.go NewQueryResponse(CompoundPredicateData)",
			env(graph.CompoundPredicateData{}), true},

		// --- NON-envelope producers ---------------------------------------
		{"graph.query.entity", "graph.ingest.query.entity -> graph-ingest, exact entity plus KV revision",
			raw(graph.ExactEntity{Entity: &graph.EntityState{ID: "acme.ops.test.system.widget.001"}, KVRevision: 1}), false},
		{"graph.query.entityByAlias", "processor/graph-query/query.go alias->exact entity plus KV revision",
			raw(graph.ExactEntity{Entity: &graph.EntityState{ID: "acme.ops.test.system.widget.001"}, KVRevision: 1}), false},
		{"graph.query.prefix", "processor/graph-ingest/query.go PrefixQueryResponse{entities,next_cursor}",
			raw(graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: "acme.ops.test.system.widget.001"}}}), false},
		{"graph.query.relationships", "processor/graph-query/query.go:342 decodes the index envelope, re-marshals the array",
			[]byte(`{"outgoing":[],"incoming":[]}`), false},
		{"graph.query.pathSearch", "processor/graph-query/query.go handlePathSearch, json.Marshal(result) map",
			[]byte(`{"paths":[],"total":0}`), false},
		{"graph.query.hierarchyStats", "processor/graph-query/query.go handleQueryHierarchyStats, json.Marshal(result) map",
			[]byte(`{"prefix":"","totalEntities":0,"children":[]}`), false},
		{"graph.query.spatial", "processor/graph-index-spatial/query.go:103 json.Marshal(results) slice",
			[]byte(`[{"id":"acme.ops.test.system.widget.001"}]`), false},
		{"graph.query.temporal", "processor/graph-index-temporal/query.go:82 json.Marshal(results) slice",
			[]byte(`[{"id":"acme.ops.test.system.widget.001"}]`), false},
		{"graph.query.semantic", "graph.embedding.query.search -> processor/graph-embedding/query.go:168 SearchResponse",
			[]byte(`{"query":"q","results":[],"duration":"1ms"}`), false},
		{"graph.query.similar", "graph.embedding.query.similar -> processor/graph-embedding/query.go:246 SimilarResponse",
			[]byte(`{"entity_id":"acme.ops.test.system.widget.001","similar":[],"duration":"1ms"}`), false},
		{"graph.query.globalSearch", "processor/graph-query/graphrag.go GlobalSearch result",
			[]byte(`{"answer":"","sources":[],"count":0}`), false},
		{"graph.query.localSearch", "processor/graph-query/graphrag.go:219 handleLocalSearch",
			[]byte(`{"answer":"","sources":[],"count":0}`), false},
		{"graph.query.searchGraph", "processor/graph-query/query.go handleSearchGraph, wraps globalSearch",
			[]byte(`{"answer":"","sources":[],"count":0}`), false},
		{"agentic.query.trajectory", "processor/agentic-loop/component.go:1873 json.Marshal(traj)",
			[]byte(`{"loop_id":"loop-1","steps":[],"status":"complete"}`), false},

		// --- Routed with NO producer --------------------------------------
		// graph.query.capabilities is routed by the gateway but no component
		// subscribes to it (gh#784), so no reply shape exists to classify.
		// graph.query.unknown is the routing fallback, likewise never served.
	}
}

// subjectsWithNoProducer are routed by the gateway but served by nobody, so
// they have no reply shape. Enumerated explicitly so the completeness guard
// below cannot be satisfied by silently forgetting them.
var subjectsWithNoProducer = map[string]string{
	"graph.query.capabilities": "no component subscribes — gh#784",
	"graph.query.unknown":      "routing fallback, never served",
}

// TestGateway_EveryRoutedSubjectHasAShapeCase makes the collision inventory
// DRIFT-PROOF, which the previous hand-written table was not.
//
// Codex review (2026-07-31) found that table both incomplete (missing
// EntityState, path/local/global search, hierarchy, embedding and trajectory
// shapes) and polluted (it included EntityBatchResponse from
// `graph.query.batch`, which the gateway does not route). The root cause was
// methodological: it was enumerated from graph-query's registration table and
// the static router, NOT from the gateway's own reachable set.
//
// This scrapes the resolved-family selectors and operation suffixes out of
// mapGraphQLQueryToNATSSubject — the single authority on what this gateway can
// produce — and fails if any route lacks a classified shape case. Adding a
// route without adding a case turns it RED, so the claim "every routed shape
// is covered" stays true instead of decaying into a comment. Source-scraping
// follows the precedent in processor/agentic-loop/inflight_test.go:135.
func TestGateway_EveryRoutedSubjectHasAShapeCase(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("component.go")
	require.NoError(t, err, "read gateway source")

	fn := string(src)
	start := strings.Index(fn, "func (c *Component) mapGraphQLQueryToNATSSubject")
	require.Positive(t, start, "routing function not found — this guard has stopped guarding")
	body := fn[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}

	routed := map[string]bool{}
	// Routes now derive their family from resolved port Facts. Scrape the
	// family selector plus operation suffix, then materialize the default
	// contract used by the shape inventory.
	defaultFamilies := map[string]string{
		"graph":   "graph.query.*",
		"index":   "graph.index.query.*",
		"agentic": "agentic.query.*",
	}
	for _, match := range regexp.MustCompile(`querySubject\(c\.queries\.(graph|index|agentic), "([a-zA-Z]+)"\)`).FindAllStringSubmatch(body, -1) {
		routed[querySubject(defaultFamilies[match[1]], match[2])] = true
	}
	require.GreaterOrEqual(t, len(routed), 15,
		"scraped only %d subjects from mapGraphQLQueryToNATSSubject — the regex has drifted "+
			"from the source and this guard is silently checking a fraction of the routes: %v",
		len(routed), routed)

	covered := map[string]bool{}
	for _, c := range routedSubjectShapes() {
		covered[c.subject] = true
	}

	for subject := range routed {
		if _, exempt := subjectsWithNoProducer[subject]; exempt {
			continue
		}
		require.True(t, covered[subject],
			"gateway routes %q but no shape case classifies its reply — add one to "+
				"routedSubjectShapes(), or record it in subjectsWithNoProducer with a reason. "+
				"An unclassified route is exactly how the detector's silent-flattening risk "+
				"would go undischarged.", subject)
	}

	// The reverse direction: a case for a subject the gateway cannot produce is
	// dead weight that inflates the coverage claim (the EntityBatchResponse
	// mistake). graph.index.query.byName is deliberately absent from the table
	// for this reason.
	for _, c := range routedSubjectShapes() {
		require.True(t, routed[c.subject],
			"shape case for %q, which mapGraphQLQueryToNATSSubject cannot produce — "+
				"remove it or the inventory overstates its own coverage", c.subject)
	}
}

// TestGateway_RoutedShapesClassifyCorrectly runs every reachable subject's real
// reply through the production projection path and asserts the outcome.
//
// This is the actual discharge of the detector's one data-loss risk: a
// non-envelope reply that gets unwrapped loses a nesting level silently.
func TestGateway_RoutedShapesClassifyCorrectly(t *testing.T) {
	t.Parallel()

	for _, tc := range routedSubjectShapes() {
		t.Run(tc.subject, func(t *testing.T) {
			t.Parallel()

			_, unwrapped := graph.UnwrapQueryResponse(tc.reply)
			require.Equal(t, tc.expectUnwrap, unwrapped,
				"envelope detection misclassified %s\n  producer: %s\n  reply: %s",
				tc.subject, tc.producer, tc.reply)

			projected := projectData(t, tc.subject, tc.reply)
			if tc.expectUnwrap {
				requireNoRepeatedDataHop(t, projected)
				return
			}
			// A non-envelope reply must survive the projection path intact.
			// graph.query.prefix is the one exception: it has its own unwrap,
			// asserted separately in TestGateway_PrefixKeepsItsOwnUnwrapPath.
			if tc.subject == "graph.query.prefix" {
				return
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(projected, &fields); err == nil && len(fields) == 1 {
				for _, inner := range fields {
					require.JSONEq(t, string(tc.reply), string(inner),
						"non-envelope reply for %s was altered in projection\n  producer: %s",
						tc.subject, tc.producer)
					return
				}
			}
			require.JSONEq(t, string(tc.reply), string(projected),
				"non-envelope reply for %s was altered in projection\n  producer: %s",
				tc.subject, tc.producer)
		})
	}
}

// TestGateway_NonEnvelopeRepliesAreUntouched covers the regression half: this
// change must not fix one family by breaking the others. A reply that is not
// the envelope reaches the caller byte-for-byte.
func TestGateway_NonEnvelopeRepliesAreUntouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subject string
		reply   []byte
	}{
		{
			name:    "payload with a legitimate top-level data field",
			subject: "graph.query.relationships",
			reply:   []byte(`{"data":{"inner":1}}`),
		},
		{
			name:    "envelope-shaped payload carrying a foreign key",
			subject: "graph.query.relationships",
			reply:   []byte(`{"data":{"inner":1},"timestamp":"2026-07-31T00:00:00Z","total":5}`),
		},
		{
			name:    "bare array (spatial/temporal results)",
			subject: "graph.query.spatial",
			reply:   []byte(`[{"id":"a"}]`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, projected := projectField(t, tc.subject, tc.reply)
			require.JSONEq(t, string(tc.reply), string(projected),
				"a non-envelope reply must reach the caller unchanged")
		})
	}
}

// TestGateway_PrefixUnwrapOrderIsLoadBearing pins design decision D4 and task
// 4.3 — the ORDER of envelope detection and the graph.query.prefix unwrap.
//
// TestGateway_PrefixKeepsItsOwnUnwrapPath below does NOT pin the order, despite
// an earlier comment claiming it did (found by review, proven by mutation:
// swapping the two blocks in component.go left the whole package green). With
// an unwrapped PrefixQueryResponse the orders are indistinguishable — the
// prefix unwrap yields a bare array, which detection then declines because it
// is not an object.
//
// The order only becomes load-bearing on an ENVELOPED prefix reply, which is
// the scenario the design's "future field addition" caveat is about. Under the
// wrong order, validateAndUnwrapPrefixResponse decodes {"data":…,"timestamp":…}
// as an empty PrefixQueryResponse, hits its len(Entities)==0 branch, returns
// the envelope unchanged, and the caller receives an OBJECT where the contract
// promises an array. This test feeds exactly that reply.
func TestGateway_PrefixUnwrapOrderIsLoadBearing(t *testing.T) {
	t.Parallel()

	reply, err := json.Marshal(graph.NewQueryResponse(graph.PrefixQueryResponse{
		Entities: []graph.EntityState{{ID: "acme.ops.test.system.widget.001"}},
	}))
	require.NoError(t, err)

	_, projected := projectField(t, "graph.query.prefix", reply)

	var entities []json.RawMessage
	require.NoError(t, json.Unmarshal(projected, &entities),
		"an enveloped prefix reply must still project a bare array — detection has to run "+
			"BEFORE the prefix unwrap, or the envelope reaches validateAndUnwrapPrefixResponse, "+
			"decodes as zero entities, and is returned as an object: %s", projected)
	require.Len(t, entities, 1)
}

// TestGateway_PrefixKeepsItsOwnUnwrapPath asserts the narrower fact its name
// promises: detection does not claim PrefixQueryResponse, and the prefix path
// still yields an array. The ORDER is pinned by the test above, not this one.
func TestGateway_PrefixKeepsItsOwnUnwrapPath(t *testing.T) {
	t.Parallel()

	reply, err := json.Marshal(graph.PrefixQueryResponse{
		Entities: []graph.EntityState{{ID: "acme.ops.test.system.widget.001"}},
	})
	require.NoError(t, err)

	// The reply must not be envelope-shaped in the first place.
	_, unwrapped := graph.UnwrapQueryResponse(reply)
	require.False(t, unwrapped,
		"PrefixQueryResponse was detected as the query envelope; detection is too permissive")

	field, projected := projectField(t, "graph.query.prefix", reply)
	require.Equal(t, "entitiesByPrefix", field)

	// Its own unwrap still ran: the projection is the bare entities array.
	var entities []json.RawMessage
	require.NoError(t, json.Unmarshal(projected, &entities),
		"graph.query.prefix must still project a bare array: %s", projected)
	require.Len(t, entities, 1)
}

// TestGateway_EnvelopeWithRequestIDProjectsUnwrapped covers the optional third
// key: request_id is allowed by the closed set, so an envelope carrying it is
// still an envelope.
func TestGateway_EnvelopeWithRequestIDProjectsUnwrapped(t *testing.T) {
	t.Parallel()

	reply, err := json.Marshal(graph.QueryResponse[graph.SummaryData]{
		Data:      graph.SummaryData{TotalEntities: 9},
		RequestID: "req-42",
		Timestamp: time.Now(),
	})
	require.NoError(t, err)

	_, projected := projectField(t, "graph.query.summary", reply)
	requireNoRepeatedDataHop(t, projected)

	var summary map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(projected, &summary))
	require.Contains(t, summary, "total_entities")
	require.NotContains(t, summary, "request_id", "envelope metadata must not leak into the payload")
}
