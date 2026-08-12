package config

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/types"
)

func TestMaxDeliveryEventsDeclarationIsFixedAndBounded(t *testing.T) {
	decls, err := resolveStreamDeclarations(&Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := declarationNamed(t, decls, "MAX_DELIVERY_EVENTS")
	want := StreamConfig{
		Subjects: []string{"$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"}, Storage: "file",
		MaxAge: "168h", MaxBytes: 64 * 1024 * 1024, Discard: StreamDiscardOld,
		Retention: "limits", Replicas: 1,
	}
	if !reflect.DeepEqual(got.cfg, want) {
		t.Fatalf("MAX_DELIVERY_EVENTS declaration = %+v, want %+v", got.cfg, want)
	}
}

func TestMaxDeliveryEventsOperatorCollisionFailsBeforeNATSAccess(t *testing.T) {
	for _, supplied := range []StreamConfig{
		maxDeliveryEventsStreamConfig,
		{Subjects: []string{"operator.>"}, MaxAge: "1h", MaxBytes: 1, Discard: StreamDiscardNew},
	} {
		cfg := &Config{Streams: StreamConfigs{"MAX_DELIVERY_EVENTS": supplied}}
		manager := NewStreamsManager(nil, nil)
		err := manager.EnsureStreams(context.Background(), cfg)
		if !errors.Is(err, errFixedFrameworkStreamCollision) {
			t.Fatalf("collision error = %v, want fixed-framework collision", err)
		}
	}
}

func TestMaxDeliveryEventsComponentCollisionIsRejected(t *testing.T) {
	raw := json.RawMessage(`{"ports":{"outputs":[{"name":"reserved","config":{"kind":"jetstream","stream_name":"MAX_DELIVERY_EVENTS","subjects":["application.>"]}}]}}`)
	cfg := &Config{Components: map[string]types.ComponentConfig{
		"publisher": {Type: types.ComponentTypeProcessor, Name: "publisher", Enabled: true, Config: raw},
	}}
	_, err := resolveStreamDeclarations(cfg, nil)
	if !errors.Is(err, errFixedFrameworkStreamCollision) {
		t.Fatalf("component collision error = %v, want fixed-framework collision", err)
	}
}

// TestExtractPortsFromConfig_JSONRoundTripPopulatesStreamName closes the
// shadow-struct gap surfaced 2026-05-08 (semspec). Stream extraction now
// decodes the canonical component-package definitions directly. Pre-fix, a
// parallel definition lacked the StreamName field, so a JSON config like
//
//	{"ports":{"outputs":[{
//	    "name":"tool.result", "config":{"kind":"jetstream",
//	    "subjects":["tool.result.*"], "stream_name":"AGENT"}}]}}
//
// silently dropped stream_name on unmarshal. EnsureStreams would then fall
// back to DeriveStreamName("tool.result.*") = "TOOL" and create a colliding
// TOOL stream. At the time, AGENT's broad "tool.>" coverage already captured
// the subject, so tool.result publishes never reached subscribers. Current
// shipped guidance uses the explicit "tool.execute.>" and "tool.result.>"
// families; the historical shadow-struct regression remains the test target.
//
// Test discipline (per feedback memory):
// "For every operator-configurable field, there must be a test that loads
// it from JSON (not Go-constructed) and asserts it reaches its consumer."
func TestExtractPortsFromConfig_JSONRoundTripPopulatesStreamName(t *testing.T) {
	tests := []struct {
		name           string
		rawConfig      string
		wantStreamName string
		wantSubjects   []string
	}{
		{
			name: "explicit stream_name preserved through JSON round-trip",
			rawConfig: `{
				"ports": {
					"outputs": [{
						"name": "tool.result",
						"config": {
							"kind": "jetstream",
							"subjects": ["tool.result.*"],
							"stream_name": "AGENT"
						}
					}]
				}
			}`,
			wantStreamName: "AGENT",
			wantSubjects:   []string{"tool.result.*"},
		},
		{
			name: "absent stream_name leaves field empty for derivation fallback",
			rawConfig: `{
				"ports": {
					"outputs": [{
						"name": "iot.sensor",
						"config": {
							"kind": "jetstream",
							"subjects": ["iot.sensor.>"]
						}
					}]
				}
			}`,
			wantStreamName: "",
			wantSubjects:   []string{"iot.sensor.>"},
		},
		{
			name: "operator override of stream_name preserved",
			rawConfig: `{
				"ports": {
					"outputs": [{
						"name": "agent.response",
						"config": {
							"kind": "jetstream",
							"subjects": ["custom.agent.response.*"],
							"stream_name": "CUSTOM_AGENT"
						}
					}]
				}
			}`,
			wantStreamName: "CUSTOM_AGENT",
			wantSubjects:   []string{"custom.agent.response.*"},
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
			port, err := ports.Outputs[0].Resolve(component.DirectionOutput)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := port.Facts()
			if err != nil {
				t.Fatal(err)
			}
			stream, ok := facts.Stream()
			if !ok || stream.Name() != tt.wantStreamName {
				t.Fatalf("stream name = %q, present=%v, want %q", stream.Name(), ok, tt.wantStreamName)
			}
			assertStringsEqual(t, stream.Subjects(), tt.wantSubjects)
		})
	}
}

