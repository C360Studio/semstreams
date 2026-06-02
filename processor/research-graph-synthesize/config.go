package researchsynthesize

import (
	"errors"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// ComponentName is the canonical registry name + log subsystem.
const ComponentName = "research-graph-synthesize"

// Default knobs surfaced as exported constants so the prompt-builder
// tests and operator docs can reference them by name rather than
// duplicating literals.
const (
	// DefaultSynthesizeTimeout caps the structured-emit LLM call.
	// Set to 30s because natsclient.Client.Subscribe wraps the
	// per-message handler in a 30-second timeout context
	// (natsclient/client.go ~line 684) — any synthesize_timeout
	// above 30s is silently clipped by the subscription wrapper
	// before reaching the LLM call. Operators tuning past 30s
	// should raise the natsclient cap; the component-side knob
	// alone won't reach the wire.
	DefaultSynthesizeTimeout = 30 * time.Second

	// DefaultMaxResponseTokens caps the LLM's response budget. The
	// synthesis text can be long; 2048 is the production default,
	// operators can raise via capability.research_synthesis.
	DefaultMaxResponseTokens = 2048

	// DefaultMaxEvidenceInPrompt caps how many ExecutionOutput
	// evidence items the prompt embeds. Higher than assess's cap
	// because synthesis needs to ground its prose in concrete
	// references — but still bounded so the prompt fits common
	// context windows.
	DefaultMaxEvidenceInPrompt = 30

	// DefaultMaxSnippetCharsInPrompt caps per-evidence SnippetText
	// chars rendered in the prompt. Higher than assess because
	// synthesis quotes snippets directly into the answer; truncated
	// snippets shrink the answer's grounding.
	DefaultMaxSnippetCharsInPrompt = 480
)

// Config holds operator-tunable knobs for the synthesize_answer
// component.
//
// LLM wiring follows the same model-registry capability seam as
// route_search + assess_sufficiency: operator declares
// CapabilityResearchSynthesis in the model registry; the component
// resolves it at Start() time. Absence is a startup error.
type Config struct {
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration. synthesize_answer requires one nats input subscribing to component.synthesize_answer.> ,category:basic"`

	LoopsBucket string `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket holding research-pipeline loops and trigger/completion keys,default:AGENT_LOOPS,category:advanced"`

	SynthesizeTimeout time.Duration `json:"synthesize_timeout,omitempty" schema:"type:duration,description:Timeout for the structured-emit LLM call. Honors capability.research_synthesis override when set on the model registry. NOTE: natsclient.Subscribe caps per-message context at 30s; values above 30s are silently clipped at the wire,default:30s,category:advanced"`

	MaxResponseTokens int `json:"max_response_tokens,omitempty" schema:"type:int,description:Cap on LLM response tokens for the synthesis output,default:2048,max:8192,category:advanced"`

	MaxEvidenceInPrompt int `json:"max_evidence_in_prompt,omitempty" schema:"type:int,description:Cap on number of ExecutionOutput evidence items embedded in the synthesizer prompt. Top-N by Score; tighter caps keep prompts within small-model context windows. 0 means default (30),default:30,max:200,category:advanced"`

	MaxSnippetCharsInPrompt int `json:"max_snippet_chars_in_prompt,omitempty" schema:"type:int,description:Cap on per-evidence SnippetText characters rendered in the synthesizer prompt. 0 means default (480),default:480,max:4000,category:advanced"`
}

// Validate validates the configuration. Negative caps are rejected;
// zero values fall through to ApplyDefaults.
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
	if c.SynthesizeTimeout == 0 {
		c.SynthesizeTimeout = DefaultSynthesizeTimeout
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
// synthesize_answer input port.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "trigger",
					Type:        "nats",
					Subject:     "component.synthesize_answer.>",
					Required:    true,
					Description: "R3's synthesize branch publish target. Subject suffix carries the research-pipeline loop_id.",
				},
			},
			Outputs: []component.PortDefinition{},
		},
		LoopsBucket:             "AGENT_LOOPS",
		SynthesizeTimeout:       DefaultSynthesizeTimeout,
		MaxResponseTokens:       DefaultMaxResponseTokens,
		MaxEvidenceInPrompt:     DefaultMaxEvidenceInPrompt,
		MaxSnippetCharsInPrompt: DefaultMaxSnippetCharsInPrompt,
	}
}

// natsclientSubscribeTimeoutCap documents the upstream constraint:
// natsclient.Client.Subscribe wraps the per-message handler in a
// 30-second context.WithTimeout. SynthesizeTimeout values above
// this cap are silently clipped — raising the cap is a natsclient
// architectural change tracked separately.
const natsclientSubscribeTimeoutCap = 30 * time.Second

// configSchema is the static configuration schema, generated from
// Config struct tags at package init.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))
