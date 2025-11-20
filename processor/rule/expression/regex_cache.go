// Package expression - Regex pattern caching using framework's cache package
package expression

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/c360/semstreams/pkg/cache"
)

// globalRegexCache is the LRU cache for compiled regular expressions
// Using the framework's cache package eliminates ~150 lines of custom cache code
var globalRegexCache cache.Cache[*regexp.Regexp]

// Initialize the regex cache - must be called during package init
func init() {
	var err error
	globalRegexCache, err = cache.NewLRU[*regexp.Regexp](100,
		cache.WithEvictionCallback(func(key string, re *regexp.Regexp) {
			// Optional: Could log evictions for debugging
			// logger.Debug("Evicted regex pattern from cache", "pattern", key)
		}),
	)
	if err != nil {
		// Cache creation should not fail with valid options, but handle gracefully
		panic(fmt.Sprintf("Failed to initialize regex cache: %v", err))
	}
}

// compileRegex returns a cached compiled regex or compiles and caches a new one
func compileRegex(pattern string) (*regexp.Regexp, error) {
	// Try to get from cache first
	if re, found := globalRegexCache.Get(pattern); found {
		// Cache hit - stats are automatically tracked by the cache package
		return re, nil
	}

	// Security check: validate pattern complexity to prevent ReDoS
	if err := validateRegexComplexity(pattern); err != nil {
		return nil, err
	}

	// Not in cache, compile it
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern '%s': %w", pattern, err)
	}

	// Add to cache - the cache package handles LRU eviction automatically
	globalRegexCache.Set(pattern, re)

	return re, nil
}

// validateRegexComplexity checks for potentially dangerous regex patterns that could cause ReDoS
// This security check is critical and must be preserved even when using the framework cache
func validateRegexComplexity(pattern string) error {
	// Check pattern length
	if len(pattern) > 500 {
		return fmt.Errorf("regex pattern too long (max 500 chars): %d chars", len(pattern))
	}

	// List of dangerous pattern fragments that indicate potential exponential backtracking
	dangerousFragments := []string{
		`(\w+)*\w`,     // Nested quantifiers with overlap
		`(\w*)+`,       // Nested quantifiers
		`(a+)+`,        // Classic ReDoS pattern
		`([a-zA-Z]+)*`, // Nested quantifiers on character class
		`(\d+)*\d`,     // Nested quantifiers with digit overlap
		`(.*)*`,        // Extremely dangerous nested wildcards
		`(.+)+`,        // Extremely dangerous nested wildcards
		`(\s+)*\s`,     // Nested whitespace quantifiers
		`([^,]+)*[^,]`, // Nested negated character class
	}

	// Check if the pattern contains any dangerous fragments
	// Note: This is a heuristic check, not exhaustive
	for _, fragment := range dangerousFragments {
		if strings.Contains(pattern, fragment) {
			return fmt.Errorf("regex pattern contains potentially dangerous construct: nested quantifiers that may cause exponential backtracking")
		}
	}

	// Check for excessive repetition counts
	// Look for patterns like {1000,} or {500,1000}
	if strings.Contains(pattern, "{") {
		// Simple check for large numbers in repetition
		for i := 1000; i <= 9999; i++ {
			if strings.Contains(pattern, fmt.Sprintf("{%d", i)) {
				return fmt.Errorf("regex pattern contains excessive repetition count (>= 1000)")
			}
		}
	}

	// Additional checks for other dangerous constructs
	if strings.Count(pattern, "(") > 20 {
		return fmt.Errorf("regex pattern has too many capture groups (max 20)")
	}

	// Check for deeply nested groups
	nestLevel := 0
	maxNest := 0
	for _, ch := range pattern {
		if ch == '(' {
			nestLevel++
			if nestLevel > maxNest {
				maxNest = nestLevel
			}
		} else if ch == ')' {
			nestLevel--
		}
	}
	if maxNest > 5 {
		return fmt.Errorf("regex pattern has excessive nesting depth (max 5 levels)")
	}

	return nil
}

// Utility functions for testing and maintenance

// clearCache removes all cached patterns (useful for testing)
func clearCache() {
	globalRegexCache.Clear()
}

// cacheSize returns the current number of cached patterns
func cacheSize() int {
	return globalRegexCache.Size()
}

// cacheStats returns cache statistics if available
func cacheStats() *cache.Statistics {
	return globalRegexCache.Stats()
}
