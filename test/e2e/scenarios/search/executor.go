package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Mode determines which GraphQL query to use for search.
type Mode string

const (
	// ModeGlobal uses globalSearch (community-based GraphRAG search).
	ModeGlobal Mode = "global"
	// ModeSimilarity uses semanticSearch (embedding similarity search).
	// Works on both statistical (BM25) and semantic (neural) tiers.
	ModeSimilarity Mode = "similarity"
)

// Executor executes search queries against the GraphQL gateway.
type Executor struct {
	httpClient *http.Client
	graphqlURL string // GraphQL endpoint (e.g., http://localhost:8084/graphql)
	mode       Mode   // Which search query to use
}

// NewExecutor creates a new search executor using globalSearch.
// graphqlURL should be the full GraphQL endpoint (e.g., http://localhost:8084/graphql).
func NewExecutor(graphqlURL string, timeout time.Duration) *Executor {
	return &Executor{
		httpClient: &http.Client{Timeout: timeout},
		graphqlURL: graphqlURL,
		mode:       ModeGlobal,
	}
}

// NewSimilarityExecutor creates a new search executor using semanticSearch.
// This provides embedding-based similarity search with real scores.
// Works on both statistical (BM25) and semantic (neural) tiers.
func NewSimilarityExecutor(graphqlURL string, timeout time.Duration) *Executor {
	return &Executor{
		httpClient: &http.Client{Timeout: timeout},
		graphqlURL: graphqlURL,
		mode:       ModeSimilarity,
	}
}

// ExecuteAll runs all queries and returns aggregated stats.
func (e *Executor) ExecuteAll(ctx context.Context, queries []Query) *Stats {
	stats := &Stats{
		TotalQueries: len(queries),
		Results:      make([]Result, 0, len(queries)),
		ExecutedAt:   time.Now(),
	}

	var allScores []float64

	for _, q := range queries {
		result := e.ExecuteOne(ctx, q)
		stats.Results = append(stats.Results, result)
		stats.TotalLatencyMs += result.LatencyMs

		if len(result.Hits) > 0 {
			stats.QueriesWithResults++
		}

		if result.Validation.MeetsMinScore {
			stats.QueriesMeetingMinScore++
		}

		if result.Validation.MeetsMinHits {
			stats.QueriesMeetingMinHits++
		}

		// Track known-answer results
		if len(q.MustInclude) > 0 {
			stats.KnownAnswerTestsTotal++
			if result.Validation.KnownAnswerPassed {
				stats.KnownAnswerTestsPassed++
			} else {
				stats.KnownAnswerFailures = append(stats.KnownAnswerFailures,
					fmt.Sprintf("query %q: missing required %v - %s",
						q.Text, result.Validation.MissingRequired, q.Description))
			}
		}

		// Track position validation results
		if len(q.MustIncludeInTopN) > 0 {
			stats.PositionTestsTotal++
			if result.Validation.PositionValidationPassed() {
				stats.PositionTestsPassed++
			} else {
				for _, pv := range result.Validation.PositionViolations {
					stats.PositionTestFailures = append(stats.PositionTestFailures,
						fmt.Sprintf("query %q: %s not in top %d (actual rank: %d) - %s",
							q.Text, pv.Pattern, pv.RequiredTopN, pv.ActualRank, q.Description))
				}
			}
		}

		// Track ranking validation results
		if len(q.MustRankHigherThan) > 0 {
			stats.RankingTestsTotal++
			if result.Validation.RankingValidationPassed() {
				stats.RankingTestsPassed++
			} else {
				for _, rv := range result.Validation.RankingViolations {
					stats.RankingTestFailures = append(stats.RankingTestFailures,
						fmt.Sprintf("query %q: %s (rank %d) should rank above %s (rank %d) - %s",
							q.Text, rv.Higher, rv.HigherRank, rv.Lower, rv.LowerRank, q.Description))
				}
			}
		}

		// Collect all scores for overall average
		for _, hit := range result.Hits {
			allScores = append(allScores, hit.Score)
		}
	}

	// Calculate overall average score
	if len(allScores) > 0 {
		var sum float64
		for _, s := range allScores {
			sum += s
		}
		stats.OverallAvgScore = sum / float64(len(allScores))
	}

	return stats
}

