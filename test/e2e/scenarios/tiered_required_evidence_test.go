package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/e2eslowconsumer"
	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestTieredControlledSearchRequiresExactFixtureIdentity(t *testing.T) {
	scenario := NewTieredScenario(nil, "", &TieredConfig{Variant: "statistical"})
	scenario.effectiveAuthority = "acme.ops"
	expected := "acme.ops.document.content.operations.doc-ops-001"
	for _, tc := range []struct {
		name  string
		stats *search.Stats
		want  string
	}{
		{"absent", nil, "unavailable"},
		{"wrong identity", &search.Stats{Results: []search.Result{{Query: search.DefaultQueries()[0].Text, Hits: []search.Hit{{EntityID: "other.ops.document.content.operations.doc-ops-001"}}}}}, "missing"},
		{"query error", &search.Stats{Results: []search.Result{{Query: search.DefaultQueries()[0].Text, Error: "connection refused"}}}, "connection refused"},
		{"exact identity", &search.Stats{Results: []search.Result{{Query: search.DefaultQueries()[0].Text, Hits: []search.Hit{{EntityID: expected}}}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation := scenario.controlledSearchObservation(tc.stats)
			if tc.want == "" {
				require.Equal(t, "passed", observation.Status)
			} else {
				require.Equal(t, "failed", observation.Status)
				require.Contains(t, observation.Reason, tc.want)
			}
			require.Equal(t, expected, observation.Evidence["expected_entity_id"])
		})
	}
}

func TestTieredFallbackHasNoRequiredCatalog(t *testing.T) {
	for _, variant := range []string{"semantic-fallback", "unknown"} {
		scenario := NewTieredScenario(nil, "", &TieredConfig{Variant: variant})
		require.Empty(t, scenario.CheckRequirements())
		result := &Result{}
		require.ErrorContains(t, result.DeclareChecks("run", "tiered", scenario.CheckRequirements()),
			"required check membership is empty")
	}
}

func TestTieredControlledSearchRecordsActualQueryResponse(t *testing.T) {
	for _, tc := range []struct {
		name, entityID, wantStatus string
	}{
		{"fixture identity", "acme.ops.document.content.operations.doc-ops-001", "passed"},
		{"foreign identity", "other.ops.document.content.operations.doc-ops-001", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Query     string `json:"query"`
					Variables struct {
						Query string `json:"query"`
						Limit int    `json:"limit"`
					} `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || r.Method != http.MethodPost ||
					!strings.Contains(request.Query, "semanticSearch(") ||
					request.Variables.Query != "What documents mention forklift safety?" || request.Variables.Limit != 10 {
					http.Error(w, "unexpected controlled query or variables", http.StatusBadRequest)
					return
				}
				_, _ = fmt.Fprintf(w, `{"data":{"semanticSearch":{"results":[{"entity_id":%q,"similarity":0.9}]}}}`, tc.entityID)
			}))
			t.Cleanup(server.Close)
			scenario := NewTieredScenario(nil, "", &TieredConfig{Variant: "statistical", GraphQLURL: server.URL})
			scenario.effectiveAuthority = "acme.ops"
			result := newResult()
			require.NoError(t, result.DeclareChecks("run", "tiered", scenario.CheckRequirements()))
			err := scenario.executeVerifySearchQuality(context.Background(), result)
			if tc.wantStatus == "passed" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Len(t, result.CheckObservations, 1)
			require.Equal(t, tc.wantStatus, result.CheckObservations[0].Status)
			require.Equal(t, "statistical.controlled-search.identity", result.CheckObservations[0].ID)
		})
	}
}

func TestStructuralZeroRunRecordsNamedObservation(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerUnrelated(t, reg)
	scenario := metricsFixture(t, reg)
	scenario.config.Variant = "structural"
	result := newResult()
	require.NoError(t, result.DeclareChecks("run", "tiered", scenario.CheckRequirements()))
	require.NoError(t, scenario.executeValidateZeroClusters(context.Background(), result))
	require.Len(t, result.CheckObservations, 1)
	require.Equal(t, "structural.zero-clustering-runs", result.CheckObservations[0].ID)
	require.Equal(t, "passed", result.CheckObservations[0].Status)
}

func TestSlowConsumerIndividualConditionsRetainPartialFailure(t *testing.T) {
	scenario := NewSlowConsumerAttributionScenario(SlowConsumerAttributionConfig{EvidenceRunID: "run", EvidenceMemberID: "slow"})
	result := &Result{}
	require.NoError(t, result.DeclareChecks("run", "slow", scenario.CheckRequirements()))
	err := assertSlowConsumerObservation(result, []map[string]any{{"level": "WRONG"}}, 0)
	require.Error(t, err)
	require.Len(t, result.CheckRequirements, slowConsumerExpectedAssertions)
	require.Len(t, result.CheckObservations, 2)
	require.Equal(t, "failed", result.CheckObservations[1].Status)
	require.Equal(t, "ERROR", result.CheckObservations[1].Evidence["expected"])
	require.Equal(t, "WRONG", result.CheckObservations[1].Evidence["actual"])
	require.Error(t, result.FinalizeChecks())
}

func TestSlowConsumerDropAvailabilityRequiresAbsentKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present bool
		value   any
		passed  bool
	}{
		{"absent", false, nil, true},
		{"present null", true, nil, false},
		{"present false", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := NewSlowConsumerAttributionScenario(SlowConsumerAttributionConfig{})
			result := &Result{}
			require.NoError(t, result.DeclareChecks("run", "slow", scenario.CheckRequirements()))
			record := map[string]any{
				"level": "ERROR", "msg": "NATS error", "component": "natsclient",
				"error": "nats: slow consumer, messages dropped", "subject": e2eslowconsumer.Subject,
				"queue": e2eslowconsumer.Queue, "dropped": float64(e2eslowconsumer.ExpectedDropped),
			}
			if tc.present {
				record["dropped_available"] = tc.value
			}
			err := assertSlowConsumerObservation(result, []map[string]any{record}, 1)
			if tc.passed {
				require.NoError(t, err)
				require.NoError(t, result.FinalizeChecks())
			} else {
				require.Error(t, err)
				require.Error(t, result.FinalizeChecks())
			}
			if tc.passed {
				require.Len(t, result.CheckObservations, slowConsumerExpectedAssertions)
			} else {
				require.Len(t, result.CheckObservations, 9)
				require.Equal(t, "failed", result.CheckObservations[8].Status)
			}
			require.Equal(t, "absent", result.CheckObservations[8].Evidence["expected"])
		})
	}
}
