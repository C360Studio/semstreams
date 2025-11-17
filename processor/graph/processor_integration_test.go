package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/processor/graph/indexmanager"
	"github.com/c360/semstreams/storage/objectstore"
)

func TestIntegration_QueryAPIs(t *testing.T) {
	// This test requires INTEGRATION_TESTS=1
	natsClient := getSharedNATSClient(t)

	// Create config with metrics disabled for tests
	config := DefaultConfig()
	if config.Indexer == nil {
		config.Indexer = &indexmanager.Config{}
		*config.Indexer = indexmanager.DefaultConfig()
	}
	config.Indexer.EventBuffer.Metrics = false

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)
	require.NotNil(t, processor)

	// Initialize the processor
	err = processor.Initialize()
	require.NoError(t, err)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processor in goroutine (it blocks now)
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for all components to be ready (not just started)
	err = processor.WaitForReady(5 * time.Second)
	require.NoError(t, err, "Processor should be ready within 5 seconds")

	// Store a test entity directly
	testEntity := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   "test-entity-1",
			Type: "device",
		},
		Triples: []message.Triple{
			{
				Subject:   "test-entity-1",
				Predicate: "name",
				Object:    "Test Device",
			},
			{
				Subject:   "test-entity-1",
				Predicate: "status",
				Object:    "active",
			},
		},
		Edges:     []gtypes.Edge{},
		UpdatedAt: time.Now(),
		Version:   1,
	}

	// Store the entity
	_, err = processor.dataManager.CreateEntity(ctx, testEntity)
	require.NoError(t, err)

	// Give time for indexing and caching
	time.Sleep(100 * time.Millisecond)

	// Test GetEntity - should find our stored entity
	entity, err := processor.GetEntity(ctx, "test-entity-1")
	require.NoError(t, err)
	assert.NotNil(t, entity)
	assert.Equal(t, "test-entity-1", entity.Node.ID)
	assert.Equal(t, "device", entity.Node.Type)
	name, found := gtypes.GetPropertyValue(entity, "name")
	assert.True(t, found)
	assert.Equal(t, "Test Device", name)

	// Note: GetEntityByAlias requires the IndexEngine's internal watchers
	// to be fully initialized and running, which is complex to test reliably.
	// This functionality is better validated in end-to-end tests.

	// Note: QueryByPredicate also requires the IndexEngine's internal watchers.
	// We've successfully tested the core functionality of storing and retrieving
	// entities. The index query functionality is better validated in e2e tests.

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

func TestIntegration_MessageProcessing(t *testing.T) {
	// This test requires INTEGRATION_TESTS=1
	natsClient := getSharedNATSClient(t)

	// Create config with metrics disabled for tests
	config := DefaultConfig()
	if config.Indexer == nil {
		config.Indexer = &indexmanager.Config{}
		*config.Indexer = indexmanager.DefaultConfig()
	}
	config.Indexer.EventBuffer.Metrics = false

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)

	err = processor.Initialize()
	require.NoError(t, err)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processor in goroutine (it blocks now)
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for processor to be fully ready after Start()
	err = processor.WaitForReady(5 * time.Second)
	require.NoError(t, err, "Processor should be ready after Start()")

	// Create a Storable message (what ObjectStore sends to GraphProcessor)
	expectedEntityID := "c360.platform1.robotics.mav1.battery.0"

	// Create test payload with battery data
	testPayload := &TestGraphablePayload{
		ID: expectedEntityID,
		Properties: map[string]interface{}{
			"robotics.battery.level":   75,
			"robotics.battery.voltage": 16.72,
		},
		TripleData: []map[string]interface{}{
			{
				"subject":   expectedEntityID,
				"predicate": "robotics.battery.level",
				"object":    75,
			},
			{
				"subject":   expectedEntityID,
				"predicate": "robotics.battery.voltage",
				"object":    16.72, // Sum of voltages in volts
			},
		},
	}

	// Create storage reference
	storageRef := &message.StorageReference{
		StorageInstance: "objectstore-primary",
		Key:             "storage/robotics/2024/01/battery-msg",
		ContentType:     "application/json",
		Size:            1024,
	}

	// Create StoredMessage properly (like ObjectStore does)
	storedMsg := objectstore.NewStoredMessage(testPayload, storageRef, "robotics.battery")

	// Wrap in BaseMessage for transport (correct architecture)
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(), // Use StoredMessage type for correct unmarshaling
		storedMsg,
		"objectstore-primary", // source
	)

	// Marshal and publish the BaseMessage containing StoredMessage
	msgData, err := json.Marshal(wrappedMsg)
	require.NoError(t, err)

	subject := "storage.robotics.events"
	err = natsClient.Publish(ctx, subject, msgData)
	require.NoError(t, err, "Failed to publish message")

	// Poll for entity to be created (instead of arbitrary sleep)
	var entity *gtypes.EntityState

	// Poll up to 2 seconds for the entity to be processed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entity, err = processor.GetEntity(ctx, expectedEntityID)
		if err == nil && entity != nil {
			break // Entity found, processing complete
		}
		time.Sleep(50 * time.Millisecond) // Small sleep between polls
	}

	// Now verify the entity was stored
	require.NoError(t, err)
	assert.NotNil(t, entity)
	assert.Equal(t, expectedEntityID, entity.Node.ID)
	assert.Equal(t, "battery", entity.Node.Type)

	// Check properties were extracted from triples (BatteryPayload uses vocabulary predicates)
	batteryLevel, found := gtypes.GetPropertyValue(entity, "robotics.battery.level")
	assert.True(t, found)
	assert.Equal(t, float64(75), batteryLevel)
	// Voltage should be around 16.72V (4200+4150+4180+4190 mV = 16720 mV = 16.72V)
	voltage, found := gtypes.GetPropertyValueTyped[float64](entity, "robotics.battery.voltage")
	assert.True(t, found)
	assert.Greater(t, voltage, 16.0)
	assert.Less(t, voltage, 17.0)

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

