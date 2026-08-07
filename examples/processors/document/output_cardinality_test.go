package document

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestNewComponentRejectsMultipleAdvertisedOutputs(t *testing.T) {
	config := DefaultConfig()
	config.Ports.Outputs = append(config.Ports.Outputs, component.PortDefinition{Name: "ignored", Config: component.NATSPort{Subject: "ignored"}})
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewComponent(raw, component.Dependencies{}); err == nil {
		t.Fatal("NewComponent accepted an output it would not publish to")
	}
}