// ExecuteOne runs a single query via GraphQL and returns the result.
// Uses globalSearch or semanticSearch based on executor mode.
func (e *Executor) ExecuteOne(ctx context.Context, q Query) Result {
	result := Result{
		Query:       q.Text,
		Description: q.Description,
	}

	limit := q.Limit
	if limit == 0 {
		limit = 10
	}

	// Build and execute GraphQL query
	graphqlQuery := e.buildGraphQLQuery(q.Text, limit)
	bodyBytes, latencyMs, httpStatus, err := e.executeGraphQLRequest(ctx, graphqlQuery)
	result.LatencyMs = latencyMs
	if err != nil {
		result.Error = err.Error()
		result.HTTPStatus = httpStatus
		return result
	}

	// Parse response based on mode
	hits, err := e.parseGraphQLResponse(bodyBytes)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Hits = hits

	// Validate result
	result.Validation = e.validate(q, result.Hits)
	return result
}

// buildGraphQLQuery constructs the GraphQL query based on executor mode.
func (e *Executor) buildGraphQLQuery(queryText string, limit int) map[string]any {
	if e.mode == ModeSimilarity {
		return map[string]any{
			"query": `query($query: String!, $limit: Int) {
				semanticSearch(query: $query, limit: $limit) {
					id
					type
					score
				}
			}`,
			"variables": map[string]any{
				"query": queryText,
				"limit": limit,
			},
		}
	}
	return map[string]any{
		"query": `query($query: String!, $level: Int, $maxCommunities: Int) {
			globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
				entities { id type }
				communitySummaries { communityId summary relevance member_count entities { id type label } }
				count
				answer
				answer_model
			}
		}`,
		"variables": map[string]any{
			"query":          queryText,
			"level":          0,
			"maxCommunities": limit,
		},
	}
}

