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
	"os/exec"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/processor/rule/expression"
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

	// AppContainer is the docker container name for the running
	// semstreams binary; verifyRegisteredTools greps its startup logs
	// to confirm ADR-026 M2 tool registrations fired. Matches
	// docker/compose/crud-tools.yml.
	AppContainer string
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
		AppContainer:    "semstreams-crud-tools-app",
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
		{"verify-registered-tools", s.verifyRegisteredTools},
		{"seed-persona-override", s.seedPersonaOverride},
		{"inject-user-message", s.injectUserMessage},
		{"wait-for-tool-execution", s.waitForToolExecution},
		{"verify-rule-in-kv", s.verifyRuleInKV},
		{"validate-rule-content", s.validateRuleContent},
		{"verify-hotreload-pickup", s.verifyHotreloadPickup},
		{"verify-fire-every-n-events", s.verifyFireEveryNEvents},
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

// verifyRegisteredTools confirms the ADR-026 M2 tools (list_components,
// monitor_flow) actually registered into the global tool registry at
// binary startup. This catches wiring regressions that unit tests
// can't see — e.g. cmd/semstreams/main.go dropping
// ComponentRegistry from ToolDependencies, or executors.RegisterAll
// dropping the registerComponentCatalog call. Each register fn emits
// a distinct slog.Info "Registered <tool> tool (global)" line on
// success and a slog.Warn "<tool> disabled: ..." on nil-skip; we grep
// container logs for the success lines and fail on absence.
//
// This is a lightweight substitute for a full tool-invocation e2e.
// Exercising the tools through the LLM → dispatcher → executor chain
// would require adding list_components/monitor_flow to the flow's
// allowed_tools and scripting multi-tool mock sequences, which dilutes
// what the crud-tools scenario exists to test. A proper invocation
// test belongs in a coordinator-flavored scenario (follow-up).
func (s *Scenario) verifyRegisteredTools(ctx context.Context, result *scenarios.Result) error {
	required := map[string]string{
		"list_components": "Registered list_components tool (global)",
		"monitor_flow":    "Registered monitor_flow tool (global)",
	}

	cmd := exec.CommandContext(ctx, "docker", "logs", s.config.AppContainer)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker logs %s: %w", s.config.AppContainer, err)
	}
	logs := string(out)

	missing := []string{}
	for tool, marker := range required {
		if !strings.Contains(logs, marker) {
			missing = append(missing, tool)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"ADR-026 M2 tools not registered at startup: %v — "+
				"check cmd/semstreams/main.go ToolDependencies and "+
				"executors.RegisterAll wiring",
			missing)
	}

	result.Details["registered_m2_tools"] = []string{"list_components", "monitor_flow"}
	return nil
}

// fireEveryNConstants groups magic numbers for the sampling-gate stage so
// they appear in one place and are easy to scan.
const (
	fireEveryNRuleID     = "e2e-fire-every-n-test"
	fireEveryNEntityType = "e2e-fire-probe"
	fireEveryNWindowN    = 3
	fireEveryNProbeCount = 9
	fireEveryNExpected   = fireEveryNProbeCount / fireEveryNWindowN // 3
	fireEveryNKVBucket   = "ENTITY_STATES"
	fireEveryNRuleKey    = "rules." + fireEveryNRuleID
	fireEveryNMetricName = "semstreams_rule_events_published_total"
	fireEveryNMetricET   = "triggered"
	fireEveryNMetricSubj = "events.rule.triggered"
)

