package component

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildPortFromDefinitionPreservesFlatNATSRequestInterface(t *testing.T) {
	port := BuildPortFromDefinition(PortDefinition{
		Name:      "mutations",
		Type:      "nats-request",
		Subject:   "graph.mutation.>",
		Interface: "semstreams.graph.mutation",
		Required:  true,
		Timeout:   "3s",
	}, DirectionOutput)

	request, ok := port.Config.(NATSRequestPort)
	if !ok {
		t.Fatalf("config type = %T, want NATSRequestPort", port.Config)
	}
	want := &InterfaceContract{Type: "semstreams.graph.mutation", Version: "v1"}
	if !reflect.DeepEqual(request.Interface, want) {
		t.Fatalf("interface = %#v, want %#v", request.Interface, want)
	}
	if request.Subject != "graph.mutation.>" || request.Timeout != "3s" || !port.Required {
		t.Fatalf("effective port lost metadata: %#v", port)
	}
}

func TestBuildPortFromDefinitionTypedNATSRequestInterfaceWins(t *testing.T) {
	typed := &InterfaceContract{Type: "semstreams.graph.mutation", Version: "v2"}
	port := BuildPortFromDefinition(PortDefinition{
		Name:      "mutations",
		Type:      "nats-request",
		Subject:   "ignored.family.>",
		Interface: "flat.interface",
		Timeout:   "1s",
		Config: NATSRequestPort{
			Subject:   "graph.mutation.>",
			Timeout:   "4s",
			Retries:   2,
			Interface: typed,
		},
	}, DirectionInput)

	request := port.Config.(NATSRequestPort)
	if request.Subject != "graph.mutation.>" || request.Timeout != "4s" || request.Retries != 2 {
		t.Fatalf("typed config did not win: %#v", request)
	}
	if !reflect.DeepEqual(request.Interface, typed) {
		t.Fatalf("interface = %#v, want %#v", request.Interface, typed)
	}
}

func TestNATSRequestInterfaceSurvivesDefinitionAndPortJSONRoundTrips(t *testing.T) {
	raw := []byte(`{
		"name":"mutations",
		"type":"nats-request",
		"subject":"graph.mutation.>",
		"required":true,
		"config":{
			"subject":"graph.mutation.>",
			"timeout":"2s",
			"interface":{"type":"semstreams.graph.mutation","version":"v1"}
		}
	}`)
	var definition PortDefinition
	if err := json.Unmarshal(raw, &definition); err != nil {
		t.Fatalf("definition Unmarshal: %v", err)
	}
	port := BuildPortFromDefinition(definition, DirectionOutput)

	encoded, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("port Marshal: %v", err)
	}
	var roundTrip Port
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("port Unmarshal: %v", err)
	}
	request, ok := roundTrip.Config.(NATSRequestPort)
	if !ok {
		t.Fatalf("round-trip config type = %T", roundTrip.Config)
	}
	want := &InterfaceContract{Type: "semstreams.graph.mutation", Version: "v1"}
	if !reflect.DeepEqual(request.Interface, want) {
		t.Fatalf("round-trip interface = %#v, want %#v", request.Interface, want)
	}
}
