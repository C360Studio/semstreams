package component

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDirection(t *testing.T) {
	tests := []struct {
		name      string
		direction Direction
		expected  string
	}{
		{"input direction", DirectionInput, "input"},
		{"output direction", DirectionOutput, "output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.direction) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.direction))
			}
		})
	}
}

func TestNetworkPort(t *testing.T) {
	tests := []struct {
		name        string
		port        NetworkPort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name:        "UDP port",
			port:        NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14550},
			resourceID:  "udp:0.0.0.0:14550",
			isExclusive: true,
			portType:    "network",
		},
		{
			name:        "TCP port",
			port:        NetworkPort{Protocol: "tcp", Host: "localhost", Port: 8080},
			resourceID:  "tcp:localhost:8080",
			isExclusive: true,
			portType:    "network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestNATSPort(t *testing.T) {
	tests := []struct {
		name        string
		port        NATSPort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name:        "NATS subject only",
			port:        NATSPort{Subject: "mavlink.position"},
			resourceID:  "nats:mavlink.position",
			isExclusive: false,
			portType:    "nats",
		},
		{
			name:        "NATS with queue",
			port:        NATSPort{Subject: "sensor.data", Queue: "workers"},
			resourceID:  "nats:sensor.data",
			isExclusive: false,
			portType:    "nats",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestFilePort(t *testing.T) {
	tests := []struct {
		name        string
		port        FilePort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name:        "File path only",
			port:        FilePort{Path: "/var/log/messages"},
			resourceID:  "file:/var/log/messages",
			isExclusive: false,
			portType:    "file",
		},
		{
			name:        "File with pattern",
			port:        FilePort{Path: "/data/logs", Pattern: "*.log"},
			resourceID:  "file:/data/logs",
			isExclusive: false,
			portType:    "file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestNATSRequestPort(t *testing.T) {
	tests := []struct {
		name        string
		port        NATSRequestPort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name:        "Request/Response with timeout",
			port:        NATSRequestPort{Subject: "storage.api", Timeout: "1s"},
			resourceID:  "nats-request:storage.api",
			isExclusive: false,
			portType:    "nats-request",
		},
		{
			name:        "Request/Response with retries",
			port:        NATSRequestPort{Subject: "entity.mutate", Timeout: "2s", Retries: 3},
			resourceID:  "nats-request:entity.mutate",
			isExclusive: false,
			portType:    "nats-request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestJetStreamPort(t *testing.T) {
	tests := []struct {
		name        string
		port        JetStreamPort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name: "JetStream output with stream",
			port: JetStreamPort{
				StreamName:    "ENTITY_EVENTS",
				Subjects:      []string{"events.graph.entity.>"},
				Storage:       "file",
				RetentionDays: 7,
				MaxSizeGB:     10,
				Replicas:      1,
			},
			resourceID:  "jetstream:ENTITY_EVENTS",
			isExclusive: false,
			portType:    "jetstream",
		},
		{
			name: "JetStream consumer",
			port: JetStreamPort{
				Subjects:      []string{"events.>"},
				ConsumerName:  "my-consumer",
				DeliverPolicy: "new",
				AckPolicy:     "explicit",
				MaxDeliver:    3,
			},
			resourceID:  "jetstream:events.>",
			isExclusive: false,
			portType:    "jetstream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestKVWatchPort(t *testing.T) {
	tests := []struct {
		name        string
		port        KVWatchPort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name: "KV Watch all keys",
			port: KVWatchPort{
				Bucket: "ENTITY_STATES",
			},
			resourceID:  "kvwatch:ENTITY_STATES",
			isExclusive: false,
			portType:    "kvwatch",
		},
		{
			name: "KV Watch specific keys with history",
			port: KVWatchPort{
				Bucket:  "CONFIG",
				Keys:    []string{"services.*", "components.*"},
				History: true,
			},
			resourceID:  "kvwatch:CONFIG",
			isExclusive: false,
			portType:    "kvwatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestPortableInterface(_ *testing.T) {
	// Test that all types implement the Portable interface
	var _ Portable = NetworkPort{}
	var _ Portable = NATSPort{}
	var _ Portable = NATSRequestPort{}
	var _ Portable = FilePort{}
	var _ Portable = JetStreamPort{}
	var _ Portable = KVWatchPort{}
	var _ Portable = KVWritePort{}
	var _ Portable = HTTPClientPort{}
}

func TestPortJSONSerialization(t *testing.T) {
	testBasicSerialization(t)
	testNATSSerialization(t)
	testNATSRequestSerialization(t)
	testFileSerialization(t)
	testJetStreamSerialization(t)
	testKVWatchSerialization(t)
}

func testBasicSerialization(t *testing.T) {
	port := Port{
		Name:        "udp_input",
		Direction:   DirectionInput,
		Required:    true,
		Description: "UDP MAVLink input",
		Config:      NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14550},
	}

	data, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("Failed to marshal port: %v", err)
	}

	var unmarshaled map[string]any
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal port: %v", err)
	}

	verifyPortFields(t, unmarshaled, port)
}

func testNATSSerialization(t *testing.T) {
	port := Port{
		Name:        "nats_output",
		Direction:   DirectionOutput,
		Required:    false,
		Description: "NATS message output",
		Config:      NATSPort{Subject: "messages.output", Queue: "processors"},
	}

	data, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("Failed to marshal port: %v", err)
	}

	var unmarshaled map[string]any
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal port: %v", err)
	}

	verifyPortFields(t, unmarshaled, port)
}

func testNATSRequestSerialization(t *testing.T) {
	port := Port{
		Name:        "storage_api",
		Direction:   DirectionInput,
		Required:    false,
		Description: "Storage API request/response",
		Config:      NATSRequestPort{Subject: "storage.api", Timeout: "1s", Retries: 3},
	}

	data, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("Failed to marshal port: %v", err)
	}

	var unmarshaled map[string]any
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal port: %v", err)
	}

	verifyPortFields(t, unmarshaled, port)

	// Verify config type
	config, ok := unmarshaled["config"].(map[string]any)
	if !ok {
		t.Fatal("Expected config to be a map")
	}
	if config["type"] != "nats-request" {
		t.Errorf("Expected config type 'nats-request', got %v", config["type"])
	}
}

func testFileSerialization(t *testing.T) {
	port := Port{
		Name:        "log_input",
		Direction:   DirectionInput,
		Required:    true,
		Description: "Log file input",
		Config:      FilePort{Path: "/var/log/app.log", Pattern: "*.log"},
	}

	data, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("Failed to marshal port: %v", err)
	}

	var unmarshaled map[string]any
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal port: %v", err)
	}

	verifyPortFields(t, unmarshaled, port)
}

func verifyPortFields(t *testing.T, unmarshaled map[string]any, original Port) {
	if unmarshaled["name"] != original.Name {
		t.Errorf("Expected name %s, got %s", original.Name, unmarshaled["name"])
	}
	if unmarshaled["direction"] != string(original.Direction) {
		t.Errorf("Expected direction %s, got %s", string(original.Direction), unmarshaled["direction"])
	}
	if unmarshaled["required"] != original.Required {
		t.Errorf("Expected required %t, got %t", original.Required, unmarshaled["required"])
	}
	if unmarshaled["description"] != original.Description {
		t.Errorf("Expected description %s, got %s", original.Description, unmarshaled["description"])
	}

	config, ok := unmarshaled["config"].(map[string]any)
	if !ok {
		t.Error("Expected config to be a map")
	}
	if len(config) == 0 {
		t.Error("Expected config to have content")
	}
}

