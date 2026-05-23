// Package expression - Expression evaluator implementation
package expression

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
)

// NewExpressionEvaluator creates a new expression evaluator with all supported operators
func NewExpressionEvaluator() *Evaluator {
	evaluator := &Evaluator{
		operators:    make(map[string]OperatorFunc),
		typeDetector: &defaultTypeDetector{},
	}

	// Register numeric operators
	evaluator.operators[OpEqual] = operatorEqual
	evaluator.operators[OpNotEqual] = operatorNotEqual
	evaluator.operators[OpLessThan] = operatorLessThan
	evaluator.operators[OpLessThanEqual] = operatorLessThanEqual
	evaluator.operators[OpGreaterThan] = operatorGreaterThan
	evaluator.operators[OpGreaterThanEqual] = operatorGreaterThanEqual

	// Register array/range operators
	evaluator.operators[OpIn] = operatorIn
	evaluator.operators[OpNotIn] = operatorNotIn
	evaluator.operators[OpBetween] = operatorBetween

	// Note: OpTransition is handled specially in evaluateConditionWithStateAndMessage
	// because it needs access to $prev.* state fields, not just fieldValue/compareValue.

	// Register string operators
	evaluator.operators[OpContains] = operatorContains
	evaluator.operators[OpStartsWith] = operatorStartsWith
	evaluator.operators[OpEndsWith] = operatorEndsWith
	evaluator.operators[OpRegexMatch] = operatorRegex

	// Register array operators (#147 — closes the split-brain validator/
	// runtime gap where these were listed by isValidOperator but never
	// wired). Used by ADR-046 Phase 1 join pattern: length_eq counts
	// per-child completion triples on the parent loop entity.
	evaluator.operators[OpLengthEq] = operatorLengthEq
	evaluator.operators[OpLengthGt] = operatorLengthGt
	evaluator.operators[OpLengthLt] = operatorLengthLt
	evaluator.operators[OpArrayContains] = operatorArrayContains

	return evaluator
}

// isArrayOperator reports whether the named operator takes its
// fieldValue as a collection (resolves via GetFieldValuesAll) rather
// than a scalar (GetFieldValue first-match). Array operators count
// or check membership across the full set of triples carrying a
// given predicate; scalar operators see only the first match.
// #147 / ADR-046 Phase 1 counter pattern.
func isArrayOperator(op string) bool {
	switch op {
	case OpLengthEq, OpLengthGt, OpLengthLt, OpArrayContains:
		return true
	}
	return false
}

// Evaluate evaluates a logical expression against an entity state.
// Equivalent to EvaluateWithStateAndMessage with nil stateFields and nil
// messageFields — entity triples are the only resolution source.
func (e *Evaluator) Evaluate(entityState *gtypes.EntityState, expr LogicalExpression) (bool, error) {
	return e.EvaluateWithStateAndMessage(entityState, nil, nil, expr)
}

// EvaluateWithStateAndMessage evaluates a logical expression against the
// full field-resolution surface: entity triples, rule match-state
// (`$state.*`/`$prev.*`), and the inbound message payload (`$message.*`).
//
// Field resolution precedence per condition:
//
//  1. `$state.*` / `$prev.*` → stateFields map
//  2. `$message.<dotted.path>` → messageFields via deep-walk
//  3. Bare field name → entity triples first (when entity is non-nil);
//     falls through to messageFields if not present on the entity.
//     When entity is nil (message-path rules), bare names resolve from
//     messageFields directly.
//
// This is the single condition evaluator for both rule-level conditions
// (where the rule fires on message-path or entity-state events) and
// action-level `when` guards. See ADR-041 for the unification rationale.
//
// `$message.command` is the recommended form when both rule-level and
// when-clause use the same field, because it makes the resolution source
// explicit. Bare `command` works equivalently on message-path rules but
// implicitly prefers entity triples on entity-path rules.
func (e *Evaluator) EvaluateWithStateAndMessage(entityState *gtypes.EntityState, stateFields StateFields, messageFields MessageFields, expr LogicalExpression) (bool, error) {
	if len(expr.Conditions) == 0 {
		return true, nil // Empty condition list passes
	}

	results := make([]bool, len(expr.Conditions))

	// Evaluate each condition
	for i, condition := range expr.Conditions {
		result, err := e.evaluateConditionWithStateAndMessage(entityState, stateFields, messageFields, condition)
		if err != nil {
			return false, err
		}
		results[i] = result
	}

	// Apply logic operator
	switch expr.Logic {
	case LogicOr, "": // Default to OR if not specified
		for _, result := range results {
			if result {
				return true, nil
			}
		}
		return false, nil

	case LogicAnd:
		for _, result := range results {
			if !result {
				return false, nil
			}
		}
		return true, nil

	default:
		return false, &EvaluationError{
			Message: fmt.Sprintf("unsupported logic operator: %s", expr.Logic),
		}
	}
}

