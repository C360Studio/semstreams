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
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// PersonaMarker is the unique substring planted in the e2e persona
// override this scenario seeds. test/e2e/mock/cmd/main.go uses the
// same constant as its tool-call marker, so the create_rule call only
// fires when the persona content reaches the LLM via the ADR-029
// step-3b assembler wiring. Exported so the mock preset and the
// scenario stay aligned on a single string.
const PersonaMarker = "E2E-CRUD-PERSONA-MARKER-v1"

// personaOverrideID is the ID of the DefaultFragments entry this
// scenario overrides. role-general is the role the agentic-loop runs
// under by default, so Upsert on this ID guarantees the override
// reaches the request's system message for this task's role.
const personaOverrideID = "role-general"

// personasBucket is the PERSONAS KV bucket name — matches the constant
// inside the persona.Manager. Duplicated here so the scenario can
// seed directly via NATSValidationClient without importing the
// Manager (which opens its own KV handle and would race with the
// running semstreams process).
const personasBucket = "PERSONAS"

// Config holds configuration for the crud-tools scenario.
type Config struct {
	NATSURL         string
	MetricsURL      string
	BaseURL         string // HTTP base URL for service-manager endpoints (e.g. /components/types)
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
		BaseURL:         "http://localhost:65080",
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

	// baselineActiveRules captures semstreams_rule_active_rules at the end of
	// verifyComponents so verify-hotreload-pickup can assert the gauge
	// increased by exactly one after the mock's create_rule call landed. A
	// value of -1 means the metrics endpoint was unreachable and the
	// hot-reload stage should be skipped rather than failed.
	baselineActiveRules float64
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
		{"seed-persona-override", s.seedPersonaOverride},
		{"inject-user-message", s.injectUserMessage},
		{"wait-for-tool-execution", s.waitForToolExecution},
		{"verify-rule-in-kv", s.verifyRuleInKV},
		{"validate-rule-content", s.validateRuleContent},
		{"verify-hotreload-pickup", s.verifyHotreloadPickup},
		{"verify-list-components", s.verifyListComponents},
		{"verify-monitor-flow", s.verifyMonitorFlow},
		{"cleanup-persona-override", s.cleanupPersonaOverride},
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

	// Capture baseline active-rules gauge so verify-hotreload-pickup can
	// assert the rule written by the mock's create_rule call incremented it.
	// Failure to reach the metrics endpoint is non-fatal here — the
	// hot-reload stage will skip gracefully when baselineActiveRules == -1.
	baseline, err := s.metrics.GetMetricValue(ctx, "semstreams_rule_active_rules")
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("hot-reload baseline: could not scrape semstreams_rule_active_rules (%v); verify-hotreload-pickup will be skipped", err))
		s.baselineActiveRules = -1
	} else {
		s.baselineActiveRules = baseline
		result.Details["baseline_active_rules"] = baseline
	}
	return nil
}

// seedPersonaOverride plants a persona record in the PERSONAS KV
// bucket that overrides the role-general DefaultFragment. The
// override's content contains PersonaMarker — the mock's tool-call
// matcher looks for that exact substring. If the ADR-029 step-3b
// wiring works (component loads PERSONAS at Start, UpsertAlls into
// the registry, Assemble prepends the override content as a system
// message), the marker reaches the LLM, the mock fires create_rule,
// and the scenario passes. If the wiring regresses, the persona
// never reaches the LLM, the marker misses, and the scenario fails
// loud at wait-for-tool-execution.
//
// The agentic-loop component only loads the PERSONAS bucket once at
// Start(). This stage therefore assumes semstreams is already
// running and reads from a bucket it opened during startup — the
// effect of writing here depends on whether the Component treats
// PERSONAS as live-refreshed or start-snapshot. Today it's
// start-snapshot, so for this scenario to work the seeding MUST
// happen before the first user message triggers a loop. It does
// (seed-persona runs before inject-user-message).
func (s *Scenario) seedPersonaOverride(ctx context.Context, result *scenarios.Result) error {
	p := &persona.Persona{
		ID:       personaOverrideID,
		Category: int(prompt.CategoryRole),
		Priority: 0,
		Content: fmt.Sprintf(
			"You are the general-purpose e2e test agent. %s Focus on authoring a test rule when asked.",
			PersonaMarker,
		),
		Roles:       []string{"general"},
		Description: "e2e fixture — replaces role-general with a marker-bearing body the mock can match on.",
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal persona fixture: %w", err)
	}
	if err := s.nats.PutKV(ctx, personasBucket, personaOverrideID, data); err != nil {
		return fmt.Errorf("seed persona %s into %s: %w", personaOverrideID, personasBucket, err)
	}
	result.Details["persona_override_id"] = personaOverrideID
	result.Details["persona_marker"] = PersonaMarker
	return nil
}

