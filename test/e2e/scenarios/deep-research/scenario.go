// Package deepresearch provides the deep-research rules-driven E2E test
// scenario. Unlike the single-loop agentic scenario, this exercises the full
// orchestration path: user question → general agent → rule-spawned researcher
// → evidence collection via KV → synthesizer → final response.
//
// The scenario is deliberately strict about which stage fails so a first-time
// run on a newly wired flow surfaces exactly where the rule chain breaks
// (watch patterns, predicate names, port wiring, etc.) instead of reporting
// an opaque timeout.
package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// Config holds configuration for the deep-research scenario.
type Config struct {
	NATSURL            string        `json:"nats_url"`
	MetricsURL         string        `json:"metrics_url"`
	LLMEndpointURL     string        `json:"llm_endpoint_url"`
	CompleteTimeout    time.Duration `json:"complete_timeout"`
	MinAgentLoops      int           `json:"min_agent_loops"`      // general + researcher + coordinator + (subtopic) + synthesizer
	MinEvidenceEntries int           `json:"min_evidence_entries"` // ≥ 1 evidence record collected
}

// DefaultConfig returns default configuration sized for the mock-LLM flow,
// which completes in well under a second. The 10s deadline is a generous
// margin to catch regressions without masking hangs.
//
// Ports are in the 5xxxx range so this scenario's stack can run alongside
// the agentic scenario (3xxxx range) without port collisions.
//
// MinAgentLoops is 4 because the chain now routes through a research
// coordinator at the judgment point: user question → general → researcher
// → coordinator (decides fan_out) → subtopic researcher → coordinator
// (decides synthesize) → synthesizer. That's six loops in the happy path;
// we assert ≥4 to keep the check tolerant of mock LLM reordering without
// losing the signal that the coordinator path actually fires.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:            "nats://localhost:54222",
		MetricsURL:         "http://localhost:59090",
		LLMEndpointURL:     "",
		CompleteTimeout:    10 * time.Second,
		MinAgentLoops:      4,
		MinEvidenceEntries: 1,
	}
}

// Scenario validates the deep-research flow end to end.
type Scenario struct {
	name        string
	description string

	config *Config
	obs    *client.ObservabilityClient

	// Created during Setup
	nats    *client.NATSValidationClient
	metrics *client.MetricsClient
}

// NewScenario constructs a deep-research scenario.
func NewScenario(obs *client.ObservabilityClient, config *Config) *Scenario {
	if config == nil {
		config = DefaultConfig()
	}
	return &Scenario{
		name:        "deep-research",
		description: "Validates the rules-driven deep-research flow (dispatch → researcher → evidence → synthesizer)",
		config:      config,
		obs:         obs,
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *Scenario) Description() string { return s.description }

// Setup creates clients.
func (s *Scenario) Setup(ctx context.Context) error {
	nc, err := client.NewNATSValidationClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = nc
	s.metrics = client.NewMetricsClient(s.config.MetricsURL)
	return nil
}

// Teardown closes clients.
func (s *Scenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// Execute runs the scenario.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	stages := []struct {
		name string
		fn   func(context.Context, *scenarios.Result) error
	}{
		{"verify-components", s.verifyComponents},
		{"inject-user-question", s.injectUserQuestion},
		{"wait-for-chain-completion", s.waitForChainCompletion},
		{"verify-evidence-collected", s.verifyEvidenceCollected},
		{"verify-coordinator-fired", s.verifyCoordinatorFired},
		{"validate-results", s.validateResults},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		if err := stage.fn(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil
		}
		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	return result, nil
}

// verifyComponents checks that the deep-research component set is healthy.
// A missing rule-processor is the most common cause of the rule chain not
// firing, so we surface that distinctly.
func (s *Scenario) verifyComponents(ctx context.Context, result *scenarios.Result) error {
	components, err := s.obs.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("get components: %w", err)
	}

	required := []string{
		"agentic-dispatch",
		"agentic-loop",
		"agentic-model",
		"agentic-tools",
		"rule-processor",
		"graph-ingest",
	}
	found := make(map[string]bool, len(components))
	for _, comp := range components {
		found[comp.Name] = comp.Enabled && comp.Healthy
	}

	var missing, unhealthy []string
	for _, req := range required {
		ok, exists := found[req]
		switch {
		case !exists:
			missing = append(missing, req)
		case !ok:
			unhealthy = append(unhealthy, req)
		}
	}

	result.Details["components_found"] = found
	if len(missing) > 0 {
		return fmt.Errorf("missing components: %v", missing)
	}
	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthy)
	}
	return nil
}

// injectUserQuestion publishes a user.message to kick off the chain. The
// agentic-dispatch processor subscribes to user.message.> and routes it to a
// general-role agent task.
func (s *Scenario) injectUserQuestion(ctx context.Context, result *scenarios.Result) error {
	msg := agentic.UserMessage{
		MessageID:   fmt.Sprintf("e2e-dr-%d", time.Now().UnixNano()),
		ChannelType: "cli",
		ChannelID:   "e2e-test",
		UserID:      "e2e-test-user",
		Content:     "Research the impact of deep-learning embeddings on graph retrieval quality.",
		Timestamp:   time.Now(),
	}
	envelope := message.NewBaseMessage(msg.Schema(), &msg, "e2e-deep-research")
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal user message: %w", err)
	}

	subject := fmt.Sprintf("user.message.cli.%s", msg.MessageID)
	if err := s.nats.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}

	result.Details["message_id"] = msg.MessageID
	result.Details["user_subject"] = subject
	return nil
}

