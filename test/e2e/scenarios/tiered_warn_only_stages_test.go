package scenarios

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// #1426: every stage below used to append the outcome it exists to detect to
// result.Warnings and return nil, so the per-PR tier was green on it. These
// tests drive the real stage functions against the production seam
// (s.config.GraphQLURL over HTTP, the metrics scrape) and require the error.

// graphqlStub answers every POST with the body pick returns for the request's
// GraphQL text, and records the decoded request variables.
type graphqlStub struct {
	mu        sync.Mutex
	variables []map[string]any
}

func (g *graphqlStub) serve(t *testing.T, pick func(query string) string) *TieredScenario {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(raw, &req))
		g.mu.Lock()
		g.variables = append(g.variables, req.Variables)
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pick(req.Query))
	}))
	t.Cleanup(srv.Close)
	return &TieredScenario{config: &TieredConfig{GraphQLURL: srv.URL, Variant: "statistical"}}
}

func fixed(body string) func(string) string { return func(string) string { return body } }

func TestWarnOnlyStages_EmptyAnswerFails(t *testing.T) {
	cases := []struct {
		name  string
		stage func(*TieredScenario) func(context.Context, *Result) error
		body  string
		want  string
	}{
		{"test-spatial-query", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestSpatialQuery },
			`{"data":{"spatialSearch":[]}}`, "spatial query returned 0 entities"},
		{"test-temporal-query", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestTemporalQuery },
			`{"data":{"temporalSearch":[]}}`, "temporal query returned 0 entities"},
		{"test-zone-relationships", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestZoneRelationships },
			`{"data":{"relationships":[]}}`, "has 0 incoming relationships"},
		{"test-predicate-list", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestPredicateList },
			`{"data":{"predicates":{"predicates":[],"total":0}}}`, "no predicates found"},
		{"test-nl-path-intent", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestNLPathIntent },
			`{"data":{"globalSearch":{"entities":[],"count":0}}}`,
			"NL path intent: 0/3 probes returned entities; first failure: path_intent_related_to: returned 0 entities"},
		{"test-nl-temporal-intent", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestNLTemporalIntent },
			`{"data":{"globalSearch":{"entities":[],"count":0}}}`,
			"NL temporal intent: 0/2 probes returned entities; first failure: temporal_last_hour: returned 0 entities"},
		{"test-graphrag-global", func(s *TieredScenario) func(context.Context, *Result) error { return s.executeTestGraphRAGGlobal },
			`{"data":{"globalSearch":{"entities":[],"community_summaries":[],"count":0}}}`, "returned no community summaries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := (&graphqlStub{}).serve(t, fixed(tc.body))
			s.effectiveAuthority = "acme.ops"
			err := tc.stage(s)(context.Background(), newResult())
			require.Error(t, err, "%s must fail on the outcome it exists to detect", tc.name)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// A transport failure and an empty answer never share a message (spec: A probe's
// deadline is reported distinctly from an empty result).
func TestNLIntent_TransportFailureIsNotAnEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler) // drop the connection: a transport error, not a body
	}))
	t.Cleanup(srv.Close)
	s := &TieredScenario{config: &TieredConfig{GraphQLURL: srv.URL}}

	err := s.executeTestNLPathIntent(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "first failure: path_intent_related_to: NL query request failed:")
	require.NotContains(t, err.Error(), "returned 0 entities")
}

// The NL probes read entities only, so they must not ask for synthesis (#1426).
func TestNLIntent_ProbesDeclineSummaries(t *testing.T) {
	stub := &graphqlStub{}
	s := stub.serve(t, fixed(`{"data":{"globalSearch":{"entities":[{"id":"a.b.c.d.e.f","type":"t"}],"count":1}}}`))

	require.NoError(t, s.executeTestNLPathIntent(context.Background(), newResult()))
	require.NoError(t, s.executeTestNLTemporalIntent(context.Background(), newResult()))
	require.Len(t, stub.variables, 5)
	for _, v := range stub.variables {
		require.Equal(t, false, v["includeSummaries"], "variables: %v", v)
	}
}

func TestGraphRAGGlobal_EmptyAnswerBesideSummariesFails(t *testing.T) {
	s := (&graphqlStub{}).serve(t, fixed(`{"data":{"globalSearch":{"entities":[],`+
		`"community_summaries":[{"community_id":"c1","summary":"s","member_count":2}],"count":0,"answer":""}}}`))
	err := s.executeTestGraphRAGGlobal(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "answer field empty")
}

func TestGraphRAG_RequestFailureFails(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // closed port: the request itself fails
	s := &TieredScenario{config: &TieredConfig{GraphQLURL: url}}

	err := s.executeTestGraphRAGGlobal(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "GraphRAG global search failed: request failed:")
}

func TestGraphRAGLocal_NoEntitiesFails(t *testing.T) {
	var resp graphRAGLocalResponse
	resp.Data.LocalSearch.CommunityID = "c1"
	s := &TieredScenario{config: &TieredConfig{}}
	err := s.validateGraphRAGLocalResult(&resp, "a.b.c.d.e.f", "q", 0, newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "returned no entities")
}

