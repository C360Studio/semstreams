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
	"github.com/c360studio/semstreams/vocabulary/rulepacks"
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
	// increased by exactly one after the mock's create_rule call landed.
	// The scenario fails before mutation when this required baseline cannot be
	// established.
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
		{"verify-tool-effect-catalog", s.verifyToolEffectCatalog},
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
	baseline, err := s.metrics.GetMetricValue(ctx, "semstreams_rule_active_rules")
	if err != nil {
		return fmt.Errorf("capture required semstreams_rule_active_rules baseline from %s: %w",
			s.config.MetricsURL, err)
	}
	s.baselineActiveRules = baseline
	result.Details["baseline_active_rules"] = baseline
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
func (s *Scenario) verifyHotreloadPickup(ctx context.Context, result *scenarios.Result) error {
	if s.baselineActiveRules < 0 {
		return fmt.Errorf("hot-reload pickup: required active-rules baseline is unavailable")
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

// verifyRegisteredTools confirms the component catalog and workflow-run
// observation tools actually registered into the shared tool registry at
// binary startup. This catches wiring regressions that unit tests
// can't see — e.g. cmd/semstreams/main.go dropping
// ComponentRegistry from ToolDependencies, or executors.RegisterBuiltins
// dropping the registerComponentCatalog call. Each register fn emits
// a distinct slog.Info "Registered <tool> tool" line on success and a
// slog.Warn "<tool> disabled: ..." on nil-skip; we grep container logs
// for the success lines and fail on absence.
//
// This is a lightweight substitute for a full tool-invocation e2e.
// Exercising the tools through the LLM → dispatcher → executor chain
// would require adding list_components/monitor_workflow_runs to the flow's
// allowed_tools and scripting multi-tool mock sequences, which dilutes
// what the crud-tools scenario exists to test. A proper invocation
// test belongs in a coordinator-flavored scenario (follow-up).
func (s *Scenario) verifyRegisteredTools(ctx context.Context, result *scenarios.Result) error {
	required := map[string]string{
		"list_components":       "Registered list_components tool",
		"monitor_workflow_runs": "Registered monitor_workflow_runs tool",
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
				"executors.RegisterBuiltins wiring",
			missing)
	}

	result.Details["registered_m2_tools"] = []string{"list_components", "monitor_workflow_runs"}
	return nil
}

// toolListSubject is the typed request/reply discovery default. This stage
// deliberately exercises the shipped default rather than a scenario override.
const toolListSubject = "discovery.tool.list"

// verifyToolEffectCatalog asserts the operator-visible discovery catalog carries
// a resolved effect classification for every tool (gh#749, ADR-089).
//
// This is the e2e coverage stage for the effect contract: the unit tests prove
// the projection in isolation, but only a live request/reply proves that a real
// deployment's tool.list responder serves the field — the gap #762's response
// shape bug lived in for weeks.
//
// Three assertions, each catching a different regression:
//   - every entry carries a NON-EMPTY effect (an omitempty regression on the DTO
//     would silently drop the fail-safe value for unclassified tools)
//   - every effect is a member of the enum (a raw unnormalized value reaching
//     the wire)
//   - a known read tool and a known write tool carry their expected values (the
//     classification survived registration → aggregation → projection, rather
//     than everything collapsing to "unknown")
func (s *Scenario) verifyToolEffectCatalog(ctx context.Context, result *scenarios.Result) error {
	// RequestClassified, not Request: a raw request returns a handler error as a
	// success-shaped {message,detail} body with err == nil, which would decode
	// here as a catalog of zero tools and report as "responder not subscribed"
	// (gh#337).
	raw, err := s.nats.RequestClassified(ctx, toolListSubject, []byte("{}"), 10*time.Second)
	if err != nil {
		return fmt.Errorf("request %s: %w (is the agentic-tools tool.list responder subscribed?)", toolListSubject, err)
	}

	var response struct {
		Tools []struct {
			Name   string `json:"name"`
			Effect string `json:"effect"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("unmarshal tool.list response: %w (body: %.200s)", err, raw)
	}
	if len(response.Tools) == 0 {
		// A body shaped like {"stream":...,"seq":...} is a JetStream publish
		// ack, not a catalog: a stream whose subjects cover the discovery
		// subject answers the request before the component's core-NATS
		// responder does, and the subscription still succeeds so nothing warns.
		return fmt.Errorf("%s returned zero tools — the catalog assertion would be vacuous (body: %.200s). "+
			"A {\"stream\",\"seq\"} body means a JetStream stream captured the discovery subject; "+
			"narrow the capturing stream subjects; the shipped TOOL contract is tool.execute.> + tool.result.>", toolListSubject, raw)
	}

	validEffects := map[string]bool{
		string(agentic.ToolEffectUnknown):  true,
		string(agentic.ToolEffectReadOnly): true,
		string(agentic.ToolEffectMutating): true,
		string(agentic.ToolEffectExternal): true,
	}
	// Expected classifications for tools this deployment is guaranteed to
	// register. Both directions: a read tool and a write tool, so a catalog
	// that collapsed every entry to one value fails here.
	expected := map[string]string{
		"list_rules":  string(agentic.ToolEffectReadOnly),
		"create_rule": string(agentic.ToolEffectMutating),
	}

	var blank, invalid, mismatched []string
	seen := make(map[string]string, len(response.Tools))
	for _, tool := range response.Tools {
		seen[tool.Name] = tool.Effect
		switch {
		case tool.Effect == "":
			blank = append(blank, tool.Name)
		case !validEffects[tool.Effect]:
			invalid = append(invalid, fmt.Sprintf("%s=%q", tool.Name, tool.Effect))
		}
	}
	for name, want := range expected {
		got, ok := seen[name]
		if !ok {
			return fmt.Errorf("tool %q absent from the discovery catalog — this deployment registers rule CRUD, so the catalog assertion is not measuring what it claims", name)
		}
		if got != want {
			mismatched = append(mismatched, fmt.Sprintf("%s: got %q want %q", name, got, want))
		}
	}

	if len(blank) > 0 {
		return fmt.Errorf("%d tool(s) served a blank effect: %v — the discovery field must always carry the resolved value, including \"unknown\"", len(blank), blank)
	}
	if len(invalid) > 0 {
		return fmt.Errorf("tool(s) served an effect outside the enum: %v — aggregation must normalize before the value reaches the wire", invalid)
	}
	if len(mismatched) > 0 {
		return fmt.Errorf("classification did not survive to discovery: %v", mismatched)
	}

	result.Details["tool_effect_catalog_size"] = len(response.Tools)
	result.Details["tool_effects"] = seen
	return nil
}

// fireEveryNConstants groups magic numbers for the sampling-gate stage so
// they appear in one place and are easy to scan.
const (
	fireEveryNRuleID            = "e2e-fire-every-n-test"
	fireEveryNEntityType        = "e2e-fire-probe"
	fireEveryNWindowN           = 3
	fireEveryNProbeCount        = 9
	fireEveryNExpected          = fireEveryNProbeCount / fireEveryNWindowN // 3
	fireEveryNKVBucket          = "ENTITY_STATES"
	fireEveryNRuleKey           = "rules." + fireEveryNRuleID
	fireEveryNEvaluationsMetric = "semstreams_rule_evaluations_total"
	fireEveryNGatePassesMetric  = "semstreams_rule_action_gate_passes_total"
)

type fireEveryNMetricValues struct {
	triggered    float64
	notTriggered float64
	gatePasses   float64
}

func (values fireEveryNMetricValues) delta(baseline fireEveryNMetricValues) fireEveryNMetricValues {
	return fireEveryNMetricValues{
		triggered:    values.triggered - baseline.triggered,
		notTriggered: values.notTriggered - baseline.notTriggered,
		gatePasses:   values.gatePasses - baseline.gatePasses,
	}
}

func (values fireEveryNMetricValues) isExact() bool {
	return values.triggered == fireEveryNProbeCount &&
		values.notTriggered == 0 &&
		values.gatePasses == fireEveryNExpected
}

// verifyFireEveryNEvents exercises the fire_every_n_events sampling gate
// (PR2 of ADR-027 Phase 1, commit 34bc655) end to end through a live
// rule-processor. The gate was previously covered only by unit and
// integration tests; this stage proves it works in a running system.
//
// Assertion path: per-rule metrics (not graph triples or optional rule-event
// notifications). Nine matching evaluations must all be triggered, none may be
// not-triggered, and the action gate must admit exactly three with N=3.
func (s *Scenario) verifyFireEveryNEvents(ctx context.Context, result *scenarios.Result) error {
	defer s.cleanupFireEveryNRule()

	if err := s.seedFireEveryNRule(ctx, result); err != nil {
		return err
	}
	if err := s.waitForFireEveryNRuleHotReload(ctx, result); err != nil {
		return err
	}

	baseline, err := s.captureFireEveryNBaseline(ctx)
	if err != nil {
		return err
	}
	result.Details["fire_every_n_baseline_triggered"] = baseline.triggered
	result.Details["fire_every_n_baseline_not_triggered"] = baseline.notTriggered
	result.Details["fire_every_n_baseline_gate_passes"] = baseline.gatePasses

	injectedKeys, err := s.injectProbeEntities(ctx, result)
	if err != nil {
		return err
	}
	defer s.cleanupProbeEntities(injectedKeys)

	return s.assertFireEveryNGate(ctx, result, baseline)
}

// seedFireEveryNRule writes the sampling-gate rule directly to KV,
// exercising the rule-processor's hot-reload watcher.
// No on_enter/while_true actions: the stateful evaluator runs those
// directly and bypasses the gate. Empty action lists keep the action-gate metric
// as the direct admission observation without requiring graph integration or an
// optional rule-event notification output.
func fireEveryNRuleDefinition() rule.Definition {
	return rule.Definition{
		ID:               fireEveryNRuleID,
		Type:             "expression",
		Name:             "Fire Every N Test",
		Description:      "e2e sampling-gate test: fires only on every 3rd matching event",
		Enabled:          true,
		FireEveryNEvents: fireEveryNWindowN,
		Entity: rule.EntityConfig{
			Pattern: "org.platform.test.probe.e2e.*",
		},
		Conditions: []expression.ConditionExpression{
			{Field: rulepacks.EntityIdentityType, Operator: "eq", Value: fireEveryNEntityType},
		},
		Logic: "and",
	}
}

func (s *Scenario) seedFireEveryNRule(ctx context.Context, result *scenarios.Result) error {
	ruleDef := fireEveryNRuleDefinition()
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
// gauge reaches baseline+2 (e2e-crud-rule plus the new gate rule).
func (s *Scenario) waitForFireEveryNRuleHotReload(ctx context.Context, result *scenarios.Result) error {
	if s.baselineActiveRules < 0 {
		return fmt.Errorf("fire-every-n hot-reload: required active-rules baseline is unavailable")
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
	return fmt.Errorf("fire-every-n hot-reload: active_rules did not reach %.0f within 2s (last=%.0f)",
		expected, lastSeen)
}

// captureFireEveryNBaseline reads all three per-rule counters from one metrics
// snapshot so the assertion compares a consistent baseline.
func (s *Scenario) captureFireEveryNBaseline(ctx context.Context) (fireEveryNMetricValues, error) {
	if s.baselineActiveRules < 0 {
		return fireEveryNMetricValues{}, fmt.Errorf(
			"capture fire_every_n_events baseline: required active-rules readiness is unavailable",
		)
	}
	values, err := s.readFireEveryNMetrics(ctx)
	if err != nil {
		return fireEveryNMetricValues{}, fmt.Errorf(
			"capture fire_every_n_events baseline from %s: %w", s.config.MetricsURL, err,
		)
	}
	return values, nil
}

func (s *Scenario) readFireEveryNMetrics(ctx context.Context) (fireEveryNMetricValues, error) {
	snapshot, err := s.metrics.FetchSnapshot(ctx)
	if err != nil {
		return fireEveryNMetricValues{}, err
	}
	var values fireEveryNMetricValues
	for _, observed := range snapshot.Metrics {
		if observed.Labels["rule_name"] != fireEveryNRuleID {
			continue
		}
		switch observed.Name {
		case fireEveryNEvaluationsMetric:
			switch observed.Labels["result"] {
			case "triggered":
				values.triggered = observed.Value
			case "not_triggered":
				values.notTriggered = observed.Value
			}
		case fireEveryNGatePassesMetric:
			values.gatePasses = observed.Value
		}
	}
	return values, nil
}

// injectProbeEntities writes 9 EntityState records to ENTITY_STATES KV.
// Each entity has a triple with predicate "entity.identity.type" = fireEveryNEntityType
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
					Predicate: "entity.identity.type",
					Object:    fireEveryNEntityType,
					Source:    "e2e-fire-every-n-seed",
					Timestamp: time.Now(),
				},
			},
			MessageType: message.Type{Domain: "e2e", Category: "probe", Version: "v1"},
			Version:     1,
			UpdatedAt:   time.Now(),
		}
		entityJSON, err := graph.MarshalEntityState(&entityState)
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

// assertFireEveryNGate polls one consistent per-rule snapshot until all exact
// deltas hold: triggered=9, not_triggered=0, and gate_passes=3.
func (s *Scenario) assertFireEveryNGate(
	ctx context.Context,
	result *scenarios.Result,
	baseline fireEveryNMetricValues,
) error {
	if s.baselineActiveRules < 0 {
		return fmt.Errorf("fire_every_n_events gate: required metrics baseline is unavailable")
	}

	deadline := time.Now().Add(2 * time.Second)
	var lastDelta fireEveryNMetricValues
	var lastReadErr error
	stageStart := time.Now()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		current, err := s.readFireEveryNMetrics(ctx)
		if err != nil {
			lastReadErr = err
			continue
		}
		lastReadErr = nil
		lastDelta = current.delta(baseline)
		if lastDelta.notTriggered > 0 ||
			lastDelta.triggered > fireEveryNProbeCount ||
			lastDelta.gatePasses > fireEveryNExpected {
			return fmt.Errorf(
				"fire_every_n_events gate exceeded exact deltas: triggered=%.0f not_triggered=%.0f gate_passes=%.0f "+
					"(want %d/0/%d)",
				lastDelta.triggered, lastDelta.notTriggered, lastDelta.gatePasses,
				fireEveryNProbeCount, fireEveryNExpected,
			)
		}
		if !lastDelta.isExact() {
			continue
		}
		result.Metrics["fire_every_n_gate_latency_ms"] = time.Since(stageStart).Milliseconds()
		result.Metrics["fire_every_n_triggered_delta"] = lastDelta.triggered
		result.Metrics["fire_every_n_not_triggered_delta"] = lastDelta.notTriggered
		result.Metrics["fire_every_n_gate_passes_delta"] = lastDelta.gatePasses
		result.Details["fire_every_n_triggered_delta"] = lastDelta.triggered
		result.Details["fire_every_n_not_triggered_delta"] = lastDelta.notTriggered
		result.Details["fire_every_n_gate_passes_delta"] = lastDelta.gatePasses
		return nil
	}

	return fmt.Errorf(
		"fire_every_n_events gate did not reach exact deltas within 2s: "+
			"triggered=%.0f not_triggered=%.0f gate_passes=%.0f (want %d/0/%d); "+
			"last metrics read error=%v — check entity_watch_buckets config and rule %q in KV",
		lastDelta.triggered, lastDelta.notTriggered, lastDelta.gatePasses,
		fireEveryNProbeCount, fireEveryNExpected, lastReadErr, fireEveryNRuleID,
	)
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
