// Package graphresearch composes the graph-first research capability defined
// by ADR-045. It is intentionally separate from the core composition roots:
// selecting it adds all five stages, research payloads, and the research_graph
// tool as one coherent capability.
package graphresearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/payloadregistry"
	researchassess "github.com/c360studio/semstreams/processor/research-graph-assess"
	researchclassify "github.com/c360studio/semstreams/processor/research-graph-classify"
	researchexecute "github.com/c360studio/semstreams/processor/research-graph-execute"
	researchroute "github.com/c360studio/semstreams/processor/research-graph-route"
	researchsynthesize "github.com/c360studio/semstreams/processor/research-graph-synthesize"
)

var stageFactories = []string{
	"research-graph-classify",
	"research-graph-route",
	"research-graph-execute",
	"research-graph-assess",
	"research-graph-synthesize",
}

var requiredRuntimeComponents = []string{
	"agentic-loop",
	"agentic-model",
	"agentic-tools",
	"graph-ingest",
	"graph-query",
	"rule-processor",
}

var requiredModelCapabilities = []string{
	model.CapabilityResearchRouting,
	model.CapabilityResearchAssessment,
	model.CapabilityResearchSynthesis,
}

var requiredAgentTools = []string{
	ResearchGraphToolName,
	"read_loop_result",
}

const defaultLoopsBucket = "AGENT_LOOPS"

var requiredRuleFiles = []string{
	"00-kickoff-classify.json",
	"01-classify-routes.json",
	"02-route-decision-dispatch.json",
	"03-execute-assesses.json",
	"04-assess-dispatch.json",
	"05-continuation.json",
}

type ruleSignature struct {
	id              string
	triggerField    string
	triggerOperator string
	triggerValue    string
	actions         []ruleActionSignature
}

type ruleActionSignature struct {
	typeName      string
	subjectPrefix string
	tool          string
}

var requiredRuleSignatures = map[string]ruleSignature{
	"00-kickoff-classify.json": {
		id:              "research_kickoff_classify",
		triggerField:    "research.request.received",
		triggerOperator: "eq",
		triggerValue:    "true",
		actions:         []ruleActionSignature{{typeName: "publish", subjectPrefix: "component.nl_classify."}},
	},
	"01-classify-routes.json": {
		id:              "research_classify_routes",
		triggerField:    "research.classify.complete",
		triggerOperator: "ne",
		actions:         []ruleActionSignature{{typeName: "publish", subjectPrefix: "component.route_search."}},
	},
	"02-route-decision-dispatch.json": {
		id:              "research_route_decision_dispatch",
		triggerField:    "research.route.complete",
		triggerOperator: "ne",
		actions: []ruleActionSignature{
			{typeName: "publish", subjectPrefix: "component.synthesize_answer."},
			{typeName: "publish", subjectPrefix: "component.nl_classify."},
			{typeName: "publish", subjectPrefix: "component.execute_subqueries."},
		},
	},
	"03-execute-assesses.json": {
		id:              "research_execute_assesses",
		triggerField:    "research.execute.complete",
		triggerOperator: "ne",
		actions:         []ruleActionSignature{{typeName: "publish", subjectPrefix: "component.assess_sufficiency."}},
	},
	"04-assess-dispatch.json": {
		id:              "research_assess_dispatch",
		triggerField:    "research.assess.complete",
		triggerOperator: "ne",
		actions: []ruleActionSignature{
			{typeName: "publish", subjectPrefix: "component.synthesize_answer."},
			{typeName: "publish", subjectPrefix: "component.execute_subqueries."},
		},
	},
	"05-continuation.json": {
		id:              "research_continuation",
		triggerField:    "research.search-result.complete",
		triggerOperator: "ne",
		actions:         []ruleActionSignature{{typeName: "publish_agent", subjectPrefix: "agent.task.research_continuation", tool: "read_loop_result"}},
	},
}

// Selected reports whether a config requests any part of graph research.
// A partial selection is still selected so ValidateConfig can reject it.
func Selected(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, componentConfig := range cfg.Components {
		if !componentConfig.Enabled {
			continue
		}
		if slices.Contains(stageFactories, componentConfig.Name) {
			return true
		}
		switch componentConfig.Name {
		case "rule-processor":
			var ruleConfig struct {
				RuleFiles []string `json:"rules_files"`
			}
			if json.Unmarshal(componentConfig.Config, &ruleConfig) == nil && containsResearchRule(ruleConfig.RuleFiles) {
				return true
			}
		case "agentic-tools":
			var toolConfig struct {
				AllowedTools []string `json:"allowed_tools"`
			}
			if json.Unmarshal(componentConfig.Config, &toolConfig) == nil && slices.Contains(toolConfig.AllowedTools, ResearchGraphToolName) {
				return true
			}
		case "agentic-dispatch":
			var dispatchConfig struct {
				DefaultTools []string `json:"default_tools"`
			}
			if json.Unmarshal(componentConfig.Config, &dispatchConfig) == nil && slices.Contains(dispatchConfig.DefaultTools, ResearchGraphToolName) {
				return true
			}
		}
	}
	return false
}

