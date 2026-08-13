package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestLocalSearchDirectClusteringFallbackStillValidatesGeneration(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	component := newSummaryTestComponent(nil)
	component.answerSynthesizer = &TemplateAnswerSynthesizer{}
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	component.communityCache.publish(generation)
	component.natsClient.(*mockNATSClient).requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.clustering.query.entity":
			return mustMarshalQueryFixture(t, map[string]any{
				"entity_id": entityID,
				"level":     0,
				"community": &clustering.Community{ID: entityID, Level: 0, Members: []string{entityID}, StatisticalSummary: "widgets"},
			}), nil
		case "graph.ingest.query.batch":
			component.communityCache.unpublish(generation)
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	}

	result, err := component.handleLocalSearch(context.Background(), []byte(`{
		"entity_id":"acme.ops.test.system.widget.001", "query":"widget", "level":0
	}`))
	require.Nil(t, result)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, gtypes.ErrorCodeIndexNotReady, classified.Code)
}

func TestGlobalSearchPreservesLowerTierWhenCommunityGenerationUnavailable(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	semantic := mustMarshalQueryFixture(t, map[string]any{
		"results":       []map[string]any{{"entity_id": entityID, "similarity": 0.91}},
		"embedder_type": "neural",
	})
	batch := mustMarshalQueryFixture(t, map[string]any{
		"entities": []gtypes.EntityState{{ID: entityID}},
	})
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			return semantic, nil
		case "graph.ingest.query.batch":
			return batch, nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)

	result, err := component.handleGlobalSearch(context.Background(), []byte(`{
		"query":"widget", "include_sources":true
	}`))
	require.NoError(t, err)
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, 1, response.Count)
	require.Len(t, response.Entities, 1)
	require.Equal(t, entityID, response.Entities[0].ID)
	require.True(t, response.Degraded)
	require.Equal(t, communityCacheNotReadyReason, response.DegradedReason)
	require.Empty(t, response.CommunitySummaries)
	require.Empty(t, response.Answer)
	require.Len(t, response.Sources, 1)
	require.Equal(t, entityID, response.Sources[0].EntityID)
	require.Empty(t, response.Sources[0].CommunityID)
	require.EqualValues(t, 1, component.messagesProcessed, "optional degraded response counts one success")
	require.Zero(t, component.errors, "optional degradation is not a required-generation failure")
}

func TestGlobalSearchExplicitNoSummariesNeedsNoCommunityGeneration(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			return mustMarshalQueryFixture(t, map[string]any{
				"results":       []map[string]any{{"entity_id": entityID, "similarity": 0.91}},
				"embedder_type": "neural",
			}), nil
		case "graph.ingest.query.batch":
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)

	result, err := component.handleGlobalSearch(context.Background(), []byte(`{
		"query":"widget", "summarize_threshold":-1, "include_summaries":false
	}`))
	require.NoError(t, err)
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, 1, response.Count)
	require.False(t, response.Degraded)
	require.Empty(t, response.DegradedReason)
}

func TestSearchGraphSemanticFallbackKeepsStrategyUnderCommunityDegradation(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	semanticCalls := 0
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		if subject != "graph.embedding.query.search" {
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
		semanticCalls++
		if semanticCalls == 1 {
			return []byte(`{"query":"widget","results":[]}`), nil
		}
		return []byte(`{"query":"widget","results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.88}]}`), nil
	})
	component.communityCache = newCommunityCache(component.logger)

	result, err := component.handleSearchGraph(context.Background(), []byte(`{"query":"widget"}`))
	require.NoError(t, err)
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, searchGraphStrategySemanticFallback, response.Strategy)
	require.Equal(t, communityCacheNotReadyReason, response.DegradedReason)
	require.Len(t, response.EntityDigests, 1)
	require.Equal(t, entityID, response.EntityDigests[0].ID)
}

func TestSearchGraphSemanticFallbackWithoutEnrichmentKeepsExistingReason(t *testing.T) {
	semanticCalls := 0
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			semanticCalls++
			if semanticCalls == 1 {
				return []byte(`{"query":"widget","results":[]}`), nil
			}
			return []byte(`{"query":"widget","results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.88}]}`), nil
		case "graph.ingest.query.batch":
			return nil, errors.New("label hydration unavailable")
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)

	result, err := component.handleSearchGraph(context.Background(), []byte(`{
		"query":"widget", "include_summaries":false, "summarize_threshold":-1
	}`))
	require.NoError(t, err)
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, searchGraphStrategySemanticFallback, response.Strategy)
	require.Equal(t, "global_search_empty_semantic_fallback", response.DegradedReason)
}