// verifyHotreloadPickup polls the Prometheus metrics endpoint until the
// semstreams_rule_active_rules gauge increments by at least one above the
// baseline captured in verifyComponents. This confirms the running
// rule-processor picked up the rule written by the mock's create_rule tool
// call without a restart — proving the hot-reload / debounce path works.
//
// The rule-processor's debounce window is 250ms; after the apply step the
// metric update is synchronous. Two seconds of polling window is generous
// for CI variance.
//
// If the metrics endpoint was unreachable at baseline time
// (s.baselineActiveRules == -1) the stage is skipped with a Warning entry
// rather than failed, keeping the scenario resilient on minimal flows
// without a metrics service.
func (s *Scenario) verifyHotreloadPickup(ctx context.Context, result *scenarios.Result) error {
	if s.baselineActiveRules < 0 {
		result.Warnings = append(result.Warnings,
			"verify-hotreload-pickup: metrics unreachable at baseline; stage skipped")
		return nil
	}

	stageStart := time.Now()
	expected := s.baselineActiveRules + 1
	deadline := time.Now().Add(2 * time.Second)
	var lastSeen float64

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		v, err := s.metrics.GetMetricValue(ctx, "semstreams_rule_active_rules")
		if err != nil {
			// Metric may briefly disappear between scrapes; keep polling.
			continue
		}
		lastSeen = v
		if v >= expected {
			result.Metrics["hotreload_pickup_latency_ms"] = time.Since(stageStart).Milliseconds()
			result.Details["active_rules_after_reload"] = v
			return nil
		}
	}

	return fmt.Errorf(
		"hot-reload pickup: semstreams_rule_active_rules did not reach %.0f within 2s "+
			"(baseline=%.0f, last observed=%.0f) — "+
			"rule-processor debounce is 250ms; check that the rule-processor is watching semstreams_config KV "+
			"and that the metric name semstreams_rule_active_rules matches processor/rule/metrics.go",
		expected, s.baselineActiveRules, lastSeen,
	)
}

// verifyListComponents exercises the list_components feature by calling the
// service-manager's GET /components/types endpoint, which shares
// BuildComponentTypeCatalog with the list_components agent tool. This proves
// the component factory registry is populated and the catalog builder works
// end to end.
//
// Approach rationale: the list_components tool is not in agentic-tools'
// allowed_tools list for this flow (only rule-CRUD tools are scoped). Adding
// it would require both a flow-config change and scripting a second mock tool
// call after the existing create_rule sequence. The HTTP endpoint shares the
// same builder (service/component_manager_http.go:BuildComponentTypeCatalog)
// and is a simpler, equally valid proof. The tool's dispatch path is covered
// by unit tests in processor/agentic-tools.
//
// Assertions: at least 3 of the well-known framework factory names registered
// in this flow are present. Count is not asserted — the catalog grows over time.
func (s *Scenario) verifyListComponents(ctx context.Context, result *scenarios.Result) error {
	url := s.config.BaseURL + "/components/types"
	httpClient := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build /components/types request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /components/types: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/components/types returned HTTP %d — service-manager may not be running or the endpoint is not registered", resp.StatusCode)
	}

	var catalog []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return fmt.Errorf("decode /components/types response: %w", err)
	}

	ids := make(map[string]bool, len(catalog))
	for _, entry := range catalog {
		if id, _ := entry["id"].(string); id != "" {
			ids[id] = true
		}
	}

	// Well-known factory names wired by this flow. These are framework-stable
	// names that won't churn — avoid asserting count since the catalog grows.
	wellKnown := []string{
		"agentic-loop",
		"agentic-dispatch",
		"agentic-model",
		"agentic-tools",
		"rule-processor",
	}
	var found []string
	for _, name := range wellKnown {
		if ids[name] {
			found = append(found, name)
		}
	}
	if len(found) < 3 {
		return fmt.Errorf(
			"list_components catalog missing expected factory types: "+
				"found %v of well-known set %v (total catalog entries: %d)",
			found, wellKnown, len(catalog),
		)
	}
	result.Details["component_catalog_found"] = found
	result.Details["component_catalog_total"] = len(catalog)
	return nil
}

