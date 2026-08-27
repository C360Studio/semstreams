package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ====================================================================================
// Query Handler: handleQuerySuffixNATS Tests
// ====================================================================================

func TestComponent_HandleQuerySuffix_Success(t *testing.T) {
	tests := []struct {
		name           string
		storedIDs      []string
		suffix         string
		expectedResult string
	}{
		{
			name: "match temp-sensor-001",
			storedIDs: []string{
				"c360.logistics.environmental.sensor.temperature.temp-sensor-001",
				"c360.logistics.environmental.sensor.temperature.temp-sensor-002",
				"c360.logistics.environmental.sensor.humidity.humid-sensor-001",
			},
			suffix:         "temp-sensor-001",
			expectedResult: "c360.logistics.environmental.sensor.temperature.temp-sensor-001",
		},
		{
			name: "match maint-001",
			storedIDs: []string{
				"c360.logistics.maintenance.work.completed.maint-001",
				"c360.logistics.maintenance.work.completed.maint-002",
			},
			suffix:         "maint-001",
			expectedResult: "c360.logistics.maintenance.work.completed.maint-001",
		},
		{
			name: "match robot-007",
			storedIDs: []string{
				"acme.ops.logistics.warehouse.robot.robot-007",
				"acme.ops.logistics.warehouse.robot.robot-008",
			},
			suffix:         "robot-007",
			expectedResult: "acme.ops.logistics.warehouse.robot.robot-007",
		},
		{
			name: "match drone.001 (with dot in suffix)",
			storedIDs: []string{
				"c360.platform.robotics.mav1.drone.001",
				"c360.platform.robotics.mav1.drone.002",
			},
			suffix:         "drone.001",
			expectedResult: "c360.platform.robotics.mav1.drone.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := createTestComponentWithMockKV(t)
			ctx := context.Background()

			// Store entities in KV bucket
			for _, id := range tt.storedIDs {
				entity := &graph.EntityState{
					ID:          id,
					MessageType: testEntityType(),
					Triples:     []message.Triple{},
					Version:     1,
					UpdatedAt:   time.Now(),
				}
				require.NoError(t, comp.CreateEntity(ctx, entity))
			}

			// Create request
			request := map[string]string{"suffix": tt.suffix}
			requestJSON, err := json.Marshal(request)
			require.NoError(t, err)

			// Call handler directly
			responseJSON, err := comp.handleQuerySuffixNATS(ctx, requestJSON)
			require.NoError(t, err, "handleQuerySuffixNATS should not return error")

			// Parse response
			var response struct {
				ID string `json:"id"`
			}
			err = json.Unmarshal(responseJSON, &response)
			require.NoError(t, err, "response should be valid JSON")

			assert.Equal(t, tt.expectedResult, response.ID, "should match expected entity ID")
		})
	}
}

func TestComponent_HandleQuerySuffix_NoMatch(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Store some entities
	entities := []string{
		"c360.logistics.environmental.sensor.temperature.temp-sensor-001",
		"c360.logistics.environmental.sensor.temperature.temp-sensor-002",
	}
	for _, id := range entities {
		entity := &graph.EntityState{
			ID:          id,
			MessageType: testEntityType(),
			Triples:     []message.Triple{},
			Version:     1,
			UpdatedAt:   time.Now(),
		}
		require.NoError(t, comp.CreateEntity(ctx, entity))
	}

	// Query for non-existent suffix
	request := map[string]string{"suffix": "nonexistent-sensor-999"}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	responseJSON, err := comp.handleQuerySuffixNATS(ctx, requestJSON)
	require.NoError(t, err, "should not error for no match")

	var response struct {
		ID string `json:"id"`
	}
	err = json.Unmarshal(responseJSON, &response)
	require.NoError(t, err)

	assert.Equal(t, "", response.ID, "should return empty ID when no match")
}

func TestComponent_HandleQuerySuffix_EmptyBucket(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Don't store any entities - bucket is empty

	request := map[string]string{"suffix": "temp-sensor-001"}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	responseJSON, err := comp.handleQuerySuffixNATS(ctx, requestJSON)
	require.NoError(t, err, "should handle empty bucket gracefully")

	var response struct {
		ID string `json:"id"`
	}
	err = json.Unmarshal(responseJSON, &response)
	require.NoError(t, err)

	assert.Equal(t, "", response.ID, "should return empty ID for empty bucket")
}

