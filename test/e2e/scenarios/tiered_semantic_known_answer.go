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
		"entities":       len(gs.Entities),
		"communities":    len(gs.CommunitySummaries),
		"latency_ms":     latency.Milliseconds(),
		"matched_substr": matched.substring,
		"match_location": matched.location,
	}

	if gs.Count == 0 {
		return knownAnswerProbeResult{
			detail:  detail,
			failure: fmt.Sprintf("term %q: count=0 at level=%d (entities=%d communities=%d)", ka.term, level, len(gs.Entities), len(gs.CommunitySummaries)),
		}
	}
	if matched.substring == "" {
		return knownAnswerProbeResult{
			detail:  detail,
			failure: fmt.Sprintf("term %q: count=%d but no entity ID or community summary contained any of %v", ka.term, gs.Count, ka.substrAny),
		}
	}
	return knownAnswerProbeResult{detail: detail}
}

// knownAnswerMatch records WHERE a substring was found (for diagnostics).
type knownAnswerMatch struct {
	substring string
	location  string // "entity_id" | "community_summary" | "community_keywords"
}

// matchKnownAnswerTerm scans a globalSearch response for any of ka.substrAny
// in entity IDs, community summaries, or community keywords. Case-insensitive.
// Returns the first match (substring + location) or zero value when no match.
func matchKnownAnswerTerm(ka knownAnswerTerm, gs globalSearchPayload) knownAnswerMatch {
	for _, e := range gs.Entities {
		idLower := strings.ToLower(e.ID)
		for _, sub := range ka.substrAny {
			if strings.Contains(idLower, sub) {
				return knownAnswerMatch{substring: sub, location: "entity_id"}
			}
		}
	}
	for _, cs := range gs.CommunitySummaries {
		summaryLower := strings.ToLower(cs.Summary)
		for _, sub := range ka.substrAny {
			if strings.Contains(summaryLower, sub) {
				return knownAnswerMatch{substring: sub, location: "community_summary"}
			}
		}
		for _, kw := range cs.Keywords {
			kwLower := strings.ToLower(kw)
			for _, sub := range ka.substrAny {
				if strings.Contains(kwLower, sub) {
					return knownAnswerMatch{substring: sub, location: "community_keywords"}
				}
			}
		}
	}
	return knownAnswerMatch{}
}

// globalSearchPayload is the shape of GraphQL globalSearch we depend on for
// known-answer assertions. Subset of graphRAGGlobalResponse.Data.GlobalSearch
// in tiered_statistical.go — kept distinct so changes here don't affect the
// existing graphrag-global stage.
type globalSearchPayload struct {
	Entities []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"entities"`
	CommunitySummaries []struct {
		CommunityID string   `json:"communityId"`
		Summary     string   `json:"summary"`
		Keywords    []string `json:"keywords"`
		Level       int      `json:"level"`
	} `json:"communitySummaries"`
	Count int `json:"count"`
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
	graphqlQuery := map[string]any{
		"query": `query($query: String!, $level: Int, $maxCommunities: Int) {
			globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
				entities { id type }
				communitySummaries { communityId summary keywords level }
				count
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

	httpClient := &http.Client{Timeout: 10 * time.Second}
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
