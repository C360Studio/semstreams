//go:build integration

package jsonmapprocessor_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
	jsonmapprocessor "github.com/c360studio/semstreams/processor/json_map"
	"github.com/nats-io/nats.go/jetstream"
)

// createTestJSONMapComponent creates a test instance for lifecycle testing.
// Uses the shared NATS client from json_map_integration_test.go TestMain.
func createTestJSONMapComponent(t *testing.T) component.LifecycleComponent {
	t.Helper()
	config := jsonmapprocessor.DefaultConfig()
	if sharedNATSClient == nil {
		panic("shared NATS client not initialized")
	}
	js, err := sharedNATSClient.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if _, err := js.CreateStream(t.Context(), jetstream.StreamConfig{
		Name: "S1_JSON_MAP", Subjects: []string{"s1.json.map.input"},
	}); err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	config.Ports.Inputs = []component.PortDefinition{{
		Name: "input",
		Config: component.JetStreamPort{
			StreamName: "S1_JSON_MAP", Subjects: []string{"s1.json.map.input"},
		},
	}}
	deps := component.Dependencies{
		NATSClient: sharedNATSClient,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	comp, err := jsonmapprocessor.NewProcessor(configJSON, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	lifecycleComponent, ok := comp.(component.LifecycleComponent)
	if !ok {
		panic("JSON map processor does not implement component.LifecycleComponent")
	}
	return lifecycleComponent
}

func TestJSONMapTerminalLifecycle(t *testing.T) {
	owner := createTestJSONMapComponent(t)
	if err := owner.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("same-instance restart error = %v, want ErrAlreadyStarted", err)
	}
}
