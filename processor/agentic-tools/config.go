package agentictools

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config holds configuration for agentic-tools processor component
type Config struct {
	Ports                *component.PortConfig `json:"ports"                schema:"type:ports,description:Port configuration,category:basic"`
	StreamName           string                `json:"stream_name"          schema:"type:string,description:JetStream stream name for agentic messages,category:basic,default:AGENT"`
	ConsumerNameSuffix   string                `json:"consumer_name_suffix" schema:"type:string,description:Suffix appended to consumer names for uniqueness,category:advanced"`
	DeleteConsumerOnStop bool                  `json:"delete_consumer_on_stop,omitempty" schema:"type:bool,description:Delete durable consumers on Stop (use for tests only),category:advanced,default:false"`
	Timeout              string                `json:"timeout"              schema:"type:string,description:Tool execution timeout,category:advanced,default:60s"`
	AllowedTools         []string              `json:"allowed_tools"        schema:"type:array,description:List of allowed tools (nil/empty allows all),category:advanced"`
	ApprovalRequired     []string              `json:"approval_required,omitempty" schema:"type:array,description:Tool names requiring human approval before execution,category:advanced"`
	// RestrictedDecideActions is the deployment-level decide-action
	// restriction policy (gh#239): decide action names barred for every
	// coordinator task — front-door and rule-spawned — composing with and
	// taking precedence over the per-task action_allowlist. Vocabulary-
	// agnostic: a product shell maps its run mode onto this list (e.g.
	// SemTeams "autonomous" → ["ask_user"]). Empty = permissive default
	// (no behaviour change). Sourced at boot into ToolDependencies, mirror
	// of LoopsBucket.
	RestrictedDecideActions []string `json:"restricted_decide_actions,omitempty" schema:"type:array,description:Decide-action names barred for every coordinator task (front-door and rule-spawned) — composes with and takes precedence over per-task action_allowlist; vocabulary-agnostic run/deployment clarification policy; empty means permissive default,category:advanced"`
	EnableCategories        bool     `json:"enable_categories,omitempty" schema:"type:bool,description:Enable tool category filtering for role-based access,category:advanced,default:false"`
	LoopsBucket             string   `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket name holding agent loop state (for read_loop_result),default:AGENT_LOOPS,category:advanced"`

	// ToolRetries is an opt-in per-tool retry policy. Tools not listed run
	// without retries. Use this for tools where transient failures (timeout,
	// external 5xx) are worth auto-retrying at the framework layer instead
	// of burning LLM iteration budget. Validation-shaped errors
	// (invalid_args, not_found) deliberately do NOT retry here by default —
	// those need LLM feedback via the agent's iteration loop.
	ToolRetries map[string]RetryPolicy `json:"tool_retries,omitempty" schema:"type:object,description:Per-tool retry policy keyed by tool name (opt-in; tools without an entry do not retry),category:advanced"`
}

