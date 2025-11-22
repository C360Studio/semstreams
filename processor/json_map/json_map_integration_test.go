//go:build integration

package jsonmapprocessor_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
	jsonmapprocessor "github.com/c360/semstreams/processor/json_map"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all JSON map processor tests
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

// getSharedNATSClient returns the shared NATS client for integration tests
func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}

// createGenericJSONMessage creates a GenericJSONPayload
func createGenericJSONMessage(data map[string]any) ([]byte, error) {
	payload := message.NewGenericJSON(data)
	return json.Marshal(payload)
}

// TestIntegration_FieldRenaming tests basic field renaming with real NATS
func TestIntegration_FieldRenaming(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create map config with field renaming
	config := jsonmapprocessor.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonmap.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonmap.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Mappings: []jsonmapprocessor.FieldMapping{
			{SourceField: "old_name", TargetField: "new_name", Transform: "copy"},
			{SourceField: "timestamp", TargetField: "ts", Transform: "copy"},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	// Create JSON map processor
	mapComp, err := jsonmapprocessor.NewProcessor(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, mapComp)

	mapProc, ok := mapComp.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize
	err = mapProc.Initialize()
	require.NoError(t, err)

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start processor
	err = mapProc.Start(ctx)
	require.NoError(t, err)
	defer mapProc.Stop(5 * time.Second)

	// Give processor time to subscribe
	time.Sleep(100 * time.Millisecond)

	// Subscribe to output to collect transformed messages
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonmap.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	// Give subscriber time to connect
	time.Sleep(100 * time.Millisecond)

	// Publish test message as GenericJSON
	msgData, err := createGenericJSONMessage(map[string]any{
		"old_name":  "test_value",
		"timestamp": "2024-01-01T00:00:00Z",
		"unchanged": "stays_same",
	})
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.jsonmap.input", msgData)
	require.NoError(t, err)

	// Wait for messages to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify transformation
	receiveMu.Lock()
	assert.Equal(t, 1, len(receivedMessages), "Should have 1 transformed message")
	if len(receivedMessages) == 1 {
		payload := receivedMessages[0]
		assert.Equal(t, "test_value", payload.Data["new_name"], "Field should be renamed")
		assert.Equal(t, "2024-01-01T00:00:00Z", payload.Data["ts"], "Timestamp should be renamed")
		assert.Equal(t, "stays_same", payload.Data["unchanged"], "Unchanged field should remain")
		assert.Nil(t, payload.Data["old_name"], "Old field name should be removed")
		assert.Nil(t, payload.Data["timestamp"], "Old timestamp field should be removed")
	}
	receiveMu.Unlock()
}

// TestIntegration_StringTransforms tests string transformation functions
func TestIntegration_StringTransforms(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := jsonmapprocessor.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonmap.transform.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonmap.transform.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Mappings: []jsonmapprocessor.FieldMapping{
			{SourceField: "text", TargetField: "upper", Transform: "uppercase"},
			{SourceField: "text", TargetField: "lower", Transform: "lowercase"},
			{SourceField: "spaced", TargetField: "trimmed", Transform: "trim"},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	mapComp, err := jsonmapprocessor.NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	mapProc, ok := mapComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = mapProc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = mapProc.Start(ctx)
	require.NoError(t, err)
	defer mapProc.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Subscribe to output
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonmap.transform.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish test message
	msgData, err := createGenericJSONMessage(map[string]any{
		"text":   "Hello",
		"spaced": "  trimmed  ",
	})
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.jsonmap.transform.input", msgData)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify transformations
	receiveMu.Lock()
	assert.Equal(t, 1, len(receivedMessages))
	if len(receivedMessages) == 1 {
		payload := receivedMessages[0]
		assert.Equal(t, "HELLO", payload.Data["upper"])
		assert.Equal(t, "hello", payload.Data["lower"])
		assert.Equal(t, "trimmed", payload.Data["trimmed"])
	}
	receiveMu.Unlock()
}

// TestIntegration_AddRemoveFields tests adding static fields and removing fields
func TestIntegration_AddRemoveFields(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := jsonmapprocessor.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonmap.addremove.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonmap.addremove.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Mappings: []jsonmapprocessor.FieldMapping{},
		AddFields: map[string]any{
			"source":  "system",
			"version": 1,
		},
		RemoveFields: []string{"sensitive", "internal"},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	mapComp, err := jsonmapprocessor.NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	mapProc, ok := mapComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = mapProc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = mapProc.Start(ctx)
	require.NoError(t, err)
	defer mapProc.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Subscribe to output
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonmap.addremove.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish test message
	msgData, err := createGenericJSONMessage(map[string]any{
		"public":    "visible",
		"sensitive": "secret",
		"internal":  "hidden",
		"data":      "important",
	})
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.jsonmap.addremove.input", msgData)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify field additions and removals
	receiveMu.Lock()
	assert.Equal(t, 1, len(receivedMessages))
	if len(receivedMessages) == 1 {
		payload := receivedMessages[0]
		// Original fields that should remain
		assert.Equal(t, "visible", payload.Data["public"])
		assert.Equal(t, "important", payload.Data["data"])
		// Added fields
		assert.Equal(t, "system", payload.Data["source"])
		assert.Equal(t, float64(1), payload.Data["version"]) // JSON numbers are float64
		// Removed fields should not exist
		assert.Nil(t, payload.Data["sensitive"])
		assert.Nil(t, payload.Data["internal"])
	}
	receiveMu.Unlock()
}

// TestIntegration_CombinedTransformations tests multiple transformations together
func TestIntegration_CombinedTransformations(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := jsonmapprocessor.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonmap.combined.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonmap.combined.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Mappings: []jsonmapprocessor.FieldMapping{
			{SourceField: "name", TargetField: "user_name", Transform: "copy"},
			{SourceField: "status", TargetField: "STATUS", Transform: "uppercase"},
		},
		AddFields: map[string]any{
			"processed": true,
		},
		RemoveFields: []string{"temp"},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	mapComp, err := jsonmapprocessor.NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	mapProc, ok := mapComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = mapProc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = mapProc.Start(ctx)
	require.NoError(t, err)
	defer mapProc.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Subscribe to output
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonmap.combined.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish test message
	msgData, err := createGenericJSONMessage(map[string]any{
		"name":   "alice",
		"status": "active",
		"temp":   "remove_this",
		"keep":   "important",
	})
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.jsonmap.combined.input", msgData)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify combined transformations
	receiveMu.Lock()
	assert.Equal(t, 1, len(receivedMessages))
	if len(receivedMessages) == 1 {
		payload := receivedMessages[0]
		// Renamed fields
		assert.Equal(t, "alice", payload.Data["user_name"])
		assert.Equal(t, "ACTIVE", payload.Data["STATUS"])
		// Added field
		assert.Equal(t, true, payload.Data["processed"])
		// Kept field
		assert.Equal(t, "important", payload.Data["keep"])
		// Removed fields
		assert.Nil(t, payload.Data["temp"])
		assert.Nil(t, payload.Data["name"])   // Original removed after rename
		assert.Nil(t, payload.Data["status"]) // Original removed after rename
	}
	receiveMu.Unlock()
}
