package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/require"
)

func TestGlobalSearchAutoSummaryHydratesFinalDigestLabelsByID(t *testing.T) {
	t.Parallel()

	firstID := "acme.ops.test.system.widget.001"
	secondID := "acme.ops.test.system.sensor.002"
	semanticResponse := mustMarshalQueryFixture(t, map[string]any{
		"results": []map[string]any{
			{"entity_id": firstID, "similarity": 0.91},
			{"entity_id": secondID, "similarity": 0.82},
		},
		"embedder_type": "neural",
	})

	var batchRequests atomic.Int32
	batchObserved := make(chan []string, 1)
	component := newSummaryTestComponent(func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			return semanticResponse, nil
		case "graph.ingest.query.batch":
			batchRequests.Add(1)
			var request struct {
				IDs []string `json:"ids"`
			}
			require.NoError(t, json.Unmarshal(data, &request))
			batchObserved <- request.IDs
			// Reordered and partial: the first result is deliberately omitted.
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{
				titledEntity(secondID, "Pressure Sensor Two"),
			}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)

	result, err := component.handleGlobalSearch(context.Background(), []byte(`{
		"query":"widget", "summarize_threshold":1, "include_summaries":false
	}`))
	require.NoError(t, err)

	select {
	case requested := <-batchObserved:
		require.Equal(t, []string{firstID, secondID}, requested)
	default:
		t.Fatal("top-level entityBatch request was not observed")
	}
	require.EqualValues(t, 1, batchRequests.Load())

	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.True(t, response.Summarized)
	require.Equal(t, []string{firstID, secondID}, response.EntityIDs)
	require.Equal(t, []EntityDigest{
		{ID: firstID, Type: "widget", Label: "001", Relevance: 0.91},
		{ID: secondID, Type: "sensor", Label: "Pressure Sensor Two", Relevance: 0.82},
	}, response.EntityDigests)
}

func TestSearchGraphDirectFallbackEnrichesExistingRowsWithOneIDJoinedBatch(t *testing.T) {
	t.Parallel()

	firstID := "acme.ops.test.system.widget.001"
	secondID := "acme.ops.test.system.sensor.002"
	var semanticRequests atomic.Int32
	var batchRequests atomic.Int32
	batchObserved := make(chan []string, 1)
	component := newSummaryTestComponent(func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.embedding.query.search":
			if semanticRequests.Add(1) == 1 {
				return []byte(`{"query":"widget","results":[]}`), nil
			}
			return mustMarshalQueryFixture(t, map[string]any{
				"query": "widget",
				"results": []map[string]any{
					{"entity_id": firstID, "similarity": 0.93},
					{"entity_id": secondID, "similarity": 0.81},
					{"entity_id": firstID, "similarity": 0.74},
				},
			}), nil
		case "graph.ingest.query.batch":
			batchRequests.Add(1)
			var request struct {
				IDs []string `json:"ids"`
			}
			require.NoError(t, json.Unmarshal(data, &request))
			batchObserved <- request.IDs
			// The authoritative response is intentionally in a different order.
			return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{
				titledEntity(secondID, "Pressure Sensor Two"),
				titledEntity(firstID, "Primary Widget"),
			}}), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", subject)
		}
	})
	component.communityCache = newCommunityCache(component.logger)

	result, err := component.handleSearchGraph(context.Background(), []byte(`{
		"query":"widget", "include_summaries":false, "summarize_threshold":-1
	}`))
	require.NoError(t, err)

	select {
	case requested := <-batchObserved:
		require.Equal(t, []string{firstID, secondID}, requested, "duplicate hit IDs should hydrate once")
	default:
		t.Fatal("top-level entityBatch request was not observed")
	}
	require.EqualValues(t, 2, semanticRequests.Load())
	require.EqualValues(t, 1, batchRequests.Load())

	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, searchGraphStrategySemanticFallback, response.Strategy)
	require.Equal(t, "global_search_empty_semantic_fallback", response.DegradedReason)
	require.Equal(t, []EntityDigest{
		{ID: firstID, Type: "widget", Label: "Primary Widget", Relevance: 0.93},
		{ID: secondID, Type: "sensor", Label: "Pressure Sensor Two", Relevance: 0.81},
		{ID: firstID, Type: "widget", Label: "Primary Widget", Relevance: 0.74},
	}, response.EntityDigests)
}

