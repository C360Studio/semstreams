package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pkgerrors "github.com/c360/semstreams/errors"
)

func TestGetOrGenerateRequestID(t *testing.T) {
	tests := []struct {
		name          string
		headerValue   string
		shouldExtract bool
	}{
		{
			name:          "extract existing request ID",
			headerValue:   "existing-request-id-12345",
			shouldExtract: true,
		},
		{
			name:          "generate new request ID when header missing",
			headerValue:   "",
			shouldExtract: false,
		},
		{
			name:          "extract UUID-style request ID",
			headerValue:   "550e8400-e29b-41d4-a716-446655440000",
			shouldExtract: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.headerValue != "" {
				req.Header.Set("X-Request-ID", tt.headerValue)
			}

			requestID := getOrGenerateRequestID(req)

			if tt.shouldExtract {
				if requestID != tt.headerValue {
					t.Errorf("expected to extract %q, got %q", tt.headerValue, requestID)
				}
			} else {
				if requestID == "" {
					t.Error("expected generated request ID, got empty string")
				}
				if len(requestID) == 0 {
					t.Error("generated request ID should not be empty")
				}
			}
		})
	}
}

func TestGetOrGenerateRequestID_Uniqueness(t *testing.T) {
	// Generate multiple request IDs and verify they're unique
	req := httptest.NewRequest("GET", "/test", nil)

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := getOrGenerateRequestID(req)
		if ids[id] {
			t.Errorf("generated duplicate request ID: %s", id)
		}
		ids[id] = true
	}
}

func TestMapErrorToHTTPStatus(t *testing.T) {
	g := &HTTPGateway{}

	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			name:           "invalid error maps to 400",
			err:            pkgerrors.WrapInvalid(pkgerrors.ErrInvalidConfig, "test", "test", "invalid input"),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "timeout error maps to 504",
			err:            pkgerrors.WrapTransient(pkgerrors.ErrConnectionTimeout, "test", "test", "timeout occurred"),
			expectedStatus: http.StatusGatewayTimeout,
		},
		{
			name:           "transient error maps to 503",
			err:            pkgerrors.WrapTransient(pkgerrors.ErrNoConnection, "test", "test", "service unavailable"),
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "fatal error maps to 500",
			err:            pkgerrors.WrapFatal(pkgerrors.ErrDataCorrupted, "test", "test", "fatal error"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "not found error maps to 404",
			err:            fmt.Errorf("entity not found"),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "unauthorized error maps to 403",
			err:            fmt.Errorf("unauthorized access"),
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "permission error maps to 403",
			err:            fmt.Errorf("permission denied"),
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := g.mapErrorToHTTPStatus(tt.err)
			if status != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, status)
			}
		})
	}
}

func TestSanitizeError(t *testing.T) {
	g := &HTTPGateway{}

	tests := []struct {
		name              string
		err               error
		expectedMsg       string
		shouldNotContain  []string
	}{
		{
			name:             "invalid error sanitized",
			err:              pkgerrors.WrapInvalid(pkgerrors.ErrInvalidConfig, "HTTPGateway", "sendNATSRequest", "invalid request to graph.query.semantic"),
			expectedMsg:      "invalid request",
			shouldNotContain: []string{"graph.query", "NATS", "semantic"},
		},
		{
			name:             "timeout error sanitized",
			err:              pkgerrors.WrapTransient(pkgerrors.ErrConnectionTimeout, "HTTPGateway", "sendNATSRequest", "timeout waiting for NATS subject graph.query"),
			expectedMsg:      "request timeout",
			shouldNotContain: []string{"NATS", "graph.query", "subject"},
		},
		{
			name:             "transient error sanitized",
			err:              pkgerrors.WrapTransient(pkgerrors.ErrNoConnection, "HTTPGateway", "sendNATSRequest", "NATS connection failed"),
			expectedMsg:      "service temporarily unavailable",
			shouldNotContain: []string{"NATS", "connection"},
		},
		{
			name:             "fatal error sanitized",
			err:              pkgerrors.WrapFatal(pkgerrors.ErrDataCorrupted, "HTTPGateway", "method", "internal panic in processor component"),
			expectedMsg:      "internal server error",
			shouldNotContain: []string{"panic", "processor", "component"},
		},
		{
			name:             "not found error sanitized",
			err:              fmt.Errorf("entity not found in database"),
			expectedMsg:      "resource not found",
			shouldNotContain: []string{"entity", "database"},
		},
		{
			name:             "permission error sanitized",
			err:              fmt.Errorf("permission denied for user admin"),
			expectedMsg:      "access denied",
			shouldNotContain: []string{"user", "admin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := g.sanitizeError(tt.err)

			if sanitized != tt.expectedMsg {
				t.Errorf("expected %q, got %q", tt.expectedMsg, sanitized)
			}

			for _, forbidden := range tt.shouldNotContain {
				if strings.Contains(sanitized, forbidden) {
					t.Errorf("sanitized message should not contain %q, but got %q", forbidden, sanitized)
				}
			}
		})
	}
}

