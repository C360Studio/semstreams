package graph

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// mustMarshal fails the test rather than returning an error, so a table entry
// that stops marshalling is a test failure and not a silently skipped case.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}

// TestUnwrapQueryResponse_RecognizesTheEnvelope covers the positive direction:
// a marshalled QueryResponse is unwrapped to exactly its data payload.
func TestUnwrapQueryResponse_RecognizesTheEnvelope(t *testing.T) {
	tests := []struct {
		name string
		resp any
		want string
	}{
		{
			name: "summary data (gh#762's motivating instance)",
			resp: NewQueryResponse(SummaryData{TotalEntities: 42}),
			want: `"total_entities":42`,
		},
		{
			name: "name data (reached via the graph.query.byName proxy)",
			resp: NewQueryResponse(NameData{Matches: []NameMatch{}}),
			want: `"matches":[]`,
		},
		{
			name: "predicate list data",
			resp: NewQueryResponse(PredicateListData{}),
			want: `{`,
		},
		{
			name: "envelope carrying request_id (the optional third key)",
			resp: QueryResponse[SummaryData]{
				Data:      SummaryData{TotalEntities: 7},
				RequestID: "req-1",
				Timestamp: time.Now(),
			},
			want: `"total_entities":7`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := mustMarshal(t, tc.resp)

			got, unwrapped := UnwrapQueryResponse(raw)
			if !unwrapped {
				t.Fatalf("expected the envelope to be recognized; got raw back\n  input: %s", raw)
			}
			if !bytes.Contains(got, []byte(tc.want)) {
				t.Errorf("unwrapped payload missing %q\n  got: %s", tc.want, got)
			}
			// The payload must be the DATA, not the envelope — assert the
			// envelope's own keys are gone rather than only that the leaf is
			// reachable. Reaching the leaf is exactly what the double-nested
			// shape also permits.
			if bytes.Contains(got, []byte(`"timestamp"`)) {
				t.Errorf("envelope key \"timestamp\" survived unwrapping\n  got: %s", got)
			}
		})
	}
}

// TestUnwrapQueryResponse_NoCollisionWithRealResponseTypes is task 2.2 of the
// gateway-response-envelope-detection change, and the discharge of its one
// data-loss risk.
//
// Every response type actually served through the gateway's projection path is
// asserted NOT to be detected as an envelope. A collision here would mean a
// real payload silently loses a nesting level, which is strictly worse than the
// double-nesting defect being fixed.
//
// Mutation check: replacing the closed-key-set loop in UnwrapQueryResponse with
// a bare `data` presence test turns the "envelope-shaped plus a foreign key"
// and "data without timestamp" cases RED.
func TestUnwrapQueryResponse_NoCollisionWithRealResponseTypes(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{
			// Task 2.3: PrefixQueryResponse is its own struct, NOT a
			// QueryResponse[T], and must fail on BOTH required keys.
			name: "PrefixQueryResponse (graph.query.prefix, has its own unwrap path)",
			raw:  mustMarshal(t, PrefixQueryResponse{}),
		},
		{
			name: "EntityBatchResponse (graph.query.batch)",
			raw:  mustMarshal(t, EntityBatchResponse{}),
		},
		{
			name: "NameData unwrapped (already-flat payload)",
			raw:  mustMarshal(t, NameData{Matches: []NameMatch{}}),
		},
		{
			name: "SummaryData unwrapped (already-flat payload)",
			raw:  mustMarshal(t, SummaryData{TotalEntities: 3}),
		},
		{
			name: "AliasData unwrapped",
			raw:  mustMarshal(t, AliasData{}),
		},
		{
			name: "OutgoingRelationshipsData unwrapped",
			raw:  mustMarshal(t, OutgoingRelationshipsData{}),
		},
		{
			// graph.embedding.query.similar's shape, by field names.
			name: "SimilarResponse shape (graph.query.similar)",
			raw:  []byte(`{"entity_id":"a.b.c.d.e.f","similar":[],"duration":"1ms"}`),
		},
		{
			name: "a bare JSON array (spatial/temporal results)",
			raw:  []byte(`[{"id":"a"},{"id":"b"}]`),
		},
		{
			name: "a JSON scalar",
			raw:  []byte(`42`),
		},
		{
			name: "malformed JSON",
			raw:  []byte(`{not json`),
		},

		// The three shapes that make the discriminator CLOSED rather than
		// permissive. Each would be unwrapped by a `has("data")` test.
		{
			name: "legitimate top-level data field, no timestamp",
			raw:  []byte(`{"data":{"inner":1}}`),
		},
		{
			name: "data and timestamp ALONGSIDE a foreign key",
			raw:  []byte(`{"data":{"inner":1},"timestamp":"2026-07-31T00:00:00Z","total":5}`),
		},
		{
			name: "timestamp without data",
			raw:  []byte(`{"timestamp":"2026-07-31T00:00:00Z"}`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, unwrapped := UnwrapQueryResponse(tc.raw)
			if unwrapped {
				t.Fatalf("FALSE POSITIVE — this type was detected as the envelope "+
					"and would silently lose a nesting level in production\n  input: %s\n  would serve: %s",
					tc.raw, got)
			}
			if !bytes.Equal(got, tc.raw) {
				t.Errorf("non-envelope input must be returned byte-for-byte\n  in:  %s\n  out: %s", tc.raw, got)
			}
		})
	}
}