func TestNetworkPortJSONSerialization(t *testing.T) {
	original := NetworkPort{
		Protocol: "tcp",
		Host:     "localhost",
		Port:     8080,
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var restored NetworkPort
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare
	if restored != original {
		t.Errorf("Expected %+v, got %+v", original, restored)
	}
}

func TestNATSPortJSONSerialization(t *testing.T) {
	original := NATSPort{
		Subject: "test.subject",
		Queue:   "test-queue",
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var restored NATSPort
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare
	if restored != original {
		t.Errorf("Expected %+v, got %+v", original, restored)
	}
}

func TestFilePortJSONSerialization(t *testing.T) {
	original := FilePort{
		Path:    "/test/path",
		Pattern: "*.test",
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var restored FilePort
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare
	if restored != original {
		t.Errorf("Expected %+v, got %+v", original, restored)
	}
}

func TestResourceIDUniqueness(t *testing.T) {
	// Test that different configurations produce different ResourceIDs
	networkPorts := []NetworkPort{
		{Protocol: "tcp", Host: "localhost", Port: 8080},
		{Protocol: "udp", Host: "localhost", Port: 8080},
		{Protocol: "tcp", Host: "0.0.0.0", Port: 8080},
		{Protocol: "tcp", Host: "localhost", Port: 9090},
	}

	resourceIDs := make(map[string]bool)
	for _, port := range networkPorts {
		id := port.ResourceID()
		if resourceIDs[id] {
			t.Errorf("Duplicate ResourceID found: %s", id)
		}
		resourceIDs[id] = true
	}

	// Test NATS ports
	natsPorts := []NATSPort{
		{Subject: "test.a"},
		{Subject: "test.b"},
		{Subject: "test.a", Queue: "different-queue"}, // Should still be same ResourceID
	}

	natsIDs := make(map[string]int)
	for _, port := range natsPorts {
		id := port.ResourceID()
		natsIDs[id]++
	}

	// Should have 2 unique IDs (test.a appears twice, test.b once)
	if len(natsIDs) != 2 {
		t.Errorf("Expected 2 unique NATS ResourceIDs, got %d", len(natsIDs))
	}
	if natsIDs["nats:test.a"] != 2 {
		t.Errorf("Expected test.a to appear twice, got %d", natsIDs["nats:test.a"])
	}
}

func testJetStreamSerialization(t *testing.T) {
	port := Port{
		Name:        "entity_events",
		Direction:   DirectionOutput,
		Required:    false,
		Description: "Entity state change events",
		Config: JetStreamPort{
			StreamName:    "ENTITY_EVENTS",
			Subjects:      []string{"events.graph.entity.>"},
			Storage:       "file",
			RetentionDays: 7,
			MaxSizeGB:     10,
			Replicas:      1,
		},
	}

	data, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("Failed to marshal port: %v", err)
	}

	var unmarshaled map[string]any
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal port: %v", err)
	}

	verifyPortFields(t, unmarshaled, port)

	// Verify JetStream-specific fields
	config, ok := unmarshaled["config"].(map[string]any)
	if !ok {
		t.Fatal("Config should be a map")
	}

	if config["type"] != "jetstream" {
		t.Errorf("Expected type jetstream, got %v", config["type"])
	}

	configData, ok := config["data"].(map[string]any)
	if !ok {
		t.Fatal("Data should be a map")
	}

	if configData["stream_name"] != "ENTITY_EVENTS" {
		t.Errorf("Expected stream_name ENTITY_EVENTS, got %v", configData["stream_name"])
	}
}

func testKVWatchSerialization(t *testing.T) {
	port := Port{
		Name:        "entity_watcher",
		Direction:   DirectionInput,
		Required:    false,
		Description: "Watch entity state changes",
		Config: KVWatchPort{
			Bucket:  "ENTITY_STATES",
			Keys:    []string{"entity.>"},
			History: true,
		},
	}

	data, err := json.Marshal(port)
	if err != nil {
		t.Fatalf("Failed to marshal port: %v", err)
	}

	var unmarshaled map[string]any
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal port: %v", err)
	}

	verifyPortFields(t, unmarshaled, port)

	// Verify KVWatch-specific fields
	config, ok := unmarshaled["config"].(map[string]any)
	if !ok {
		t.Fatal("Config should be a map")
	}

	if config["type"] != "kvwatch" {
		t.Errorf("Expected type kvwatch, got %v", config["type"])
	}

	configData, ok := config["data"].(map[string]any)
	if !ok {
		t.Fatal("Data should be a map")
	}

	if configData["bucket"] != "ENTITY_STATES" {
		t.Errorf("Expected bucket ENTITY_STATES, got %v", configData["bucket"])
	}

	if configData["history"] != true {
		t.Errorf("Expected history true, got %v", configData["history"])
	}
}

func TestJetStreamPortJSONSerialization(t *testing.T) {
	original := JetStreamPort{
		StreamName:    "TEST_STREAM",
		Subjects:      []string{"test.>"},
		Storage:       "memory",
		RetentionDays: 1,
		MaxSizeGB:     1,
		Replicas:      3,
		ConsumerName:  "test-consumer",
		DeliverPolicy: "last",
		AckPolicy:     "explicit",
		MaxDeliver:    5,
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var restored JetStreamPort
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare
	if restored.StreamName != original.StreamName {
		t.Errorf("StreamName mismatch: %s != %s", restored.StreamName, original.StreamName)
	}
	if len(restored.Subjects) != len(original.Subjects) || restored.Subjects[0] != original.Subjects[0] {
		t.Errorf("Subjects mismatch: %v != %v", restored.Subjects, original.Subjects)
	}
	if restored.Storage != original.Storage {
		t.Errorf("Storage mismatch: %s != %s", restored.Storage, original.Storage)
	}
}

func TestKVWatchPortJSONSerialization(t *testing.T) {
	original := KVWatchPort{
		Bucket:  "TEST_BUCKET",
		Keys:    []string{"key1", "key2.*"},
		History: true,
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var restored KVWatchPort
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare
	if restored.Bucket != original.Bucket {
		t.Errorf("Bucket mismatch: %s != %s", restored.Bucket, original.Bucket)
	}
	if len(restored.Keys) != len(original.Keys) {
		t.Errorf("Keys length mismatch: %d != %d", len(restored.Keys), len(original.Keys))
	}
	if restored.History != original.History {
		t.Errorf("History mismatch: %t != %t", restored.History, original.History)
	}
}

func TestKVWritePort(t *testing.T) {
	tests := []struct {
		name        string
		port        KVWritePort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name: "KV Write basic bucket",
			port: KVWritePort{
				Bucket: "ENTITY_STATES",
			},
			resourceID:  "kvwrite:ENTITY_STATES",
			isExclusive: false,
			portType:    "kvwrite",
		},
		{
			name: "KV Write with interface contract",
			port: KVWritePort{
				Bucket: "PREDICATE_INDEX",
				Interface: &InterfaceContract{
					Type:    "graph.PredicateEntry",
					Version: "v1",
				},
			},
			resourceID:  "kvwrite:PREDICATE_INDEX",
			isExclusive: false,
			portType:    "kvwrite",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("Expected ResourceID %s, got %s", tt.resourceID, tt.port.ResourceID())
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("Expected IsExclusive %t, got %t", tt.isExclusive, tt.port.IsExclusive())
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Expected Type %s, got %s", tt.portType, tt.port.Type())
			}
		})
	}
}

