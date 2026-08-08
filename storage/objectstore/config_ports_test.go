package objectstore

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestDefaultPortsResolve(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	for _, definition := range config.Ports.Inputs {
		assertObjectStorePortResolves(t, definition, component.DirectionInput)
	}
	for _, definition := range config.Ports.Outputs {
		assertObjectStorePortResolves(t, definition, component.DirectionOutput)
	}
}

func TestDefaultPortsExcludeRequestAPI(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	for _, definition := range config.Ports.Inputs {
		if definition.Name == "api" {
			t.Fatalf("default ObjectStore input %q must be absent", definition.Name)
		}
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			t.Fatalf("resolve default input %q: %v", definition.Name, err)
		}
		facts, err := port.Facts()
		if err != nil {
			t.Fatalf("project default input %q: %v", definition.Name, err)
		}
		if facts.Kind() == component.PortKindNATSRequest {
			t.Fatalf("default ObjectStore input %q must not use nats-request", definition.Name)
		}
	}
}

func TestNewComponentRejectsRemovedRequestAPIInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		definition component.PortDefinition
	}{
		{
			name: "reserved api name with ordinary nats kind",
			definition: component.PortDefinition{
				Name: "api", Config: component.NATSPort{Subject: "storage.objectstore.write"},
			},
		},
		{
			name: "arbitrary nats request input",
			definition: component.PortDefinition{
				Name: "legacy", Config: component.NATSRequestPort{Subject: "storage.legacy.request"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configJSON, err := json.Marshal(Config{Ports: &component.PortConfig{
				Inputs: []component.PortDefinition{test.definition},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewComponent(configJSON, component.Dependencies{}); err == nil {
				t.Fatal("NewComponent accepted removed ObjectStore request API input")
			}
		})
	}
}

func TestNewComponentAcceptsOrdinaryWriteInputs(t *testing.T) {
	t.Parallel()

	tests := []component.PortDefinition{
		{Name: "write", Config: component.NATSPort{Subject: "storage.objectstore.write"}},
		{Name: "write", Config: component.JetStreamPort{
			StreamName: "OBJECTSTORE_WRITES", Subjects: []string{"storage.objectstore.write"},
		}},
	}
	for _, definition := range tests {
		configJSON, err := json.Marshal(Config{Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{definition},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewComponent(configJSON, component.Dependencies{}); err != nil {
			t.Fatalf("NewComponent rejected ordinary %T write input: %v", definition.Config, err)
		}
	}
}

func assertObjectStorePortResolves(t *testing.T, definition component.PortDefinition, direction component.Direction) {
	t.Helper()
	definitionData, err := json.Marshal(definition)
	if err != nil {
		t.Errorf("port %q failed production definition encoding: %v", definition.Name, err)
		return
	}
	var wire map[string]any
	if err := json.Unmarshal(definitionData, &wire); err != nil {
		t.Fatal(err)
	}
	wire["direction"] = direction
	portData, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var port component.Port
	err = json.Unmarshal(portData, &port)
	if err != nil {
		t.Errorf("port %q failed production resolution: %v", definition.Name, err)
	}
}