// verifyMonitorFlow validates the data path that the monitor_flow tool reads:
// the AGENT_LOOPS KV bucket must contain at least one COMPLETE_* entry after
// the mock's create_rule loop terminated, and that entry must carry a valid
// outcome field and non-negative token counts.
//
// Approach rationale: monitor_flow is not in agentic-tools' allowed_tools list
// for this flow. Scripting it via the mock would require both a flow-config
// change (adding monitor_flow to allowed_tools) and a second mock tool-call
// sequence after create_rule. Instead, we assert directly on the AGENT_LOOPS
// KV data the tool reads — this proves the same guarantees:
//   - total_loops >= 1 (bucket has COMPLETE_* keys)
//   - by_outcome populated (outcome field present in the record)
//   - total_tokens_in >= 0 (tokens_in non-negative)
func (s *Scenario) verifyMonitorFlow(ctx context.Context, result *scenarios.Result) error {
	const loopsBucket = "AGENT_LOOPS"

	keys, err := s.nats.GetBucketKeysSample(ctx, loopsBucket, 100)
	if err != nil {
		return fmt.Errorf("list %s keys: %w", loopsBucket, err)
	}

	var completeKeys []string
	for _, k := range keys {
		if strings.HasPrefix(k, "COMPLETE_") {
			completeKeys = append(completeKeys, k)
		}
	}
	if len(completeKeys) == 0 {
		return fmt.Errorf(
			"monitor_flow data path: no COMPLETE_* keys in %s after loop termination "+
				"(total_loops=0) — agentic-loop may not have written a completion record",
			loopsBucket,
		)
	}

	// Read the first completion record and assert the fields monitor_flow
	// aggregates are present and sane.
	data, err := s.nats.GetKV(ctx, loopsBucket, completeKeys[0])
	if err != nil {
		return fmt.Errorf("read completion record %s: %w", completeKeys[0], err)
	}

	// Minimal struct matching eventDiscriminator + token fields from
	// FlowMonitorExecutor. No need to import agentic package types here.
	var entry struct {
		Outcome  string `json:"outcome"`
		TokensIn int    `json:"tokens_in"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return fmt.Errorf("unmarshal completion record %s: %w", completeKeys[0], err)
	}
	if entry.Outcome == "" {
		return fmt.Errorf(
			"monitor_flow data path: completion record %s missing outcome field "+
				"(by_outcome would be empty)",
			completeKeys[0],
		)
	}
	if entry.TokensIn < 0 {
		return fmt.Errorf(
			"monitor_flow data path: total_tokens_in < 0 (%d) in record %s",
			entry.TokensIn, completeKeys[0],
		)
	}

	result.Details["monitor_flow_total_loops"] = len(completeKeys)
	result.Details["monitor_flow_first_outcome"] = entry.Outcome
	result.Details["monitor_flow_tokens_in"] = entry.TokensIn
	return nil
}

// cleanupPersonaOverride removes the seeded persona so a second run
// of the scenario against the same NATS deployment starts from a
// clean PERSONAS bucket. Skipping cleanup on an assertion failure
// would leave the seed in place — acceptable since the bucket is
// wiped between e2e runs by the docker-compose teardown, but we try
// to be tidy when we can.
func (s *Scenario) cleanupPersonaOverride(ctx context.Context, _ *scenarios.Result) error {
	if err := s.nats.DeleteKV(ctx, personasBucket, personaOverrideID); err != nil {
		// Not fatal — the scenario's primary assertions already ran.
		return fmt.Errorf("delete seeded persona %s: %w", personaOverrideID, err)
	}
	return nil
}

// injectUserMessage publishes a user.message the mock LLM routes to a
// create_rule tool call. The mock's marker is the PersonaMarker
// constant — not a user-message substring — so the user content only
// needs to be plausible enough to justify a create_rule call
// semantically. The actual triggering happens via the persona
// override seeded in seed-persona-override.
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
