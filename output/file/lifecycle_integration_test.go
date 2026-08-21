//go:build integration

package file_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/output/file"
)

// createTestComponent creates a test instance for lifecycle testing.
// Uses the shared NATS client from file_integration_test.go TestMain.
func createTestComponent() component.LifecycleComponent {
	if sharedNATSClient == nil {
		panic("shared NATS client not initialized")
	}

	config := file.Config{
		Directory:  "/tmp/test-output",
		FilePrefix: "test",
		Format:     "jsonl",
		Append:     true,
		BufferSize: 100,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "nats_input", Config: component.NATSPort{Subject: "test.file.output"}, Required: true,
				},
			},
		},
	}
	deps := component.Dependencies{
		NATSClient: sharedNATSClient,
		Platform: component.PlatformMeta{
			Org:      "test",
			Platform: "test-platform",
		},
	}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	output, err := file.NewOutput(rawConfig, deps)
	if err != nil {
		panic("failed to create test component: " + err.Error())
	}

	lifecycleComp, ok := output.(component.LifecycleComponent)
	if !ok {
		panic("component does not implement LifecycleComponent")
	}

	return lifecycleComp
}

// TestFileOutput_OneShotLifecycle exercises the assembled core-NATS owner.
func TestFileOutput_OneShotLifecycle(t *testing.T) {
	owner := createTestComponent()
	if err := owner.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Stop(context.Background()); err != nil {
		t.Fatalf("repeated terminal Stop: %v", err)
	}
}
