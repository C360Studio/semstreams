package graph

import (
	"testing"

	"github.com/c360/semstreams/config"
	"github.com/stretchr/testify/assert"
)

func TestFederatedEntity_GetEntityIRI(t *testing.T) {
	tests := []struct {
		name       string
		entity     *FederatedEntity
		entityType string
		expected   string
	}{
		{
			name: "valid federated entity with region",
			entity: &FederatedEntity{
				LocalID:    "drone_1",
				GlobalID:   "us-west-prod:gulf_mexico:drone_1",
				PlatformID: "us-west-prod",
				Region:     "gulf_mexico",
			},
			entityType: "robotics.drone",                                                                                 // INPUT: Dotted notation
			expected:   "https://semstreams.semanticstream.ing/entities/us-west-prod/gulf_mexico/robotics/drone/drone_1", // OUTPUT: Full entity IRI
		},
		{
			name: "valid federated entity without region",
			entity: &FederatedEntity{
				LocalID:    "battery_main",
				GlobalID:   "standalone:battery_main",
				PlatformID: "standalone",
				Region:     "",
			},
			entityType: "robotics.battery",
			expected:   "https://semstreams.semanticstream.ing/entities/standalone/robotics/battery/battery_main",
		},
		{
			name: "entity with empty platform ID returns empty",
			entity: &FederatedEntity{
				LocalID:    "drone_1",
				GlobalID:   "drone_1",
				PlatformID: "",
				Region:     "",
			},
			entityType: "robotics.drone",
			expected:   "",
		},
		{
			name: "entity with empty local ID returns empty",
			entity: &FederatedEntity{
				LocalID:    "",
				GlobalID:   "",
				PlatformID: "us-west-prod",
				Region:     "gulf_mexico",
			},
			entityType: "robotics.drone",
			expected:   "",
		},
		{
			name: "invalid entity type returns empty",
			entity: &FederatedEntity{
				LocalID:    "drone_1",
				GlobalID:   "us-west-prod:gulf_mexico:drone_1",
				PlatformID: "us-west-prod",
				Region:     "gulf_mexico",
			},
			entityType: "invalid",
			expected:   "",
		},
		{
			name: "non-federated entity (minimal)",
			entity: &FederatedEntity{
				LocalID:  "local_entity",
				GlobalID: "local_entity",
			},
			entityType: "robotics.position",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entity.GetEntityIRI(tt.entityType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFederatedEntity_GetEntityIRI_Integration(t *testing.T) {
	// Test integration with existing federation functionality
	t.Run("integration with BuildFederatedEntity", func(t *testing.T) {
		// This tests that our new IRI method works with existing federation code
		localID := "test_drone"

		// Create a federated entity as would be done in normal operation
		fed := &FederatedEntity{
			LocalID:    localID,
			GlobalID:   BuildGlobalID(config.PlatformConfig{ID: "test-platform", Region: "test-region"}, localID),
			PlatformID: "test-platform",
			Region:     "test-region",
		}

		// Test that GetEntityIRI works correctly
		iri := fed.GetEntityIRI(
			"robotics.drone",
		) // INPUT: Dotted notation for IRI generation
		expected := "https://semstreams.semanticstream.ing/entities/test-platform/test-region/robotics/drone/test_drone" // OUTPUT: Full entity IRI
		assert.Equal(t, expected, iri)
	})
}

// Benchmark the GetEntityIRI method for performance verification
func BenchmarkFederatedEntity_GetEntityIRI(b *testing.B) {
	fe := &FederatedEntity{
		LocalID:    "drone_1",
		GlobalID:   "us-west-prod:gulf_mexico:drone_1",
		PlatformID: "us-west-prod",
		Region:     "gulf_mexico",
	}

	for i := 0; i < b.N; i++ {
		fe.GetEntityIRI("robotics.drone")
	}
}
