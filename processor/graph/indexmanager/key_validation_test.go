package indexmanager

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNATSKeyValidation tests the NATS key validation patterns to identify root cause
func TestNATSKeyValidation(t *testing.T) {
	validKeyRe := regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)

	testCases := []struct {
		name              string
		input             string
		expectedSanitized string
		shouldBeValid     bool
		description       string
	}{
		{
			name:              "simple_valid_key",
			input:             "test.predicate.1",
			expectedSanitized: "test.predicate.1",
			shouldBeValid:     true,
			description:       "Known good key should pass",
		},
		{
			name:              "robotics_system_id",
			input:             "robotics.system.id",
			expectedSanitized: "robotics.system.id",
			shouldBeValid:     true,
			description:       "E2E test predicate - should work",
		},
		{
			name:              "robotics_system_type",
			input:             "robotics.system.type",
			expectedSanitized: "robotics.system.type",
			shouldBeValid:     true,
			description:       "E2E test predicate - should work",
		},
		{
			name:              "robotics_flight_armed",
			input:             "robotics.flight.armed",
			expectedSanitized: "robotics.flight.armed",
			shouldBeValid:     true,
			description:       "E2E test predicate - should work",
		},
		{
			name:              "empty_string",
			input:             "",
			expectedSanitized: "unknown",
			shouldBeValid:     true,
			description:       "Empty input should become 'unknown'",
		},
		{
			name:              "with_spaces",
			input:             "test predicate with spaces",
			expectedSanitized: "test_predicate_with_spaces",
			shouldBeValid:     true,
			description:       "Spaces should become underscores",
		},
		{
			name:              "with_special_chars",
			input:             "test@predicate#with$special%chars",
			expectedSanitized: "testpredicatewithspecialchars",
			shouldBeValid:     true,
			description:       "Special chars should be removed",
		},
		{
			name:              "consecutive_dots",
			input:             "test..predicate...with....dots",
			expectedSanitized: "test.predicate.with.dots",
			shouldBeValid:     true,
			description:       "Consecutive dots should be collapsed",
		},
		{
			name:              "leading_trailing_dots",
			input:             ".test.predicate.trailing.",
			expectedSanitized: "test.predicate.trailing",
			shouldBeValid:     true,
			description:       "Leading/trailing dots should be trimmed",
		},
		{
			name:              "only_dots",
			input:             "....",
			expectedSanitized: "unknown",
			shouldBeValid:     true,
			description:       "Only dots should become 'unknown'",
		},
		{
			name:              "unicode_chars",
			input:             "test.predicate.with.unicode.ñáéíóú",
			expectedSanitized: "test.predicate.with.unicode",
			shouldBeValid:     true,
			description:       "Unicode chars should be removed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test sanitization
			sanitized := sanitizeNATSKey(tc.input)
			assert.Equal(t, tc.expectedSanitized, sanitized,
				"Sanitization failed for input: %q", tc.input)

			// Test NATS pattern validation
			isValid := validKeyRe.MatchString(sanitized)
			assert.Equal(t, tc.shouldBeValid, isValid,
				"NATS pattern validation failed for sanitized key: %q (from input: %q)",
				sanitized, tc.input)

			// Log for debugging
			t.Logf("Input: %q -> Sanitized: %q -> Valid: %t (%s)",
				tc.input, sanitized, isValid, tc.description)

			// Additional character analysis for failing cases
			if !isValid && tc.shouldBeValid {
				t.Logf("Character analysis for failing key %q:", sanitized)
				for i, c := range sanitized {
					t.Logf("  Char %d: %q (rune=%d)", i, string(c), c)
				}
			}
		})
	}
}

// TestSanitizeNATSKeyEdgeCases tests edge cases in the sanitization function
func TestSanitizeNATSKeyEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"nil_equivalent", "", "unknown"},
		{"whitespace_only", "   ", "___"}, // spaces become underscores
		{"tabs_and_newlines", "test\tpredicate\nwith\rwhitespace", "testpredicatewithwhitespace"},
		{"very_long_key", string(make([]byte, 300)), "unknown"}, // All null bytes -> removed -> empty -> unknown
		{"all_valid_chars", "test-predicate_with/valid=chars.123", "test-predicate_with/valid=chars.123"},
		{"mixed_valid_invalid", "test@predicate-valid_chars/ok=123.end", "testpredicate-valid_chars/ok=123.end"},
	}

	validKeyRe := regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeNATSKey(tt.input)
			assert.Equal(t, tt.expected, result)

			// Every sanitized key should be valid
			isValid := validKeyRe.MatchString(result)
			assert.True(t, isValid, "Sanitized key %q should be valid", result)

			t.Logf("Input: %q (len=%d) -> Output: %q (len=%d) -> Valid: %t",
				tt.input, len(tt.input), result, len(result), isValid)
		})
	}
}

// TestNATSPatternExamples tests the exact NATS pattern against known examples
func TestNATSPatternExamples(t *testing.T) {
	validKeyRe := regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)

	validKeys := []string{
		"test",
		"test.key",
		"test-key",
		"test_key",
		"test/key",
		"test=key",
		"123",
		"test123",
		"TEST",
		"Test.Key_With-All/Valid=Chars123",
		"robotics.system.id",
		"robotics.system.type",
		"robotics.flight.armed",
	}

	invalidKeys := []string{
		"",          // empty - but our sanitizer handles this
		"test key",  // space
		"test@key",  // @
		"test#key",  // #
		"test$key",  // $
		"test%key",  // %
		"test^key",  // ^
		"test&key",  // &
		"test*key",  // *
		"test(key",  // (
		"test)key",  // )
		"test+key",  // +
		"test[key",  // [
		"test]key",  // ]
		"test{key",  // {
		"test}key",  // }
		"test|key",  // |
		"test\\key", // \
		"test:key",  // :
		"test;key",  // ;
		"test\"key", // "
		"test'key",  // '
		"test<key",  // <
		"test>key",  // >
		"test,key",  // ,
		"test?key",  // ?
	}

	t.Run("valid_keys", func(t *testing.T) {
		for _, key := range validKeys {
			isValid := validKeyRe.MatchString(key)
			assert.True(t, isValid, "Key %q should be valid", key)
		}
	})

	t.Run("invalid_keys", func(t *testing.T) {
		for _, key := range invalidKeys {
			isValid := validKeyRe.MatchString(key)
			assert.False(t, isValid, "Key %q should be invalid", key)
		}
	})
}
