package researchroute

import (
	"errors"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

// ComponentName is the canonical registry name + log subsystem.
const ComponentName = "research-graph-route"

// Default knobs surfaced as exported constants so the prompt-builder
// tests and operator docs can reference them by name rather than
// duplicating literals.
const (
	// DefaultRouteTimeout caps the structured-emit LLM call. Generous
	// enough for slower self-hosted endpoints; operators can tighten
	// for faster hosted models.
	DefaultRouteTimeout = 30 * time.Second

	// DefaultMaxResponseTokens caps the LLM's response budget. The
	// JSON-shaped output is small (action + args + rationale); 512 is
	// well above the worst case for the four action shapes.
	DefaultMaxResponseTokens = 512

	// DefaultMaxCandidatesInPrompt caps how many ClassifierOutput
	// candidates the prompt embeds. The router doesn't need the full
	// candidate list to decide — the top-N by relevance is enough —
	// and a tight cap keeps the prompt fitting frontier-tier and
	// small-model context windows alike.
	DefaultMaxCandidatesInPrompt = 10
)

// Config holds operator-tunable knobs for the route_search component.
//
// LLM wiring follows the same model-registry capability seam as
// processor/research-graph-classify: operator declares
// CapabilityResearchRouting in the model registry; the component
// resolves it at Start() time. Absence is a startup error — unlike
// the optional LLM tier on nl_classify, route_search has no
// keyword-only fallback (its entire job is the LLM judgment).
type Config struct {
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration. route_search requires one nats input subscribing to component.route_search.> ,category:basic"`

	LoopsBucket string `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket holding research-pipeline loops and trigger/completion keys,default:AGENT_LOOPS,category:advanced"`

	RouteTimeout time.Duration `json:"route_timeout,omitempty" schema:"type:duration,description:Timeout for the structured-emit LLM call. Honors capability.research_routing override when set on the model registry,default:30s,category:advanced"`

	MaxResponseTokens int `json:"max_response_tokens,omitempty" schema:"type:int,description:Cap on LLM response tokens. The JSON-shaped output is small; 512 covers the worst case across the four action shapes,default:512,max:2048,category:advanced"`

	MaxCandidatesInPrompt int `json:"max_candidates_in_prompt,omitempty" schema:"type:int,description:Cap on number of ClassifierOutput candidates embedded in the router prompt. Top-N by relevance; tighter caps keep prompts within small-model context windows. 0 means default (10),default:10,max:50,category:advanced"`
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
	if c.MaxCandidatesInPrompt < 0 {
		return errors.New("max_candidates_in_prompt must be >= 0")
	}
	return nil
}

// ApplyDefaults fills in defaults for unset fields.
func (c *Config) ApplyDefaults() {
	if c.LoopsBucket == "" {
		c.LoopsBucket = "AGENT_LOOPS"
	}
	if c.RouteTimeout == 0 {
		c.RouteTimeout = DefaultRouteTimeout
	}
	if c.MaxResponseTokens == 0 {
		c.MaxResponseTokens = DefaultMaxResponseTokens
	}
	if c.MaxCandidatesInPrompt == 0 {
		c.MaxCandidatesInPrompt = DefaultMaxCandidatesInPrompt
	}
}

// DefaultConfig returns a default Config skeleton with the standard
// route_search input port.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "trigger",
					Type:        "nats",
					Subject:     "component.route_search.>",
					Required:    true,
					Description: "R1's publish target. Subject suffix carries the research-pipeline loop_id.",
				},
			},
			Outputs: []component.PortDefinition{{
				Name: "graph_mutations", Type: "nats-request", Subject: graphmutation.SubjectFamily,
				Interface: graphmutation.InterfaceType, Required: true,
			}},
		},
		LoopsBucket:           "AGENT_LOOPS",
		RouteTimeout:          DefaultRouteTimeout,
		MaxResponseTokens:     DefaultMaxResponseTokens,
		MaxCandidatesInPrompt: DefaultMaxCandidatesInPrompt,
	}
}

// configSchema is the static configuration schema, generated from
// Config struct tags at package init.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))