func TestApplyCORS(t *testing.T) {
	tests := []struct {
		name                string
		allowedOrigins      []string
		requestOrigin       string
		expectCORSHeaders   bool
		expectedOriginHeader string
	}{
		{
			name:                "exact origin match",
			allowedOrigins:      []string{"https://example.com"},
			requestOrigin:       "https://example.com",
			expectCORSHeaders:   true,
			expectedOriginHeader: "https://example.com",
		},
		{
			name:                "wildcard allows any origin",
			allowedOrigins:      []string{"*"},
			requestOrigin:       "https://example.com",
			expectCORSHeaders:   true,
			expectedOriginHeader: "https://example.com",
		},
		{
			name:                "wildcard without origin header",
			allowedOrigins:      []string{"*"},
			requestOrigin:       "",
			expectCORSHeaders:   true,
			expectedOriginHeader: "*",
		},
		{
			name:                "origin not in allowed list",
			allowedOrigins:      []string{"https://allowed.com"},
			requestOrigin:       "https://notallowed.com",
			expectCORSHeaders:   false,
			expectedOriginHeader: "",
		},
		{
			name:                "multiple allowed origins - first match",
			allowedOrigins:      []string{"https://app1.com", "https://app2.com"},
			requestOrigin:       "https://app1.com",
			expectCORSHeaders:   true,
			expectedOriginHeader: "https://app1.com",
		},
		{
			name:                "multiple allowed origins - second match",
			allowedOrigins:      []string{"https://app1.com", "https://app2.com"},
			requestOrigin:       "https://app2.com",
			expectCORSHeaders:   true,
			expectedOriginHeader: "https://app2.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Need to use actual Config type from gateway package
			// For now, create HTTPGateway with minimal config
			g := &HTTPGateway{}
			g.config.EnableCORS = true
			g.config.CORSOrigins = tt.allowedOrigins

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}

			w := httptest.NewRecorder()
			g.applyCORS(w, req)

			originHeader := w.Header().Get("Access-Control-Allow-Origin")

			if tt.expectCORSHeaders {
				if originHeader != tt.expectedOriginHeader {
					t.Errorf("expected Origin header %q, got %q", tt.expectedOriginHeader, originHeader)
				}

				if w.Header().Get("Access-Control-Allow-Methods") == "" {
					t.Error("expected Access-Control-Allow-Methods header to be set")
				}

				if w.Header().Get("Access-Control-Allow-Headers") == "" {
					t.Error("expected Access-Control-Allow-Headers header to be set")
				}
			} else {
				if originHeader != "" {
					t.Errorf("expected no CORS headers, but got Origin: %q", originHeader)
				}
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	g := &HTTPGateway{}

	tests := []struct {
		name        string
		statusCode  int
		message     string
	}{
		{
			name:       "400 bad request",
			statusCode: http.StatusBadRequest,
			message:    "invalid request",
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			message:    "resource not found",
		},
		{
			name:       "500 internal error",
			statusCode: http.StatusInternalServerError,
			message:    "internal server error",
		},
		{
			name:       "503 service unavailable",
			statusCode: http.StatusServiceUnavailable,
			message:    "service temporarily unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			g.writeError(w, tt.statusCode, tt.message)

			if w.Code != tt.statusCode {
				t.Errorf("expected status code %d, got %d", tt.statusCode, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", contentType)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.message) {
				t.Errorf("expected body to contain %q, got %q", tt.message, body)
			}

			if !strings.Contains(body, `"error"`) {
				t.Error("expected body to contain 'error' field")
			}

			if !strings.Contains(body, `"status"`) {
				t.Error("expected body to contain 'status' field")
			}
		})
	}
}

func TestDataFlow_Calculations(t *testing.T) {
	g := &HTTPGateway{}
	g.startTime = time.Now().Add(-10 * time.Second) // Started 10 seconds ago
	g.lastActivity = time.Now()

	// Simulate some activity
	g.requestsTotal.Store(100)
	g.requestsSuccess.Store(90)
	g.requestsFailed.Store(10)
	g.bytesReceived.Store(5000)
	g.bytesSent.Store(10000)

	metrics := g.DataFlow()

	// Check error rate
	expectedErrorRate := 10.0 / 100.0
	if metrics.ErrorRate != expectedErrorRate {
		t.Errorf("expected error rate %f, got %f", expectedErrorRate, metrics.ErrorRate)
	}

	// Check throughput is calculated (should be non-zero)
	if metrics.MessagesPerSecond == 0 {
		t.Error("expected MessagesPerSecond to be calculated, got 0")
	}

	if metrics.BytesPerSecond == 0 {
		t.Error("expected BytesPerSecond to be calculated, got 0")
	}

	// Rough validation of rates (100 messages / ~10 seconds = ~10 msg/s)
	if metrics.MessagesPerSecond < 5 || metrics.MessagesPerSecond > 15 {
		t.Errorf("expected MessagesPerSecond around 10, got %f", metrics.MessagesPerSecond)
	}

	// Total bytes: 15000, over ~10 seconds = ~1500 bytes/s
	if metrics.BytesPerSecond < 1000 || metrics.BytesPerSecond > 2000 {
		t.Errorf("expected BytesPerSecond around 1500, got %f", metrics.BytesPerSecond)
	}
}

func TestDataFlow_ZeroRequests(t *testing.T) {
	g := &HTTPGateway{}
	g.startTime = time.Now()
	g.lastActivity = time.Now()

	metrics := g.DataFlow()

	if metrics.ErrorRate != 0 {
		t.Errorf("expected error rate 0 with no requests, got %f", metrics.ErrorRate)
	}

	if metrics.MessagesPerSecond != 0 {
		t.Errorf("expected 0 messages/sec with no requests, got %f", metrics.MessagesPerSecond)
	}

	if metrics.BytesPerSecond != 0 {
		t.Errorf("expected 0 bytes/sec with no requests, got %f", metrics.BytesPerSecond)
	}
}
