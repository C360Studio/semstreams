// Package expression - Simple DSL for rule condition evaluation
package expression

import (
	"fmt"

	gtypes "github.com/c360studio/semstreams/graph"
)

// ConditionExpression represents a single field/operator/value condition
type ConditionExpression struct {
	Field    string      `json:"field"`          // Predicate field (e.g., "robotics.battery.level")
	Operator string      `json:"operator"`       // Comparison operator (e.g., "lte", "eq", "contains")
	Value    interface{} `json:"value"`          // Comparison value (20.0, "active", true)
	Required bool        `json:"required"`       // If false, missing field doesn't fail evaluation
	From     interface{} `json:"from,omitempty"` // For transition operator: allowed previous value(s)
	// Negate inverts the boolean result of the operator. negate:true on
	// eq means "not equal"; on in means "not in array"; etc. Forbidden
	// on the transition operator (validate at config load) — three plausible
	// meanings of "negated transition" make the semantics ambiguous.
	Negate bool `json:"negate,omitempty"`
}

// LogicalExpression combines multiple conditions with logic operators
type LogicalExpression struct {
	Conditions []ConditionExpression `json:"conditions"`
	Logic      string                `json:"logic"` // "and", "or"
}

// Evaluator processes expressions against entity state
type Evaluator struct {
	operators    map[string]OperatorFunc
	typeDetector TypeDetector
	// windowReader provides KV-backed sliding-window state for the
	// count_in_window operator. Nil when no window reader is configured
	// (the operator returns an error when invoked without one, rather
	// than silently ignoring it). Set via NewExpressionEvaluatorWithWindowReader.
	windowReader WindowStateReader
	// windowRuleID is the rule ID passed to windowReader.Increment.
	// It is set alongside windowReader and identifies which rule's
	// dimension namespace the counter belongs to. Empty when windowReader is nil.
	windowRuleID string
}

// OperatorFunc defines the signature for operator implementations
type OperatorFunc func(fieldValue, compareValue interface{}) (bool, error)

// TypeDetector determines field type and extracts values from entity state
type TypeDetector interface {
	GetFieldValue(entityState *gtypes.EntityState, field string) (value interface{}, exists bool, err error)
	DetectFieldType(value interface{}) FieldType
}

// FieldType represents the detected type of a field
type FieldType int

const (
	// FieldTypeUnknown represents an unknown or unsupported field type
	FieldTypeUnknown FieldType = iota
	// FieldTypeFloat64 represents a floating point number field
	FieldTypeFloat64
	// FieldTypeString represents a string field
	FieldTypeString
	// FieldTypeBool represents a boolean field
	FieldTypeBool
	// FieldTypeArray represents an array field
	FieldTypeArray
)

func (f FieldType) String() string {
	switch f {
	case FieldTypeFloat64:
		return "float64"
	case FieldTypeString:
		return "string"
	case FieldTypeBool:
		return "bool"
	case FieldTypeArray:
		return "array"
	default:
		return "unknown"
	}
}

// EvaluationError represents an error during expression evaluation
type EvaluationError struct {
	Field    string
	Operator string
	Message  string
	Err      error
}

func (e *EvaluationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("evaluation error for field '%s' with operator '%s': %s: %v",
			e.Field, e.Operator, e.Message, e.Err)
	}
	return fmt.Sprintf("evaluation error for field '%s' with operator '%s': %s",
		e.Field, e.Operator, e.Message)
}

func (e *EvaluationError) Unwrap() error {
	return e.Err
}

// Supported operators by field type
const (
	// Numeric operators
	OpEqual            = "eq"
	OpNotEqual         = "ne"
	OpLessThan         = "lt"
	OpLessThanEqual    = "lte"
	OpGreaterThan      = "gt"
	OpGreaterThanEqual = "gte"
	OpBetween          = "between"

	// String operators
	OpContains   = "contains"
	OpStartsWith = "starts_with"
	OpEndsWith   = "ends_with"
	OpRegexMatch = "regex"

	// Boolean operators (eq/ne only)

	// Array operators
	OpIn            = "in"
	OpNotIn         = "not_in"
	OpLengthEq      = "length_eq"
	OpLengthGt      = "length_gt"
	OpLengthLt      = "length_lt"
	OpArrayContains = "array_contains"

	// State transition operator
	OpTransition = "transition"

	// Count-in-window operator: counts occurrences of a dimension over a
	// rolling time window. Value is a config object:
	//   {"window": "5m", "gt": 100, "max_dimensions": 1000, "max_per_window": 5000}
	// The nested comparator (gt/gte/lt/lte/eq/ne) applies to the rolling count.
	// Requires a WindowStateReader injected via NewExpressionEvaluatorWithWindowReader.
	OpCountInWindow = "count_in_window"
)

// Logic operators
const (
	LogicAnd = "and"
	LogicOr  = "or"
	// LogicNot wraps a single condition's boolean result in negation.
	// Requires EXACTLY ONE condition — rejected at config-load time if zero
	// or more than one condition is present (De Morgan ambiguity). Cannot be
	// combined with per-condition Negate:true (double-negation anti-pattern).
	LogicNot = "not"
)

// StateFields provides rule match state values for $state.* pseudo-field resolution
// in condition expressions. Keys are the full field names (e.g., "$state.iteration").
// This avoids circular dependencies between the expression and rule packages.
type StateFields map[string]interface{}
