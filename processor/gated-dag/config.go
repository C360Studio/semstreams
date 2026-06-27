package gateddagexec

import (
	"fmt"
	"time"
)

// Default predicate vocabulary. Consumers may override any of these; the
// framework defaults give a working out-of-the-box configuration.
const (
	defaultCompletedPredicate = "gateddag.completed"
	defaultFailedPredicate    = "gateddag.failed"
	defaultDirtiedPredicate   = "gateddag.dirtied"
	defaultDependsOnPredicate = "gateddag.depends_on"
	defaultClaimPredicate     = "gateddag.claim"

	defaultWorkers          = 4
	defaultQueueSize        = 256
	defaultBackstopInterval = "30s"
	defaultQueryTimeout     = "30s"
	defaultMaxUnits         = 1000

	// FailurePolicyContinueOthers keeps independent branches flowing when a unit
	// fails (its dependents stay Blocked; everything else proceeds). Default.
	FailurePolicyContinueOthers = "continue_others"
	// FailurePolicyStopOnFirstFailure halts all new dispatch once any unit has
	// failed (in-flight units still finish). A blunt circuit-breaker.
	FailurePolicyStopOnFirstFailure = "stop_on_first_failure"
)

// Config parameterizes the gated-DAG executor. The zero value is NOT valid —
// NewComponent applies DefaultConfig for unset fields then calls Validate.
type Config struct {
	// FanOutWorkflow is the lifecycle.Workflow.Name this executor watches for
	// re-eval triggers. Defaults to the framework FanOut workflow; a consumer
	// may point it at its own registered workflow. The executor self-registers
	// the framework default when this is left at the default (see component.go).
	FanOutWorkflow string `json:"fan_out_workflow,omitempty"`

	// UnitEntityPrefix is the graph.query.prefix scope read authoritatively each
	// evaluation — the blast radius of one fan-out. REQUIRED (no default: it is
	// the set of entities this executor will act on).
	UnitEntityPrefix string `json:"unit_entity_prefix"`

	// DispatchSubject is published (with the unit entity ID as a reference, never
	// content) when a unit becomes dispatchable. The consumer wires its own
	// handler here. REQUIRED.
	DispatchSubject string `json:"dispatch_subject"`

	// Marker / edge predicate vocabulary. Must be pairwise distinct after
	// defaulting — a collision would make e.g. a completed marker read as a
	// claim, silently corrupting dispatch.
	CompletedPredicate string `json:"completed_predicate,omitempty"`
	FailedPredicate    string `json:"failed_predicate,omitempty"`
	DirtiedPredicate   string `json:"dirtied_predicate,omitempty"`
	DependsOnPredicate string `json:"depends_on_predicate,omitempty"`
	ClaimPredicate     string `json:"claim_predicate,omitempty"`

	// Workers / QueueSize bound the dispatch concurrency leg (pkg/dispatch).
	Workers   int `json:"workers,omitempty"`
	QueueSize int `json:"queue_size,omitempty"`

	// BackstopInterval is the period of the unconditional re-eval tick that
	// closes the missed-watch-event hole and surfaces stalls (invariant #4).
	BackstopInterval string `json:"backstop_interval,omitempty"`

	// QueryTimeout bounds each authoritative graph.query.prefix read.
	QueryTimeout string `json:"query_timeout,omitempty"`

	// MaxUnits caps the authoritative whole-set read (QueryPrefixAll bound),
	// guarding reply size / memory. A fan-out larger than this logs a truncation
	// warning rather than silently dropping units.
	MaxUnits int `json:"max_units,omitempty"`

	// FailurePolicy selects how a failed unit affects new dispatch. One of
	// FailurePolicyContinueOthers (default) or FailurePolicyStopOnFirstFailure.
	FailurePolicy string `json:"failure_policy,omitempty"`
}

// DefaultConfig returns the framework defaults. Required fields
// (UnitEntityPrefix, DispatchSubject) are intentionally left empty — Validate
// rejects a config that does not set them.
func DefaultConfig() Config {
	return Config{
		FanOutWorkflow:     FanOutWorkflow,
		CompletedPredicate: defaultCompletedPredicate,
		FailedPredicate:    defaultFailedPredicate,
		DirtiedPredicate:   defaultDirtiedPredicate,
		DependsOnPredicate: defaultDependsOnPredicate,
		ClaimPredicate:     defaultClaimPredicate,
		Workers:            defaultWorkers,
		QueueSize:          defaultQueueSize,
		BackstopInterval:   defaultBackstopInterval,
		QueryTimeout:       defaultQueryTimeout,
		MaxUnits:           defaultMaxUnits,
		FailurePolicy:      FailurePolicyContinueOthers,
	}
}

