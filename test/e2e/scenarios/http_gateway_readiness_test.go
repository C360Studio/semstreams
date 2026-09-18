package scenarios

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
)

// gh#1336 — test-http-gateway raced graph-query's community generation.
//
// graph-query serves globalSearch from a community generation its consumer cache
// publishes once per watch (processor/graph-query/community_cache.go:92-99) and
// unpublishes on watch loss (:140-153); while none is published, every community
// path returns the DECLARED transient errs.ClassifiedCode(ErrorTransient,
// graph.ErrorCodeIndexNotReady, "community index is not ready")
// (processor/graph-query/graphrag.go:120-122), which the gateway projects as
// errors[0].extensions {class:"transient", code:"index_not_ready"}
// (gateway/graph-gateway/component.go:1491-1503, pinned by
// gateway/graph-gateway/query_contract_closure_test.go:83-102).
//
// The stage used to turn the first such response into a tier failure. These tests
// drive the stage against a stub gateway speaking that exact envelope.

// notReadyEnvelope is the gateway's projection of graph-query's readiness
// transient, byte-for-byte the shape query_contract_closure_test.go pins.
const notReadyEnvelope = `{"errors":[{"message":"community index is not ready",` +
	`"extensions":{"class":"transient","code":"` + graph.ErrorCodeIndexNotReady + `"}}]}`

// readyEnvelope is a served globalSearch answer.
const readyEnvelope = `{"data":{"globalSearch":{"entities":[{"id":"acme.ops.e2e.graph.entity.001",` +
	`"type":"Entity"}],"count":1,"strategy":"graphrag"}}}`

// stubGateway serves bodies in order, repeating the last one forever, and counts
// the requests it answered.
func stubGateway(t *testing.T, bodies ...string) (url string, requests *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(n.Add(1)) - 1
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, bodies[i])
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &n
}

func readinessScenario(url string) (*TieredScenario, *Result) {
	return &TieredScenario{config: &TieredConfig{GraphQLURL: url}},
		&Result{Metrics: map[string]any{}, Details: map[string]any{}}
}

// TestHTTPGatewayStageWaitsOutCommunityIndexNotReady is the gh#1336 regression.
// The stage must retry the declared readiness transient and assert against the
// first served answer, not fail on the race.
func TestHTTPGatewayStageWaitsOutCommunityIndexNotReady(t *testing.T) {
	t.Parallel()

	url, requests := stubGateway(t, notReadyEnvelope, readyEnvelope)
	s, result := readinessScenario(url)

	if err := s.executeTestHTTPGateway(context.Background(), result); err != nil {
		t.Fatalf("stage failed on a readiness transient it must wait out: %v", err)
	}

	if got := requests.Load(); got != 2 {
		t.Errorf("gateway requests = %d, want 2 (one not-ready, then the served answer)", got)
	}
	if got := result.Details["graphql_gateway_strategy"]; got != "graphrag" {
		t.Errorf("graphql_gateway_strategy = %v, want graphrag — the stage's assertions must be unchanged", got)
	}
	if got := result.Metrics["graphql_gateway_search_hits"]; got != 1 {
		t.Errorf("graphql_gateway_search_hits = %v, want 1", got)
	}
	// The wait is a declared event, not a private smoothing.
	if got := result.Metrics["graphql_gateway_index_not_ready_retries"]; got != 1 {
		t.Errorf("graphql_gateway_index_not_ready_retries = %v, want 1", got)
	}
	if _, ok := result.Metrics["graphql_gateway_readiness_wait_ms"]; !ok {
		t.Error("graphql_gateway_readiness_wait_ms was not recorded")
	}
}

