package agenticloop

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// DefaultContextLimit is the fallback context window size when the model is unknown.
const DefaultContextLimit = 128000

// Tool-call governance mode constants. ADR-039.
//
// Disabled is the default — no publish to the proposed subject,
// direct dispatch. The in-process ToolCallFilter wiring beta.67+68
// shipped was retired in beta.70; subject-mode is now the sole
// governance path.
//
// Audit publishes proposed tool calls to the proposed subject and
// dispatches IMMEDIATELY without waiting for a verdict — shadow mode
// for operators developing governance rules without blocking
// production tool-call flow. Verdicts that arrive later are counted
// for observability only.
//
// Enforce subscribes-then-publishes, waits up to ToolCallGovernance.Timeout
// for a per-call verdict, and dispatches on approve / fails on
// reject / fail-closed on timeout. The default 1s timeout is
// deliberately generous for beta.69 to surface real p99 latency
// before tightening in beta.70.
const (
	ToolCallGovernanceModeDisabled = "disabled"
	ToolCallGovernanceModeAudit    = "audit"
	ToolCallGovernanceModeEnforce  = "enforce"
)

// DefaultToolCallGovernanceTimeout is the wait window for a per-call
// verdict in enforce mode before the dispatcher fails-closed (treats
// the absence of a verdict as a reject). 1s is generous for beta.69 to
// surface real p99 latency; tightens after measurement in beta.70 per
// ADR-039 §"Observability".
const DefaultToolCallGovernanceTimeout = "1s"

// Config represents the configuration for the agentic-loop processor
type Config struct {
	MaxIterations        int                      `json:"max_iterations" schema:"type:int,description:Maximum number of iterations before loop terminates,default:20,min:1,max:1000,category:basic,required"`
	Timeout              string                   `json:"timeout" schema:"type:string,description:Timeout duration for loop execution (e.g. 120s or 5m),default:120s,category:basic,required"`
	StreamName           string                   `json:"stream_name" schema:"type:string,description:JetStream stream name,default:AGENT,category:advanced"`
	ConsumerNameSuffix   string                   `json:"consumer_name_suffix" schema:"type:string,description:Suffix for consumer names,category:advanced"`
	DeleteConsumerOnStop bool                     `json:"delete_consumer_on_stop,omitempty" schema:"type:bool,description:Delete durable consumers on Stop (use for tests only),category:advanced,default:false"`
	LoopsBucket          string                   `json:"loops_bucket" schema:"type:string,description:NATS KV bucket name for storing loop state,default:AGENT_LOOPS,category:advanced,required"`
	ToolResultMaxBytes   int                      `json:"tool_result_max_bytes,omitempty" schema:"type:int,description:Maximum bytes for tool result content before truncation. 0 means no limit,default:32768,category:advanced"`
	TrajectoryDetail     string                   `json:"trajectory_detail,omitempty" schema:"type:string,description:Trajectory detail level: summary (default) or full,default:summary,category:advanced"`
	ContentBucket        string                   `json:"content_bucket,omitempty" schema:"type:string,description:NATS ObjectStore bucket for trajectory step content (tool results and model responses),default:AGENT_CONTENT,category:advanced"`
	TrajectoryCacheTTL   string                   `json:"trajectory_cache_ttl,omitempty" schema:"type:string,description:TTL for trajectory cache (e.g. 4h or 30m). Trajectories older than this are only available via graph queries,default:4h,category:advanced"`
	ApprovalTimeoutStr   string                   `json:"approval_timeout,omitempty" schema:"type:string,description:Auto-reject pending approvals after this duration (e.g. 5m or 1h). Empty means wait indefinitely,category:advanced"`
	Consumer             ConsumerConfig           `json:"consumer" schema:"type:object,description:JetStream consumer tuning for long-running ports (agent.task/agent.response/tool.result),category:advanced"`
	Context              ContextConfig            `json:"context" schema:"type:object,description:Context window management. Model limits are resolved from the model registry,category:advanced"`
	ToolCallGovernance   ToolCallGovernanceConfig `json:"tool_call_governance,omitempty" schema:"type:object,description:Subject-mode tool-call governance (ADR-039). Default mode=disabled is no-op (no governance gate),category:advanced"`
	// SynthesizeTerminalOnCompletion enables the framework to synthesize a
	// decide(needs_clarification) when a loop completes without a terminal
	// tool call (#133). Cheap-model substrate recovery: when a small model
	// returns text-only at completion despite persona prose enforcing a
	// decide call, the framework stamps coordinator.next_action +
	// coordinator.decision.reason + coordinator.decision.synthetic="true"
	// on the loop entity so existing recovery rules match. Default false
	// for back-compat — new deployments targeting cheap-model substrates
	// should opt in.
	SynthesizeTerminalOnCompletion bool                  `json:"synthesize_terminal_on_completion,omitempty" schema:"type:bool,description:Synthesize decide(needs_clarification) when a loop completes without a terminal tool call (#133). Belt-and-suspenders recovery for cheap-model substrates where models occasionally return text-only at completion despite persona prose,default:false,category:advanced"`
	Ports                          *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration for inputs and outputs,category:basic"`
}