// evaluateConditionWithStateAndMessage resolves a single condition against
// the layered sources (stateFields, messageFields, entity triples) per
// the precedence rule documented on EvaluateWithStateAndMessage.
func (e *Evaluator) evaluateConditionWithStateAndMessage(entityState *gtypes.EntityState, stateFields StateFields, messageFields MessageFields, condition ConditionExpression) (bool, error) {
	// 1. `$state.*` / `$prev.*` — pseudo-fields from rule match state.
	if strings.HasPrefix(condition.Field, "$state.") || strings.HasPrefix(condition.Field, "$prev.") {
		fieldValue, exists := stateFields[condition.Field]
		if !exists {
			if condition.Required {
				return false, &EvaluationError{
					Field:   condition.Field,
					Message: "required state field not found",
				}
			}
			return false, nil
		}
		return e.applyOperator(condition, fieldValue)
	}

	// 2. `$message.<path>` — explicit message payload access.
	if strings.HasPrefix(condition.Field, "$message.") {
		path := strings.TrimPrefix(condition.Field, "$message.")
		fieldValue, exists := ExtractMessageValue(messageFields, path)
		if !exists {
			if condition.Required {
				return false, &EvaluationError{
					Field:   condition.Field,
					Message: "required message field not found",
				}
			}
			return false, nil
		}
		return e.applyOperator(condition, fieldValue)
	}

	// 3. Transition operator — entity-state-only (uses $prev.* from
	//    stateFields against the current entity triple value).
	if condition.Operator == OpTransition {
		return e.evaluateTransition(entityState, stateFields, condition)
	}

	// 4. Bare field name resolution.
	//    Entity-path (entity non-nil): triples first, fall through to
	//    messageFields if not present.
	//    Message-path (entity nil): messageFields only.
	//    Array operators (#147 — length_eq, length_gt, length_lt,
	//    array_contains) resolve via GetFieldValuesAll so they see
	//    EVERY matching triple, not just the first. The counter
	//    pattern stamps N triples with the same predicate keyed by
	//    child ID; the join rule needs all N to count correctly.
	if entityState != nil {
		if isArrayOperator(condition.Operator) {
			values, exists, err := e.typeDetector.GetFieldValuesAll(entityState, condition.Field)
			if err != nil {
				return false, &EvaluationError{
					Field:    condition.Field,
					Operator: condition.Operator,
					Message:  "failed to get field values",
					Err:      err,
				}
			}
			if exists {
				return e.applyOperator(condition, values)
			}
			// Predicate carries zero matching triples — array
			// operators get a zero-length array rather than
			// "field missing." length_eq with value=0 is a legit
			// match (no children completed yet); array_contains
			// returns false on empty. Fall through to messageFields
			// only when the entity entirely lacks the predicate AND
			// the operator is non-array; for array ops we want the
			// "empty list" semantic.
			return e.applyOperator(condition, []interface{}{})
		}
		fieldValue, exists, err := e.typeDetector.GetFieldValue(entityState, condition.Field)
		if err != nil {
			return false, &EvaluationError{
				Field:   condition.Field,
				Message: "failed to get field value",
				Err:     err,
			}
		}
		if exists {
			return e.applyOperator(condition, fieldValue)
		}
		// Fall through to messageFields below.
	}

	if messageFields != nil {
		if fieldValue, ok := ExtractMessageValue(messageFields, condition.Field); ok {
			return e.applyOperator(condition, fieldValue)
		}
	}

	if condition.Required {
		if entityState == nil && messageFields == nil {
			return false, &EvaluationError{
				Field:   condition.Field,
				Message: "no entity state or message fields available",
			}
		}
		return false, &EvaluationError{
			Field:   condition.Field,
			Message: "required field not found",
		}
	}
	return false, nil
}

