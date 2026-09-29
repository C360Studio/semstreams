package scenarios

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
)

// #1426 review round 1 (owner Q2 "absorb"): the class sweep over every stage
// function in the table found twelve more stages that passed on the outcome they
// exist to detect. These tests drive the real stage functions at their seams.

// Every NATS-seam stage the sweep found used to warn and pass with no client.
func TestRound1_NATSSeamStages_NoClientFails(t *testing.T) {
	s := &TieredScenario{config: &TieredConfig{Variant: "statistical"}}
	for name, stage := range map[string]func(context.Context, *Result) error{
		"verify-index-population":             s.executeVerifyIndexPopulation,
		"verify-entity-count":                 s.executeVerifyEntityCount,
		"verify-entity-retrieval":             s.executeVerifyEntityRetrieval,
		"validate-entity-structure":           s.executeValidateEntityStructure,
		"validate-hierarchy-inference":        s.validateHierarchyInference,
		"validate-incoming-index-predicates":  s.validateIncomingIndexPredicates,
		"validate-bidirectional-traversal":    s.validateBidirectionalTraversal,
		"validate-inverse-edges-materialized": s.validateInverseEdgesMaterialized,
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, stage(context.Background(), newResult()))
		})
	}
}

// componentsServer answers /components/list with comps and serves an empty
// Prometheus exposition (plus 404 for the message logger) everywhere else.
func componentsServer(t *testing.T, comps []client.ComponentInfo) *TieredScenario {
	t.Helper()
	reg := prometheus.NewRegistry()
	prom := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/components/list":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(comps))
		case strings.HasPrefix(r.URL.Path, "/metrics"):
			prom.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	metrics := client.NewMetricsClient(srv.URL)
	return &TieredScenario{
		client:  client.NewObservabilityClient(srv.URL),
		metrics: metrics,
		tracer:  client.NewFlowTracer(metrics, client.NewMessageLoggerClient(srv.URL)),
		udpAddr: "127.0.0.1:9", // UDP dial never fails; the datagram goes nowhere
		config:  &TieredConfig{Variant: "statistical"},
	}
}

func comp(name string, healthy bool) client.ComponentInfo {
	return client.ComponentInfo{Name: name, Healthy: healthy, State: map[bool]string{true: "running", false: "error"}[healthy]}
}

