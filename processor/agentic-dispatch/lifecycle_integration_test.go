//go:build integration

package agenticdispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

var sharedLifecycleNATSClient *natsclient.TestClient

func TestMain(m *testing.M) {
	streams := []natsclient.TestStreamConfig{
		{Name: "USER", Subjects: []string{"user.>"}},
		{Name: "AGENT", Subjects: []string{"agent.>"}},
	}
	var err error
	sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithStreams(streams...),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shared lifecycle NATS client: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := sharedLifecycleNATSClient.Terminate(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean up shared lifecycle NATS client: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() component.LifecycleComponent {
	tc := sharedLifecycleNATSClient
	if tc == nil {
		panic("shared NATS client not initialized")
	}

	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient: tc.Client,
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"test-model": {Model: "test-model", MaxTokens: 128000},
			},
			Defaults: model.DefaultsConfig{Model: "test-model"},
		},
	}

	rawConfig, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	discoverable, err := NewComponent(rawConfig, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	comp, ok := discoverable.(component.LifecycleComponent)
	if !ok {
		panic("component does not implement LifecycleComponent")
	}

	return comp
}

// TestAgenticDispatch_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestAgenticDispatch_ComprehensiveLifecycle(t *testing.T) {
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