func TestKVWritePortJSONSerialization(t *testing.T) {
	original := KVWritePort{
		Bucket: "TEST_BUCKET",
		Interface: &InterfaceContract{
			Type:    "test.Entity",
			Version: "v1",
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var restored KVWritePort
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare
	if restored.Bucket != original.Bucket {
		t.Errorf("Bucket mismatch: %s != %s", restored.Bucket, original.Bucket)
	}
	if restored.Interface == nil || original.Interface == nil {
		if restored.Interface != original.Interface {
			t.Errorf("Interface mismatch: one is nil")
		}
	} else {
		if restored.Interface.Type != original.Interface.Type {
			t.Errorf("Interface.Type mismatch: %s != %s", restored.Interface.Type, original.Interface.Type)
		}
		if restored.Interface.Version != original.Interface.Version {
			t.Errorf("Interface.Version mismatch: %s != %s", restored.Interface.Version, original.Interface.Version)
		}
	}
}

func TestGetConsumerConfig(t *testing.T) {
	tests := []struct {
		name           string
		port           Port
		wantDeliverPol string
		wantAckPol     string
		wantMaxDeliver int
	}{
		{
			name: "JetStream port with all values",
			port: Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config: JetStreamPort{
					DeliverPolicy: "all",
					AckPolicy:     "none",
					MaxDeliver:    10,
				},
			},
			wantDeliverPol: "all",
			wantAckPol:     "none",
			wantMaxDeliver: 10,
		},
		{
			name: "JetStream port with partial values",
			port: Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config: JetStreamPort{
					DeliverPolicy: "last",
					// AckPolicy and MaxDeliver not set
				},
			},
			wantDeliverPol: "last",
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "JetStream port with no values (defaults)",
			port: Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config:    JetStreamPort{},
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "Non-JetStream port (NATS)",
			port: Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config:    NATSPort{Subject: "test.subject"},
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "Non-JetStream port (KVWatch)",
			port: Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config:    KVWatchPort{Bucket: "TEST"},
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "Port with nil config",
			port: Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config:    nil,
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetConsumerConfig(tt.port)

			if cfg.DeliverPolicy != tt.wantDeliverPol {
				t.Errorf("DeliverPolicy = %q, want %q", cfg.DeliverPolicy, tt.wantDeliverPol)
			}
			if cfg.AckPolicy != tt.wantAckPol {
				t.Errorf("AckPolicy = %q, want %q", cfg.AckPolicy, tt.wantAckPol)
			}
			if cfg.MaxDeliver != tt.wantMaxDeliver {
				t.Errorf("MaxDeliver = %d, want %d", cfg.MaxDeliver, tt.wantMaxDeliver)
			}
		})
	}
}

