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
	"github.com/c360studio/semstreams/test/e2e/client"
	e2econfig "github.com/c360studio/semstreams/test/e2e/config"
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

func TestTieredFallbackReadsStatisticalDeploymentAuthority(t *testing.T) {
	fixture := newKVFixture(t, "semantic-fallback")
	stem := e2econfig.TierAuthorityStem(e2econfig.VariantStatistical)
	bucket, err := e2econfig.PlatformIdentityBucket(stem)
	require.NoError(t, err)
	fixture.put(bucket, e2econfig.PlatformIdentityKey, map[string]string{
		"org": "c360", "stem": "semstreams-statistical", "id": "semstreams-statistical-abc123",
	})

	// Stop after authority resolution at the first unrelated stage. Execute
	// must read the running deployment before any tiered fixture can use it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "component fixture stops the scenario", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	fixture.s.client = client.NewObservabilityClient(server.URL)
	result, err := fixture.s.Execute(t.Context())
	require.NoError(t, err)
	require.Equal(t, "c360.semstreams-statistical-abc123", result.Details["effective_authority"])
	require.Equal(t, "semantic-fallback", result.Metrics["variant"])
	require.Contains(t, result.Error, "verify-components failed")
}

func TestTieredFallbackRejectsDifferentDeploymentAuthority(t *testing.T) {
	fixture := newKVFixture(t, "semantic-fallback")
	stem := e2econfig.TierAuthorityStem(e2econfig.VariantStatistical)
	bucket, err := e2econfig.PlatformIdentityBucket(stem)
	require.NoError(t, err)
	fixture.put(bucket, e2econfig.PlatformIdentityKey, map[string]string{
		"org": "c360", "stem": "different-deployment", "id": "different-deployment-abc123",
	})
	result, err := fixture.s.Execute(t.Context())
	require.NoError(t, err)
	require.Contains(t, result.Error, "the stack under test is not the configuration this scenario names")
	require.NotContains(t, result.Details, "effective_authority")
}

func TestTieredFallbackGraphProbeUsesStatisticalDeploymentAuthority(t *testing.T) {
	fixture := newKVFixture(t, "semantic-fallback")
	stem := e2econfig.TierAuthorityStem(e2econfig.VariantStatistical)
	bucket, err := e2econfig.PlatformIdentityBucket(stem)
	require.NoError(t, err)
	fixture.put(bucket, e2econfig.PlatformIdentityKey, map[string]string{
		"org": "c360", "stem": "semstreams-statistical", "id": "semstreams-statistical-abc123",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "message logger fixture stops the probe", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	fixture.s.msgLogger = client.NewMessageLoggerClient(server.URL)
	fixture.s.config.GraphQLURL = server.URL
	fixture.s.effectiveAuthority = "c360.semstreams-statistical-abc123"
	result := newResult()
	result.Metrics["variant"] = "semantic-fallback"
	err = fixture.s.executeGraphRoundTrip(t.Context(), result)
	require.ErrorContains(t, err, "message logger is required")
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
					!strings.Contains(request.Query, "semanticSearch(") || request.Variables.Limit != 10 {
					http.Error(w, "unexpected controlled query or variables", http.StatusBadRequest)
					return
				}
				var hit string
				switch request.Variables.Query {
				case search.DefaultQueries()[0].Text:
					hit = tc.entityID
				case search.DefaultQueries()[1].Text, search.DefaultQueries()[5].Text:
					hit = "acme.ops.sensor-temp-001"
				case search.DefaultQueries()[2].Text, search.DefaultQueries()[6].Text:
					hit = "acme.ops.maint-001"
				case search.DefaultQueries()[3].Text:
					hit = "acme.ops.sensor-zone-a-001"
				case search.DefaultQueries()[4].Text:
					hit = "acme.ops.doc-ops-001"
				case search.DefaultQueries()[7].Text:
					hit = "acme.ops.document.content.safety.doc-safety-001"
				default:
					http.Error(w, "unknown search query", http.StatusBadRequest)
					return
				}
				_, _ = fmt.Fprintf(w, `{"data":{"semanticSearch":{"results":[{"entity_id":%q,"similarity":0.9}]}}}`, hit)
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
