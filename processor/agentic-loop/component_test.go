package agenticloop_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestComponent_Meta(t *testing.T) {
	comp := createComponentForTest(t)

	meta := comp.Meta()

	if meta.Name != "agentic-loop" {
		t.Errorf("Meta().Name = %s, want agentic-loop", meta.Name)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta().Type = %s, want processor", meta.Type)
	}
	if meta.Description == "" {
		t.Error("Meta().Description should not be empty")
	}
	if meta.Version == "" {
		t.Error("Meta().Version should not be empty")
	}
}

func TestComponent_InputPorts(t *testing.T) {
	comp := createComponentForTest(t)

	ports := comp.InputPorts()

	if len(ports) != 5 {
		t.Fatalf("InputPorts() count = %d, want 5", len(ports))
	}

	// Expected input ports with required flag
	type portExpectation struct {
		subject  string
		required bool
	}
	expected := map[string]portExpectation{
		"agent.task":     {"agent.task.*", true},
		"agent.response": {"agent.response.>", true},
		"tool.result":    {"tool.result.>", true},
		"agent.signal":   {"agent.signal.*", false}, // Optional - not all deployments need signal handling
		"agent.boid":     {"agent.boid.>", false},   // Optional - Boid steering signals for coordination
	}

	for _, port := range ports {
		expectation, ok := expected[port.Name]
		if !ok {
			t.Errorf("Unexpected input port: %s", port.Name)
			continue
		}

		if port.Direction != component.DirectionInput {
			t.Errorf("Port %s direction = %v, want DirectionInput", port.Name, port.Direction)
		}

		if port.Required != expectation.required {
			t.Errorf("Port %s required = %v, want %v", port.Name, port.Required, expectation.required)
		}

		// Verify JetStream config
		jsConfig, ok := port.Config.(component.JetStreamPort)
		if !ok {
			t.Errorf("Port %s config should be JetStreamPort, got %T", port.Name, port.Config)
			continue
		}
		if len(jsConfig.Subjects) == 0 || jsConfig.Subjects[0] != expectation.subject {
			t.Errorf("Port %s subject = %v, want %s", port.Name, jsConfig.Subjects, expectation.subject)
		}
	}
}

func TestComponent_OutputPorts(t *testing.T) {
	comp := createComponentForTest(t)

	ports := comp.OutputPorts()

	if len(ports) != 7 {
		t.Fatalf("OutputPorts() count = %d, want 7", len(ports))
	}

	// Expected output ports
	expected := map[string]string{
		"agent.request":            "agent.request.*",
		"tool.execute":             "tool.execute.*",
		"agent.complete":           "agent.complete.*",
		"agent.created":            "agent.created.*",
		"agent.failed":             "agent.failed.*",
		"agent.context.compaction": "agent.context.compaction.*",
		"agent.approval_pending":   "agent.approval_pending.*",
	}

	for _, port := range ports {
		expectedSubject, ok := expected[port.Name]
		if !ok {
			t.Errorf("Unexpected output port: %s", port.Name)
			continue
		}

		if port.Direction != component.DirectionOutput {
			t.Errorf("Port %s direction = %v, want DirectionOutput", port.Name, port.Direction)
		}

		// Verify JetStream config
		jsConfig, ok := port.Config.(component.JetStreamPort)
		if !ok {
			t.Errorf("Port %s config should be JetStreamPort, got %T", port.Name, port.Config)
			continue
		}
		if len(jsConfig.Subjects) == 0 || jsConfig.Subjects[0] != expectedSubject {
			t.Errorf("Port %s subject = %v, want %s", port.Name, jsConfig.Subjects, expectedSubject)
		}
	}
}

func TestComponent_KVPorts(t *testing.T) {
	comp := createComponentForTest(t)

	// Component should expose KV ports via component interface
	discoverable, ok := comp.(component.Discoverable)
	if !ok {
		t.Fatal("Component should implement Discoverable interface")
	}

	// Check for KV ports in the component's port config
	// This may be implementation-specific, but we can verify via config
	config := agenticloop.DefaultConfig()

	if len(config.Ports.KVWrite) != 1 {
		t.Errorf("Config should have 1 KV write port, got %d", len(config.Ports.KVWrite))
	}

	// Verify KV bucket names
	expectedBuckets := []string{"AGENT_LOOPS"}
	foundBuckets := make(map[string]bool)
	for _, kvPort := range config.Ports.KVWrite {
		foundBuckets[kvPort.Bucket] = true
	}

	for _, bucket := range expectedBuckets {
		if !foundBuckets[bucket] {
			t.Errorf("Expected KV write port for bucket %s", bucket)
		}
	}

	_ = discoverable // Used
}

