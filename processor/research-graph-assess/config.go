package researchassess

import (
	"errors"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

// ComponentName is the canonical registry name + log subsystem.
const ComponentName = "research-graph-assess"

// Default knobs surfaced as exported constants so the prompt-builder
// tests and operator docs can reference them by name rather than
// duplicating literals.
const (
	// DefaultAssessTimeout caps the structured-emit LLM call.
	// Generous enough for slower self-hosted endpoints; operators can
	// tighten for faster hosted models.
	DefaultAssessTimeout = 30 * time.Second

	// DefaultMaxResponseTokens caps the LLM's response budget. The
	// JSON-shaped output is small (decision + ~5 refined queries +
	// rationale); 1024 covers the worst case across both decision
	// paths.
	DefaultMaxResponseTokens = 1024

	// DefaultMaxEvidenceInPrompt caps how many ExecutionOutput
	// evidence items the prompt embeds. The assessor doesn't need
	// the full evidence array to decide — the top-N by score is
	// enough — and a tight cap keeps the prompt fitting frontier-tier
	// and small-model context windows alike.
	DefaultMaxEvidenceInPrompt = 20

	// DefaultMaxSnippetCharsInPrompt caps how many SnippetText
	// characters render per evidence item in the prompt. Per-item
	// rendering past this length truncates with an ellipsis. Keeps
	// the prompt bounded when individual SnippetText fields are
	// long.
	DefaultMaxSnippetCharsInPrompt = 280
)

// Config holds operator-tunable knobs for the assess_sufficiency
// component.
//
// LLM wiring follows the same model-registry capability seam as
// route_search: operator declares CapabilityResearchAssessment in the
// model registry; the component resolves it at Start() time. Absence
// is a startup error — assess_sufficiency has no keyword-only
// fallback (its entire job is the LLM judgment).
type Config struct {
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration. assess_sufficiency requires one nats input subscribing to component.assess_sufficiency.> ,category:basic"`

	LoopsBucket string `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket holding research-pipeline loops and trigger/completion keys,default:AGENT_LOOPS,category:advanced"`

	AssessTimeout time.Duration `json:"assess_timeout,omitempty" schema:"type:duration,description:Timeout for the structured-emit LLM call. Honors capability.research_assessment override when set on the model registry,default:30s,category:advanced"`

	MaxResponseTokens int `json:"max_response_tokens,omitempty" schema:"type:int,description:Cap on LLM response tokens. The JSON-shaped output is small; 1024 covers the worst case across decision paths,default:1024,max:4096,category:advanced"`

	MaxEvidenceInPrompt int `json:"max_evidence_in_prompt,omitempty" schema:"type:int,description:Cap on number of ExecutionOutput evidence items embedded in the assessor prompt. Top-N by Score; tighter caps keep prompts within small-model context windows. 0 means default (20),default:20,max:100,category:advanced"`

	MaxSnippetCharsInPrompt int `json:"max_snippet_chars_in_prompt,omitempty" schema:"type:int,description:Cap on per-evidence SnippetText characters rendered in the assessor prompt. 0 means default (280),default:280,max:2000,category:advanced"`
}

// Validate validates the configuration. Negative caps are rejected;
// zero values fall through to ApplyDefaults (treated as "use
// default").
func (c *Config) Validate() error {
	if c.Ports == nil || len(c.Ports.Inputs) == 0 {
		return errors.New("ports configuration with at least one input port is required")
	}
	if c.MaxResponseTokens < 0 {
		return errors.New("max_response_tokens must be >= 0")
	}
	if c.MaxEvidenceInPrompt < 0 {
		return errors.New("max_evidence_in_prompt must be >= 0")
	}
	if c.MaxSnippetCharsInPrompt < 0 {
		return errors.New("max_snippet_chars_in_prompt must be >= 0")
	}
	return nil
}

// ApplyDefaults fills in defaults for unset fields.
func (c *Config) ApplyDefaults() {
	if c.LoopsBucket == "" {
		c.LoopsBucket = "AGENT_LOOPS"
	}
	if c.AssessTimeout == 0 {
		c.AssessTimeout = DefaultAssessTimeout
	}
	if c.MaxResponseTokens == 0 {
		c.MaxResponseTokens = DefaultMaxResponseTokens
	}
	if c.MaxEvidenceInPrompt == 0 {
		c.MaxEvidenceInPrompt = DefaultMaxEvidenceInPrompt
	}
	if c.MaxSnippetCharsInPrompt == 0 {
		c.MaxSnippetCharsInPrompt = DefaultMaxSnippetCharsInPrompt
	}
}

// DefaultConfig returns a default Config skeleton with the standard
// assess_sufficiency input port.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "trigger", Config: component.NATSPort{Subject: "component.assess_sufficiency.>"}, Required: true,
					Description: "R3's publish target. Subject suffix carries the research-pipeline loop_id.",
				},
			},
			Outputs: []component.PortDefinition{{
				Name: "graph_mutations", Config: component.NATSRequestPort{Subject: graphmutation.SubjectFamily, Interface: &component.InterfaceContract{Type: graphmutation.InterfaceType, Version: graphmutation.InterfaceVersion}}, Required: true,
			}},
		},
		LoopsBucket:             "AGENT_LOOPS",
		AssessTimeout:           DefaultAssessTimeout,
		MaxResponseTokens:       DefaultMaxResponseTokens,
		MaxEvidenceInPrompt:     DefaultMaxEvidenceInPrompt,
		MaxSnippetCharsInPrompt: DefaultMaxSnippetCharsInPrompt,
	}
}

// configSchema is the static configuration schema, generated from
// Config struct tags at package init.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))
