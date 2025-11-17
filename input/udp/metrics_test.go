package udp

import (
	"testing"

	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
)

func TestUDPMetrics_Creation(t *testing.T) {
	// Create metrics registry
	registry := metric.NewMetricsRegistry()

	// Create UDP metrics
	metrics := newMetrics(registry, 14550, "0.0.0.0")

	// Verify all metrics were created
	if metrics == nil {
		t.Fatal("Expected metrics to be created, but got nil")
	}

	if metrics.packetsReceived == nil {
		t.Fatal("Expected packetsReceived metric to be created")
	}

	if metrics.bytesReceived == nil {
		t.Fatal("Expected bytesReceived metric to be created")
	}

	if metrics.packetsDropped == nil {
		t.Fatal("Expected packetsDropped metric to be created")
	}

	if metrics.bufferUtilization == nil {
		t.Fatal("Expected bufferUtilization metric to be created")
	}

	if metrics.batchSize == nil {
		t.Fatal("Expected batchSize metric to be created")
	}

	if metrics.publishLatency == nil {
		t.Fatal("Expected publishLatency metric to be created")
	}

	if metrics.socketErrors == nil {
		t.Fatal("Expected socketErrors metric to be created")
	}

	if metrics.lastActivity == nil {
		t.Fatal("Expected lastActivity metric to be created")
	}

	t.Log("All UDP metrics successfully created")
}

func TestUDPInput_MetricsIntegration(t *testing.T) {
	// Create mock NATS client
	natsClient := &natsclient.Client{} // This is just for interface compliance

	// Create metrics registry
	registry := metric.NewMetricsRegistry()

	// Create UDP input with metrics using new idiomatic pattern
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      natsClient,
		MetricsRegistry: registry,
		Logger:          nil,
	}
	udpInput := NewInput(deps)

	// Verify metrics were wired up
	if udpInput.metrics == nil {
		t.Fatal("Expected metrics to be created on UDP input")
	}

	// Verify metrics are the correct type
	if udpInput.metrics.packetsReceived == nil {
		t.Fatal("Expected packetsReceived metric to be wired")
	}

	t.Log("UDP input metrics integration successful")
}

func TestUDPInput_NoMetrics(t *testing.T) {
	// Create mock NATS client
	natsClient := &natsclient.Client{} // This is just for interface compliance

	// Create UDP input without metrics (nil registry) using new idiomatic pattern
	deps := InputDeps{
		Config:          testUDPConfig(14550, "127.0.0.1", "test.subject"),
		NATSClient:      natsClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}
	udpInput := NewInput(deps)

	// Verify no metrics were created
	if udpInput.metrics != nil {
		t.Fatal("Expected no metrics when registry is nil")
	}

	t.Log("UDP input correctly handles nil metrics registry")
}

func TestNewUDPMetrics_NilRegistry(t *testing.T) {
	// Test "nil input = nil feature" pattern
	metrics := newMetrics(nil, 14550, "0.0.0.0")

	// Verify nil is returned when registry is nil
	if metrics != nil {
		t.Fatal("Expected nil metrics when registry is nil")
	}

	t.Log("newMetrics correctly returns nil for nil registry")
}
