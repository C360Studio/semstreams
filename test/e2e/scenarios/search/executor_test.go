package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecutor_ExecuteOne_Success(t *testing.T) {
	// Mock server returning GraphQL search results (similaritySearch format from graph-embedding)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Return GraphQL similaritySearch response format matching graph-embedding output
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "temperature sensors",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.sensor-temp-001", "similarity": 0.85},
						{"entity_id": "test.e2e.search.scenario.entity.sensor-temp-002", "similarity": 0.72},
					},
					"duration": "15ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	query := Query{
		Text:            "temperature sensors",
		Description:     "Test query",
		ExpectedPattern: "temp",
		MinScore:        0.5,
		MinHits:         1,
		MustInclude:     []string{"sensor-temp"},
	}

	result := executor.ExecuteOne(context.Background(), query)

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if len(result.Hits) != 2 {
		t.Errorf("expected 2 hits, got %d", len(result.Hits))
	}
	if result.LatencyMs == 0 {
		t.Error("expected non-zero latency")
	}
	if !result.Validation.MatchesPattern {
		t.Error("expected pattern match")
	}
	if !result.Validation.MeetsMinScore {
		t.Error("expected min score met")
	}
	if !result.Validation.MeetsMinHits {
		t.Error("expected min hits met")
	}
	if !result.Validation.KnownAnswerPassed {
		t.Error("expected known answer to pass")
	}
}

func TestExecutor_ExecuteOne_NoHits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query":    "nonexistent",
					"results":  []map[string]any{},
					"duration": "10ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	query := Query{
		Text:        "nonexistent",
		Description: "Query with no results",
		MinHits:     1,
	}

	result := executor.ExecuteOne(context.Background(), query)

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if len(result.Hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(result.Hits))
	}
	if result.Validation.MeetsMinHits {
		t.Error("should not meet min hits")
	}
}

func TestExecutor_ExecuteOne_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	query := Query{Text: "test"}

	result := executor.ExecuteOne(context.Background(), query)

	if result.Error == "" {
		t.Error("expected error for 500 response")
	}
	if result.HTTPStatus != 500 {
		t.Errorf("expected status 500, got %d", result.HTTPStatus)
	}
}

func TestExecutor_ExecuteOne_MustIncludeFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "temperature",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.sensor-humid-001", "similarity": 0.9},
					},
					"duration": "12ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	query := Query{
		Text:        "temperature",
		MustInclude: []string{"sensor-temp"}, // Not in results
	}

	result := executor.ExecuteOne(context.Background(), query)

	if result.Validation.KnownAnswerPassed {
		t.Error("known answer should fail - missing sensor-temp")
	}
	if len(result.Validation.MissingRequired) != 1 {
		t.Errorf("expected 1 missing required, got %d", len(result.Validation.MissingRequired))
	}
}

func TestExecutor_ExecuteOne_MustExcludeWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "temperature",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.sensor-temp-001", "similarity": 0.9},
						{"entity_id": "test.e2e.search.scenario.entity.doc-hr-001", "similarity": 0.5}, // Should be excluded
					},
					"duration": "14ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	query := Query{
		Text:        "temperature",
		MustInclude: []string{"sensor-temp"},
		MustExclude: []string{"doc-hr"},
	}

	result := executor.ExecuteOne(context.Background(), query)

	// KnownAnswerPassed should still be true (mustExclude is warning only)
	if !result.Validation.KnownAnswerPassed {
		t.Error("known answer should pass - mustExclude is warning only")
	}
	if len(result.Validation.UnexpectedFound) != 1 {
		t.Errorf("expected 1 unexpected found, got %d", len(result.Validation.UnexpectedFound))
	}
}

