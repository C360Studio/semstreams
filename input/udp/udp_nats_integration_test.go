//go:build integration

package udp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	gonats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// Behavior-based tests using ComponentConfig and testcontainers
func TestIntegration_UDPInput_Creation_ValidConfig(t *testing.T) {
	// Use testcontainer for real NATS
	testClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())

	// Create UDP config
	udpConfig := testUDPConfig(14550, "127.0.0.1", "test.udp.mavlink")
	configJSON, err := json.Marshal(udpConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	// Create REAL component
	udpComponent, err := CreateInput(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, udpComponent)

	// Cast to Input to verify implementation
	udpInput, ok := udpComponent.(*Input)
	require.True(t, ok, "Component should be Input type")

	// Verify real behavior - component metadata
	meta := udpInput.Meta()
	require.Equal(t, "input", meta.Type)
	require.Contains(t, meta.Description, "127.0.0.1:14550")

	// Verify real behavior - port configuration
	inputPorts := udpInput.InputPorts()
	require.Len(t, inputPorts, 1)
	networkPort := inputPorts[0].Config.(component.NetworkPort)
	require.Equal(t, 14550, networkPort.Port)
	require.Equal(t, "127.0.0.1", networkPort.Host)

	// Verify NATS output configuration
	outputPorts := udpInput.OutputPorts()
	require.Len(t, outputPorts, 1)
	natsPort := outputPorts[0].Config.(component.NATSPort)
	require.Equal(t, "test.udp.mavlink", natsPort.Subject)
}

func TestIntegration_UDPInput_Creation_DefaultConfig(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())

	// Create empty config to use defaults
	configJSON := json.RawMessage(`{}`)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	udpComponent, err := CreateInput(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, udpComponent)

	udpInput := udpComponent.(*Input)

	// Verify defaults were applied
	require.Equal(t, 14550, udpInput.port)
	require.Equal(t, "0.0.0.0", udpInput.bind)
	require.Equal(t, "input.udp.mavlink", udpInput.subject) // Component-owned default subject
}

func TestIntegration_UDPInput_Creation_CustomConfig(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())

	// Create custom UDP config
	udpConfig := testUDPConfig(12345, "192.168.1.1", "custom.udp.subject")
	configJSON, err := json.Marshal(udpConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	udpComponent, err := CreateInput(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, udpComponent)

	udpInput := udpComponent.(*Input)

	// Verify custom configuration was applied
	require.Equal(t, 12345, udpInput.port)
	require.Equal(t, "192.168.1.1", udpInput.bind)
	require.Equal(t, "custom.udp.subject", udpInput.subject)
	// Note: name is set by ComponentManager, not via config

	// Verify metadata
	meta := udpInput.Meta()
	require.Equal(t, "udp-input", meta.Name) // Default name from constructor
}

func TestIntegration_UDPInput_Creation_InvalidPort(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())

	testCases := []struct {
		name string
		port any
	}{
		{"port too high", 99999},
		{"negative port", -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Keep the invalid value on the wire so the production decoder,
			// rather than typed fixture marshaling, owns its rejection.
			configJSON := json.RawMessage(fmt.Sprintf(`{
				"ports": {
					"inputs": [{
						"name": "udp_socket",
						"config": {"kind": "network", "protocol": "udp", "host": "127.0.0.1", "port": %d}
					}],
					"outputs": [{
						"name": "nats_output",
						"config": {"kind": "nats", "subject": "test.udp"}
					}]
				}
			}`, tc.port))

			// Create component dependencies
			deps := component.Dependencies{
				NATSClient: testClient.Client,
				Platform: component.PlatformMeta{
					Org:      "test",
					Platform: "test-platform",
				},
			}

			// The merged effective configuration rejects invalid ports at creation time.
			udpComponent, err := CreateInput(configJSON, deps)
			require.Error(t, err) // Creation should fail with invalid port
			require.Nil(t, udpComponent)

			// Verify error mentions port validation
			require.Contains(t, err.Error(), "port")
		})
	}
}

func TestIntegration_UDPInput_Lifecycle_StartStop(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithFastStartup())

	// Create UDP config with random high port to avoid conflicts
	randomPort := 50000 + (int(time.Now().UnixNano()) % 10000) // Random port 50000-59999
	udpConfig := testUDPConfig(randomPort, "127.0.0.1", "test.udp.lifecycle")
	configJSON, err := json.Marshal(udpConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	udpComponent, err := CreateInput(configJSON, deps)
	require.NoError(t, err)

	// Cast to lifecycle component
	udpInput := udpComponent.(component.LifecycleComponent)

	// Test Initialize
	err = udpInput.Initialize()
	require.NoError(t, err)

	// Test Start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = udpInput.Start(ctx)
	require.NoError(t, err)

	// Verify actually running
	health := udpInput.Health()
	require.True(t, health.Healthy, "Component should be healthy after start")

	// Test Stop
	err = udpInput.Stop(5 * time.Second)
	require.NoError(t, err)

	// Verify actually stopped
	health = udpInput.Health()
	require.False(t, health.Healthy, "Component should be unhealthy after stop")
}