// RetryPolicy controls how a single tool's transient failures are retried
// inside executeWithTimeout. Zero/empty values are replaced with defaults by
// the runtime; an all-zero policy still means "no retry" because
// MaxAttempts defaults to 1.
type RetryPolicy struct {
	// MaxAttempts is the total number of tries including the first call.
	// 1 means no retry. Values below 1 are clamped to 1.
	MaxAttempts int `json:"max_attempts" schema:"type:int,description:Total attempts including the first call (1 = no retry),default:1"`

	// BackoffInitialMs is the wait before the second attempt, in
	// milliseconds. Subsequent attempts use exponential backoff
	// (initial * 2^(attempt-1)) capped at BackoffMaxMs.
	BackoffInitialMs int `json:"backoff_initial_ms,omitempty" schema:"type:int,description:Initial backoff before retry in milliseconds,default:100"`

	// BackoffMaxMs caps the per-attempt backoff.
	BackoffMaxMs int `json:"backoff_max_ms,omitempty" schema:"type:int,description:Maximum backoff between retries in milliseconds,default:2000"`

	// RetryOnKinds names the ToolErrorKind values that should trigger
	// retry. Defaults to ["timeout", "external", "network"] when unset. Pass an
	// explicit empty list to retry only on raw executor errors (no
	// tool-level error kind considered).
	RetryOnKinds []string `json:"retry_on_kinds,omitempty" schema:"type:array,description:ToolErrorKind values that trigger retry (defaults to timeout+external+network),category:advanced"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	// Validate timeout
	if c.Timeout == "" {
		return errs.WrapInvalid(fmt.Errorf("timeout is required"), "Config", "Validate", "check timeout")
	}

	// Parse timeout to ensure it's valid
	duration, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "parse timeout format")
	}

	// Timeout must be positive
	if duration <= 0 {
		return errs.WrapInvalid(fmt.Errorf("timeout must be positive"), "Config", "Validate", "check timeout value")
	}

	// When the operator supplied a Ports block, enforce that Inputs and
	// Outputs are non-empty whenever DefaultConfig declares ports in
	// those directions. Pre-fix, an operator config that overrode Ports
	// without any Inputs (or set inputs:[]) started the component
	// running and healthy with zero JetStream consumers — silent
	// dispatch death. Symmetric to the publishResult silent-drop bug
	// closed in beta.57. Audit finding 2026-05-08.
	//
	// Validation is by-presence-of-any-port, not by-canonical-name:
	// operators can rename ports freely, override subjects, etc., as
	// long as they don't omit the entire direction. Subject coherence
	// across components is an operator concern; per-component validation
	// only catches structural emptiness.
	if c.Ports != nil {
		if err := c.validatePortsNonEmpty(); err != nil {
			return err
		}
	}

	// AllowedTools can be nil or empty (both mean allow all tools)
	// No validation needed for allowed_tools

	return nil
}

// validatePortsNonEmpty enforces that when the operator supplied a Ports
// block, neither Inputs nor Outputs is empty (provided DefaultConfig
// declares ports in those directions). Catches the silent-broken case
// where Ports is overridden but a direction is forgotten.
func (c *Config) validatePortsNonEmpty() error {
	defaults := DefaultConfig()
	if defaults.Ports == nil {
		return nil
	}

	if len(defaults.Ports.Inputs) > 0 && len(c.Ports.Inputs) == 0 {
		return errs.WrapInvalid(
			fmt.Errorf("Ports.Inputs is empty but DefaultConfig declares %d input port(s); agentic-tools cannot run without consumers — supply at least one input port or omit the Ports block to use canonical defaults", len(defaults.Ports.Inputs)),
			"Config", "validatePortsNonEmpty", "input port presence",
		)
	}
	if len(defaults.Ports.Outputs) > 0 && len(c.Ports.Outputs) == 0 {
		return errs.WrapInvalid(
			fmt.Errorf("Ports.Outputs is empty but DefaultConfig declares %d output port(s); tool.result publishes will silently drop — supply at least one output port or omit the Ports block to use canonical defaults", len(defaults.Ports.Outputs)),
			"Config", "validatePortsNonEmpty", "output port presence",
		)
	}

	return nil
}

// DefaultConfig returns default configuration for agentic-tools processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "tool.execute",
			Type:        "jetstream",
			Subject:     "tool.execute.>",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Tool execution requests (JetStream)",
		},
		{
			Name:        "tool.list",
			Type:        "nats",
			Subject:     "tool.list",
			Description: "Tool discovery request/reply (core NATS). Override to e.g. 'discovery.tool.list' when JetStream streams cover 'tool.>'.",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "tool.result",
			Type:        "jetstream",
			Subject:     "tool.result.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Tool execution results (JetStream)",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		StreamName:   "AGENT",
		Timeout:      "60s",
		AllowedTools: nil, // nil means allow all tools
		LoopsBucket:  "AGENT_LOOPS",
	}
}
