package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/storage/objectstore"
)

// Helper function to create a processor with test dependencies
func createTestProcessor(t *testing.T, config *Config) *Processor {
	natsClient := getSharedNATSClient(t)

	// Ensure NATS connection is ready
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := natsClient.WaitForConnection(ctx)
	require.NoError(t, err)

	if config == nil {
		config = DefaultConfig()
	}

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)
	require.NotNil(t, processor)

	return processor
}

// Unit Tests (no NATS required)

func TestNewProcessor_FailsWithoutNATSClient(t *testing.T) {
	deps := ProcessorDeps{
		NATSClient: nil,
	}

	processor, err := NewProcessor(deps)
	assert.Nil(t, processor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NATS client required")
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 10, config.Workers)
	assert.Equal(t, 10000, config.QueueSize)
	assert.Equal(t, "storage.*.events", config.InputSubject)
	assert.NotNil(t, config.DataManager)
	assert.NotNil(t, config.Querier)

	// DataManager defaults
	assert.Equal(t, 5, config.DataManager.Workers)
	assert.Equal(t, 10000, config.DataManager.BufferConfig.Capacity)

	// Querier defaults
	assert.NotNil(t, config.Querier.Timeouts)
}

// Integration Tests (real NATS required)

func TestIntegration_ProcessorLifecycle(t *testing.T) {
	processor := createTestProcessor(t, nil)

	// Verify processor is properly initialized
	assert.NotNil(t, processor.natsClient)
	assert.NotNil(t, processor.config)
	assert.True(t, processor.health.Healthy)

	// Test default config values
	assert.Equal(t, 10, processor.config.Workers)
	assert.Equal(t, 10000, processor.config.QueueSize)
}

func TestIntegration_ComponentInterface(t *testing.T) {
	processor := createTestProcessor(t, nil)

	// Test Meta
	meta := processor.Meta()
	assert.Equal(t, "graph", meta.Name)
	assert.Equal(t, "semantic-processor", meta.Type)
	assert.Equal(t, "1.0.0", meta.Version)

	// Test Input/Output Ports
	inputPorts := processor.InputPorts()
	assert.Len(t, inputPorts, 2)
	assert.Equal(t, "entities_input", inputPorts[0].Name)
	assert.Equal(t, "mutations_api", inputPorts[1].Name)

	outputPorts := processor.OutputPorts()
	assert.Len(t, outputPorts, 3)
	assert.Equal(t, "entity_states", outputPorts[0].Name)
	assert.Equal(t, "predicate_index", outputPorts[1].Name)
	assert.Equal(t, "entities_output", outputPorts[2].Name)

	// Test Health
	health := processor.Health()
	assert.NotNil(t, health)

	// Test ConfigSchema
	schema := processor.ConfigSchema()
	assert.NotNil(t, schema)
}

func TestIntegration_InitializeStartStop(t *testing.T) {
	processor := createTestProcessor(t, nil)

	// Initialize
	err := processor.Initialize()
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

	// Check that engines were created
	assert.NotNil(t, processor.dataManager)
	assert.NotNil(t, processor.indexManager)
	assert.NotNil(t, processor.queryManager)

	// Verify it's running
	assert.True(t, processor.IsReady())

	// Cancel context to trigger shutdown
	cancel()

	// Wait for Start to return
	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestIntegration_HandleMessage(t *testing.T) {
	processor := createTestProcessor(t, nil)

	// Initialize
	err := processor.Initialize()
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

	// Create a test message using proper StoredMessage pattern (like ObjectStore does)
	testPayload := &TestGraphablePayload{
		ID: "test-entity-msg",
		Properties: map[string]interface{}{
			"test.property": "test-value",
		},
		TripleData: []map[string]interface{}{
			{
				"subject":   "test-entity-msg",
				"predicate": "test.property",
				"object":    "test-value",
			},
		},
	}

	// Create storage reference (like ObjectStore would)
	storageRef := &message.StorageReference{
		StorageInstance: "test-objectstore",
		Key:             "storage/test/msg-handle-001",
		ContentType:     "application/json",
		Size:            1024,
	}

	// Create StoredMessage properly (like ObjectStore does)
	storedMsg := objectstore.NewStoredMessage(testPayload, storageRef, "test.message")

	// Wrap in BaseMessage for transport (correct architecture)
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(), // Use StoredMessage type for correct unmarshaling
		storedMsg,
		"test-objectstore", // source
	)

	msgData, err := json.Marshal(wrappedMsg)
	require.NoError(t, err)

	// Publish via NATS (proper flow instead of direct handleMessage call)
	err = processor.natsClient.Publish(ctx, "storage.test.events", msgData)
	require.NoError(t, err)

	// Give it time to process
	timer := time.NewTimer(500 * time.Millisecond)
	<-timer.C

	// Verify entity was created
	entity, err := processor.GetEntity(ctx, "test-entity-msg")
	assert.NoError(t, err)
	if entity != nil {
		assert.Equal(t, "test-entity-msg", entity.Node.ID)
	}

	// Cancel context to trigger shutdown
	cancel()

	// Wait for Start to return
	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestIntegration_ErrorHandling(t *testing.T) {
	processor := createTestProcessor(t, nil)

	// Test handling invalid message
	processor.handleMessage(context.Background(), []byte("invalid json"))

	// Should record error
	assert.False(t, processor.health.Healthy)
	assert.NotEmpty(t, processor.health.LastError)
}