func TestComponent_ConfigSchema(t *testing.T) {
	comp := createComponentForTest(t)

	schema := comp.ConfigSchema()

	// ConfigSchema has Properties and Required fields (no Type field)
	if len(schema.Properties) == 0 {
		t.Error("ConfigSchema().Properties should not be empty")
	}

	// Verify expected properties exist
	expectedProps := []string{"max_iterations", "timeout", "loops_bucket", "ports"}
	for _, propName := range expectedProps {
		if _, ok := schema.Properties[propName]; !ok {
			t.Errorf("ConfigSchema() should have %q property", propName)
		}
	}

	// Verify max_iterations property
	maxIterProp, ok := schema.Properties["max_iterations"]
	if !ok {
		t.Fatal("ConfigSchema() should have 'max_iterations' property")
	}
	if maxIterProp.Type != "int" {
		t.Errorf("max_iterations type = %s, want int", maxIterProp.Type)
	}

	// Verify timeout property
	timeoutProp, ok := schema.Properties["timeout"]
	if !ok {
		t.Fatal("ConfigSchema() should have 'timeout' property")
	}
	if timeoutProp.Type != "string" {
		t.Errorf("timeout type = %s, want string", timeoutProp.Type)
	}

	// Verify loops_bucket property
	loopsBucketProp, ok := schema.Properties["loops_bucket"]
	if !ok {
		t.Fatal("ConfigSchema() should have 'loops_bucket' property")
	}
	if loopsBucketProp.Type != "string" {
		t.Errorf("loops_bucket type = %s, want string", loopsBucketProp.Type)
	}

}

func TestComponent_Health_NotStarted(t *testing.T) {
	comp := createComponentForTest(t)

	health := comp.Health()

	if health.Healthy {
		t.Error("Health() should be unhealthy before Start()")
	}
}

func TestNewComponent_ValidConfig(t *testing.T) {
	config := agenticloop.DefaultConfig()
	config.MaxIterations = 25
	config.Timeout = "180s"

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil, // Unit test doesn't need real NATS
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() failed: %v", err)
	}
	if comp == nil {
		t.Fatal("NewComponent() returned nil component")
	}
}

func TestNewComponent_MissingNATSClient(t *testing.T) {
	config := agenticloop.DefaultConfig()

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil,
	}

	// Component creation should succeed even without NATS client
	// (error happens on Start, not creation)
	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() should succeed without NATS client, got error: %v", err)
	}
	if comp == nil {
		t.Fatal("NewComponent() returned nil component")
	}
}

func TestNewComponent_InvalidConfigJSON(t *testing.T) {
	rawConfig := []byte(`{invalid json}`)

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err == nil {
		t.Fatal("NewComponent() should fail with invalid JSON")
	}
	if comp != nil {
		t.Error("NewComponent() should return nil on error")
	}
}

func TestNewComponent_InvalidMaxIterations(t *testing.T) {
	config := agenticloop.DefaultConfig()
	config.MaxIterations = 0 // Invalid

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err == nil {
		t.Fatal("NewComponent() should fail with invalid max_iterations")
	}
	if comp != nil {
		t.Error("NewComponent() should return nil on error")
	}
}

func TestNewComponent_InvalidTimeout(t *testing.T) {
	config := agenticloop.DefaultConfig()
	config.Timeout = "invalid"

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err == nil {
		t.Fatal("NewComponent() should fail with invalid timeout")
	}
	if comp != nil {
		t.Error("NewComponent() should return nil on error")
	}
}

func TestNewComponent_EmptyBucketNames(t *testing.T) {
	config := agenticloop.DefaultConfig()
	config.LoopsBucket = "" // Invalid

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err == nil {
		t.Fatal("NewComponent() should fail with empty loops_bucket")
	}
	if comp != nil {
		t.Error("NewComponent() should return nil on error")
	}
}