func TestExecutor_ExecuteAll_Stats(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var resp map[string]any
		if callCount == 1 {
			resp = map[string]any{
				"data": map[string]any{
					"similaritySearch": map[string]any{
						"query": "query1",
						"results": []map[string]any{
							{"entity_id": "test.e2e.search.scenario.entity.hit-1", "similarity": 0.8},
							{"entity_id": "test.e2e.search.scenario.entity.hit-2", "similarity": 0.6},
						},
						"duration": "20ms",
					},
				},
			}
		} else {
			resp = map[string]any{
				"data": map[string]any{
					"similaritySearch": map[string]any{
						"query":    "query2",
						"results":  []map[string]any{},
						"duration": "10ms",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	queries := []Query{
		{Text: "query1", MinHits: 1, MinScore: 0.5},
		{Text: "query2", MinHits: 1},
	}

	stats := executor.ExecuteAll(context.Background(), queries)

	if stats.TotalQueries != 2 {
		t.Errorf("expected 2 total queries, got %d", stats.TotalQueries)
	}
	if stats.QueriesWithResults != 1 {
		t.Errorf("expected 1 query with results, got %d", stats.QueriesWithResults)
	}
	if stats.QueriesMeetingMinHits != 1 {
		t.Errorf("expected 1 query meeting min hits, got %d", stats.QueriesMeetingMinHits)
	}
	if len(stats.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(stats.Results))
	}
	// Average of 0.8 and 0.6 = 0.7
	if stats.OverallAvgScore < 0.69 || stats.OverallAvgScore > 0.71 {
		t.Errorf("expected avg score ~0.7, got %f", stats.OverallAvgScore)
	}
}

func TestExecutor_ExecuteAll_KnownAnswerTracking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "test",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.sensor-temp-001", "similarity": 0.8},
					},
					"duration": "15ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	queries := []Query{
		{Text: "q1", MustInclude: []string{"sensor-temp"}}, // Pass
		{Text: "q2", MustInclude: []string{"doc-ops"}},     // Fail
		{Text: "q3"}, // No known-answer validation
	}

	stats := executor.ExecuteAll(context.Background(), queries)

	if stats.KnownAnswerTestsTotal != 2 {
		t.Errorf("expected 2 known-answer tests, got %d", stats.KnownAnswerTestsTotal)
	}
	if stats.KnownAnswerTestsPassed != 1 {
		t.Errorf("expected 1 passed, got %d", stats.KnownAnswerTestsPassed)
	}
	if len(stats.KnownAnswerFailures) != 1 {
		t.Errorf("expected 1 failure, got %d", len(stats.KnownAnswerFailures))
	}
}

func TestValidation_AvgScore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "test",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.a", "similarity": 0.9},
						{"entity_id": "test.e2e.search.scenario.entity.b", "similarity": 0.7},
						{"entity_id": "test.e2e.search.scenario.entity.c", "similarity": 0.5},
					},
					"duration": "18ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	result := executor.ExecuteOne(context.Background(), Query{Text: "test"})

	// Average of 0.9, 0.7, 0.5 = 0.7
	if result.Validation.AvgScore < 0.69 || result.Validation.AvgScore > 0.71 {
		t.Errorf("expected avg score ~0.7, got %f", result.Validation.AvgScore)
	}
}

func TestValidation_HitsAboveMinScore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "test",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.a", "similarity": 0.9},
						{"entity_id": "test.e2e.search.scenario.entity.b", "similarity": 0.4}, // Below 0.5
						{"entity_id": "test.e2e.search.scenario.entity.c", "similarity": 0.6},
					},
					"duration": "22ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	result := executor.ExecuteOne(context.Background(), Query{Text: "test", MinScore: 0.5})

	if result.Validation.HitsAboveMinScore != 2 {
		t.Errorf("expected 2 hits above min score, got %d", result.Validation.HitsAboveMinScore)
	}
}

