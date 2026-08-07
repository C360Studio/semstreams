package agentictools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

func TestNewComponentMergesCanonicalPortOverridesIntoDefaults(t *testing.T) {
	rawConfig, err := json.Marshal(agentictools.Config{
		Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
			Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"custom.tool.result.*"}},
		}}},
		Timeout: "45s",
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := agentictools.NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	if got := len(created.InputPorts()); got != 4 {
		t.Fatalf("InputPorts() count = %d, want 4 preserved defaults", got)
	}
	if got := len(created.OutputPorts()); got != 2 {
		t.Fatalf("OutputPorts() count = %d, want 2 merged defaults", got)
	}
	for _, port := range created.OutputPorts() {
		if port.Name != "tool.result" {
			continue
		}
		facts, factsErr := port.Facts()
		if factsErr != nil {
			t.Fatal(factsErr)
		}
		if subjects := facts.NATSSubjects(); len(subjects) != 1 || subjects[0] != "custom.tool.result.*" {
			t.Fatalf("tool.result subjects = %v, want [custom.tool.result.*]", subjects)
		}
		if port.Required || port.Description != "" {
			t.Fatalf("tool.result inherited omitted metadata: %#v", port)
		}
		return
	}
	t.Fatal("merged outputs omit tool.result")
}

func TestNewComponentRejectsNoncanonicalPortOverrides(t *testing.T) {
	tests := []struct {
		name    string
		ports   func() *component.PortConfig
		wantErr string
	}{
		{
			name: "renamed ports",
			ports: func() *component.PortConfig {
				return &component.PortConfig{
					Inputs:  []component.PortDefinition{{Name: "tool_calls", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}}},
					Outputs: []component.PortDefinition{{Name: "tool_results", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}}}},
				}
			},
			wantErr: "unknown override port name",
		},
		{
			name: "kind change",
			ports: func() *component.PortConfig {
				ports := agentictools.DefaultConfig().Ports
				ports.Outputs[1].Config = component.NATSPort{Subject: "tool.result.*"}
				return ports
			},
			wantErr: "does not match default kind",
		},
		{
			name: "direction change",
			ports: func() *component.PortConfig {
				ports := agentictools.DefaultConfig().Ports
				ports.Inputs = append(ports.Inputs, ports.Outputs[1])
				return ports
			},
			wantErr: "unknown override port name",
		},
		{
			name: "duplicate name",
			ports: func() *component.PortConfig {
				ports := agentictools.DefaultConfig().Ports
				ports.Outputs = append(ports.Outputs, ports.Outputs[1])
				return ports
			},
			wantErr: "duplicate",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rawConfig, err := json.Marshal(agentictools.Config{Ports: test.ports(), Timeout: "60s"})
			if err != nil {
				t.Fatal(err)
			}
			created, err := agentictools.NewComponent(rawConfig, component.Dependencies{})
			if err == nil {
				t.Fatalf("NewComponent() succeeded with ports %#v", test.ports())
			}
			if created != nil {
				t.Fatal("NewComponent() returned a component with invalid override")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("NewComponent() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}
