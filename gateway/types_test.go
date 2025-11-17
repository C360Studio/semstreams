package gateway_test

import (
	"testing"
	"time"

	pkgerrors "github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/gateway"
)

func TestRouteMapping_Validate(t *testing.T) {
	tests := []struct {
		name        string
		route       gateway.RouteMapping
		expectError bool
	}{
		{
			name: "valid GET route",
			route: gateway.RouteMapping{
				Path:        "/search/semantic",
				Method:      "GET",
				NATSSubject: "graph.query.semantic",
				TimeoutStr:  "5s",
			},
			expectError: false,
		},
		{
			name: "valid POST route",
			route: gateway.RouteMapping{
				Path:        "/entity/:id",
				Method:      "POST",
				NATSSubject: "graph.query.entity",
			},
			expectError: false,
		},
		{
			name: "empty path",
			route: gateway.RouteMapping{
				Path:        "",
				Method:      "GET",
				NATSSubject: "test.subject",
			},
			expectError: true,
		},
		{
			name: "empty method",
			route: gateway.RouteMapping{
				Path:        "/test",
				Method:      "",
				NATSSubject: "test.subject",
			},
			expectError: true,
		},
		{
			name: "invalid method",
			route: gateway.RouteMapping{
				Path:        "/test",
				Method:      "INVALID",
				NATSSubject: "test.subject",
			},
			expectError: true,
		},
		{
			name: "empty NATS subject",
			route: gateway.RouteMapping{
				Path:        "/test",
				Method:      "GET",
				NATSSubject: "",
			},
			expectError: true,
		},
		{
			name: "timeout too short",
			route: gateway.RouteMapping{
				Path:        "/test",
				Method:      "GET",
				NATSSubject: "test.subject",
				TimeoutStr:  "50ms",
			},
			expectError: true,
		},
		{
			name: "timeout too long",
			route: gateway.RouteMapping{
				Path:        "/test",
				Method:      "GET",
				NATSSubject: "test.subject",
				TimeoutStr:  "60s",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.route.Validate()

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if !pkgerrors.IsInvalid(err) {
					t.Errorf("expected Invalid error classification, got: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Verify default timeout was set
				if tt.route.TimeoutStr == "" {
					if tt.route.Timeout() != 5*time.Second {
						t.Errorf("expected default timeout to be set to 5s, got: %v", tt.route.Timeout())
					}
				}
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      gateway.Config
		expectError bool
	}{
		{
			name: "valid config with CORS",
			config: gateway.Config{
				Routes: []gateway.RouteMapping{
					{
						Path:        "/search/semantic",
						Method:      "GET",
						NATSSubject: "graph.query.semantic",
					},
				},
				EnableCORS:     true,
				CORSOrigins:    []string{"https://example.com"},
				MaxRequestSize: 1024 * 1024,
			},
			expectError: false,
		},
		{
			name: "valid config without CORS",
			config: gateway.Config{
				Routes: []gateway.RouteMapping{
					{
						Path:        "/admin/shutdown",
						Method:      "POST",
						NATSSubject: "admin.shutdown",
					},
				},
				EnableCORS:     false,
				MaxRequestSize: 2048,
			},
			expectError: false,
		},
		{
			name: "no routes",
			config: gateway.Config{
				Routes: []gateway.RouteMapping{},
			},
			expectError: true,
		},
		{
			name: "invalid route in list",
			config: gateway.Config{
				Routes: []gateway.RouteMapping{
					{
						Path:        "",
						Method:      "GET",
						NATSSubject: "test.subject",
					},
				},
			},
			expectError: true,
		},
		{
			name: "negative max request size",
			config: gateway.Config{
				Routes: []gateway.RouteMapping{
					{
						Path:        "/test",
						Method:      "GET",
						NATSSubject: "test.subject",
					},
				},
				MaxRequestSize: -1,
			},
			expectError: true,
		},
		{
			name: "max request size too large",
			config: gateway.Config{
				Routes: []gateway.RouteMapping{
					{
						Path:        "/test",
						Method:      "GET",
						NATSSubject: "test.subject",
					},
				},
				MaxRequestSize: 200 * 1024 * 1024, // 200MB
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if !pkgerrors.IsInvalid(err) {
					t.Errorf("expected Invalid error classification, got: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Verify MaxRequestSize default was set
				if tt.config.MaxRequestSize == 0 {
					if tt.config.MaxRequestSize != 1024*1024 {
						t.Errorf("expected default MaxRequestSize to be 1MB, got: %d", tt.config.MaxRequestSize)
					}
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := gateway.DefaultConfig()

	if config.EnableCORS {
		t.Error("expected EnableCORS to be false by default (requires explicit configuration)")
	}

	if len(config.CORSOrigins) != 0 {
		t.Errorf("expected default CORS origins to be empty, got: %v", config.CORSOrigins)
	}

	if config.MaxRequestSize != 1024*1024 {
		t.Errorf("expected default MaxRequestSize to be 1MB, got: %d", config.MaxRequestSize)
	}

	if len(config.Routes) != 0 {
		t.Errorf("expected default Routes to be empty, got: %d routes", len(config.Routes))
	}
}