// With no NATS client the community lookup fails; that arm used to warn and pass.
func TestNATSSeamStages_NoClientFails(t *testing.T) {
	s := &TieredScenario{config: &TieredConfig{}}
	for name, stage := range map[string]func(context.Context, *Result) error{
		"test-graphrag-local":          s.executeTestGraphRAGLocal,
		"validate-community-structure": s.executeValidateCommunityStructure,
		"validate-llm-enhancement":     s.executeValidateLLMEnhancement,
		"validate-virtual-edges":       s.executeValidateVirtualEdges,
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, stage(context.Background(), newResult()))
		})
	}
}

func TestPredicateStats(t *testing.T) {
	list := `{"data":{"predicates":{"predicates":[{"predicate":"sensor.measurement.celsius","entity_count":3}],"total":1}}}`
	stats := func(count int) func(string) string {
		return func(q string) string {
			if strings.Contains(q, "predicateStats") {
				b, _ := json.Marshal(map[string]any{"data": map[string]any{"predicateStats": map[string]any{
					"predicate": "sensor.measurement.celsius", "entity_count": count,
					"sample_entities": []string{"a.b.c.d.e.f"},
				}}})
				return string(b)
			}
			return list
		}
	}

	t.Run("reads the handler's snake_case body", func(t *testing.T) {
		s := (&graphqlStub{}).serve(t, stats(3))
		result := newResult()
		require.NoError(t, s.executeTestPredicateStats(context.Background(), result))
		require.Equal(t, 3, result.Metrics["predicate_stats_entity_count"])
		require.Equal(t, 1, result.Metrics["predicate_stats_sample_count"])
	})
	t.Run("zero entities for a listed predicate fails", func(t *testing.T) {
		s := (&graphqlStub{}).serve(t, stats(0))
		err := s.executeTestPredicateStats(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "reported 0 entities for a listed predicate")
	})
	t.Run("no predicates listed fails", func(t *testing.T) {
		s := (&graphqlStub{}).serve(t, fixed(`{"data":{"predicates":{"predicates":[]}}}`))
		err := s.executeTestPredicateStats(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "no predicates available")
	})
	t.Run("list request failure fails", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		s := &TieredScenario{config: &TieredConfig{GraphQLURL: url}}
		err := s.executeTestPredicateStats(context.Background(), newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "predicate list request failed:")
	})
}

func TestPredicateList_ReadsSnakeCaseEntityCount(t *testing.T) {
	s := (&graphqlStub{}).serve(t, fixed(
		`{"data":{"predicates":{"predicates":[{"predicate":"sensor.measurement.celsius","entity_count":7}],"total":1}}}`))
	result := newResult()
	require.NoError(t, s.executeTestPredicateList(context.Background(), result))
	preds := result.Details["predicate_list_test"].(map[string]any)["predicates"].([]map[string]any)
	require.Equal(t, 7, preds[0]["entity_count"])
}

// D3(b): validate-rules is the one home of MinRuleFirings/MinActionsDispatched.
func TestValidateRules_AssertsActivityThresholds(t *testing.T) {
	scenario := func(t *testing.T, firings, actions float64) *TieredScenario {
		t.Helper()
		reg := prometheus.NewRegistry()
		for name, v := range map[string]float64{
			"semstreams_rule_evaluations_total":      200, // >= 100 skips the evaluation wait
			"semstreams_rule_triggers_total":         firings,
			"semstreams_rule_events_published_total": actions,
		} {
			c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: name})
			c.Add(v)
			require.NoError(t, reg.Register(c))
		}
		srv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		t.Cleanup(srv.Close)
		return &TieredScenario{
			metrics: client.NewMetricsClient(srv.URL),
			config:  &TieredConfig{MinRuleFirings: 2, MinActionsDispatched: 1},
		}
	}

	require.NoError(t, scenario(t, 3, 5).executeValidateRules(context.Background(), newResult()))

	err := scenario(t, 1, 5).executeValidateRules(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "rule firings 1 < MinRuleFirings 2")

	err = scenario(t, 3, 0).executeValidateRules(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "actions dispatched 0 < MinActionsDispatched 1")
}

// The handler answers community_summaries/community_id; a camelCase decoder read
// zero summaries on every run (#1426, the D4(a) class).
func TestGraphRAGGlobal_ReadsSnakeCaseSummaries(t *testing.T) {
	s := (&graphqlStub{}).serve(t, fixed(`{"data":{"globalSearch":{"entities":[{"id":"a.b.c.d.e.f","type":"t"}],`+
		`"community_summaries":[{"community_id":"c1","summary":"s1","member_count":2},{"community_id":"c2","summary":"s2","member_count":3}],`+
		`"count":1,"answer":"Found 1 entities across 2 knowledge clusters."}}}`))
	result := newResult()
	require.NoError(t, s.executeTestGraphRAGGlobal(context.Background(), result))
	require.Equal(t, 2, result.Metrics["graphrag_global_communities_found"])
}
