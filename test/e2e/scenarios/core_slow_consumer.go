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
	EvidenceRunID    string `json:"evidence_run_id,omitempty"`
	EvidenceMemberID string `json:"evidence_member_id,omitempty"`
	AppContainer     string
	MetricsURL       string
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

var slowConsumerCheckIDs = []string{
	"slow-consumer.record-count", "slow-consumer.level", "slow-consumer.message",
	"slow-consumer.component", "slow-consumer.error", "slow-consumer.subject",
	"slow-consumer.queue", "slow-consumer.dropped", "slow-consumer.drop-available-absent",
	"slow-consumer.error-counter", "slow-consumer.assertion-count",
}

// CheckRequirements declares every separately evaluated fixture condition.
func (*SlowConsumerAttributionScenario) CheckRequirements() []CheckRequirement {
	checks := make([]CheckRequirement, 0, len(slowConsumerCheckIDs))
	for _, id := range slowConsumerCheckIDs {
		checks = append(checks, CheckRequirement{ID: id, Required: true})
	}
	return checks
}

// Setup requires no host-side mutation; compose owns the disposable stack.
func (*SlowConsumerAttributionScenario) Setup(context.Context) error { return nil }

// Teardown requires no scenario mutation; the task owns bounded compose teardown.
func (*SlowConsumerAttributionScenario) Teardown(context.Context) error { return nil }

// Execute observes configured JSON stdout and the existing counter.
func (s *SlowConsumerAttributionScenario) Execute(parent context.Context) (*Result, error) {
	start := time.Now()
	result := &Result{ScenarioName: s.Name(), StartTime: start}
	if s.config.EvidenceRunID != "" || s.config.EvidenceMemberID != "" {
		if err := result.DeclareChecks(s.config.EvidenceRunID, s.config.EvidenceMemberID, s.CheckRequirements()); err != nil {
			return result, err
		}
	}
	defer func() {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(start)
	}()

	ctx, cancel := context.WithTimeout(parent, slowConsumerObservationTimeout)
	defer cancel()
	records, counter, err := s.waitForObservation(ctx)
	if err != nil {
		result.Error = err.Error()
		if result.RunID != "" {
			_ = result.RecordCheck(CheckObservation{ID: slowConsumerCheckIDs[0], RunID: result.RunID,
				MemberID: result.MemberID, Status: "failed", Reason: err.Error()})
		}
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
	if err := requireSlowConsumer(result, slowConsumerCheckIDs[0], len(records) == 1,
		"1", fmt.Sprint(len(records)),
		"matching NATS error records=%d, want 1", len(records)); err != nil {
		return err
	}
	record := records[0]
	_, droppedAvailablePresent := record["dropped_available"]
	checks := []struct {
		id        string
		condition bool
		expected  string
		actual    string
		message   string
	}{
		{slowConsumerCheckIDs[1], record["level"] == "ERROR", "ERROR", fmt.Sprint(record["level"]), "level must be ERROR"},
		{slowConsumerCheckIDs[2], record["msg"] == "NATS error", "NATS error", fmt.Sprint(record["msg"]), "message must be NATS error"},
		{slowConsumerCheckIDs[3], record["component"] == "natsclient", "natsclient", fmt.Sprint(record["component"]), "component must be natsclient"},
		{slowConsumerCheckIDs[4], record["error"] == "nats: slow consumer, messages dropped", "nats: slow consumer, messages dropped", fmt.Sprint(record["error"]), "error must preserve ErrSlowConsumer"},
		{slowConsumerCheckIDs[5], record["subject"] == e2eslowconsumer.Subject, e2eslowconsumer.Subject, fmt.Sprint(record["subject"]), "subject must identify the fixture"},
		{slowConsumerCheckIDs[6], record["queue"] == e2eslowconsumer.Queue, e2eslowconsumer.Queue, fmt.Sprint(record["queue"]), "queue must identify the fixture"},
		{slowConsumerCheckIDs[7], record["dropped"] == float64(e2eslowconsumer.ExpectedDropped), fmt.Sprint(e2eslowconsumer.ExpectedDropped), fmt.Sprint(record["dropped"]), "dropped must equal exact fixture overflow"},
		{slowConsumerCheckIDs[8], !droppedAvailablePresent, "absent",
			fmt.Sprintf("present=%t,value=%v", droppedAvailablePresent, record["dropped_available"]),
			"drop-unavailable fallback must be absent"},
		{slowConsumerCheckIDs[9], counter == 1, "1", fmt.Sprint(counter), "existing natsclient ERROR counter must equal one"},
	}
	for _, check := range checks {
		if err := requireSlowConsumer(result, check.id, check.condition, check.expected, check.actual, "%s", check.message); err != nil {
			return err
		}
	}
	return requireSlowConsumer(result, slowConsumerCheckIDs[10], result.AssertionsRun+1 == slowConsumerExpectedAssertions,
		fmt.Sprint(slowConsumerExpectedAssertions), fmt.Sprint(result.AssertionsRun+1),
		"assertions run after final check must equal %d", slowConsumerExpectedAssertions)
}

func requireSlowConsumer(result *Result, id string, condition bool, expected, actual, format string, args ...any) error {
	result.AssertionsRun++
	var checkErr error
	if !condition {
		checkErr = fmt.Errorf(format, args...)
	}
	if result.RunID != "" {
		observation := CheckObservation{ID: id, RunID: result.RunID, MemberID: result.MemberID, Status: "passed",
			Evidence: map[string]string{"expected": expected, "actual": actual}}
		if checkErr != nil {
			observation.Status, observation.Reason = "failed", checkErr.Error()
		}
		if err := result.RecordCheck(observation); err != nil {
			return err
		}
	}
	return checkErr
}
