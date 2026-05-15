// Package mock provides mock servers for E2E testing.
package mock

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// AGNTCYServer provides mock endpoints for AGNTCY integration testing.
// It handles:
// - Directory registration (/v1/agents/register, /v1/agents/heartbeat)
// - OTEL HTTP traces (/v1/traces)
// - Health checks (/health)
type AGNTCYServer struct {
	mux    *http.ServeMux
	server *http.Server

	// Registration tracking
	registrations   map[string]*AgentRegistration
	registrationsMu sync.RWMutex

	// OTEL trace tracking (POST counters + parsed-span aggregates)
	tracesReceived    int64
	tracesSpansTotal  int64
	tracesStatusOK    int64
	tracesStatusError int64
	lastTracePayload  []byte
	traceSpansMu      sync.RWMutex
	spanNames         map[string]int
	traceLoopIDs      map[string]struct{}
	parentSpanIDs     map[string]struct{}
	childSpanIDs      map[string]struct{}

	// OTEL metric tracking
	metricsReceived      int64
	metricsDataPointsTot int64
	metricsMu            sync.RWMutex
	metricNames          map[string]int

	// Stats
	requestCount int64
}

// AgentRegistration represents a registered agent. Field/JSON-tag
// shape matches directory-bridge's production wire (see
// output/directory-bridge/client.go RegistrationRequest). Pre-fix the
// mock used agent_id + string TTL, neither of which the bridge sends —
// every registration landed with empty AgentDID and dropped TTL, and
// the verify-directory-bridge e2e stage silently warned to timeout
// for the entire ADR-042 arc.
type AgentRegistration struct {
	AgentDID      string         `json:"agent_did"`
	OASFRecord    map[string]any `json:"oasf_record"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	RegisteredAt  time.Time      `json:"registered_at"`
	LastHeartbeat time.Time      `json:"last_heartbeat"`
	TTLSeconds    int            `json:"ttl_seconds"`
}

// NewAGNTCYServer creates a new mock AGNTCY server.
func NewAGNTCYServer() *AGNTCYServer {
	s := &AGNTCYServer{
		mux:           http.NewServeMux(),
		registrations: make(map[string]*AgentRegistration),
		spanNames:     make(map[string]int),
		traceLoopIDs:  make(map[string]struct{}),
		parentSpanIDs: make(map[string]struct{}),
		childSpanIDs:  make(map[string]struct{}),
		metricNames:   make(map[string]int),
	}
	s.setupRoutes()
	return s
}

func (s *AGNTCYServer) setupRoutes() {
	// Health endpoint
	s.mux.HandleFunc("/health", s.handleHealth)

	// Directory endpoints — paths mirror the production wire that
	// output/directory-bridge.DirectoryClient drives:
	//   POST   /v1/agents                              (register)
	//   POST   /v1/agents/{registration_id}/heartbeat
	//   DELETE /v1/agents/{registration_id}            (deregister)
	//   GET    /v1/agents                              (list, e2e-only)
	s.mux.HandleFunc("POST /v1/agents", s.handleRegister)
	s.mux.HandleFunc("POST /v1/agents/{id}/heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("DELETE /v1/agents/{id}", s.handleDeregister)
	s.mux.HandleFunc("GET /v1/agents", s.handleListAgents)

	// Stats endpoint for e2e assertions
	s.mux.HandleFunc("/stats", s.handleStats)

	// OTEL HTTP endpoints
	s.mux.HandleFunc("/v1/traces", s.handleOTELTraces)
	s.mux.HandleFunc("/v1/metrics", s.handleOTELMetrics)
}

// Start starts the server on the given address. Pass ":0" to bind an
// ephemeral port; the resolved address is available via Addr/URL.
func (s *AGNTCYServer) Start(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.server = &http.Server{
		Addr:    listener.Addr().String(),
		Handler: s.mux,
	}
	go func() {
		if err := s.server.Serve(listener); err != http.ErrServerClosed {
			log.Printf("AGNTCY mock server error: %v", err)
		}
	}()
	return nil
}

// Stop stops the server.
func (s *AGNTCYServer) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

// URL returns the server URL.
func (s *AGNTCYServer) URL() string {
	if s.server == nil {
		return ""
	}
	// server.Addr is "[::]:PORT" or "127.0.0.1:PORT" depending on bind.
	// Always prefix the host explicitly for the ephemeral-port case so
	// callers do not have to splice strings.
	if _, port, err := net.SplitHostPort(s.server.Addr); err == nil {
		return "http://127.0.0.1:" + port
	}
	return "http://" + s.server.Addr
}

// Health handlers
func (s *AGNTCYServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "healthy",
		"services": map[string]string{
			"directory": "healthy",
			"otel":      "healthy",
		},
	})
}

// Directory handlers
func (s *AGNTCYServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Payload matches output/directory-bridge.RegistrationRequest:
	// agent_did (DID string), oasf_record (record body), ttl as int
	// seconds, metadata (carries semstreams_entity_id for correlation
	// with the source agent entity).
	var req struct {
		AgentDID   string         `json:"agent_did"`
		OASFRecord map[string]any `json:"oasf_record"`
		TTL        int            `json:"ttl"`
		Metadata   map[string]any `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Generate a server-side registration_id so the mock matches the
	// production directory contract — directory-bridge expects
	// resp.RegistrationID + resp.ExpiresAt back to drive Refresh /
	// Withdraw. Pre-fix the mock returned agent_id only; bridge then
	// stored an empty RegistrationID and every subsequent heartbeat
	// 404'd silently.
	regID := "reg-" + req.AgentDID

	s.registrationsMu.Lock()
	s.registrations[regID] = &AgentRegistration{
		AgentDID:      req.AgentDID,
		OASFRecord:    req.OASFRecord,
		Metadata:      req.Metadata,
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
		TTLSeconds:    req.TTL,
	}
	s.registrationsMu.Unlock()

	log.Printf("[AGNTCY Mock] Agent registered: did=%s registration_id=%s", req.AgentDID, regID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":         true,
		"registration_id": regID,
		"expires_at":      time.Now().Add(time.Duration(req.TTL) * time.Second).Format(time.RFC3339),
	})
}

