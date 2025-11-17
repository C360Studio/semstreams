package indexmanager

import (
	"strings"

	"github.com/c360/semstreams/message"
)

// shouldEmbed determines if a message type should be embedded based on config filters.
//
// Logic:
//  1. If SkipTypes matches, return false (deny list)
//  2. If EnabledTypes is empty, return true (no filter)
//  3. If EnabledTypes matches, return true (allow list)
//  4. Otherwise return false (not in allow list)
//
// Message types are in format: "domain.category.version"
// Examples: "alerts.critical.v1", "telemetry.gps.v1", "events.incident.v2"
//
// Patterns support wildcards (*):
//   - "alerts.*.*" matches all alerts
//   - "telemetry.gps.*" matches all GPS telemetry versions
//   - "*.critical.v1" matches all critical messages v1
func shouldEmbed(msgType message.Type, config *EmbeddingConfig) bool {
	if config == nil {
		return false
	}

	typeStr := msgType.String() // Returns "domain.category.version"

	// Check skip patterns first (deny list)
	for _, skipPattern := range config.SkipTypes {
		if matchWildcard(skipPattern, typeStr) {
			return false
		}
	}

	// If no enabled types specified, embed everything (except skipped)
	if len(config.EnabledTypes) == 0 {
		return true
	}

	// Check enabled patterns (allow list)
	for _, enabledPattern := range config.EnabledTypes {
		if matchWildcard(enabledPattern, typeStr) {
			return true
		}
	}

	// Not in allow list
	return false
}

// matchWildcard performs wildcard pattern matching.
//
// Supports:
//   - "*" matches any sequence of characters
//   - Literal matching for non-wildcard parts
//
// Examples:
//   - matchWildcard("alerts.*.*", "alerts.critical.v1") → true
//   - matchWildcard("telemetry.*.v1", "telemetry.gps.v1") → true
//   - matchWildcard("*.critical.*", "alerts.critical.v1") → true
//   - matchWildcard("alerts.*.v2", "alerts.critical.v1") → false
func matchWildcard(pattern, text string) bool {
	// Split by dots for component-wise matching
	patternParts := strings.Split(pattern, ".")
	textParts := strings.Split(text, ".")

	// Must have same number of parts (domain.category.version)
	if len(patternParts) != len(textParts) {
		return false
	}

	// Match each part
	for i := range patternParts {
		if patternParts[i] == "*" {
			continue // Wildcard matches anything
		}
		if patternParts[i] != textParts[i] {
			return false // Literal mismatch
		}
	}

	return true
}