// executeGraphQLRequest sends the GraphQL query and returns the response body.
func (e *Executor) executeGraphQLRequest(ctx context.Context, graphqlQuery map[string]any) ([]byte, int64, int, error) {
	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("marshal error: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.graphqlURL, strings.NewReader(string(queryJSON)))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("request error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := e.httpClient.Do(req)
	elapsed := time.Since(start)
	latencyMs := elapsed.Milliseconds()
	if latencyMs == 0 && elapsed > 0 {
		latencyMs = 1
	}

	if err != nil {
		return nil, latencyMs, 0, fmt.Errorf("http error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, latencyMs, resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latencyMs, resp.StatusCode, fmt.Errorf("read error: %v", err)
	}
	return bodyBytes, latencyMs, resp.StatusCode, nil
}

// parseGraphQLResponse parses the response based on executor mode.
func (e *Executor) parseGraphQLResponse(bodyBytes []byte) ([]Hit, error) {
	if e.mode == ModeSimilarity {
		return parseSimilarityResponse(bodyBytes)
	}
	return parseGlobalSearchResponse(bodyBytes)
}

func parseSimilarityResponse(bodyBytes []byte) ([]Hit, error) {
	// Response format from graph-embedding via graph-gateway:
	// {"data": {"semanticSearch": {"query": "...", "results": [{"entity_id": "...", "similarity": 0.85}]}}}
	var gqlResp struct {
		Data struct {
			SemanticSearch struct {
				Query   string `json:"query"`
				Results []struct {
					EntityID   string  `json:"entity_id"`
					Similarity float64 `json:"similarity"`
				} `json:"results"`
				Duration string `json:"duration"`
			} `json:"semanticSearch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse error: %v", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("%s", gqlResp.Errors[0].Message)
	}

	var hits []Hit
	for _, result := range gqlResp.Data.SemanticSearch.Results {
		hits = append(hits, Hit{EntityID: result.EntityID, Score: result.Similarity})
	}
	return hits, nil
}

func parseGlobalSearchResponse(bodyBytes []byte) ([]Hit, error) {
	var gqlResp struct {
		Data struct {
			GlobalSearch struct {
				Entities []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"entities"`
				CommunitySummaries []struct {
					CommunityID string  `json:"communityId"`
					Summary     string  `json:"summary"`
					Relevance   float64 `json:"relevance"`
				} `json:"communitySummaries"`
				Count int `json:"count"`
			} `json:"globalSearch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse error: %v", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("%s", gqlResp.Errors[0].Message)
	}

	var hits []Hit
	for _, entity := range gqlResp.Data.GlobalSearch.Entities {
		hits = append(hits, Hit{EntityID: entity.ID, Score: 1.0})
	}
	return hits, nil
}

// findRank returns the 1-indexed rank of the first hit matching the pattern, or -1 if not found.
func findRank(hits []Hit, pattern string) int {
	for i, hit := range hits {
		if strings.Contains(strings.ToLower(hit.EntityID), strings.ToLower(pattern)) {
			return i + 1 // 1-indexed
		}
	}
	return -1 // not found
}

// validate checks the hits against query expectations.
func (e *Executor) validate(q Query, hits []Hit) ValidationResult {
	v := ValidationResult{
		KnownAnswerPassed: true, // Assume pass until proven otherwise
		MeetsMinHits:      len(hits) >= q.MinHits,
	}

	var scoreSum float64
	for _, hit := range hits {
		scoreSum += hit.Score

		if hit.Score >= q.MinScore {
			v.HitsAboveMinScore++
			v.MeetsMinScore = true
		}

		if q.ExpectedPattern != "" &&
			strings.Contains(strings.ToLower(hit.EntityID), strings.ToLower(q.ExpectedPattern)) {
			v.MatchesPattern = true
		}
	}

	if len(hits) > 0 {
		v.AvgScore = scoreSum / float64(len(hits))
	}

	// Check mustInclude - required entities that MUST appear
	for _, required := range q.MustInclude {
		found := false
		for _, hit := range hits {
			if strings.Contains(strings.ToLower(hit.EntityID), strings.ToLower(required)) {
				found = true
				break
			}
		}
		if !found {
			v.KnownAnswerPassed = false
			v.MissingRequired = append(v.MissingRequired, required)
		}
	}

	// Check mustExclude - entities that should NOT appear (warning only)
	for _, forbidden := range q.MustExclude {
		for _, hit := range hits {
			if strings.Contains(strings.ToLower(hit.EntityID), strings.ToLower(forbidden)) {
				v.UnexpectedFound = append(v.UnexpectedFound, hit.EntityID)
				break
			}
		}
	}

	// Check MustIncludeInTopN - patterns that must appear in top N positions
	for topN, patterns := range q.MustIncludeInTopN {
		for _, pattern := range patterns {
			rank := findRank(hits, pattern)
			if rank == -1 || rank > topN {
				v.PositionViolations = append(v.PositionViolations, PositionViolation{
					Pattern:      pattern,
					RequiredTopN: topN,
					ActualRank:   rank,
				})
			}
		}
	}

	// Check MustRankHigherThan - relative ranking constraints
	for higherPattern, lowerPatterns := range q.MustRankHigherThan {
		higherRank := findRank(hits, higherPattern)
		for _, lowerPattern := range lowerPatterns {
			lowerRank := findRank(hits, lowerPattern)

			// Determine if constraint is violated
			violated := false
			if higherRank == -1 && lowerRank != -1 {
				// Higher absent, lower present: FAIL (absent loses to present)
				violated = true
			} else if higherRank != -1 && lowerRank != -1 && higherRank > lowerRank {
				// Both present but higher ranks below lower: FAIL
				violated = true
			}
			// Higher present, lower absent: PASS (present beats absent)
			// Both absent: PASS (no constraint violated)

			if violated {
				v.RankingViolations = append(v.RankingViolations, RankingViolation{
					Higher:     higherPattern,
					Lower:      lowerPattern,
					HigherRank: higherRank,
					LowerRank:  lowerRank,
				})
			}
		}
	}

	return v
}
