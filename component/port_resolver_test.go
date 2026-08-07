package component

import (
	"strings"
	"testing"
)

func TestResolvePortCoversEveryKindAndDirection(t *testing.T) {
	tests := []struct {
		name       string
		direction  Direction
		config     Portable
		resourceID string
		exclusive  bool
	}{
		{"timer", DirectionInput, TimerPort{Interval: "1s"}, "timer:1s", false},
		{"network-input", DirectionInput, NetworkPort{Protocol: "udp", Port: 1001}, "udp:0.0.0.0:1001", true},
		{"network-output", DirectionOutput, NetworkPort{Protocol: "tcp", Host: "host", Port: 1002}, "tcp:host:1002", true},
		{"file-input", DirectionInput, FilePort{Path: "/in"}, "file:/in", false},
		{"file-output", DirectionOutput, FilePort{Path: "/out"}, "file:/out", false},
		{"http-client", DirectionInput, HTTPClientPort{Method: "GET", URLPattern: "https://example.test"}, "http-client:GET:https://example.test", false},
		{"nats-input", DirectionInput, NATSPort{Subject: "in"}, "nats:in", false},
		{"nats-output", DirectionOutput, NATSPort{Subject: "out"}, "nats:out", false},
		{"request-input", DirectionInput, NATSRequestPort{Subject: "in"}, "nats-request:in", false},
		{"request-output", DirectionOutput, NATSRequestPort{Subject: "out"}, "nats-request:out", false},
		{"jetstream-input", DirectionInput, JetStreamPort{StreamName: "IN", Subjects: []string{"in.>"}}, "jetstream:IN", false},
		{"jetstream-output", DirectionOutput, JetStreamPort{Subjects: []string{"out.>"}}, "jetstream:out.>", false},
		{"kv-watch", DirectionInput, KVWatchPort{Bucket: "WATCH"}, "kv:WATCH", false},
		{"kv-read", DirectionInput, KVReadPort{Bucket: "READ"}, "kv:READ", false},
		{"kv-write", DirectionOutput, KVWritePort{Bucket: "WRITE"}, "kv:WRITE", false},
		{"store-read", DirectionInput, StoreReadPort{Bucket: "STORE"}, "store-read:STORE", false},
		{"store-provide", DirectionOutput, StoreProvidePort{Instance: "provider"}, "store-provide:provider", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := resolvePort(PortDefinition{Name: "p", Config: tt.config}, tt.direction)
			if err != nil {
				t.Fatalf("resolvePort: %v", err)
			}
			facts, err := factsForPort(port)
			if err != nil {
				t.Fatalf("factsForPort: %v", err)
			}
			if facts.kind != tt.config.Kind() || facts.resourceID != tt.resourceID || facts.exclusive != tt.exclusive {
				t.Fatalf("facts = %#v", facts)
			}
			if port.Direction != tt.direction || port.Config.Kind() != tt.config.Kind() {
				t.Fatalf("resolved port = %#v", port)
			}
		})
	}
}

func TestResolvePortNormalizesOnlyApprovedDefaults(t *testing.T) {
	network, err := resolvePort(PortDefinition{Name: "network", Config: NetworkPort{Protocol: "udp", Port: 14550}}, DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	if got := network.Config.(NetworkPort).Host; got != "0.0.0.0" {
		t.Fatalf("network host = %q", got)
	}

	request, err := resolvePort(PortDefinition{Name: "request", Config: NATSRequestPort{Subject: "request"}}, DirectionOutput)
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Config.(NATSRequestPort).Timeout; got != "1s" {
		t.Fatalf("request timeout = %q", got)
	}
}

func TestResolvePortRejectsInvalidDeclarations(t *testing.T) {
	tests := []struct {
		name      string
		def       PortDefinition
		direction Direction
		want      string
	}{
		{"missing name", PortDefinition{Config: NATSPort{Subject: "a"}}, DirectionInput, "name"},
		{"unknown direction", PortDefinition{Name: "p", Config: NATSPort{Subject: "a"}}, Direction("sideways"), "direction"},
		{"wrong direction", PortDefinition{Name: "p", Config: KVWritePort{Bucket: "B"}}, DirectionInput, "direction"},
		{"timer duration", PortDefinition{Name: "p", Config: TimerPort{Interval: "soon"}}, DirectionInput, "interval"},
		{"request duration", PortDefinition{Name: "p", Config: NATSRequestPort{Subject: "a", Timeout: "soon"}}, DirectionInput, "timeout"},
		{"jetstream ack duration", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", AckWait: "soon"}}, DirectionInput, "ack_wait"},
		{"network port zero", PortDefinition{Name: "p", Config: NetworkPort{Protocol: "udp"}}, DirectionInput, "port"},
		{"network port too high", PortDefinition{Name: "p", Config: NetworkPort{Protocol: "udp", Port: 65536}}, DirectionInput, "port"},
		{"missing timer interval", PortDefinition{Name: "p", Config: TimerPort{}}, DirectionInput, "interval"},
		{"missing file path", PortDefinition{Name: "p", Config: FilePort{}}, DirectionInput, "path"},
		{"missing http method", PortDefinition{Name: "p", Config: HTTPClientPort{URLPattern: "https://example.test"}}, DirectionInput, "method"},
		{"missing http url", PortDefinition{Name: "p", Config: HTTPClientPort{Method: "GET"}}, DirectionInput, "url_pattern"},
		{"missing nats subject", PortDefinition{Name: "p", Config: NATSPort{}}, DirectionInput, "subject"},
		{"missing request subject", PortDefinition{Name: "p", Config: NATSRequestPort{}}, DirectionInput, "subject"},
		{"missing jetstream identity", PortDefinition{Name: "p", Config: JetStreamPort{}}, DirectionInput, "stream_name"},
		{"missing watch bucket", PortDefinition{Name: "p", Config: KVWatchPort{}}, DirectionInput, "bucket"},
		{"missing read bucket", PortDefinition{Name: "p", Config: KVReadPort{}}, DirectionInput, "bucket"},
		{"missing write bucket", PortDefinition{Name: "p", Config: KVWritePort{}}, DirectionOutput, "bucket"},
		{"missing store bucket", PortDefinition{Name: "p", Config: StoreReadPort{}}, DirectionInput, "bucket"},
		{"missing store instance", PortDefinition{Name: "p", Config: StoreProvidePort{}}, DirectionOutput, "instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolvePort(tt.def, tt.direction)
			if err == nil {
				t.Fatal("resolvePort succeeded")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), `port "`) {
				t.Fatalf("error = %q, want port context and %q", err, tt.want)
			}
		})
	}
}

func TestFactsForPortPreservesStreamAndInterfaceFacts(t *testing.T) {
	iface := &InterfaceContract{Type: "example", Version: "v1"}
	port, err := resolvePort(PortDefinition{Name: "stream", Config: completeJetStreamPort(iface)}, DirectionOutput)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := factsForPort(port)
	if err != nil {
		t.Fatal(err)
	}
	if facts.interfaceContract == iface {
		t.Fatal("facts retained caller-owned interface pointer")
	}
	if facts.interfaceContract == nil || facts.interfaceContract.Type != "example" {
		t.Fatalf("interface facts = %#v", facts.interfaceContract)
	}
	if len(facts.natsSubjects) != 2 || len(facts.connectionIDs) != 3 || facts.stream == nil {
		t.Fatalf("stream facts incomplete: %#v", facts)
	}
	if facts.stream.maxAckPending != 4321 || facts.stream.ackWait != "2m" || facts.stream.replicas != 3 {
		t.Fatalf("stream fields lost: %#v", facts.stream)
	}
}