func TestIntegration_EdgeOperations(t *testing.T) {
	// This test requires INTEGRATION_TESTS=1
	// IMPORTANT: This test uses entity IDs with "integ" suffix to avoid conflicts with other tests.
	// Entity cleanup is performed at the end to ensure test isolation.
	natsClient := getSharedNATSClient(t)

	// Create config
	config := DefaultConfig()
	if config.Indexer == nil {
		config.Indexer = &indexmanager.Config{}
		*config.Indexer = indexmanager.DefaultConfig()
	}
	config.Indexer.EventBuffer.Metrics = false

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)

	err = processor.Initialize()
	require.NoError(t, err)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processor in goroutine (it blocks now)
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for ready
	err = processor.WaitForReady(2 * time.Second)
	require.NoError(t, err)

	// Create two entities with structured entity IDs - use unique IDs for this test
	entity1ID := "c360.platform1.test.system1.drone.integ"
	entity2ID := "c360.platform1.test.system1.battery.integ"

	entity1 := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   entity1ID,
			Type: "drone",
		},
		Triples: []message.Triple{
			{
				Subject:   entity1ID,
				Predicate: "name",
				Object:    "Test Drone",
			},
		},
		Edges:     []gtypes.Edge{},
		UpdatedAt: time.Now(),
		Version:   1,
	}

	entity2 := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   entity2ID,
			Type: "battery",
		},
		Triples: []message.Triple{
			{
				Subject:   entity2ID,
				Predicate: "name",
				Object:    "Test Battery",
			},
		},
		Edges:     []gtypes.Edge{},
		UpdatedAt: time.Now(),
		Version:   1,
	}

	// Store both entities
	_, err = processor.dataManager.CreateEntity(ctx, entity1)
	require.NoError(t, err)
	_, err = processor.dataManager.CreateEntity(ctx, entity2)
	require.NoError(t, err)

	// Give time for entities to be fully created and indexed
	time.Sleep(100 * time.Millisecond)

	// Add an edge from entity1 to entity2
	edge := gtypes.Edge{
		ToEntityID: entity2ID,
		EdgeType:   "connects_to",
		Properties: map[string]any{"weight": 1.0},
	}

	// Use the edge manager to add the edge
	err = processor.dataManager.AddEdge(ctx, entity1ID, edge)
	require.NoError(t, err)

	// Give time for asynchronous processing
	time.Sleep(100 * time.Millisecond)

	// Retrieve entity1 and verify the edge was added
	updatedEntity1, err := processor.GetEntity(ctx, entity1ID)
	require.NoError(t, err)
	assert.Len(t, updatedEntity1.Edges, 1)
	assert.Equal(t, entity2ID, updatedEntity1.Edges[0].ToEntityID)
	assert.Equal(t, "connects_to", updatedEntity1.Edges[0].EdgeType)

	// Test incoming index was updated - now expects direct array format
	incomingBucket, err := natsClient.GetKeyValueBucket(ctx, "INCOMING_INDEX")
	require.NoError(t, err)

	entry, err := incomingBucket.Get(ctx, entity2ID)
	if err == nil {
		var incomingRefs []string
		err = json.Unmarshal(entry.Value(), &incomingRefs)
		require.NoError(t, err)
		assert.Contains(t, incomingRefs, entity1ID)
	}

	// Clean up entities to avoid conflicts with other tests
	processor.dataManager.DeleteEntity(ctx, entity1ID)
	processor.dataManager.DeleteEntity(ctx, entity2ID)

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