// applyOperator runs the registered operator function for the condition
// against fieldValue and returns the boolean result. Used by every field
// resolution branch in evaluateConditionWithStateAndMessage so error
// shapes stay uniform.
func (e *Evaluator) applyOperator(condition ConditionExpression, fieldValue any) (bool, error) {
	opFunc, ok := e.operators[condition.Operator]
	if !ok {
		return false, &EvaluationError{
			Field:    condition.Field,
			Operator: condition.Operator,
			Message:  "unsupported operator",
		}
	}
	result, err := opFunc(fieldValue, condition.Value)
	if err != nil {
		return false, &EvaluationError{
			Field:    condition.Field,
			Operator: condition.Operator,
			Message:  "operator execution failed",
			Err:      err,
		}
	}
	return result, nil
}

// evaluateTransition checks whether a field is transitioning from an allowed previous
// value to a specified target value. It requires:
//   - The current field value (from entity triples) equals condition.Value (the "to" state)
//   - The previous field value (from stateFields["$prev.<field>"]) is in condition.From
//
// If no previous value is tracked (first evaluation), returns false — a transition
// requires history. If condition.From is nil, any change from a different previous value
// to condition.Value is treated as a valid transition.
func (e *Evaluator) evaluateTransition(entityState *gtypes.EntityState, stateFields StateFields, condition ConditionExpression) (bool, error) {
	// Entity state is required for transition checks
	if entityState == nil {
		if condition.Required {
			return false, &EvaluationError{
				Field:    condition.Field,
				Operator: OpTransition,
				Message:  "entity state is nil, transition cannot be evaluated",
			}
		}
		return false, nil
	}

	// Get current field value from entity triples
	currentValue, exists, err := e.typeDetector.GetFieldValue(entityState, condition.Field)
	if err != nil {
		return false, &EvaluationError{
			Field:    condition.Field,
			Operator: OpTransition,
			Message:  "failed to get current field value",
			Err:      err,
		}
	}
	if !exists {
		return false, nil
	}

	// Check that current value matches the target ("to") value
	if compareValues(currentValue, condition.Value) != 0 {
		return false, nil
	}

	// Look up previous value from state fields
	prevKey := "$prev." + condition.Field
	prevValue, hasPrev := stateFields[prevKey]
	if !hasPrev {
		// First evaluation — no previous value tracked, can't detect a transition
		return false, nil
	}

	// If From is specified, check that previous value is in the allowed set
	if condition.From != nil {
		fromSlice := normalizeToSlice(condition.From)
		for _, allowed := range fromSlice {
			if compareValues(prevValue, allowed) == 0 {
				return true, nil
			}
		}
		// Previous value not in allowed From set
		return false, nil
	}

	// From is nil — any change to the target value counts as a valid transition
	// (but the previous value must differ from current to be a real transition)
	return compareValues(prevValue, currentValue) != 0, nil
}

// normalizeToSlice converts a value that may be a single item or a slice into []interface{}.
func normalizeToSlice(v interface{}) []interface{} {
	if arr, ok := v.([]interface{}); ok {
		return arr
	}
	return []interface{}{v}
}

// defaultTypeDetector implements TypeDetector using existing triple access functions
type defaultTypeDetector struct{}

// GetFieldValue extracts a field value from entity state using existing helper functions
func (d *defaultTypeDetector) GetFieldValue(entityState *gtypes.EntityState, field string) (interface{}, bool, error) {
	// Import existing functions from entity_state_rule.go
	// We'll need to make them accessible or recreate the logic here
	for _, triple := range entityState.Triples {
		if triple.Predicate == field {
			return triple.Object, true, nil
		}
	}
	return nil, false, nil
}

