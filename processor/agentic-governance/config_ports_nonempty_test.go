package agenticgovernance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestNewComponentMergesCanonicalPortOverridesIntoDefaults(t *testing.T) {
	rawConfig, err := json.Marshal(struct {
		Ports *component.PortConfig `json:"ports"`
	}{Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
		Name: "agent.request.validated",
		Config: component.JetStreamPort{
			StreamName: "AGENT",
			Subjects:   []string{"custom.agent.request.validated.*"},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}

	created, err := NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	if got := len(created.InputPorts()); got != 3 {
		t.Fatalf("InputPorts() count = %d, want 3 preserved defaults", got)
	}
	outputs := governancePortsByName(created.OutputPorts())
	if got := len(outputs); got != 4 {
		t.Fatalf("OutputPorts() count = %d, want 4 merged defaults", got)
	}
	facts, err := outputs["agent.request.validated"].Facts()
	if err != nil {
		t.Fatal(err)
	}
	if subjects := facts.NATSSubjects(); len(subjects) != 1 || subjects[0] != "custom.agent.request.validated.*" {
		t.Fatalf("agent.request.validated subjects = %v, want [custom.agent.request.validated.*]", subjects)
	}
	for _, name := range []string{"agent.task.validated", "agent.response.validated", "violations"} {
		if _, ok := outputs[name]; !ok {
			t.Fatalf("omitted default %s was not preserved: %v", name, outputs)
		}
	}
}

func TestNewComponentRejectsRenamedPortRole(t *testing.T) {
	rawConfig, err := json.Marshal(struct {
		Ports *component.PortConfig `json:"ports"`
	}{Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
		Name: "validated_requests",
		Config: component.JetStreamPort{
			StreamName: "AGENT",
			Subjects:   []string{"agent.request.validated.*"},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}

	created, err := NewComponent(rawConfig, component.Dependencies{})
	if err == nil {
		t.Fatal("NewComponent() succeeded with renamed port role")
	}
	if created != nil {
		t.Fatal("NewComponent() returned a component with renamed port role")
	}
	if !strings.Contains(err.Error(), "unknown override port name") {
		t.Fatalf("NewComponent() error = %v, want unknown override port name", err)
	}
}

func governancePortsByName(ports []component.Port) map[string]component.Port {
	byName := make(map[string]component.Port, len(ports))
	for _, port := range ports {
		byName[port.Name] = port
	}
	return byName
}
