//go:build integration

package httppost_test

import (
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

// TestHTTPPostOutput_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestHTTPPostOutput_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponent)
}
