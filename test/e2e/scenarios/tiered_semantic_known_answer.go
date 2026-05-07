package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// knownAnswerTerm describes a single-word search term that should reliably
// match content in the semantic-tier fixtures (testdata/semantic/*.jsonl).
// Each term must appear in at least two fixture files so a stray ingestion
// failure on one fixture doesn't false-fail the assertion.
type knownAnswerTerm struct {
	term string
	// substrAny is the lowercased substring patterns that should appear in
	// at least one entity ID OR community summary text. Loose enough to
	// tolerate ID-shape variation (different entityID parts may carry the
	// substring) and summary phrasing.
	substrAny []string
}

// knownAnswerTerms are the corpus probes for the semantic-tier known-answer
// test. Adding a new term requires confirming it appears in at least two
// fixture files in testdata/semantic/.
var knownAnswerTerms = []knownAnswerTerm{
	{
		term:      "forklift",
		substrAny: []string{"forklift", "fl-042", "fl_042"},
	},
	{
		term:      "hydraulic",
		substrAny: []string{"hydraulic", "fluid", "cylinder"},
	},
	{
		term:      "temperature",
		substrAny: []string{"temperature", "temp-sensor", "cold storage", "cold_storage"},
	},
}

// executeValidateGlobalSearchKnownAnswer is the semantic-tier guard against
// the "globalSearch returns count=0 for content that demonstrably exists"
// bug class (see semspec Meshtastic report, 2026-05-07). Earlier
// executeTestGraphRAGGlobal probes a broad query and only WARNs on empty
// results; this stage uses single-word queries that match deterministic
// fixture content and HARD-FAILs when:
//
//   - The response count is 0 for any known-answer term.
//   - None of the term's expected substrings appear in any returned entity
//     ID or community summary text (the search may return entities, but if
//     none are related to the term the search is broken).
//
// Probes use level=0 because that's the level the bug surfaced at. If a
// future deployment's communities live at a different level, change the
// constant below alongside the assertion.
func (s *TieredScenario) executeValidateGlobalSearchKnownAnswer(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	if gatewayURL == "" {
		result.Warnings = append(result.Warnings,
			"executeValidateGlobalSearchKnownAnswer: no GraphQL URL configured, skipping")
		return nil
	}

	level := 0 // matches the level the Meshtastic bug surfaced at

	results := make([]map[string]any, 0, len(knownAnswerTerms))
	var failures []string

	for _, ka := range knownAnswerTerms {
		probe := s.runKnownAnswerProbe(ctx, gatewayURL, ka, level)
		results = append(results, probe.detail)
		if probe.failure != "" {
			failures = append(failures, probe.failure)
		}
	}

	result.Details["global_search_known_answer"] = map[string]any{
		"level":   level,
		"probes":  results,
		"failed":  len(failures),
		"checked": len(knownAnswerTerms),
	}
	result.Metrics["global_search_known_answer_probes"] = len(knownAnswerTerms)
	result.Metrics["global_search_known_answer_failures"] = len(failures)

	if len(failures) > 0 {
		return fmt.Errorf("globalSearch known-answer assertion failed for %d/%d probes: %s",
			len(failures), len(knownAnswerTerms), strings.Join(failures, "; "))
	}
	return nil
}

// knownAnswerProbeResult captures the outcome of a single term probe.
type knownAnswerProbeResult struct {
	detail  map[string]any
	failure string // non-empty when the probe failed an assertion
}

// runKnownAnswerProbe runs a single globalSearch + known-answer assertion.
func (s *TieredScenario) runKnownAnswerProbe(ctx context.Context, gatewayURL string, ka knownAnswerTerm, level int) knownAnswerProbeResult {
	resp, latency, err := s.sendGlobalSearchAtLevel(ctx, gatewayURL, ka.term, level)
	if err != nil {
		return knownAnswerProbeResult{
			detail: map[string]any{
				"term":  ka.term,
				"error": err.Error(),
			},
			failure: fmt.Sprintf("term %q: request failed: %v", ka.term, err),
		}
	}

	gs := resp.Data.GlobalSearch
	matched := matchKnownAnswerTerm(ka, gs)

	detail := map[string]any{
		"term":           ka.term,
		"count":          gs.Count,
		"summarized":     gs.Summarized,
		"entities":       len(gs.Entities),
		"entity_ids":     len(gs.EntityIDs),
		"entity_digests": len(gs.EntityDigests),
		"communities":    len(gs.CommunitySummaries),
		"latency_ms":     latency.Milliseconds(),
		"matched_substr": matched.substring,
		"match_location": matched.location,
	}

	if gs.Count == 0 {
		return knownAnswerProbeResult{
			detail:  detail,
			failure: fmt.Sprintf("term %q: count=0 at level=%d (entities=%d ids=%d digests=%d communities=%d)",
				ka.term, level, len(gs.Entities), len(gs.EntityIDs), len(gs.EntityDigests), len(gs.CommunitySummaries)),
		}
	}
	if matched.substring == "" {
		return knownAnswerProbeResult{
			detail: detail,
			failure: fmt.Sprintf("term %q: count=%d (summarized=%v ids=%d digests=%d communities=%d) — none of %v in any surface",
				ka.term, gs.Count, gs.Summarized, len(gs.EntityIDs), len(gs.EntityDigests), len(gs.CommunitySummaries), ka.substrAny),
		}
	}
	return knownAnswerProbeResult{detail: detail}
}

