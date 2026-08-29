package component

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPortKindClosedSet(t *testing.T) {
	want := []PortKind{
		PortKindTimer,
		PortKindNetwork,
		PortKindFile,
		PortKindHTTPClient,
		PortKindNATS,
		PortKindNATSRequest,
		PortKindJetStream,
		PortKindKVWatch,
		PortKindKVRead,
		PortKindKVWrite,
		PortKindStoreRead,
		PortKindStoreProvide,
	}
	if got := portKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("portKinds() = %v, want %v", got, want)
	}
}

func TestPortCodecOuterErrorsRetainPortAndKindContext(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		target any
		field  string
	}{
		{
			name:   "definition",
			raw:    `{"name":"named","extra":true,"config":{"kind":"nats","subject":"a"}}`,
			target: &PortDefinition{},
			field:  "definition",
		},
		{
			name:   "port",
			raw:    `{"name":"named","direction":"input","extra":true,"config":{"kind":"nats","subject":"a"}}`,
			target: &Port{},
			field:  "port",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tt.raw), tt.target)
			if err == nil {
				t.Fatal("unmarshal succeeded")
			}
			for _, want := range []string{`port "named"`, `kind "nats"`, `field "` + tt.field + `"`} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want %q", err, want)
				}
			}
		})
	}
}

func TestPortDefinitionAndPortUseOneStrictWire(t *testing.T) {
	iface := &InterfaceContract{Type: "example.payload", Version: "v2", Compatible: []string{"v1"}}
	outputJetStream := completeJetStreamPort(iface)
	outputJetStream.MaxAckPending = 0
	tests := []struct {
		name      string
		direction Direction
		config    Portable
	}{
		{"timer", DirectionInput, TimerPort{Interval: "30s", Interface: iface}},
		{"network-input", DirectionInput, NetworkPort{Protocol: "udp", Host: "127.0.0.1", Port: 14550}},
		{"network-output", DirectionOutput, NetworkPort{Protocol: "tcp", Host: "example.test", Port: 443}},
		{"file-input", DirectionInput, FilePort{Path: "/tmp/in", Pattern: "*.json"}},
		{"file-output", DirectionOutput, FilePort{Path: "/tmp/out", Pattern: "*.json"}},
		{"http-client", DirectionInput, HTTPClientPort{Method: "POST", URLPattern: "https://example.test/*", TriggerPort: "poll", AuthRef: "token", ContactPolicy: "ops@example.test", Interface: iface}},
		{"nats-input", DirectionInput, NATSPort{Subject: "events.in", Queue: "workers", Interface: iface}},
		{"nats-output", DirectionOutput, NATSPort{Subject: "events.out", Interface: iface}},
		{"nats-request-input", DirectionInput, NATSRequestPort{Subject: "request.in", Timeout: "2s", Retries: 3, Interface: iface}},
		{"nats-request-output", DirectionOutput, NATSRequestPort{Subject: "request.out", Timeout: "3s", Retries: 2, Interface: iface}},
		{"jetstream-input", DirectionInput, completeJetStreamPort(iface)},
		{"jetstream-output", DirectionOutput, outputJetStream},
		{"kv-watch", DirectionInput, KVWatchPort{Bucket: "WATCH", Keys: []string{"a", "b.*"}, History: true, Interface: iface}},
		{"kv-read", DirectionInput, KVReadPort{Bucket: "READ", Interface: iface}},
		{"kv-write", DirectionOutput, KVWritePort{Bucket: "WRITE", Interface: iface}},
		{"store-read", DirectionInput, StoreReadPort{Bucket: "OBJECTS", Interface: iface}},
		{"store-provide", DirectionOutput, StoreProvidePort{Instance: "primary"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := PortDefinition{Name: "port", Required: true, Description: "test", Config: tt.config}
			definitionJSON, err := json.Marshal(definition)
			if err != nil {
				t.Fatalf("marshal definition: %v", err)
			}
			assertCommonConfigWire(t, definitionJSON, tt.config.Kind())

			var decodedDefinition PortDefinition
			if err := json.Unmarshal(definitionJSON, &decodedDefinition); err != nil {
				t.Fatalf("unmarshal definition: %v", err)
			}
			if !reflect.DeepEqual(decodedDefinition, definition) {
				t.Fatalf("definition round trip:\n got %#v\nwant %#v", decodedDefinition, definition)
			}

			port := Port{Name: "port", Direction: tt.direction, Required: true, Description: "test", Config: tt.config}
			portJSON, err := json.Marshal(port)
			if err != nil {
				t.Fatalf("marshal port: %v", err)
			}
			assertCommonConfigWire(t, portJSON, tt.config.Kind())

			var decodedPort Port
			if err := json.Unmarshal(portJSON, &decodedPort); err != nil {
				t.Fatalf("unmarshal port: %v", err)
			}
			if !reflect.DeepEqual(decodedPort, port) {
				t.Fatalf("port round trip:\n got %#v\nwant %#v", decodedPort, port)
			}
		})
	}
}

func TestPortCodecRejectsUnknownAndLegacyShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"unknown outer field", `{"name":"p","extra":true,"config":{"kind":"nats","subject":"a"}}`},
		{"unknown config field", `{"name":"p","config":{"kind":"nats","subject":"a","extra":true}}`},
		{"unknown kind", `{"name":"p","config":{"kind":"future","subject":"a"}}`},
		// port-grammar:legacy-fixture exercises the strict rejection path.
		{"legacy top-level type", `{"name":"p","type":"nats","config":{"kind":"nats","subject":"a"}}`},
		{"legacy runtime wrapper", `{"name":"p","config":{"type":"nats","data":{"subject":"a"}}}`},
		// port-grammar:legacy-fixture exercises strict alias rejection.
		{"alias kv", `{"name":"p","config":{"kind":"kv","bucket":"A"}}`},
		// port-grammar:legacy-fixture exercises strict alias rejection.
		{"alias kvwatch", `{"name":"p","config":{"kind":"kvwatch","bucket":"A"}}`},
		// port-grammar:legacy-fixture exercises strict alias rejection.
		{"alias kvwrite", `{"name":"p","config":{"kind":"kvwrite","bucket":"A"}}`},
		// port-grammar:legacy-fixture exercises strict alias rejection.
		{"alias http", `{"name":"p","config":{"kind":"http","protocol":"http","port":80}}`},
		// port-grammar:legacy-fixture exercises strict alias rejection.
		{"alias grpc", `{"name":"p","config":{"kind":"grpc","protocol":"grpc","port":80}}`},
		// port-grammar:legacy-fixture exercises strict alias rejection.
		{"alias websocket", `{"name":"p","config":{"kind":"websocket-server","protocol":"websocket","port":80}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var definition PortDefinition
			if err := json.Unmarshal([]byte(tt.raw), &definition); err == nil {
				t.Fatal("unmarshal succeeded, want strict rejection")
			}
		})
	}
}

func TestPortConfigJSONRejectsDuplicateNamesWithinEachLane(t *testing.T) {
	tests := []string{
		`{"inputs":[{"name":"same","config":{"kind":"nats","subject":"a"}},{"name":"same","config":{"kind":"nats","subject":"b"}}]}`,
		`{"outputs":[{"name":"same","config":{"kind":"nats","subject":"a"}},{"name":"same","config":{"kind":"nats","subject":"b"}}]}`,
	}
	for _, raw := range tests {
		var config PortConfig
		if err := json.Unmarshal([]byte(raw), &config); err == nil {
			t.Fatalf("PortConfig accepted duplicate name: %s", raw)
		}
	}
}

func TestPortConfigJSONResolvesJetStreamDefinitionsByLane(t *testing.T) {
	t.Run("subject-only input fails with typed stream identity context", func(t *testing.T) {
		config := PortConfig{Outputs: []PortDefinition{{Name: "sentinel", Config: NATSPort{Subject: "sentinel"}}}}
		raw := []byte(`{"inputs":[{"name":"events","config":{"kind":"jetstream","subjects":["events.>"]}}]}`)

		err := json.Unmarshal(raw, &config)
		if err == nil {
			t.Fatal("PortConfig accepted a subject-only JetStream input")
		}
		for _, context := range []string{`port "events"`, `kind "jetstream"`, `field "stream_name"`} {
			if !strings.Contains(err.Error(), context) {
				t.Fatalf("PortConfig error %q missing %q", err, context)
			}
		}
		if len(config.Outputs) != 1 || config.Outputs[0].Name != "sentinel" || len(config.Inputs) != 0 {
			t.Fatalf("failed decode partially replaced receiver: %#v", config)
		}
	})

	t.Run("subject-only output remains valid", func(t *testing.T) {
		raw := []byte(`{"outputs":[{"name":"events","config":{"kind":"jetstream","subjects":["events.>"]}}]}`)
		var config PortConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatalf("PortConfig rejected subject-only JetStream output: %v", err)
		}
		if len(config.Outputs) != 1 {
			t.Fatalf("outputs = %#v", config.Outputs)
		}
		stream, ok := config.Outputs[0].Config.(JetStreamPort)
		if !ok || stream.StreamName != "" || !reflect.DeepEqual(stream.Subjects, []string{"events.>"}) {
			t.Fatalf("output config = %#v", config.Outputs[0].Config)
		}
	})
}

func completeJetStreamPort(iface *InterfaceContract) JetStreamPort {
	return JetStreamPort{
		StreamName:        "EVENTS",
		Subjects:          []string{"events.>", "audit.>"},
		Storage:           "file",
		RetentionPolicy:   "interest",
		RetentionDays:     9,
		MaxSizeGB:         12,
		Replicas:          3,
		ConsumerName:      "consumer",
		DeliverPolicy:     "all",
		AckPolicy:         "explicit",
		MaxDeliver:        7,
		AckWait:           "2m",
		HeartbeatInterval: "30s",
		MaxAckPending:     4321,
		Interface:         iface,
	}
}

func assertCommonConfigWire(t *testing.T, raw []byte, kind PortKind) {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if _, ok := envelope["type"]; ok {
		t.Fatal("wire contains retired top-level type")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(envelope["config"], &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	var gotKind PortKind
	if err := json.Unmarshal(config["kind"], &gotKind); err != nil {
		t.Fatalf("decode kind: %v", err)
	}
	if gotKind != kind {
		t.Fatalf("kind = %q, want %q", gotKind, kind)
	}
	if _, ok := config["data"]; ok {
		t.Fatal("wire contains retired data wrapper")
	}
}

// TestJetStreamImportLaneDecodesFromOperatorJSON pins the import lane across
// the seam an operator actually uses: JSON. The flag is the ONE knob deciding
// whether graph-ingest admits a foreign org.platform (ADR-102 d5), so the path
// from `"import": true` in a config to StreamFacts.Import() is contract, not
// convenience — and a Go struct literal in a component test does not exercise
// the decoder.
//
// It is asserted here rather than by shipping an enabled import lane in a
// reference config: a lane is an operator statement of TRUST, and the shipped
// default must import nothing (review MEDIUM-4).
func TestJetStreamImportLaneDecodesFromOperatorJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "declared true",
			raw: `{"name":"peer_import","config":{"kind":"jetstream","stream_name":"PEER_ENTITY",
			       "subjects":["peer.entity.>"],"deliver_policy":"all","import":true}}`,
			want: true,
		},
		{
			// The fail-closed default: a port that says nothing imports nothing.
			name: "omitted",
			raw: `{"name":"entity_stream","config":{"kind":"jetstream","stream_name":"ENTITY",
			       "subjects":["entity.>"],"deliver_policy":"all"}}`,
			want: false,
		},
		{
			name: "declared false",
			raw: `{"name":"entity_stream","config":{"kind":"jetstream","stream_name":"ENTITY",
			       "subjects":["entity.>"],"import":false}}`,
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var definition PortDefinition
			if err := json.Unmarshal([]byte(tc.raw), &definition); err != nil {
				t.Fatalf("decode operator port JSON: %v", err)
			}
			port, err := definition.Resolve(DirectionInput)
			if err != nil {
				t.Fatalf("resolve input port: %v", err)
			}
			facts, err := port.Facts()
			if err != nil {
				t.Fatalf("port facts: %v", err)
			}
			stream, ok := facts.Stream()
			if !ok {
				t.Fatal("a jetstream port must project StreamFacts")
			}
			if got := stream.Import(); got != tc.want {
				t.Errorf("StreamFacts.Import() = %v, want %v — the operator's declaration did not survive decode", got, tc.want)
			}
		})
	}
}

// TestJetStreamImportLaneIsRefusedOnAnOutputPort pins the direction scoping: a
// lane is an INPUT-side statement, and declaring it on an output is an
// authoring mistake that must be loud rather than ignored.
func TestJetStreamImportLaneIsRefusedOnAnOutputPort(t *testing.T) {
	var definition PortDefinition
	raw := `{"name":"bad","config":{"kind":"jetstream","stream_name":"ENTITY",
	         "subjects":["entity.>"],"import":true}}`
	if err := json.Unmarshal([]byte(raw), &definition); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := definition.Resolve(DirectionOutput); err == nil {
		t.Fatal("declaring import on an output port must be refused, not silently ignored")
	}
}