func TestNewComponent_CustomMaxIterations(t *testing.T) {
	tests := []struct {
		name          string
		maxIterations int
		wantErr       bool
	}{
		{
			name:          "valid custom 10",
			maxIterations: 10,
			wantErr:       false,
		},
		{
			name:          "valid custom 50",
			maxIterations: 50,
			wantErr:       false,
		},
		{
			name:          "valid custom 100",
			maxIterations: 100,
			wantErr:       false,
		},
		{
			name:          "invalid zero",
			maxIterations: 0,
			wantErr:       true,
		},
		{
			name:          "invalid negative",
			maxIterations: -5,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := agenticloop.DefaultConfig()
			config.MaxIterations = tt.maxIterations

			rawConfig, err := json.Marshal(config)
			if err != nil {
				t.Fatalf("Marshal config failed: %v", err)
			}

			deps := component.Dependencies{
				NATSClient: nil,
			}

			comp, err := agenticloop.NewComponent(rawConfig, deps)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewComponent() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && comp == nil {
				t.Error("NewComponent() should return component when valid")
			}

			if tt.wantErr && comp != nil {
				t.Error("NewComponent() should return nil on error")
			}
		})
	}
}

func TestNewComponent_CustomBucketNames(t *testing.T) {
	config := agenticloop.DefaultConfig()
	config.LoopsBucket = "CUSTOM_LOOPS"

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() with custom buckets failed: %v", err)
	}
	if comp == nil {
		t.Fatal("NewComponent() returned nil component")
	}

	// Verify custom bucket names are used
	// This may require exposing config or checking via component methods
}

func TestNewComponent_VariousTimeouts(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{
			name:    "valid 30s",
			timeout: "30s",
			wantErr: false,
		},
		{
			name:    "valid 5m",
			timeout: "5m",
			wantErr: false,
		},
		{
			name:    "valid 1h",
			timeout: "1h",
			wantErr: false,
		},
		{
			name:    "invalid format",
			timeout: "invalid",
			wantErr: true,
		},
		{
			name:    "empty timeout",
			timeout: "",
			wantErr: true,
		},
		{
			name:    "negative timeout",
			timeout: "-10s",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := agenticloop.DefaultConfig()
			config.Timeout = tt.timeout

			rawConfig, err := json.Marshal(config)
			if err != nil {
				t.Fatalf("Marshal config failed: %v", err)
			}

			deps := component.Dependencies{
				NATSClient: nil,
			}

			comp, err := agenticloop.NewComponent(rawConfig, deps)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewComponent() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && comp == nil {
				t.Error("NewComponent() should return component when valid")
			}

			if tt.wantErr && comp != nil {
				t.Error("NewComponent() should return nil on error")
			}
		})
	}
}

func TestComponent_Lifecycle(t *testing.T) {
	comp := createComponentForTest(t)

	// Component should implement LifecycleComponent interface
	lifecycle, ok := comp.(interface {
		Start(ctx context.Context) error
		Stop(timeout time.Duration) error
	})
	if !ok {
		t.Fatal("Component should implement lifecycle methods (Start, Stop)")
	}

	// Before start, health should be unhealthy
	health := comp.Health()
	if health.Healthy {
		t.Error("Health should be unhealthy before Start()")
	}

	// Note: In unit tests without real NATS, Start may fail
	// That's expected. Just verify it doesn't panic
	ctx := context.Background()
	_ = lifecycle.Start(ctx)

	// Stop should not panic
	_ = lifecycle.Stop(5 * time.Second)
}

func TestComponent_StopWithTimeout(t *testing.T) {
	comp := createComponentForTest(t)

	lifecycle, ok := comp.(interface {
		Start(ctx context.Context) error
		Stop(timeout time.Duration) error
	})
	if !ok {
		t.Fatal("Component should implement lifecycle methods")
	}

	// Start (may fail in unit tests, that's okay)
	ctx := context.Background()
	_ = lifecycle.Start(ctx)

	// Stop should use timeout properly (not ignore with _)
	// This tests that Stop() respects the timeout duration
	err := lifecycle.Stop(5 * time.Second)

	// Should return (not hang indefinitely)
	// Error is acceptable in unit tests without real NATS
	_ = err
}

// Helper function to create component for testing
func createComponentForTest(t *testing.T) component.Discoverable {
	config := agenticloop.DefaultConfig()

	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}

	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := agenticloop.NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent() failed: %v", err)
	}

	return comp
}
