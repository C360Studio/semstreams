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

func TestCaptureFireEveryNBaseline_AbsentFirstPublishSeriesIsZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# no rule event metric\n"))
	}))
	t.Cleanup(server.Close)

	scenario := &Scenario{
		config:  &Config{MetricsURL: server.URL},
		metrics: client.NewMetricsClient(server.URL),
	}
	baseline, err := scenario.captureFireEveryNBaseline(context.Background())
	if err != nil {
		t.Fatalf("absent first-publish series: %v", err)
	}
	if baseline != 0 {
		t.Fatalf("absent first-publish series baseline = %v, want 0", baseline)
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
	if err == nil || !strings.Contains(err.Error(), fireEveryNMetricName) {
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

	err := scenario.assertFireEveryNGate(context.Background(), result, 0)
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("unavailable baseline error = %v, want fail-closed diagnostic", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unavailable baseline was downgraded to warnings: %v", result.Warnings)
	}
}