// verifyFireEveryNEvents exercises the fire_every_n_events sampling gate
// (PR2 of ADR-027 Phase 1, commit 34bc655) end to end through a live
// rule-processor. The gate was previously covered only by unit and
// integration tests; this stage proves it works in a running system.
//
// Assertion path: metrics (not graph triples). The crud-tools flow config
// disables graph integration (enable_graph_integration: false), so
// add_triple actions do not persist. Instead, the stage measures how many
// times publishRuleEvent fires inside fireRuleActions — that function is
// entered only after the gate passes, making the metric delta a direct,
// honest measurement of the gate's behaviour.
//
// Metric: semstreams_rule_events_published_total{event_type="triggered",
// subject="events.rule.triggered"} — delta of exactly 3 expected after
// 9 matching events with N=3.
func (s *Scenario) verifyFireEveryNEvents(ctx context.Context, result *scenarios.Result) error {
	defer s.cleanupFireEveryNRule()

	if err := s.seedFireEveryNRule(ctx, result); err != nil {
		return err
	}
	if err := s.waitForFireEveryNRuleHotReload(ctx, result); err != nil {
		return err
	}

	baselinePublished := s.captureFireEveryNBaseline(ctx)
	result.Details["fire_every_n_baseline_published"] = baselinePublished

	injectedKeys, err := s.injectProbeEntities(ctx, result)
	if err != nil {
		return err
	}
	defer s.cleanupProbeEntities(injectedKeys)

	return s.assertFireEveryNGate(ctx, result, baselinePublished)
}

// seedFireEveryNRule writes the sampling-gate rule directly to KV,
// exercising the rule-processor's hot-reload watcher.
// No on_enter/while_true actions: the stateful evaluator runs those
// directly and bypasses the gate. An empty action list makes publishRuleEvent
// (inside fireRuleActions, after the gate) the sole observable side effect.
func (s *Scenario) seedFireEveryNRule(ctx context.Context, result *scenarios.Result) error {
	ruleDef := rule.Definition{
		ID:               fireEveryNRuleID,
		Type:             "expression",
		Name:             "Fire Every N Test",
		Description:      "e2e sampling-gate test: fires only on every 3rd matching event",
		Enabled:          true,
		FireEveryNEvents: fireEveryNWindowN,
		Conditions: []expression.ConditionExpression{
			{Field: "entity.type", Operator: "eq", Value: fireEveryNEntityType},
		},
		Logic: "and",
	}
	ruleJSON, err := json.Marshal(ruleDef)
	if err != nil {
		return fmt.Errorf("marshal sampling-gate rule: %w", err)
	}
	if err := s.nats.PutKV(ctx, s.config.RulesBucket, fireEveryNRuleKey, ruleJSON); err != nil {
		return fmt.Errorf("seed sampling-gate rule into %s: %w", s.config.RulesBucket, err)
	}
	result.Details["fire_every_n_rule_id"] = fireEveryNRuleID
	return nil
}

// waitForFireEveryNRuleHotReload polls until the rule-processor's active_rules
// gauge reaches baseline+2 (e2e-crud-rule plus the new gate rule). Falls back
// to a fixed 400ms delay when metrics are unavailable.
func (s *Scenario) waitForFireEveryNRuleHotReload(ctx context.Context, result *scenarios.Result) error {
	if s.baselineActiveRules < 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		return nil
	}

	// e2e-crud-rule (loaded by verify-hotreload-pickup) + e2e-fire-every-n-test
	expected := s.baselineActiveRules + 2
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
			continue
		}
		lastSeen = v
		if v >= expected {
			result.Details["active_rules_after_fire_every_n_rule"] = v
			return nil
		}
	}
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("fire-every-n hot-reload: active_rules did not reach %.0f within 2s (last=%.0f); proceeding",
			expected, lastSeen))
	return nil
}

// captureFireEveryNBaseline reads the current value of the events-published
// metric so the assertion can compute a delta rather than an absolute count.
func (s *Scenario) captureFireEveryNBaseline(ctx context.Context) float64 {
	if s.baselineActiveRules < 0 {
		return 0
	}
	metrics, err := s.metrics.GetMetricByLabels(ctx, fireEveryNMetricName,
		map[string]string{"event_type": fireEveryNMetricET, "subject": fireEveryNMetricSubj})
	if err != nil || len(metrics) == 0 {
		return 0
	}
	return metrics[0].Value
}