// TestHTTPGatewayStageRecordsZeroRetriesWhenReadyImmediately keeps the declared
// event honest in the common case: the metrics exist and read zero.
func TestHTTPGatewayStageRecordsZeroRetriesWhenReadyImmediately(t *testing.T) {
	t.Parallel()

	url, requests := stubGateway(t, readyEnvelope)
	s, result := readinessScenario(url)

	if err := s.executeTestHTTPGateway(context.Background(), result); err != nil {
		t.Fatalf("stage failed against a ready gateway: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("gateway requests = %d, want 1", got)
	}
	if got := result.Metrics["graphql_gateway_index_not_ready_retries"]; got != 0 {
		t.Errorf("graphql_gateway_index_not_ready_retries = %v, want 0", got)
	}
}

// TestHTTPGatewayStageFailsClosedOnNonReadinessError proves the wait covers ONLY
// the declared readiness transient. Anything else — a different code, a
// different class, an uncoded error — fails on the first response, with the
// stage's existing message, and is never retried.
func TestHTTPGatewayStageFailsClosedOnNonReadinessError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "different code, same class",
			body: `{"errors":[{"message":"embeddings unavailable","extensions":` +
				`{"class":"transient","code":"` + graph.ErrorCodeEmbeddingUnavailable + `"}}]}`,
		},
		{
			name: "same code, non-transient class",
			body: `{"errors":[{"message":"community index is not ready","extensions":` +
				`{"class":"fatal","code":"` + graph.ErrorCodeIndexNotReady + `"}}]}`,
		},
		{
			name: "uncoded error",
			body: `{"errors":[{"message":"query failed"}]}`,
		},
		{
			name: "readiness transient beside a real error",
			body: `{"errors":[{"message":"community index is not ready","extensions":` +
				`{"class":"transient","code":"` + graph.ErrorCodeIndexNotReady + `"}},` +
				`{"message":"graph state reset required","extensions":{"class":"fatal","code":"` +
				graph.ErrorCodeGraphStateResetRequired + `"}}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			url, requests := stubGateway(t, tc.body)
			s, result := readinessScenario(url)

			err := s.executeTestHTTPGateway(context.Background(), result)
			if err == nil {
				t.Fatal("stage passed on an error it must not wait out")
			}
			if !strings.Contains(err.Error(), "GraphQL gateway search error") {
				t.Errorf("error = %q, want the stage's existing non-readiness message", err)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("gateway requests = %d, want 1 — a non-readiness error must not be retried", got)
			}
		})
	}
}

// TestHTTPGatewayStageTimeoutNamesTheIndexAndTheElapsedTime: a community
// generation that never publishes is a real finding, and the failure has to say
// what was waited on and for how long — otherwise the wait converts a broken
// deployment into a mystery timeout. Driven at the bounded seam so the assertion
// costs milliseconds instead of the production bound.
func TestHTTPGatewayStageTimeoutNamesTheIndexAndTheElapsedTime(t *testing.T) {
	t.Parallel()

	url, requests := stubGateway(t, notReadyEnvelope)
	s, result := readinessScenario(url)

	_, err := s.awaitReadyGatewayGlobalSearch(context.Background(), result,
		40*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("stage passed against a gateway that never became ready")
	}
	for _, want := range []string{
		"community",                    // WHAT was waited on
		graph.ErrorCodeIndexNotReady,   // the signal that was polled
		"attempts",                     // HOW MANY probes
		"community index is not ready", // the gateway's own last word
		"40ms",                         // the bound that was applied
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("timeout error %q does not name %q", err, want)
		}
	}
	if got := requests.Load(); got < 2 {
		t.Errorf("gateway requests = %d, want the bound to have been polled more than once", got)
	}
	// The elapsed time is recorded too, so a run that waited is visible in the
	// results file and not only in the failure text.
	if _, ok := result.Metrics["graphql_gateway_readiness_wait_ms"]; !ok {
		t.Error("graphql_gateway_readiness_wait_ms was not recorded on the timeout path")
	}
}

// TestHTTPGatewayStageStopsOnContextCancellation keeps the wait joined to the
// scenario's context rather than to wall clock alone.
func TestHTTPGatewayStageStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	url, _ := stubGateway(t, notReadyEnvelope)
	s, result := readinessScenario(url)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.awaitReadyGatewayGlobalSearch(ctx, result, time.Minute, time.Millisecond); err == nil {
		t.Fatal("stage ignored a cancelled context")
	}
}
