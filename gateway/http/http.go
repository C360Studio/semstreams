// Package http provides HTTP gateway implementation for SemStreams.
package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/gateway"
	"github.com/c360/semstreams/natsclient"
)

// httpGatewaySchema defines the configuration schema for HTTP gateway component
var httpGatewaySchema = component.GenerateConfigSchema(reflect.TypeOf(gateway.Config{}))

// getOrGenerateRequestID extracts request ID from headers or generates a new one
// for distributed tracing across HTTP gateway and NATS services
func getOrGenerateRequestID(r *http.Request) string {
	// Try to extract from incoming X-Request-ID header
	if reqID := r.Header.Get("X-Request-ID"); reqID != "" {
		return reqID
	}

	// Generate a new request ID using crypto/rand for uniqueness
	// Format: 16 hex characters (8 random bytes)
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Gateway implements the Gateway interface for HTTP protocol
type Gateway struct {
	name       string
	config     gateway.Config
	routes     []gateway.RouteMapping
	natsClient *natsclient.Client

	// Lifecycle state (atomic operations)
	running atomic.Bool

	// Protects metrics and lastActivity for concurrent reads
	mu        sync.RWMutex
	startTime time.Time

	// Metrics (atomic operations)
	requestsTotal   atomic.Uint64
	requestsSuccess atomic.Uint64
	requestsFailed  atomic.Uint64
	bytesReceived   atomic.Uint64 // Total bytes received in requests
	bytesSent       atomic.Uint64 // Total bytes sent in responses
	lastActivity    time.Time
}

// NewGateway creates a new HTTP gateway from configuration
func NewGateway(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config gateway.Config
	if err := component.SafeUnmarshal(rawConfig, &config); err != nil {
		return nil, errors.WrapInvalid(err, "Gateway", "NewGateway", "config unmarshal")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errors.WrapInvalid(err, "Gateway", "NewGateway", "config validation")
	}

	if deps.NATSClient == nil {
		return nil, errors.WrapFatal(errors.ErrMissingConfig, "Gateway", "NewGateway",
			"NATS client is required")
	}

	return &Gateway{
		name:       "http-gateway",
		config:     config,
		routes:     config.Routes,
		natsClient: deps.NATSClient,
	}, nil
}

// Initialize prepares the HTTP gateway
func (g *Gateway) Initialize() error {
	return nil
}

// Start begins the HTTP gateway operation
func (g *Gateway) Start(_ context.Context) error {
	if g.running.Load() {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "Gateway", "Start",
			"gateway already running")
	}

	g.mu.Lock()
	g.running.Store(true)
	g.startTime = time.Now()
	g.mu.Unlock()

	return nil
}

// Stop gracefully stops the HTTP gateway
func (g *Gateway) Stop(_ time.Duration) error {
	if !g.running.Load() {
		return nil
	}

	g.mu.Lock()
	g.running.Store(false)
	g.mu.Unlock()

	return nil
}

// RegisterHTTPHandlers registers gateway routes with the HTTP mux
func (g *Gateway) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	for _, route := range g.routes {
		path := prefix + strings.TrimPrefix(route.Path, "/")

		// Wrap handler to filter by HTTP method
		handler := g.createRouteHandler(route)
		mux.HandleFunc(path, handler)
	}
}

// createRouteHandler creates an HTTP handler for a route mapping
func (g *Gateway) createRouteHandler(route gateway.RouteMapping) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get or generate request ID for distributed tracing
		requestID := getOrGenerateRequestID(r)
		w.Header().Set("X-Request-ID", requestID)
		// TODO: Add request ID to context for structured logging when logger is available
		// ctx := context.WithValue(r.Context(), "request_id", requestID)

		// Record request
		g.requestsTotal.Add(1)
		g.mu.Lock()
		g.lastActivity = time.Now()
		g.mu.Unlock()

		// Check HTTP method
		if r.Method != route.Method {
			g.writeError(w, http.StatusMethodNotAllowed,
				fmt.Sprintf("method %s not allowed", r.Method))
			g.requestsFailed.Add(1)
			return
		}

		// Apply CORS if enabled
		if g.config.EnableCORS {
			g.applyCORS(w, r)
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		// Close body when done (must be before any error returns to prevent resource leak)
		defer r.Body.Close()

		// Read request body with size limit + 1 to detect if request exceeds limit
		bodyReader := io.LimitReader(r.Body, g.config.MaxRequestSize+1)
		requestBody, err := io.ReadAll(bodyReader)
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "failed to read request body")
			g.requestsFailed.Add(1)
			return
		}

		// Check if request exceeded size limit
		if int64(len(requestBody)) > g.config.MaxRequestSize {
			g.writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body exceeds maximum size of %d bytes", g.config.MaxRequestSize))
			g.requestsFailed.Add(1)
			return
		}

		// Track bytes received
		g.bytesReceived.Add(uint64(len(requestBody)))

		// Create context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), route.Timeout())
		defer cancel()

		// Send NATS request
		response, err := g.sendNATSRequest(ctx, route.NATSSubject, requestBody)
		if err != nil {
			statusCode := g.mapErrorToHTTPStatus(err)
			sanitizedMsg := g.sanitizeError(err)
			// TODO: Log full error details internally when logger is available
			// For now, error details are preserved in the error wrapping chain
			g.writeError(w, statusCode, sanitizedMsg)
			g.requestsFailed.Add(1)
			return
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(response); err != nil {
			// Can't write error response at this point
			g.requestsFailed.Add(1)
			return
		}

		// Track bytes sent
		g.bytesSent.Add(uint64(len(response)))
		g.requestsSuccess.Add(1)
	}
}