// knownAnswerMatch records WHERE a substring was found (for diagnostics).
type knownAnswerMatch struct {
	substring string
	// location is one of: "entity", "entity_id", "entity_digest",
	// "community_summary", "community_keywords".
	location string
}

// matchKnownAnswerTerm scans a globalSearch response for any of ka.substrAny.
// Case-insensitive. globalSearch returns two distinct response shapes:
//
//   - Non-summarized (count ≤ summarize_threshold): `entities` populated with
//     id/type/label tuples; `entity_ids`, `entity_digests` empty.
//   - Summarized (count > threshold, default 50): `entities` is null;
//     `entity_ids` and `entity_digests` carry the result set, with
//     `entity_digests` providing labels (often the human-readable surface,
//     e.g. "Forklift Hydraulic System Repair").
//
// Both shapes may include `community_summaries`. The matcher scans every
// surface globalSearch actually exposes — looking only at `entities` made
// the test silently miss every summarized response (the 2026-05-07
// known-answer regression).
func matchKnownAnswerTerm(ka knownAnswerTerm, gs globalSearchPayload) knownAnswerMatch {
	containsAny := func(haystack string) string {
		hl := strings.ToLower(haystack)
		for _, sub := range ka.substrAny {
			if strings.Contains(hl, sub) {
				return sub
			}
		}
		return ""
	}

	for _, e := range gs.Entities {
		if m := containsAny(e.ID); m != "" {
			return knownAnswerMatch{substring: m, location: "entity"}
		}
		if m := containsAny(e.Label); m != "" {
			return knownAnswerMatch{substring: m, location: "entity"}
		}
	}
	for _, id := range gs.EntityIDs {
		if m := containsAny(id); m != "" {
			return knownAnswerMatch{substring: m, location: "entity_id"}
		}
	}
	for _, d := range gs.EntityDigests {
		if m := containsAny(d.ID); m != "" {
			return knownAnswerMatch{substring: m, location: "entity_digest"}
		}
		if m := containsAny(d.Label); m != "" {
			return knownAnswerMatch{substring: m, location: "entity_digest"}
		}
	}
	for _, cs := range gs.CommunitySummaries {
		if m := containsAny(cs.Summary); m != "" {
			return knownAnswerMatch{substring: m, location: "community_summary"}
		}
		for _, kw := range cs.Keywords {
			if m := containsAny(kw); m != "" {
				return knownAnswerMatch{substring: m, location: "community_keywords"}
			}
		}
	}
	return knownAnswerMatch{}
}

// globalSearchPayload mirrors the wire format graph-query emits for
// globalSearch. The component speaks snake_case JSON (Go struct tags) and
// the gateway forwards that wire format directly — GraphQL response shaping
// at this gateway is bypass-style, so query-side aliases don't apply.
// Both summarized and non-summarized response shapes are covered here so the
// matcher above can scan whichever fields the server populated.
type globalSearchPayload struct {
	// Non-summarized response: full entities list.
	Entities []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"entities"`
	// Summarized response: entities is null; ids + digests populated.
	EntityIDs     []string `json:"entity_ids"`
	EntityDigests []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"entity_digests"`
	// Community summaries surface in both shapes when available.
	CommunitySummaries []struct {
		CommunityID string   `json:"community_id"`
		Summary     string   `json:"summary"`
		Keywords    []string `json:"keywords"`
		Level       int      `json:"level"`
	} `json:"community_summaries"`
	Count      int  `json:"count"`
	Summarized bool `json:"summarized"`
}

// knownAnswerResponse is the GraphQL envelope for the globalSearch query.
type knownAnswerResponse struct {
	Data struct {
		GlobalSearch globalSearchPayload `json:"globalSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// sendGlobalSearchAtLevel issues a globalSearch GraphQL query for the given
// query + level and returns the parsed response. Mirrors the pattern of
// sendGraphRAGGlobalRequest but with explicit level (the existing helper
// hard-codes level=1).
func (s *TieredScenario) sendGlobalSearchAtLevel(ctx context.Context, gatewayURL, query string, level int) (*knownAnswerResponse, time.Duration, error) {
	// Note: the gateway forwards the graph-query JSON wire format (snake_case)
	// directly, so response field names are snake_case regardless of the
	// camelCase used in this query. The struct tags on globalSearchPayload
	// match the wire format. Request both summarized and non-summarized
	// surfaces so the matcher can scan whichever the server populated.
	graphqlQuery := map[string]any{
		"query": `query($query: String!, $level: Int, $maxCommunities: Int) {
			globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
				entities { id type label }
				entity_ids
				entity_digests { id type label }
				communitySummaries { communityId summary keywords level }
				count
				summarized
			}}`,
		"variables": map[string]any{
			"query":          query,
			"level":          level,
			"maxCommunities": 10,
		},
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 60s budget covers classifier → embedding search → community scoring →
	// answer synthesis on local llama.cpp models (qwen3-0.6b throughput
	// ~25 tok/s warm). The existing test-graphrag-global stage uses 10s
	// and silently times out on every probe — don't replicate that. If the
	// real query path is slow enough to need >60s, that itself is a bug.
	httpClient := &http.Client{Timeout: 60 * time.Second}
	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, latency, fmt.Errorf("returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, fmt.Errorf("read response: %w", err)
	}

	var graphqlResp knownAnswerResponse
	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, latency, fmt.Errorf("parse response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, latency, fmt.Errorf("GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	return &graphqlResp, latency, nil
}
