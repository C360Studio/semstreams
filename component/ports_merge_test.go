package component

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMergePortConfigCompleteReplacementStableOrderAndClone(t *testing.T) {
	defaults := PortConfig{
		Inputs: []PortDefinition{
			{Name: "first", Description: "default", Config: NATSPort{Subject: "first.default"}},
			{Name: "second", Required: true, Config: NATSRequestPort{Subject: "second.default", Timeout: "2s"}},
		},
		Outputs: []PortDefinition{
			{Name: "out", Config: JetStreamPort{StreamName: "OUT", Subjects: []string{"out.>"}}},
		},
	}
	overrides := PortConfig{
		Inputs: []PortDefinition{
			{Name: "second", Description: "replacement", Config: NATSRequestPort{Subject: "second.override", Timeout: "3s"}},
		},
	}

	got, err := MergePortConfig(defaults, overrides)
	if err != nil {
		t.Fatalf("MergePortConfig: %v", err)
	}
	want := PortConfig{
		Inputs: []PortDefinition{
			{Name: "first", Description: "default", Config: NATSPort{Subject: "first.default"}},
			{Name: "second", Description: "replacement", Config: NATSRequestPort{Subject: "second.override", Timeout: "3s"}},
		},
		Outputs: []PortDefinition{
			{Name: "out", Config: JetStreamPort{StreamName: "OUT", Subjects: []string{"out.>"}}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge:\n got %#v\nwant %#v", got, want)
	}

	defaults.Outputs[0].Config.(JetStreamPort).Subjects[0] = "mutated.default"
	overrides.Inputs[0].Config = NATSRequestPort{Subject: "mutated.override", Timeout: "4s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("result aliases caller-owned input")
	}
	got.Outputs[0].Config.(JetStreamPort).Subjects[0] = "mutated.result"
	if defaults.Outputs[0].Config.(JetStreamPort).Subjects[0] == "mutated.result" {
		t.Fatal("caller-visible result aliases defaults")
	}
}

func TestMergePortConfigRejectsInvalidOverrides(t *testing.T) {
	nats := func(name, subject string) PortDefinition {
		return PortDefinition{Name: name, Config: NATSPort{Subject: subject}}
	}
	tests := []struct {
		name      string
		defaults  PortConfig
		overrides PortConfig
		want      string
	}{
		{"duplicate defaults", PortConfig{Inputs: []PortDefinition{nats("p", "a"), nats("p", "b")}}, PortConfig{}, "duplicate"},
		{"duplicate overrides", PortConfig{Inputs: []PortDefinition{nats("p", "a")}}, PortConfig{Inputs: []PortDefinition{nats("p", "b"), nats("p", "c")}}, "duplicate"},
		{"unknown override", PortConfig{Inputs: []PortDefinition{nats("p", "a")}}, PortConfig{Inputs: []PortDefinition{nats("other", "b")}}, "unknown"},
		{"kind change", PortConfig{Inputs: []PortDefinition{nats("p", "a")}}, PortConfig{Inputs: []PortDefinition{{Name: "p", Config: NATSRequestPort{Subject: "a"}}}}, "kind"},
		{"direction move", PortConfig{Inputs: []PortDefinition{nats("p", "a")}}, PortConfig{Outputs: []PortDefinition{nats("p", "a")}}, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MergePortConfig(tt.defaults, tt.overrides)
			if err == nil {
				t.Fatal("MergePortConfig succeeded")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
		})
	}
}

// TestPortDefinitionExternalRoundTrip — the external marker survives the
// strict envelope codec, resolution, the runtime Port view, and MergePortConfig.
func TestPortDefinitionExternalRoundTrip(t *testing.T) {
	var definition PortDefinition
	if err := json.Unmarshal([]byte(`{"name":"in","required":true,"external":true,"config":{"kind":"jetstream","stream_name":"USER","subjects":["user.message.>"]}}`), &definition); err != nil {
		t.Fatal(err)
	}
	if !definition.External {
		t.Fatal("decoded definition lost external")
	}
	port, err := definition.Resolve(DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	if !port.External {
		t.Fatal("resolved port lost external")
	}
	encoded, err := json.Marshal(port)
	if err != nil {
		t.Fatal(err)
	}
	var runtime Port
	if err := json.Unmarshal(encoded, &runtime); err != nil {
		t.Fatal(err)
	}
	if !runtime.External || runtime.Name != "in" || !runtime.Required {
		t.Fatalf("runtime view = %+v, want external required input", runtime)
	}
	merged, err := MergePortConfig(PortConfig{Inputs: []PortDefinition{definition}}, PortConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Inputs[0].External {
		t.Fatal("MergePortConfig dropped external")
	}
	if err := json.Unmarshal([]byte(`{"name":"in","externl":true,"config":{"kind":"nats","subject":"x"}}`), &definition); err == nil {
		t.Fatal("strict envelope accepted an unknown field")
	}
	// external is an input statement: an output declaring it is refused, never
	// silently ignored.
	_, err = (PortDefinition{Name: "out", External: true, Config: NATSPort{Subject: "x"}}).Resolve(DirectionOutput)
	if err == nil || !strings.Contains(err.Error(), "external") {
		t.Fatalf("output port with external=true resolved without an error naming the field: %v", err)
	}
	if _, err := (PortDefinition{Name: "in", External: true, Config: NATSPort{Subject: "x"}}).Resolve(DirectionInput); err != nil {
		t.Fatalf("input port with external=true failed to resolve: %v", err)
	}
}