func TestGetConsumerConfigFromDefinition(t *testing.T) {
	tests := []struct {
		name           string
		portDef        PortDefinition
		wantDeliverPol string
		wantAckPol     string
		wantMaxDeliver int
	}{
		{
			name: "PortDefinition with JetStreamPort config",
			portDef: PortDefinition{
				Name:    "entity_watch",
				Type:    "jetstream",
				Subject: "events.graph.entity.>",
				Config: JetStreamPort{
					DeliverPolicy: "all",
					AckPolicy:     "explicit",
					MaxDeliver:    5,
				},
			},
			wantDeliverPol: "all",
			wantAckPol:     "explicit",
			wantMaxDeliver: 5,
		},
		{
			name: "PortDefinition with partial JetStreamPort config",
			portDef: PortDefinition{
				Name:    "entity_watch",
				Type:    "jetstream",
				Subject: "events.graph.entity.>",
				Config: JetStreamPort{
					DeliverPolicy: "new",
				},
			},
			wantDeliverPol: "new",
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "PortDefinition with empty JetStreamPort config",
			portDef: PortDefinition{
				Name:    "entity_watch",
				Type:    "jetstream",
				Subject: "events.graph.entity.>",
				Config:  JetStreamPort{},
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "PortDefinition with nil config",
			portDef: PortDefinition{
				Name:    "entity_watch",
				Type:    "jetstream",
				Subject: "events.graph.entity.>",
				Config:  nil,
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
		{
			name: "PortDefinition with non-JetStream config type",
			portDef: PortDefinition{
				Name:    "nats_port",
				Type:    "nats",
				Subject: "test.subject",
				Config:  NATSPort{Subject: "test.subject"},
			},
			wantDeliverPol: "new",      // default
			wantAckPol:     "explicit", // default
			wantMaxDeliver: 3,          // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GetConsumerConfigFromDefinition(tt.portDef)

			if cfg.DeliverPolicy != tt.wantDeliverPol {
				t.Errorf("DeliverPolicy = %q, want %q", cfg.DeliverPolicy, tt.wantDeliverPol)
			}
			if cfg.AckPolicy != tt.wantAckPol {
				t.Errorf("AckPolicy = %q, want %q", cfg.AckPolicy, tt.wantAckPol)
			}
			if cfg.MaxDeliver != tt.wantMaxDeliver {
				t.Errorf("MaxDeliver = %d, want %d", cfg.MaxDeliver, tt.wantMaxDeliver)
			}
		})
	}
}

func TestConsumerConfigDefaults(t *testing.T) {
	// Test that default values are safe for production use
	emptyPort := Port{Name: "test", Direction: DirectionInput}
	cfg := GetConsumerConfig(emptyPort)

	// "new" is the safe default - doesn't replay historical messages
	if cfg.DeliverPolicy != "new" {
		t.Errorf("Default DeliverPolicy should be 'new' (safe), got %q", cfg.DeliverPolicy)
	}

	// "explicit" requires ack - prevents message loss
	if cfg.AckPolicy != "explicit" {
		t.Errorf("Default AckPolicy should be 'explicit' (safe), got %q", cfg.AckPolicy)
	}

	// 3 retries is a reasonable default
	if cfg.MaxDeliver != 3 {
		t.Errorf("Default MaxDeliver should be 3, got %d", cfg.MaxDeliver)
	}
}

// TestResolveSubject covers the four behavioral branches in
// component.ResolveSubject. The function is load-bearing for every
// component that needs to publish to an output port: it returns the
// configured subject pattern with the wildcard replaced, falling back
// to "portName.<suffix>" when no matching port is configured. This
// fallback is what makes a component robust against operator configs
// that override Ports without listing every default — without it,
// components like agentic-tools (publishResult) silently drop messages
// when their output port isn't in the user's config (regression
// surfaced 2026-05-08; agentic-model dodged the same shape because it
// was already using ResolveSubject).
func TestResolveSubject(t *testing.T) {
	tests := []struct {
		name     string
		ports    []PortDefinition
		portName string
		suffix   string
		want     string
	}{
		{
			name:     "no ports falls back to portName.suffix",
			ports:    nil,
			portName: "tool.result",
			suffix:   "call-abc",
			want:     "tool.result.call-abc",
		},
		{
			name:     "empty slice falls back to portName.suffix",
			ports:    []PortDefinition{},
			portName: "tool.result",
			suffix:   "call-xyz",
			want:     "tool.result.call-xyz",
		},
		{
			name: "non-matching port falls back to portName.suffix",
			ports: []PortDefinition{
				{Name: "other.port", Subject: "some.other.subject.>"},
			},
			portName: "tool.result",
			suffix:   "call-123",
			want:     "tool.result.call-123",
		},
		{
			name: "trailing-star wildcard replaced with suffix",
			ports: []PortDefinition{
				{Name: "tool.result", Subject: "tool.result.*"},
			},
			portName: "tool.result",
			suffix:   "call-abc",
			want:     "tool.result.call-abc",
		},
		{
			name: "trailing-arrow wildcard replaced with suffix",
			ports: []PortDefinition{
				{Name: "agent.response", Subject: "agent.response.>"},
			},
			portName: "agent.response",
			suffix:   "req-123",
			want:     "agent.response.req-123",
		},
		{
			name: "no wildcard appends suffix with separator",
			ports: []PortDefinition{
				{Name: "events", Subject: "events.graph"},
			},
			portName: "events",
			suffix:   "entity",
			want:     "events.graph.entity",
		},
		{
			name: "operator-overridden subject prefix honored",
			ports: []PortDefinition{
				{Name: "tool.result", Subject: "custom.tool.results.*"},
			},
			portName: "tool.result",
			suffix:   "call-99",
			want:     "custom.tool.results.call-99",
		},
		{
			name: "first matching port wins on duplicate names",
			ports: []PortDefinition{
				{Name: "tool.result", Subject: "first.subject.>"},
				{Name: "tool.result", Subject: "second.subject.>"},
			},
			portName: "tool.result",
			suffix:   "tail",
			want:     "first.subject.tail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSubject(tt.ports, tt.portName, tt.suffix)
			if got != tt.want {
				t.Errorf("ResolveSubject(%v, %q, %q) = %q, want %q", tt.ports, tt.portName, tt.suffix, got, tt.want)
			}
		})
	}
}

// TestPortDefinition_UnmarshalJSON_JetStreamConfig is the load-bearing
// regression test for the beta.55 hotfix: a JSON-loaded PortDefinition
// must place a JetStreamPort struct into Config (not a map[string]any),
// otherwise every consumer-config field declared in YAML/JSON is
// silently dropped at the type assertion in BuildPortFromDefinition
// and applyJetStreamConsumerConfig.
//
// The semspec repro (2026-05-07) showed AckWait/HeartbeatInterval/
// MaxDeliver/DeliverPolicy all reaching zero values at runtime even
// though they were declared in the port's `config` block.
func TestPortDefinition_UnmarshalJSON_JetStreamConfig(t *testing.T) {
	raw := []byte(`{
		"name": "agent.request",
		"type": "jetstream",
		"subject": "agent.request.>",
		"config": {
			"deliver_policy": "all",
			"ack_policy": "explicit",
			"max_deliver": 1,
			"consumer_name": "model-consumer",
			"ack_wait": "300s",
			"heartbeat_interval": "60s"
		}
	}`)

	var pd PortDefinition
	if err := json.Unmarshal(raw, &pd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if pd.Name != "agent.request" || pd.Type != "jetstream" || pd.Subject != "agent.request.>" {
		t.Errorf("top-level fields lost: %+v", pd)
	}

	js, ok := pd.Config.(JetStreamPort)
	if !ok {
		t.Fatalf("Config = %T, want JetStreamPort (the bug semspec repro'd)", pd.Config)
	}
	if js.DeliverPolicy != "all" {
		t.Errorf("DeliverPolicy = %q, want %q", js.DeliverPolicy, "all")
	}
	if js.AckPolicy != "explicit" {
		t.Errorf("AckPolicy = %q, want %q", js.AckPolicy, "explicit")
	}
	if js.MaxDeliver != 1 {
		t.Errorf("MaxDeliver = %d, want 1", js.MaxDeliver)
	}
	if js.ConsumerName != "model-consumer" {
		t.Errorf("ConsumerName = %q, want %q", js.ConsumerName, "model-consumer")
	}
	if js.AckWait != "300s" {
		t.Errorf("AckWait = %q, want %q", js.AckWait, "300s")
	}
	if js.HeartbeatInterval != "60s" {
		t.Errorf("HeartbeatInterval = %q, want %q", js.HeartbeatInterval, "60s")
	}
}

// TestPortDefinition_UnmarshalJSON_RoundTripsToConsumerConfig closes the
// loop by going JSON → PortDefinition → ConsumerConfig — the path every
// agentic-model / agentic-tools consumer uses at runtime. This is the
// end-to-end test that would have caught the beta.54 ship-to-runtime gap.
func TestPortDefinition_UnmarshalJSON_RoundTripsToConsumerConfig(t *testing.T) {
	raw := []byte(`{
		"name": "agent.request",
		"type": "jetstream",
		"subject": "agent.request.>",
		"config": {
			"max_deliver": 1,
			"ack_wait": "5m",
			"heartbeat_interval": "2m"
		}
	}`)

	var pd PortDefinition
	if err := json.Unmarshal(raw, &pd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cfg := GetConsumerConfigFromDefinition(pd)
	if cfg.MaxDeliver != 1 {
		t.Errorf("MaxDeliver = %d, want 1 (Posture B fail-loud-once value must reach the consumer)", cfg.MaxDeliver)
	}
	if cfg.AckWait != 5*time.Minute {
		t.Errorf("AckWait = %v, want 5m", cfg.AckWait)
	}
	if cfg.HeartbeatInterval != 2*time.Minute {
		t.Errorf("HeartbeatInterval = %v, want 2m", cfg.HeartbeatInterval)
	}
}

// TestPortDefinition_UnmarshalJSON_NoConfig asserts an entry with no
// `config` block round-trips cleanly (no panic, Config stays nil).
func TestPortDefinition_UnmarshalJSON_NoConfig(t *testing.T) {
	raw := []byte(`{"name":"x","type":"jetstream","subject":"x.>"}`)

	var pd PortDefinition
	if err := json.Unmarshal(raw, &pd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pd.Config != nil {
		t.Errorf("Config = %v, want nil when JSON had no config block", pd.Config)
	}
}

// TestPortDefinition_UnmarshalJSON_UnknownType falls back to map[string]any
// so custom port types not yet known to the framework are forward-compat.
func TestPortDefinition_UnmarshalJSON_UnknownType(t *testing.T) {
	raw := []byte(`{"name":"x","type":"custom-future-type","config":{"key":"value"}}`)

	var pd PortDefinition
	if err := json.Unmarshal(raw, &pd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m, ok := pd.Config.(map[string]any)
	if !ok {
		t.Fatalf("Config = %T, want map[string]any for unknown types", pd.Config)
	}
	if m["key"] != "value" {
		t.Errorf("map missing key/value: %v", m)
	}
}

// TestBuildPortFromDefinition_CopiesAckWaitAndHeartbeat asserts the second
// half of the beta.55 fix: even with a Go-constructed PortDefinition
// (not JSON-loaded), BuildPortFromDefinition's jetstream case must copy
// AckWait + HeartbeatInterval into the resulting Port's JetStreamPort.
// Beta.54 added these fields to the struct but didn't add the merge
// branch.
func TestBuildPortFromDefinition_CopiesAckWaitAndHeartbeat(t *testing.T) {
	pd := PortDefinition{
		Name:    "agent.request",
		Type:    "jetstream",
		Subject: "agent.request.>",
		Config: JetStreamPort{
			AckWait:           "300s",
			HeartbeatInterval: "60s",
			MaxDeliver:        1,
			DeliverPolicy:     "all",
		},
	}
	port := BuildPortFromDefinition(pd, DirectionInput)
	js, ok := port.Config.(JetStreamPort)
	if !ok {
		t.Fatalf("Config = %T, want JetStreamPort", port.Config)
	}
	if js.AckWait != "300s" {
		t.Errorf("AckWait = %q, want %q", js.AckWait, "300s")
	}
	if js.HeartbeatInterval != "60s" {
		t.Errorf("HeartbeatInterval = %q, want %q", js.HeartbeatInterval, "60s")
	}
	if js.MaxDeliver != 1 {
		t.Errorf("MaxDeliver = %d, want 1", js.MaxDeliver)
	}
	if js.DeliverPolicy != "all" {
		t.Errorf("DeliverPolicy = %q, want %q", js.DeliverPolicy, "all")
	}
}

// TestGetConsumerConfig_AckWaitAndHeartbeat verifies the per-port AckWait
// and HeartbeatInterval fields parse from string ("90s", "2m") into a
// time.Duration on ConsumerConfig, and that empty/invalid values produce
// the zero duration so callers can apply their per-component default.
//
// This is the load-bearing surface for semspec ADR-? — a single AckWait
// per consumer/port lets paid-LLM deployments tune ack_wait above their
// longest endpoint.RequestTimeout without forking the agentic-model
// component (semstreams beta.53 audit identified this gap).
func TestGetConsumerConfig_AckWaitAndHeartbeat(t *testing.T) {
	tests := []struct {
		name              string
		ackWait           string
		heartbeatInterval string
		wantAckWait       time.Duration
		wantHeartbeat     time.Duration
	}{
		{
			name:              "both set with valid durations",
			ackWait:           "300s",
			heartbeatInterval: "60s",
			wantAckWait:       300 * time.Second,
			wantHeartbeat:     60 * time.Second,
		},
		{
			name:              "minute units accepted",
			ackWait:           "5m",
			heartbeatInterval: "2m",
			wantAckWait:       5 * time.Minute,
			wantHeartbeat:     2 * time.Minute,
		},
		{
			name:              "both empty falls through to zero (caller default)",
			ackWait:           "",
			heartbeatInterval: "",
			wantAckWait:       0,
			wantHeartbeat:     0,
		},
		{
			name:              "invalid AckWait stays zero (caller default)",
			ackWait:           "not-a-duration",
			heartbeatInterval: "60s",
			wantAckWait:       0,
			wantHeartbeat:     60 * time.Second,
		},
		{
			name:              "invalid HeartbeatInterval stays zero (caller default)",
			ackWait:           "90s",
			heartbeatInterval: "garbage",
			wantAckWait:       90 * time.Second,
			wantHeartbeat:     0,
		},
		{
			name:              "only AckWait set",
			ackWait:           "120s",
			heartbeatInterval: "",
			wantAckWait:       120 * time.Second,
			wantHeartbeat:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := Port{
				Name:      "test_input",
				Direction: DirectionInput,
				Config: JetStreamPort{
					AckWait:           tt.ackWait,
					HeartbeatInterval: tt.heartbeatInterval,
				},
			}
			cfg := GetConsumerConfig(port)
			if cfg.AckWait != tt.wantAckWait {
				t.Errorf("AckWait = %v, want %v", cfg.AckWait, tt.wantAckWait)
			}
			if cfg.HeartbeatInterval != tt.wantHeartbeat {
				t.Errorf("HeartbeatInterval = %v, want %v", cfg.HeartbeatInterval, tt.wantHeartbeat)
			}

			// Same surface via PortDefinition path
			portDef := PortDefinition{
				Name:    "test_input",
				Type:    "jetstream",
				Subject: "test.>",
				Config: JetStreamPort{
					AckWait:           tt.ackWait,
					HeartbeatInterval: tt.heartbeatInterval,
				},
			}
			cfgDef := GetConsumerConfigFromDefinition(portDef)
			if cfgDef.AckWait != tt.wantAckWait {
				t.Errorf("FromDefinition AckWait = %v, want %v", cfgDef.AckWait, tt.wantAckWait)
			}
			if cfgDef.HeartbeatInterval != tt.wantHeartbeat {
				t.Errorf("FromDefinition HeartbeatInterval = %v, want %v", cfgDef.HeartbeatInterval, tt.wantHeartbeat)
			}
		})
	}
}

// TestPort_UnmarshalJSON_RoundTripsToTypedConfig closes the audit-discovered
// test gap on Port (component/port.go:78). The pre-existing
// TestPortJSONSerialization marshals a Go-constructed Port to JSON and
// inspects via map[string]any — it never asserts that
// `json.Unmarshal(raw, &Port{})` produces a Port whose Config field
// satisfies the typed assertion `port.Config.(SpecificPortType)`. That's
// exactly the assertion shape that would catch a beta.54-class regression
// (Port.UnmarshalJSON breaking type-discrimination silently). Mirrors the
// PortDefinition coverage from PR #42 / beta.55.
//
// Wire shape under test is Port's `{"type":"...","data":{...}}` envelope
// (component/port.go:50-71), distinct from PortDefinition's flat shape.
func TestPort_UnmarshalJSON_RoundTripsToTypedConfig(t *testing.T) {
	tests := []struct {
		name     string
		original Port
		assert   func(t *testing.T, decoded Port)
	}{
		{
			name: "JetStream",
			original: Port{
				Name:        "agent.request",
				Direction:   DirectionInput,
				Required:    true,
				Description: "Agent request input",
				Config: JetStreamPort{
					StreamName:        "AGENT",
					Subjects:          []string{"agent.request.>"},
					DeliverPolicy:     "all",
					AckPolicy:         "explicit",
					MaxDeliver:        1,
					AckWait:           "300s",
					HeartbeatInterval: "60s",
					ConsumerName:      "agent-consumer",
				},
			},
			assert: func(t *testing.T, decoded Port) {
				js, ok := decoded.Config.(JetStreamPort)
				if !ok {
					t.Fatalf("Config = %T, want JetStreamPort (drift in Port.UnmarshalJSON jetstream case)", decoded.Config)
				}
				if js.StreamName != "AGENT" {
					t.Errorf("StreamName = %q, want %q", js.StreamName, "AGENT")
				}
				if js.MaxDeliver != 1 {
					t.Errorf("MaxDeliver = %d, want 1", js.MaxDeliver)
				}
				if js.AckWait != "300s" {
					t.Errorf("AckWait = %q, want %q", js.AckWait, "300s")
				}
				if js.HeartbeatInterval != "60s" {
					t.Errorf("HeartbeatInterval = %q, want %q", js.HeartbeatInterval, "60s")
				}
			},
		},
		{
			name: "NATS",
			original: Port{
				Name:        "nats_output",
				Direction:   DirectionOutput,
				Required:    false,
				Description: "NATS message output",
				Config:      NATSPort{Subject: "messages.output", Queue: "processors"},
			},
			assert: func(t *testing.T, decoded Port) {
				n, ok := decoded.Config.(NATSPort)
				if !ok {
					t.Fatalf("Config = %T, want NATSPort", decoded.Config)
				}
				if n.Subject != "messages.output" {
					t.Errorf("Subject = %q, want %q", n.Subject, "messages.output")
				}
				if n.Queue != "processors" {
					t.Errorf("Queue = %q, want %q", n.Queue, "processors")
				}
			},
		},
		{
			name: "NATSRequest",
			original: Port{
				Name:        "storage_api",
				Direction:   DirectionInput,
				Required:    false,
				Description: "Storage API request/response",
				Config:      NATSRequestPort{Subject: "storage.api", Timeout: "1s", Retries: 3},
			},
			assert: func(t *testing.T, decoded Port) {
				nr, ok := decoded.Config.(NATSRequestPort)
				if !ok {
					t.Fatalf("Config = %T, want NATSRequestPort", decoded.Config)
				}
				if nr.Subject != "storage.api" {
					t.Errorf("Subject = %q, want %q", nr.Subject, "storage.api")
				}
				if nr.Retries != 3 {
					t.Errorf("Retries = %d, want 3", nr.Retries)
				}
			},
		},
		{
			name: "KVWatch",
			original: Port{
				Name:        "entity_watch",
				Direction:   DirectionInput,
				Required:    true,
				Description: "Entity state watch",
				Config:      KVWatchPort{Bucket: "ENTITIES"},
			},
			assert: func(t *testing.T, decoded Port) {
				kv, ok := decoded.Config.(KVWatchPort)
				if !ok {
					t.Fatalf("Config = %T, want KVWatchPort", decoded.Config)
				}
				if kv.Bucket != "ENTITIES" {
					t.Errorf("Bucket = %q, want %q", kv.Bucket, "ENTITIES")
				}
			},
		},
		{
			name: "Network",
			original: Port{
				Name:        "udp_input",
				Direction:   DirectionInput,
				Required:    true,
				Description: "UDP MAVLink input",
				Config:      NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14550},
			},
			assert: func(t *testing.T, decoded Port) {
				n, ok := decoded.Config.(NetworkPort)
				if !ok {
					t.Fatalf("Config = %T, want NetworkPort", decoded.Config)
				}
				if n.Protocol != "udp" {
					t.Errorf("Protocol = %q, want udp", n.Protocol)
				}
				if n.Port != 14550 {
					t.Errorf("Port = %d, want 14550", n.Port)
				}
			},
		},
		{
			name: "File",
			original: Port{
				Name:        "log_input",
				Direction:   DirectionInput,
				Required:    true,
				Description: "Log file input",
				Config:      FilePort{Path: "/var/log/app.log", Pattern: "*.log"},
			},
			assert: func(t *testing.T, decoded Port) {
				f, ok := decoded.Config.(FilePort)
				if !ok {
					t.Fatalf("Config = %T, want FilePort", decoded.Config)
				}
				if f.Path != "/var/log/app.log" {
					t.Errorf("Path = %q, want /var/log/app.log", f.Path)
				}
			},
		},
		{
			// Envelope round-trip for HTTPClientPort catches drift in
			// Port.UnmarshalJSON's http-client case (same class as
			// TestPort_UnmarshalJSON_RoundTripsToTypedConfig motivation).
			name: "HTTPClient",
			original: Port{
				Name:        "cap_feed",
				Direction:   DirectionInput,
				Required:    false,
				Description: "CAP alert feed input",
				Config: HTTPClientPort{
					Method:        "GET",
					URLPattern:    "https://api.weather.gov/alerts/active",
					TriggerPort:   "cap_timer",
					AuthRef:       "opensky_basic",
					ContactPolicy: "SemStreams/1.0 (contact@example.com)",
					Interface:     &InterfaceContract{Type: "alert.CAP", Version: "v1"},
				},
			},
			assert: func(t *testing.T, decoded Port) {
				hc, ok := decoded.Config.(HTTPClientPort)
				if !ok {
					t.Fatalf("Config = %T, want HTTPClientPort (drift in Port.UnmarshalJSON http-client case)", decoded.Config)
				}
				if hc.URLPattern != "https://api.weather.gov/alerts/active" {
					t.Errorf("URLPattern = %q, want https://api.weather.gov/alerts/active", hc.URLPattern)
				}
				if hc.TriggerPort != "cap_timer" {
					t.Errorf("TriggerPort = %q, want cap_timer", hc.TriggerPort)
				}
				if hc.AuthRef != "opensky_basic" {
					t.Errorf("AuthRef = %q, want opensky_basic", hc.AuthRef)
				}
				if hc.Method != "GET" {
					t.Errorf("Method = %q, want GET", hc.Method)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.original)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded Port
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// Top-level fields preserve through round-trip.
			if decoded.Name != tt.original.Name {
				t.Errorf("Name = %q, want %q", decoded.Name, tt.original.Name)
			}
			if decoded.Direction != tt.original.Direction {
				t.Errorf("Direction = %q, want %q", decoded.Direction, tt.original.Direction)
			}
			if decoded.Required != tt.original.Required {
				t.Errorf("Required = %v, want %v", decoded.Required, tt.original.Required)
			}
			if decoded.Description != tt.original.Description {
				t.Errorf("Description = %q, want %q", decoded.Description, tt.original.Description)
			}

			// The load-bearing assertion: Config is the typed Portable
			// implementation, not a map[string]any. This is what catches
			// drift in Port.UnmarshalJSON's type-discrimination switch.
			tt.assert(t, decoded)
		})
	}
}

// TestPort_UnmarshalJSON_KVDashedAliases locks in audit-finding-5
// (2026-05-08): Port.UnmarshalJSON must accept the dashed kv
// spellings that PortDefinition.UnmarshalJSON already accepts.
// Pre-fix, operator configs using "kv-watch" / "kv-write" / "kv"
// parsed via the PortDefinition path but failed loudly if the same
// Port struct was round-tripped through Port.UnmarshalJSON (e.g.
// KV-stored Port descriptors or serialized flow definitions).
//
// Each case feeds a raw envelope-shaped Port JSON and asserts the
// Config decodes to the expected typed Portable. Drift between this
// switch and component/ports.go's PortDefinition.UnmarshalJSON
// switch trips the test immediately.
func TestPort_UnmarshalJSON_KVDashedAliases(t *testing.T) {
	tests := []struct {
		name     string
		typeStr  string
		wantImpl string // pretty-printed concrete type the Config should decode to
	}{
		{name: "kvwatch concatenated", typeStr: "kvwatch", wantImpl: "component.KVWatchPort"},
		{name: "kv-watch dashed", typeStr: "kv-watch", wantImpl: "component.KVWatchPort"},
		{name: "kvwrite concatenated", typeStr: "kvwrite", wantImpl: "component.KVWritePort"},
		{name: "kv-write dashed", typeStr: "kv-write", wantImpl: "component.KVWritePort"},
		{name: "kv shorthand", typeStr: "kv", wantImpl: "component.KVWritePort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wire-format mirror of how Port serializes: top-level
			// name/direction/required/description, plus a config
			// envelope with {type, data}.
			var configData string
			switch tt.wantImpl {
			case "component.KVWatchPort":
				configData = `{"bucket":"ENTITY_STATES"}`
			case "component.KVWritePort":
				configData = `{"bucket":"AGENT_LOOPS"}`
			default:
				t.Fatalf("test setup gap: no fixture for impl %q", tt.wantImpl)
			}
			raw := fmt.Sprintf(`{
				"name": "test.port",
				"direction": "output",
				"required": false,
				"description": "audit-finding-5 dashed-alias coverage",
				"config": {"type": %q, "data": %s}
			}`, tt.typeStr, configData)

			var decoded Port
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				t.Fatalf("unmarshal %q: %v", tt.typeStr, err)
			}

			// Locate which typed Portable the Config decoded to.
			var gotImpl string
			switch decoded.Config.(type) {
			case KVWatchPort:
				gotImpl = "component.KVWatchPort"
			case KVWritePort:
				gotImpl = "component.KVWritePort"
			default:
				gotImpl = fmt.Sprintf("%T", decoded.Config)
			}
			if gotImpl != tt.wantImpl {
				t.Errorf("Config = %s, want %s (spelling %q dropped to map or wrong type)", gotImpl, tt.wantImpl, tt.typeStr)
			}
		})
	}
}

// TestHTTPClientPort exercises ResourceID, IsExclusive, and Type.
// ResourceID must prefix with "http-client:", default an empty Method to GET,
// and incorporate the URL pattern. IsExclusive must be false (outbound client
// relationships are shareable). Type must be "http-client".
func TestHTTPClientPort(t *testing.T) {
	tests := []struct {
		name        string
		port        HTTPClientPort
		resourceID  string
		isExclusive bool
		portType    string
	}{
		{
			name:        "GET with full URL",
			port:        HTTPClientPort{Method: "GET", URLPattern: "https://api.weather.gov/alerts/active"},
			resourceID:  "http-client:GET:https://api.weather.gov/alerts/active",
			isExclusive: false,
			portType:    "http-client",
		},
		{
			name:        "empty method defaults to GET",
			port:        HTTPClientPort{URLPattern: "https://api.opensky-network.org/api/states/all"},
			resourceID:  "http-client:GET:https://api.opensky-network.org/api/states/all",
			isExclusive: false,
			portType:    "http-client",
		},
		{
			name:        "POST variant",
			port:        HTTPClientPort{Method: "POST", URLPattern: "https://api.example.com/data"},
			resourceID:  "http-client:POST:https://api.example.com/data",
			isExclusive: false,
			portType:    "http-client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.port.ResourceID() != tt.resourceID {
				t.Errorf("ResourceID = %q, want %q", tt.port.ResourceID(), tt.resourceID)
			}
			if tt.port.IsExclusive() != tt.isExclusive {
				t.Errorf("IsExclusive = %v, want %v", tt.port.IsExclusive(), tt.isExclusive)
			}
			if tt.port.Type() != tt.portType {
				t.Errorf("Type = %q, want %q", tt.port.Type(), tt.portType)
			}
		})
	}
}