// Integration test with actual UDP communication and real NATS
func TestIntegration_UDPInput_Integration_RealUDPAndNATS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create real NATS client with JetStream for message verification
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())

	// Find available port for UDP
	port := findAvailablePort(t)
	subject := "integration.udp.test"

	// Create UDP config
	udpConfig := testUDPConfig(port, "127.0.0.1", subject)
	configJSON, err := json.Marshal(udpConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	// Create real UDP component
	udpComponent, err := CreateInput(configJSON, deps)
	require.NoError(t, err)

	udpInput := udpComponent.(component.LifecycleComponent)

	// Initialize and start
	require.NoError(t, udpInput.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, udpInput.Start(ctx))
	defer udpInput.Stop(5 * time.Second)

	// Verify component is healthy and socket is bound
	health := udpInput.Health()
	require.True(t, health.Healthy, "UDP input should be healthy after start")

	// Set up NATS subscriber to verify message flow
	nc := testClient.GetNativeConnection()
	msgCh := make(chan []byte, 1)

	sub, err := nc.Subscribe(subject, func(msg *gonats.Msg) {
		msgCh <- msg.Data
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Allow some time for subscription to be ready
	time.Sleep(100 * time.Millisecond)

	// Send real UDP data
	testData := []byte("integration test message")
	sendTestUDPData(t, port, testData)

	// Verify message reaches NATS
	select {
	case receivedData := <-msgCh:
		require.Equal(t, testData, receivedData, "Message should flow from UDP to NATS unchanged")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for UDP message to reach NATS")
	}

	// Verify metrics updated
	udpInputImpl := udpComponent.(*Input)
	require.Greater(t, udpInputImpl.messagesReceived.Load(), int64(0), "Should have received messages")
	require.Greater(t, udpInputImpl.bytesReceived.Load(), int64(0), "Should have received bytes")

	flow := udpInputImpl.DataFlow()
	require.Greater(t, flow.MessagesPerSecond, float64(0), "Should have message rate > 0")
	require.Greater(t, flow.BytesPerSecond, float64(0), "Should have byte rate > 0")
}

// Integration test for multiple UDP messages and buffer behavior
func TestIntegration_UDPInput_Integration_MultipleMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	port := findAvailablePort(t)
	subject := "integration.udp.multi"

	// Create UDP config
	udpConfig := testUDPConfig(port, "127.0.0.1", subject)
	configJSON, err := json.Marshal(udpConfig)
	require.NoError(t, err)

	// Create component dependencies
	deps := component.Dependencies{
		NATSClient: testClient.Client,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	udpComponent, err := CreateInput(configJSON, deps)
	require.NoError(t, err)

	udpInput := udpComponent.(component.LifecycleComponent)
	require.NoError(t, udpInput.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	require.NoError(t, udpInput.Start(ctx))
	defer udpInput.Stop(5 * time.Second)

	// Set up NATS subscriber to collect all messages
	nc := testClient.GetNativeConnection()
	var receivedMessages [][]byte
	msgCh := make(chan []byte, 10)

	sub, err := nc.Subscribe(subject, func(msg *gonats.Msg) {
		msgCh <- msg.Data
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	time.Sleep(100 * time.Millisecond) // Allow subscription to be ready

	// Send multiple UDP messages
	const numMessages = 5
	expectedMessages := make([][]byte, numMessages)

	for i := 0; i < numMessages; i++ {
		testData := []byte(fmt.Sprintf("test message %d", i))
		expectedMessages[i] = testData
		sendTestUDPData(t, port, testData)
		time.Sleep(50 * time.Millisecond) // Small delay between messages
	}

	// Collect all messages
	timeout := time.After(10 * time.Second)
	for len(receivedMessages) < numMessages {
		select {
		case msg := <-msgCh:
			receivedMessages = append(receivedMessages, msg)
		case <-timeout:
			t.Fatalf("Timeout: received %d/%d messages", len(receivedMessages), numMessages)
		}
	}

	// Verify all messages received correctly
	require.Len(t, receivedMessages, numMessages, "Should receive all sent messages")

	for i, expected := range expectedMessages {
		found := false
		for _, received := range receivedMessages {
			if string(expected) == string(received) {
				found = true
				break
			}
		}
		require.True(t, found, "Message %d should be received: %s", i, string(expected))
	}

	// Verify metrics reflect all messages
	udpInputImpl := udpComponent.(*Input)
	require.GreaterOrEqual(t, udpInputImpl.messagesReceived.Load(), int64(numMessages),
		"Should have received at least %d messages", numMessages)
}
