package composition_test

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

// fakeSpec is one package-local fake factory: a declarer and nothing else.
// The Factory returns an error so any construction attempt is loud — the
// offline validator must never construct.
type fakeSpec struct {
	name       string
	typ        string
	inputs     []component.PortDefinition
	outputs    []component.PortDefinition
	declareErr error
}

func fakeRegistry(t *testing.T, specs ...fakeSpec) *component.Registry {
	t.Helper()
	registry := component.NewRegistry()
	for _, spec := range specs {
		spec := spec
		err := registry.RegisterWithConfig(component.RegistrationConfig{
			Name: spec.name, Type: spec.typ, Protocol: "fake", Domain: "test", Version: "0.0.1",
			Description: "fake " + spec.name,
			Schema:      component.ConfigSchema{Properties: map[string]component.PropertySchema{"knob": {Type: "string"}}},
			Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
				return nil, errors.New("offline validation must not construct " + spec.name)
			},
			Ports: func(json.RawMessage, string) (component.PortConfig, error) {
				if spec.declareErr != nil {
					return component.PortConfig{}, spec.declareErr
				}
				return component.PortConfig{Inputs: spec.inputs, Outputs: spec.outputs}, nil
			},
		})
		if err != nil {
			t.Fatalf("register fake %s: %v", spec.name, err)
		}
	}
	return registry
}

func instance(factory string, typ types.ComponentType) types.ComponentConfig {
	return types.ComponentConfig{Name: factory, Type: typ, Enabled: true, Config: json.RawMessage(`{}`)}
}

func compositionOf(components map[string]types.ComponentConfig) *config.Config {
	return &config.Config{
		Version:    "1.0.0",
		Platform:   config.PlatformConfig{Org: "test", ID: "composition", Environment: "test"},
		Components: components,
	}
}

// validateRoundTrip runs Validate and decodes the JSON it marshals to into a
// fresh Result, so every assertion sees the wire shape, not the Go value.
func validateRoundTrip(t *testing.T, registry *component.Registry, cfg *config.Config) composition.Result {
	t.Helper()
	result, err := composition.Validate(registry, cfg)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded composition.Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return decoded
}

func findingsOfType(findings []composition.Finding, typ string) []composition.Finding {
	var out []composition.Finding
	for _, finding := range findings {
		if finding.Type == typ {
			out = append(out, finding)
		}
	}
	return out
}

func typesOf(findings []composition.Finding) []string {
	seen := map[string]struct{}{}
	for _, finding := range findings {
		seen[finding.Type] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for typ := range seen {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

func natsOut(name, subject string, iface *component.InterfaceContract) component.PortDefinition {
	return component.PortDefinition{Name: name, Required: true, Config: component.NATSPort{Subject: subject, Interface: iface}}
}

func natsIn(name, subject string, required bool, iface *component.InterfaceContract) component.PortDefinition {
	return component.PortDefinition{Name: name, Required: required, Config: component.NATSPort{Subject: subject, Interface: iface}}
}

func jetStreamIn(name, stream, subject string, required bool) component.PortDefinition {
	return component.PortDefinition{Name: name, Required: required, Config: component.JetStreamPort{StreamName: stream, Subjects: []string{subject}}}
}

func jetStreamOut(name, stream, subject string) component.PortDefinition {
	return component.PortDefinition{Name: name, Required: true, Config: component.JetStreamPort{StreamName: stream, Subjects: []string{subject}}}
}
