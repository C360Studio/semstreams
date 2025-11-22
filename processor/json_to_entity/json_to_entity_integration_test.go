//go:build integration

package jsontoentity_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
	jsontoentity "github.com/c360/semstreams/processor/json_to_entity"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all json_to_entity processor tests
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") != "" {
		// Create a single shared test client for integration tests
		testClient, err := natsclient.NewSharedTestClient(
			natsclient.WithJetStream(),
			natsclient.WithKV(),
			natsclient.WithTestTimeout(5*time.Second),
			natsclient.WithStartTimeout(30*time.Second),
		)
		if err != nil {
			panic("Failed to create shared test client: " + err.Error())
		}

		sharedTestClient = testClient
		sharedNATSClient = testClient.Client
	}

	// Run all tests
	exitCode := m.Run()

	// Cleanup integration test resources if they were created
	if sharedTestClient != nil {
		sharedTestClient.Terminate()
	}

	os.Exit(exitCode)
}

func TestJSONToEntityIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=1 to run)")
	}

	require.NotNil(t, sharedNATSClient, "NATS client should be initialized")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create test config
	config := jsontoentity.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "test_in",
					Type:      "nats",
					Subject:   "test.generic.input",
					Interface: "core.json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "test_out",
					Type:      "nats",
					Subject:   "test.entity.output",
					Interface: "graph.Entity.v1",
					Required:  true,
				},
			},
		},
		EntityIDField:   "entity_id",
		EntityTypeField: "entity_type",
		EntityClass:     message.ClassObject,
		EntityRole:      message.RolePrimary,
		SourceField:     "test_converter",
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err, "Failed to marshal config")

	// Create processor
	deps := component.Dependencies{
		NATSClient: sharedNATSClient,
		Logger:     slog.Default(),
	}

	proc, err := jsontoentity.NewProcessor(configJSON, deps)
	require.NoError(t, err, "Failed to create processor")

	processor := proc.(*jsontoentity.Processor)

	// Initialize and start processor
	require.NoError(t, processor.Initialize(), "Failed to initialize processor")
	require.NoError(t, processor.Start(ctx), "Failed to start processor")
	defer processor.Stop(5 * time.Second)

	// Subscribe to output to verify Entity messages
	var receivedEntity *message.EntityPayload
	var receivedBase message.BaseMessage
	receivedChan := make(chan bool, 1)

	err = sharedNATSClient.Subscribe(ctx, "test.entity.output", func(_ context.Context, data []byte) {
		if err := json.Unmarshal(data, &receivedBase); err == nil {
			if payload, ok := receivedBase.Payload().(*message.EntityPayload); ok {
				receivedEntity = payload
				receivedChan <- true
			}
		}
	})
	require.NoError(t, err, "Failed to subscribe to output")

	// Give subscriptions time to be ready
	time.Sleep(200 * time.Millisecond)

	// Create test GenericJSON message
	testData := map[string]any{
		"entity_id":   "test.sensor.001",
		"entity_type": "sensors.temperature",
		"temperature": 23.5,
		"location":    "lab-A",
		"status":      "active",
	}

	genericPayload := message.NewGenericJSON(testData)

	// Wrap in BaseMessage
	baseMsg := message.NewBaseMessage(
		genericPayload.Schema(),
		genericPayload,
		"test-publisher",
	)

	// Publish test message
	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err, "Failed to marshal test message")

	err = sharedNATSClient.Publish(ctx, "test.generic.input", msgData)
	require.NoError(t, err, "Failed to publish test message")

	// Wait for output message
	select {
	case <-receivedChan:
		require.NotNil(t, receivedEntity, "Received entity should not be nil")

		// Verify message type
		msgType := receivedBase.Type()
		assert.Equal(t, "graph", msgType.Domain, "Wrong message domain")
		assert.Equal(t, "Entity", msgType.Category, "Wrong message category")
		assert.Equal(t, "v1", msgType.Version, "Wrong message version")

		// Verify entity fields
		assert.Equal(t, "test.sensor.001", receivedEntity.ID, "Wrong entity ID")
		assert.Equal(t, "sensors.temperature", receivedEntity.Type, "Wrong entity type")

		// Verify Graphable interface
		assert.Equal(t, "test.sensor.001", receivedEntity.EntityID(), "Wrong EntityID() result")

		// Verify triples
		triples := receivedEntity.Triples()
		assert.NotEmpty(t, triples, "Triples() returned empty slice")

		// Verify rdf:type triple
		foundTypeTriple := false
		for _, triple := range triples {
			if triple.Predicate == "rdf:type" && triple.Object == "sensors.temperature" {
				foundTypeTriple = true
			}
			// All triples should have correct subject
			assert.Equal(t, "test.sensor.001", triple.Subject, "Wrong triple subject")
		}
		assert.True(t, foundTypeTriple, "Missing rdf:type triple")

		// Verify properties were extracted (temperature, location, status)
		assert.Equal(t, 3, len(receivedEntity.Properties), "Wrong property count")
		assert.Equal(t, 23.5, receivedEntity.Properties["temperature"], "Wrong temperature value")

		t.Logf("✅ Successfully converted GenericJSON to Entity with %d triples", len(triples))

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for output message")
	}

	// Verify processor metrics
	health := processor.Health()
	assert.True(t, health.Healthy, "Processor should be healthy")
	assert.Equal(t, 0, health.ErrorCount, "Processor should have no errors")
}

func TestJSONToEntityMissingFields(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=1 to run)")
	}

	require.NotNil(t, sharedNATSClient, "NATS client should be initialized")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := jsontoentity.DefaultConfig()
	config.Ports.Inputs[0].Subject = "test.generic.missing"
	config.Ports.Outputs[0].Subject = "test.entity.missing"

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: sharedNATSClient,
		Logger:     slog.Default(),
	}

	proc, err := jsontoentity.NewProcessor(configJSON, deps)
	require.NoError(t, err)

	processor := proc.(*jsontoentity.Processor)

	require.NoError(t, processor.Initialize())
	require.NoError(t, processor.Start(ctx))
	defer processor.Stop(5 * time.Second)

	// Subscribe to output (should not receive anything)
	receivedChan := make(chan bool, 1)
	err = sharedNATSClient.Subscribe(ctx, "test.entity.missing", func(_ context.Context, data []byte) {
		receivedChan <- true
	})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Create test GenericJSON message missing required fields
	testData := map[string]any{
		"temperature": 23.5,
		"location":    "lab-A",
		// Missing entity_id and entity_type
	}

	genericPayload := message.NewGenericJSON(testData)
	baseMsg := message.NewBaseMessage(
		genericPayload.Schema(),
		genericPayload,
		"test-publisher",
	)

	msgData, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	err = sharedNATSClient.Publish(ctx, "test.generic.missing", msgData)
	require.NoError(t, err)

	// Should NOT receive output message (conversion should fail)
	select {
	case <-receivedChan:
		t.Fatal("Received output message when expecting error (missing required fields)")
	case <-time.After(1 * time.Second):
		// Expected - no message should be published
		t.Log("✅ Correctly rejected message with missing required fields")
	}

	// Verify error was recorded
	health := processor.Health()
	assert.Greater(t, health.ErrorCount, 0, "Expected error count > 0 for invalid message")
}
