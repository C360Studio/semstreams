package indexmanager

import (
	"testing"

	"github.com/c360/semstreams/message"
)

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		text     string
		expected bool
	}{
		// Exact matches
		{"exact match", "alerts.critical.v1", "alerts.critical.v1", true},
		{"exact mismatch", "alerts.critical.v1", "alerts.warning.v1", false},

		// Single wildcard
		{"wildcard domain", "*.critical.v1", "alerts.critical.v1", true},
		{"wildcard category", "alerts.*.v1", "alerts.critical.v1", true},
		{"wildcard version", "alerts.critical.*", "alerts.critical.v1", true},

		// Multiple wildcards
		{"all wildcards", "*.*.*", "alerts.critical.v1", true},
		{"two wildcards", "alerts.*.*", "alerts.critical.v1", true},
		{"domain and version wildcard", "*.critical.*", "alerts.critical.v1", true},

		// Mismatches
		{"wrong domain", "events.*.*", "alerts.critical.v1", false},
		{"wrong category", "alerts.warning.*", "alerts.critical.v1", false},
		{"wrong version", "alerts.critical.v2", "alerts.critical.v1", false},

		// Different part counts
		{"too few parts", "alerts.critical", "alerts.critical.v1", false},
		{"too many parts", "alerts.critical.v1.extra", "alerts.critical.v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.text)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v",
					tt.pattern, tt.text, result, tt.expected)
			}
		})
	}
}

func TestShouldEmbed(t *testing.T) {
	tests := []struct {
		name         string
		msgType      message.Type
		enabledTypes []string
		skipTypes    []string
		expected     bool
	}{
		// No filters - embed everything
		{
			name:     "no filters - embed",
			msgType:  message.Type{Domain: "alerts", Category: "critical", Version: "v1"},
			expected: true,
		},

		// SkipTypes only
		{
			name:      "skip telemetry",
			msgType:   message.Type{Domain: "telemetry", Category: "gps", Version: "v1"},
			skipTypes: []string{"telemetry.*.*"},
			expected:  false,
		},
		{
			name:      "don't skip alerts",
			msgType:   message.Type{Domain: "alerts", Category: "critical", Version: "v1"},
			skipTypes: []string{"telemetry.*.*"},
			expected:  true,
		},

		// EnabledTypes only
		{
			name:         "enabled alerts",
			msgType:      message.Type{Domain: "alerts", Category: "critical", Version: "v1"},
			enabledTypes: []string{"alerts.*.*", "events.*.*"},
			expected:     true,
		},
		{
			name:         "not enabled telemetry",
			msgType:      message.Type{Domain: "telemetry", Category: "gps", Version: "v1"},
			enabledTypes: []string{"alerts.*.*", "events.*.*"},
			expected:     false,
		},

		// Both EnabledTypes and SkipTypes (SkipTypes takes precedence)
		{
			name:         "skip overrides enabled",
			msgType:      message.Type{Domain: "alerts", Category: "critical", Version: "v1"},
			enabledTypes: []string{"alerts.*.*"},
			skipTypes:    []string{"*.critical.*"},
			expected:     false,
		},
		{
			name:         "enabled and not skipped",
			msgType:      message.Type{Domain: "alerts", Category: "warning", Version: "v1"},
			enabledTypes: []string{"alerts.*.*"},
			skipTypes:    []string{"*.critical.*"},
			expected:     true,
		},

		// Complex patterns
		{
			name:         "specific category enabled",
			msgType:      message.Type{Domain: "events", Category: "incident", Version: "v1"},
			enabledTypes: []string{"events.incident.*", "alerts.*.*"},
			expected:     true,
		},
		{
			name:         "specific category not enabled",
			msgType:      message.Type{Domain: "events", Category: "audit", Version: "v1"},
			enabledTypes: []string{"events.incident.*", "alerts.*.*"},
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &EmbeddingConfig{
				EnabledTypes: tt.enabledTypes,
				SkipTypes:    tt.skipTypes,
			}

			result := shouldEmbed(tt.msgType, config)
			if result != tt.expected {
				t.Errorf("shouldEmbed(%v) with enabled=%v skip=%v = %v, want %v",
					tt.msgType.String(), tt.enabledTypes, tt.skipTypes, result, tt.expected)
			}
		})
	}
}

func TestShouldEmbed_NilConfig(t *testing.T) {
	msgType := message.Type{Domain: "alerts", Category: "critical", Version: "v1"}
	result := shouldEmbed(msgType, nil)
	if result != false {
		t.Errorf("shouldEmbed with nil config should return false, got %v", result)
	}
}