func TestGlobalSearchAutoSummaryHydrationFailureAndPoison(t *testing.T) {
	t.Parallel()

	entityIDs := []string{
		"acme.ops.test.system.widget.001",
		"acme.ops.test.system.sensor.002",
	}
	semanticResponse := mustMarshalQueryFixture(t, map[string]any{
		"results": []map[string]any{
			{"entity_id": entityIDs[0], "similarity": 0.91},
			{"entity_id": entityIDs[1], "similarity": 0.82},
		},
		"embedder_type": "neural",
	})

	tests := []struct {
		name         string
		batchReply   []byte
		batchErr     error
		wantContract bool
	}{
		{name: "ordinary failure keeps ranked instance fallbacks", batchErr: errors.New("batch unavailable")},
		{
			name: "authoritative poison is fatal",
			batchReply: mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{
				{ID: "bad"},
			}}),
			wantContract: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var batchRequests atomic.Int32
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.embedding.query.search":
					return semanticResponse, nil
				case "graph.ingest.query.batch":
					batchRequests.Add(1)
					return test.batchReply, test.batchErr
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			component.communityCache = newCommunityCache(component.logger)

			result, err := component.handleGlobalSearch(context.Background(), []byte(`{
				"query":"widget", "summarize_threshold":1, "include_summaries":false
			}`))
			require.EqualValues(t, 1, batchRequests.Load())
			if test.wantContract {
				require.Nil(t, result)
				require.Error(t, err)
				require.True(t, gtypes.IsStateContractError(err), "error = %T %v", err, err)
				return
			}

			require.NoError(t, err)
			var response GlobalSearchResponse
			require.NoError(t, json.Unmarshal(result, &response))
			require.Equal(t, []EntityDigest{
				{ID: entityIDs[0], Type: "widget", Label: "001", Relevance: 0.91},
				{ID: entityIDs[1], Type: "sensor", Label: "002", Relevance: 0.82},
			}, response.EntityDigests)
		})
	}
}

func TestSearchGraphDirectFallbackHydrationFailureAndPoison(t *testing.T) {
	t.Parallel()

	entityID := "acme.ops.test.system.widget.001"
	tests := []struct {
		name         string
		batchReply   []byte
		batchErr     error
		wantContract bool
	}{
		{name: "ordinary failure keeps instance fallback", batchErr: errors.New("batch unavailable")},
		{
			name: "authoritative poison is fatal",
			batchReply: mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{
				{ID: "bad"},
			}}),
			wantContract: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var semanticRequests atomic.Int32
			var batchRequests atomic.Int32
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.embedding.query.search":
					if semanticRequests.Add(1) == 1 {
						return []byte(`{"query":"widget","results":[]}`), nil
					}
					return []byte(`{"query":"widget","results":[{"entity_id":"acme.ops.test.system.widget.001","similarity":0.88}]}`), nil
				case "graph.ingest.query.batch":
					batchRequests.Add(1)
					return test.batchReply, test.batchErr
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			component.communityCache = newCommunityCache(component.logger)

			result, err := component.handleSearchGraph(context.Background(), []byte(`{
				"query":"widget", "include_summaries":false, "summarize_threshold":-1
			}`))
			require.EqualValues(t, 1, batchRequests.Load())
			if test.wantContract {
				require.Nil(t, result)
				require.Error(t, err)
				require.True(t, gtypes.IsStateContractError(err), "error = %T %v", err, err)
				return
			}

			require.NoError(t, err)
			var response GlobalSearchResponse
			require.NoError(t, json.Unmarshal(result, &response))
			require.Equal(t, []EntityDigest{{
				ID: entityID, Type: "widget", Label: "001", Relevance: 0.88,
			}}, response.EntityDigests)
		})
	}
}

func TestLocalSearchDoesNotAddASecondLabelHydrationBatch(t *testing.T) {
	t.Parallel()

	entityID := "acme.ops.test.system.widget.001"
	communityID := "acme.ops.test.cluster.group.001"
	var batchRequests atomic.Int32
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		require.Equal(t, "graph.ingest.query.batch", subject)
		batchRequests.Add(1)
		return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{
			titledEntity(entityID, "Primary Widget"),
		}}), nil
	})
	component.communityCache = newCommunityCache(component.logger)
	component.answerSynthesizer = &TemplateAnswerSynthesizer{}
	generation := newCommunityGeneration(1)
	generation.applyUpdate(communityKVKey(0, communityID), mustCommunityJSON(t, &clustering.Community{
		ID: communityID, Level: 0, Members: []string{entityID}, StatisticalSummary: "widget cluster",
	}))
	component.communityCache.publish(generation)

	result, err := component.handleLocalSearch(context.Background(), []byte(`{
		"entity_id":"acme.ops.test.system.widget.001", "query":"widget", "level":0
	}`))
	require.NoError(t, err)
	require.EqualValues(t, 1, batchRequests.Load())
	var response LocalSearchResponse
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, "Primary Widget", response.EntityDigests[0].Label)
}

func titledEntity(id, title string) gtypes.EntityState {
	return gtypes.EntityState{
		ID: id,
		Triples: []message.Triple{{
			Subject: id, Predicate: "dc.terms.title", Object: title,
		}},
	}
}