// withDefaults returns a copy of c with unset fields filled from DefaultConfig.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.FanOutWorkflow == "" {
		c.FanOutWorkflow = d.FanOutWorkflow
	}
	if c.CompletedPredicate == "" {
		c.CompletedPredicate = d.CompletedPredicate
	}
	if c.FailedPredicate == "" {
		c.FailedPredicate = d.FailedPredicate
	}
	if c.DirtiedPredicate == "" {
		c.DirtiedPredicate = d.DirtiedPredicate
	}
	if c.DependsOnPredicate == "" {
		c.DependsOnPredicate = d.DependsOnPredicate
	}
	if c.ClaimPredicate == "" {
		c.ClaimPredicate = d.ClaimPredicate
	}
	if c.Workers == 0 {
		c.Workers = d.Workers
	}
	if c.QueueSize == 0 {
		c.QueueSize = d.QueueSize
	}
	if c.BackstopInterval == "" {
		c.BackstopInterval = d.BackstopInterval
	}
	if c.QueryTimeout == "" {
		c.QueryTimeout = d.QueryTimeout
	}
	if c.MaxUnits == 0 {
		c.MaxUnits = d.MaxUnits
	}
	if c.FailurePolicy == "" {
		c.FailurePolicy = d.FailurePolicy
	}
	return c
}

// Validate checks a defaulted config. Call withDefaults first (NewComponent
// does). Returns the first violation found.
func (c Config) Validate() error {
	if c.UnitEntityPrefix == "" {
		return fmt.Errorf("unit_entity_prefix is required (it scopes the unit set this executor reads)")
	}
	if c.DispatchSubject == "" {
		return fmt.Errorf("dispatch_subject is required (it is where a dispatchable unit's reference is published)")
	}
	if c.FanOutWorkflow == "" {
		return fmt.Errorf("fan_out_workflow must not be empty")
	}
	if c.Workers <= 0 {
		return fmt.Errorf("workers must be > 0 (got %d)", c.Workers)
	}
	if c.QueueSize <= 0 {
		return fmt.Errorf("queue_size must be > 0 (got %d)", c.QueueSize)
	}
	if c.MaxUnits <= 0 {
		return fmt.Errorf("max_units must be > 0 (got %d)", c.MaxUnits)
	}
	if _, err := c.backstopInterval(); err != nil {
		return fmt.Errorf("backstop_interval: %w", err)
	}
	if _, err := c.queryTimeout(); err != nil {
		return fmt.Errorf("query_timeout: %w", err)
	}
	switch c.FailurePolicy {
	case FailurePolicyContinueOthers, FailurePolicyStopOnFirstFailure:
	default:
		return fmt.Errorf("failure_policy must be %q or %q (got %q)",
			FailurePolicyContinueOthers, FailurePolicyStopOnFirstFailure, c.FailurePolicy)
	}
	// Predicates must be pairwise distinct: a collision silently mis-reads one
	// marker class as another.
	preds := map[string]string{
		"completed_predicate":  c.CompletedPredicate,
		"failed_predicate":     c.FailedPredicate,
		"dirtied_predicate":    c.DirtiedPredicate,
		"depends_on_predicate": c.DependsOnPredicate,
		"claim_predicate":      c.ClaimPredicate,
	}
	seen := make(map[string]string, len(preds))
	for name, val := range preds {
		if val == "" {
			return fmt.Errorf("%s must not be empty", name)
		}
		if other, dup := seen[val]; dup {
			return fmt.Errorf("predicate %q is used by both %s and %s; they must be distinct", val, other, name)
		}
		seen[val] = name
	}
	return nil
}

// backstopInterval parses BackstopInterval; must be > 0.
func (c Config) backstopInterval() (time.Duration, error) {
	d, err := time.ParseDuration(c.BackstopInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.BackstopInterval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0 (got %s)", d)
	}
	return d, nil
}

// queryTimeout parses QueryTimeout; must be > 0.
func (c Config) queryTimeout() (time.Duration, error) {
	d, err := time.ParseDuration(c.QueryTimeout)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", c.QueryTimeout, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0 (got %s)", d)
	}
	return d, nil
}