func TestExtractPortsFromConfig_AbsentIsEmptyAndMalformedIsRejected(t *testing.T) {
	ports, err := extractPortsFromConfig(nil)
	if err != nil {
		t.Fatalf("absent component config: %v", err)
	}
	if len(ports.Inputs) != 0 || len(ports.Outputs) != 0 {
		t.Fatalf("absent component config produced ports: %#v", ports)
	}

	if _, err := extractPortsFromConfig(json.RawMessage(`{"ports":`)); err == nil {
		t.Fatal("malformed component config must be rejected")
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
		port         component.PortDefinition
		wantStream   string
		wantSubjects []string
	}{
		{
			name: "explicit stream_name wins over derived name",
			port: component.PortDefinition{
				Name: "tool.result",
				Config: component.JetStreamPort{
					StreamName: "AGENT", Subjects: []string{"tool.result.*"},
				},
			},
			wantStream:   "AGENT",
			wantSubjects: []string{"tool.result.*"},
		},
		{
			name: "no stream_name falls back to DeriveStreamName",
			port: component.PortDefinition{
				Name:   "iot.sensor",
				Config: component.JetStreamPort{Subjects: []string{"iot.sensor.>"}},
			},
			wantStream:   "IOT",
			wantSubjects: []string{"iot.sensor.>"},
		},
		{
			name: "explicit stream_name lowercased for subject pattern",
			port: component.PortDefinition{
				Name: "agent.response",
				Config: component.JetStreamPort{
					StreamName: "CUSTOM_AGENT", Subjects: []string{"agent.response.*"},
				},
			},
			wantStream:   "CUSTOM_AGENT",
			wantSubjects: []string{"agent.response.*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, err := tt.port.Resolve(component.DirectionOutput)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := port.Facts()
			if err != nil {
				t.Fatal(err)
			}
			stream, ok := facts.Stream()
			if !ok {
				t.Fatal("stream facts absent")
			}
			streamName, subjects, err := derivePortStream(stream)
			if err != nil {
				t.Fatal(err)
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

func TestPortDerivedStreamPreservesCanonicalOutputPolicy(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"ports": map[string]any{"outputs": []map[string]any{{
			"name": "events",
			"config": map[string]any{
				"kind": "jetstream", "stream_name": "EVENTS", "subjects": []string{"events.>"},
				"storage": "memory", "retention": "work_queue", "retention_days": 3,
				"max_size_gb": 2, "replicas": 3,
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := guardTestConfig()
	cfg.Components["publisher"] = types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "publisher", Enabled: true, Config: raw,
	}

	declarations, err := resolveStreamDeclarations(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	events := declarationNamed(t, declarations, "EVENTS")
	if events.cfg.Storage != "memory" || events.cfg.Retention != "workqueue" ||
		events.cfg.MaxAge != "3d" || events.cfg.MaxBytes != 2*1024*1024*1024 || events.cfg.Replicas != 3 {
		t.Fatalf("derived stream policy = %+v", events.cfg)
	}

	cfg.Streams["EVENTS"] = StreamConfig{
		Subjects: []string{"operator.>"}, Storage: "file", Retention: "interest",
		MaxAge: "12h", MaxBytes: 1024, Replicas: 1, Discard: StreamDiscardNew,
	}
	declarations, err = resolveStreamDeclarations(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	events = declarationNamed(t, declarations, "EVENTS")
	if !reflect.DeepEqual(events.cfg, cfg.Streams["EVENTS"]) {
		t.Fatalf("explicit stream declaration lost precedence: got %+v want %+v", events.cfg, cfg.Streams["EVENTS"])
	}
}

func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
