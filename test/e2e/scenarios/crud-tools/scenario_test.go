package crudtools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestCaptureFireEveryNBaseline_AbsentPerRuleSeriesAreZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# no per-rule counter series yet\n"))
	}))
	t.Cleanup(server.Close)

	scenario := &Scenario{
		config:  &Config{MetricsURL: server.URL},
		metrics: client.NewMetricsClient(server.URL),
	}
	baseline, err := scenario.captureFireEveryNBaseline(context.Background())
	if err != nil {
		t.Fatalf("absent per-rule series: %v", err)
	}
	if baseline != (fireEveryNMetricValues{}) {
		t.Fatalf("absent per-rule series baseline = %+v, want zero values", baseline)
	}
}

func TestCaptureFireEveryNBaseline_UnreachableEndpointFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	metricsURL := server.URL
	server.Close()

	scenario := &Scenario{
		config:  &Config{MetricsURL: metricsURL},
		metrics: client.NewMetricsClient(metricsURL),
	}
	_, err := scenario.captureFireEveryNBaseline(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fire_every_n_events baseline") {
		t.Fatalf("unreachable metrics endpoint error = %v, want fail-closed diagnostic", err)
	}
}

func TestWaitForFireEveryNRuleHotReload_UnavailableBaselineDoesNotFallback(t *testing.T) {
	scenario := &Scenario{baselineActiveRules: -1}
	result := &scenarios.Result{Details: make(map[string]any)}
	started := time.Now()

	err := scenario.waitForFireEveryNRuleHotReload(context.Background(), result)
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("unavailable baseline error = %v, want fail-closed diagnostic", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("unavailable baseline used a timing fallback: returned after %s", elapsed)
	}
}

func TestAssertFireEveryNGate_MissingConfiguredBaselineFailsClosed(t *testing.T) {
	scenario := &Scenario{baselineActiveRules: -1}
	result := &scenarios.Result{
		Metrics:  make(map[string]any),
		Details:  make(map[string]any),
		Warnings: []string{},
	}

	err := scenario.assertFireEveryNGate(context.Background(), result, fireEveryNMetricValues{})
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("unavailable baseline error = %v, want fail-closed diagnostic", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unavailable baseline was downgraded to warnings: %v", result.Warnings)
	}
}

func TestAssertFireEveryNGate_RecordsExactDeltasAsMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"semstreams_rule_evaluations_total{rule_name=\"e2e-fire-every-n-test\",result=\"triggered\"} 9\n" +
				"semstreams_rule_evaluations_total{rule_name=\"e2e-fire-every-n-test\",result=\"not_triggered\"} 0\n" +
				"semstreams_rule_action_gate_passes_total{rule_name=\"e2e-fire-every-n-test\"} 3\n",
		))
	}))
	t.Cleanup(server.Close)

	scenario := &Scenario{
		baselineActiveRules: 0,
		metrics:             client.NewMetricsClient(server.URL),
	}
	result := &scenarios.Result{
		Metrics: make(map[string]any),
		Details: make(map[string]any),
	}

	if err := scenario.assertFireEveryNGate(
		context.Background(), result, fireEveryNMetricValues{},
	); err != nil {
		t.Fatalf("assert exact fire-every-n gate: %v", err)
	}

	for key, want := range map[string]float64{
		"fire_every_n_triggered_delta":     9,
		"fire_every_n_not_triggered_delta": 0,
		"fire_every_n_gate_passes_delta":   3,
	} {
		if got := result.Metrics[key]; got != want {
			t.Fatalf("result metric %q = %v, want %v", key, got, want)
		}
	}
}