// GetFieldValuesAll collects every triple Object matching the
// predicate, in trigger-time order. #147 / ADR-046 Phase 1 counter
// pattern: rule stamps N triples with the same predicate keyed by
// child ID; the join rule's length_eq operator needs all N to count.
// Returns (nil, false) when no triple matches.
func (d *defaultTypeDetector) GetFieldValuesAll(entityState *gtypes.EntityState, field string) ([]interface{}, bool, error) {
	if entityState == nil {
		return nil, false, nil
	}
	var values []interface{}
	for _, triple := range entityState.Triples {
		if triple.Predicate == field {
			values = append(values, triple.Object)
		}
	}
	if len(values) == 0 {
		return nil, false, nil
	}
	return values, true, nil
}

// DetectFieldType determines the Go type of a field value
func (d *defaultTypeDetector) DetectFieldType(value interface{}) FieldType {
	switch value.(type) {
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return FieldTypeFloat64
	case string:
		return FieldTypeString
	case bool:
		return FieldTypeBool
	case []interface{}:
		return FieldTypeArray
	default:
		return FieldTypeUnknown
	}
}

// Operator implementations

func operatorEqual(fieldValue, compareValue interface{}) (bool, error) {
	return compareValues(fieldValue, compareValue) == 0, nil
}

func operatorNotEqual(fieldValue, compareValue interface{}) (bool, error) {
	return compareValues(fieldValue, compareValue) != 0, nil
}

func operatorLessThan(fieldValue, compareValue interface{}) (bool, error) {
	cmp, err := compareValuesWithError(fieldValue, compareValue)
	if err != nil {
		return false, err
	}
	return cmp < 0, nil
}

func operatorLessThanEqual(fieldValue, compareValue interface{}) (bool, error) {
	cmp, err := compareValuesWithError(fieldValue, compareValue)
	if err != nil {
		return false, err
	}
	return cmp <= 0, nil
}

func operatorGreaterThan(fieldValue, compareValue interface{}) (bool, error) {
	cmp, err := compareValuesWithError(fieldValue, compareValue)
	if err != nil {
		return false, err
	}
	return cmp > 0, nil
}

func operatorGreaterThanEqual(fieldValue, compareValue interface{}) (bool, error) {
	cmp, err := compareValuesWithError(fieldValue, compareValue)
	if err != nil {
		return false, err
	}
	return cmp >= 0, nil
}

func operatorContains(fieldValue, compareValue interface{}) (bool, error) {
	fieldStr, ok := fieldValue.(string)
	if !ok {
		fieldStr = fmt.Sprintf("%v", fieldValue)
	}

	compareStr, ok := compareValue.(string)
	if !ok {
		compareStr = fmt.Sprintf("%v", compareValue)
	}

	return strings.Contains(fieldStr, compareStr), nil
}

func operatorStartsWith(fieldValue, compareValue interface{}) (bool, error) {
	fieldStr, ok := fieldValue.(string)
	if !ok {
		fieldStr = fmt.Sprintf("%v", fieldValue)
	}

	compareStr, ok := compareValue.(string)
	if !ok {
		compareStr = fmt.Sprintf("%v", compareValue)
	}

	return strings.HasPrefix(fieldStr, compareStr), nil
}

func operatorEndsWith(fieldValue, compareValue interface{}) (bool, error) {
	fieldStr, ok := fieldValue.(string)
	if !ok {
		fieldStr = fmt.Sprintf("%v", fieldValue)
	}

	compareStr, ok := compareValue.(string)
	if !ok {
		compareStr = fmt.Sprintf("%v", compareValue)
	}

	return strings.HasSuffix(fieldStr, compareStr), nil
}

func operatorIn(fieldValue, compareValue interface{}) (bool, error) {
	arr, ok := compareValue.([]interface{})
	if !ok {
		return false, fmt.Errorf("in operator requires array value, got %T", compareValue)
	}
	for _, item := range arr {
		if compareValues(fieldValue, item) == 0 {
			return true, nil
		}
	}
	return false, nil
}

func operatorNotIn(fieldValue, compareValue interface{}) (bool, error) {
	result, err := operatorIn(fieldValue, compareValue)
	if err != nil {
		return false, err
	}
	return !result, nil
}

