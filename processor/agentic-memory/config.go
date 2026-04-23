package agenticmemory

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config holds configuration for agentic-memory processor component
type Config struct {
	Extraction         ExtractionConfig      `json:"extraction" schema:"type:object,description:Fact extraction configuration,category:basic"`
	Hydration          HydrationConfig       `json:"hydration" schema:"type:object,description:Context hydration configuration,category:basic"`
	Checkpoint         CheckpointConfig      `json:"checkpoint" schema:"type:object,description:Memory checkpoint configuration,category:basic"`
	Ports              *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
	StreamName         string                `json:"stream_name,omitempty" schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
	ConsumerNameSuffix string                `json:"consumer_name_suffix,omitempty" schema:"type:string,description:Consumer name suffix for uniqueness,category:advanced"`
}

// ExtractionConfig holds fact extraction configuration
type ExtractionConfig struct {
	LLMAssisted LLMAssistedConfig `json:"llm_assisted" schema:"type:object,description:LLM-assisted extraction configuration,category:basic"`
}

// LLMAssistedConfig holds LLM-assisted extraction settings
type LLMAssistedConfig struct {
	Enabled                  bool    `json:"enabled" schema:"type:bool,description:Enable LLM-assisted fact extraction,category:basic,default:true"`
	Model                    string  `json:"model" schema:"type:string,description:Model alias for extraction,category:basic,default:fast"`
	TriggerIterationInterval int     `json:"trigger_iteration_interval" schema:"type:int,description:Trigger extraction every N iterations,category:advanced,default:5"`
	TriggerContextThreshold  float64 `json:"trigger_context_threshold" schema:"type:float,description:Trigger when context exceeds threshold (0.0-1.0),category:advanced,default:0.8"`
	MaxTokens                int     `json:"max_tokens" schema:"type:int,description:Maximum tokens for extraction request,category:advanced,default:1000"`
}

// HydrationConfig holds context hydration configuration
type HydrationConfig struct {
	PreTask        PreTaskConfig        `json:"pre_task" schema:"type:object,description:Pre-task hydration configuration,category:basic"`
	PostCompaction PostCompactionConfig `json:"post_compaction" schema:"type:object,description:Post-compaction hydration configuration,category:basic"`
}

// PreTaskConfig holds pre-task hydration settings
type PreTaskConfig struct {
	Enabled          bool `json:"enabled" schema:"type:bool,description:Enable pre-task context hydration,category:basic,default:true"`
	MaxContextTokens int  `json:"max_context_tokens" schema:"type:int,description:Maximum tokens for pre-task context,category:advanced,default:2000"`
	IncludeDecisions bool `json:"include_decisions" schema:"type:bool,description:Include past decisions in context,category:advanced,default:true"`
	IncludeFiles     bool `json:"include_files" schema:"type:bool,description:Include file context,category:advanced,default:true"`
}

// PostCompactionConfig holds post-compaction hydration settings
type PostCompactionConfig struct {
	Enabled                   bool `json:"enabled" schema:"type:bool,description:Enable post-compaction reconstruction,category:basic,default:true"`
	ReconstructFromCheckpoint bool `json:"reconstruct_from_checkpoint" schema:"type:bool,description:Reconstruct from checkpoint after compaction,category:advanced,default:true"`
	MaxRecoveryTokens         int  `json:"max_recovery_tokens" schema:"type:int,description:Maximum tokens for recovery context,category:advanced,default:1500"`
}

// CheckpointConfig holds memory checkpoint configuration
type CheckpointConfig struct {
	Enabled       bool   `json:"enabled" schema:"type:bool,description:Enable memory checkpointing,category:basic,default:true"`
	StorageBucket string `json:"storage_bucket" schema:"type:string,description:NATS KV bucket for checkpoints,category:basic,default:AGENT_MEMORY_CHECKPOINTS"`
	RetentionDays int    `json:"retention_days" schema:"type:int,description:Checkpoint retention days,category:advanced,default:7"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	// Validate extraction config only if enabled
	if c.Extraction.LLMAssisted.Enabled {
		if err := c.Extraction.LLMAssisted.Validate(); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "validate extraction config")
		}
	}

	// Validate hydration config
	if c.Hydration.PreTask.Enabled {
		if err := c.Hydration.PreTask.Validate(); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "validate hydration.pre_task")
		}
	}

	if c.Hydration.PostCompaction.Enabled {
		if err := c.Hydration.PostCompaction.Validate(); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "validate hydration.post_compaction")
		}
	}

	// Validate checkpoint config only if enabled
	if c.Checkpoint.Enabled {
		if err := c.Checkpoint.Validate(); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "validate checkpoint config")
		}
	}

	return nil
}