// TestHTTPClientPortJSONSerialization verifies struct-level marshal → unmarshal
// round-trip preserving all six fields. Mirrors TestNetworkPortJSONSerialization.
func TestHTTPClientPortJSONSerialization(t *testing.T) {
	original := HTTPClientPort{
		Method:        "GET",
		URLPattern:    "https://api.weather.gov/alerts/active",
		TriggerPort:   "cap_timer",
		AuthRef:       "opensky_basic",
		ContactPolicy: "SemStreams/1.0 (contact@example.com)",
		Interface:     &InterfaceContract{Type: "alert.CAP", Version: "v1"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored HTTPClientPort
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.Method != original.Method {
		t.Errorf("Method = %q, want %q", restored.Method, original.Method)
	}
	if restored.URLPattern != original.URLPattern {
		t.Errorf("URLPattern = %q, want %q", restored.URLPattern, original.URLPattern)
	}
	if restored.TriggerPort != original.TriggerPort {
		t.Errorf("TriggerPort = %q, want %q", restored.TriggerPort, original.TriggerPort)
	}
	if restored.AuthRef != original.AuthRef {
		t.Errorf("AuthRef = %q, want %q", restored.AuthRef, original.AuthRef)
	}
	if restored.ContactPolicy != original.ContactPolicy {
		t.Errorf("ContactPolicy = %q, want %q", restored.ContactPolicy, original.ContactPolicy)
	}
	if restored.Interface == nil {
		t.Fatal("Interface = nil, want non-nil")
	}
	if restored.Interface.Type != original.Interface.Type {
		t.Errorf("Interface.Type = %q, want %q", restored.Interface.Type, original.Interface.Type)
	}
	if restored.Interface.Version != original.Interface.Version {
		t.Errorf("Interface.Version = %q, want %q", restored.Interface.Version, original.Interface.Version)
	}
}

// TestHTTPClientPort_SecretsNotInSerializedOutput verifies that:
//  1. AuthRef (a credential key reference, NOT a value) is present in the
//     serialized JSON — the reference must survive the round-trip so the
//     runtime resolver can find it.
//  2. The set of JSON keys emitted for a fully-populated HTTPClientPort is
//     exactly {method, url_pattern, trigger_port, auth_ref, contact_policy,
//     interface} — no extra keys leak into the wire format.
//  3. A sparse port (only URLPattern set) omits all omitempty optional fields,
//     confirming they do not pollute the wire format when unused.
func TestHTTPClientPort_SecretsNotInSerializedOutput(t *testing.T) {
	t.Run("fully populated — AuthRef survives and key set is exact", func(t *testing.T) {
		port := HTTPClientPort{
			Method:        "GET",
			URLPattern:    "https://api.weather.gov/alerts/active",
			TriggerPort:   "cap_timer",
			AuthRef:       "opensky_basic",
			ContactPolicy: "SemStreams/1.0 (contact@example.com)",
			Interface:     &InterfaceContract{Type: "alert.CAP", Version: "v1"},
		}

		data, err := json.Marshal(port)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		// (a) AuthRef reference value must survive in the output.
		if !strings.Contains(string(data), "opensky_basic") {
			t.Errorf("AuthRef key reference %q not found in JSON; reference was lost during serialization", "opensky_basic")
		}

		// (b) Key set must be exactly the declared fields — no extras leak in.
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal to map: %v", err)
		}
		expected := map[string]bool{
			"method":         true,
			"url_pattern":    true,
			"trigger_port":   true,
			"auth_ref":       true,
			"contact_policy": true,
			"interface":      true,
		}
		for k := range m {
			if !expected[k] {
				t.Errorf("unexpected JSON key %q in serialized HTTPClientPort; struct has undeclared field", k)
			}
		}
		// Only url_pattern is non-omitempty, so verify it is present.
		if _, ok := m["url_pattern"]; !ok {
			t.Errorf("expected JSON key %q missing from serialized HTTPClientPort", "url_pattern")
		}
	})

	t.Run("sparse port — omitempty fields absent when zero", func(t *testing.T) {
		// Only URLPattern set; all other fields are zero/nil.
		sparse := HTTPClientPort{
			URLPattern: "https://api.weather.gov/alerts/active",
		}

		data, err := json.Marshal(sparse)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal to map: %v", err)
		}

		// url_pattern must be present (it is not omitempty).
		if _, ok := m["url_pattern"]; !ok {
			t.Errorf("url_pattern missing from sparse HTTPClientPort JSON")
		}

		// All other optional fields must be absent (they are omitempty).
		omitemptyFields := []string{"method", "trigger_port", "auth_ref", "contact_policy", "interface"}
		for _, k := range omitemptyFields {
			if _, ok := m[k]; ok {
				t.Errorf("omitempty field %q should not appear in sparse HTTPClientPort JSON", k)
			}
		}
	})
}

