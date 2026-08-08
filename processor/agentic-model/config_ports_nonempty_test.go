package agenticmodel_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

func TestModelConstructorsMergeCanonicalPortOverridesIntoDefaults(t *testing.T) {
	constructors := map[string]func(json.RawMessage, component.Dependencies) (component.Discoverable, error){
		"NewComponent": agenticmodel.NewComponent,
		"NewComponentWithOptions": func(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
			return agenticmodel.NewComponentWithOptions(rawConfig, deps)
		},
	}

	for name, constructor := range constructors {
		t.Run(name, func(t *testing.T) {
			rawConfig, err := json.Marshal(struct {
				Ports *component.PortConfig `json:"ports"`
			}{Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
				Name: "agent.response",
				Config: component.JetStreamPort{
					StreamName: "AGENT",
					Subjects:   []string{"custom.agent.response.*"},
				},
			}}}})
			if err != nil {
				t.Fatal(err)
			}

			created, err := constructor(rawConfig, modelDependencies())
			if err != nil {
				t.Fatalf("constructor error = %v", err)
			}
			if got := len(created.InputPorts()); got != 1 {
				t.Fatalf("InputPorts() count = %d, want 1 preserved default", got)
			}
			outputs := portsByName(created.OutputPorts())
			if got := len(outputs); got != 2 {
				t.Fatalf("OutputPorts() count = %d, want 2 merged defaults", got)
			}
			response := outputs["agent.response"]
			facts, err := response.Facts()
			if err != nil {
				t.Fatal(err)
			}
			if subjects := facts.NATSSubjects(); len(subjects) != 1 || subjects[0] != "custom.agent.response.*" {
				t.Fatalf("agent.response subjects = %v, want [custom.agent.response.*]", subjects)
			}
			if _, ok := outputs["agent.stream"]; !ok {
				t.Fatalf("omitted default agent.stream was not preserved: %v", outputs)
			}
		})
	}
}

func TestModelConstructorsRejectRenamedPortRole(t *testing.T) {
	constructors := map[string]func(json.RawMessage, component.Dependencies) (component.Discoverable, error){
		"NewComponent": agenticmodel.NewComponent,
		"NewComponentWithOptions": func(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
			return agenticmodel.NewComponentWithOptions(rawConfig, deps)
		},
	}
	rawConfig, err := json.Marshal(struct {
		Ports *component.PortConfig `json:"ports"`
	}{Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
		Name: "model.response",
		Config: component.JetStreamPort{
			StreamName: "AGENT",
			Subjects:   []string{"agent.response.*"},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}

	for name, constructor := range constructors {
		t.Run(name, func(t *testing.T) {
			created, err := constructor(rawConfig, modelDependencies())
			if err == nil {
				t.Fatal("constructor succeeded with renamed port role")
			}
			if created != nil {
				t.Fatal("constructor returned a component with renamed port role")
			}
			if !strings.Contains(err.Error(), "unknown override port name") {
				t.Fatalf("constructor error = %v, want unknown override port name", err)
			}
		})
	}
}

func modelDependencies() component.Dependencies {
	return component.Dependencies{ModelRegistry: &model.Registry{Endpoints: map[string]*model.EndpointConfig{
		"default": {URL: "http://localhost:8080/v1", Model: "test-model", MaxTokens: 1024},
	}}}
}

func portsByName(ports []component.Port) map[string]component.Port {
	byName := make(map[string]component.Port, len(ports))
	for _, port := range ports {
		byName[port.Name] = port
	}
	return byName
}