func TestGlobalSearchAutoSummaryThresholdAndSources(t *testing.T) {
	entityIDs := []string{
		"acme.ops.test.system.widget.001",
		"acme.ops.test.system.widget.002",
	}
	semantic := mustMarshalQueryFixture(t, map[string]any{
		"results": []map[string]any{
			{"entity_id": entityIDs[0], "similarity": 0.91},
			{"entity_id": entityIDs[1], "similarity": 0.82},
		},
		"embedder_type": "neural",
	})

	tests := []struct {
		name       string
		request    string
		wantBrief  bool
		wantSource bool
	}{
		{name: "omitted threshold uses default", request: `{"query":"widget","include_summaries":false}`, wantBrief: false},
		{name: "zero disables", request: `{"query":"widget","summarize_threshold":0,"include_summaries":false}`, wantBrief: false},
		{name: "trigger ignores include summaries false", request: `{"query":"widget","summarize_threshold":1,"include_summaries":false,"include_sources":true}`, wantBrief: true, wantSource: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.embedding.query.search":
					return semantic, nil
				case "graph.ingest.query.batch":
					return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityIDs[0]}, {ID: entityIDs[1]}}}), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			component.communityCache = newCommunityCache(component.logger)

			result, err := component.handleGlobalSearch(context.Background(), []byte(tt.request))
			require.NoError(t, err)
			var response GlobalSearchResponse
			require.NoError(t, json.Unmarshal(result, &response))
			require.Equal(t, tt.wantBrief, response.Summarized)
			if tt.wantSource {
				require.Len(t, response.Sources, 2)
				require.Equal(t, entityIDs[0], response.Sources[0].EntityID)
				require.InDelta(t, 0.91, response.Sources[0].Relevance, 0.001)
			}
		})
	}
}

func TestGlobalSearchAutoSummarySourcesUseOneGenerationForDecoration(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			return []byte(`{"results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.93},{"entity_id":"acme.ops.test.system.widget.002","similarity":0.80}],"embedder_type":"neural"}`), nil
		case "graph.ingest.query.batch":
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}, {ID: "acme.ops.test.system.widget.002"}}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	generation.applyUpdate(communityKVKey(0, entityID), mustCommunityJSON(t,
		&clustering.Community{ID: entityID, Level: 0, Members: []string{entityID, "acme.ops.test.system.widget.002"}}))
	component.communityCache.publish(generation)

	result, err := component.handleGlobalSearch(context.Background(), []byte(`{
		"query":"widget", "summarize_threshold":1, "include_summaries":false, "include_sources":true
	}`))
	require.NoError(t, err)
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.True(t, response.Summarized)
	require.Len(t, response.Sources, 2)
	require.Equal(t, entityID, response.Sources[0].CommunityID)
}

func TestSearchGraphSemanticFallbackPreservesRequestedSources(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	communityID := "acme.ops.test.cluster.group.001"
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprintf("generation_ready_%t", ready), func(t *testing.T) {
			semanticCalls := 0
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.embedding.query.search":
					semanticCalls++
					if semanticCalls == 1 {
						return []byte(`{"query":"quux","results":[]}`), nil
					}
					return []byte(`{"query":"quux","results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.88}]}`), nil
				case "graph.ingest.query.batch":
					return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			component.communityCache = newCommunityCache(component.logger)
			component.minTextRelevance = MinTextRelevance
			if ready {
				generation := newCommunityGeneration(1)
				generation.applyUpdate(communityKVKey(0, communityID), mustCommunityJSON(t,
					&clustering.Community{ID: communityID, Level: 0, Members: []string{entityID}}))
				component.communityCache.publish(generation)
			}

			result, err := component.handleSearchGraph(context.Background(), []byte(`{
				"query":"quux", "include_summaries":false, "summarize_threshold":-1, "include_sources":true
			}`))
			require.NoError(t, err)
			var response GlobalSearchResponse
			require.NoError(t, json.Unmarshal(result, &response))
			require.Len(t, response.Sources, 1)
			require.Equal(t, entityID, response.Sources[0].EntityID)
			require.InDelta(t, 0.88, response.Sources[0].Relevance, 0.001)
			if ready {
				require.Equal(t, communityID, response.Sources[0].CommunityID)
			} else {
				require.Empty(t, response.Sources[0].CommunityID)
				require.Equal(t, communityCacheNotReadyReason, response.DegradedReason)
			}
		})
	}
}

