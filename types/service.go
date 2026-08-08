// Package types contains shared domain types used across the semstreams platform
package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ServiceConfig provides configuration for creating a service instance.
// This standardizes service configuration similar to ComponentConfig,
// providing activation separately from service-specific config. The containing
// map key is the sole service identity.
type ServiceConfig struct {
	Enabled bool            `json:"enabled"` // Whether service is enabled at runtime
	Config  json.RawMessage `json:"config"`  // Service-specific configuration
}

// UnmarshalJSON rejects retired outer fields rather than silently accepting an
// alias for map-key identity.
func (s *ServiceConfig) UnmarshalJSON(data []byte) error {
	type serviceConfig ServiceConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded serviceConfig
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("decode service config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode service config: multiple JSON values")
		}
		return fmt.Errorf("decode service config: %w", err)
	}
	*s = ServiceConfig(decoded)
	return nil
}

// Validate ensures the service configuration is structurally valid.
func (s ServiceConfig) Validate() error {
	return nil
}

// ServiceConfigs holds service instance configurations.
// The map key is the service name (e.g., "metrics", "discovery").
// Services are only created if both:
// 1. They have registered a constructor via init()
// 2. They have an entry in this config map with enabled=true
type ServiceConfigs map[string]ServiceConfig
