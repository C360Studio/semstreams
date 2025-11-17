package jsonfilter_test

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
	jsonfilter "github.com/c360/semstreams/processor/json_filter"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all JSON filter processor tests
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

// TestIntegration_JSONFilterProcessing tests JSON filter processor with GenericJSON payloads
func TestIntegration_JSONFilterProcessing(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create filter config with gt rule
	config := jsonfilter.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonfilter.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonfilter.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Rules: []jsonfilter.FilterRule{
			{Field: "value", Operator: "gt", Value: float64(100)},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	// Create JSON filter processor
	filterComp, err := jsonfilter.NewProcessor(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, filterComp)

	filterProc, ok := filterComp.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize
	err = filterProc.Initialize()
	require.NoError(t, err)

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start processor
	err = filterProc.Start(ctx)
	require.NoError(t, err)
	defer filterProc.Stop(5 * time.Second)

	// Give processor time to subscribe
	time.Sleep(100 * time.Millisecond)

	// Subscribe to output to collect filtered messages
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonfilter.output", func(_ context.Context, data []byte) {
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

	// Publish test messages as GenericJSON
	testCases := []struct {
		value      float64
		shouldPass bool
	}{
		{50, false},  // Below threshold
		{100, false}, // Equal to threshold (gt means strictly greater)
		{150, true},  // Above threshold
		{200, true},  // Above threshold
		{75, false},  // Below threshold
	}

	for _, tc := range testCases {
		msgData, err := createGenericJSONMessage(map[string]any{
			"value": tc.value,
			"name":  "test",
		})
		require.NoError(t, err)

		err = natsClient.Publish(ctx, "test.jsonfilter.input", msgData)
		require.NoError(t, err)
	}

	// Wait for messages to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify correct number of messages passed filter
	receiveMu.Lock()
	assert.Equal(t, 2, len(receivedMessages), "Should have 2 messages that passed filter (value > 100)")

	// Verify the messages that passed have correct values
	if len(receivedMessages) >= 2 {
		values := make([]float64, len(receivedMessages))
		for i, payload := range receivedMessages {
			values[i] = payload.Data["value"].(float64)
		}
		assert.Contains(t, values, float64(150))
		assert.Contains(t, values, float64(200))
	}
	receiveMu.Unlock()
}

// TestIntegration_MultipleRules tests JSON filter with multiple rules (AND logic)
func TestIntegration_MultipleRules(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create filter with multiple rules
	config := jsonfilter.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonfilter.multi.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonfilter.multi.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Rules: []jsonfilter.FilterRule{
			{Field: "value", Operator: "gt", Value: float64(100)},
			{Field: "status", Operator: "eq", Value: "active"},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	filterComp, err := jsonfilter.NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	filterProc, ok := filterComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = filterProc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = filterProc.Start(ctx)
	require.NoError(t, err)
	defer filterProc.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Subscribe to output
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonfilter.multi.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish test messages - both rules must match
	testCases := []struct {
		value      float64
		status     string
		shouldPass bool
	}{
		{150, "active", true},    // Both rules match
		{150, "inactive", false}, // value matches, status doesn't
		{50, "active", false},    // status matches, value doesn't
		{50, "inactive", false},  // Neither match
		{200, "active", true},    // Both rules match
	}

	for _, tc := range testCases {
		msgData, err := createGenericJSONMessage(map[string]any{
			"value":  tc.value,
			"status": tc.status,
		})
		require.NoError(t, err)

		err = natsClient.Publish(ctx, "test.jsonfilter.multi.input", msgData)
		require.NoError(t, err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify only messages matching ALL rules passed
	receiveMu.Lock()
	assert.Equal(t, 2, len(receivedMessages), "Should have 2 messages that passed both rules")
	receiveMu.Unlock()
}

// TestIntegration_ContainsOperator tests the contains operator with strings
func TestIntegration_ContainsOperator(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := jsonfilter.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonfilter.contains.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonfilter.contains.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Rules: []jsonfilter.FilterRule{
			{Field: "message", Operator: "contains", Value: "error"},
		},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	filterComp, err := jsonfilter.NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	filterProc, ok := filterComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = filterProc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = filterProc.Start(ctx)
	require.NoError(t, err)
	defer filterProc.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Subscribe to output
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonfilter.contains.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish test messages
	testMessages := []struct {
		message    string
		shouldPass bool
	}{
		{"this is an error message", true},
		{"all good here", false},
		{"ERROR: connection failed", false}, // case sensitive - doesn't match lowercase 'error'
		{"warning: something happened", false},
		{"another error occurred", true}, // lowercase error matches
	}

	for _, tm := range testMessages {
		msgData, err := createGenericJSONMessage(map[string]any{
			"message": tm.message,
		})
		require.NoError(t, err)

		err = natsClient.Publish(ctx, "test.jsonfilter.contains.input", msgData)
		require.NoError(t, err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify contains operator worked
	receiveMu.Lock()
	assert.Equal(t, 2, len(receivedMessages), "Should have 2 messages containing 'error'")
	receiveMu.Unlock()
}

// TestIntegration_RejectsInvalidJSON tests that invalid JSON is rejected
func TestIntegration_RejectsInvalidJSON(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	config := jsonfilter.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:      "input",
					Type:      "nats",
					Subject:   "test.jsonfilter.reject.input",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:      "output",
					Type:      "nats",
					Subject:   "test.jsonfilter.reject.output",
					Interface: "core .json.v1",
					Required:  true,
				},
			},
		},
		Rules: []jsonfilter.FilterRule{},
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	filterComp, err := jsonfilter.NewProcessor(rawConfig, deps)
	require.NoError(t, err)

	filterProc, ok := filterComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = filterProc.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = filterProc.Start(ctx)
	require.NoError(t, err)
	defer filterProc.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Subscribe to output
	receivedMessages := make([]message.GenericJSONPayload, 0)
	var receiveMu sync.Mutex

	err = natsClient.Subscribe(ctx, "test.jsonfilter.reject.output", func(_ context.Context, data []byte) {
		var payload message.GenericJSONPayload
		if err := json.Unmarshal(data, &payload); err == nil {
			receiveMu.Lock()
			receivedMessages = append(receivedMessages, payload)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish valid GenericJSON (should pass)
	validMsg, err := createGenericJSONMessage(map[string]any{"test": "valid"})
	require.NoError(t, err)
	err = natsClient.Publish(ctx, "test.jsonfilter.reject.input", validMsg)
	require.NoError(t, err)

	// Publish GenericJSON with nil data (should be rejected due to validation)
	invalidPayload := &message.GenericJSONPayload{Data: nil}
	invalidJSON, err := json.Marshal(invalidPayload)
	require.NoError(t, err)
	err = natsClient.Publish(ctx, "test.jsonfilter.reject.input", invalidJSON)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Verify only valid GenericJSON message passed
	receiveMu.Lock()
	assert.Equal(t, 1, len(receivedMessages), "Should only pass valid GenericJSON messages")
	if len(receivedMessages) == 1 {
		assert.Equal(t, "valid", receivedMessages[0].Data["test"])
	}
	receiveMu.Unlock()

	// Check error metrics increased for invalid message
	health := filterComp.Health()
	assert.Greater(t, health.ErrorCount, 0, "Error count should increase for invalid messages")
}
