package vocabulary

import (
	"strings"
	"testing"

	"github.com/c360/semstreams/config"
	"github.com/stretchr/testify/assert"
)

func TestEntityTypeIRI(t *testing.T) {
	tests := []struct {
		name        string
		domainType  string
		expected    string
		expectError bool
	}{
		{
			name:        "valid system device type",
			domainType:  "system.device",                            // INPUT: Dotted notation
			expected:    "https://semstreams.semanticstream.ing/system#Device", // OUTPUT: IRI format (capitalized)
			expectError: false,
		},
		{
			name:        "valid graph node type",
			domainType:  "graph.node",
			expected:    "https://semstreams.semanticstream.ing/graph#Node",
			expectError: false,
		},
		{
			name:        "valid system component type",
			domainType:  "system.component",
			expected:    "https://semstreams.semanticstream.ing/system#Component",
			expectError: false,
		},
		{
			name:        "empty string returns empty",
			domainType:  "",
			expected:    "",
			expectError: false,
		},
		{
			name:        "invalid format without dot",
			domainType:  "systemDevice",
			expected:    "",
			expectError: false,
		},
		{
			name:        "invalid format with multiple dots",
			domainType:  "system.entity.device",
			expected:    "",
			expectError: false,
		},
		{
			name:        "empty domain part",
			domainType:  ".drone",
			expected:    "",
			expectError: false,
		},
		{
			name:        "empty type part",
			domainType:  "system.",
			expected:    "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EntityTypeIRI(tt.domainType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEntityIRI(t *testing.T) {
	tests := []struct {
		name        string
		domainType  string
		platform    config.PlatformConfig
		localID     string
		expected    string
		expectError bool
	}{
		{
			name:       "valid device entity with region",
			domainType: "system.device", // INPUT: Dotted notation
			platform: config.PlatformConfig{
				ID:     "us-west-prod",
				Region: "gulf_mexico",
			},
			localID:  "device_1",
			expected: "https://semstreams.semanticstream.ing/entities/us-west-prod/gulf_mexico/system/device/device_1",
		},
		{
			name:       "valid entity without region",
			domainType: "system.component",
			platform: config.PlatformConfig{
				ID: "standalone",
			},
			localID:  "component_main",
			expected: "https://semstreams.semanticstream.ing/entities/standalone/system/component/component_main",
		},
		{
			name:       "empty platform ID returns empty",
			domainType: "system.device",
			platform: config.PlatformConfig{
				ID:     "",
				Region: "gulf_mexico",
			},
			localID:  "device_1",
			expected: "",
		},
		{
			name:       "empty local ID returns empty",
			domainType: "system.device",
			platform: config.PlatformConfig{
				ID:     "us-west-prod",
				Region: "gulf_mexico",
			},
			localID:  "",
			expected: "",
		},
		{
			name:       "invalid domain type returns empty",
			domainType: "invalid",
			platform: config.PlatformConfig{
				ID:     "us-west-prod",
				Region: "gulf_mexico",
			},
			localID:  "entity_1",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EntityIRI(tt.domainType, tt.platform, tt.localID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRelationshipIRI(t *testing.T) {
	tests := []struct {
		name     string
		relType  string
		expected string
	}{
		{
			name:     "POWERED_BY converts to kebab-case",
			relType:  "POWERED_BY",
			expected: "https://semstreams.semanticstream.ing/relationships#powered-by",
		},
		{
			name:     "HAS_COMPONENT converts to kebab-case",
			relType:  "HAS_COMPONENT",
			expected: "https://semstreams.semanticstream.ing/relationships#has-component",
		},
		{
			name:     "PART_OF converts to kebab-case",
			relType:  "PART_OF",
			expected: "https://semstreams.semanticstream.ing/relationships#part-of",
		},
		{
			name:     "already lowercase stays unchanged",
			relType:  "connects-to",
			expected: "https://semstreams.semanticstream.ing/relationships#connects-to",
		},
		{
			name:     "mixed case gets converted",
			relType:  "PoweredBy",
			expected: "https://semstreams.semanticstream.ing/relationships#powered-by",
		},
		{
			name:     "empty string returns empty",
			relType:  "",
			expected: "",
		},
		{
			name:     "single word lowercase",
			relType:  "CONNECTS",
			expected: "https://semstreams.semanticstream.ing/relationships#connects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RelationshipIRI(tt.relType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubjectIRI(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{
			name:     "semantic system status",
			subject:  "semantic.system.status",
			expected: "https://semstreams.semanticstream.ing/subjects/semantic/system/status",
		},
		{
			name:     "raw udp mavlink",
			subject:  "raw.udp.mavlink",
			expected: "https://semstreams.semanticstream.ing/subjects/raw/udp/mavlink",
		},
		{
			name:     "entity events device",
			subject:  "entity.events.device",
			expected: "https://semstreams.semanticstream.ing/subjects/entity/events/device",
		},
		{
			name:     "graph events node",
			subject:  "graph.events.node",
			expected: "https://semstreams.semanticstream.ing/subjects/graph/events/node",
		},
		{
			name:     "empty string returns empty",
			subject:  "",
			expected: "",
		},
		{
			name:     "single segment",
			subject:  "status",
			expected: "https://semstreams.semanticstream.ing/subjects/status",
		},
		{
			name:     "complex nested subject",
			subject:  "platform.us-west.region.gulf.entity.device.status",
			expected: "https://semstreams.semanticstream.ing/subjects/platform/us-west/region/gulf/entity/device/status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SubjectIRI(tt.subject)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test constants and namespace definitions
func TestNamespaceConstants(t *testing.T) {
	// Verify namespace constants are properly defined
	assert.Equal(t, "https://semstreams.semanticstream.ing", SemStreamsBase)
	// RoboticsNamespace moved to domain modules (semops, streamkit-robotics)
	// assert.Equal(t, SemStreamsBase+"/robotics", RoboticsNamespace)
	assert.Equal(t, SemStreamsBase+"/graph", GraphNamespace)
	assert.Equal(t, SemStreamsBase+"/system", SystemNamespace)

	// Verify consistency in namespace usage
	// assert.True(t, strings.HasPrefix(RoboticsNamespace, SemStreamsBase))
	assert.True(t, strings.HasPrefix(GraphNamespace, SemStreamsBase))
	assert.True(t, strings.HasPrefix(SystemNamespace, SemStreamsBase))
}

// Test edge cases and error conditions
func TestIRIGenerationEdgeCases(t *testing.T) {
	t.Run("EntityTypeIRI with whitespace", func(t *testing.T) {
		result := EntityTypeIRI("  system.device  ")
		assert.Equal(
			t,
			"https://semstreams.semanticstream.ing/system#Device",
			result,
			"should trim whitespace and process normally",
		)
	})

	t.Run("RelationshipIRI with special characters", func(t *testing.T) {
		result := RelationshipIRI("HAS_COMPONENT_123")
		assert.Equal(t, "https://semstreams.semanticstream.ing/relationships#has-component-123", result)
	})

	t.Run("SubjectIRI with leading/trailing dots", func(t *testing.T) {
		result := SubjectIRI(".semantic.system.")
		assert.Equal(t, "", result, "should handle malformed subjects")
	})
}

// Benchmarks for performance verification
func BenchmarkEntityTypeIRI(b *testing.B) {
	for i := 0; i < b.N; i++ {
		EntityTypeIRI("system.device")
	}
}

func BenchmarkEntityIRI(b *testing.B) {
	platform := config.PlatformConfig{
		ID:     "us-west-prod",
		Region: "gulf_mexico",
	}

	for i := 0; i < b.N; i++ {
		EntityIRI("system.device", platform, "device_1")
	}
}

func BenchmarkRelationshipIRI(b *testing.B) {
	for i := 0; i < b.N; i++ {
		RelationshipIRI("POWERED_BY")
	}
}

func BenchmarkSubjectIRI(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SubjectIRI("semantic.system.status")
	}
}
