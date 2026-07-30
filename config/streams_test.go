package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExtractPortsFromConfig_JSONRoundTripPopulatesStreamName closes the
// shadow-struct gap surfaced 2026-05-08 (semspec). PortsConfig and
// PortDefinition are now type aliases for the canonical component-package
// types (after the cycle break that promoted PlatformConfig to
// pkg/platform). Pre-fix, a parallel shadow PortDefinition lacked the
// StreamName field, so a JSON config like
//
//	{"ports":{"outputs":[{
//	    "name":"tool.result", "type":"jetstream",
//	    "subject":"tool.result.*", "stream_name":"AGENT"}]}}
//
// silently dropped stream_name on unmarshal. EnsureStreams would then fall
// back to DeriveStreamName("tool.result.*") = "TOOL", create a colliding
// TOOL stream, and tool.result publishes never reached subscribers
// because AGENT's "tool.>" capture already covered the subject.
//
// Test discipline (per feedback memory):
// "For every operator-configurable field, there must be a test that loads
// it from JSON (not Go-constructed) and asserts it reaches its consumer."
func TestExtractPortsFromConfig_JSONRoundTripPopulatesStreamName(t *testing.T) {
	tests := []struct {
		name           string
		rawConfig      string
		wantStreamName string
		wantSubject    string
		wantType       string
	}{
		{
			name: "explicit stream_name preserved through JSON round-trip",
			rawConfig: `{
				"ports": {
					"outputs": [{
						"name": "tool.result",
						"type": "jetstream",
						"subject": "tool.result.*",
						"stream_name": "AGENT"
					}]
				}
			}`,
			wantStreamName: "AGENT",
			wantSubject:    "tool.result.*",
			wantType:       "jetstream",
		},
		{
			name: "absent stream_name leaves field empty for derivation fallback",
			rawConfig: `{
				"ports": {
					"outputs": [{
						"name": "iot.sensor",
						"type": "jetstream",
						"subject": "iot.sensor.>"
					}]
				}
			}`,
			wantStreamName: "",
			wantSubject:    "iot.sensor.>",
			wantType:       "jetstream",
		},
		{
			name: "operator override of stream_name preserved",
			rawConfig: `{
				"ports": {
					"outputs": [{
						"name": "agent.response",
						"type": "jetstream",
						"subject": "custom.agent.response.*",
						"stream_name": "CUSTOM_AGENT"
					}]
				}
			}`,
			wantStreamName: "CUSTOM_AGENT",
			wantSubject:    "custom.agent.response.*",
			wantType:       "jetstream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports, err := extractPortsFromConfig(json.RawMessage(tt.rawConfig))
			if err != nil {
				t.Fatalf("extractPortsFromConfig: %v", err)
			}
			if len(ports.Outputs) != 1 {
				t.Fatalf("want 1 output port, got %d", len(ports.Outputs))
			}
			got := ports.Outputs[0]
			if got.StreamName != tt.wantStreamName {
				t.Errorf("StreamName: got %q, want %q", got.StreamName, tt.wantStreamName)
			}
			if got.Subject != tt.wantSubject {
				t.Errorf("Subject: got %q, want %q", got.Subject, tt.wantSubject)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type: got %q, want %q", got.Type, tt.wantType)
			}
		})
	}
}

// TestStreamDerivation_HonorsExplicitStreamName documents the EnsureStreams
// derivation contract: when a port carries an explicit StreamName, that
// name wins over DeriveStreamName(subject). This isolates the loop logic
// from the NATS-bound EnsureStreams path so the contract can be asserted
// without testcontainers.
func TestStreamDerivation_HonorsExplicitStreamName(t *testing.T) {
	tests := []struct {
		name         string
		port         PortDefinition
		wantStream   string
		wantSubjects []string
	}{
		{
			name: "explicit stream_name wins over derived name",
			port: PortDefinition{
				Name:       "tool.result",
				Type:       "jetstream",
				Subject:    "tool.result.*",
				StreamName: "AGENT",
			},
			wantStream:   "AGENT",
			wantSubjects: []string{"agent.>"},
		},
		{
			name: "no stream_name falls back to DeriveStreamName",
			port: PortDefinition{
				Name:    "iot.sensor",
				Type:    "jetstream",
				Subject: "iot.sensor.>",
			},
			wantStream:   "IOT",
			wantSubjects: []string{"iot.>"},
		},
		{
			name: "explicit stream_name lowercased for subject pattern",
			port: PortDefinition{
				Name:       "agent.response",
				Type:       "jetstream",
				Subject:    "agent.response.*",
				StreamName: "CUSTOM_AGENT",
			},
			wantStream:   "CUSTOM_AGENT",
			wantSubjects: []string{"custom_agent.>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamName := tt.port.StreamName
			subjects := []string{strings.ToLower(streamName) + ".>"}
			if streamName == "" {
				streamName = DeriveStreamName(tt.port.Subject)
				subjects = DeriveStreamSubjects(tt.port.Subject)
			}
			if streamName != tt.wantStream {
				t.Errorf("streamName: got %q, want %q", streamName, tt.wantStream)
			}
			if len(subjects) != len(tt.wantSubjects) {
				t.Fatalf("subjects len: got %d, want %d", len(subjects), len(tt.wantSubjects))
			}
			for i := range subjects {
				if subjects[i] != tt.wantSubjects[i] {
					t.Errorf("subjects[%d]: got %q, want %q", i, subjects[i], tt.wantSubjects[i])
				}
			}
		})
	}
}
