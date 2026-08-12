package objectstore

import (
	"encoding/json"
	"strings"
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

func TestObjectStoreInputBindingPlanPreservesUniqueLegacyConsumerName(t *testing.T) {
	t.Parallel()

	plans := plannedObjectStoreInputs(t, []component.PortDefinition{{
		Name: "store_in", Config: component.JetStreamPort{
			StreamName: "MAPPED", Subjects: []string{"mapped.messages"},
		},
	}})
	if got, want := plans[0].consumerName, "objectstore-objectstore-mapped-messages"; got != want {
		t.Fatalf("consumer name = %q, want legacy name %q", got, want)
	}
}

func TestObjectStoreInputBindingPlanDisambiguatesLegacyCollisionsStably(t *testing.T) {
	t.Parallel()

	forward := []component.PortDefinition{
		{Name: "dot", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo.bar"}}},
		{Name: "dash", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo-bar"}}},
		{Name: "wild", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo.*"}}},
		{Name: "word", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo.all"}}},
	}
	reverse := []component.PortDefinition{
		{Name: "renamed_word", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo.all"}}},
		{Name: "renamed_wild", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo.*"}}},
		{Name: "renamed_dash", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo-bar"}}},
		{Name: "renamed_dot", Config: component.JetStreamPort{StreamName: "COLLIDE", Subjects: []string{"foo.bar"}}},
	}

	gotForward := consumerNamesBySubject(plannedObjectStoreInputs(t, forward))
	gotReverse := consumerNamesBySubject(plannedObjectStoreInputs(t, reverse))
	want := map[string]string{
		"foo.bar": "objectstore-h-bc749a19c08acd9ba792effb9dd8c7ae0304ee7ac10090326f644f01276c7da8",
		"foo-bar": "objectstore-h-b144089414ccbd79134bee9a337a99ef4dbb992076176b4002615114b42924df",
		"foo.*":   "objectstore-h-2d23635824be8dd4eac9fb12e81f4e01741477990b7343acb72ba87200cf9d24",
		"foo.all": "objectstore-h-cae04769e97a0c8a9db2b2cd75e3846ca95f50f35343fb319ee90c171cfc4127",
	}
	for subject, wantName := range want {
		if gotForward[subject] != wantName {
			t.Errorf("forward consumer for %q = %q, want %q", subject, gotForward[subject], wantName)
		}
		if gotReverse[subject] != wantName {
			t.Errorf("reverse consumer for %q = %q, want %q", subject, gotReverse[subject], wantName)
		}
		if len(wantName) > 255 || validateObjectStoreConsumerName(wantName) != nil {
			t.Errorf("planned collision consumer %q is not a legal <=255-byte NATS name", wantName)
		}
	}
}

func TestObjectStoreInputBindingPlanRejectsExactDuplicatesDeterministically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		definitions []component.PortDefinition
		want        string
	}{
		{
			name: "core NATS",
			definitions: []component.PortDefinition{
				{Name: "zeta", Config: component.NATSPort{Subject: "duplicate.core"}},
				{Name: "alpha", Config: component.NATSPort{Subject: "duplicate.core"}},
			},
			want: `duplicate ObjectStore NATS binding subject="duplicate.core" declared by ports ["alpha" "zeta"]`,
		},
		{
			name: "JetStream",
			definitions: []component.PortDefinition{
				{Name: "zeta", Config: component.JetStreamPort{StreamName: "DUP", Subjects: []string{"duplicate.js"}}},
				{Name: "alpha", Config: component.JetStreamPort{StreamName: "DUP", Subjects: []string{"duplicate.js"}}},
			},
			want: `duplicate ObjectStore JetStream binding stream="DUP" subject="duplicate.js" declared by ports ["alpha" "zeta"]`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configJSON, err := json.Marshal(Config{Ports: &component.PortConfig{Inputs: test.definitions}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewComponent(configJSON, component.Dependencies{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewComponent error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestObjectStoreInputBindingPlanRejectsOverlengthLegacyName(t *testing.T) {
	t.Parallel()

	subject := strings.Repeat("a", 240)
	configJSON, err := json.Marshal(Config{Ports: &component.PortConfig{Inputs: []component.PortDefinition{{
		Name: "long", Config: component.JetStreamPort{StreamName: "LONG", Subjects: []string{subject}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewComponent(configJSON, component.Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "maximum is 255") {
		t.Fatalf("NewComponent error = %v, want planned consumer length rejection", err)
	}
}

func plannedObjectStoreInputs(t *testing.T, inputs []component.PortDefinition) []objectStoreInputBinding {
	t.Helper()
	configJSON, err := json.Marshal(Config{Ports: &component.PortConfig{Inputs: inputs}})
	if err != nil {
		t.Fatal(err)
	}
	discoverable, err := NewComponent(configJSON, component.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	return discoverable.(*Component).inputBindings
}

func consumerNamesBySubject(plans []objectStoreInputBinding) map[string]string {
	result := make(map[string]string, len(plans))
	for _, plan := range plans {
		result[plan.subject] = plan.consumerName
	}
	return result
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

	tests := []struct {
		name        string
		definitions []component.PortDefinition
	}{
		{
			name: "default write label",
			definitions: []component.PortDefinition{
				{Name: "write", Config: component.NATSPort{Subject: "storage.objectstore.write"}},
			},
		},
		{
			name: "renamed core NATS lane",
			definitions: []component.PortDefinition{
				{Name: "direct_ingest", Config: component.NATSPort{Subject: "storage.objectstore.direct"}},
			},
		},
		{
			name: "shipped store_in JetStream lane",
			definitions: []component.PortDefinition{
				{Name: "store_in", Config: component.JetStreamPort{
					StreamName: "MAPPED", Subjects: []string{"mapped.messages"},
				}},
			},
		},
		{
			name: "multiple ordinary lanes",
			definitions: []component.PortDefinition{
				{Name: "direct_ingest", Config: component.NATSPort{Subject: "storage.objectstore.direct"}},
				{Name: "archive_in", Config: component.NATSPort{Subject: "storage.objectstore.archive"}},
				{Name: "store_in", Config: component.JetStreamPort{
					StreamName: "MAPPED", Subjects: []string{"mapped.messages"},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configJSON, err := json.Marshal(Config{Ports: &component.PortConfig{
				Inputs: test.definitions,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewComponent(configJSON, component.Dependencies{}); err != nil {
				t.Fatalf("NewComponent rejected ordinary write inputs: %v", err)
			}
		})
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
