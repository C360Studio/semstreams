package component

import "testing"

func TestPortsToCapabilitiesUsesResolvedFacts(t *testing.T) {
	t.Parallel()

	ports := []Port{
		{
			Name:      "events",
			Direction: DirectionInput,
			Config: NATSPort{
				Subject:   "events.>",
				Interface: &InterfaceContract{Type: "example.events", Version: "v1"},
			},
		},
		{
			Name:      "requests",
			Direction: DirectionOutput,
			Config:    NATSRequestPort{Subject: "request.run"},
		},
		{
			Name:      "durable",
			Direction: DirectionInput,
			Config:    JetStreamPort{StreamName: "EVENTS", Subjects: []string{"events.created", "events.updated"}},
		},
	}

	got, err := NewRegistry().portsToCapabilities(ports)
	if err != nil {
		t.Fatalf("portsToCapabilities() error = %v", err)
	}
	want := []PortCapability{
		{Name: "events", Subject: "events.>", Type: "stream", Interface: "example.events"},
		{Name: "requests", Subject: "request.run", Type: "request"},
		{Name: "durable", Subject: "events.created", Type: "stream"},
	}
	if len(got) != len(want) {
		t.Fatalf("len(portsToCapabilities()) = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("portsToCapabilities()[%d] = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestPortsToCapabilitiesRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry().portsToCapabilities([]Port{{
		Name:      "broken",
		Direction: DirectionInput,
		Config:    NATSPort{},
	}})
	if err == nil {
		t.Fatal("portsToCapabilities() error = nil, want invalid port error")
	}
}
