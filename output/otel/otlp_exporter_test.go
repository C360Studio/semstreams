package otel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// captureServer is a test HTTP server that records all received requests.
type captureServer struct {
	mu       sync.Mutex
	requests []capturedRequest
}

type capturedRequest struct {
	method      string
	path        string
	contentType string
	body        []byte
}

func newCaptureServer(t *testing.T) (*httptest.Server, *captureServer) {
	t.Helper()
	cs := &captureServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		cs.mu.Lock()
		cs.requests = append(cs.requests, capturedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		})
		cs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, cs
}

func (cs *captureServer) getRequests() []capturedRequest {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]capturedRequest, len(cs.requests))
	copy(out, cs.requests)
	return out
}

// ---- ExportSpans tests ----

func TestOTLPExporter_ExportSpans(t *testing.T) {
	srv, cs := newCaptureServer(t)
	exp := NewOTLPExporter(srv.URL, nil, nil)

	now := time.Now()
	spans := []*SpanData{
		{
			TraceID:   "aabbccdd00112233aabbccdd00112233",
			SpanID:    "0011223344556677",
			Name:      "agent.loop",
			Kind:      "server",
			StartTime: now,
			EndTime:   now.Add(5 * time.Second),
			Status:    SpanStatus{Code: "ok"},
			Attributes: map[string]any{
				"agent.loop_id": "loop-001",
				"agent.model":   "gpt-4o",
			},
		},
	}

	if err := exp.ExportSpans(context.Background(), spans); err != nil {
		t.Fatalf("ExportSpans failed: %v", err)
	}

	reqs := cs.getRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	req := reqs[0]
	if req.method != http.MethodPost {
		t.Errorf("expected POST, got %q", req.method)
	}
	if req.path != "/v1/traces" {
		t.Errorf("expected path /v1/traces, got %q", req.path)
	}
	if req.contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", req.contentType)
	}

	// Validate OTLP JSON structure
	var payload map[string]any
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	resourceSpans, ok := payload["resourceSpans"].([]any)
	if !ok || len(resourceSpans) == 0 {
		t.Fatal("expected resourceSpans array")
	}
	rs := resourceSpans[0].(map[string]any)
	scopeSpans, ok := rs["scopeSpans"].([]any)
	if !ok || len(scopeSpans) == 0 {
		t.Fatal("expected scopeSpans array")
	}
	ss := scopeSpans[0].(map[string]any)
	spansArr, ok := ss["spans"].([]any)
	if !ok || len(spansArr) != 1 {
		t.Fatalf("expected 1 span in scopeSpans, got %v", ss["spans"])
	}
	span := spansArr[0].(map[string]any)
	if span["traceId"] != "aabbccdd00112233aabbccdd00112233" {
		t.Errorf("unexpected traceId: %v", span["traceId"])
	}
	if span["spanId"] != "0011223344556677" {
		t.Errorf("unexpected spanId: %v", span["spanId"])
	}
	if span["name"] != "agent.loop" {
		t.Errorf("unexpected span name: %v", span["name"])
	}
	// kind SERVER = 2
	if span["kind"].(float64) != 2 {
		t.Errorf("expected kind 2 (SERVER), got %v", span["kind"])
	}
}

