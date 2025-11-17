// Package websocket provides WebSocket input component for receiving federated data
package websocket

import (
	"reflect"
	"time"

	"github.com/c360/semstreams/component"
)

// Mode defines the operation mode for WebSocket input
type Mode string

const (
	// ModeServer listens for incoming WebSocket connections
	ModeServer Mode = "server"
	// ModeClient connects to remote WebSocket server
	ModeClient Mode = "client"
)

// Config holds configuration for WebSocket input component
type Config struct {
	// Mode determines if component acts as server (listen) or client (connect)
	Mode Mode `json:"mode" schema:"type:string,description:Operation mode (server or client),category:basic"`

	// Server mode configuration
	ServerConfig *ServerConfig `json:"server,omitempty" schema:"type:object,description:Server mode configuration,category:server"`

	// Client mode configuration
	ClientConfig *ClientConfig `json:"client,omitempty" schema:"type:object,description:Client mode configuration,category:client"`

	// Authentication configuration
	Auth *AuthConfig `json:"auth,omitempty" schema:"type:object,description:Authentication configuration,category:security"`

	// Bidirectional communication configuration
	Bidirectional *BidirectionalConfig `json:"bidirectional,omitempty" schema:"type:object,description:Bidirectional request/reply configuration,category:advanced"`

	// Backpressure configuration
	Backpressure *BackpressureConfig `json:"backpressure,omitempty" schema:"type:object,description:Backpressure handling configuration,category:advanced"`

	// Port configuration for inputs and outputs
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}

// ServerConfig holds server mode configuration
type ServerConfig struct {
	HTTPPort          int    `json:"http_port" schema:"type:int,description:HTTP port to listen on,category:basic"`
	Path              string `json:"path" schema:"type:string,description:WebSocket endpoint path,category:basic"`
	MaxConnections    int    `json:"max_connections" schema:"type:int,description:Maximum concurrent connections,category:limits"`
	ReadBufferSize    int    `json:"read_buffer_size" schema:"type:int,description:WebSocket read buffer size,category:advanced"`
	WriteBufferSize   int    `json:"write_buffer_size" schema:"type:int,description:WebSocket write buffer size,category:advanced"`
	EnableCompression bool   `json:"enable_compression" schema:"type:bool,description:Enable per-message compression,category:advanced"`
}

// ClientConfig holds client mode configuration
type ClientConfig struct {
	URL       string           `json:"url" schema:"type:string,description:WebSocket server URL to connect to,category:basic"`
	Reconnect *ReconnectConfig `json:"reconnect,omitempty" schema:"type:object,description:Reconnection configuration,category:reliability"`
}

// ReconnectConfig holds reconnection configuration for client mode
type ReconnectConfig struct {
	Enabled         bool          `json:"enabled" schema:"type:bool,description:Enable automatic reconnection,category:basic"`
	MaxRetries      int           `json:"max_retries" schema:"type:int,description:Maximum reconnection attempts (0=unlimited),category:limits"`
	InitialInterval time.Duration `json:"initial_interval" schema:"type:duration,description:Initial retry interval,category:timing"`
	MaxInterval     time.Duration `json:"max_interval" schema:"type:duration,description:Maximum retry interval,category:timing"`
	Multiplier      float64       `json:"multiplier" schema:"type:float,description:Backoff multiplier,category:advanced"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Type             string `json:"type" schema:"type:string,description:Authentication type (none bearer basic),category:basic"`
	BearerTokenEnv   string `json:"bearer_token_env,omitempty" schema:"type:string,description:Environment variable for bearer token,category:security"`
	BasicUsernameEnv string `json:"basic_username_env,omitempty" schema:"type:string,description:Environment variable for basic auth username,category:security"`
	BasicPasswordEnv string `json:"basic_password_env,omitempty" schema:"type:string,description:Environment variable for basic auth password,category:security"`
}

// BidirectionalConfig holds bidirectional communication configuration
type BidirectionalConfig struct {
	Enabled               bool          `json:"enabled" schema:"type:bool,description:Enable request/reply patterns,category:basic"`
	RequestTimeout        time.Duration `json:"request_timeout" schema:"type:duration,description:Timeout for request/reply,category:timing"`
	MaxConcurrentRequests int           `json:"max_concurrent_requests" schema:"type:int,description:Maximum concurrent requests,category:limits"`
}

// BackpressureConfig holds backpressure handling configuration
type BackpressureConfig struct {
	Enabled   bool   `json:"enabled" schema:"type:bool,description:Enable backpressure handling,category:basic"`
	QueueSize int    `json:"queue_size" schema:"type:int,description:Internal message queue size,category:limits"`
	OnFull    string `json:"on_full" schema:"type:string,description:Action when queue full (drop_oldest drop_newest block),category:policy"`
}

// DefaultConfig returns the default configuration for WebSocket input
func DefaultConfig() Config {
	// Output port for received data
	outputDefs := []component.PortDefinition{
		{
			Name:        "ws_data",
			Type:        "nats",
			Subject:     "federated.data",
			Required:    false,
			Description: "Data messages received via WebSocket",
		},
		{
			Name:        "ws_control",
			Type:        "nats",
			Subject:     "federated.control",
			Required:    false,
			Description: "Control messages (requests/replies)",
		},
	}

	return Config{
		Mode: ModeServer,
		ServerConfig: &ServerConfig{
			HTTPPort:          8081,
			Path:              "/",
			MaxConnections:    100,
			ReadBufferSize:    4096,
			WriteBufferSize:   4096,
			EnableCompression: true,
		},
		ClientConfig: &ClientConfig{
			URL: "ws://localhost:8080/stream",
			Reconnect: &ReconnectConfig{
				Enabled:         true,
				MaxRetries:      10,
				InitialInterval: 1 * time.Second,
				MaxInterval:     60 * time.Second,
				Multiplier:      2.0,
			},
		},
		Auth: &AuthConfig{
			Type: "none",
		},
		Bidirectional: &BidirectionalConfig{
			Enabled:               true,
			RequestTimeout:        5 * time.Second,
			MaxConcurrentRequests: 10,
		},
		Backpressure: &BackpressureConfig{
			Enabled:   true,
			QueueSize: 1000,
			OnFull:    "drop_oldest",
		},
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{}, // No NATS inputs for input component
			Outputs: outputDefs,
		},
	}
}

// websocketInputSchema defines the configuration schema
var websocketInputSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))
