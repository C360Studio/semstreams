package expression

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateRegexComplexity tests ReDoS protection validation
func TestValidateRegexComplexity(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		shouldFail bool
		errorMsg   string
	}{
		// Dangerous patterns that should be rejected
		{
			name:       "nested_quantifiers_overlap",
			pattern:    `(\w+)*\w`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "nested_quantifiers_simple",
			pattern:    `(\w*)+`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "classic_redos",
			pattern:    `(a+)+`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "nested_wildcards_star",
			pattern:    `(.*)*`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "nested_wildcards_plus",
			pattern:    `(.+)+`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "nested_whitespace",
			pattern:    `(\s+)*\s`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "nested_negated_class",
			pattern:    `([^,]+)*[^,]`,
			shouldFail: true,
			errorMsg:   "nested quantifiers",
		},
		{
			name:       "excessive_length",
			pattern:    strings.Repeat("a", 501),
			shouldFail: true,
			errorMsg:   "too long",
		},
		{
			name:       "excessive_repetition",
			pattern:    `a{1000,}`,
			shouldFail: true,
			errorMsg:   "excessive repetition count",
		},
		{
			name:       "too_many_capture_groups",
			pattern:    strings.Repeat("(a)", 21),
			shouldFail: true,
			errorMsg:   "too many capture groups",
		},
		{
			name:       "excessive_nesting",
			pattern:    `((((((a))))))`,
			shouldFail: true,
			errorMsg:   "excessive nesting depth",
		},

		// Safe patterns that should be allowed
		{
			name:       "simple_literal",
			pattern:    `drone`,
			shouldFail: false,
		},
		{
			name:       "anchored_pattern",
			pattern:    `^drone-[0-9]+$`,
			shouldFail: false,
		},
		{
			name:       "character_class",
			pattern:    `[a-zA-Z0-9]+`,
			shouldFail: false,
		},
		{
			name:       "simple_alternation",
			pattern:    `(drone|robot|uav)`,
			shouldFail: false,
		},
		{
			name:       "safe_repetition",
			pattern:    `a{1,10}`,
			shouldFail: false,
		},
		{
			name:       "word_boundary",
			pattern:    `\bword\b`,
			shouldFail: false,
		},
		{
			name:       "lookahead",
			pattern:    `(?=.*digit)`,
			shouldFail: false,
		},
		{
			name:       "max_safe_length",
			pattern:    strings.Repeat("a", 500),
			shouldFail: false,
		},
		{
			name:       "max_safe_groups",
			pattern:    strings.Repeat("(a)", 20),
			shouldFail: false,
		},
		{
			name:       "max_safe_nesting",
			pattern:    `(((((a)))))`,
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexComplexity(tt.pattern)
			if tt.shouldFail {
				assert.Error(t, err, "Pattern should be rejected: %s", tt.pattern)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error should mention: %s", tt.errorMsg)
				}
			} else {
				assert.NoError(t, err, "Pattern should be accepted: %s", tt.pattern)
			}
		})
	}
}

// TestCompileRegex tests the regex compilation with caching
func TestCompileRegex(t *testing.T) {
	// Clear cache before tests
	clearCache()

	t.Run("successful_compilation", func(t *testing.T) {
		pattern := "^test-[0-9]+$"
		re, err := compileRegex(pattern)
		require.NoError(t, err)
		require.NotNil(t, re)

		// Verify it works
		assert.True(t, re.MatchString("test-123"))
		assert.False(t, re.MatchString("test-abc"))
	})

	t.Run("cache_hit", func(t *testing.T) {
		clearCache()
		pattern := "cached"

		// First call - cache miss
		re1, err := compileRegex(pattern)
		require.NoError(t, err)
		initialSize := cacheSize()
		assert.Equal(t, 1, initialSize)

		// Second call - cache hit (same object returned)
		re2, err := compileRegex(pattern)
		require.NoError(t, err)
		assert.Same(t, re1, re2, "Should return same cached regex object")
		assert.Equal(t, initialSize, cacheSize(), "Cache size shouldn't change")
	})

	t.Run("rejects_dangerous_pattern", func(t *testing.T) {
		pattern := "(a+)+"
		_, err := compileRegex(pattern)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nested quantifiers")
	})

	t.Run("invalid_regex_syntax", func(t *testing.T) {
		pattern := "[unclosed"
		_, err := compileRegex(pattern)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid regex pattern")
	})
}

// TestRegexCache_CacheEviction tests LRU eviction behavior
func TestRegexCache_CacheEviction(t *testing.T) {
	// Note: This test assumes cache max size is 100 (from implementation)
	clearCache()

	// Fill cache to capacity
	for i := 0; i < 100; i++ {
		pattern := fmt.Sprintf("pattern%d", i)
		_, err := compileRegex(pattern)
		require.NoError(t, err)
	}

	assert.Equal(t, 100, cacheSize(), "Cache should be at max capacity")

	// Add one more - should trigger eviction
	_, err := compileRegex("pattern100")
	require.NoError(t, err)

	// Cache size should still be at max (one was evicted)
	assert.LessOrEqual(t, cacheSize(), 100, "Cache should not exceed max size")
}

// TestRegexCache_Concurrency tests thread safety
func TestRegexCache_Concurrency(t *testing.T) {
	clearCache()
	pattern := "concurrent.*test"

	// Launch multiple goroutines to compile the same pattern
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			re, err := compileRegex(pattern)
			assert.NoError(t, err)
			assert.NotNil(t, re)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should only have one entry in cache
	assert.Equal(t, 1, cacheSize())
}

// TestRegexCache_NoHang tests that ReDoS patterns don't hang
func TestRegexCache_NoHang(t *testing.T) {
	dangerousPatterns := []string{
		"(a+)+$",
		"(.*)*anything",
		"([a-zA-Z]+)*",
		"(\\d+)*\\d",
	}

	for _, pattern := range dangerousPatterns {
		t.Run(pattern, func(t *testing.T) {
			// Should reject immediately without hanging
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			done := make(chan bool)
			go func() {
				_, err := compileRegex(pattern)
				assert.Error(t, err, "Should reject dangerous pattern")
				done <- true
			}()

			select {
			case <-done:
				// Good - completed quickly
			case <-ctx.Done():
				t.Fatal("Validation took too long - possible hang")
			}
		})
	}
}

// TestRegexCache_Statistics tests that cache stats are available
func TestRegexCache_Statistics(t *testing.T) {
	clearCache()

	// Get initial stats
	stats := cacheStats()
	assert.NotNil(t, stats, "Stats should always be available")

	// Perform some operations
	_, err := compileRegex("test1")
	require.NoError(t, err)

	// Cache hit
	_, err = compileRegex("test1")
	require.NoError(t, err)

	// Cache miss
	_, err = compileRegex("test2")
	require.NoError(t, err)

	// Stats should reflect operations
	newStats := cacheStats()
	assert.NotNil(t, newStats)
	// The cache package tracks hits/misses internally
}

// BenchmarkCompileRegex benchmarks regex compilation with caching
func BenchmarkCompileRegex(b *testing.B) {
	clearCache()
	patterns := []string{
		"^drone-[0-9]+$",
		"battery.*low",
		"(error|warning|critical)",
		"\\btest\\b",
		"[a-zA-Z0-9]+",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern := patterns[i%len(patterns)]
		_, err := compileRegex(pattern)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateRegexComplexity benchmarks validation overhead
func BenchmarkValidateRegexComplexity(b *testing.B) {
	safePattern := "^drone-[0-9]+$"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := validateRegexComplexity(safePattern)
		if err != nil {
			b.Fatal(err)
		}
	}
}