func TestGlobalSearchGenerationLossFinalizationPreservesLowerTierAndRecordsMetric(t *testing.T) {
	component := newSummaryTestComponent(nil)
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	component.communityCache.publish(generation)
	ctx, state := component.withCommunityRequestLease(context.Background())
	requestCommunityEnrichment(ctx)

	degraded := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_global_search_degraded_total",
		Help: "test",
	}, []string{"reason"})
	component.promMetrics = &queryMetrics{globalSearchDegraded: degraded}

	component.communityCache.unpublish(generation)
	payload, err := json.Marshal(GlobalSearchResponse{
		Strategy:           "semantic",
		Entities:           []*gtypes.EntityState{{ID: "acme.ops.test.system.widget.001"}},
		EntityIDs:          []string{"acme.ops.test.system.widget.001"},
		EntityDigests:      []EntityDigest{{ID: "acme.ops.test.system.widget.001", Relevance: 0.91}},
		CommunitySummaries: []CommunitySummary{{CommunityID: "community-1", Summary: "stale"}},
		Sources:            []Source{{EntityID: "acme.ops.test.system.widget.001", CommunityID: "community-1", Relevance: 0.91}},
		Count:              1,
		Answer:             "stale answer",
		AnswerModel:        "stale model",
	})
	require.NoError(t, err)

	result, err := component.completeGlobalCommunityResult(nil, payload, state)
	require.NoError(t, err)
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, "semantic", response.Strategy)
	require.Equal(t, 1, response.Count)
	require.Len(t, response.Entities, 1)
	require.Len(t, response.EntityIDs, 1)
	require.Len(t, response.EntityDigests, 1)
	require.Len(t, response.Sources, 1)
	require.Equal(t, 0.91, response.Sources[0].Relevance)
	require.Empty(t, response.Sources[0].CommunityID)
	require.Empty(t, response.CommunitySummaries)
	require.Empty(t, response.Answer)
	require.Empty(t, response.AnswerModel)
	require.True(t, response.Degraded)
	require.Equal(t, communityCacheNotReadyReason, response.DegradedReason)
	require.Equal(t, float64(1), testutil.ToFloat64(degraded.WithLabelValues(communityCacheNotReadyReason)))
}

func TestSearchGraphOutermostFinalizationRejectsStaleGlobalEnrichment(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	communityID := "acme.ops.test.cluster.group.001"
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			return []byte(`{"results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.91}],"embedder_type":"neural"}`), nil
		case "graph.ingest.query.batch":
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	generation.applyUpdate(communityKVKey(0, communityID), mustCommunityJSON(t,
		&clustering.Community{
			ID:                 communityID,
			Level:              0,
			Members:            []string{entityID},
			StatisticalSummary: "widget cluster",
		}))
	component.communityCache.publish(generation)
	hookCalled := make(chan struct{}, 1)
	component.searchGraphBeforeFinalize = func() {
		component.communityCache.publish(newCommunityGeneration(2))
		hookCalled <- struct{}{}
	}

	result, err := component.handleSearchGraph(context.Background(), []byte(`{
		"query":"widget", "include_sources":true
	}`))
	require.NoError(t, err)
	select {
	case <-hookCalled:
	default:
		t.Fatal("outer SearchGraph finalization hook was not reached")
	}
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, 1, response.Count)
	require.Len(t, response.Entities, 1)
	require.Empty(t, response.CommunitySummaries)
	require.Empty(t, response.Answer)
	require.Empty(t, response.AnswerModel)
	require.Len(t, response.Sources, 1)
	require.Empty(t, response.Sources[0].CommunityID)
	require.Equal(t, communityCacheNotReadyReason, response.DegradedReason)
}

func TestLocalSearchFinalValidationRunsAfterResponseWork(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	communityID := "acme.ops.test.cluster.group.001"
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		require.Equal(t, "graph.ingest.query.batch", subject)
		return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
	})
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	generation.applyUpdate(communityKVKey(0, communityID), mustCommunityJSON(t,
		&clustering.Community{ID: communityID, Level: 0, Members: []string{entityID}, StatisticalSummary: "widget cluster"}))
	component.communityCache.publish(generation)
	hookCalled := false
	component.localSearchBeforeFinalize = func() {
		hookCalled = true
		component.communityCache.publish(newCommunityGeneration(2))
	}

	result, err := component.handleLocalSearch(context.Background(), []byte(`{
		"entity_id":"acme.ops.test.system.widget.001", "query":"widget", "level":0
	}`))
	require.Nil(t, result)
	require.True(t, hookCalled)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, gtypes.ErrorCodeIndexNotReady, classified.Code)
	require.Zero(t, component.messagesProcessed, "failed final validation must not count success")
	require.EqualValues(t, 1, component.errors, "failed final validation must count one error")
}