func TestOTLPExporter_ExportSpans_WithParentAndEvents(t *testing.T) {
	srv, cs := newCaptureServer(t)
	exp := NewOTLPExporter(srv.URL, nil, nil)

	now := time.Now()
	spans := []*SpanData{
		{
			TraceID:      "aabbccdd00112233aabbccdd00112233",
			SpanID:       "aabb112233445566",
			ParentSpanID: "0011223344556677",
			Name:         "agent.tool.code_search",
			Kind:         "client",
			StartTime:    now,
			EndTime:      now,
			Status:       SpanStatus{Code: "error", Message: "timeout"},
			Attributes: map[string]any{
				"tool.name":   "code_search",
				"tool.status": "error",
			},
			Events: []SpanEvent{
				{
					Name:      "retry",
					Timestamp: now,
					Attributes: map[string]any{
						"attempt": 2,
					},
				},
			},
		},
	}

	if err := exp.ExportSpans(context.Background(), spans); err != nil {
		t.Fatalf("ExportSpans failed: %v", err)
	}

	reqs := cs.getRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	var payload map[string]any
	if err := json.Unmarshal(reqs[0].body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rs := payload["resourceSpans"].([]any)[0].(map[string]any)
	ss := rs["scopeSpans"].([]any)[0].(map[string]any)
	span := ss["spans"].([]any)[0].(map[string]any)

	if span["parentSpanId"] != "0011223344556677" {
		t.Errorf("expected parentSpanId, got %v", span["parentSpanId"])
	}
	events, ok := span["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("expected 1 event, got %v", span["events"])
	}
	ev := events[0].(map[string]any)
	if ev["name"] != "retry" {
		t.Errorf("expected event name 'retry', got %v", ev["name"])
	}
}

func TestOTLPExporter_ExportSpans_Empty(t *testing.T) {
	srv, cs := newCaptureServer(t)
	exp := NewOTLPExporter(srv.URL, nil, nil)

	if err := exp.ExportSpans(context.Background(), nil); err != nil {
		t.Fatalf("ExportSpans with empty slice failed: %v", err)
	}
	// No HTTP request should have been sent
	if len(cs.getRequests()) != 0 {
		t.Error("expected no HTTP request for empty span slice")
	}
}

func TestOTLPExporter_ExportSpans_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	exp := NewOTLPExporter(srv.URL, nil, nil)
	err := exp.ExportSpans(context.Background(), []*SpanData{
		{
			TraceID: "aabb", SpanID: "ccdd",
			Name: "test", StartTime: time.Now(), EndTime: time.Now(),
		},
	})
	if err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

// ---- ExportMetrics tests ----

func TestOTLPExporter_ExportMetrics_Counter(t *testing.T) {
	srv, cs := newCaptureServer(t)
	exp := NewOTLPExporter(srv.URL, nil, nil)

	metrics := []*MetricData{
		{
			Name:        "agent.iterations_total",
			Description: "Total loop iterations",
			Unit:        "1",
			Type:        MetricTypeCounter,
			DataPoints: []DataPoint{
				{Timestamp: time.Now(), Value: 42.0},
			},
		},
	}

	if err := exp.ExportMetrics(context.Background(), metrics); err != nil {
		t.Fatalf("ExportMetrics failed: %v", err)
	}

	reqs := cs.getRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].path != "/v1/metrics" {
		t.Errorf("expected path /v1/metrics, got %q", reqs[0].path)
	}

	var payload map[string]any
	if err := json.Unmarshal(reqs[0].body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	rm := payload["resourceMetrics"].([]any)[0].(map[string]any)
	sm := rm["scopeMetrics"].([]any)[0].(map[string]any)
	metricsArr := sm["metrics"].([]any)
	if len(metricsArr) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metricsArr))
	}
	m := metricsArr[0].(map[string]any)
	if m["name"] != "agent.iterations_total" {
		t.Errorf("unexpected metric name: %v", m["name"])
	}
	// Counter should have sum field
	if _, ok := m["sum"]; !ok {
		t.Error("expected 'sum' field for counter metric")
	}
}

func TestOTLPExporter_ExportMetrics_Gauge(t *testing.T) {
	srv, cs := newCaptureServer(t)
	exp := NewOTLPExporter(srv.URL, nil, nil)

	metrics := []*MetricData{
		{
			Name: "agent.active_loops",
			Type: MetricTypeGauge,
			DataPoints: []DataPoint{
				{Timestamp: time.Now(), Value: 3.0},
			},
		},
	}

	if err := exp.ExportMetrics(context.Background(), metrics); err != nil {
		t.Fatalf("ExportMetrics failed: %v", err)
	}

	reqs := cs.getRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	var payload map[string]any
	_ = json.Unmarshal(reqs[0].body, &payload)
	rm := payload["resourceMetrics"].([]any)[0].(map[string]any)
	sm := rm["scopeMetrics"].([]any)[0].(map[string]any)
	m := sm["metrics"].([]any)[0].(map[string]any)

	if _, ok := m["gauge"]; !ok {
		t.Error("expected 'gauge' field for gauge metric")
	}
}

