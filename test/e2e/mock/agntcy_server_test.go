package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"

	directorybridge "github.com/c360studio/semstreams/output/directory-bridge"
	otel "github.com/c360studio/semstreams/output/otel"
	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
)

// TestAGNTCYServer_DirectoryRoundTrip drives the production
// DirectoryClient against the mock to confirm the wire shape matches
// end-to-end: Register → Heartbeat → Deregister all succeed, and the
// stored AgentRegistration carries the agent_did, oasf_record, and
// metadata.semstreams_entity_id that the bridge sends. Pre-fix the
// mock expected agent_id + string TTL + a flat /heartbeat route, none
// of which the bridge speaks; every registration silently landed with
// empty fields and heartbeats / deregisters 404'd.
func TestAGNTCYServer_DirectoryRoundTrip(t *testing.T) {
	server := NewAGNTCYServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer server.Stop()

	client := directorybridge.NewDirectoryClient(server.URL())
	ctx := context.Background()

	const (
		agentDID = "did:semstreams:test-roundtrip"
		entityID = "acme.ops.agentic.system.agent.roundtrip"
	)
	regResp, err := client.Register(ctx, &directorybridge.RegistrationRequest{
		AgentDID: agentDID,
		OASFRecord: &oasfgenerator.OASFRecord{
			Name:          "roundtrip-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0.0",
		},
		TTL: 300,
		Metadata: map[string]any{
			"semstreams_entity_id": entityID,
			"source":               "semstreams",
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !regResp.Success || regResp.RegistrationID == "" {
		t.Fatalf("Register response = %+v, want Success + non-empty RegistrationID", regResp)
	}

	// Mock should have stored the DID + metadata in its registration map.
	regs := server.GetRegistrations()
	stored, ok := regs[regResp.RegistrationID]
	if !ok {
		t.Fatalf("mock did not store registration_id %q (have %d entries)", regResp.RegistrationID, len(regs))
	}
	if stored.AgentDID != agentDID {
		t.Errorf("stored AgentDID = %q, want %q", stored.AgentDID, agentDID)
	}
	if got, _ := stored.Metadata["semstreams_entity_id"].(string); got != entityID {
		t.Errorf("stored Metadata[semstreams_entity_id] = %q, want %q", got, entityID)
	}
	if stored.TTLSeconds != 300 {
		t.Errorf("stored TTLSeconds = %d, want 300", stored.TTLSeconds)
	}

	// Heartbeat should hit /v1/agents/{id}/heartbeat and refresh LastHeartbeat.
	before := stored.LastHeartbeat
	time.Sleep(5 * time.Millisecond)
	hbResp, err := client.Heartbeat(ctx, &directorybridge.HeartbeatRequest{
		RegistrationID: regResp.RegistrationID,
		AgentDID:       agentDID,
	})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !hbResp.Success {
		t.Errorf("Heartbeat Success = false: %s", hbResp.Error)
	}
	stored = server.GetRegistrations()[regResp.RegistrationID]
	if !stored.LastHeartbeat.After(before) {
		t.Errorf("LastHeartbeat not advanced: before=%v after=%v", before, stored.LastHeartbeat)
	}

	// Deregister should remove the entry.
	if err := client.Deregister(ctx, &directorybridge.DeregistrationRequest{
		RegistrationID: regResp.RegistrationID,
		AgentDID:       agentDID,
	}); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if _, stillThere := server.GetRegistrations()[regResp.RegistrationID]; stillThere {
		t.Error("registration still present after Deregister")
	}
}

// TestAGNTCYServer_OTELTracesStructuralParse drives an end-to-end loop:
// build a SpanData (the same shape semstreams's span collector produces) →
// send it through the production OTLPExporter → assert the mock's parsed
// aggregates pick up name, status, parent/child link, and the agent.loop_id
// attribute. If the exporter's JSON wire format ever drifts, this fails before
// the full e2e stack does.
func TestAGNTCYServer_OTELTracesStructuralParse(t *testing.T) {
	server := NewAGNTCYServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer server.Stop()

	// Use the production exporter so we never hand-craft OTLP JSON in the test;
	// any wire-format drift in the exporter is what we want this to catch.
	exporter := otel.NewOTLPExporter(server.URL(), true, nil, slog.Default())
	defer exporter.Shutdown(t.Context())

	start := time.Now()
	end := start.Add(50 * time.Millisecond)

	spans := []*otel.SpanData{
		{
			TraceID:   "trace-0001",
			SpanID:    "span-loop-1",
			Name:      "agent.loop",
			Kind:      "server",
			StartTime: start,
			EndTime:   end,
			Status:    otel.SpanStatus{Code: "ok"},
			Attributes: map[string]any{
				"agent.loop_id": "loop-abc-123",
				"agent.role":    "general",
				"service.name":  "semstreams",
			},
		},
		{
			TraceID:      "trace-0001",
			SpanID:       "span-tool-1",
			ParentSpanID: "span-loop-1",
			Name:         "agent.tool.read_loop_result",
			Kind:         "internal",
			StartTime:    start,
			EndTime:      end,
			Status:       otel.SpanStatus{Code: "error"},
			Attributes: map[string]any{
				"agent.loop_id": "loop-abc-123",
				"agent.tool":    "read_loop_result",
				"agent.error":   "kv miss",
			},
		},
	}

	if err := exporter.ExportSpans(t.Context(), spans); err != nil {
		t.Fatalf("export spans: %v", err)
	}

	stats := server.Stats()

	if got := stats["traces_received"].(int64); got != 1 {
		t.Errorf("traces_received = %d, want 1", got)
	}
	if got := stats["traces_spans_total"].(int64); got != 2 {
		t.Errorf("traces_spans_total = %d, want 2", got)
	}
	if got := stats["traces_status_ok"].(int64); got != 1 {
		t.Errorf("traces_status_ok = %d, want 1", got)
	}
	if got := stats["traces_status_error"].(int64); got != 1 {
		t.Errorf("traces_status_error = %d, want 1", got)
	}
	if got := stats["traces_parent_child_links"].(int); got != 1 {
		t.Errorf("traces_parent_child_links = %d, want 1 (tool span → loop span)", got)
	}

	names := stats["traces_span_names"].([]string)
	if !slices.Contains(names, "agent.loop") {
		t.Errorf("span names missing agent.loop: %v", names)
	}
	if !slices.Contains(names, "agent.tool.read_loop_result") {
		t.Errorf("span names missing agent.tool.*: %v", names)
	}

	loopIDs := stats["traces_loop_ids"].([]string)
	if !slices.Contains(loopIDs, "loop-abc-123") {
		t.Errorf("loop ids missing injected value: %v", loopIDs)
	}
}

// TestAGNTCYServer_OTELMetricsStructuralParse validates the parallel metric
// path: counter and histogram payloads emitted by the production exporter
// surface in metrics_data_points_total and metrics_names.
func TestAGNTCYServer_OTELMetricsStructuralParse(t *testing.T) {
	server := NewAGNTCYServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer server.Stop()

	exporter := otel.NewOTLPExporter(server.URL(), true, nil, slog.Default())
	defer exporter.Shutdown(t.Context())

	metrics := []*otel.MetricData{
		{
			Name: "semstreams.agentic.loop.loops_completed",
			Type: otel.MetricTypeCounter,
			DataPoints: []otel.DataPoint{
				{Value: 3, Attributes: map[string]any{"role": "general"}},
			},
		},
		{
			Name: "semstreams.agentic.loop.iteration_duration",
			Type: otel.MetricTypeHistogram,
			DataPoints: []otel.DataPoint{
				{Sum: 1.5, Count: 4, Attributes: map[string]any{"role": "general"}},
				{Sum: 0.3, Count: 1, Attributes: map[string]any{"role": "architect"}},
			},
		},
	}

	if err := exporter.ExportMetrics(t.Context(), metrics); err != nil {
		t.Fatalf("export metrics: %v", err)
	}

	stats := server.Stats()

	if got := stats["metrics_received"].(int64); got != 1 {
		t.Errorf("metrics_received = %d, want 1", got)
	}
	if got := stats["metrics_data_points_total"].(int64); got != 3 {
		t.Errorf("metrics_data_points_total = %d, want 3 (1 counter + 2 histogram)", got)
	}

	names := stats["metrics_names"].([]string)
	if !slices.Contains(names, "semstreams.agentic.loop.loops_completed") {
		t.Errorf("metric names missing counter: %v", names)
	}
	if !slices.Contains(names, "semstreams.agentic.loop.iteration_duration") {
		t.Errorf("metric names missing histogram: %v", names)
	}
}

// TestAGNTCYServer_OTELMalformedPayloadDoesNotPanic ensures bad payloads are
// logged but don't 5xx — the exporter retries on non-2xx, and we want the mock
// to soak up garbage during fuzz / partial-write scenarios without breaking
// the surrounding e2e run.
func TestAGNTCYServer_OTELMalformedPayloadDoesNotPanic(t *testing.T) {
	server := NewAGNTCYServer()
	if err := server.Start(":0"); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer server.Stop()

	resp, err := http.Post(server.URL()+"/v1/traces", "application/json",
		bytes.NewReader([]byte(`{"resourceSpans": "not-an-array"}`)))
	if err != nil {
		t.Fatalf("post malformed traces: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("malformed payload returned %d, want 200 (exporter retries on non-2xx)", resp.StatusCode)
	}

	stats := server.Stats()
	if got := stats["traces_received"].(int64); got != 1 {
		t.Errorf("traces_received = %d, want 1 (POST count is bytes-level, independent of parse)", got)
	}
	if got := stats["traces_spans_total"].(int64); got != 0 {
		t.Errorf("traces_spans_total = %d, want 0 (parse rejected the body)", got)
	}

	// Sanity: stats response still serializable.
	if _, err := json.Marshal(stats); err != nil {
		t.Errorf("stats marshal: %v", err)
	}
}
