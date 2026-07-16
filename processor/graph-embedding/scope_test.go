package graphembedding

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/c360studio/semstreams/graph"
)

// TestSearchRequest_ScopeRoundTrip is the production-decoder round-trip
// (feedback_production_decoder_round_trip_required): a SearchRequest carrying a
// scope decodes through the SAME json.Unmarshal path graph.embedding.query.search
// uses, and the decoded scope drives the candidate filter both similarity paths
// share (graph.MatchesAnyIDPrefix). Closes the warning-not-fail gap — no
// anonymous shape-cast, the real request type.
func TestSearchRequest_ScopeRoundTrip(t *testing.T) {
	raw := []byte(`{"query":"what exceptions can be raised","limit":40,` +
		`"scope":["c360.semspec.source.doc","c360.semspec.source.chunk"]}`)

	var req SearchRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Query != "what exceptions can be raised" || req.Limit != 40 {
		t.Fatalf("query/limit decoded wrong: %+v", req)
	}
	want := []string{"c360.semspec.source.doc", "c360.semspec.source.chunk"}
	if !slices.Equal(req.Scope, want) {
		t.Fatalf("scope = %v, want %v", req.Scope, want)
	}

	// The decoded scope drives the same filter both paths apply.
	corpus := []string{
		"c360.semspec.python.pkg.fn.test_raises",         // code — out of scope
		"c360.semspec.source.doc.page.exceptions",        // in scope
		"c360.semspec.source.chunk.segment.exceptions_0", // in scope
		"c360.semspec.golang.pkg.fn.Handle",              // code — out of scope
	}
	var kept []string
	for _, id := range corpus {
		if graph.MatchesAnyIDPrefix(id, req.Scope) {
			kept = append(kept, id)
		}
	}
	wantKept := []string{"c360.semspec.source.doc.page.exceptions", "c360.semspec.source.chunk.segment.exceptions_0"}
	if !slices.Equal(kept, wantKept) {
		t.Errorf("filtered corpus = %v, want %v", kept, wantKept)
	}
}

// TestSearchRequest_UnscopedDecode: an absent scope decodes to nil, so an
// unscoped search is byte-for-byte today's behavior; an un-migrated server that
// lacks the field (plain json.Unmarshal, no DisallowUnknownFields) also decodes
// a scoped request and degrades to unscoped.
func TestSearchRequest_UnscopedDecode(t *testing.T) {
	var req SearchRequest
	if err := json.Unmarshal([]byte(`{"query":"x","limit":10}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Scope != nil {
		t.Errorf("scope = %v, want nil (unscoped)", req.Scope)
	}
}