// ToolCallGovernanceConfig configures subject-mode tool-call governance
// (ADR-039). When Mode is "disabled" (the default), tool calls dispatch
// directly with no governance gate. When Mode is "audit", every tool
// call is published to agent.toolcall.proposed.* but dispatch is NOT
// blocked — verdicts that arrive are logged/counted for observability.
// When Mode is "enforce", the loop publishes-then-waits for a per-call
// verdict on agent.toolcall.approved/rejected.* subjects, with Timeout
// as the fail-closed window.
//
// Beta.70 retired the in-process ToolCallFilter wiring that the
// beta.67+68 release shipped (per ADR-039 Phase 3); subject-mode is
// now the sole governance path.
type ToolCallGovernanceConfig struct {
	// Mode is one of "disabled", "audit", or "enforce". Default
	// "disabled" leaves no governance gate.
	Mode string `json:"mode,omitempty" schema:"type:string,description:Governance mode (disabled|audit|enforce). Default disabled means no governance gate,default:disabled,category:advanced"`

	// Timeout is the per-call wait window in enforce mode before the
	// dispatcher fails-closed (rejects). Parsed as a Go duration string
	// (e.g. "500ms", "2s"). Ignored when Mode is "disabled" or "audit".
	Timeout string `json:"timeout,omitempty" schema:"type:string,description:Per-call verdict wait window in enforce mode (e.g. 500ms or 2s). Default 1s,default:1s,category:advanced"`
}

// IsEnabled returns true when the governance flow is active (audit or
// enforce). Callers use this to skip publish setup work when governance
// is disabled.
func (c ToolCallGovernanceConfig) IsEnabled() bool {
	return c.Mode == ToolCallGovernanceModeAudit || c.Mode == ToolCallGovernanceModeEnforce
}

// IsEnforcing returns true when verdicts gate dispatch. Callers use
// this to decide whether to wait for verdicts before dispatching.
func (c ToolCallGovernanceConfig) IsEnforcing() bool {
	return c.Mode == ToolCallGovernanceModeEnforce
}

// ParsedTimeout returns the parsed Timeout duration. Falls back to the
// default when unset or unparseable; Validate is the safety net for
// malformed input.
func (c ToolCallGovernanceConfig) ParsedTimeout() time.Duration {
	if c.Timeout == "" {
		d, _ := time.ParseDuration(DefaultToolCallGovernanceTimeout)
		return d
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		fallback, _ := time.ParseDuration(DefaultToolCallGovernanceTimeout)
		return fallback
	}
	return d
}

// Validate validates the tool-call governance configuration.
func (c ToolCallGovernanceConfig) Validate() error {
	switch c.Mode {
	case "", ToolCallGovernanceModeDisabled, ToolCallGovernanceModeAudit, ToolCallGovernanceModeEnforce:
		// ok
	default:
		return errs.WrapInvalid(
			fmt.Errorf("mode must be one of disabled, audit, enforce (got %q)", c.Mode),
			"ToolCallGovernanceConfig", "Validate", "check mode")
	}

	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return errs.WrapInvalid(err, "ToolCallGovernanceConfig", "Validate", "parse timeout")
		}
		if d <= 0 {
			return errs.WrapInvalid(
				fmt.Errorf("timeout must be positive (got %s)", c.Timeout),
				"ToolCallGovernanceConfig", "Validate", "check timeout value")
		}
	}
	return nil
}

// EnsureDefaults fills zero-valued fields with defaults. Called from
// Config-level defaulting at boot so an unset governance section has a
// stable Mode + Timeout instead of empty strings.
func (c *ToolCallGovernanceConfig) EnsureDefaults() {
	if c.Mode == "" {
		c.Mode = ToolCallGovernanceModeDisabled
	}
	if c.Timeout == "" {
		c.Timeout = DefaultToolCallGovernanceTimeout
	}
}