// Validate checks the LLM-assisted extraction configuration
func (l *LLMAssistedConfig) Validate() error {
	// Check model first
	if l.Model == "" {
		return errs.WrapInvalid(
			fmt.Errorf("model cannot be empty when extraction is enabled"),
			"LLMAssistedConfig",
			"Validate",
			"validate model",
		)
	}

	// Check each field independently (order doesn't matter for error reporting)
	if l.MaxTokens < 0 {
		return errs.WrapInvalid(
			fmt.Errorf("max_tokens cannot be negative"),
			"LLMAssistedConfig",
			"Validate",
			"validate max_tokens",
		)
	}

	if l.TriggerContextThreshold < 0.0 || l.TriggerContextThreshold > 1.0 {
		return errs.WrapInvalid(
			fmt.Errorf("trigger_context_threshold must be between 0.0 and 1.0"),
			"LLMAssistedConfig",
			"Validate",
			"validate trigger_context_threshold",
		)
	}

	if l.TriggerIterationInterval <= 0 {
		return errs.WrapInvalid(
			fmt.Errorf("trigger_iteration_interval must be greater than 0"),
			"LLMAssistedConfig",
			"Validate",
			"validate trigger_iteration_interval",
		)
	}

	return nil
}

// Validate checks the pre-task hydration configuration
func (p *PreTaskConfig) Validate() error {
	if p.MaxContextTokens < 0 {
		return errs.WrapInvalid(
			fmt.Errorf("max_context_tokens cannot be negative"),
			"PreTaskConfig",
			"Validate",
			"validate max_context_tokens",
		)
	}

	return nil
}

// Validate checks the post-compaction hydration configuration
func (p *PostCompactionConfig) Validate() error {
	if p.MaxRecoveryTokens < 0 {
		return errs.WrapInvalid(
			fmt.Errorf("max_recovery_tokens cannot be negative"),
			"PostCompactionConfig",
			"Validate",
			"validate max_recovery_tokens",
		)
	}

	return nil
}

// Validate checks the checkpoint configuration
func (c *CheckpointConfig) Validate() error {
	if c.StorageBucket == "" {
		return errs.WrapInvalid(
			fmt.Errorf("storage_bucket cannot be empty when checkpointing is enabled"),
			"CheckpointConfig",
			"Validate",
			"validate storage_bucket",
		)
	}

	if c.RetentionDays <= 0 {
		return errs.WrapInvalid(
			fmt.Errorf("retention_days must be greater than 0"),
			"CheckpointConfig",
			"Validate",
			"validate retention_days",
		)
	}

	return nil
}

// DefaultConfig returns default configuration for agentic-memory processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "compaction_events",
			Type:        "jetstream",
			Subject:     "agent.context.compaction.>",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Compaction events input (JetStream)",
		},
		{
			Name:        "hydrate_requests",
			Type:        "jetstream",
			Subject:     "memory.hydrate.request.*",
			StreamName:  "AGENT",
			Required:    false,
			Description: "Hydration request input (JetStream)",
		},
		{
			Name:        "entity_states",
			Type:        "kv-watch",
			Bucket:      "ENTITY_STATES",
			Required:    false,
			Description: "Entity state changes (KV Watch)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "injected_context",
			Type:        "jetstream",
			Subject:     "agent.context.injected.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Injected context output (JetStream)",
		},
		{
			Name:        "graph_mutations",
			Type:        "nats",
			Subject:     "graph.mutation.*",
			Required:    true,
			Description: "Graph mutation commands (NATS)",
		},
		{
			Name:        "checkpoint_events",
			Type:        "nats",
			Subject:     "memory.checkpoint.created.*",
			Required:    true,
			Description: "Checkpoint event notifications (NATS)",
		},
	}

	return Config{
		Extraction: ExtractionConfig{
			LLMAssisted: LLMAssistedConfig{
				Enabled:                  true,
				Model:                    "fast",
				TriggerIterationInterval: 5,
				TriggerContextThreshold:  0.8,
				MaxTokens:                1000,
			},
		},
		Hydration: HydrationConfig{
			PreTask: PreTaskConfig{
				Enabled:          true,
				MaxContextTokens: 2000,
				IncludeDecisions: true,
				IncludeFiles:     true,
			},
			PostCompaction: PostCompactionConfig{
				Enabled:                   true,
				ReconstructFromCheckpoint: true,
				MaxRecoveryTokens:         1500,
			},
		},
		Checkpoint: CheckpointConfig{
			Enabled:       true,
			StorageBucket: "AGENT_MEMORY_CHECKPOINTS",
			RetentionDays: 7,
		},
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
	}
}
