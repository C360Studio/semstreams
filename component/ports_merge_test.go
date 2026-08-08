package component

import (
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
