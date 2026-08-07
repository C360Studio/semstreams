package file

import (
	"os"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// createTestComponent creates a test instance for lifecycle testing.
func createTestComponent() component.LifecycleComponent {
	mockClient := &natsclient.Client{}

	// Create a test file for the component to read
	testFilePath := "/tmp/test-lifecycle-input.jsonl"
	testFile, err := os.Create(testFilePath)
	if err == nil {
		testFile.WriteString(`{"test":"data"}` + "\n")
		testFile.Close()
	}

	config := Config{
		Path:     testFilePath,
		Format:   "jsonl",
		Interval: "10ms",
		Loop:     false,
		Ports: &component.PortConfig{
			Outputs: []component.PortDefinition{
				{
					Name: "nats_output", Required: true,
					Config: component.NATSPort{Subject: "test.file.input"},
				},
			},
		},
	}

	deps := InputDeps{
		Name:            "file-input-test",
		Config:          config,
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}

	input, err := NewInput(deps)
	if err != nil {
		panic(err)
	}
	return input
}

// TestFileInput_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestFileInput_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponent)
}
