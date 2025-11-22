// Package expression - Simple DSL for rule condition evaluation
package expression

import (
	"fmt"

	gtypes "github.com/c360/semstreams/types/graph"
)

// ConditionExpression represents a single field/operator/value condition
type ConditionExpression struct {
	Field    string      `json:"field"`    // Predicate field (e.g., "robotics.battery.level")
	Operator string      `json:"operator"` // Comparison operator (e.g., "lte", "eq", "contains")
	Value    interface{} `json:"value"`    // Comparison value (20.0, "active", true)
	Required bool        `json:"required"` // If false, missing field doesn't fail evaluation
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
)

// Logic operators
const (
	LogicAnd = "and"
	LogicOr  = "or"
)
