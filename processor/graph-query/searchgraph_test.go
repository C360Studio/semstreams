package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestSearchGraphResponseEmpty covers the empty-detection predicate
// used to decide whether to fire the semantic fallback. Mirrors
// semspec's globalSearchEmpty contract from tools/workflow/graph.go.
func TestSearchGraphResponseEmpty(t *testing.T) {
	tests := []struct {
		name     string
		response GlobalSearchResponse
		want     bool
	}{
		{
			name:     "zero-valued response is empty",
			response: GlobalSearchResponse{},
			want:     true,
		},
		{
			name:     "non-zero count is non-empty",
			response: GlobalSearchResponse{Count: 1},
			want:     false,
		},
		{
			name:     "entity digest present is non-empty",
			response: GlobalSearchResponse{EntityDigests: []EntityDigest{{ID: "x"}}},
			want:     false,
		},
		{
			name:     "community summary present is non-empty",
			response: GlobalSearchResponse{CommunitySummaries: []CommunitySummary{{}}},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := searchGraphResponseEmpty(data)
			if got != tt.want {
				t.Errorf("searchGraphResponseEmpty = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSearchGraphResponseEmpty_MalformedJSONIsNonEmpty documents the
// defensive posture: an unparseable response does NOT trigger the
// fallback (treated as non-empty so we don't spend an extra NATS hop
// chasing a payload we couldn't decode).
func TestSearchGraphResponseEmpty_MalformedJSONIsNonEmpty(t *testing.T) {
	if searchGraphResponseEmpty([]byte(`not-json`)) {
		t.Errorf("malformed response should NOT count as empty (would trigger redundant fallback)")
	}
}

// TestAdaptSemanticToGlobalSearchResponse_HappyPath confirms semantic
// results map onto EntityDigests with similarity → Relevance and the
// fallback-marker fields are set.
func TestAdaptSemanticToGlobalSearchResponse_HappyPath(t *testing.T) {
	semanticPayload := []byte(`{"similaritySearch":{"results":[
		{"entity_id":"acme.x.agent.web.observation.h1","similarity":0.82},
		{"entity_id":"acme.x.agent.web.observation.h2","similarity":0.71}
	]}}`)

	got := adaptSemanticToGlobalSearchResponse(semanticPayload)
	if got == nil {
		t.Fatalf("expected non-nil adapted response")
	}
	if got.Strategy != searchGraphStrategySemanticFallback {
		t.Errorf("Strategy = %q, want %q", got.Strategy, searchGraphStrategySemanticFallback)
	}
	if !got.Degraded {
		t.Errorf("Degraded = false, want true (fallback path always marks degraded)")
	}
	if got.DegradedReason == "" {
		t.Errorf("DegradedReason should be set on the fallback path")
	}
	if got.Count != 2 {
		t.Errorf("Count = %d, want 2", got.Count)
	}
	if len(got.EntityDigests) != 2 {
		t.Fatalf("EntityDigests len = %d, want 2", len(got.EntityDigests))
	}
	if got.EntityDigests[0].ID != "acme.x.agent.web.observation.h1" {
		t.Errorf("first digest ID = %q", got.EntityDigests[0].ID)
	}
	if got.EntityDigests[0].Relevance != 0.82 {
		t.Errorf("first digest Relevance = %v, want 0.82", got.EntityDigests[0].Relevance)
	}
}

// TestAdaptSemanticToGlobalSearchResponse_EmptyReturnsNil — the
// adapter signals "nothing useful here" so the caller can fall back
// to the original empty globalSearch payload (uniform shape).
func TestAdaptSemanticToGlobalSearchResponse_EmptyReturnsNil(t *testing.T) {
	tests := []struct{ name, body string }{
		{"unparseable", "not-json"},
		{"no results key", `{}`},
		{"empty results", `{"similaritySearch":{"results":[]}}`},
		{"all rows missing entity_id", `{"similaritySearch":{"results":[{"similarity":0.9}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adaptSemanticToGlobalSearchResponse([]byte(tt.body)); got != nil {
				t.Errorf("expected nil, got %+v", got)
			}
		})
	}
}

// TestHandleSearchGraph_InvalidRequestPropagates confirms invalid-
// input errors from handleGlobalSearch (unmarshal failure, empty
// query) propagate through handleSearchGraph unchanged. The fallback
// path is for empty-but-successful responses, not for caller bugs;
// surfacing the error lets the caller fix the request rather than
// silently triggering a redundant semantic search.
//
// A full end-to-end test of the empty→fallback path requires
// distinguishing globalSearch's INTERNAL semantic call (graphrag
// Tier 1) from the OUTER semantic fallback — both hit the same NATS
// subject so they're indistinguishable at the mock layer. The
// fallback logic is covered as unit tests against the helpers
// (searchGraphResponseEmpty + adaptSemanticToGlobalSearchResponse)
// instead; live integration coverage will land with the e2e tier
// that exercises a real graph-embedding deployment.
func TestHandleSearchGraph_InvalidRequestPropagates(t *testing.T) {
	c := newSummaryTestComponent(func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return nil, errors.New("should not be called: handleGlobalSearch should fail at unmarshal")
	})
	// Unparseable JSON — handleGlobalSearch fails at json.Unmarshal
	// before any NATS subordinate call. handleSearchGraph propagates
	// the error.
	if _, err := c.handleSearchGraph(context.Background(), []byte(`{bad json`)); err == nil {
		t.Errorf("expected error to propagate for unparseable request; got nil")
	}
}