// TestPortDefinition_UnmarshalJSON_HTTPClientConfig is the load-bearing
// regression test for PortDefinition.UnmarshalJSON's http-client case.
// Mirrors TestPortDefinition_UnmarshalJSON_JetStreamConfig: a JSON-loaded
// PortDefinition with type "http-client" must place an HTTPClientPort struct
// into Config (not a map[string]any), otherwise every type assertion in
// BuildPortFromDefinition silently fails.
func TestPortDefinition_UnmarshalJSON_HTTPClientConfig(t *testing.T) {
	raw := []byte(`{
		"name": "cap_feed",
		"type": "http-client",
		"subject": "https://api.weather.gov/alerts/active",
		"config": {
			"method": "GET",
			"auth_ref": "opensky_basic",
			"trigger_port": "cap_timer"
		}
	}`)

	var pd PortDefinition
	if err := json.Unmarshal(raw, &pd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if pd.Name != "cap_feed" || pd.Type != "http-client" {
		t.Errorf("top-level fields lost: %+v", pd)
	}

	hc, ok := pd.Config.(HTTPClientPort)
	if !ok {
		t.Fatalf("Config = %T, want HTTPClientPort (http-client case in PortDefinition.UnmarshalJSON missing)", pd.Config)
	}
	if hc.Method != "GET" {
		t.Errorf("Method = %q, want GET", hc.Method)
	}
	if hc.AuthRef != "opensky_basic" {
		t.Errorf("AuthRef = %q, want opensky_basic", hc.AuthRef)
	}
	if hc.TriggerPort != "cap_timer" {
		t.Errorf("TriggerPort = %q, want cap_timer", hc.TriggerPort)
	}
}

// TestBuildPortFromDefinition_HTTPClient exercises BuildPortFromDefinition's
// http-client case:
//   - def.Subject is used as the default URLPattern when no typed Config is present.
//   - A typed def.Config.(HTTPClientPort) overrides non-empty fields including
//     URLPattern (typed Config wins over def.Subject).
func TestBuildPortFromDefinition_HTTPClient(t *testing.T) {
	t.Run("subject is default URLPattern", func(t *testing.T) {
		def := PortDefinition{
			Name:    "cap_feed",
			Type:    "http-client",
			Subject: "https://api.weather.gov/alerts/active",
		}
		port := BuildPortFromDefinition(def, DirectionInput)
		hc, ok := port.Config.(HTTPClientPort)
		if !ok {
			t.Fatalf("Config = %T, want HTTPClientPort", port.Config)
		}
		if hc.URLPattern != "https://api.weather.gov/alerts/active" {
			t.Errorf("URLPattern = %q, want def.Subject value", hc.URLPattern)
		}
	})

	t.Run("typed Config overrides subject for URLPattern", func(t *testing.T) {
		def := PortDefinition{
			Name:    "cap_feed",
			Type:    "http-client",
			Subject: "https://api.weather.gov/alerts/active", // scalar default
			Config: HTTPClientPort{
				URLPattern:  "https://api.weather.gov/alerts/active?area=US", // typed Config wins
				Method:      "GET",
				TriggerPort: "cap_timer",
				AuthRef:     "opensky_basic",
			},
		}
		port := BuildPortFromDefinition(def, DirectionInput)
		hc, ok := port.Config.(HTTPClientPort)
		if !ok {
			t.Fatalf("Config = %T, want HTTPClientPort", port.Config)
		}
		if hc.URLPattern != "https://api.weather.gov/alerts/active?area=US" {
			t.Errorf("URLPattern = %q, want typed Config value (Config wins over Subject)", hc.URLPattern)
		}
		if hc.Method != "GET" {
			t.Errorf("Method = %q, want GET", hc.Method)
		}
		if hc.TriggerPort != "cap_timer" {
			t.Errorf("TriggerPort = %q, want cap_timer", hc.TriggerPort)
		}
		if hc.AuthRef != "opensky_basic" {
			t.Errorf("AuthRef = %q, want opensky_basic", hc.AuthRef)
		}
	})

	t.Run("flat interface field is honored when no Config block", func(t *testing.T) {
		// Regression (go-reviewer C3-2): a config-less declaration that sets
		// only the flat `interface:` scalar must not silently lose its contract.
		def := PortDefinition{
			Name:      "cap_feed",
			Type:      "http-client",
			Subject:   "https://api.weather.gov/alerts/active",
			Interface: "alert.CAP",
		}
		port := BuildPortFromDefinition(def, DirectionInput)
		hc, ok := port.Config.(HTTPClientPort)
		if !ok {
			t.Fatalf("Config = %T, want HTTPClientPort", port.Config)
		}
		if hc.Interface == nil {
			t.Fatal("Interface = nil, want flat def.Interface to seed the contract")
		}
		if hc.Interface.Type != "alert.CAP" {
			t.Errorf("Interface.Type = %q, want alert.CAP", hc.Interface.Type)
		}
	})

	t.Run("typed Config.Interface wins over flat interface field", func(t *testing.T) {
		def := PortDefinition{
			Name:      "cap_feed",
			Type:      "http-client",
			Subject:   "https://api.weather.gov/alerts/active",
			Interface: "alert.CAP", // flat seed
			Config: HTTPClientPort{
				Interface: &InterfaceContract{Type: "alert.CAP.v2", Version: "v2"}, // typed wins
			},
		}
		port := BuildPortFromDefinition(def, DirectionInput)
		hc := port.Config.(HTTPClientPort)
		if hc.Interface == nil || hc.Interface.Type != "alert.CAP.v2" {
			t.Errorf("Interface = %+v, want typed Config.Interface (alert.CAP.v2) to win", hc.Interface)
		}
	})
}