func TestOTLPExporter_ExportMetrics_Empty(t *testing.T) {
	srv, cs := newCaptureServer(t)
	exp := NewOTLPExporter(srv.URL, nil, nil)

	if err := exp.ExportMetrics(context.Background(), nil); err != nil {
		t.Fatalf("ExportMetrics with nil failed: %v", err)
	}
	if len(cs.getRequests()) != 0 {
		t.Error("expected no HTTP request for empty metric slice")
	}
}

func TestOTLPExporter_ExportMetrics_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	exp := NewOTLPExporter(srv.URL, nil, nil)
	err := exp.ExportMetrics(context.Background(), []*MetricData{
		{
			Name: "test", Type: MetricTypeGauge,
			DataPoints: []DataPoint{{Timestamp: time.Now(), Value: 1.0}},
		},
	})
	if err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

// ---- Custom headers ----

func TestOTLPExporter_CustomHeaders(t *testing.T) {
	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Auth-Token")
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp := NewOTLPExporter(srv.URL, map[string]string{
		"X-Auth-Token": "secret-token",
	}, nil)

	_ = exp.ExportSpans(context.Background(), []*SpanData{
		{TraceID: "aa", SpanID: "bb", Name: "test", StartTime: time.Now(), EndTime: time.Now()},
	})

	if capturedHeader != "secret-token" {
		t.Errorf("expected X-Auth-Token 'secret-token', got %q", capturedHeader)
	}
}

// ---- Shutdown ----

func TestOTLPExporter_Shutdown(t *testing.T) {
	exp := NewOTLPExporter("http://localhost:4318", nil, nil)
	if err := exp.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

// ---- Attribute type conversion ----

func TestToOTLPAttrs_TypeConversions(t *testing.T) {
	strVal := "hello"
	i64 := int64(42)
	f64 := float64(3.14)
	boolVal := true

	attrs := map[string]any{
		"str":   strVal,
		"int":   42,
		"int64": i64,
		"float": f64,
		"bool":  boolVal,
		"other": []string{"a", "b"},
	}
	kvs := toOTLPAttrs(attrs)
	if len(kvs) != len(attrs) {
		t.Errorf("expected %d kvs, got %d", len(attrs), len(kvs))
	}
	for _, kv := range kvs {
		switch kv.Key {
		case "str":
			if kv.Value.StringValue == nil || *kv.Value.StringValue != "hello" {
				t.Errorf("str: unexpected value %+v", kv.Value)
			}
		case "int":
			if kv.Value.IntValue == nil || *kv.Value.IntValue != 42 {
				t.Errorf("int: unexpected value %+v", kv.Value)
			}
		case "float":
			if kv.Value.DoubleValue == nil {
				t.Errorf("float: expected DoubleValue, got %+v", kv.Value)
			}
		case "bool":
			if kv.Value.BoolValue == nil || !*kv.Value.BoolValue {
				t.Errorf("bool: unexpected value %+v", kv.Value)
			}
		case "other":
			// Falls through to string representation
			if kv.Value.StringValue == nil {
				t.Errorf("other: expected StringValue fallback, got %+v", kv.Value)
			}
		}
	}
}

func TestMapKindToInt(t *testing.T) {
	cases := []struct {
		kind string
		want int
	}{
		{"internal", 1},
		{"server", 2},
		{"client", 3},
		{"producer", 4},
		{"consumer", 5},
		{"unknown", 1},
	}
	for _, tc := range cases {
		if got := mapKindToInt(tc.kind); got != tc.want {
			t.Errorf("mapKindToInt(%q) = %d, want %d", tc.kind, got, tc.want)
		}
	}
}

func TestMapStatusCode(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{"unset", 0},
		{"ok", 1},
		{"error", 2},
		{"", 0},
	}
	for _, tc := range cases {
		if got := mapStatusCode(tc.code); got != tc.want {
			t.Errorf("mapStatusCode(%q) = %d, want %d", tc.code, got, tc.want)
		}
	}
}
