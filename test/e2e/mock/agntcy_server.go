// Package mock provides mock servers for E2E testing.
package mock

import (
	"encoding/json"
	"io"
	"log"
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

	// OTEL tracking
	tracesReceived   int64
	metricsReceived  int64
	lastTracePayload []byte

	// Stats
	requestCount int64
}

// AgentRegistration represents a registered agent.
type AgentRegistration struct {
	AgentID       string         `json:"agent_id"`
	OASFRecord    map[string]any `json:"oasf_record"`
	RegisteredAt  time.Time      `json:"registered_at"`
	LastHeartbeat time.Time      `json:"last_heartbeat"`
	TTL           string         `json:"ttl"`
}

// NewAGNTCYServer creates a new mock AGNTCY server.
func NewAGNTCYServer() *AGNTCYServer {
	s := &AGNTCYServer{
		mux:           http.NewServeMux(),
		registrations: make(map[string]*AgentRegistration),
	}
	s.setupRoutes()
	return s
}

func (s *AGNTCYServer) setupRoutes() {
	// Health endpoint
	s.mux.HandleFunc("/health", s.handleHealth)

	// Directory endpoints
	s.mux.HandleFunc("/v1/agents/register", s.handleRegister)
	s.mux.HandleFunc("/v1/agents/heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("/v1/agents", s.handleListAgents)

	// Stats endpoint for e2e assertions
	s.mux.HandleFunc("/stats", s.handleStats)

	// OTEL HTTP endpoints
	s.mux.HandleFunc("/v1/traces", s.handleOTELTraces)
	s.mux.HandleFunc("/v1/metrics", s.handleOTELMetrics)
}

// Start starts the server on the given address.
func (s *AGNTCYServer) Start(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("AGNTCY mock server error: %v", err)
		}
	}()
	// Give server time to start
	time.Sleep(50 * time.Millisecond)
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
	return "http://localhost" + s.server.Addr
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

	var req struct {
		AgentID    string         `json:"agent_id"`
		OASFRecord map[string]any `json:"oasf_record"`
		TTL        string         `json:"ttl"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	s.registrationsMu.Lock()
	s.registrations[req.AgentID] = &AgentRegistration{
		AgentID:       req.AgentID,
		OASFRecord:    req.OASFRecord,
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
		TTL:           req.TTL,
	}
	s.registrationsMu.Unlock()

	log.Printf("[AGNTCY Mock] Agent registered: %s", req.AgentID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":     "registered",
		"agent_id":   req.AgentID,
		"expires_at": time.Now().Add(5 * time.Minute).Format(time.RFC3339),
	})
}

func (s *AGNTCYServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	s.registrationsMu.Lock()
	if reg, ok := s.registrations[req.AgentID]; ok {
		reg.LastHeartbeat = time.Now()
	}
	s.registrationsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
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

	log.Printf("[AGNTCY Mock] Received OTEL traces: %d bytes", len(body))

	// Return OTLP HTTP response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"partialSuccess": map[string]any{},
	})
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

	log.Printf("[AGNTCY Mock] Received OTEL metrics: %d bytes", len(body))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"partialSuccess": map[string]any{},
	})
}

// handleStats exposes server statistics as JSON for e2e test assertions.
func (s *AGNTCYServer) handleStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Stats())
}

// Stats returns server statistics.
func (s *AGNTCYServer) Stats() map[string]any {
	s.registrationsMu.RLock()
	regCount := len(s.registrations)
	s.registrationsMu.RUnlock()

	return map[string]any{
		"request_count":    atomic.LoadInt64(&s.requestCount),
		"registrations":    regCount,
		"traces_received":  atomic.LoadInt64(&s.tracesReceived),
		"metrics_received": atomic.LoadInt64(&s.metricsReceived),
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