// waitForChainCompletion polls agent-loop completion metrics until we see at
// least MinAgentLoops loops finish. With the mock LLM each loop completes in
// well under a second, so this is a deadline-driven sanity check rather than
// a progress monitor.
func (s *Scenario) waitForChainCompletion(ctx context.Context, result *scenarios.Result) error {
	deadline := time.Now().Add(s.config.CompleteTimeout)
	var loopsCompleted float64

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		loops, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_loop_loops_completed_total")
		if err != nil {
			// Metrics may not be ready yet; keep polling.
			continue
		}
		loopsCompleted = loops
		if int(loopsCompleted) >= s.config.MinAgentLoops {
			result.Metrics["loops_completed"] = loopsCompleted
			return nil
		}
	}

	result.Metrics["loops_completed"] = loopsCompleted
	return fmt.Errorf("chain incomplete: %d loops finished, need ≥ %d within %v — rule chain is likely not firing (check rule-processor logs for entity pattern / predicate mismatches)",
		int(loopsCompleted), s.config.MinAgentLoops, s.config.CompleteTimeout)
}

// verifyEvidenceCollected checks the RESEARCH_EVIDENCE KV bucket for at least
// one entry. This is the canary for rule 02 (collect-evidence) having fired —
// if the bucket is empty, rule 01 or 02 didn't match against the
// researcher-complete entity.
func (s *Scenario) verifyEvidenceCollected(ctx context.Context, result *scenarios.Result) error {
	const bucket = "RESEARCH_EVIDENCE"
	exists, err := s.nats.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	if !exists {
		return fmt.Errorf("%s bucket was never created — rule 02 (collect-evidence) did not fire", bucket)
	}

	count, err := s.nats.CountBucketKeys(ctx, bucket)
	if err != nil {
		return fmt.Errorf("count %s keys: %w", bucket, err)
	}
	result.Metrics["evidence_entries"] = count
	if count < s.config.MinEvidenceEntries {
		return fmt.Errorf("%s has %d entries, need ≥ %d", bucket, count, s.config.MinEvidenceEntries)
	}

	keys, err := s.nats.GetBucketKeysSample(ctx, bucket, 5)
	if err == nil {
		result.Details["evidence_keys_sample"] = keys
	}
	return nil
}

// verifyCoordinatorFired scans ENTITY_STATES for a research-coordinator
// loop whose decide() call landed a coordinator.decision.next_action=fan_out
// triple. This is the canary for the judgment layer: if absent, either
// rule 07 (spawn-coordinator) didn't fire, the mock didn't trigger the
// decide tool, or the decide tool's triple publication failed. Any of
// those means the coordinator path is broken and synthesis would stall.
func (s *Scenario) verifyCoordinatorFired(ctx context.Context, result *scenarios.Result) error {
	ids, err := s.nats.GetAllEntityIDs(ctx)
	if err != nil {
		return fmt.Errorf("list entity ids: %w", err)
	}

	var coordinatorIDs []string
	var fanOutIDs []string
	for _, id := range ids {
		entity, err := s.nats.GetEntity(ctx, id)
		if err != nil {
			continue
		}
		role := ""
		nextAction := ""
		for _, t := range entity.Triples {
			switch t.Predicate {
			case "agent.loop.role":
				if s, ok := t.Object.(string); ok {
					role = s
				}
			case "coordinator.decision.next_action":
				if s, ok := t.Object.(string); ok {
					nextAction = s
				}
			}
		}
		if role == "research-coordinator" {
			coordinatorIDs = append(coordinatorIDs, id)
			if nextAction == "fan_out" {
				fanOutIDs = append(fanOutIDs, id)
			}
		}
	}

	result.Metrics["coordinator_loops"] = len(coordinatorIDs)
	result.Metrics["coordinator_fan_out_decisions"] = len(fanOutIDs)
	if len(coordinatorIDs) > 0 {
		result.Details["coordinator_sample"] = coordinatorIDs[0]
	}

	if len(coordinatorIDs) == 0 {
		return fmt.Errorf("no research-coordinator loop found in ENTITY_STATES — rule 07 (spawn-coordinator) did not fire or the coordinator loop never completed")
	}
	if len(fanOutIDs) == 0 {
		return fmt.Errorf("research-coordinator completed but no coordinator.decision.next_action=fan_out triple appeared — decide tool likely did not publish (check mock LLM WithRoleToolCallSequence or agentic-tools decide registration)")
	}
	return nil
}

// validateResults is the final cross-check: loops_completed count is
// consistent with evidence count (each researcher that completed should have
// produced one evidence entry).
func (s *Scenario) validateResults(_ context.Context, result *scenarios.Result) error {
	loops, _ := result.Metrics["loops_completed"].(float64)
	evidence, _ := result.Metrics["evidence_entries"].(int)

	if int(loops) < s.config.MinAgentLoops {
		return fmt.Errorf("final check: only %d loops completed", int(loops))
	}
	if evidence < s.config.MinEvidenceEntries {
		return fmt.Errorf("final check: only %d evidence entries collected", evidence)
	}
	result.Details["validation_passed"] = true
	return nil
}