func (s *AGNTCYServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	// Registration ID comes from the path (mirrors production:
	// DirectoryClient.Heartbeat POSTs to /v1/agents/{id}/heartbeat).
	regID := r.PathValue("id")
	if regID == "" {
		http.Error(w, "registration_id required in path", http.StatusBadRequest)
		return
	}

	// Body is optional — DirectoryClient sends {registration_id, agent_did}
	// but the mock only needs the path-extracted ID. Drain to keep the
	// connection clean.
	_, _ = io.ReadAll(r.Body)

	s.registrationsMu.Lock()
	reg, ok := s.registrations[regID]
	if ok {
		reg.LastHeartbeat = time.Now()
	}
	s.registrationsMu.Unlock()

	if !ok {
		http.Error(w, "registration not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"expires_at": time.Now().Add(time.Duration(reg.TTLSeconds) * time.Second).Format(time.RFC3339),
	})
}

// handleDeregister removes a registration by ID — mirrors production
// DirectoryClient.Deregister which DELETEs /v1/agents/{registration_id}.
// Pre-fix the docker-stack mock had no DELETE route at all, so every
// Deregister call from the bridge 404'd silently.
func (s *AGNTCYServer) handleDeregister(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	regID := r.PathValue("id")
	if regID == "" {
		http.Error(w, "registration_id required in path", http.StatusBadRequest)
		return
	}

	s.registrationsMu.Lock()
	_, ok := s.registrations[regID]
	delete(s.registrations, regID)
	s.registrationsMu.Unlock()

	if !ok {
		// 404 is the appropriate response, but the bridge's
		// Deregister treats any non-2xx as an error. Many directories
		// return 204 for "gone already" — match that behaviour so
		// idempotent withdraws don't fail.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *AGNTCYServer) handleListAgents(w http.ResponseWriter, _ *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	s.registrationsMu.RLock()
	agents := make([]*AgentRegistration, 0, len(s.registrations))
	for _, reg := range s.registrations {
		agents = append(agents, reg)
	}
	s.registrationsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"agents": agents,
		"count":  len(agents),
	})
}