// ConsumerConfig holds JetStream consumer tuning for long-running ports.
// Deployments with slow LLMs (local Ollama, constrained GPUs) should increase
// ack_wait and heartbeat_interval to prevent premature redelivery.
type ConsumerConfig struct {
	AckWait           string `json:"ack_wait,omitempty" schema:"type:string,description:AckWait duration for long-running consumers (e.g. 90s or 5m),default:90s,category:advanced"`
	HeartbeatInterval string `json:"heartbeat_interval,omitempty" schema:"type:string,description:InProgress heartbeat interval (e.g. 60s or 2m). Must be less than ack_wait,default:60s,category:advanced"`
	MaxDeliver        int    `json:"max_deliver,omitempty" schema:"type:int,description:Maximum redelivery attempts for long-running consumers,default:2,min:1,max:10,category:advanced"`
}

// ContextConfig represents configuration for context memory management.
// Model context limits are resolved from the unified model registry (component.Dependencies.ModelRegistry).
type ContextConfig struct {
	Enabled          bool    `json:"enabled" description:"Deprecated: context management is always enabled (required for Gemini compatibility)"`
	CompactThreshold float64 `json:"compact_threshold" description:"Utilization threshold (0.01-1.0) that triggers context compaction"`
	HeadroomRatio    float64 `json:"headroom_ratio" description:"Fraction of model context to reserve for responses (0.0-0.5). Takes precedence over headroom_tokens when the computed value is larger"`
	HeadroomTokens   int     `json:"headroom_tokens" description:"Minimum token headroom floor — ratio-based headroom never goes below this value"`
	// Multi-agent context budget fields
	MaxBudgetTokens      int      `json:"max_budget_tokens,omitempty" description:"Hard token limit for context budget (overrides model limits when set)"`
	SliceOnBudget        bool     `json:"slice_on_budget,omitempty" description:"Enable context slicing when budget is exceeded"`
	PreserveEntities     []string `json:"preserve_entities,omitempty" description:"Entity IDs to always keep in context during slicing"`
	EntityPriority       int      `json:"entity_priority,omitempty" description:"Priority for entity context vs conversation (1-10, higher = more entity context)"`
	InjectProfileContext bool     `json:"inject_profile_context,omitempty" description:"Inject operating model profile context into system prompt when available"`
}

// Validate validates the configuration
func (c Config) Validate() error {
	// Validate max_iterations
	if c.MaxIterations <= 0 {
		return errs.WrapInvalid(fmt.Errorf("max_iterations must be greater than 0"), "Config", "Validate", "check max_iterations")
	}

	// Validate timeout
	if strings.TrimSpace(c.Timeout) == "" {
		return errs.WrapInvalid(fmt.Errorf("timeout is required"), "Config", "Validate", "check timeout")
	}

	// Parse timeout to ensure it's valid
	duration, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "parse timeout format")
	}

	// Ensure timeout is positive
	if duration <= 0 {
		return errs.WrapInvalid(fmt.Errorf("timeout must be positive"), "Config", "Validate", "check timeout value")
	}

	// Validate loops_bucket
	if strings.TrimSpace(c.LoopsBucket) == "" {
		return errs.WrapInvalid(fmt.Errorf("loops_bucket is required"), "Config", "Validate", "check loops_bucket")
	}

	// Validate trajectory_detail if set
	if c.TrajectoryDetail != "" && c.TrajectoryDetail != "summary" && c.TrajectoryDetail != "full" {
		return errs.WrapInvalid(fmt.Errorf("trajectory_detail must be 'summary' or 'full'"), "Config", "Validate", "check trajectory_detail")
	}

	// Validate approval timeout (empty is allowed — means wait forever)
	if strings.TrimSpace(c.ApprovalTimeoutStr) != "" {
		d, err := time.ParseDuration(c.ApprovalTimeoutStr)
		if err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "parse approval_timeout format")
		}
		if d < 0 {
			return errs.WrapInvalid(fmt.Errorf("approval_timeout must be non-negative"), "Config", "Validate", "check approval_timeout value")
		}
	}

	// Validate consumer config
	if err := c.Consumer.Validate(); err != nil {
		return err
	}

	// Validate tool-call governance config
	if err := c.ToolCallGovernance.Validate(); err != nil {
		return err
	}

	// Validate context config
	return c.Context.Validate()
}

// ApprovalTimeout returns the parsed duration for ApprovalTimeoutStr.
// Returns zero (wait indefinitely) when unset or unparseable; the
// validation step is the safety net for malformed input.
func (c Config) ApprovalTimeout() time.Duration {
	if c.ApprovalTimeoutStr == "" {
		return 0
	}
	d, err := time.ParseDuration(c.ApprovalTimeoutStr)
	if err != nil {
		return 0
	}
	return d
}