func operatorBetween(fieldValue, compareValue interface{}) (bool, error) {
	arr, ok := compareValue.([]interface{})
	if !ok || len(arr) != 2 {
		return false, fmt.Errorf("between operator requires array of [min, max], got %T", compareValue)
	}
	gteResult, err := operatorGreaterThanEqual(fieldValue, arr[0])
	if err != nil {
		return false, err
	}
	if !gteResult {
		return false, nil
	}
	return operatorLessThanEqual(fieldValue, arr[1])
}

// coerceToInt extracts an int from a comparison value. Accepts the
// JSON-unmarshal float64 default, plus the common int widths, plus
// strings (rule configs sometimes carry numeric literals as strings
// when loaded from substitution paths that pre-stringify). Returns
// an error for anything else so authoring mistakes surface loudly.
func coerceToInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case int32:
		return int(n), nil
	case float64:
		return int(n), nil
	case float32:
		return int(n), nil
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as integer: %w", n, err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("expected integer-coercible value, got %T", v)
	}
}

// arrayLen returns the length of a field value resolved via
// GetFieldValuesAll. The resolver returns []interface{}, but
// stateFields / messageFields paths may pass other shapes — handle
// the common ones loudly.
func arrayLen(fieldValue interface{}) (int, error) {
	switch v := fieldValue.(type) {
	case nil:
		return 0, nil
	case []interface{}:
		return len(v), nil
	case []string:
		return len(v), nil
	case string:
		// A single scalar triple Object that arrived through the
		// non-array resolution path. Length-of-one semantics so a
		// rule that expects exactly-one-completion-stamp still
		// matches without authors having to special-case.
		return 1, nil
	default:
		return 0, fmt.Errorf("length operator requires array-shaped field, got %T", fieldValue)
	}
}

func operatorLengthEq(fieldValue, compareValue interface{}) (bool, error) {
	got, err := arrayLen(fieldValue)
	if err != nil {
		return false, err
	}
	want, err := coerceToInt(compareValue)
	if err != nil {
		return false, fmt.Errorf("length_eq compare value: %w", err)
	}
	return got == want, nil
}

func operatorLengthGt(fieldValue, compareValue interface{}) (bool, error) {
	got, err := arrayLen(fieldValue)
	if err != nil {
		return false, err
	}
	want, err := coerceToInt(compareValue)
	if err != nil {
		return false, fmt.Errorf("length_gt compare value: %w", err)
	}
	return got > want, nil
}

func operatorLengthLt(fieldValue, compareValue interface{}) (bool, error) {
	got, err := arrayLen(fieldValue)
	if err != nil {
		return false, err
	}
	want, err := coerceToInt(compareValue)
	if err != nil {
		return false, fmt.Errorf("length_lt compare value: %w", err)
	}
	return got < want, nil
}

// operatorArrayContains reports whether the array fieldValue contains
// an item equal to compareValue. Equality is compareValues == 0 to
// match the cross-type-coercion semantics the rest of the evaluator
// uses for `in` / `eq`. Scalar fieldValue degenerates to a
// single-element array (so a rule that expects one specific value can
// match without special-casing whether the predicate fired once or
// multiple times).
func operatorArrayContains(fieldValue, compareValue interface{}) (bool, error) {
	arr := normalizeToSlice(fieldValue)
	for _, item := range arr {
		if compareValues(item, compareValue) == 0 {
			return true, nil
		}
	}
	return false, nil
}

func operatorRegex(fieldValue, compareValue interface{}) (bool, error) {
	fieldStr, ok := fieldValue.(string)
	if !ok {
		fieldStr = fmt.Sprintf("%v", fieldValue)
	}

	pattern, ok := compareValue.(string)
	if !ok {
		return false, errs.WrapInvalid(errs.ErrInvalidData, "Evaluator", "operatorRegex", "regex pattern must be a string")
	}

	// Use cached regex compilation for better performance
	re, err := compileRegex(pattern)
	if err != nil {
		return false, err
	}

	return re.MatchString(fieldStr), nil
}

// Helper functions for value comparison

func compareValues(a, b interface{}) int {
	result, _ := compareValuesWithError(a, b)
	return result
}

