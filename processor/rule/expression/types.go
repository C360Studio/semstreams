// Package expression - Simple DSL for rule condition evaluation
package expression

import (
	"fmt"

	gtypes "github.com/c360studio/semstreams/graph"
)

// ConditionExpression represents a single field/operator/value condition
type ConditionExpression struct {
	Field    string      `json:"field"`    // Predicate field (e.g., "robotics.battery.level")
	Operator string      `json:"operator"` // Comparison operator (e.g., "lte", "eq", "contains")
	Value    interface{} `json:"value"`    // Comparison value (20.0, "active", true)
	// Required: when true, a missing field causes evaluation to error
	// rather than silently match false. Scalar operators (eq, lt,
	// contains, ...) honour this strictly: missing predicate +
	// Required=true → EvaluationError.
	//
	// Array operators (length_eq, length_gt, length_lt, array_contains
	// — see isArrayOperator) DELIBERATELY DIVERGE: a missing predicate
	// resolves to an empty array (`[]interface{}{}`) and the operator
	// runs against it. length_eq with value=0 then matches; length_gt
	// with value=N>0 doesn't; array_contains returns false. This is
	// the intended semantic for the #147 / ADR-046 Phase 1 counter
	// pattern, where a join rule wanting "fire before any children
	// complete" is a legitimate authoring intent. Authors who want
	// "this predicate MUST exist on the entity" should pair the array
	// operator with a separate scalar eq/exists condition over the
	// same field.
	Required bool        `json:"required"`
	From     interface{} `json:"from,omitempty"` // For transition operator: allowed previous value(s)
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
	// GetFieldValuesAll returns every triple Object matching the given
	// predicate as a []interface{}. Used by array operators (length_eq,
	// length_gt, length_lt, array_contains — see isArrayOperator) where
	// the natural semantics are "how many triples carry this predicate"
	// rather than "what is the first triple's Object". Returns
	// (nil, false) when no triple matches. #147 / ADR-046 Phase 1 join
	// gap: the counter pattern stamps N triples with the same
	// predicate keyed by child ID; the join rule counts them via
	// length_eq on this multi-valued path.
	GetFieldValuesAll(entityState *gtypes.EntityState, field string) (values []interface{}, exists bool, err error)
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
)

// Logic operators
const (
	LogicAnd = "and"
	LogicOr  = "or"
)

// StateFields provides rule match state values for $state.* pseudo-field resolution
// in condition expressions. Keys are the full field names (e.g., "$state.iteration").
// This avoids circular dependencies between the expression and rule packages.
type StateFields map[string]interface{}

// MessageFields carries the inbound NATS message payload for `$message.*`
// pseudo-field resolution in condition expressions. The map shape is
// `map[string]any` (matching `GenericJSONPayload.Data` and what
// `ExecutionContext.MessageData` carries through the rule pipeline).
//
// Deep-path access is supported: `$message.tool_args.command` walks the
// map along the dotted path. Bare field names (e.g. `command`) also
// resolve here when entity state is nil or when the field isn't present
// as an entity triple — see Evaluator.EvaluateWithStateAndMessage for the
// full precedence rule.
//
// Empty or nil maps cause `$message.*` and bare-name message lookups to
// return "not found" — same surfacing behaviour as missing entity triples
// (condition fails when Required=false, errors when Required=true).
type MessageFields map[string]any