// sendNATSRequest sends a request to NATS and waits for a reply
func (g *Gateway) sendNATSRequest(ctx context.Context, subject string, data []byte) ([]byte, error) {
	nc := g.natsClient.GetConnection()
	if nc == nil {
		return nil, errors.WrapTransient(nil, "Gateway", "sendNATSRequest",
			"NATS connection not available")
	}

	// Determine timeout from context
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second) // Default timeout
	}
	timeout := time.Until(deadline)

	// Send request and wait for reply
	msg, err := nc.Request(subject, data, timeout)
	if err != nil {
		return nil, errors.WrapTransient(err, "Gateway", "sendNATSRequest",
			fmt.Sprintf("NATS request to %s failed", subject))
	}

	return msg.Data, nil
}

// applyCORS applies CORS headers to the response
func (g *Gateway) applyCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")

	// Check if origin is allowed
	allowed := false
	for _, allowedOrigin := range g.config.CORSOrigins {
		if allowedOrigin == "*" || allowedOrigin == origin {
			allowed = true
			break
		}
	}

	if allowed {
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")
	}
}

// mapErrorToHTTPStatus maps SemStreams errors to HTTP status codes
func (g *Gateway) mapErrorToHTTPStatus(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}

	if errors.IsInvalid(err) {
		return http.StatusBadRequest
	}
	if errors.IsTransient(err) {
		// Could be timeout, service unavailable, etc.
		if strings.Contains(err.Error(), "timeout") {
			return http.StatusGatewayTimeout
		}
		return http.StatusServiceUnavailable
	}
	if errors.IsFatal(err) {
		return http.StatusInternalServerError
	}

	// Check for specific error patterns
	errStr := err.Error()
	if strings.Contains(errStr, "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "permission") {
		return http.StatusForbidden
	}

	return http.StatusInternalServerError
}

// sanitizeError returns a safe error message for external clients
// Internal error details are logged but not exposed to prevent information disclosure
func (g *Gateway) sanitizeError(err error) string {
	if err == nil {
		return "internal server error"
	}

	// Never expose NATS subjects, internal service names, or detailed errors
	if errors.IsInvalid(err) {
		return "invalid request"
	}
	if errors.IsTransient(err) {
		if strings.Contains(err.Error(), "timeout") {
			return "request timeout"
		}
		return "service temporarily unavailable"
	}
	if errors.IsFatal(err) {
		return "internal server error"
	}

	// Check for specific safe error patterns
	errStr := err.Error()
	if strings.Contains(errStr, "not found") {
		return "resource not found"
	}
	if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "permission") {
		return "access denied"
	}

	return "internal server error"
}

// writeError writes an error response
func (g *Gateway) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error":  message,
		"status": statusCode,
	}

	data, _ := json.Marshal(response)
	w.Write(data)
}

// Component metadata implementation

// Meta returns component metadata
func (g *Gateway) Meta() component.Metadata {
	return component.Metadata{
		Name:        g.name,
		Type:        "gateway",
		Description: "HTTP gateway for NATS request/reply",
		Version:     "0.1.0",
	}
}

// InputPorts returns no input ports (gateway is request-driven)
func (g *Gateway) InputPorts() []component.Port {
	return []component.Port{}
}

// OutputPorts returns no output ports (gateway uses request/reply)
func (g *Gateway) OutputPorts() []component.Port {
	return []component.Port{}
}

// ConfigSchema returns the configuration schema
func (g *Gateway) ConfigSchema() component.ConfigSchema {
	return httpGatewaySchema
}

// Health returns the current health status
func (g *Gateway) Health() component.HealthStatus {
	g.mu.RLock()
	startTime := g.startTime
	g.mu.RUnlock()

	running := g.running.Load()
	healthy := running
	var lastError string

	// Check NATS connectivity if gateway is running
	if running {
		nc := g.natsClient.GetConnection()
		if nc == nil || !nc.IsConnected() {
			healthy = false
			lastError = "NATS connection unavailable"
		}
	}

	return component.HealthStatus{
		Healthy:    healthy,
		LastError:  lastError,
		LastCheck:  time.Now(),
		ErrorCount: int(g.requestsFailed.Load()),
		Uptime:     time.Since(startTime),
	}
}

// DataFlow returns current data flow metrics
func (g *Gateway) DataFlow() component.FlowMetrics {
	g.mu.RLock()
	startTime := g.startTime
	lastActivity := g.lastActivity
	g.mu.RUnlock()

	total := g.requestsTotal.Load()
	failed := g.requestsFailed.Load()
	bytesRx := g.bytesReceived.Load()
	bytesTx := g.bytesSent.Load()

	// Calculate error rate
	var errorRate float64
	if total > 0 {
		errorRate = float64(failed) / float64(total)
	}

	// Calculate throughput rates (average since startup)
	// TODO: Implement sliding window (e.g., last 60 seconds) for more accurate current rates
	var messagesPerSecond, bytesPerSecond float64
	uptime := time.Since(startTime).Seconds()
	if uptime > 0 {
		messagesPerSecond = float64(total) / uptime
		totalBytes := bytesRx + bytesTx
		bytesPerSecond = float64(totalBytes) / uptime
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSecond,
		BytesPerSecond:    bytesPerSecond,
		ErrorRate:         errorRate,
		LastActivity:      lastActivity,
	}
}

// Register registers the HTTP gateway with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "http",
		Factory:     NewGateway,
		Schema:      httpGatewaySchema,
		Type:        "gateway",
		Protocol:    "http",
		Domain:      "network",
		Description: "HTTP gateway for bidirectional NATS request/reply",
		Version:     "0.1.0",
	})
}
