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
	EnableCategories     bool                  `json:"enable_categories,omitempty" schema:"type:bool,description:Enable tool category filtering for role-based access,category:advanced,default:false"`
	LoopsBucket          string                `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket name holding agent loop state (for read_loop_result),default:AGENT_LOOPS,category:advanced"`
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

	// AllowedTools can be nil or empty (both mean allow all tools)
	// No validation needed for allowed_tools

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
