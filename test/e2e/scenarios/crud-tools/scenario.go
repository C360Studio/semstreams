// Package crudtools provides an end-to-end test scenario that exercises
// the ADR-029 Pattern-B CRUD tools through a running semstreams instance.
//
// The scenario is deliberately narrow: dispatch a user message →
// general-role agent gets scoped to rule-CRUD tools → mock LLM scripts a
// create_rule call → verify the rule landed in the semstreams_config KV
// bucket with the expected content. Covers the end-to-end path for
// rule tools; the other three Pattern-B families (flow, persona,
// flow_template) share the same registration + dispatch plumbing so a
// single-family scenario proves the whole pattern.
//
// When a regression breaks the CRUD path, this scenario fails at the
// stage closest to the break (tool dispatch → executor → manager → KV)
// so first-time debuggers see which layer dropped the ball.
package crudtools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// Config holds configuration for the crud-tools scenario.
type Config struct {
	NATSURL         string
	MetricsURL      string
	CompleteTimeout time.Duration

	// ExpectedRuleID must match the id the mock LLM's scripted
	// create_rule call emits. Kept as config so the assertion stays
	// aligned with the mock wiring in
	// test/e2e/mock/cmd/main.go applyCRUDToolsPreset.
	ExpectedRuleID string

	// RulesBucket is where rule.ConfigManager persists rule definitions.
	// Matches the bucket constant inside kv_config_integration.go
	// InitializeKVStore.
	RulesBucket string
}

// DefaultConfig returns defaults for the mock-LLM path. Ports are in
// the 6xxxx range so this scenario can run alongside agentic (3xxxx)
// and deep-research (5xxxx) without collision.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:         "nats://localhost:64222",
		MetricsURL:      "http://localhost:65190",
		CompleteTimeout: 15 * time.Second,
		ExpectedRuleID:  "e2e-crud-rule",
		RulesBucket:     "semstreams_config",
	}
}

// Scenario validates the Pattern-B CRUD path end to end.
type Scenario struct {
	name        string
	description string

	config *Config
	obs    *client.ObservabilityClient

	nats    *client.NATSValidationClient
	metrics *client.MetricsClient
}

// NewScenario constructs a crud-tools scenario.
func NewScenario(obs *client.ObservabilityClient, config *Config) *Scenario {
	if config == nil {
		config = DefaultConfig()
	}
	return &Scenario{
		name:        "crud-tools",
		description: "Verifies Pattern-B CRUD tools round-trip: dispatch → agent → create_rule tool → KV (ADR-029)",
		config:      config,
		obs:         obs,
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *Scenario) Description() string { return s.description }

// Setup creates NATS + metrics clients.
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
		{"inject-user-message", s.injectUserMessage},
		{"wait-for-tool-execution", s.waitForToolExecution},
		{"verify-rule-in-kv", s.verifyRuleInKV},
		{"validate-rule-content", s.validateRuleContent},
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

// verifyComponents surfaces a missing agentic-tools or rule-processor
// distinctly — both are required for the CRUD path to wire up, and a
// missing component is the #1 cause of a silent failure.
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

// injectUserMessage publishes a user.message the mock LLM routes to a
// create_rule tool call. Phrase "author a test rule" matches the Marker
// configured in test/e2e/mock/cmd/main.go applyCRUDToolsPreset.
func (s *Scenario) injectUserMessage(ctx context.Context, result *scenarios.Result) error {
	msg := agentic.UserMessage{
		MessageID:   fmt.Sprintf("e2e-crud-%d", time.Now().UnixNano()),
		ChannelType: "cli",
		ChannelID:   "e2e-test",
		UserID:      "e2e-test-user",
		Content:     "Please author a test rule that verifies the CRUD path end-to-end.",
		Timestamp:   time.Now(),
	}
	envelope := message.NewBaseMessage(msg.Schema(), &msg, "e2e-crud-tools")
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal user message: %w", err)
	}
	subject := fmt.Sprintf("user.message.cli.%s", msg.MessageID)
	if err := s.nats.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}
	result.Details["message_id"] = msg.MessageID
	return nil
}

// waitForToolExecution polls tool-execution metrics until a tool has
// run. If a tool never executes, the root cause is either: mock LLM
// didn't return a tool call, the call was filtered out by allowed_tools,
// or the tool-execute stream routing dropped the message.
func (s *Scenario) waitForToolExecution(ctx context.Context, result *scenarios.Result) error {
	deadline := time.Now().Add(s.config.CompleteTimeout)
	var executions float64

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		exec, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_tools_executions_total")
		if err != nil {
			continue
		}
		executions = exec
		if int(executions) >= 1 {
			result.Metrics["tool_executions"] = executions
			return nil
		}
	}
	result.Metrics["tool_executions"] = executions
	return fmt.Errorf("no tool executions recorded within %v — create_rule tool was never invoked (check agentic-tools executions_total metric + tool.execute stream routing)",
		s.config.CompleteTimeout)
}

// verifyRuleInKV reads the rules KV bucket for the ID the mock
// scripted. Missing key here means the create_rule tool ran but the
// RuleManager didn't persist — either rule.ConfigManager wasn't
// wired (nil RuleManager path in RegisterAll) or the Manager hit a
// KV error.
func (s *Scenario) verifyRuleInKV(ctx context.Context, result *scenarios.Result) error {
	exists, err := s.nats.BucketExists(ctx, s.config.RulesBucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", s.config.RulesBucket, err)
	}
	if !exists {
		return fmt.Errorf("bucket %s does not exist — RuleManager never initialised its KV store", s.config.RulesBucket)
	}

	// Rule keys are namespaced as "rules.<id>" by the ConfigManager.
	key := fmt.Sprintf("rules.%s", s.config.ExpectedRuleID)
	deadline := time.Now().Add(s.config.CompleteTimeout)
	for time.Now().Before(deadline) {
		value, err := s.nats.GetKV(ctx, s.config.RulesBucket, key)
		if err == nil && len(value) > 0 {
			result.Metrics["rule_size_bytes"] = len(value)
			result.Details["rule_key"] = key
			result.Details["rule_value"] = string(value)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("rule key %s never appeared in %s bucket within %v — create_rule tool executed but the write to KV didn't land",
		key, s.config.RulesBucket, s.config.CompleteTimeout)
}

// validateRuleContent unmarshals the stored rule and asserts the mock's
// scripted payload round-tripped. Catches regressions where the
// executor's JSON round-trip drops fields (e.g. via a wrong unmarshal
// target type).
func (s *Scenario) validateRuleContent(ctx context.Context, result *scenarios.Result) error {
	key := fmt.Sprintf("rules.%s", s.config.ExpectedRuleID)
	data, err := s.nats.GetKV(ctx, s.config.RulesBucket, key)
	if err != nil {
		return fmt.Errorf("re-read rule for validation: %w", err)
	}
	var def rule.Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return fmt.Errorf("stored rule is not a valid Definition JSON: %w (raw=%q)", err, string(data))
	}
	if def.ID != s.config.ExpectedRuleID {
		return fmt.Errorf("rule ID mismatch: stored %q, expected %q", def.ID, s.config.ExpectedRuleID)
	}
	if def.Type != "expression" {
		return fmt.Errorf("rule Type mismatch: stored %q, expected %q", def.Type, "expression")
	}
	if !def.Enabled {
		return fmt.Errorf("rule Enabled mismatch: stored false, expected true")
	}
	result.Details["rule_round_trip"] = map[string]any{
		"id":      def.ID,
		"type":    def.Type,
		"name":    def.Name,
		"enabled": def.Enabled,
	}
	return nil
}
