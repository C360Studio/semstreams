//go:build integration

package agenticloop_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
// Uses the shared NATS client from loop_integration_test.go TestMain.
func createTestComponentForLifecycle() component.LifecycleComponent {
	tc := sharedTestClient
	if tc == nil {
		panic("shared NATS client not initialized - TestMain from loop_integration_test.go must run first")
	}

	config := agenticloop.DefaultConfig()
	deps := component.Dependencies{
		NATSClient: tc.Client,
	}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	discoverable, err := agenticloop.NewComponent(rawConfig, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	comp, ok := discoverable.(component.LifecycleComponent)
	if !ok {
		panic("component does not implement LifecycleComponent")
	}

	return comp
}

// TestAgenticLoop_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestAgenticLoop_ComprehensiveLifecycle(t *testing.T) {
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
