package timestamp_test

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/pkg/timestamp"
)

// ExampleNow demonstrates getting the current timestamp
func ExampleNow() {
	ts := timestamp.Now()
	fmt.Printf("Current timestamp: %d (milliseconds)\n", ts)
	// Output would vary, so we'll just show the format
}

// ExampleParse demonstrates parsing various timestamp formats
func ExampleParse() {
	// Parse RFC3339 string
	ts1 := timestamp.Parse("2023-01-15T12:30:45Z")
	fmt.Printf("RFC3339 parsed: %d\n", ts1)

	// Parse Unix seconds
	ts2 := timestamp.Parse(int64(1673784645))
	fmt.Printf("Unix seconds parsed: %d\n", ts2)

	// Parse Unix milliseconds
	ts3 := timestamp.Parse(int64(1673784645123))
	fmt.Printf("Unix milliseconds parsed: %d\n", ts3)

	// Output:
	// RFC3339 parsed: 1673785845000
	// Unix seconds parsed: 1673784645000
	// Unix milliseconds parsed: 1673784645123
}

// ExampleFormat demonstrates formatting timestamps for display
func ExampleFormat() {
	ts := int64(1673785845123)
	formatted := timestamp.Format(ts)
	fmt.Printf("Formatted: %s\n", formatted)

	// Zero timestamp returns empty string
	empty := timestamp.Format(0)
	fmt.Printf("Zero formatted: '%s'\n", empty)

	// Output:
	// Formatted: 2023-01-15T12:30:45Z
	// Zero formatted: ''
}

// ExampleToUnixMs demonstrates converting time.Time to milliseconds
func ExampleToUnixMs() {
	t := time.Date(2023, 1, 15, 12, 30, 45, 123000000, time.UTC)
	ts := timestamp.ToUnixMs(t)
	fmt.Printf("time.Time to milliseconds: %d\n", ts)

	// Output:
	// time.Time to milliseconds: 1673785845123
}

// ExampleFromUnixMs demonstrates converting milliseconds to time.Time
func ExampleFromUnixMs() {
	ts := int64(1673785845123)
	t := timestamp.FromUnixMs(ts)
	fmt.Printf("Milliseconds to time.Time: %s\n", t.UTC().Format(time.RFC3339))

	// Zero timestamp returns zero time
	zeroTime := timestamp.FromUnixMs(0)
	fmt.Printf("Zero timestamp: %v\n", zeroTime.IsZero())

	// Output:
	// Milliseconds to time.Time: 2023-01-15T12:30:45Z
	// Zero timestamp: true
}

// ExampleAdd demonstrates timestamp arithmetic
func ExampleAdd() {
	ts := int64(1673785845123)

	// Add 1 hour
	future := timestamp.Add(ts, time.Hour)
	fmt.Printf("Original: %s\n", timestamp.Format(ts))
	fmt.Printf("Plus 1 hour: %s\n", timestamp.Format(future))

	// Add to zero timestamp returns zero
	zero := timestamp.Add(0, time.Hour)
	fmt.Printf("Add to zero: %d\n", zero)

	// Output:
	// Original: 2023-01-15T12:30:45Z
	// Plus 1 hour: 2023-01-15T13:30:45Z
	// Add to zero: 0
}

// ExampleBetween demonstrates calculating duration between timestamps
func ExampleBetween() {
	start := int64(1673785845123)
	end := timestamp.Add(start, 30*time.Minute)

	duration := timestamp.Between(start, end)
	fmt.Printf("Duration: %v\n", duration)

	// Zero timestamps return zero duration
	zeroDuration := timestamp.Between(0, end)
	fmt.Printf("With zero: %v\n", zeroDuration)

	// Output:
	// Duration: 30m0s
	// With zero: 0s
}