// TestValidate_MustIncludeInTopN tests position-based validation.
func TestValidate_MustIncludeInTopN(t *testing.T) {
	tests := []struct {
		name              string
		hits              []Hit
		mustIncludeInTopN map[int][]string
		wantViolations    []PositionViolation
		wantPass          bool
	}{
		{
			name: "entity_found_in_top_3",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.8},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-003", Score: 0.7},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.6},
			},
			mustIncludeInTopN: map[int][]string{
				3: {"test.e2e.search.scenario.entity.doc-ops-001"},
			},
			wantViolations: nil,
			wantPass:       true,
		},
		{
			name: "entity_found_but_not_in_top_3",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-002", Score: 0.8},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-003", Score: 0.7},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.6}, // Rank 4, not in top 3
			},
			mustIncludeInTopN: map[int][]string{
				3: {"test.e2e.search.scenario.entity.doc-ops-001"},
			},
			wantViolations: []PositionViolation{
				{
					Pattern:      "test.e2e.search.scenario.entity.doc-ops-001",
					RequiredTopN: 3,
					ActualRank:   4,
				},
			},
			wantPass: false,
		},
		{
			name: "entity_not_found_at_all",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-002", Score: 0.8},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-003", Score: 0.7},
			},
			mustIncludeInTopN: map[int][]string{
				3: {"test.e2e.search.scenario.entity.doc-ops-001"},
			},
			wantViolations: []PositionViolation{
				{
					Pattern:      "test.e2e.search.scenario.entity.doc-ops-001",
					RequiredTopN: 3,
					ActualRank:   -1,
				},
			},
			wantPass: false,
		},
		{
			name: "multiple_positions_top_3_and_top_5",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.95},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.90},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-003", Score: 0.85},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-004", Score: 0.80},
				{EntityID: "test.e2e.search.scenario.entity.sensor-temp-001", Score: 0.75},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-005", Score: 0.70},
			},
			mustIncludeInTopN: map[int][]string{
				3: {"test.e2e.search.scenario.entity.doc-ops-001"},
				5: {"sensor-temp"},
			},
			wantViolations: nil,
			wantPass:       true,
		},
		{
			name:              "empty_hits_with_position_constraint",
			hits:              []Hit{},
			mustIncludeInTopN: map[int][]string{3: {"test.e2e.search.scenario.entity.doc-ops-001"}},
			wantViolations: []PositionViolation{
				{
					Pattern:      "test.e2e.search.scenario.entity.doc-ops-001",
					RequiredTopN: 3,
					ActualRank:   -1,
				},
			},
			wantPass: false,
		},
		{
			name: "pattern_matches_multiple_but_first_in_top_n",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.8},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.7},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-003", Score: 0.6},
			},
			mustIncludeInTopN: map[int][]string{
				2: {"doc-ops"}, // Matches test.e2e.search.scenario.entity.doc-ops-001 at rank 1
			},
			wantViolations: nil,
			wantPass:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				results := make([]map[string]any, len(tt.hits))
				for i, hit := range tt.hits {
					results[i] = map[string]any{
						"entity_id":  hit.EntityID,
						"similarity": hit.Score,
					}
				}

				resp := map[string]any{
					"data": map[string]any{
						"similaritySearch": map[string]any{
							"query":    "test",
							"results":  results,
							"duration": "10ms",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			executor := NewSimilarityExecutor(server.URL, 5*time.Second)
			query := Query{
				Text:              "test",
				MustIncludeInTopN: tt.mustIncludeInTopN,
			}

			result := executor.ExecuteOne(context.Background(), query)

			// Check position violations
			if len(result.Validation.PositionViolations) != len(tt.wantViolations) {
				t.Errorf("expected %d position violations, got %d",
					len(tt.wantViolations), len(result.Validation.PositionViolations))
			}

			for i, wantViol := range tt.wantViolations {
				if i >= len(result.Validation.PositionViolations) {
					break
				}
				gotViol := result.Validation.PositionViolations[i]
				if gotViol.Pattern != wantViol.Pattern ||
					gotViol.RequiredTopN != wantViol.RequiredTopN ||
					gotViol.ActualRank != wantViol.ActualRank {
					t.Errorf("violation %d mismatch:\ngot  %+v\nwant %+v",
						i, gotViol, wantViol)
				}
			}

			// Overall pass/fail
			gotPass := len(result.Validation.PositionViolations) == 0
			if gotPass != tt.wantPass {
				t.Errorf("expected position validation pass=%v, got %v", tt.wantPass, gotPass)
			}
		})
	}
}

