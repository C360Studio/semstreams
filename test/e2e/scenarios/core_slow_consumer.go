package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/c360studio/semstreams/internal/e2eslowconsumer"
)

const (
	slowConsumerExpectedAssertions = 11
	slowConsumerObservationTimeout = 10 * time.Second
	slowConsumerPollInterval       = 50 * time.Millisecond
)

// SlowConsumerAttributionConfig identifies the isolated disposable E2E stack.
type SlowConsumerAttributionConfig struct {
	AppContainer string
	MetricsURL   string
}

// SlowConsumerAttributionScenario externally observes the tagged product fixture.
type SlowConsumerAttributionScenario struct {
	config SlowConsumerAttributionConfig
}

// NewSlowConsumerAttributionScenario creates the isolated product-assembly proof.
func NewSlowConsumerAttributionScenario(config SlowConsumerAttributionConfig) *SlowConsumerAttributionScenario {
	return &SlowConsumerAttributionScenario{config: config}
}

// Name returns the stable scenario name.
func (*SlowConsumerAttributionScenario) Name() string { return "core-slow-consumer" }

// Description describes the externally observed behavior.
func (*SlowConsumerAttributionScenario) Description() string {
	return "assembled cmd/semstreams emits exact slow-consumer attribution"
}

// Setup requires no host-side mutation; compose owns the disposable stack.
func (*SlowConsumerAttributionScenario) Setup(context.Context) error { return nil }

// Teardown requires no scenario mutation; the task owns bounded compose teardown.
func (*SlowConsumerAttributionScenario) Teardown(context.Context) error { return nil }

// Execute observes configured JSON stdout and the existing counter.
func (s *SlowConsumerAttributionScenario) Execute(parent context.Context) (*Result, error) {
	start := time.Now()
	result := &Result{ScenarioName: s.Name(), StartTime: start}
	defer func() {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(start)
	}()

	ctx, cancel := context.WithTimeout(parent, slowConsumerObservationTimeout)
	defer cancel()
	records, counter, err := s.waitForObservation(ctx)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if err := assertSlowConsumerObservation(result, records, counter); err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.Success = true
	result.Metrics = map[string]any{
		"assertions_run": result.AssertionsRun,
		"known_dropped":  e2eslowconsumer.ExpectedDropped,
	}
	return result, nil
}

func (s *SlowConsumerAttributionScenario) waitForObservation(
	ctx context.Context,
) ([]map[string]any, float64, error) {
	ticker := time.NewTicker(slowConsumerPollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		records, counter, err := s.readObservation(ctx)
		if err == nil && len(records) > 0 {
			return records, counter, nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil, 0, fmt.Errorf("observe slow-consumer diagnostic: %w (last observation: %v)",
				ctx.Err(), lastErr)
		}
	}
}

func (s *SlowConsumerAttributionScenario) readObservation(
	ctx context.Context,
) ([]map[string]any, float64, error) {
	logs, err := exec.CommandContext(ctx, "docker", "logs", s.config.AppContainer).Output()
	if err != nil {
		return nil, 0, fmt.Errorf("read docker logs: %w", err)
	}
	records, err := parseSlowConsumerRecords(string(logs))
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(s.config.MetricsURL, "/")+"/metrics", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create metrics request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("read metrics: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("metrics status %d", response.StatusCode)
	}
	metrics, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read metrics body: %w", err)
	}
	counter, err := parseNATSClientErrorCounter(string(metrics))
	if err != nil {
		return nil, 0, err
	}
	return records, counter, nil
}

func parseSlowConsumerRecords(output string) ([]map[string]any, error) {
	records := make([]map[string]any, 0, 1)
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse JSON log record: %w", err)
		}
		if record["msg"] == "NATS error" && record["subject"] == e2eslowconsumer.Subject {
			records = append(records, record)
		}
	}
	return records, nil
}

func parseNATSClientErrorCounter(metrics string) (float64, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(metrics))
	if err != nil {
		return 0, fmt.Errorf("parse metrics: %w", err)
	}
	family := families["semstreams_log_entries_total"]
	if family == nil {
		return 0, errors.New("semstreams_log_entries_total is absent")
	}
	for _, sample := range family.Metric {
		labels := make(map[string]string, len(sample.Label))
		for _, label := range sample.Label {
			labels[label.GetName()] = label.GetValue()
		}
		if labels["component"] == "natsclient" && labels["level"] == "error" && sample.Counter != nil {
			return sample.Counter.GetValue(), nil
		}
	}
	return 0, errors.New("natsclient ERROR counter sample is absent")
}

func assertSlowConsumerObservation(result *Result, records []map[string]any, counter float64) error {
	if err := requireSlowConsumer(result, len(records) == 1,
		"matching NATS error records=%d, want 1", len(records)); err != nil {
		return err
	}
	record := records[0]
	checks := []struct {
		condition bool
		message   string
	}{
		{record["level"] == "ERROR", "level must be ERROR"},
		{record["msg"] == "NATS error", "message must be NATS error"},
		{record["component"] == "natsclient", "component must be natsclient"},
		{record["error"] == "nats: slow consumer, messages dropped", "error must preserve ErrSlowConsumer"},
		{record["subject"] == e2eslowconsumer.Subject, "subject must identify the fixture"},
		{record["queue"] == e2eslowconsumer.Queue, "queue must identify the fixture"},
		{record["dropped"] == float64(e2eslowconsumer.ExpectedDropped), "dropped must equal exact fixture overflow"},
		{record["dropped_available"] == nil, "drop-unavailable fallback must be absent"},
		{counter == 1, "existing natsclient ERROR counter must equal one"},
	}
	for _, check := range checks {
		if err := requireSlowConsumer(result, check.condition, "%s", check.message); err != nil {
			return err
		}
	}
	return requireSlowConsumer(result, result.AssertionsRun+1 == slowConsumerExpectedAssertions,
		"assertions run after final check must equal %d", slowConsumerExpectedAssertions)
}

func requireSlowConsumer(result *Result, condition bool, format string, args ...any) error {
	result.AssertionsRun++
	if !condition {
		return fmt.Errorf(format, args...)
	}
	return nil
}
