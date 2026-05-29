// Package graphquery — searchGraph handler.
package graphquery

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	// searchGraphSemanticFallbackLimit caps the semantic fallback's
	// result count. Matches semspec's empirical V1 value (semspec's
	// tools/workflow/graph.go:semanticSearchFallback hardcoded 8 —
	// "enough for the agent to choose a few good entities to graph_query
	// into without flooding the response").
	searchGraphSemanticFallbackLimit = 8

	// searchGraphStrategySemanticFallback is the Strategy field value
	// on the synthesized response when the semantic fallback fires.
	// Distinct from globalSearch's existing strategy names so callers
	// can filter dashboards on it and operators see "fallback fired N
	// times" in graph telemetry.
	searchGraphStrategySemanticFallback = "semantic_fallback"
)

// handleSearchGraph is the canonical "find me stuff" entry point for
// agents and external callers. It composes:
//
//  1. handleGlobalSearch (GraphRAG path: classifier → strategy → tier-1
//     semantic → tier-2 text-based community).
//  2. When that returns an empty response (no entities, no digests,
//     no community summaries, no count), fall back to a direct
//     semantic similarity search.
//
// The fallback closes the gap semspec discovered empirically (their
// tools/workflow/graph.go:semanticSearchFallback): some classifier-
// routed strategies (entity_lookup, pathrag, spatial, temporal) can
// return empty even when a direct semantic search would find on-topic
// matches. Moving the fallback server-side means every caller —
// agent tools, MCP, external apps — gets the same degraded-but-useful
// behavior without re-implementing the logic.
//
// Response shape is always GlobalSearchResponse so callers don't need
// to special-case the fallback path. When the fallback fires:
//
//   - Strategy = "semantic_fallback"
//   - Degraded = true
//   - DegradedReason = "global_search_empty_semantic_fallback"
//
// Callers can drive UI/persona behavior off Strategy or simply read
// the entity_digests as usual.
func (c *Component) handleSearchGraph(ctx context.Context, data []byte) ([]byte, error) {
	// 1) Always try globalSearch first. It owns query classification
	// and strategy routing; we don't second-guess.
	globalResp, err := c.handleGlobalSearch(ctx, data)
	if err != nil {
		// Hard transport / parse errors propagate. The fallback is
		// for empty-but-successful responses, not for failures.
		return nil, err
	}

	if !searchGraphResponseEmpty(globalResp) {
		return globalResp, nil
	}

	// 2) Empty result — synthesize a fallback request and call the
	// semantic search path directly.
	var origReq GlobalSearchRequest
	if err := json.Unmarshal(data, &origReq); err != nil {
		// If we can't parse the original request we can't fall back;
		// return the empty-but-valid globalSearch response so the
		// caller at least sees the structured zero result.
		return globalResp, nil
	}

	semanticPayload, marshalErr := json.Marshal(map[string]any{
		"query": origReq.Query,
		"limit": searchGraphSemanticFallbackLimit,
	})
	if marshalErr != nil {
		// Same defensive posture: prefer returning the empty
		// globalSearch payload over failing the whole call.
		return globalResp, nil
	}

	semanticResp, semanticErr := c.handleQuerySemantic(ctx, semanticPayload)
	if semanticErr != nil {
		c.recordError(semanticErr)
		return globalResp, nil
	}

	// 3) Adapt the semantic response into a GlobalSearchResponse shape.
	// The semantic backend returns {"results": [{"entity_id", "similarity"}], ...}
	// per the existing graph-embedding contract; we map entries to
	// EntityDigest so existing callers reading the digest list keep
	// working.
	out := adaptSemanticToGlobalSearchResponse(semanticResp)
	if out == nil || len(out.EntityDigests) == 0 {
		// Fallback found nothing either — return the original empty
		// globalSearch payload so the caller has uniform structure.
		return globalResp, nil
	}

	respBytes, err := json.Marshal(out)
	if err != nil {
		// Marshal failure is genuinely exceptional — surface it.
		return nil, errs.Wrap(err, "GraphQuery", "handleSearchGraph", "marshal fallback response")
	}
	return respBytes, nil
}

