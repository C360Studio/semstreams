// Package rule - Rule Factory Pattern for Dynamic Rule Creation
package rule

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule/expression"
	"github.com/c360studio/semstreams/types"
)

// Definition represents a JSON rule configuration
type Definition struct {
	ID              string                           `json:"id"`
	Type            string                           `json:"type"`
	Name            string                           `json:"name"`
	Description     string                           `json:"description"`
	Enabled         bool                             `json:"enabled"`
	Conditions      []expression.ConditionExpression `json:"conditions"`
	Logic           string                           `json:"logic"`
	Cooldown        string                           `json:"cooldown,omitempty"`
	Entity          EntityConfig                     `json:"entity,omitempty"`
	Metadata        map[string]interface{}           `json:"metadata,omitempty"`
	OnEnter         []Action                         `json:"on_enter,omitempty"`         // Fires on false→true transition
	OnExit          []Action                         `json:"on_exit,omitempty"`          // Fires on true→false transition
	WhileTrue       []Action                         `json:"while_true,omitempty"`       // Fires on every update while true
	OnRecovery      []Action                         `json:"on_recovery,omitempty"`      // Fires once on restart if still matching; falls back to OnEnter
	RelatedPatterns []string                         `json:"related_patterns,omitempty"` // For pair rules

	// RerunOnRecovery opts rules with only OnEnter/WhileTrue defined into the
	// bootstrap recovery path. When false (default), such rules must declare
	// OnRecovery explicitly to re-fire on restart. Rules that already declare
	// OnRecovery always participate regardless of this flag.
	RerunOnRecovery bool `json:"rerun_on_recovery,omitempty"`

	// MaxIterations limits how many times a rule can enter the matching state for an entity.
	// 0 means no limit. Use $state.iteration and $state.max_iterations in When clauses
	// for retry budgets (e.g., retry up to 3 times then escalate).
	MaxIterations int `json:"max_iterations,omitempty"`

	// FireEveryNEvents makes the rule fire its action only every Nth matching
	// event. N=0 and N=1 both mean "fire on every match" (backward-compatible
	// default). N=2 fires on the 2nd, 4th, 6th matching events; N=10 fires on
	// the 10th, 20th, 30th, etc.
	//
	// The MATCH check still runs on every event — only the action is gated.
	// Counter state is per-rule (not per-entity) and resets when the rule
	// definition is replaced via applyRuleChanges, so a policy change begins
	// with a fresh rhythm.
	//
	// This is a parallel dimension to time-based AlertCooldownPeriod; neither
	// replaces the other.
	FireEveryNEvents int `json:"fire_every_n_events,omitempty"`

	// Schedule is the cron expression for `type: "cron"` rules. Required
	// when Type == "cron"; ignored otherwise. Supports POSIX 5-field
	// (`min hour dom mon dow`) and the descriptors @hourly / @daily /
	// @weekly / @monthly / @yearly. Parsed via robfig/cron/v3 at config
	// load time; bad expressions fail rule construction.
	Schedule string `json:"schedule,omitempty"`

	// Actions is the action list for `type: "cron"` rules. Cron rules use
	// this field instead of OnEnter/OnExit/WhileTrue because they have no
	// state-transition semantics — every scheduled tick fires every Action
	// here. Required when Type == "cron"; ignored otherwise.
	Actions []Action `json:"actions,omitempty"`
}

// EntityConfig defines entity-specific configuration
type EntityConfig struct {
	Pattern      string   `json:"pattern"`       // Exact six-position EntityState ID pattern
	WatchBuckets []string `json:"watch_buckets"` // Optional declaration; ENTITY_STATES only
}

// Factory creates rules from configuration
type Factory interface {
	// Create creates a rule instance from configuration
	Create(id string, config Definition, deps Dependencies) (Rule, error)

	// Type returns the rule type this factory creates
	Type() string

	// Schema returns the configuration schema for UI discovery
	Schema() Schema

	// Validate validates a rule configuration
	Validate(config Definition) error
}

// Dependencies provides dependencies for rule creation
type Dependencies struct {
	NATSClient *natsclient.Client
	Logger     *slog.Logger
	PackID     string
	// Platform is the deployment's own authority (deps.Platform at the
	// composition root): positions 1-2 of every entity a rule mints. Never
	// read back from a firing entity (ADR-102 d2; #1096).
	Platform types.PlatformMeta
}

// Schema describes the configuration schema for a rule type
type Schema struct {
	Type        string                              `json:"type"`
	DisplayName string                              `json:"display_name"`
	Description string                              `json:"description"`
	Category    string                              `json:"category"`
	Icon        string                              `json:"icon,omitempty"`
	Properties  map[string]component.PropertySchema `json:"properties"`
	Required    []string                            `json:"required"`
	Examples    []Example                           `json:"examples,omitempty"`
}

// Example provides example configurations
type Example struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Config      Definition `json:"config"`
}

// Global rule factory registry
var (
	factoryRegistry = make(map[string]Factory)
	factoryMutex    sync.RWMutex
)

// RegisterRuleFactory registers a rule factory
func RegisterRuleFactory(ruleType string, factory Factory) error {
	factoryMutex.Lock()
	defer factoryMutex.Unlock()

	if _, exists := factoryRegistry[ruleType]; exists {
		return fmt.Errorf("rule factory already registered for type: %s", ruleType)
	}

	factoryRegistry[ruleType] = factory
	return nil
}

