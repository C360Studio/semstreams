// Package expression - count_in_window operator for sliding-window rate/threshold conditions.
//
// The count_in_window operator counts distinct-dimension occurrences over a rolling
// time window. It is the primary building block for rate-limiting rules:
//
//	{
//	  "field": "$caller.id",
//	  "operator": "count_in_window",
//	  "value": {
//	    "window": "5m",
//	    "gt": 100,
//	    "max_dimensions": 1000,
//	    "max_per_window": 5000
//	  }
//	}
//
// # Storage model
//
// The operator delegates ALL state reads/writes to a WindowStateReader
// implementation (see window_tracker.go in the parent rule package). The
// expression package itself is KV-free for testability — swapping the reader
// to an in-memory fake is sufficient to unit-test every operator path without
// spinning up NATS.
//
// # Cardinality safety
//
// The reader implementation enforces cardinality caps. When a dimension is
// rejected by the cap, Increment returns (-1, nil) and the operator returns
// false (conservative non-match). The reader also enforces a per-key count
// cap to bound bucket byte size.
//
// # Window-comparison semantics
//
// The nested comparison operator (gt/gte/lt/lte/eq/ne) applies to the count
// returned by Increment, which already includes the current occurrence. A
// condition "gt: 100" fires when this occurrence is the 101st in the window.
package expression

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WindowStateReader abstracts KV-backed sliding-window state so the expression
// package stays KV-free and fully unit-testable without NATS.
//
// Increment records one occurrence of the given dimension under ruleID at now,
// counts all occurrences within the rolling window ending at now, and returns
// the total count. The current occurrence IS included in the returned count.
//
// Returns (-1, nil) when the dimension is rejected by the cardinality cap;
// callers MUST treat -1 as a non-match (false). Any other negative count is
// an implementation bug.
//
// Context propagation is mandatory — callers must honour cancellation so that
// a stuck KV write does not block rule evaluation indefinitely.
type WindowStateReader interface {
	Increment(ctx context.Context, ruleID, dimension string, now time.Time, window time.Duration) (count int, err error)
}

// WindowConditionConfig is the parsed form of the count_in_window condition value
// map. Validation is the caller's responsibility (config_validation.go); this struct
// is intentionally loose about zero values so the operator can surface helpful
// errors rather than silent failures.
type WindowConditionConfig struct {
	Window        time.Duration
	Comparator    string // gt / gte / lt / lte / eq / ne
	Threshold     float64
	MaxDimensions int // 0 means "use framework default"
	MaxPerWindow  int // 0 means "use framework default"
}