// searchGraphResponseEmpty mirrors semspec's globalSearchEmpty
// detection: a GlobalSearchResponse is "empty" when no facet has
// content. Defensive against missing/malformed JSON — treats any
// parse failure as non-empty so a half-decoded response doesn't
// trigger a redundant fallback call.
func searchGraphResponseEmpty(data []byte) bool {
	var resp GlobalSearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return false
	}
	if resp.Count > 0 {
		return false
	}
	if len(resp.Entities) > 0 {
		return false
	}
	if len(resp.EntityDigests) > 0 {
		return false
	}
	if len(resp.CommunitySummaries) > 0 {
		return false
	}
	return true
}

// semanticHit is the per-result row from the graph-embedding
// similaritySearch envelope. Kept local because the envelope is
// internal to the embedding contract; lifting it would entangle
// packages.
type semanticHit struct {
	EntityID   string  `json:"entity_id"`
	Similarity float64 `json:"similarity"`
}

// semanticEnvelope is the response shape the GraphQL gateway returns
// for the `similaritySearch` resolver — results nested under the
// resolver field name. This is what external GraphQL clients (e.g.
// semspec's tools/workflow/graph.go:semanticSearchFallback) parse.
type semanticEnvelope struct {
	SimilaritySearch struct {
		Results []semanticHit `json:"results"`
	} `json:"similaritySearch"`
}

// semanticRawEnvelope is the response shape graph-embedding's
// handleQuerySearchNATS returns DIRECTLY when called via NATS request/
// reply (no GraphQL wrapper). This is what handleSearchGraph's fallback
// call to handleQuerySemantic actually receives — the gateway-shape
// wrapper is added only when the response flows through the GraphQL
// resolver path, which the in-process fallback bypasses.
//
// Without this shape variant the adapter silently returned nil for
// every fallback (the empirical "single-word search doesn't work" bug
// the user flagged). See processor/graph-embedding/query.go SearchResponse.
type semanticRawEnvelope struct {
	Query   string        `json:"query"`
	Results []semanticHit `json:"results"`
}

// adaptSemanticToGlobalSearchResponse turns a raw semantic-search
// response into a GlobalSearchResponse with Strategy and Degraded
// markers indicating the fallback fired. Returns nil if the input
// is unparseable or empty (callers fall back to the original empty
// globalSearch payload).
//
// Tolerates BOTH wire shapes:
//
//   - GraphQL-wrapped: {"similaritySearch":{"results":[...]}} — what
//     gateway-facing callers see.
//   - Raw NATS:        {"query":"...","results":[...]}         — what
//     in-process handleQuerySemantic returns.
//
// The Answer field is intentionally omitted: the fallback is a
// last-resort discovery surface, not a synthesis path. Downstream
// callers that want narrative output answer-synthesize on their own.
func adaptSemanticToGlobalSearchResponse(data []byte) *GlobalSearchResponse {
	// Try the GraphQL-wrapped shape first (what external callers send
	// in via gateway-mounted searchGraph composition).
	var hits []semanticHit
	var env semanticEnvelope
	if err := json.Unmarshal(data, &env); err == nil && len(env.SimilaritySearch.Results) > 0 {
		hits = env.SimilaritySearch.Results
	}
	// Fallback to the raw NATS shape (what in-process handleQuerySemantic
	// returns, which is the actual path searchGraph takes today).
	if len(hits) == 0 {
		var raw semanticRawEnvelope
		if err := json.Unmarshal(data, &raw); err != nil || len(raw.Results) == 0 {
			return nil
		}
		hits = raw.Results
	}
	digests := make([]EntityDigest, 0, len(hits))
	for _, hit := range hits {
		if hit.EntityID == "" {
			continue
		}
		digests = append(digests, EntityDigest{
			ID:        hit.EntityID,
			Relevance: hit.Similarity,
		})
	}
	if len(digests) == 0 {
		return nil
	}
	return &GlobalSearchResponse{
		Strategy:       searchGraphStrategySemanticFallback,
		EntityDigests:  digests,
		Count:          len(digests),
		Degraded:       true,
		DegradedReason: "global_search_empty_semantic_fallback",
	}
}