func TestComponent_HandleQuerySuffix_InvalidRequest(t *testing.T) {
	tests := []struct {
		name        string
		requestData []byte
	}{
		{
			name:        "malformed JSON",
			requestData: []byte(`{invalid json}`),
		},
		{
			name:        "empty suffix",
			requestData: []byte(`{"suffix": ""}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := createTestComponentWithMockKV(t)
			ctx := context.Background()

			_, err := comp.handleQuerySuffixNATS(ctx, tt.requestData)
			assert.Error(t, err, "should return error for invalid request")
		})
	}
}

func TestComponent_HandleQuerySuffix_PartialSuffixNoMatch(t *testing.T) {
	// Ensure we only match on complete instance suffix, not partial
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Store entity
	entity := &graph.EntityState{
		ID:          "c360.logistics.environmental.sensor.temperature.temp-sensor-001",
		MessageType: testEntityType(),
		Triples:     []message.Triple{},
		Version:     1,
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, comp.CreateEntity(ctx, entity))

	// Query with partial suffix (should NOT match "sensor-001" because we need ".sensor-001")
	request := map[string]string{"suffix": "sensor-001"}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	responseJSON, err := comp.handleQuerySuffixNATS(ctx, requestJSON)
	require.NoError(t, err)

	var response struct {
		ID string `json:"id"`
	}
	err = json.Unmarshal(responseJSON, &response)
	require.NoError(t, err)

	// This should NOT match because "sensor-001" is not the full instance part
	// The entity ends with ".temp-sensor-001" not ".sensor-001"
	assert.Equal(t, "", response.ID, "partial suffix should not match")
}

// ====================================================================================
// Query Handler: handleQueryPrefixNATS Tests
// ====================================================================================

func TestComponent_HandleQueryPrefix_Success(t *testing.T) {
	tests := []struct {
		name              string
		storedIDs         []string
		prefix            string
		limit             int
		expectedCount     int
		expectedContained []string
	}{
		{
			name: "match by org prefix",
			storedIDs: []string{
				"acme.ops.logistics.warehouse.robot.robot-001",
				"acme.ops.logistics.warehouse.robot.robot-002",
				"c360.platform.robotics.mav1.drone.001",
			},
			prefix:        "acme",
			limit:         10,
			expectedCount: 2,
			expectedContained: []string{
				"acme.ops.logistics.warehouse.robot.robot-001",
				"acme.ops.logistics.warehouse.robot.robot-002",
			},
		},
		{
			name: "match by full prefix path",
			storedIDs: []string{
				"c360.platform.robotics.mav1.drone.001",
				"c360.platform.robotics.mav1.drone.002",
				"c360.platform.robotics.mav2.drone.001",
			},
			prefix:        "c360.platform.robotics.mav1",
			limit:         10,
			expectedCount: 2,
			expectedContained: []string{
				"c360.platform.robotics.mav1.drone.001",
				"c360.platform.robotics.mav1.drone.002",
			},
		},
		{
			name: "empty prefix returns all",
			storedIDs: []string{
				"acme.ops.logistics.warehouse.robot.robot-001",
				"c360.platform.robotics.mav1.drone.001",
			},
			prefix:        "",
			limit:         10,
			expectedCount: 2,
			expectedContained: []string{
				"acme.ops.logistics.warehouse.robot.robot-001",
				"c360.platform.robotics.mav1.drone.001",
			},
		},
		{
			name: "limit restricts count",
			storedIDs: []string{
				"c360.platform.robotics.mav1.drone.001",
				"c360.platform.robotics.mav1.drone.002",
				"c360.platform.robotics.mav1.drone.003",
			},
			prefix:        "c360",
			limit:         2,
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := createTestComponentWithMockKV(t)
			ctx := context.Background()

			// Store entities
			for _, id := range tt.storedIDs {
				entity := &graph.EntityState{
					ID:          id,
					MessageType: testEntityType(),
					Triples: []message.Triple{
						{
							Subject:   id,
							Predicate: "test.entity.predicate",
							Object:    "test-value",
							Timestamp: time.Now(),
						},
					},
					Version:   1,
					UpdatedAt: time.Now(),
				}
				require.NoError(t, comp.CreateEntity(ctx, entity))
			}

			// Create request
			request := map[string]any{
				"prefix": tt.prefix,
				"limit":  tt.limit,
			}
			requestJSON, err := json.Marshal(request)
			require.NoError(t, err)

			// Call handler
			responseJSON, err := comp.handleQueryPrefixWithMaxPayload(ctx, requestJSON, 1<<20)
			require.NoError(t, err)

			// Parse response - should be entities envelope
			var resp struct {
				Entities []graph.EntityState `json:"entities"`
			}
			err = json.Unmarshal(responseJSON, &resp)
			require.NoError(t, err, "response should be valid entities envelope JSON")

			assert.Equal(t, tt.expectedCount, len(resp.Entities), "should return expected count")

			// Verify expected entities are contained
			entityIDs := make(map[string]bool)
			for _, e := range resp.Entities {
				entityIDs[e.ID] = true
				// Verify entity has triples (full entity, not just ID)
				assert.NotEmpty(t, e.Triples, "entity %s should have triples", e.ID)
			}

			for _, expectedID := range tt.expectedContained {
				assert.True(t, entityIDs[expectedID], "should contain entity %s", expectedID)
			}
		})
	}
}

func TestComponent_HandleQueryPrefix_ReturnsFullEntities(t *testing.T) {
	// This test specifically verifies that the response contains full entity data
	// not just entity IDs (the bug that was fixed)
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Create entity with triples
	entity := &graph.EntityState{
		ID:          "c360.platform.robotics.mav1.drone.001",
		MessageType: testEntityType(),
		Triples: []message.Triple{
			{
				Subject:   "c360.platform.robotics.mav1.drone.001",
				Predicate: "robotics.status.armed",
				Object:    true,
				Timestamp: time.Now(),
			},
			{
				Subject:   "c360.platform.robotics.mav1.drone.001",
				Predicate: "robotics.battery.level",
				Object:    85.5,
				Timestamp: time.Now(),
			},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	require.NoError(t, comp.CreateEntity(ctx, entity))

	// Query by prefix
	request := map[string]any{
		"prefix": "c360",
		"limit":  10,
	}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	responseJSON, err := comp.handleQueryPrefixWithMaxPayload(ctx, requestJSON, 1<<20)
	require.NoError(t, err)

	// Parse response as entities envelope
	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	err = json.Unmarshal(responseJSON, &resp)
	require.NoError(t, err, "response should be valid entities envelope")

	require.Len(t, resp.Entities, 1, "should return 1 entity")

	// Verify it's a full entity with all data
	assert.Equal(t, entity.ID, resp.Entities[0].ID)
	// 2 user triples + the ADR-054 entity.indexing.profile stamp (appended at
	// creation). No producer declared a profile here, so it defaults to the
	// control floor; the user triples keep their original leading order.
	assert.Len(t, resp.Entities[0].Triples, 3, "should have 2 user triples + the indexing-profile stamp")
	assert.Equal(t, "robotics.status.armed", resp.Entities[0].Triples[0].Predicate)
	profile, ok := resp.Entities[0].GetPropertyValue(vocabulary.EntityIndexingProfile)
	assert.True(t, ok, "created entity should carry entity.indexing.profile")
	assert.Equal(t, vocabulary.IndexingProfileControl, profile)
}

func TestComponent_HandleQueryPrefix_InvalidRequest(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Malformed JSON
	_, err := comp.handleQueryPrefixWithMaxPayload(ctx, []byte(`{invalid json}`), 1<<20)
	assert.Error(t, err, "should return error for invalid JSON")
}

func TestComponent_HandleQueryPrefix_NoMatches(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// Store some entities
	entity := &graph.EntityState{
		ID:          "c360.platform.robotics.mav1.drone.001",
		MessageType: testEntityType(),
		Triples:     []message.Triple{},
		Version:     1,
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, comp.CreateEntity(ctx, entity))

	// Query with non-matching prefix
	request := map[string]any{
		"prefix": "nonexistent",
		"limit":  10,
	}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	responseJSON, err := comp.handleQueryPrefixWithMaxPayload(ctx, requestJSON, 1<<20)
	require.NoError(t, err)

	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	err = json.Unmarshal(responseJSON, &resp)
	require.NoError(t, err)

	assert.Empty(t, resp.Entities, "should return empty array when no matches")
}
