package graphquery

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/stretchr/testify/require"
)

func TestSliceESpecializedIndexRowsUseCanonicalID(t *testing.T) {
	got, err := parseEntityIDsFromResults([]byte(`[{"id":"acme.ops.robotics.gcs.drone.001"},{"id":"acme.ops.robotics.gcs.sensor.002"}]`))
	require.NoError(t, err)
	require.Equal(t, []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.sensor.002"}, got)
}

func TestSliceEGlobalSearchReportsTerminalStrategy(t *testing.T) {
	id := "acme.ops.robotics.gcs.drone.001"
	tests := []struct {
		name      string
		call      func(*Component) ([]byte, error)
		want      string
		wantCount int
	}{
		{
			name: "graphrag direct",
			call: func(c *Component) ([]byte, error) {
				return c.handleGlobalSearch(context.Background(), []byte(`{"query":"drone","include_summaries":false,"summarize_threshold":-1}`))
			},
			want:      "graphrag",
			wantCount: 1,
		},
		{
			name: "semantic direct success",
			call: func(c *Component) ([]byte, error) {
				return c.handleStrategySemantic(context.Background(), "drone", nil, &GlobalSearchRequest{Query: "drone"}, time.Now())
			},
			want:      "semantic",
			wantCount: 1,
		},
		{
			name: "semantic empty success",
			call: func(c *Component) ([]byte, error) {
				return c.handleStrategySemantic(context.Background(), "drone", nil, &GlobalSearchRequest{Query: "drone"}, time.Now())
			},
			want: "semantic",
		},
		{
			name: "temporal direct empty success",
			call: func(c *Component) ([]byte, error) {
				cr := &query.ClassificationResult{Options: map[string]any{"time_range": &query.TimeRange{Start: time.Unix(0, 0), End: time.Unix(60, 0)}}}
				include := false
				return c.handleStrategyTemporal(context.Background(), cr, &GlobalSearchRequest{Query: "recent", IncludeSummaries: &include}, time.Now())
			},
			want: "temporal",
		},
		{
			name: "temporal direct success",
			call: func(c *Component) ([]byte, error) {
				cr := &query.ClassificationResult{Options: map[string]any{"time_range": &query.TimeRange{Start: time.Unix(0, 0), End: time.Unix(60, 0)}}}
				include := false
				return c.handleStrategyTemporal(context.Background(), cr, &GlobalSearchRequest{Query: "recent", IncludeSummaries: &include}, time.Now())
			},
			want:      "temporal",
			wantCount: 1,
		},
		{
			name: "spatial direct empty success",
			call: func(c *Component) ([]byte, error) {
				cr := &query.ClassificationResult{Options: map[string]any{"geo_bounds": &query.SpatialBounds{North: 1, South: 0, East: 1, West: 0}}}
				include := false
				return c.handleStrategySpatial(context.Background(), cr, &GlobalSearchRequest{Query: "nearby", IncludeSummaries: &include}, time.Now())
			},
			want: "spatial",
		},
		{
			name: "spatial direct success",
			call: func(c *Component) ([]byte, error) {
				cr := &query.ClassificationResult{Options: map[string]any{"geo_bounds": &query.SpatialBounds{North: 1, South: 0, East: 1, West: 0}}}
				include := false
				return c.handleStrategySpatial(context.Background(), cr, &GlobalSearchRequest{Query: "nearby", IncludeSummaries: &include}, time.Now())
			},
			want:      "spatial",
			wantCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.embedding.query.search":
					if tc.wantCount > 0 {
						return []byte(`{"results":[{"entity_id":"acme.ops.robotics.gcs.drone.001","similarity":0.9}],"embedder_type":"neural"}`), nil
					}
					return nil, fmt.Errorf("semantic unavailable")
				case "graph.ingest.query.batch":
					return mustMarshalQueryFixture(t, map[string]any{"entities": []gtypes.EntityState{{ID: id}}}), nil
				case "graph.temporal.query.range", "graph.spatial.query.bounds":
					if tc.wantCount > 0 {
						return []byte(`[{"id":"acme.ops.robotics.gcs.drone.001"}]`), nil
					}
					return []byte(`[]`), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			result, err := tc.call(component)
			require.NoError(t, err)
			var response GlobalSearchResponse
			require.NoError(t, json.Unmarshal(result, &response))
			require.Equal(t, tc.want, response.Strategy)
			require.Equal(t, tc.wantCount, response.Count)
		})
	}
}

