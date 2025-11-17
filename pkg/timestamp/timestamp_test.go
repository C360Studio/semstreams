package timestamp

import (
	"testing"
	"time"
)

// Test constants
var (
	testTime       = time.Date(2023, 1, 15, 12, 30, 45, 123000000, time.UTC) // Use exact milliseconds
	testTimeMs     = int64(1673785845123)                                    // Correct timestamp for the date above
	testTimeString = "2023-01-15T12:30:45Z"
)

func TestNow(t *testing.T) {
	before := time.Now().UnixMilli()
	ts := Now()
	after := time.Now().UnixMilli()

	if ts < before || ts > after {
		t.Errorf("Now() = %d, expected between %d and %d", ts, before, after)
	}
}

func TestToUnixMs(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected int64
	}{
		{
			name:     "normal time",
			input:    testTime,
			expected: testTimeMs,
		},
		{
			name:     "zero time",
			input:    time.Time{},
			expected: 0,
		},
		{
			name:     "unix epoch",
			input:    time.Unix(0, 0),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToUnixMs(tt.input)
			if result != tt.expected {
				t.Errorf("ToUnixMs(%v) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFromUnixMs(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected time.Time
	}{
		{
			name:     "normal timestamp",
			input:    testTimeMs,
			expected: time.UnixMilli(testTimeMs),
		},
		{
			name:     "zero timestamp",
			input:    0,
			expected: time.Time{},
		},
		{
			name:     "negative timestamp",
			input:    -1000,
			expected: time.UnixMilli(-1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FromUnixMs(tt.input)
			if !result.Equal(tt.expected) {
				t.Errorf("FromUnixMs(%d) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToTime(t *testing.T) {
	// ToTime is an alias for FromUnixMs, so test basic functionality
	result := ToTime(testTimeMs)
	expected := time.UnixMilli(testTimeMs)

	if !result.Equal(expected) {
		t.Errorf("ToTime(%d) = %v, expected %v", testTimeMs, result, expected)
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{
			name:     "normal timestamp",
			input:    testTimeMs,
			expected: "2023-01-15T12:30:45Z",
		},
		{
			name:     "zero timestamp",
			input:    0,
			expected: "",
		},
		{
			name:     "unix epoch",
			input:    0,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%d) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int64
	}{
		// int64 tests
		{
			name:     "int64 milliseconds",
			input:    int64(1673785845123),
			expected: 1673785845123,
		},
		{
			name:     "int64 seconds",
			input:    int64(1673784645),
			expected: 1673784645000,
		},
		{
			name:     "int64 zero",
			input:    int64(0),
			expected: 0,
		},

		// float64 tests
		{
			name:     "float64 milliseconds",
			input:    float64(1673785845123),
			expected: 1673785845123,
		},
		{
			name:     "float64 seconds",
			input:    float64(1673784645),
			expected: 1673784645000,
		},
		{
			name:     "float64 zero",
			input:    float64(0),
			expected: 0,
		},

		// int tests
		{
			name:     "int seconds",
			input:    int(1673784645),
			expected: 1673784645000,
		},

		// int32 tests
		{
			name:     "int32 seconds",
			input:    int32(1673784645),
			expected: 1673784645000,
		},

		// string tests
		{
			name:     "RFC3339 string",
			input:    "2023-01-15T12:30:45Z",
			expected: 1673785845000,
		},
		{
			name:     "RFC3339 with milliseconds",
			input:    "2023-01-15T12:30:45.123Z",
			expected: 1673785845123,
		},
		{
			name:     "unix timestamp string seconds",
			input:    "1673784645",
			expected: 1673784645000,
		},
		{
			name:     "unix timestamp string milliseconds",
			input:    "1673785845123",
			expected: 1673785845123,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "invalid string",
			input:    "invalid",
			expected: 0,
		},

		// time.Time tests
		{
			name:     "time.Time",
			input:    time.UnixMilli(1673785845123),
			expected: 1673785845123,
		},
		{
			name:     "zero time.Time",
			input:    time.Time{},
			expected: 0,
		},

		// *time.Time tests
		{
			name:     "*time.Time",
			input:    &testTime,
			expected: testTimeMs,
		},
		{
			name:     "nil *time.Time",
			input:    (*time.Time)(nil),
			expected: 0,
		},

		// nil and unsupported types
		{
			name:     "nil",
			input:    nil,
			expected: 0,
		},
		{
			name:     "unsupported type",
			input:    []int{1, 2, 3},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.input)
			if result != tt.expected {
				t.Errorf("Parse(%v) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected bool
	}{
		{
			name:     "zero timestamp",
			input:    0,
			expected: true,
		},
		{
			name:     "non-zero timestamp",
			input:    1673785845123,
			expected: false,
		},
		{
			name:     "negative timestamp",
			input:    -1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsZero(tt.input)
			if result != tt.expected {
				t.Errorf("IsZero(%d) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSince(t *testing.T) {
	// Test with a timestamp from 1 second ago
	oneSecondAgo := time.Now().Add(-time.Second).UnixMilli()
	duration := Since(oneSecondAgo)

	// Should be approximately 1 second, allow for some variance
	if duration < 900*time.Millisecond || duration > 1100*time.Millisecond {
		t.Errorf("Since(%d) = %v, expected approximately 1 second", oneSecondAgo, duration)
	}

	// Test with zero timestamp
	zeroDuration := Since(0)
	if zeroDuration != 0 {
		t.Errorf("Since(0) = %v, expected 0", zeroDuration)
	}
}

func TestAdd(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		duration time.Duration
		expected int64
	}{
		{
			name:     "add hour",
			input:    testTimeMs,
			duration: time.Hour,
			expected: testTimeMs + 3600000, // 1 hour in milliseconds
		},
		{
			name:     "zero timestamp",
			input:    0,
			duration: time.Hour,
			expected: 0,
		},
		{
			name:     "add negative duration",
			input:    testTimeMs,
			duration: -time.Hour,
			expected: testTimeMs - 3600000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Add(tt.input, tt.duration)
			if result != tt.expected {
				t.Errorf("Add(%d, %v) = %d, expected %d", tt.input, tt.duration, result, tt.expected)
			}
		})
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		duration time.Duration
		expected int64
	}{
		{
			name:     "subtract hour",
			input:    testTimeMs,
			duration: time.Hour,
			expected: testTimeMs - 3600000, // 1 hour in milliseconds
		},
		{
			name:     "zero timestamp",
			input:    0,
			duration: time.Hour,
			expected: 0,
		},
		{
			name:     "subtract negative duration",
			input:    testTimeMs,
			duration: -time.Hour,
			expected: testTimeMs + 3600000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Sub(tt.input, tt.duration)
			if result != tt.expected {
				t.Errorf("Sub(%d, %v) = %d, expected %d", tt.input, tt.duration, result, tt.expected)
			}
		})
	}
}

func TestBetween(t *testing.T) {
	start := testTimeMs
	end := testTimeMs + 5000 // 5 seconds later

	tests := []struct {
		name     string
		start    int64
		end      int64
		expected time.Duration
	}{
		{
			name:     "normal duration",
			start:    start,
			end:      end,
			expected: 5 * time.Second,
		},
		{
			name:     "zero start",
			start:    0,
			end:      end,
			expected: 0,
		},
		{
			name:     "zero end",
			start:    start,
			end:      0,
			expected: 0,
		},
		{
			name:     "both zero",
			start:    0,
			end:      0,
			expected: 0,
		},
		{
			name:     "reverse order",
			start:    end,
			end:      start,
			expected: -5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Between(tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("Between(%d, %d) = %v, expected %v", tt.start, tt.end, result, tt.expected)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a        int64
		b        int64
		expected int64
	}{
		{
			name:     "a smaller",
			a:        1000,
			b:        2000,
			expected: 1000,
		},
		{
			name:     "b smaller",
			a:        2000,
			b:        1000,
			expected: 1000,
		},
		{
			name:     "equal",
			a:        1000,
			b:        1000,
			expected: 1000,
		},
		{
			name:     "a zero",
			a:        0,
			b:        1000,
			expected: 1000,
		},
		{
			name:     "b zero",
			a:        1000,
			b:        0,
			expected: 1000,
		},
		{
			name:     "both zero",
			a:        0,
			b:        0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		name     string
		a        int64
		b        int64
		expected int64
	}{
		{
			name:     "a larger",
			a:        2000,
			b:        1000,
			expected: 2000,
		},
		{
			name:     "b larger",
			a:        1000,
			b:        2000,
			expected: 2000,
		},
		{
			name:     "equal",
			a:        1000,
			b:        1000,
			expected: 1000,
		},
		{
			name:     "a zero",
			a:        0,
			b:        1000,
			expected: 1000,
		},
		{
			name:     "b zero",
			a:        1000,
			b:        0,
			expected: 1000,
		},
		{
			name:     "both zero",
			a:        0,
			b:        0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		input       int64
		expectError bool
	}{
		{
			name:        "valid timestamp",
			input:       testTimeMs,
			expectError: false,
		},
		{
			name:        "zero timestamp",
			input:       0,
			expectError: false,
		},
		{
			name:        "negative timestamp",
			input:       -1000,
			expectError: true,
		},
		{
			name:        "far future timestamp",
			input:       32503680000001, // Year 3000 + 1ms
			expectError: true,
		},
		{
			name:        "year 3000 exactly",
			input:       32503680000000,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.input)
			if tt.expectError && err == nil {
				t.Errorf("Validate(%d) expected error but got none", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Validate(%d) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestRoundTripAccuracy(t *testing.T) {
	// Test that converting back and forth preserves accuracy
	original := time.Now()
	ms := ToUnixMs(original)
	recovered := FromUnixMs(ms)

	// Due to millisecond precision, we expect some loss in nanoseconds
	diff := original.Sub(recovered).Abs()
	if diff >= time.Millisecond {
		t.Errorf("Round trip lost too much precision: %v", diff)
	}
}

func TestParseEdgeCases(t *testing.T) {
	// Test boundary between seconds and milliseconds interpretation
	boundary := int64(1e12)

	// Just under boundary should be treated as seconds
	result := Parse(boundary - 1)
	expected := (boundary - 1) * 1000
	if result != expected {
		t.Errorf("Parse(%d) = %d, expected %d", boundary-1, result, expected)
	}

	// Just over boundary should be treated as milliseconds
	result = Parse(boundary + 1)
	expected = boundary + 1
	if result != expected {
		t.Errorf("Parse(%d) = %d, expected %d", boundary+1, result, expected)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	// Test that Format and Parse are inverse operations (with precision loss)
	original := testTimeMs
	formatted := Format(original)
	parsed := Parse(formatted)

	// Allow for some precision loss due to RFC3339 format limitations
	diff := original - parsed
	if diff < 0 {
		diff = -diff
	}
	if diff >= 1000 { // Allow up to 1 second difference due to format precision
		t.Errorf(
			"Format/Parse round trip lost too much precision: original=%d, parsed=%d, diff=%d",
			original,
			parsed,
			diff,
		)
	}
}

// Benchmark tests
func BenchmarkNow(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Now()
	}
}

func BenchmarkToUnixMs(b *testing.B) {
	t := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ToUnixMs(t)
	}
}

func BenchmarkFromUnixMs(b *testing.B) {
	ts := testTimeMs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FromUnixMs(ts)
	}
}

func BenchmarkFormat(b *testing.B) {
	ts := testTimeMs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Format(ts)
	}
}

func BenchmarkParseString(b *testing.B) {
	s := "2023-01-15T12:30:45Z"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Parse(s)
	}
}

func BenchmarkParseInt64(b *testing.B) {
	ts := testTimeMs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Parse(ts)
	}
}
