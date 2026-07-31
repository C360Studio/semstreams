package graphgateway

import (
	"encoding/json"
	"net/http/httptest"
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
		return // a non-object projection cannot carry a repeated hop
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
// decision is made from the reply (see the cross-family test below).
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

// TestGateway_EnvelopeProjectsIdenticallyAcrossFamilies is the REQUIRED
// cross-family test (task 4.5), and the one that actually falsifies a
// subject-keyed implementation.
//
// The mechanism it covers: graph-query's byName handler PROXIES to
// graph.index.query.byName and returns that reply verbatim, so a graph-index
// envelope reaches the gateway under a graph.query.* subject. Testing the two
// families SEPARATELY would pass under the old prefix gate and prove nothing —
// the gate unwrapped one family and not the other, and each family's own test
// would simply encode whatever that family currently did.
//
// Asserting that ONE payload projects IDENTICALLY under both subjects is what
// makes the claim "the decision comes from the reply, not the subject"
// falsifiable: under the prefix gate these two differ by exactly one `data` hop.
func TestGateway_EnvelopeProjectsIdenticallyAcrossFamilies(t *testing.T) {
	t.Parallel()

	// One reply, produced by graph-index, reachable under either subject.
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

// TestGateway_PrefixKeepsItsOwnUnwrapPath pins design decision D4 and task 4.3.
//
// graph.query.prefix returns PrefixQueryResponse — its own struct, NOT a
// QueryResponse[T] — and has a separate unwrap producing a bare array. Envelope
// detection must not claim it, and the ORDER of the two steps is pinned here by
// test rather than left to reading, so a future field addition breaks a test
// instead of a deployment.
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
