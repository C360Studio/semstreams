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