// UnregisterRuleFactory removes a rule factory for a given type
// This is primarily for testing or dynamic factory management
func UnregisterRuleFactory(ruleType string) error {
	factoryMutex.Lock()
	defer factoryMutex.Unlock()

	if _, exists := factoryRegistry[ruleType]; !exists {
		return fmt.Errorf("no factory registered for type: %s", ruleType)
	}

	delete(factoryRegistry, ruleType)
	return nil
}

// GetRuleFactory returns a registered rule factory
func GetRuleFactory(ruleType string) (Factory, bool) {
	factoryMutex.RLock()
	defer factoryMutex.RUnlock()

	factory, exists := factoryRegistry[ruleType]
	return factory, exists
}

// GetRegisteredRuleTypes returns all registered rule types.
//
// Note: built-in rule kinds that do not go through the factory registry
// (today: "cron" — see CronRuleType) are not returned here. Callers that
// need the complete known-types surface should also include them
// explicitly. See isKnownRuleType for the validation-side equivalent.
func GetRegisteredRuleTypes() []string {
	factoryMutex.RLock()
	defer factoryMutex.RUnlock()

	types := make([]string, 0, len(factoryRegistry))
	for ruleType := range factoryRegistry {
		types = append(types, ruleType)
	}
	return types
}

// GetRuleSchemas returns schemas for all registered rule types
func GetRuleSchemas() map[string]Schema {
	factoryMutex.RLock()
	defer factoryMutex.RUnlock()

	schemas := make(map[string]Schema)
	for ruleType, factory := range factoryRegistry {
		schemas[ruleType] = factory.Schema()
	}
	return schemas
}

// CreateRuleFromDefinition creates a rule using the appropriate factory
func CreateRuleFromDefinition(def Definition, deps Dependencies) (Rule, error) {
	if err := ValidateDefinition(def); err != nil {
		return nil, fmt.Errorf("rule authoring validation failed: %w", err)
	}
	if err := validatePackID(deps.PackID); err != nil {
		return nil, fmt.Errorf("rule construction: %w", err)
	}
	factory, exists := GetRuleFactory(def.Type)
	if !exists {
		return nil, fmt.Errorf("no factory registered for rule type: %s", def.Type)
	}

	// Validate the configuration
	if err := factory.Validate(def); err != nil {
		return nil, fmt.Errorf("rule validation failed: %w", err)
	}

	// Create the rule
	return factory.Create(def.ID, def, deps)
}

// BaseRuleFactory provides common factory functionality
type BaseRuleFactory struct {
	ruleType    string
	displayName string
	description string
	category    string
}

// NewBaseRuleFactory creates a base factory implementation
func NewBaseRuleFactory(ruleType, displayName, description, category string) *BaseRuleFactory {
	return &BaseRuleFactory{
		ruleType:    ruleType,
		displayName: displayName,
		description: description,
		category:    category,
	}
}

// Type returns the rule type
func (f *BaseRuleFactory) Type() string {
	return f.ruleType
}

// ValidateExpression validates expression configuration
func (f *BaseRuleFactory) ValidateExpression(def Definition) error {
	if len(def.Conditions) == 0 {
		return fmt.Errorf("rule %s must have at least one condition", def.ID)
	}

	// Validate each condition
	for i, cond := range def.Conditions {
		if cond.Field == "" {
			return fmt.Errorf("rule %s condition[%d] missing field", def.ID, i)
		}
		if cond.Operator == "" {
			return fmt.Errorf("rule %s condition[%d] missing operator", def.ID, i)
		}
		if !isValidOperator(cond.Operator) {
			return fmt.Errorf("rule %s condition[%d] invalid operator: %s", def.ID, i, cond.Operator)
		}
	}

	// Validate logic operator
	if def.Logic != "" && def.Logic != "and" && def.Logic != "or" {
		return fmt.Errorf("rule %s invalid logic operator: %s (must be 'and' or 'or')", def.ID, def.Logic)
	}

	// Bootstrap recovery opt-ins must have actions to re-fire. RerunOnRecovery
	// replays OnEnter on restart, so OnEnter must be non-empty; otherwise the
	// flag is silently inert.
	if def.RerunOnRecovery && len(def.OnEnter) == 0 && len(def.OnRecovery) == 0 {
		return fmt.Errorf("rule %s has rerun_on_recovery=true but neither on_enter nor on_recovery actions are defined", def.ID)
	}

	return nil
}

// isValidOperator checks if an operator is valid.
//
// Derives the valid set from the evaluator's actual registered
// operators (#149 follow-up to the #147 split-brain finding).
// Pre-#149 this hardcoded a string list that drifted from the
// evaluator's operator map — length_eq / length_gt / length_lt /
// array_contains were validator-listed but never wired in the
// evaluator. Rules using them passed config validation and failed
// at fire time with "unsupported operator". Deriving from
// expression.RegisteredOperators() makes the validator's set match
// the evaluator's wiring by construction: an operator declared in
// expression/types.go but never wired in NewExpressionEvaluator
// is rejected by the validator at config-load time, not by a
// runtime error after the rule fires.
func isValidOperator(op string) bool {
	_, ok := expression.RegisteredOperators()[op]
	return ok
}