func TestLocalSearchSemanticSuccessUsesSameFinalValidation(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.clustering.query.entity":
			return mustMarshalQueryFixture(t, map[string]any{"entity_id": entityID, "level": 0, "community": nil}), nil
		case "graph.embedding.query.search":
			return []byte(`{"results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.91}],"embedder_type":"neural"}`), nil
		case "graph.ingest.query.batch":
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	component.communityCache.publish(generation)
	hookCalled := false
	component.localSearchBeforeFinalize = func() {
		hookCalled = true
		component.communityCache.publish(newCommunityGeneration(2))
	}

	result, err := component.handleLocalSearch(context.Background(), []byte(`{
		"entity_id":"acme.ops.test.system.widget.001", "query":"widget", "level":0
	}`))
	require.Nil(t, result)
	require.True(t, hookCalled)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, gtypes.ErrorCodeIndexNotReady, classified.Code)
	require.Zero(t, component.messagesProcessed, "semantic success invalidated at completion must not count success")
	require.EqualValues(t, 1, component.errors, "semantic completion invalidation must count one error")
}

func TestGlobalSearchCommunityTextGenerationLossCompletionMetrics(t *testing.T) {
	entityID := "acme.ops.test.system.widget.001"
	communityID := "acme.ops.test.cluster.group.001"
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			return []byte(`{"results":[],"embedder_type":"neural"}`), nil
		case "graph.ingest.query.batch":
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityID}}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)
	generation := newCommunityGeneration(1)
	generation.applyUpdate(communityKVKey(0, communityID), mustCommunityJSON(t,
		&clustering.Community{
			ID: communityID, Level: 0, Members: []string{entityID},
			StatisticalSummary: "widget cluster", Keywords: []string{"widget"},
		}))
	component.communityCache.publish(generation)
	hookCalled := false
	component.globalSearchBeforeFinalize = func() {
		hookCalled = true
		component.communityCache.publish(newCommunityGeneration(2))
	}

	result, err := component.handleGlobalSearch(context.Background(), []byte(`{"query":"widget","level":0}`))
	require.Nil(t, result)
	require.True(t, hookCalled)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, gtypes.ErrorCodeIndexNotReady, classified.Code)
	require.Zero(t, component.messagesProcessed, "required community-text loss must not count success")
	require.EqualValues(t, 1, component.errors, "required community-text loss must count one error")
}

func TestSearchGraphSemanticFallbackInheritsSummaryIntent(t *testing.T) {
	entityIDs := []string{"acme.ops.test.system.widget.001", "acme.ops.test.system.widget.002"}
	communityID := "acme.ops.test.cluster.group.001"

	for _, tc := range []struct {
		name           string
		request        string
		fallbackResult string
		wantSummarized bool
	}{
		{
			name:           "default summaries",
			request:        `{"query":"widget","level":1}`,
			fallbackResult: `{"query":"widget","results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.91}]}`,
		},
		{
			name:           "auto summary overrides include summaries false",
			request:        `{"query":"widget","level":1,"summarize_threshold":1,"include_summaries":false}`,
			fallbackResult: `{"query":"widget","results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.91},{"entity_id":"acme.ops.test.system.widget.002","similarity":0.82}]}`,
			wantSummarized: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			semanticCalls := 0
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.embedding.query.search":
					semanticCalls++
					if semanticCalls == 1 {
						return []byte(`{"query":"widget","results":[]}`), nil
					}
					return []byte(tc.fallbackResult), nil
				case "graph.ingest.query.batch":
					return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: entityIDs[0]}, {ID: entityIDs[1]}}}), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			component.communityCache = newCommunityCache(component.logger)
			generation := newCommunityGeneration(1)
			generation.applyUpdate(communityKVKey(0, communityID), mustCommunityJSON(t,
				&clustering.Community{
					ID: communityID, Level: 0, Members: entityIDs,
					StatisticalSummary: "widget cluster", Keywords: []string{"widget"},
				}))
			component.communityCache.publish(generation)

			result, err := component.handleSearchGraph(context.Background(), []byte(tc.request))
			require.NoError(t, err)
			var response GlobalSearchResponse
			require.NoError(t, json.Unmarshal(result, &response))
			require.Equal(t, searchGraphStrategySemanticFallback, response.Strategy)
			require.Equal(t, tc.wantSummarized, response.Summarized)
			require.NotEmpty(t, response.CommunitySummaries)
			require.NotEmpty(t, response.Answer)
			require.NotEmpty(t, response.EntityDigests)
			require.InDelta(t, 0.91, response.EntityDigests[0].Relevance, 0.001)
			if tc.wantSummarized {
				require.Equal(t, entityIDs, response.EntityIDs)
			}
		})
	}
}
