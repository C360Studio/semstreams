package messagemanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/message"
)

func TestExtractPropertiesAndRelationships(t *testing.T) {
	mp := &Manager{}

	testCases := []struct {
		name             string
		triples          []message.Triple
		expectedProps    int
		expectedRels     int
		expectedEdgeType string
	}{
		{
			name: "property and relationship triples",
			triples: []message.Triple{
				{
					Subject:   "c360.platform1.robotics.mav1.drone.0",
					Predicate: "robotics.battery.level",
					Object:    85.5,
					Timestamp: time.Now(),
				},
				{
					Subject:   "c360.platform1.robotics.mav1.drone.0",
					Predicate: "robotics.component.has",
					Object:    "c360.platform1.robotics.mav1.battery.0",
					Timestamp: time.Now(),
				},
			},
			expectedProps:    5, // battery.level + 4 default metadata properties
			expectedRels:     1,
			expectedEdgeType: "has_component",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			props, rels := mp.extractPropertiesAndRelationships(tc.triples)

			assert.Len(t, props, tc.expectedProps, "properties count mismatch")
			assert.Len(t, rels, tc.expectedRels, "relationships count mismatch")

			if tc.expectedRels > 0 {
				edges := mp.buildEdgesFromRelationships(rels)
				require.Len(t, edges, tc.expectedRels)
				assert.Equal(t, tc.expectedEdgeType, edges[0].EdgeType)
				assert.Equal(t, "c360.platform1.robotics.mav1.battery.0", edges[0].ToEntityID)
			}
		})
	}
}