// ValidateConfig rejects partially configured graph research before any tool
// catalog can be served. An entirely absent capability is valid.
func ValidateConfig(cfg *config.Config) error {
	if !Selected(cfg) {
		return nil
	}

	enabled := make(map[string]bool)
	configured := make(map[string][]json.RawMessage)
	var ruleFiles []string
	for _, componentConfig := range cfg.Components {
		if !componentConfig.Enabled {
			continue
		}
		enabled[componentConfig.Name] = true
		configured[componentConfig.Name] = append(configured[componentConfig.Name], componentConfig.Config)
		if componentConfig.Name == "rule-processor" {
			if len(componentConfig.Config) == 0 {
				continue
			}
			var ruleConfig struct {
				RuleFiles []string `json:"rules_files"`
			}
			if err := json.Unmarshal(componentConfig.Config, &ruleConfig); err != nil {
				return fmt.Errorf("graph research requires readable rule-processor config: %w", err)
			}
			ruleFiles = append(ruleFiles, ruleConfig.RuleFiles...)
		}
	}

	var missing []string
	for _, name := range append(slices.Clone(stageFactories), requiredRuntimeComponents...) {
		if !enabled[name] {
			missing = append(missing, "component "+name)
		}
	}
	for _, ruleName := range requiredRuleFiles {
		found := false
		for _, file := range ruleFiles {
			if strings.HasSuffix(file, ruleName) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, "rule "+ruleName)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("partial graph research composition; missing %s", strings.Join(missing, ", "))
	}
	if err := validateCanonicalRuleUniqueness(ruleFiles); err != nil {
		return fmt.Errorf("partial graph research composition: %w", err)
	}
	if err := validateModelRegistry(cfg.ModelRegistry); err != nil {
		return fmt.Errorf("partial graph research composition: %w", err)
	}
	if err := validateToolAllowlist(configured["agentic-tools"]); err != nil {
		return fmt.Errorf("partial graph research composition: %w", err)
	}
	if err := validateLoopsBuckets(configured); err != nil {
		return fmt.Errorf("partial graph research composition: %w", err)
	}
	if err := validateRulePack(ruleFiles); err != nil {
		return fmt.Errorf("partial graph research composition: %w", err)
	}
	return nil
}

func containsResearchRule(files []string) bool {
	for _, file := range files {
		if strings.Contains(file, "research-graph") {
			return true
		}
		for _, required := range requiredRuleFiles {
			if strings.HasSuffix(file, required) {
				return true
			}
		}
	}
	return false
}

// LoopsBucket returns the agentic-tools loop bucket selected for the
// capability. ValidateConfig proves that agentic-loop and all five stages use
// this same value before registration reaches the NATS store.
func LoopsBucket(cfg *config.Config) string {
	if cfg == nil {
		return defaultLoopsBucket
	}
	for _, componentConfig := range cfg.Components {
		if componentConfig.Name != "agentic-tools" || !componentConfig.Enabled {
			continue
		}
		var bucketConfig struct {
			LoopsBucket string `json:"loops_bucket"`
		}
		if json.Unmarshal(componentConfig.Config, &bucketConfig) == nil && bucketConfig.LoopsBucket != "" {
			return bucketConfig.LoopsBucket
		}
	}
	return defaultLoopsBucket
}

func validateModelRegistry(registry *model.Registry) error {
	if registry == nil {
		return errors.New("model registry is required")
	}
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("invalid model registry: %w", err)
	}
	for _, capability := range requiredModelCapabilities {
		if registry.GetCapability(capability) == nil {
			return fmt.Errorf("missing model capability %s", capability)
		}
		endpointName := registry.Resolve(capability)
		if endpointName == "" || registry.GetEndpoint(endpointName) == nil {
			return fmt.Errorf("model capability %s does not resolve to an endpoint", capability)
		}
	}
	defaultEndpoint := registry.GetEndpoint(registry.GetDefault())
	if defaultEndpoint == nil || !defaultEndpoint.SupportsTools {
		return errors.New("a tool-capable default model endpoint is required")
	}
	return nil
}

func validateToolAllowlist(configs []json.RawMessage) error {
	for _, raw := range configs {
		if len(raw) == 0 {
			continue
		}
		var toolConfig struct {
			AllowedTools []string `json:"allowed_tools"`
		}
		if err := json.Unmarshal(raw, &toolConfig); err != nil {
			return fmt.Errorf("read agentic-tools config: %w", err)
		}
		// Empty is the component's documented allow-all setting.
		if len(toolConfig.AllowedTools) == 0 {
			continue
		}
		for _, required := range requiredAgentTools {
			if !slices.Contains(toolConfig.AllowedTools, required) {
				return fmt.Errorf("missing agentic-tools allowed_tools entry %s", required)
			}
		}
	}
	return nil
}

