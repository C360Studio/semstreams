package swecommon

import (
	"fmt"
	"strconv"
	"time"
)

// Values is one record's worth of typed field values, keyed by
// field name. Missing keys are treated as nil (encoders emit the
// schema's nilValue or JSON null). Per-field Go types:
//
//   - Quantity → float64
//   - Count    → int64
//   - Time     → time.Time (or RFC 3339 string — encoders convert)
//   - Boolean  → bool
//   - Text     → string
//   - Category → string
//
// Encoders are tolerant on input: a Quantity field accepts any
// numeric type (int, int32, int64, float32, float64) and rounds
// through float64; a Time field accepts either time.Time or an
// RFC 3339 string. Decoders always populate the canonical type.
type Values map[string]any

// normalizeValue coerces a Go value into the canonical Go type for
// the given component, returning (canonicalValue, isNil, error).
// Used by every encoder so input-shape tolerance lives in one
// place. isNil true means the value should be emitted as the
// schema's nilValue token (or wire null when no nilValue is set).
func normalizeValue(c DataComponent, v any) (any, bool, error) {
	if v == nil {
		return nil, true, nil
	}
	switch c.Kind() {
	case KindQuantity:
		f, err := coerceFloat(v)
		if err != nil {
			return nil, false, fmt.Errorf("Quantity: %w", err)
		}
		return f, false, nil
	case KindCount:
		i, err := coerceInt(v)
		if err != nil {
			return nil, false, fmt.Errorf("Count: %w", err)
		}
		return i, false, nil
	case KindTime:
		s, err := coerceTime(v)
		if err != nil {
			return nil, false, fmt.Errorf("Time: %w", err)
		}
		return s, false, nil
	case KindBoolean:
		b, err := coerceBool(v)
		if err != nil {
			return nil, false, fmt.Errorf("Boolean: %w", err)
		}
		return b, false, nil
	case KindText:
		s, ok := v.(string)
		if !ok {
			return nil, false, fmt.Errorf("Text: want string, got %T", v)
		}
		return s, false, nil
	case KindCategory:
		s, ok := v.(string)
		if !ok {
			return nil, false, fmt.Errorf("Category: want string, got %T", v)
		}
		return s, false, nil
	case KindRecord:
		// Nested records are not supported in Phase 1 value flow —
		// the framework MVP carries one flat record per row, which
		// is what every CS API observation/command consumer needs.
		// Nested-record support is straightforward to add later by
		// recursing through normalizeValue.
		return nil, false, fmt.Errorf("nested DataRecord values not supported in Phase 1")
	default:
		return nil, false, fmt.Errorf("unknown component kind %q", c.Kind())
	}
}

// coerceFloat converts any numeric Go type to float64. Strings are
// parsed (so the CSV/binary decoders can route through here too).
func coerceFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("parse float %q: %w", n, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("want number, got %T", v)
	}
}

// coerceInt converts any integer Go type (or numerically-integer
// float) to int64. Float inputs are accepted when they round-trip
// losslessly to int64.
func coerceInt(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case uint:
		return int64(n), nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case float32:
		i := int64(n)
		if float32(i) != n {
			return 0, fmt.Errorf("float %v not exactly representable as int64", n)
		}
		return i, nil
	case float64:
		i := int64(n)
		if float64(i) != n {
			return 0, fmt.Errorf("float %v not exactly representable as int64", n)
		}
		return i, nil
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse int %q: %w", n, err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("want integer, got %T", v)
	}
}

// coerceTime converts a Go time.Time or RFC 3339 string to its
// canonical RFC 3339 Nano string for the wire. RFC 3339 is the
// JSON-friendly subset of ISO 8601 the SWE Common JSON encoding
// uses for Time values.
func coerceTime(v any) (string, error) {
	switch n := v.(type) {
	case time.Time:
		return n.UTC().Format(time.RFC3339Nano), nil
	case string:
		// Validate by re-parsing — round-trip-stable strings only.
		if _, err := time.Parse(time.RFC3339Nano, n); err != nil {
			if _, err2 := time.Parse(time.RFC3339, n); err2 != nil {
				return "", fmt.Errorf("parse time %q: %w", n, err)
			}
		}
		return n, nil
	default:
		return "", fmt.Errorf("want time.Time or RFC 3339 string, got %T", v)
	}
}

// coerceBool converts a Go bool or canonical SWE bool string
// ("true"/"false") to bool.
func coerceBool(v any) (bool, error) {
	switch n := v.(type) {
	case bool:
		return n, nil
	case string:
		b, err := strconv.ParseBool(n)
		if err != nil {
			return false, fmt.Errorf("parse bool %q: %w", n, err)
		}
		return b, nil
	default:
		return false, fmt.Errorf("want bool, got %T", v)
	}
}

// fieldValue retrieves the value for a field name, treating an
// absent key as nil.
func fieldValue(row Values, name string) any {
	if row == nil {
		return nil
	}
	v, ok := row[name]
	if !ok {
		return nil
	}
	return v
}

// matchesNilToken reports whether the given input value should be
// treated as Go nil for emission, given the component's declared
// nilValue token. Returns false when nilValue is empty (no
// stand-in declared) or v is already nil.
//
// String inputs match verbatim. Numeric inputs are formatted to
// their canonical wire form (the same form the encoder would
// otherwise emit) and compared — so a Quantity field with
// nilValue "-9999" matches an input of float64(-9999), int(-9999),
// and the string "-9999" identically. Boolean inputs are formatted
// via strconv.FormatBool. Time inputs match against the canonical
// RFC 3339 nano string.
func matchesNilToken(c DataComponent, v any) bool {
	nilStr := nilValueOf(c)
	if nilStr == "" || v == nil {
		return false
	}
	if asStr, ok := v.(string); ok {
		return asStr == nilStr
	}
	switch c.Kind() {
	case KindQuantity:
		f, err := coerceFloat(v)
		if err != nil {
			return false
		}
		return strconv.FormatFloat(f, 'f', -1, 64) == nilStr
	case KindCount:
		i, err := coerceInt(v)
		if err != nil {
			return false
		}
		return strconv.FormatInt(i, 10) == nilStr
	case KindTime:
		s, err := coerceTime(v)
		if err != nil {
			return false
		}
		return s == nilStr
	case KindBoolean:
		b, err := coerceBool(v)
		if err != nil {
			return false
		}
		return strconv.FormatBool(b) == nilStr
	default:
		return false
	}
}