// parseWindowConfig extracts a WindowConditionConfig from a condition's Value
// field (which is map[string]interface{} when decoded from JSON).
// Returns a descriptive error when any required sub-field is missing or malformed.
func parseWindowConfig(value interface{}) (WindowConditionConfig, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return WindowConditionConfig{}, fmt.Errorf("count_in_window value must be an object, got %T", value)
	}

	// Parse window duration (required)
	windowRaw, ok := m["window"]
	if !ok {
		return WindowConditionConfig{}, fmt.Errorf("count_in_window value missing required field 'window'")
	}
	windowStr, ok := windowRaw.(string)
	if !ok {
		return WindowConditionConfig{}, fmt.Errorf("count_in_window window must be a string, got %T", windowRaw)
	}
	window, err := time.ParseDuration(windowStr)
	if err != nil {
		return WindowConditionConfig{}, fmt.Errorf("count_in_window window %q is not a valid duration: %w", windowStr, err)
	}
	if window <= 0 {
		return WindowConditionConfig{}, fmt.Errorf("count_in_window window must be positive, got %s", windowStr)
	}

	// Detect nested comparison operator and threshold (exactly one required)
	comparators := []string{"gt", "gte", "lt", "lte", "eq", "ne"}
	found := ""
	var threshold float64
	for _, cmp := range comparators {
		if raw, exists := m[cmp]; exists {
			if found != "" {
				return WindowConditionConfig{}, fmt.Errorf("count_in_window value has conflicting comparators: %q and %q — specify exactly one", found, cmp)
			}
			t, ok := toFloat64(raw)
			if !ok {
				return WindowConditionConfig{}, fmt.Errorf("count_in_window %s threshold must be numeric, got %T", cmp, raw)
			}
			found = cmp
			threshold = t
		}
	}
	if found == "" {
		return WindowConditionConfig{}, fmt.Errorf("count_in_window value missing comparison operator — add one of: gt, gte, lt, lte, eq, ne")
	}

	cfg := WindowConditionConfig{
		Window:     window,
		Comparator: found,
		Threshold:  threshold,
	}

	// Optional cardinality caps (validated by config_validation.go to be ≤ framework defaults)
	if raw, ok := m["max_dimensions"]; ok {
		v, isNum := toFloat64(raw)
		if !isNum {
			return WindowConditionConfig{}, fmt.Errorf("count_in_window max_dimensions must be numeric, got %T", raw)
		}
		cfg.MaxDimensions = int(v)
	}
	if raw, ok := m["max_per_window"]; ok {
		v, isNum := toFloat64(raw)
		if !isNum {
			return WindowConditionConfig{}, fmt.Errorf("count_in_window max_per_window must be numeric, got %T", raw)
		}
		cfg.MaxPerWindow = int(v)
	}

	return cfg, nil
}

// applyWindowComparison applies the nested comparison operator from the condition
// config to the count returned by Increment.
func applyWindowComparison(count int, cfg WindowConditionConfig) (bool, error) {
	c := float64(count)
	switch cfg.Comparator {
	case "gt":
		return c > cfg.Threshold, nil
	case "gte":
		return c >= cfg.Threshold, nil
	case "lt":
		return c < cfg.Threshold, nil
	case "lte":
		return c <= cfg.Threshold, nil
	case "eq":
		return c == cfg.Threshold, nil
	case "ne":
		return c != cfg.Threshold, nil
	default:
		return false, fmt.Errorf("count_in_window unknown comparator %q", cfg.Comparator)
	}
}

// makeCountInWindowOperator returns an OperatorFunc that evaluates count_in_window
// conditions using the provided WindowStateReader.
//
// The returned function:
//  1. Extracts the dimension from fieldValue (the resolved $caller.id value).
//  2. Parses the condition config from compareValue (a map[string]interface{}).
//  3. Calls reader.Increment to record the occurrence and get the rolling count.
//  4. Applies the nested comparison (gt/gte/etc.) and returns the result.
//
// A -1 count (cardinality cap hit) always returns false regardless of comparator.
// Field-absent semantics (nil fieldValue) return false without calling Increment.
func makeCountInWindowOperator(reader WindowStateReader, ruleID string) OperatorFunc {
	return func(fieldValue, compareValue interface{}) (bool, error) {
		// Dimension comes from the resolved field value. Nil or empty → absent
		// field semantic: return false without touching the counter so that a
		// missing $caller.id does not pollute the dimension cardinality map.
		if fieldValue == nil {
			return false, nil
		}
		dimension := strings.TrimSpace(fmt.Sprintf("%v", fieldValue))
		if dimension == "" {
			return false, nil
		}

		cfg, err := parseWindowConfig(compareValue)
		if err != nil {
			return false, &EvaluationError{
				Operator: OpCountInWindow,
				Message:  "invalid count_in_window config",
				Err:      err,
			}
		}

		ctx := context.Background()
		count, err := reader.Increment(ctx, ruleID, dimension, time.Now(), cfg.Window)
		if err != nil {
			return false, &EvaluationError{
				Operator: OpCountInWindow,
				Message:  "window state read failed",
				Err:      err,
			}
		}

		// Cardinality cap rejection sentinel — treat as non-match
		if count < 0 {
			return false, nil
		}

		return applyWindowComparison(count, cfg)
	}
}
