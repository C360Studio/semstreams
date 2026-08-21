//go:build integration

package agenticmodel_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
// Uses the shared NATS client from model_integration_test.go TestMain.
func createTestComponentForLifecycle() component.LifecycleComponent {
	if sharedNATSClient == nil {
		panic("shared NATS client not initialized")
	}

	config := agenticmodel.DefaultConfig()
	// Use a unique consumer suffix for test isolation.
	config.ConsumerNameSuffix = "lifecycle"

	registry := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"default": {
				URL:       "http://localhost:8080/v1",
				Model:     "gpt-4",
				MaxTokens: 128000,
			},
		},
	}

	deps := component.Dependencies{
		NATSClient:    sharedNATSClient,
		ModelRegistry: registry,
	}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	discoverable, err := agenticmodel.NewComponent(rawConfig, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	comp, ok := discoverable.(component.LifecycleComponent)
	if !ok {
		panic("component does not implement LifecycleComponent")
	}

	return comp
}

// TestAgenticModel_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestAgenticModel_ComprehensiveLifecycle(t *testing.T) {
	owner := createTestComponentForLifecycle()
	if err := owner.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("restart error = %v", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeat Stop: %v", err)
	}
}
