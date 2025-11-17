// Package timestamp provides standardized Unix timestamp handling utilities.
//
// This package uses int64 milliseconds as the canonical timestamp format to
// eliminate timestamp parsing bugs and provide consistent behavior across the
// codebase. All timestamps are stored as milliseconds since Unix epoch (UTC).
//
// Zero Value Semantics:
//   - A timestamp value of 0 means "not set" or "unknown"
//   - Functions handle zero values gracefully, returning appropriate defaults
//
// Migration Guide:
//   - Replace time.Now().Unix() with timestamp.Now()
//   - Replace time.Unix(sec, 0) with timestamp.FromUnixMs(sec * 1000)
//   - Replace RFC3339 string parsing with timestamp.Parse()
//
// Usage Examples:
//
//	// Current time
//	now := timestamp.Now()
//
//	// Convert from time.Time
//	t := time.Now()
//	ts := timestamp.ToUnixMs(t)
//
//	// Convert to time.Time
//	t := timestamp.FromUnixMs(ts)
//
//	// Format for display
//	display := timestamp.Format(ts)
//
//	// Parse various formats
//	ts := timestamp.Parse("2023-01-01T12:00:00Z")
//	ts := timestamp.Parse(1672574400000)
package timestamp

import (
	"fmt"
	"strconv"
	"time"
)

// Now returns the current time as Unix milliseconds.
func Now() int64 {
	return time.Now().UnixMilli()
}

// ToUnixMs converts a time.Time to Unix milliseconds.
func ToUnixMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// FromUnixMs converts Unix milliseconds to time.Time.
// Returns zero time if timestamp is 0.
func FromUnixMs(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// ToTime is an alias for FromUnixMs for better readability.
func ToTime(ms int64) time.Time {
	return FromUnixMs(ms)
}

// Format converts Unix milliseconds to RFC3339 string for display.
// Returns empty string if timestamp is 0.
func Format(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// Parse converts various timestamp formats to Unix milliseconds.
// Supports:
//   - int64 (assumed to be milliseconds if > 1e12, otherwise seconds)
//   - float64 (converted to int64, same logic as int64)
//   - string (RFC3339 or Unix timestamp string)
//   - time.Time
//   - nil/zero values (returns 0)
//
// Returns 0 for invalid input or parsing errors.
func Parse(input any) int64 {
	if input == nil {
		return 0
	}

	switch v := input.(type) {
	case int64:
		if v == 0 {
			return 0
		}
		// If value is greater than 1e12 (year 2001 in seconds), assume milliseconds
		// Otherwise assume seconds and convert
		if v > 1e12 {
			return v
		}
		return v * 1000

	case float64:
		if v == 0 {
			return 0
		}
		// Same logic as int64
		if v > 1e12 {
			return int64(v)
		}
		return int64(v * 1000)

	case int:
		return Parse(int64(v))

	case int32:
		return Parse(int64(v))

	case string:
		if v == "" {
			return 0
		}

		// Try parsing as RFC3339 first
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return ToUnixMs(t)
		}

		// Try parsing as Unix timestamp string
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			return Parse(ts)
		}

		// Try parsing as float string
		if ts, err := strconv.ParseFloat(v, 64); err == nil {
			return Parse(ts)
		}

		return 0

	case time.Time:
		return ToUnixMs(v)

	case *time.Time:
		if v == nil {
			return 0
		}
		return ToUnixMs(*v)

	default:
		return 0
	}
}

// IsZero checks if a timestamp is unset (zero).
func IsZero(ms int64) bool {
	return ms == 0
}

// Since returns the duration since the given timestamp.
// Returns 0 if timestamp is zero.
func Since(ms int64) time.Duration {
	if ms == 0 {
		return 0
	}
	return time.Since(time.UnixMilli(ms))
}

// Add adds a duration to a timestamp and returns the new timestamp.
// Returns 0 if the input timestamp is zero.
func Add(ms int64, d time.Duration) int64 {
	if ms == 0 {
		return 0
	}
	return time.UnixMilli(ms).Add(d).UnixMilli()
}

// Sub subtracts a duration from a timestamp and returns the new timestamp.
// Returns 0 if the input timestamp is zero.
func Sub(ms int64, d time.Duration) int64 {
	if ms == 0 {
		return 0
	}
	return time.UnixMilli(ms).Add(-d).UnixMilli()
}

// Between returns the duration between two timestamps.
// Returns 0 if either timestamp is zero.
func Between(start, end int64) time.Duration {
	if start == 0 || end == 0 {
		return 0
	}
	return time.UnixMilli(end).Sub(time.UnixMilli(start))
}

// Min returns the earlier of two timestamps.
// Zero values are treated as "later than any other time".
func Min(a, b int64) int64 {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

// Max returns the later of two timestamps.
// Zero values are treated as "earlier than any other time".
func Max(a, b int64) int64 {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a > b {
		return a
	}
	return b
}

// Validate checks if a timestamp is valid (non-negative and reasonable).
// Returns an error if the timestamp is negative or unreasonably large.
func Validate(ms int64) error {
	if ms < 0 {
		return fmt.Errorf("timestamp cannot be negative: %d", ms)
	}
	// Check if timestamp is unreasonably far in the future (year 3000)
	if ms > 32503680000000 {
		return fmt.Errorf("timestamp too far in future: %d", ms)
	}
	return nil
}
