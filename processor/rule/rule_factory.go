// Package rule - Rule Factory Pattern for Dynamic Rule Creation
package rule

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/processor/rule/expression"
	rtypes "github.com/c360/semstreams/types/rule"
)

// RuleDefinition represents a JSON rule configuration
type RuleDefinition struct {
	ID          string                           `json:"id"`
	Type        string                           `json:"type"`
	Name        string                           `json:"name"`
	Description string                           `json:"description"`
	Enabled     bool                             `json:"enabled"`
	Conditions  []expression.ConditionExpression `json:"conditions"`
	Logic       string                           `json:"logic"`
	Cooldown    string                           `json:"cooldown,omitempty"`
	Entity      EntityConfig                     `json:"entity,omitempty"`
	Metadata    map[string]interface{}           `json:"metadata,omitempty"`
}

// EntityConfig defines entity-specific configuration
type EntityConfig struct {
	Pattern      string   `json:"pattern"`       // Entity ID pattern to match
	WatchBuckets []string `json:"watch_buckets"` // KV buckets to watch
}

// RuleFactory creates rules from configuration
type RuleFactory interface {
	// Create creates a rule instance from configuration
	Create(id string, config RuleDefinition, deps RuleDependencies) (rtypes.Rule, error)

	// Type returns the rule type this factory creates
	Type() string

	// Schema returns the configuration schema for UI discovery
	Schema() RuleSchema

	// Validate validates a rule configuration
	Validate(config RuleDefinition) error
}

// RuleDependencies provides dependencies for rule creation
type RuleDependencies struct {
	NATSClient *natsclient.Client
	Logger     *slog.Logger
}

// RuleSchema describes the configuration schema for a rule type
type RuleSchema struct {
	Type        string                              `json:"type"`
	DisplayName string                              `json:"display_name"`
	Description string                              `json:"description"`
	Category    string                              `json:"category"`
	Icon        string                              `json:"icon,omitempty"`
	Properties  map[string]component.PropertySchema `json:"properties"`
	Required    []string                            `json:"required"`
	Examples    []RuleExample                       `json:"examples,omitempty"`
}

// RuleExample provides example configurations
type RuleExample struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      RuleDefinition `json:"config"`
}

// Global rule factory registry
var (
	factoryRegistry = make(map[string]RuleFactory)
	factoryMutex    sync.RWMutex
)

// RegisterRuleFactory registers a rule factory
func RegisterRuleFactory(ruleType string, factory RuleFactory) error {
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
func GetRuleFactory(ruleType string) (RuleFactory, bool) {
	factoryMutex.RLock()
	defer factoryMutex.RUnlock()

	factory, exists := factoryRegistry[ruleType]
	return factory, exists
}

// GetRegisteredRuleTypes returns all registered rule types
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
func GetRuleSchemas() map[string]RuleSchema {
	factoryMutex.RLock()
	defer factoryMutex.RUnlock()

	schemas := make(map[string]RuleSchema)
	for ruleType, factory := range factoryRegistry {
		schemas[ruleType] = factory.Schema()
	}
	return schemas
}

// CreateRuleFromDefinition creates a rule using the appropriate factory
func CreateRuleFromDefinition(def RuleDefinition, deps RuleDependencies) (rtypes.Rule, error) {
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
func (f *BaseRuleFactory) ValidateExpression(def RuleDefinition) error {
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

	return nil
}

// isValidOperator checks if an operator is valid
func isValidOperator(op string) bool {
	validOps := []string{
		"eq", "ne", "lt", "lte", "gt", "gte",
		"contains", "starts_with", "ends_with", "regex",
	}
	for _, valid := range validOps {
		if op == valid {
			return true
		}
	}
	return false
}
