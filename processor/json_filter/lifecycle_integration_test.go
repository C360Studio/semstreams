//go:build integration

package jsonfilter_test

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	jsonfilter "github.com/c360studio/semstreams/processor/json_filter"
)

// createTestJSONFilterComponent creates a test instance for lifecycle testing.
// Uses the shared NATS client from json_filter_integration_test.go TestMain.
func createTestJSONFilterComponent() component.LifecycleComponent {
	config := jsonfilter.DefaultConfig()
	if sharedNATSClient == nil {
		panic("shared NATS client not initialized")
	}
	deps := component.Dependencies{
		NATSClient: sharedNATSClient,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	comp, err := jsonfilter.NewProcessor(configJSON, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	lifecycleComponent, ok := comp.(component.LifecycleComponent)
	if !ok {
		panic("JSON filter processor does not implement component.LifecycleComponent")
	}
	return lifecycleComponent
}

// TestJSONFilter_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestJSONFilter_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestJSONFilterComponent)
}