func TestSliceEGlobalSearchReportsEntityAndPathStrategies(t *testing.T) {
	const id = "acme.ops.robotics.gcs.drone.001"

	t.Run("entity lookup direct", func(t *testing.T) {
		component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
			require.Equal(t, "graph.ingest.query.batch", subject)
			return mustMarshalQueryFixture(t, map[string]any{"entities": []*gtypes.EntityState{{ID: id}}}), nil
		})
		classification := &query.ClassificationResult{Options: map[string]any{"path_start_node": id}}

		result, handled, err := component.handleStrategyEntityLookup(
			context.Background(), classification, id, &GlobalSearchRequest{Query: id}, time.Now(),
		)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, "entity_lookup", sliceEResponseStrategy(t, result))
	})

	t.Run("path direct", func(t *testing.T) {
		component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
			switch subject {
			case "graph.ingest.query.entity":
				return mustMarshalQueryFixture(t, gtypes.ExactEntity{
					Entity:     &gtypes.EntityState{ID: id},
					KVRevision: 1,
				}), nil
			case "graph.index.query.outgoing":
				return mustMarshalQueryFixture(t, gtypes.NewQueryResponse(gtypes.OutgoingRelationshipsData{
					Relationships: []gtypes.OutgoingEntry{},
				})), nil
			case "graph.ingest.query.batch":
				return mustMarshalQueryFixture(t, map[string]any{"entities": []*gtypes.EntityState{{ID: id}}}), nil
			default:
				return nil, fmt.Errorf("unexpected request: %s", subject)
			}
		})
		classification := &query.ClassificationResult{Options: map[string]any{
			"path_intent":     true,
			"path_start_node": id,
		}}

		result, handled, err := component.tryPathIntentSearch(
			context.Background(), classification, &GlobalSearchRequest{Query: "connected"}, time.Now(),
		)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, "pathrag", sliceEResponseStrategy(t, result))
	})

	t.Run("path empty", func(t *testing.T) {
		component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
			switch subject {
			case "graph.index.query.alias":
				return []byte(`{"data":{"canonical_id":null}}`), nil
			case "graph.ingest.query.suffix":
				return []byte(`{"id":""}`), nil
			default:
				return nil, fmt.Errorf("unexpected request: %s", subject)
			}
		})
		classification := &query.ClassificationResult{Options: map[string]any{
			"path_intent":     true,
			"path_start_node": "drone-001",
		}}

		result, handled, err := component.tryPathIntentSearch(
			context.Background(), classification, &GlobalSearchRequest{Query: "connected"}, time.Now(),
		)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, "pathrag", sliceEResponseStrategy(t, result))
	})
}

func TestSliceEGlobalSearchReportsFallbackExecutor(t *testing.T) {
	tests := []struct {
		name     string
		strategy query.SearchStrategy
		options  map[string]any
	}{
		{name: "entity lookup falls through", strategy: query.StrategyExact},
		{name: "path falls through", strategy: query.StrategyPathRAG, options: map[string]any{"path_intent": true, "path_start_node": "acme.ops.robotics.gcs.drone.001"}},
		{name: "temporal falls through", strategy: query.StrategyTemporalGraphRAG},
		{name: "spatial falls through", strategy: query.StrategyGeoGraphRAG},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			queryText := "slice e fallback " + tc.name
			options := map[string]any{"strategy": string(tc.strategy)}
			for key, value := range tc.options {
				options[key] = value
			}
			component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				switch subject {
				case "graph.ingest.query.entity":
					return nil, fmt.Errorf("path unavailable")
				case "graph.embedding.query.search":
					return nil, fmt.Errorf("semantic unavailable")
				default:
					return nil, fmt.Errorf("unexpected request: %s", subject)
				}
			})
			component.classifier = sliceEClassifier(queryText, options)
			component.communityCache = newTestCache()
			testGeneration(component.communityCache)

			result, err := component.handleGlobalSearch(context.Background(), mustMarshalQueryFixture(t, GlobalSearchRequest{
				Query:              queryText,
				IncludeSummaries:   sliceEBool(false),
				SummarizeThreshold: sliceEInt(-1),
			}))
			require.NoError(t, err)
			require.Equal(t, "graphrag", sliceEResponseStrategy(t, result))
		})
	}
}

func TestSliceEGlobalSearchReportsGraphRAGEmptySuccess(t *testing.T) {
	component := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		require.Equal(t, "graph.embedding.query.search", subject)
		return nil, fmt.Errorf("semantic unavailable")
	})
	component.communityCache = newTestCache()
	testGeneration(component.communityCache)

	result, err := component.handleGlobalSearch(context.Background(), []byte(`{
		"query":"nothing here", "include_summaries":false, "summarize_threshold":-1
	}`))
	require.NoError(t, err)
	require.Equal(t, "graphrag", sliceEResponseStrategy(t, result))
}

func sliceEClassifier(queryText string, options map[string]any) *query.ClassifierChain {
	domain := &query.DomainExamples{Examples: []query.Example{{
		Query:   queryText,
		Intent:  "slice_e_contract",
		Options: options,
	}}}
	return query.NewClassifierChain(nil, query.NewEmbeddingClassifier([]*query.DomainExamples{domain}, 0))
}

func sliceEResponseStrategy(t *testing.T, data []byte) string {
	t.Helper()
	var response GlobalSearchResponse
	require.NoError(t, json.Unmarshal(data, &response))
	require.NotEmpty(t, response.Strategy)
	return response.Strategy
}

func sliceEBool(value bool) *bool { return &value }

func sliceEInt(value int) *int { return &value }
