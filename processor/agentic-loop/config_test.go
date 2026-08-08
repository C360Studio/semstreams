package agenticloop_test

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestConfig_JSONSerialization(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*agenticloop.Config)
	}{
		{
			name: "minimal config",
			modify: func(c *agenticloop.Config) {
				c.LoopsBucket = ""
			},
		},
		{
			name: "full config with buckets",
			modify: func(c *agenticloop.Config) {
				c.MaxIterations = 25
				c.Timeout = "180s"
			},
		},
		{
			name: "custom max iterations",
			modify: func(c *agenticloop.Config) {
				c.MaxIterations = 50
				c.Timeout = "300s"
				c.LoopsBucket = "CUSTOM_LOOPS"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validBaseConfig()
			if tt.modify != nil {
				tt.modify(&config)
			}

			// Marshal
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal
			var decoded agenticloop.Config
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Verify round-trip
			if decoded.MaxIterations != config.MaxIterations {
				t.Errorf("MaxIterations = %v, want %v", decoded.MaxIterations, config.MaxIterations)
			}
			if decoded.Timeout != config.Timeout {
				t.Errorf("Timeout = %v, want %v", decoded.Timeout, config.Timeout)
			}
			if decoded.LoopsBucket != config.LoopsBucket {
				t.Errorf("LoopsBucket = %v, want %v", decoded.LoopsBucket, config.LoopsBucket)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*agenticloop.Config)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid minimal config",
			wantErr: false,
		},
		{
			name: "valid with higher max iterations",
			modify: func(c *agenticloop.Config) {
				c.MaxIterations = 50
				c.Timeout = "180s"
			},
			wantErr: false,
		},
		{
			name: "zero max iterations",
			modify: func(c *agenticloop.Config) {
				c.MaxIterations = 0
			},
			wantErr: true,
			errMsg:  "max_iterations",
		},
		{
			name: "negative max iterations",
			modify: func(c *agenticloop.Config) {
				c.MaxIterations = -1
			},
			wantErr: true,
			errMsg:  "max_iterations",
		},
		{
			name: "empty timeout",
			modify: func(c *agenticloop.Config) {
				c.Timeout = ""
			},
			wantErr: true,
			errMsg:  "timeout",
		},
		{
			name: "invalid timeout format",
			modify: func(c *agenticloop.Config) {
				c.Timeout = "not-a-duration"
			},
			wantErr: true,
			errMsg:  "timeout",
		},
		{
			name: "negative timeout",
			modify: func(c *agenticloop.Config) {
				c.Timeout = "-5s"
			},
			wantErr: true,
			errMsg:  "timeout",
		},
		{
			name: "empty loops bucket",
			modify: func(c *agenticloop.Config) {
				c.LoopsBucket = ""
			},
			wantErr: true,
			errMsg:  "loops_bucket",
		},
		{
			name: "valid edge case max iterations (1)",
			modify: func(c *agenticloop.Config) {
				c.MaxIterations = 1
			},
			wantErr: false,
		},
		{
			name: "valid short timeout",
			modify: func(c *agenticloop.Config) {
				c.Timeout = "1s"
			},
			wantErr: false,
		},
		{
			name: "valid long timeout",
			modify: func(c *agenticloop.Config) {
				c.Timeout = "10m"
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validBaseConfig()
			if tt.modify != nil {
				tt.modify(&config)
			}
			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !containsIgnoreCase(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := agenticloop.DefaultConfig()

	// Verify default max iterations
	if cfg.MaxIterations != 20 {
		t.Errorf("DefaultConfig() max_iterations = %d, want 20", cfg.MaxIterations)
	}

	// Verify default timeout
	if cfg.Timeout != "120s" {
		t.Errorf("DefaultConfig() timeout = %s, want 120s", cfg.Timeout)
	}

	// Verify default loops bucket
	if cfg.LoopsBucket != "AGENT_LOOPS" {
		t.Errorf("DefaultConfig() loops_bucket = %s, want AGENT_LOOPS", cfg.LoopsBucket)
	}

	// Verify ports are configured
	if cfg.Ports == nil {
		t.Fatal("DefaultConfig() ports should not be nil")
	}

	// Verify input ports (includes agent.approval_response for human-
	// in-the-loop flow, and the ADR-039 verdict ports
	// agent.toolcall.approved/rejected for subject-mode tool-call
	// governance).
	if len(cfg.Ports.Inputs) != 8 {
		t.Errorf("DefaultConfig() input ports count = %d, want 8", len(cfg.Ports.Inputs))
	}

	// Verify output ports (agent.created + agent.failed declared as explicit
	// output ports so downstream products can override their subjects via
	// port config, matching the existing treatment of agent.request etc.
	// agent.toolcall.proposed added in ADR-039 — published before dispatch
	// when governance mode is audit or enforce).
	if len(cfg.Ports.Outputs) != 11 {
		t.Errorf("DefaultConfig() output ports count = %d, want 11", len(cfg.Ports.Outputs))
	}

	// Verify specific input subjects
	expectedInputs := map[string]string{
		"agent.task":     "agent.task.*",
		"agent.response": "agent.response.>",
		"tool.result":    "tool.result.>",
	}
	for name, subject := range expectedInputs {
		found := false
		for _, port := range cfg.Ports.Inputs {
			resolved, err := port.Resolve(component.DirectionInput)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := resolved.Facts()
			if err != nil {
				t.Fatal(err)
			}
			if port.Name == name && len(facts.NATSSubjects()) == 1 && facts.NATSSubjects()[0] == subject {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultConfig() missing input port %s with subject %s", name, subject)
		}
	}

	// Verify specific output subjects
	expectedOutputs := map[string]string{
		"graph_mutations": "graph.mutation.>",
		"agent.request":   "agent.request.*",
		"tool.execute":    "tool.execute.*",
		"agent.complete":  "agent.complete.*",
	}
	for _, port := range cfg.Ports.Outputs {
		request, ok := port.Config.(component.NATSRequestPort)
		if port.Name == "graph_mutations" &&
			(!ok || request.Interface == nil || request.Interface.Type != "semstreams.graph.mutation" || !port.Required) {
			t.Errorf("graph_mutations output is not the required typed request port: %#v", port)
		}
	}
	for name, subject := range expectedOutputs {
		found := false
		for _, port := range cfg.Ports.Outputs {
			resolved, err := port.Resolve(component.DirectionOutput)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := resolved.Facts()
			if err != nil {
				t.Fatal(err)
			}
			if port.Name == name && len(facts.NATSSubjects()) == 1 && facts.NATSSubjects()[0] == subject {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultConfig() missing output port %s with subject %s", name, subject)
		}
	}

	// Verify default config is valid
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid, got error: %v", err)
	}
}

func TestConfig_TimeoutParsing(t *testing.T) {
	tests := []struct {
		name      string
		timeout   string
		wantValid bool
	}{
		{"seconds", "30s", true},
		{"minutes", "5m", true},
		{"hours", "1h", true},
		{"milliseconds", "500ms", true},
		{"combined", "1h30m", true},
		{"invalid format", "30", false},
		{"invalid unit", "30x", false},
		{"empty", "", false},
		{"negative", "-10s", false},
		{"zero", "0s", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validBaseConfig()
			config.Timeout = tt.timeout

			err := config.Validate()
			isValid := err == nil

			if isValid != tt.wantValid {
				t.Errorf("Validate() with timeout=%q: valid=%v, want %v", tt.timeout, isValid, tt.wantValid)
			}
		})
	}
}

func TestConfig_MaxIterationsBoundaries(t *testing.T) {
	tests := []struct {
		name          string
		maxIterations int
		wantValid     bool
	}{
		{"zero", 0, false},
		{"one", 1, true},
		{"default", 20, true},
		{"high", 100, true},
		{"very high", 1000, true},
		{"negative", -1, false},
		{"negative large", -100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validBaseConfig()
			config.MaxIterations = tt.maxIterations

			err := config.Validate()
			isValid := err == nil

			if isValid != tt.wantValid {
				t.Errorf("Validate() with max_iterations=%d: valid=%v, want %v", tt.maxIterations, isValid, tt.wantValid)
			}
		})
	}
}

func TestConfig_BucketNames(t *testing.T) {
	tests := []struct {
		name        string
		loopsBucket string
		wantValid   bool
	}{
		{"default name", "AGENT_LOOPS", true},
		{"custom name", "MY_LOOPS", true},
		{"empty loops bucket", "", false},
		{"whitespace loops", "  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validBaseConfig()
			config.LoopsBucket = tt.loopsBucket

			err := config.Validate()
			isValid := err == nil

			if isValid != tt.wantValid {
				t.Errorf("Validate() valid=%v, want %v (error: %v)", isValid, tt.wantValid, err)
			}
		})
	}
}

func TestConfig_TrajectoryEvidenceStorageInstance(t *testing.T) {
	config := agenticloop.DefaultConfig()
	if config.TrajectoryEvidenceStorageInstance != "objectstore" {
		t.Fatalf("default trajectory evidence storage instance = %q, want objectstore", config.TrajectoryEvidenceStorageInstance)
	}
	config.TrajectoryEvidenceStorageInstance = " "
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() accepted an empty trajectory evidence storage instance")
	}
}

// validBaseConfig returns a Config with valid defaults for all fields.
// Tests override specific fields to test validation of those fields.
func validBaseConfig() agenticloop.Config {
	return agenticloop.Config{
		TrajectoryEvidenceStorageInstance: "objectstore",
		MaxIterations:                     20,
		Timeout:                           "120s",
		LoopsBucket:                       "AGENT_LOOPS",
		Context:                           agenticloop.DefaultContextConfig(),
	}
}

// Helper functions

func containsIgnoreCase(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	return contains(s, substr)
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
