package service

import (
	"fmt"
	"time"
)

// ManagerConfig holds configuration for the Manager HTTP server
// Simple struct - no UnmarshalJSON, no Enabled field
type ManagerConfig struct {
	HTTPPort   int      `json:"http_port"`
	SwaggerUI  bool     `json:"swagger_ui"`
	ServerInfo InfoSpec `json:"server_info"`

	// HTTPReadTimeout caps how long the server waits for the request body.
	// Go duration string (e.g. "30s"). Empty falls back to
	// DefaultHTTPReadTimeout. Bump for paths that stream large request
	// bodies; the default is enough for typical JSON GraphQL queries.
	HTTPReadTimeout string `json:"http_read_timeout,omitempty"`

	// HTTPWriteTimeout caps how long the server takes to write the
	// response. Go duration string (e.g. "120s"). Empty falls back to
	// DefaultHTTPWriteTimeout. CRITICAL for LLM-backed paths (globalSearch
	// answer synthesis, classifier, etc.) — the prior hardcoded 10s
	// killed every long-running query mid-write with EOF on the client
	// side. Set to >= 2× the slowest expected handler time.
	HTTPWriteTimeout string `json:"http_write_timeout,omitempty"`
}

// HTTP server timeout defaults. Tuned for LLM-backed workloads — the
// prior 10s/10s defaults killed every long graph query with EOF. See
// commit message + feedback memory for the case study.
const (
	DefaultHTTPReadTimeout  = 30 * time.Second
	DefaultHTTPWriteTimeout = 120 * time.Second
)

// Validate checks if the configuration is valid
func (c ManagerConfig) Validate() error {
	if c.HTTPPort < 0 || c.HTTPPort > 65535 {
		return fmt.Errorf("invalid HTTP port: %d", c.HTTPPort)
	}
	if c.HTTPReadTimeout != "" {
		if _, err := time.ParseDuration(c.HTTPReadTimeout); err != nil {
			return fmt.Errorf("invalid http_read_timeout %q: %w", c.HTTPReadTimeout, err)
		}
	}
	if c.HTTPWriteTimeout != "" {
		if _, err := time.ParseDuration(c.HTTPWriteTimeout); err != nil {
			return fmt.Errorf("invalid http_write_timeout %q: %w", c.HTTPWriteTimeout, err)
		}
	}
	// SwaggerUI is a boolean (no validation needed)
	// ServerInfo fields can be empty (will get defaults)
	return nil
}

// ResolvedHTTPReadTimeout returns the configured read timeout or the
// default when unset/invalid.
func (c ManagerConfig) ResolvedHTTPReadTimeout() time.Duration {
	if c.HTTPReadTimeout == "" {
		return DefaultHTTPReadTimeout
	}
	d, err := time.ParseDuration(c.HTTPReadTimeout)
	if err != nil {
		return DefaultHTTPReadTimeout
	}
	return d
}

// ResolvedHTTPWriteTimeout returns the configured write timeout or the
// default when unset/invalid.
func (c ManagerConfig) ResolvedHTTPWriteTimeout() time.Duration {
	if c.HTTPWriteTimeout == "" {
		return DefaultHTTPWriteTimeout
	}
	d, err := time.ParseDuration(c.HTTPWriteTimeout)
	if err != nil {
		return DefaultHTTPWriteTimeout
	}
	return d
}