func compareValuesWithError(a, b interface{}) (int, error) {
	// Try numeric comparison first
	aNum, aIsNum := toFloat64(a)
	bNum, bIsNum := toFloat64(b)

	if aIsNum && bIsNum {
		if aNum < bNum {
			return -1, nil
		} else if aNum > bNum {
			return 1, nil
		}
		return 0, nil
	}

	// Fallback to string comparison
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)

	if aStr < bStr {
		return -1, nil
	} else if aStr > bStr {
		return 1, nil
	}
	return 0, nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	default:
		return 0, false
	}
}

// Expression helper functions for rule conditions

// HasTriple checks if an entity has a triple with the given predicate.
// Returns false if entity is nil or predicate not found.
func (e *Evaluator) HasTriple(entity *gtypes.EntityState, predicate string) bool {
	if entity == nil {
		return false
	}

	for _, triple := range entity.Triples {
		if triple.Predicate == predicate {
			return true
		}
	}

	return false
}

// GetOutgoing returns a list of entity IDs for outgoing relationships with the given predicate.
// Only returns valid entity IDs (4-part dotted notation), not literal values.
// Returns empty slice if entity is nil or no relationships found.
func (e *Evaluator) GetOutgoing(entity *gtypes.EntityState, predicate string) []string {
	// Always return non-nil slice for consistency
	outgoing := []string{}

	if entity == nil {
		return outgoing
	}

	for _, triple := range entity.Triples {
		if triple.Predicate == predicate {
			// Check if Object is a valid entity ID (relationship)
			if objStr, ok := triple.Object.(string); ok {
				// Use message.IsValidEntityID to check for valid entity reference
				if isValidEntityID(objStr) {
					outgoing = append(outgoing, objStr)
				}
			}
		}
	}

	return outgoing
}

// Distance calculates the great-circle distance between two entities in meters
// using the Haversine formula. Returns error if either entity lacks position data.
// Position is now extracted from geo.location.* triples (single source of truth).
func (e *Evaluator) Distance(entity1, entity2 *gtypes.EntityState) (float64, error) {
	if entity1 == nil || entity2 == nil {
		return 0, errs.WrapInvalid(errs.ErrInvalidData, "Evaluator", "Distance", "both entities must be non-nil")
	}

	// Extract position from triples
	lat1, lon1 := extractLatLonFromTriples(entity1)
	lat2, lon2 := extractLatLonFromTriples(entity2)

	if lat1 == 0 && lon1 == 0 {
		return 0, errs.WrapInvalid(errs.ErrInvalidData, "Evaluator", "Distance", "entity1 must have position data in triples")
	}
	if lat2 == 0 && lon2 == 0 {
		return 0, errs.WrapInvalid(errs.ErrInvalidData, "Evaluator", "Distance", "entity2 must have position data in triples")
	}

	return haversineDistance(lat1, lon1, lat2, lon2), nil
}

// extractLatLonFromTriples extracts latitude and longitude from entity triples
func extractLatLonFromTriples(entity *gtypes.EntityState) (float64, float64) {
	var lat, lon float64
	for _, triple := range entity.Triples {
		switch triple.Predicate {
		case "geo.location.latitude", "latitude":
			if v, ok := triple.Object.(float64); ok {
				lat = v
			}
		case "geo.location.longitude", "longitude":
			if v, ok := triple.Object.(float64); ok {
				lon = v
			}
		}
	}
	return lat, lon
}

// haversineDistance calculates the great-circle distance between two points
// on Earth given their latitude and longitude in degrees.
// Returns distance in meters.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000 // Earth's radius in meters

	// Convert degrees to radians
	lat1Rad := lat1 * 3.14159265359 / 180
	lat2Rad := lat2 * 3.14159265359 / 180
	deltaLatRad := (lat2 - lat1) * 3.14159265359 / 180
	deltaLonRad := (lon2 - lon1) * 3.14159265359 / 180

	// Haversine formula
	a := math.Sin(deltaLatRad/2)*math.Sin(deltaLatRad/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLonRad/2)*math.Sin(deltaLonRad/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}

// isValidEntityID checks if a string is a valid 6-part entity ID
// This is a local copy to avoid circular dependency with message package
func isValidEntityID(s string) bool {
	if s == "" {
		return false
	}

	parts := strings.Split(s, ".")
	if len(parts) != 6 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}
	}

	return true
}