// Validate validates the context configuration.
// Context management is always enabled; the Enabled field is deprecated.
func (c ContextConfig) Validate() error {
	// Validate threshold: must be 0.01-1.0
	if c.CompactThreshold < 0.01 || c.CompactThreshold > 1.0 {
		return errs.WrapInvalid(fmt.Errorf("compact_threshold must be between 0.01 and 1.0"), "ContextConfig", "Validate", "check compact_threshold")
	}

	// Validate HeadroomRatio: must be 0.0-0.5 (reserving >50% defeats the purpose)
	if c.HeadroomRatio < 0 || c.HeadroomRatio > 0.5 {
		return errs.WrapInvalid(fmt.Errorf("headroom_ratio must be between 0.0 and 0.5"), "ContextConfig", "Validate", "check headroom_ratio")
	}

	// Validate HeadroomTokens (floor): must be >= 0
	if c.HeadroomTokens < 0 {
		return errs.WrapInvalid(fmt.Errorf("headroom_tokens must be non-negative"), "ContextConfig", "Validate", "check headroom_tokens")
	}

	return nil
}

// EnsureDefaults fills zero-valued fields with defaults.
// This is needed because json.Unmarshal overwrites nested structs even when
// the JSON contains zero values (e.g., "compact_threshold": 0).
func (c *ContextConfig) EnsureDefaults() {
	defaults := DefaultContextConfig()
	if c.CompactThreshold == 0 {
		c.CompactThreshold = defaults.CompactThreshold
	}
	if c.HeadroomRatio == 0 && c.HeadroomTokens == 0 {
		c.HeadroomRatio = defaults.HeadroomRatio
		c.HeadroomTokens = defaults.HeadroomTokens
	}
}

// Validate validates the consumer configuration.
func (c ConsumerConfig) Validate() error {
	if c.AckWait != "" {
		d, err := time.ParseDuration(c.AckWait)
		if err != nil {
			return errs.WrapInvalid(err, "ConsumerConfig", "Validate", "parse ack_wait")
		}
		if d < 10*time.Second {
			return errs.WrapInvalid(fmt.Errorf("ack_wait must be at least 10s"), "ConsumerConfig", "Validate", "check ack_wait")
		}
	}
	if c.HeartbeatInterval != "" {
		d, err := time.ParseDuration(c.HeartbeatInterval)
		if err != nil {
			return errs.WrapInvalid(err, "ConsumerConfig", "Validate", "parse heartbeat_interval")
		}
		if d < 5*time.Second {
			return errs.WrapInvalid(fmt.Errorf("heartbeat_interval must be at least 5s"), "ConsumerConfig", "Validate", "check heartbeat_interval")
		}
	}
	if c.MaxDeliver < 0 {
		return errs.WrapInvalid(fmt.Errorf("max_deliver must be non-negative"), "ConsumerConfig", "Validate", "check max_deliver")
	}
	return nil
}

// EnsureDefaults fills zero-valued fields with defaults.
func (c *ConsumerConfig) EnsureDefaults() {
	defaults := DefaultConsumerConfig()
	if c.AckWait == "" {
		c.AckWait = defaults.AckWait
	}
	if c.HeartbeatInterval == "" {
		c.HeartbeatInterval = defaults.HeartbeatInterval
	}
	if c.MaxDeliver == 0 {
		c.MaxDeliver = defaults.MaxDeliver
	}
}

// ParsedAckWait returns AckWait as a time.Duration. Caller must validate first.
func (c ConsumerConfig) ParsedAckWait() time.Duration {
	d, _ := time.ParseDuration(c.AckWait)
	return d
}

// ParsedHeartbeatInterval returns HeartbeatInterval as a time.Duration. Caller must validate first.
func (c ConsumerConfig) ParsedHeartbeatInterval() time.Duration {
	d, _ := time.ParseDuration(c.HeartbeatInterval)
	return d
}

// DefaultConsumerConfig returns the default consumer configuration.
// These defaults match the original hardcoded values.
func DefaultConsumerConfig() ConsumerConfig {
	return ConsumerConfig{
		AckWait:           "90s",
		HeartbeatInterval: "60s",
		MaxDeliver:        2,
	}
}

// DefaultContextConfig returns the default context configuration
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		Enabled:          true,
		CompactThreshold: 0.60,
		HeadroomRatio:    0.05,
		HeadroomTokens:   4000,
	}
}

