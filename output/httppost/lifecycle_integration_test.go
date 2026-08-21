//go:build integration

package httppost_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/output/httppost"
)

// createTestComponent creates a test instance for lifecycle testing.
// Uses the shared NATS client from httppost_integration_test.go TestMain.
func createTestComponent() component.LifecycleComponent {
	if sharedNATSClient == nil {
		panic("shared NATS client not initialized")
	}

	config := httppost.Config{
		URL:         "http://localhost:8080/test",
		Headers:     map[string]string{"X-Test": "value"},
		Timeout:     30,
		RetryCount:  3,
		ContentType: "application/json",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "nats_input", Config: component.NATSPort{Subject: "test.httppost.output"}, Required: true,
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

	output, err := httppost.NewOutput(rawConfig, deps)
	if err != nil {
		panic("failed to create test component: " + err.Error())
	}

	lifecycleComp, ok := output.(component.LifecycleComponent)
	if !ok {
		panic("component does not implement LifecycleComponent")
	}

	return lifecycleComp
}

// TestHTTPPostOutput_OneShotLifecycle exercises the assembled core-NATS owner.
func TestHTTPPostOutput_OneShotLifecycle(t *testing.T) {
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