func TestVerifyOutputs_MissingOutputFails(t *testing.T) {
	require.NoError(t, componentsServer(t, []client.ComponentInfo{comp("file", true), comp("objectstore", true)}).
		executeVerifyOutputs(context.Background(), newResult()))

	err := componentsServer(t, []client.ComponentInfo{comp("file", true)}).
		executeVerifyOutputs(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing outputs: [objectstore]")
}

func TestValidateProcessing_UnhealthyGraphComponentFails(t *testing.T) {
	healthy := []client.ComponentInfo{comp("graph-ingest", true), comp("graph-index", true), comp("graph-gateway", true)}
	require.NoError(t, componentsServer(t, healthy).executeValidateProcessing(context.Background(), newResult()))

	sick := []client.ComponentInfo{comp("graph-ingest", true), comp("graph-index", false), comp("graph-gateway", true)}
	err := componentsServer(t, sick).executeValidateProcessing(context.Background(), newResult())
	require.Error(t, err)
	require.Contains(t, err.Error(), "graph components not healthy: [graph-index (state=error)]")
}

func TestEmbeddingFallback_UnhealthyGraphEmbeddingFails(t *testing.T) {
	require.NoError(t, componentsServer(t, []client.ComponentInfo{comp("graph-embedding", true)}).
		executeTestEmbeddingFallback(context.Background(), newResult()))

	for name, comps := range map[string][]client.ComponentInfo{
		"unhealthy": {comp("graph-embedding", false)},
		"absent":    {comp("graph-ingest", true)},
	} {
		t.Run(name, func(t *testing.T) {
			err := componentsServer(t, comps).executeTestEmbeddingFallback(context.Background(), newResult())
			require.Error(t, err)
			require.Contains(t, err.Error(), "graph-embedding not healthy")
		})
	}
}

// searchStub answers semanticSearch with hits for every query, computed by hitsFor.
func searchStub(t *testing.T, variant string, hitsFor func(query string) []string) *TieredScenario {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Query string `json:"query"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		results := []map[string]any{}
		for i, id := range hitsFor(req.Variables.Query) {
			results = append(results, map[string]any{"entity_id": id, "similarity": 0.9 - float64(i)*0.01})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"semanticSearch": map[string]any{"query": req.Variables.Query, "results": results}},
		}))
	}))
	t.Cleanup(srv.Close)
	return &TieredScenario{config: &TieredConfig{GraphQLURL: srv.URL, Variant: variant}}
}

// knownAnswers returns, for every default query, one hit per MustInclude
// pattern embedded in a six-part ID, so every known-answer check passes.
func knownAnswers(query string) []string {
	for _, q := range search.DefaultQueries() {
		if q.Text != query {
			continue
		}
		ids := []string{}
		for _, p := range q.MustInclude {
			ids = append(ids, "c360.test.document.content."+p+"x")
		}
		return append(ids, "c360.test.a.b.c.filler")
	}
	return []string{"c360.test.a.b.c.filler"}
}

func TestVerifySearchQuality(t *testing.T) {
	ctx := context.Background()

	t.Run("all known answers found passes in both variants", func(t *testing.T) {
		for _, variant := range []string{"statistical", "semantic"} {
			require.NoError(t, searchStub(t, variant, knownAnswers).executeVerifySearchQuality(ctx, newResult()), variant)
		}
	})

	t.Run("no hits fails in both variants", func(t *testing.T) {
		for _, variant := range []string{"statistical", "semantic"} {
			err := searchStub(t, variant, func(string) []string { return nil }).executeVerifySearchQuality(ctx, newResult())
			require.Error(t, err, variant)
			require.Contains(t, err.Error(), "returned no hits")
		}
	})

	t.Run("transport failure fails", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		s := &TieredScenario{config: &TieredConfig{GraphQLURL: url, Variant: "statistical"}}
		err := s.executeVerifySearchQuality(ctx, newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "http error")
	})

	missSafety := func(q string) []string {
		if strings.HasPrefix(q, "warehouse safety") {
			return []string{"c360.test.record.observation.high.obs-008"}
		}
		return knownAnswers(q)
	}

	t.Run("missed known answer fails under statistical (BM25)", func(t *testing.T) {
		err := searchStub(t, "statistical", missSafety).executeVerifySearchQuality(ctx, newResult())
		require.Error(t, err)
		require.Contains(t, err.Error(), "known-answer search failed under BM25 (6/7 passed)")
	})

	t.Run("missed known answer is recorded under semantic", func(t *testing.T) {
		result := newResult()
		require.NoError(t, searchStub(t, "semantic", missSafety).executeVerifySearchQuality(ctx, result))
		require.Equal(t, 6, result.Metrics["known_answer_tests_passed"])
		require.NotEmpty(t, result.Warnings)
	})

	// The minted ID carries a category segment; the pattern must match it.
	t.Run("safety pattern matches the minted document ID", func(t *testing.T) {
		const minted = "c360.semstreams-statistical-eb03ac.document.content.safety.doc-safety-001"
		for _, q := range search.DefaultQueries() {
			if strings.HasPrefix(q.Text, "warehouse safety") {
				require.Len(t, q.MustInclude, 1)
				require.Contains(t, minted, q.MustInclude[0])
				return
			}
		}
		t.Fatal("safety query missing from DefaultQueries")
	})
}

// B1 (owner Q1, "wait but bound"): validate-rules waits for the asserted counters
// up to ValidationTimeout instead of sampling them once. The counters rise on a
// chosen scrape, so the test synchronises on scrapes, not on sleeps.
// M1: a failed rule-metrics read fails the stage instead of warning.
// H1: test-graphrag-global asserts on synthesized fields, so its client deadline
// comes from globalSearchClientTimeout and a variant overlay can change it. The
// stub holds the request until the client gives up; the 5 s ceiling is far above
// the 50 ms override and far below the 10 s literal the helper replaced.
