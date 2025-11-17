package service

import "fmt"

// ManagerConfig holds configuration for the Manager HTTP server
// Simple struct - no UnmarshalJSON, no Enabled field
type ManagerConfig struct {
	HTTPPort   int      `json:"http_port"`
	SwaggerUI  bool     `json:"swagger_ui"`
	ServerInfo InfoSpec `json:"server_info"`
}

// Validate checks if the configuration is valid
func (c ManagerConfig) Validate() error {
	if c.HTTPPort < 0 || c.HTTPPort > 65535 {
		return fmt.Errorf("invalid HTTP port: %d", c.HTTPPort)
	}
	// SwaggerUI is a boolean (no validation needed)
	// ServerInfo fields can be empty (will get defaults)
	return nil
}
