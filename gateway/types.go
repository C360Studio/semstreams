package gateway

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
)

// RouteMapping defines how an external endpoint maps to a NATS subject
type RouteMapping struct {
	// Path is the HTTP route path (e.g., "/search/semantic", "/entity/:id")
	// Supports path parameters with colon notation (:id, :type)
	Path string `json:"path" schema:"type:string,description:HTTP route path,category:basic"`

	// Method is the HTTP method (GET, POST, PUT, DELETE)
	Method string `json:"method" schema:"type:string,description:HTTP method,category:basic"`

	// NATSSubject is the NATS subject to send requests to
	NATSSubject string `json:"nats_subject" schema:"type:string,description:NATS request subject,category:basic"`

	// TimeoutStr for NATS request/reply (default: "5s")
	TimeoutStr string `json:"timeout,omitempty" schema:"type:string,description:Request timeout,default:5s,category:advanced"`

	// Description for OpenAPI documentation
	Description string `json:"description,omitempty" schema:"type:string,description:Route description,category:advanced"`

	// timeout is the parsed duration (internal use)
	timeout time.Duration
}

// Validate ensures the route mapping is valid
func (r *RouteMapping) Validate() error {
	if r.Path == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "RouteMapping", "Validate",
			"path cannot be empty")
	}

	if r.Method == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "RouteMapping", "Validate",
			"method cannot be empty")
	}

	// Validate HTTP method
	validMethods := map[string]bool{
		"GET":    true,
		"POST":   true,
		"PUT":    true,
		"DELETE": true,
		"PATCH":  true,
	}
	if !validMethods[r.Method] {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "RouteMapping", "Validate",
			fmt.Sprintf("invalid HTTP method: %s", r.Method))
	}

	if r.NATSSubject == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "RouteMapping", "Validate",
			"nats_subject cannot be empty")
	}

	// Parse timeout string
	if r.TimeoutStr == "" {
		r.timeout = 5 * time.Second // default
	} else {
		parsedTimeout, err := time.ParseDuration(r.TimeoutStr)
		if err != nil {
			return errors.WrapInvalid(err, "RouteMapping", "Validate",
				fmt.Sprintf("invalid timeout format: %s", r.TimeoutStr))
		}
		r.timeout = parsedTimeout
	}

	// Validate timeout range (100ms to 30s)
	if r.timeout < 100*time.Millisecond || r.timeout > 30*time.Second {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "RouteMapping", "Validate",
			"timeout must be between 100ms and 30s")
	}

	return nil
}

// Timeout returns the parsed timeout duration
func (r *RouteMapping) Timeout() time.Duration {
	return r.timeout
}

// Config holds configuration for gateway components
type Config struct {
	// Routes defines external endpoint to NATS mappings
	Routes []RouteMapping `json:"routes" schema:"type:array,description:Route mappings,category:basic"`

	// EnableCORS enables CORS headers (default: false, requires explicit cors_origins)
	EnableCORS bool `json:"enable_cors" schema:"type:bool,description:Enable CORS,category:advanced"`

	// CORSOrigins lists allowed CORS origins (required when EnableCORS is true)
	// Use ["*"] for development only - production should specify exact origins
	// Example: ["https://app.example.com", "https://app-staging.example.com"]
	CORSOrigins []string `json:"cors_origins,omitempty" schema:"type:array,description:Allowed origins (required for CORS),category:advanced"`

	// MaxRequestSize limits request body size in bytes (default: 1MB)
	MaxRequestSize int64 `json:"max_request_size,omitempty" schema:"type:int,description:Max request size (bytes),category:advanced"`
}

// Validate ensures the gateway configuration is valid
func (c *Config) Validate() error {
	if len(c.Routes) == 0 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"at least one route mapping is required")
	}

	// Validate each route
	for i, route := range c.Routes {
		if err := route.Validate(); err != nil {
			return errors.WrapInvalid(err, "Config", "Validate",
				fmt.Sprintf("invalid route at index %d", i))
		}
	}

	// Validate max request size
	if c.MaxRequestSize < 0 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"max_request_size cannot be negative")
	}

	if c.MaxRequestSize == 0 {
		c.MaxRequestSize = 1024 * 1024 // 1MB default
	}

	if c.MaxRequestSize > 100*1024*1024 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"max_request_size cannot exceed 100MB")
	}

	// CORS requires explicit origin configuration for security
	if c.EnableCORS && len(c.CORSOrigins) == 0 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"enable_cors requires explicit cors_origins configuration (use [\"*\"] for development only)")
	}

	return nil
}

// DefaultConfig returns default gateway configuration
func DefaultConfig() Config {
	return Config{
		Routes:         []RouteMapping{},
		EnableCORS:     false, // Disabled by default (requires explicit configuration)
		CORSOrigins:    []string{},
		MaxRequestSize: 1024 * 1024, // 1MB
	}
}
