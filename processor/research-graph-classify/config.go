package researchclassify

import (
	"errors"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

// ComponentName is the canonical registry name + log subsystem.
const ComponentName = "research-graph-classify"

// Config holds operator-tunable knobs for the nl_classify component.
//
// LLM classifier wiring follows the same model-registry capability
// seam as processor/graph-query: the operator declares
// CapabilityQueryClassification in the model registry; the component
// resolves it at Start() time. Absence is a clean disable (keyword-
// only chain) rather than a startup error — same shape as
// graph-query's initLLMClassifier.
type Config struct {
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration. nl_classify requires one nats input subscribing to component.nl_classify.> ,category:basic"`

	LoopsBucket string `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket name holding research-pipeline loops and trigger/completion keys,default:AGENT_LOOPS,category:advanced"`

	ClassifyTimeout time.Duration `json:"classify_timeout,omitempty" schema:"type:duration,description:Timeout for the classifier chain (+ optional LLM tier). Honors capability.query_classification override when set on the model registry,default:30s,category:advanced"`

	RetrieveTimeout time.Duration `json:"retrieve_timeout,omitempty" schema:"type:duration,description:Timeout for the downstream graph.query.searchGraph request that fetches initial candidate entities,default:30s,category:advanced"`

	MaxCandidates int `json:"max_candidates,omitempty" schema:"type:int,description:Cap on number of candidate entities to surface from the search_graph response (latency vs route_search prompt budget). 0 means default (25). ,default:25,max:200,category:advanced"`

	// EnableLLMClassifier toggles wiring the T3 LLM tier into the
	// classifier chain when a query_classification capability is
	// configured in the model registry. Default false matches PR 2's
	// "thinnest viable" stance — operators opt in once they've
	// observed which topics the keyword tier handles.
	EnableLLMClassifier bool `json:"enable_llm_classifier,omitempty" schema:"type:bool,description:When true and a query_classification capability is configured on the model registry, wire the LLM tier into the classifier chain,default:false,category:advanced"`
}

// Validate validates the configuration. MaxCandidates < 0 is
// rejected; 0 falls through to ApplyDefaults (treated as "use
// default"). The schema tag intentionally omits `min:1` so JSON-
// omitted MaxCandidates keys round-trip clean through validation.
func (c *Config) Validate() error {
	if c.Ports == nil || len(c.Ports.Inputs) == 0 {
		return errors.New("ports configuration with at least one input port is required")
	}
	if c.MaxCandidates < 0 {
		return errors.New("max_candidates must be >= 0")
	}
	return nil
}

// ApplyDefaults fills in defaults for unset fields.
func (c *Config) ApplyDefaults() {
	if c.LoopsBucket == "" {
		c.LoopsBucket = "AGENT_LOOPS"
	}
	if c.ClassifyTimeout == 0 {
		c.ClassifyTimeout = 30 * time.Second
	}
	if c.RetrieveTimeout == 0 {
		c.RetrieveTimeout = 30 * time.Second
	}
	if c.MaxCandidates == 0 {
		c.MaxCandidates = 25
	}
}

// DefaultConfig returns a default Config skeleton with the standard
// nl_classify input port. Operators typically copy this and add a
// chain-specific port name override.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "trigger", Config: component.NATSPort{Subject: "component.nl_classify.>"}, Required: true,
					Description: "R0's publish target. Subject suffix carries the research-pipeline loop_id.",
				},
			},
			Outputs: []component.PortDefinition{{
				Name: "graph_mutations", Config: component.NATSRequestPort{Subject: graphmutation.SubjectFamily, Interface: &component.InterfaceContract{Type: graphmutation.InterfaceType}}, Required: true,
			}},
		},
		LoopsBucket:         "AGENT_LOOPS",
		ClassifyTimeout:     30 * time.Second,
		RetrieveTimeout:     30 * time.Second,
		MaxCandidates:       25,
		EnableLLMClassifier: false,
	}
}

// configSchema is the static configuration schema, generated from
// Config struct tags at package init.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))