// DefaultToolCallGovernanceConfig returns the default tool-call
// governance configuration: disabled mode (preserves pre-ADR-039
// behavior) with the framework default Timeout for when an operator
// later flips to enforce.
func DefaultToolCallGovernanceConfig() ToolCallGovernanceConfig {
	return ToolCallGovernanceConfig{
		Mode:    ToolCallGovernanceModeDisabled,
		Timeout: DefaultToolCallGovernanceTimeout,
	}
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		MaxIterations:      20,
		Timeout:            "120s",
		StreamName:         "AGENT",
		LoopsBucket:        "AGENT_LOOPS",
		ContentBucket:      "AGENT_CONTENT",
		ToolResultMaxBytes: 32768,
		TrajectoryDetail:   "summary",
		Consumer:           DefaultConsumerConfig(),
		Context:            DefaultContextConfig(),
		ToolCallGovernance: DefaultToolCallGovernanceConfig(),
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "agent.task",
					Type:        "jetstream",
					Subject:     "agent.task.*",
					StreamName:  "AGENT",
					Required:    true,
					Description: "Agent task requests (JetStream)",
				},
				{
					Name:        "agent.response",
					Type:        "jetstream",
					Subject:     "agent.response.>",
					StreamName:  "AGENT",
					Required:    true,
					Description: "Agent model responses (JetStream)",
				},
				{
					Name:        "tool.result",
					Type:        "jetstream",
					Subject:     "tool.result.>",
					StreamName:  "AGENT",
					Required:    true,
					Description: "Tool execution results (JetStream)",
				},
				{
					Name:        "agent.signal",
					Type:        "jetstream",
					Subject:     "agent.signal.*",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Control signals for loops (cancel, pause, etc.)",
				},
				{
					Name:        "agent.approval_response",
					Type:        "jetstream",
					Subject:     "agent.approval_response.*",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Human approval responses for gated tool calls",
				},
				{
					Name:        "agent.toolcall.approved",
					Type:        "jetstream",
					Subject:     "agent.toolcall.approved.>",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Approve verdicts from rule-driven tool-call governance (ADR-039). Wildcard subscription; demuxed per-call by trailing path segments.",
				},
				{
					Name:        "agent.toolcall.rejected",
					Type:        "jetstream",
					Subject:     "agent.toolcall.rejected.>",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Reject verdicts from rule-driven tool-call governance (ADR-039). Wildcard subscription; demuxed per-call by trailing path segments.",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "agent.request",
					Type:        "jetstream",
					Subject:     "agent.request.*",
					StreamName:  "AGENT",
					Description: "Agent model requests (JetStream)",
				},
				{
					Name:        "tool.execute",
					Type:        "jetstream",
					Subject:     "tool.execute.*",
					StreamName:  "AGENT",
					Description: "Tool execution requests (JetStream)",
				},
				{
					Name:        "agent.complete",
					Type:        "jetstream",
					Subject:     "agent.complete.*",
					StreamName:  "AGENT",
					Description: "Agent task completions (JetStream)",
				},
				{
					Name:        "agent.created",
					Type:        "jetstream",
					Subject:     "agent.created.*",
					StreamName:  "AGENT",
					Description: "Loop-created lifecycle events (JetStream)",
				},
				{
					Name:        "agent.failed",
					Type:        "jetstream",
					Subject:     "agent.failed.*",
					StreamName:  "AGENT",
					Description: "Loop-failed lifecycle events (JetStream)",
				},
				{
					Name:        "agent.context.compaction",
					Type:        "jetstream",
					Subject:     "agent.context.compaction.*",
					StreamName:  "AGENT",
					Description: "Context compaction events (JetStream)",
				},
				{
					Name:        "agent.approval_pending",
					Type:        "jetstream",
					Subject:     "agent.approval_pending.*",
					StreamName:  "AGENT",
					Description: "Tool calls awaiting human approval (JetStream)",
				},
				{
					Name:        "agent.toolcall.proposed",
					Type:        "jetstream",
					Subject:     "agent.toolcall.proposed.*",
					StreamName:  "AGENT",
					Description: "Proposed tool calls awaiting rule-driven governance verdict (ADR-039). Emitted in audit and enforce modes.",
				},
			},
			KVWrite: []component.PortDefinition{
				{
					Name:        "loops",
					Type:        "kv-write",
					Bucket:      "AGENT_LOOPS",
					Description: "Loop state storage",
				},
			},
		},
	}
}