// TestUnwrapQueryResponse_RemovesExactlyOneLayer pins design decision D3.
//
// A payload whose own contents are envelope-shaped must survive. Unwrapping
// "while it still looks like an envelope" would make the number of layers
// removed depend on user data, so the same query would project differently for
// different entities.
func TestUnwrapQueryResponse_RemovesExactlyOneLayer(t *testing.T) {
	inner := map[string]any{
		"data":      map[string]any{"deep": true},
		"timestamp": "2026-07-31T00:00:00Z",
	}
	raw := mustMarshal(t, NewQueryResponse(inner))

	got, unwrapped := UnwrapQueryResponse(raw)
	if !unwrapped {
		t.Fatalf("outer envelope not recognized: %s", raw)
	}

	// Exactly one layer came off: the inner object is delivered intact,
	// including its own data/timestamp keys.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unwrapped payload is not an object: %s", got)
	}
	if _, ok := payload["data"]; !ok {
		t.Errorf("inner \"data\" was stripped — more than one layer removed\n  got: %s", got)
	}
	if _, ok := payload["timestamp"]; !ok {
		t.Errorf("inner \"timestamp\" was stripped — more than one layer removed\n  got: %s", got)
	}
	if !bytes.Contains(got, []byte(`"deep":true`)) {
		t.Errorf("inner payload lost\n  got: %s", got)
	}
}

// TestQueryResponse_HasNoErrorField guards ADR-060 and task 2.5.
//
// The in-body Error field was REMOVED by ADR-060: a query reply is either this
// success body or a classified error on the err channel. The gateway carried an
// `Error string` branch for years afterward that no producer could satisfy.
// If a future change reintroduces the field, this fails and the gateway's
// deleted branch has to be reconsidered deliberately rather than by accident.
func TestQueryResponse_HasNoErrorField(t *testing.T) {
	raw := mustMarshal(t, NewQueryResponse(SummaryData{}))

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := fields["error"]; present {
		t.Fatalf("QueryResponse marshalled an \"error\" key; ADR-060 removed it. "+
			"The gateway's deleted error branch must be reconsidered.\n  got: %s", raw)
	}

	// The closed key set must stay in sync with the struct — a field added to
	// QueryResponse without updating the constants would make the discriminator
	// stop recognizing its own envelope.
	for key := range fields {
		switch key {
		case queryResponseDataKey, queryResponseRequestIDKey, queryResponseTimestampKey:
		default:
			t.Errorf("QueryResponse marshals key %q, absent from the closed key set "+
				"used by UnwrapQueryResponse — add it there or detection breaks", key)
		}
	}
}
