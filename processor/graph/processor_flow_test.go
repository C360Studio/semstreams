package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/storage/objectstore"
)

// TestGraphablePayload is already defined in edge_building_test.go

// TestIntegration_GraphProcessorMessageFlow verifies the complete message flow from ObjectStore
// through GraphProcessor according to the reference architecture
func TestIntegration_GraphProcessorMessageFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use shared NATS client from main_test.go
	natsClient := getSharedNATSClient(t)

	// Create GraphProcessor with default config (uses storage.*.events)
	deps := ProcessorDeps{
		Config:          DefaultConfig(), // Uses storage.*.events by default
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)
	require.NotNil(t, processor)

	// Initialize
	require.NoError(t, processor.Initialize())

	// Start processor in goroutine (it blocks now)
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for processor to be ready
	require.Eventually(t, func() bool {
		return processor.IsReady()
	}, 5*time.Second, 100*time.Millisecond, "Processor should be ready")

	// Get bucket for verifying results
	entityBucket, err := natsClient.GetKeyValueBucket(ctx, "ENTITY_STATES")
	require.NoError(t, err)

	// Test the critical architecture requirement: StoredMessage flow with ObjectRef
	t.Run("StoredMessage_Flow_With_ObjectRef", func(t *testing.T) {
		entityID := "c360.platform.robotics.system.drone.test1"
		batteryID := "c360.platform.robotics.system.battery.test1"

		// Create a test graphable payload with triples
		testPayload := &TestGraphablePayload{
			ID: entityID,
			Properties: map[string]interface{}{
				"system:battery_level": 85.5,
			},
			TripleData: []map[string]interface{}{
				// Property triple
				{
					"subject":   entityID,
					"predicate": "system:battery_level",
					"object":    85.5,
				},
				// Relationship triple
				{
					"subject":   entityID,
					"predicate": "system:has_component",
					"object":    batteryID,
				},
			},
		}

		// Create storage reference
		storageRef := &message.StorageReference{
			StorageInstance: "objectstore-primary",
			Key:             "storage/robotics/2024/01/msg-12345",
			ContentType:     "application/json",
			Size:            1024,
		}

		// Create StoredMessage properly (like ObjectStore does)
		storedMsg := objectstore.NewStoredMessage(testPayload, storageRef, "test.heartbeat")

		// Wrap in BaseMessage for transport (correct architecture)
		wrappedMsg := message.NewBaseMessage(
			storedMsg.Schema(), // Use StoredMessage type for correct unmarshaling
			storedMsg,
			"objectstore-primary", // source
		)

		// Publish to storage.robotics.events (ObjectStore → GraphProcessor flow)
		data, err := json.Marshal(wrappedMsg)
		require.NoError(t, err)
		err = natsClient.Publish(ctx, "storage.robotics.events", data)
		require.NoError(t, err)

		// Verify entity was created with correct architecture
		require.Eventually(t, func() bool {
			entry, err := entityBucket.Get(ctx, entityID)
			if err != nil {
				t.Logf("Entity not found: %v", err)
				return false
			}

			var entity gtypes.EntityState
			err = json.Unmarshal(entry.Value(), &entity)
			if err != nil {
				t.Logf("Failed to unmarshal entity: %v", err)
				return false
			}

			// Critical: ObjectRef must be populated from StorageReference
			if entity.ObjectRef != "storage/robotics/2024/01/msg-12345" {
				t.Logf("ObjectRef mismatch: got %q, want %q",
					entity.ObjectRef, "storage/robotics/2024/01/msg-12345")
				return false
			}

			// Critical: Edges must be created from relationship triples
			if len(entity.Edges) != 1 {
				t.Logf("Expected 1 edge, got %d edges: %+v", len(entity.Edges), entity.Edges)
				return false
			}

			if entity.Edges[0].ToEntityID != batteryID {
				t.Logf("Edge target mismatch: got %q, want %q",
					entity.Edges[0].ToEntityID, batteryID)
				return false
			}

			t.Logf("SUCCESS: Entity created with ObjectRef=%q and %d edges",
				entity.ObjectRef, len(entity.Edges))
			return true
		}, 10*time.Second, 100*time.Millisecond, "Entity should be created with ObjectRef and edges")
	})

	// Cancel context to trigger shutdown
	cancel()

	// Wait for Start to return
	select {
	case err := <-startErr:
		require.NoError(t, err, "Start should complete without error")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}