// injectProbeEntities writes 9 EntityState records to ENTITY_STATES KV.
// Each entity has a triple with predicate "entity.type" = fireEveryNEntityType
// so the sampling-gate rule's condition matches. Returns the injected keys for
// cleanup.
func (s *Scenario) injectProbeEntities(ctx context.Context, result *scenarios.Result) ([]string, error) {
	keys := make([]string, 0, fireEveryNProbeCount)
	for i := 1; i <= fireEveryNProbeCount; i++ {
		entityID := fmt.Sprintf("org.platform.test.probe.e2e.%03d", i)
		entityState := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "entity.type",
					Object:    fireEveryNEntityType,
					Source:    "e2e-fire-every-n-seed",
					Timestamp: time.Now(),
				},
			},
			MessageType: message.Type{Domain: "e2e", Category: "probe", Version: "v1"},
			Version:     1,
			UpdatedAt:   time.Now(),
		}
		entityJSON, err := json.Marshal(entityState)
		if err != nil {
			return keys, fmt.Errorf("marshal probe entity %d: %w", i, err)
		}
		if err := s.nats.PutKV(ctx, fireEveryNKVBucket, entityID, entityJSON); err != nil {
			return keys, fmt.Errorf("seed probe entity %d (%s): %w", i, entityID, err)
		}
		keys = append(keys, entityID)
	}
	result.Details["fire_every_n_injected_count"] = fireEveryNProbeCount
	return keys, nil
}

// assertFireEveryNGate polls the events-published metric until the delta from
// baseline reaches exactly fireEveryNExpected (3), confirming the gate fired 3
// times for 9 matching events with N=3.
func (s *Scenario) assertFireEveryNGate(ctx context.Context, result *scenarios.Result, baselinePublished float64) error {
	if s.baselineActiveRules < 0 {
		result.Warnings = append(result.Warnings,
			"verify-fire-every-n-events: metrics unreachable; gate assertion skipped")
		return nil
	}

	target := baselinePublished + float64(fireEveryNExpected)
	deadline := time.Now().Add(2 * time.Second)
	var lastSeen float64
	stageStart := time.Now()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		metrics, err := s.metrics.GetMetricByLabels(ctx, fireEveryNMetricName,
			map[string]string{"event_type": fireEveryNMetricET, "subject": fireEveryNMetricSubj})
		if err != nil || len(metrics) == 0 {
			continue
		}
		lastSeen = metrics[0].Value
		if lastSeen < target {
			continue
		}
		result.Metrics["fire_every_n_gate_latency_ms"] = time.Since(stageStart).Milliseconds()
		result.Details["fire_every_n_published_after"] = lastSeen
		delta := lastSeen - baselinePublished
		result.Details["fire_every_n_published_delta"] = delta
		// Strict equality: more fires mean the gate counter is not working.
		if delta != float64(fireEveryNExpected) {
			return fmt.Errorf(
				"fire_every_n_events gate: expected exactly %d fires for %d events with N=%d, "+
					"got %.0f (baseline=%.0f, current=%.0f)",
				fireEveryNExpected, fireEveryNProbeCount, fireEveryNWindowN,
				delta, baselinePublished, lastSeen)
		}
		return nil
	}

	return fmt.Errorf(
		"fire_every_n_events gate: %s did not reach %.0f within 2s "+
			"(baseline=%.0f, last=%.0f, expected_delta=%d) — "+
			"check entity_watch_buckets config and rule %q in KV",
		fireEveryNMetricName, target, baselinePublished, lastSeen,
		fireEveryNExpected, fireEveryNRuleID)
}

// cleanupFireEveryNRule deletes the test rule so a second scenario run starts
// with a fresh counter and an unbiased active_rules gauge. Uses a fresh
// Background context because the parent context may already be cancelled
// by the time the deferred cleanup runs.
func (s *Scenario) cleanupFireEveryNRule() {
	dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.nats.DeleteKV(dctx, s.config.RulesBucket, fireEveryNRuleKey)
}

// cleanupProbeEntities deletes the injected ENTITY_STATES entries so the
// bucket is tidy for subsequent scenario runs.
func (s *Scenario) cleanupProbeEntities(keys []string) {
	dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, key := range keys {
		_ = s.nats.DeleteKV(dctx, fireEveryNKVBucket, key)
	}
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
