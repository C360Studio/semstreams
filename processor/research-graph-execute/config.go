package researchexecute

import (
	"errors"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

// ComponentName is the canonical registry name + log subsystem.
const ComponentName = "research-graph-execute"

// Default knobs surfaced as exported constants so the materializer
// + fan-out tests and operator docs can reference them by name
// rather than duplicating literals.
const (
	// DefaultExecuteTimeout caps the wall-clock for the full
	// fan-out (all sub-queries × all tiers). Generous default; per-
	// tier deadlines are derived as fractions of this cap.
	DefaultExecuteTimeout = 60 * time.Second

	// DefaultMaxParallelism caps concurrent sub-query execution.
	// Keeps fan-out from saturating the responding gateways under
	// a wide-decompose scenario; operators can raise for staging
	// environments with more capacity.
	DefaultMaxParallelism = 8

	// DefaultMaxResultsPerSubquery caps per-sub-query result count
	// before ranking + budget enforcement run. Prevents a single
	// runaway sub-query from dominating the evidence array.
	DefaultMaxResultsPerSubquery = 50

	// DefaultBudgetTokens is the fallback evidence-budget when the
	// upstream Intent doesn't supply one. Should never normally
	// fire (Intent.ResolvedBudgetTokens supplies a default of 4000
	// per agentic/research) but acts as a safety net.
	DefaultBudgetTokens = 4000

	// DefaultPerSubqueryTimeoutFraction is the fraction of the
	// overall ExecuteTimeout each sub-query gets as its per-call
	// deadline. 0.5 leaves slack for fan-out overhead and ranking;
	// operators can tighten.
	DefaultPerSubqueryTimeoutFraction = 0.5
)

// Config holds operator-tunable knobs for the execute_subqueries
// component.
type Config struct {
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration. execute_subqueries requires one nats input subscribing to component.execute_subqueries.> ,category:basic"`

	LoopsBucket string `json:"loops_bucket,omitempty" schema:"type:string,description:NATS KV bucket holding research-pipeline loops and trigger/completion keys,default:AGENT_LOOPS,category:advanced"`

	ExecuteTimeout time.Duration `json:"execute_timeout,omitempty" schema:"type:duration,description:Wall-clock cap for the full fan-out (all sub-queries x all tiers),default:60s,category:advanced"`

	MaxParallelism int `json:"max_parallelism,omitempty" schema:"type:int,description:Concurrent sub-query execution cap. Keeps fan-out from saturating gateways. 0 means default (8),default:8,max:64,category:advanced"`

	MaxResultsPerSubquery int `json:"max_results_per_subquery,omitempty" schema:"type:int,description:Per-sub-query result count cap before ranking + budget enforcement. Prevents a runaway sub-query from dominating evidence. 0 means default (50),default:50,max:500,category:advanced"`
}

// Validate validates the configuration. Negative caps are rejected;
// zero values fall through to ApplyDefaults.
func (c *Config) Validate() error {
	if c.Ports == nil || len(c.Ports.Inputs) == 0 {
		return errors.New("ports configuration with at least one input port is required")
	}
	if c.MaxParallelism < 0 {
		return errors.New("max_parallelism must be >= 0")
	}
	if c.MaxResultsPerSubquery < 0 {
		return errors.New("max_results_per_subquery must be >= 0")
	}
	return nil
}

// ApplyDefaults fills in defaults for unset fields.
func (c *Config) ApplyDefaults() {
	if c.LoopsBucket == "" {
		c.LoopsBucket = "AGENT_LOOPS"
	}
	if c.ExecuteTimeout == 0 {
		c.ExecuteTimeout = DefaultExecuteTimeout
	}
	if c.MaxParallelism == 0 {
		c.MaxParallelism = DefaultMaxParallelism
	}
	if c.MaxResultsPerSubquery == 0 {
		c.MaxResultsPerSubquery = DefaultMaxResultsPerSubquery
	}
}

// DefaultConfig returns a default Config skeleton with the standard
// execute_subqueries input port.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "trigger",
					Type:        "nats",
					Subject:     "component.execute_subqueries.>",
					Required:    true,
					Description: "R2's publish target. Subject suffix carries the research-pipeline loop_id.",
				},
			},
			Outputs: []component.PortDefinition{{
				Name: "graph_mutations", Type: "nats-request", Subject: graphmutation.SubjectFamily,
				Interface: graphmutation.InterfaceType, Required: true,
			}},
		},
		LoopsBucket:           "AGENT_LOOPS",
		ExecuteTimeout:        DefaultExecuteTimeout,
		MaxParallelism:        DefaultMaxParallelism,
		MaxResultsPerSubquery: DefaultMaxResultsPerSubquery,
	}
}

// configSchema is the static configuration schema, generated from
// Config struct tags at package init.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))