// otlpTracePayload mirrors the JSON wire shape emitted by output/otel.OTLPExporter.
// Only the fields the mock asserts on are decoded; unknown keys are ignored.
type otlpTracePayload struct {
	ResourceSpans []struct {
		ScopeSpans []struct {
			Spans []otlpSpan `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type otlpSpan struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Name         string          `json:"name"`
	Kind         int             `json:"kind"`
	Status       otlpStatus      `json:"status"`
	Attributes   []otlpAttribute `json:"attributes"`
}

type otlpStatus struct {
	Code int `json:"code"` // 0=Unset, 1=Ok, 2=Error
}

type otlpAttribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue *string  `json:"stringValue,omitempty"`
		IntValue    *int64   `json:"intValue,omitempty"`
		DoubleValue *float64 `json:"doubleValue,omitempty"`
		BoolValue   *bool    `json:"boolValue,omitempty"`
	} `json:"value"`
}

// otlpMetricPayload covers the subset of resourceMetrics this mock inspects.
type otlpMetricPayload struct {
	ResourceMetrics []struct {
		ScopeMetrics []struct {
			Metrics []struct {
				Name string `json:"name"`
				Sum  *struct {
					DataPoints []json.RawMessage `json:"dataPoints"`
				} `json:"sum,omitempty"`
				Gauge *struct {
					DataPoints []json.RawMessage `json:"dataPoints"`
				} `json:"gauge,omitempty"`
				Histogram *struct {
					DataPoints []json.RawMessage `json:"dataPoints"`
				} `json:"histogram,omitempty"`
				Summary *struct {
					DataPoints []json.RawMessage `json:"dataPoints"`
				} `json:"summary,omitempty"`
			} `json:"metrics"`
		} `json:"scopeMetrics"`
	} `json:"resourceMetrics"`
}

// OTEL handlers
func (s *AGNTCYServer) handleOTELTraces(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	atomic.AddInt64(&s.tracesReceived, 1)
	s.lastTracePayload = body

	if err := s.recordTracePayload(body); err != nil {
		// Decode failures are flagged but the response still succeeds so the
		// exporter does not retry; the e2e stage surfaces the parse error.
		log.Printf("[AGNTCY Mock] OTEL trace parse error: %v (raw=%d bytes)", err, len(body))
	} else {
		log.Printf("[AGNTCY Mock] Received OTEL traces: %d bytes, %d spans cumulative",
			len(body), atomic.LoadInt64(&s.tracesSpansTotal))
	}

	// Return OTLP HTTP response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"partialSuccess": map[string]any{},
	})
}

// recordTracePayload decodes a single OTLP trace POST and updates the
// structural aggregates the e2e harness inspects.
func (s *AGNTCYServer) recordTracePayload(body []byte) error {
	var payload otlpTracePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	s.traceSpansMu.Lock()
	defer s.traceSpansMu.Unlock()

	for _, rs := range payload.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				atomic.AddInt64(&s.tracesSpansTotal, 1)
				s.spanNames[span.Name]++

				switch span.Status.Code {
				case 1:
					atomic.AddInt64(&s.tracesStatusOK, 1)
				case 2:
					atomic.AddInt64(&s.tracesStatusError, 1)
				}

				if span.ParentSpanID != "" {
					s.parentSpanIDs[span.ParentSpanID] = struct{}{}
					s.childSpanIDs[span.SpanID] = struct{}{}
				}

				for _, attr := range span.Attributes {
					if attr.Key == "agent.loop_id" && attr.Value.StringValue != nil {
						s.traceLoopIDs[*attr.Value.StringValue] = struct{}{}
					}
				}
			}
		}
	}
	return nil
}

func (s *AGNTCYServer) handleOTELMetrics(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.requestCount, 1)

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	atomic.AddInt64(&s.metricsReceived, 1)

	if err := s.recordMetricPayload(body); err != nil {
		log.Printf("[AGNTCY Mock] OTEL metric parse error: %v (raw=%d bytes)", err, len(body))
	} else {
		log.Printf("[AGNTCY Mock] Received OTEL metrics: %d bytes, %d data points cumulative",
			len(body), atomic.LoadInt64(&s.metricsDataPointsTot))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"partialSuccess": map[string]any{},
	})
}

func (s *AGNTCYServer) recordMetricPayload(body []byte) error {
	var payload otlpMetricPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	for _, rm := range payload.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				s.metricNames[m.Name]++
				switch {
				case m.Sum != nil:
					atomic.AddInt64(&s.metricsDataPointsTot, int64(len(m.Sum.DataPoints)))
				case m.Gauge != nil:
					atomic.AddInt64(&s.metricsDataPointsTot, int64(len(m.Gauge.DataPoints)))
				case m.Histogram != nil:
					atomic.AddInt64(&s.metricsDataPointsTot, int64(len(m.Histogram.DataPoints)))
				case m.Summary != nil:
					atomic.AddInt64(&s.metricsDataPointsTot, int64(len(m.Summary.DataPoints)))
				}
			}
		}
	}
	return nil
}

// handleStats exposes server statistics as JSON for e2e test assertions.
func (s *AGNTCYServer) handleStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Stats())
}

// Stats returns server statistics. Both byte-level counters (preserved for
// backwards-compatibility with older e2e harnesses) and structural span/metric
// aggregates are exposed.
func (s *AGNTCYServer) Stats() map[string]any {
	s.registrationsMu.RLock()
	regCount := len(s.registrations)
	s.registrationsMu.RUnlock()

	s.traceSpansMu.RLock()
	spanNames := make([]string, 0, len(s.spanNames))
	for name := range s.spanNames {
		spanNames = append(spanNames, name)
	}
	loopIDs := make([]string, 0, len(s.traceLoopIDs))
	for id := range s.traceLoopIDs {
		loopIDs = append(loopIDs, id)
	}
	parentLinks := len(s.childSpanIDs)
	s.traceSpansMu.RUnlock()

	s.metricsMu.RLock()
	metricNames := make([]string, 0, len(s.metricNames))
	for name := range s.metricNames {
		metricNames = append(metricNames, name)
	}
	s.metricsMu.RUnlock()

	return map[string]any{
		"request_count":             atomic.LoadInt64(&s.requestCount),
		"registrations":             regCount,
		"traces_received":           atomic.LoadInt64(&s.tracesReceived),
		"traces_spans_total":        atomic.LoadInt64(&s.tracesSpansTotal),
		"traces_status_ok":          atomic.LoadInt64(&s.tracesStatusOK),
		"traces_status_error":       atomic.LoadInt64(&s.tracesStatusError),
		"traces_parent_child_links": parentLinks,
		"traces_span_names":         spanNames,
		"traces_loop_ids":           loopIDs,
		"metrics_received":          atomic.LoadInt64(&s.metricsReceived),
		"metrics_data_points_total": atomic.LoadInt64(&s.metricsDataPointsTot),
		"metrics_names":             metricNames,
	}
}

// GetRegistrations returns all agent registrations.
func (s *AGNTCYServer) GetRegistrations() map[string]*AgentRegistration {
	s.registrationsMu.RLock()
	defer s.registrationsMu.RUnlock()

	result := make(map[string]*AgentRegistration, len(s.registrations))
	for k, v := range s.registrations {
		result[k] = v
	}
	return result
}

// TracesReceived returns the number of trace exports received.
func (s *AGNTCYServer) TracesReceived() int64 {
	return atomic.LoadInt64(&s.tracesReceived)
}

// MetricsReceived returns the number of metric exports received.
func (s *AGNTCYServer) MetricsReceived() int64 {
	return atomic.LoadInt64(&s.metricsReceived)
}
