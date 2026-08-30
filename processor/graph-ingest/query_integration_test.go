//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_QueryHandlers tests query handlers with real NATS JetStream.
// Only the production-wire (NATS-suffixed) handlers are exercised here;
// the former msg-style handlers (handleQueryEntity, handleQueryBatch) were
// deleted as part of gh#164 part 1 dead-code cleanup.
func TestIntegration_QueryHandlers(t *testing.T) {
	ctx := context.Background()

	// Create NATS test client with required streams
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	// Create component
	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: newTestPayloadRegistry(t),
		Platform:        component.PlatformMeta{Org: testDeploymentOrg, Platform: testDeploymentPlatform},
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)

	component := comp.(*Component)
	require.NoError(t, component.Initialize())
	require.NoError(t, component.Start(ctx))
	defer func() {
		_ = component.Stop(context.Background())
	}()

	// Wait for component to be ready
	time.Sleep(100 * time.Millisecond)

	// Create test entities
	entities := []*graph.EntityState{
		{
			ID:          "c360.platform.robotics.mav1.drone.001",
			MessageType: testEntityType(),
			Triples: []message.Triple{
				{
					Subject:   "c360.platform.robotics.mav1.drone.001",
					Predicate: "robotics.status.armed",
					Object:    true,
					Timestamp: time.Now(),
				},
			},
			Version:   1,
			UpdatedAt: time.Now(),
		},
		{
			ID:          "c360.platform.robotics.mav1.drone.002",
			MessageType: testEntityType(),
			Triples: []message.Triple{
				{
					Subject:   "c360.platform.robotics.mav1.drone.002",
					Predicate: "robotics.battery.level",
					Object:    85.5,
					Timestamp: time.Now(),
				},
			},
			Version:   1,
			UpdatedAt: time.Now(),
		},
	}

	// Store entities
	for _, entity := range entities {
		require.NoError(t, component.CreateEntity(ctx, entity))
	}

	t.Run("batch query with real NATS", func(t *testing.T) {
		// Use the component's built-in batch query handler (registered during Start)
		batchSubject := "graph.ingest.query.batch"

		// Send batch query request
		request := map[string][]string{
			"ids": {
				"c360.platform.robotics.mav1.drone.001",
				"c360.platform.robotics.mav1.drone.002",
			},
		}
		requestJSON, err := json.Marshal(request)
		require.NoError(t, err)

		responseData, err := natsClient.Request(ctx, batchSubject, requestJSON, 5*time.Second)
		require.NoError(t, err)

		// Verify response - batch query returns {"entities": [...]} format
		var response struct {
			Entities []graph.EntityState `json:"entities"`
		}
		err = json.Unmarshal(responseData, &response)
		require.NoError(t, err)

		assert.Equal(t, 2, len(response.Entities))
	})
}
