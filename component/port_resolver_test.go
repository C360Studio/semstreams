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
			port, err := (PortDefinition{Name: "p", Config: tt.config}).Resolve(tt.direction)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			facts, err := port.Facts()
			if err != nil {
				t.Fatalf("Facts: %v", err)
			}
			if facts.Kind() != tt.config.Kind() || facts.ResourceID() != tt.resourceID || facts.IsExclusive() != tt.exclusive {
				t.Fatalf("facts = %#v", facts)
			}
			if port.Direction != tt.direction || port.Config.Kind() != tt.config.Kind() {
				t.Fatalf("resolved port = %#v", port)
			}
		})
	}
}

func TestResolvePortNormalizesOnlyApprovedDefaults(t *testing.T) {
	network, err := (PortDefinition{Name: "network", Config: NetworkPort{Protocol: "udp", Port: 14550}}).Resolve(DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	if got := network.Config.(NetworkPort).Host; got != "0.0.0.0" {
		t.Fatalf("network host = %q", got)
	}

	request, err := (PortDefinition{Name: "request", Config: NATSRequestPort{Subject: "request"}}).Resolve(DirectionOutput)
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
		{"jetstream ack duration", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, AckWait: "soon"}}, DirectionInput, "ack_wait"},
		{"jetstream input missing stream name", PortDefinition{Name: "p", Config: JetStreamPort{Subjects: []string{"events.>"}}}, DirectionInput, "stream_name"},
		{"jetstream input missing subjects", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "EVENTS"}}, DirectionInput, "subjects"},
		{"jetstream output missing subjects", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "EVENTS"}}, DirectionOutput, "subjects"},
		{"jetstream empty subject", PortDefinition{Name: "p", Config: JetStreamPort{Subjects: []string{"events", " "}}}, DirectionInput, "subjects[1]"},
		{"jetstream storage", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, Storage: "disk"}}, DirectionInput, "storage"},
		{"jetstream retention", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, RetentionPolicy: "forever"}}, DirectionInput, "retention"},
		{"jetstream deliver policy", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, DeliverPolicy: "nwe"}}, DirectionInput, "deliver_policy"},
		{"jetstream ack policy", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, AckPolicy: "sometimes"}}, DirectionInput, "ack_policy"},
		{"jetstream retention days", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, RetentionDays: -1}}, DirectionInput, "retention_days"},
		{"jetstream max size", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, MaxSizeGB: -1}}, DirectionInput, "max_size_gb"},
		{"jetstream replicas", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, Replicas: -1}}, DirectionInput, "replicas"},
		{"jetstream max deliver", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, MaxDeliver: -1}}, DirectionInput, "max_deliver"},
		{"jetstream max ack pending", PortDefinition{Name: "p", Config: JetStreamPort{StreamName: "S", Subjects: []string{"events.>"}, MaxAckPending: -2}}, DirectionInput, "max_ack_pending"},
		{"network port zero", PortDefinition{Name: "p", Config: NetworkPort{Protocol: "udp"}}, DirectionInput, "port"},
		{"network port too high", PortDefinition{Name: "p", Config: NetworkPort{Protocol: "udp", Port: 65536}}, DirectionInput, "port"},
		{"missing timer interval", PortDefinition{Name: "p", Config: TimerPort{}}, DirectionInput, "interval"},
		{"missing file path", PortDefinition{Name: "p", Config: FilePort{}}, DirectionInput, "path"},
		{"missing http method", PortDefinition{Name: "p", Config: HTTPClientPort{URLPattern: "https://example.test"}}, DirectionInput, "method"},
		{"missing http url", PortDefinition{Name: "p", Config: HTTPClientPort{Method: "GET"}}, DirectionInput, "url_pattern"},
		{"missing nats subject", PortDefinition{Name: "p", Config: NATSPort{}}, DirectionInput, "subject"},
		{"missing request subject", PortDefinition{Name: "p", Config: NATSRequestPort{}}, DirectionInput, "subject"},
		{"missing jetstream subjects", PortDefinition{Name: "p", Config: JetStreamPort{}}, DirectionInput, "subjects"},
		{"missing watch bucket", PortDefinition{Name: "p", Config: KVWatchPort{}}, DirectionInput, "bucket"},
		{"missing read bucket", PortDefinition{Name: "p", Config: KVReadPort{}}, DirectionInput, "bucket"},
		{"missing write bucket", PortDefinition{Name: "p", Config: KVWritePort{}}, DirectionOutput, "bucket"},
		{"missing store bucket", PortDefinition{Name: "p", Config: StoreReadPort{}}, DirectionInput, "bucket"},
		{"missing store instance", PortDefinition{Name: "p", Config: StoreProvidePort{}}, DirectionOutput, "instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.def.Resolve(tt.direction)
			if err == nil {
				t.Fatal("Resolve succeeded")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), `port "`) {
				t.Fatalf("error = %q, want port context and %q", err, tt.want)
			}
		})
	}
}

func TestResolveJetStreamOutputAllowsProvisionerOwnedName(t *testing.T) {
	port, err := (PortDefinition{
		Name: "events", Config: JetStreamPort{Subjects: []string{"events.>"}},
	}).Resolve(DirectionOutput)
	if err != nil {
		t.Fatalf("Resolve subject-only output: %v", err)
	}
	facts, err := port.Facts()
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := facts.Stream()
	if !ok || stream.Name() != "" || !sameStrings(stream.Subjects(), []string{"events.>"}) {
		t.Fatalf("subject-only output facts = %#v", facts)
	}
}

func TestResolveJetStreamMaxAckPendingDirectionConstraints(t *testing.T) {
	tests := []struct {
		name      string
		direction Direction
		value     int
		wantErr   bool
	}{
		{name: "input zero", direction: DirectionInput, value: 0},
		{name: "input positive", direction: DirectionInput, value: 17},
		{name: "input unlimited", direction: DirectionInput, value: -1},
		{name: "input below minimum", direction: DirectionInput, value: -2, wantErr: true},
		{name: "output zero remains omission", direction: DirectionOutput, value: 0},
		{name: "output positive", direction: DirectionOutput, value: 17, wantErr: true},
		{name: "output unlimited", direction: DirectionOutput, value: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := PortDefinition{Name: "events", Config: JetStreamPort{
				StreamName: "EVENTS", Subjects: []string{"events.>"}, MaxAckPending: tt.value,
			}}
			_, err := definition.Resolve(tt.direction)
			if tt.wantErr && err == nil {
				t.Fatal("Resolve succeeded")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "max_ack_pending") {
				t.Fatalf("error = %q, want max_ack_pending context", err)
			}
		})
	}
}

func TestFactsForPortPreservesStreamAndInterfaceFacts(t *testing.T) {
	iface := &InterfaceContract{Type: "example", Version: "v1"}
	port, err := (PortDefinition{Name: "stream", Config: completeJetStreamPort(iface)}).Resolve(DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := port.Facts()
	if err != nil {
		t.Fatal(err)
	}
	contract, ok := facts.Interface()
	if !ok || contract.Type != "example" {
		t.Fatalf("interface facts = %#v, %v", contract, ok)
	}
	stream, ok := facts.Stream()
	if len(facts.NATSSubjects()) != 2 || len(facts.ConnectionIDs()) != 3 || !ok {
		t.Fatalf("stream facts incomplete: %#v", facts)
	}
	if stream.MaxAckPending() != 4321 || stream.AckWait() != "2m" || stream.Replicas() != 3 {
		t.Fatalf("stream fields lost: %#v", stream)
	}
}