// TestValidate_MustRankHigherThan tests relative ranking validation.
func TestValidate_MustRankHigherThan(t *testing.T) {
	tests := []struct {
		name               string
		hits               []Hit
		mustRankHigherThan map[string][]string
		wantViolations     []RankingViolation
		wantPass           bool
	}{
		{
			name: "entity_a_ranks_higher_than_b",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.8},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.7},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"},
			},
			wantViolations: nil,
			wantPass:       true,
		},
		{
			name: "entity_a_ranks_lower_than_b",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.8},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"},
			},
			wantViolations: []RankingViolation{
				{
					Higher:     "test.e2e.search.scenario.entity.doc-ops-001",
					Lower:      "test.e2e.search.scenario.entity.doc-hr-001",
					HigherRank: 2,
					LowerRank:  1,
				},
			},
			wantPass: false,
		},
		{
			name: "entity_a_present_b_not_present",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.8},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"},
			},
			wantViolations: nil, // A present beats absent B
			wantPass:       true,
		},
		{
			name: "entity_a_not_present_b_present",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-002", Score: 0.8},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"},
			},
			wantViolations: []RankingViolation{
				{
					Higher:     "test.e2e.search.scenario.entity.doc-ops-001",
					Lower:      "test.e2e.search.scenario.entity.doc-hr-001",
					HigherRank: -1,
					LowerRank:  1,
				},
			},
			wantPass: false,
		},
		{
			name: "neither_present",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-finance-001", Score: 0.9},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"},
			},
			wantViolations: nil, // No ranking constraint violated
			wantPass:       true,
		},
		{
			name: "multiple_ranking_constraints",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.95},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.90},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.85},
				{EntityID: "test.e2e.search.scenario.entity.doc-finance-001", Score: 0.80},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001", "test.e2e.search.scenario.entity.doc-finance-001"},
				"test.e2e.search.scenario.entity.doc-ops-002": {"test.e2e.search.scenario.entity.doc-hr-001"},
			},
			wantViolations: nil,
			wantPass:       true,
		},
		{
			name: "one_constraint_passes_one_fails",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.95},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.90},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.85},
			},
			mustRankHigherThan: map[string][]string{
				"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"}, // Pass: rank 1 > rank 2
				"test.e2e.search.scenario.entity.doc-ops-002": {"test.e2e.search.scenario.entity.doc-hr-001"}, // Fail: rank 3 < rank 2
			},
			wantViolations: []RankingViolation{
				{
					Higher:     "test.e2e.search.scenario.entity.doc-ops-002",
					Lower:      "test.e2e.search.scenario.entity.doc-hr-001",
					HigherRank: 3,
					LowerRank:  2,
				},
			},
			wantPass: false,
		},
		{
			name: "pattern_matching_first_occurrence",
			hits: []Hit{
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-001", Score: 0.9},
				{EntityID: "test.e2e.search.scenario.entity.doc-ops-002", Score: 0.8},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-001", Score: 0.7},
				{EntityID: "test.e2e.search.scenario.entity.doc-hr-002", Score: 0.6},
			},
			mustRankHigherThan: map[string][]string{
				"doc-ops": {"doc-hr"}, // First doc-ops (rank 1) vs first doc-hr (rank 3)
			},
			wantViolations: nil,
			wantPass:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				results := make([]map[string]any, len(tt.hits))
				for i, hit := range tt.hits {
					results[i] = map[string]any{
						"entity_id":  hit.EntityID,
						"similarity": hit.Score,
					}
				}

				resp := map[string]any{
					"data": map[string]any{
						"similaritySearch": map[string]any{
							"query":    "test",
							"results":  results,
							"duration": "10ms",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			executor := NewSimilarityExecutor(server.URL, 5*time.Second)
			query := Query{
				Text:               "test",
				MustRankHigherThan: tt.mustRankHigherThan,
			}

			result := executor.ExecuteOne(context.Background(), query)

			// Check ranking violations
			if len(result.Validation.RankingViolations) != len(tt.wantViolations) {
				t.Errorf("expected %d ranking violations, got %d",
					len(tt.wantViolations), len(result.Validation.RankingViolations))
			}

			for i, wantViol := range tt.wantViolations {
				if i >= len(result.Validation.RankingViolations) {
					break
				}
				gotViol := result.Validation.RankingViolations[i]
				if gotViol.Higher != wantViol.Higher ||
					gotViol.Lower != wantViol.Lower ||
					gotViol.HigherRank != wantViol.HigherRank ||
					gotViol.LowerRank != wantViol.LowerRank {
					t.Errorf("violation %d mismatch:\ngot  %+v\nwant %+v",
						i, gotViol, wantViol)
				}
			}

			// Overall pass/fail
			gotPass := len(result.Validation.RankingViolations) == 0
			if gotPass != tt.wantPass {
				t.Errorf("expected ranking validation pass=%v, got %v", tt.wantPass, gotPass)
			}
		})
	}
}

// TestValidate_CombinedPositionAndRanking tests both position and ranking constraints together.
func TestValidate_CombinedPositionAndRanking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"similaritySearch": map[string]any{
					"query": "test",
					"results": []map[string]any{
						{"entity_id": "test.e2e.search.scenario.entity.doc-ops-001", "similarity": 0.9},
						{"entity_id": "test.e2e.search.scenario.entity.doc-ops-002", "similarity": 0.8},
						{"entity_id": "test.e2e.search.scenario.entity.doc-hr-001", "similarity": 0.7},
						{"entity_id": "test.e2e.search.scenario.entity.sensor-temp-001", "similarity": 0.6},
						{"entity_id": "test.e2e.search.scenario.entity.doc-finance-001", "similarity": 0.5},
					},
					"duration": "15ms",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := NewSimilarityExecutor(server.URL, 5*time.Second)
	query := Query{
		Text: "test",
		MustIncludeInTopN: map[int][]string{
			3: {"test.e2e.search.scenario.entity.doc-ops-001"},
			5: {"sensor-temp"},
		},
		MustRankHigherThan: map[string][]string{
			"test.e2e.search.scenario.entity.doc-ops-001": {"test.e2e.search.scenario.entity.doc-hr-001"},
			"test.e2e.search.scenario.entity.doc-ops-002": {"sensor-temp"},
		},
	}

	result := executor.ExecuteOne(context.Background(), query)

	// Both position and ranking constraints should pass
	if len(result.Validation.PositionViolations) != 0 {
		t.Errorf("expected no position violations, got %d", len(result.Validation.PositionViolations))
	}
	if len(result.Validation.RankingViolations) != 0 {
		t.Errorf("expected no ranking violations, got %d", len(result.Validation.RankingViolations))
	}
}