func validateCanonicalRuleUniqueness(configuredFiles []string) error {
	for _, name := range requiredRuleFiles {
		matches := 0
		for _, configured := range configuredFiles {
			if strings.HasSuffix(configured, name) {
				matches++
			}
		}
		if matches > 1 {
			return fmt.Errorf("duplicate canonical rule %s configured %d times", name, matches)
		}
	}
	return nil
}

func validateLoopsBuckets(configs map[string][]json.RawMessage) error {
	names := append([]string{"agentic-loop", "agentic-tools"}, stageFactories...)
	var common string
	for _, name := range names {
		for _, raw := range configs[name] {
			var bucketConfig struct {
				LoopsBucket string `json:"loops_bucket"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &bucketConfig); err != nil {
					return fmt.Errorf("read %s loops_bucket: %w", name, err)
				}
			}
			bucket := bucketConfig.LoopsBucket
			if bucket == "" {
				bucket = defaultLoopsBucket
			}
			if common == "" {
				common = bucket
				continue
			}
			if bucket != common {
				return fmt.Errorf("all graph research components require a common loops_bucket: %s uses %q, want %q", name, bucket, common)
			}
		}
	}
	return nil
}

func validateRulePack(configuredFiles []string) error {
	for _, name := range requiredRuleFiles {
		path := ""
		for _, configured := range configuredFiles {
			if strings.HasSuffix(configured, name) {
				path = configured
				break
			}
		}
		if path == "" {
			// The missing-name list above normally catches this; retain a
			// defensive error for callers that reuse this helper.
			return fmt.Errorf("missing canonical rule %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read canonical rule %s: %w", path, err)
		}
		if err := validateRuleContent(name, data); err != nil {
			return fmt.Errorf("canonical rule %s: %w", path, err)
		}
	}
	return nil
}

func validateRuleContent(name string, data []byte) error {
	var rule struct {
		ID         string `json:"id"`
		Enabled    bool   `json:"enabled"`
		Conditions []struct {
			Field    string `json:"field"`
			Operator string `json:"operator"`
			Value    any    `json:"value"`
		} `json:"conditions"`
		OnEnter []struct {
			Type    string   `json:"type"`
			Subject string   `json:"subject"`
			Tools   []string `json:"tools"`
		} `json:"on_enter"`
	}
	if err := json.Unmarshal(data, &rule); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	signature := requiredRuleSignatures[name]
	if rule.ID != signature.id {
		return fmt.Errorf("canonical rule identity = %q, want %q", rule.ID, signature.id)
	}
	if !rule.Enabled {
		return errors.New("canonical rule must be enabled")
	}
	hasRoleScope := false
	hasTrigger := false
	for _, condition := range rule.Conditions {
		value, _ := condition.Value.(string)
		if condition.Field == "agent.loop.role" && condition.Operator == "eq" && value == "research_pipeline" {
			hasRoleScope = true
		}
		if condition.Field == signature.triggerField && condition.Operator == signature.triggerOperator && value == signature.triggerValue {
			hasTrigger = true
		}
	}
	if !hasRoleScope {
		return errors.New("missing canonical research_pipeline role condition")
	}
	if !hasTrigger {
		return fmt.Errorf("missing canonical trigger condition %s", signature.triggerField)
	}
	for _, required := range signature.actions {
		found := false
		for _, action := range rule.OnEnter {
			if action.Type != required.typeName || !strings.HasPrefix(action.Subject, required.subjectPrefix) {
				continue
			}
			if required.tool != "" && !slices.Contains(action.Tools, required.tool) {
				continue
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("missing required %s action for subject %s", required.typeName, required.subjectPrefix)
		}
	}
	return nil
}

// RegisterComponents registers all five bounded graph-research stages.
func RegisterComponents(registry *component.Registry) error {
	if registry == nil {
		return errors.New("register graph research components: nil registry")
	}
	registrations := []struct {
		name string
		fn   func(*component.Registry) error
	}{
		{"research-graph-classify", researchclassify.Register},
		{"research-graph-route", researchroute.Register},
		{"research-graph-execute", researchexecute.Register},
		{"research-graph-assess", researchassess.Register},
		{"research-graph-synthesize", researchsynthesize.Register},
	}
	var errs []error
	for _, registration := range registrations {
		if err := registration.fn(registry); err != nil {
			errs = append(errs, fmt.Errorf("register %s: %w", registration.name, err))
		}
	}
	return errors.Join(errs...)
}

// RegisterPayloads registers the complete research message family.
func RegisterPayloads(registry *payloadregistry.Registry) error {
	if registry == nil {
		return errors.New("register graph research payloads: nil registry")
	}
	return research.RegisterPayloads(registry)
}
