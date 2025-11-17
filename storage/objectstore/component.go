// Package objectstore provides a NATS ObjectStore-based storage component
// for immutable message storage with time-bucketed keys and caching.
package objectstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/natsclient"
	"github.com/nats-io/nats.go"
)

// objectstoreSchema defines the configuration schema for ObjectStore component
// Generated from Config struct tags using reflection
var objectstoreSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component wraps ObjectStore as a component with NATS ports
//
// Composition-Friendly Design:
//   - Generic NATS port handling (no semantic requirements)
//   - Publishes simple storage events (not semantic messages)
//   - Allows SemStreams to wrap/extend for semantic behavior
type Component struct {
	// Component metadata
	instanceName string
	enabled      bool
	started      bool

	// core dependencies
	store      *Store
	natsClient *natsclient.Client
	config     Config
	logger     *slog.Logger

	// NATS subscriptions
	apiSub   *nats.Subscription
	writeSub *nats.Subscription

	// Metrics tracking
	messagesReceived uint64
	messagesStored   uint64
	lastActivity     atomic.Value // stores time.Time
}

// Request represents a request to the ObjectStore API
type Request struct {
	Action string          `json:"action"` // "get", "store", "list"
	Key    string          `json:"key,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Prefix string          `json:"prefix,omitempty"` // For list operation
}

// Response represents a response from the ObjectStore API
type Response struct {
	Success bool            `json:"success"`
	Key     string          `json:"key,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Keys    []string        `json:"keys,omitempty"` // For list operation
	Error   string          `json:"error,omitempty"`
}

// Event represents a simple storage event published by ObjectStore
// core design: Just indicates what happened, no semantic payload
type Event struct {
	Type      string         `json:"type"` // "stored", "retrieved"
	Key       string         `json:"key"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Ensure Component implements required interfaces
var _ component.Discoverable = (*Component)(nil)
var _ component.LifecycleComponent = (*Component)(nil)

// Initialize sets up the component (no I/O operations)
func (c *Component) Initialize() error {
	// No initialization needed - all setup happens in Start
	return nil
}

// NewComponent creates a new ObjectStore component factory
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Parse user config if provided
	if len(rawConfig) > 0 {
		var userConfig Config
		if err := json.Unmarshal(rawConfig, &userConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}

		// Apply user overrides
		if userConfig.Ports != nil {
			cfg.Ports = userConfig.Ports
		}
		if userConfig.BucketName != "" {
			cfg.BucketName = userConfig.BucketName
		}
		if userConfig.DataCache.Enabled || userConfig.DataCache.MaxSize > 0 {
			cfg.DataCache = userConfig.DataCache
		}
		// Copy over pluggable generators
		if userConfig.KeyGenerator != nil {
			cfg.KeyGenerator = userConfig.KeyGenerator
		}
		if userConfig.MetadataExtractor != nil {
			cfg.MetadataExtractor = userConfig.MetadataExtractor
		}
	}

	// Default instance name - would be provided by ComponentManager
	instanceName := "objectstore"

	return &Component{
		instanceName: instanceName,
		enabled:      true,
		config:       cfg,
		natsClient:   deps.NATSClient,
		logger:       deps.GetLogger(),
	}, nil
}

// Start initializes the ObjectStore and sets up NATS handlers
func (c *Component) Start(ctx context.Context) error {
	if c.started {
		c.logger.Debug("ObjectStore already started", "name", c.instanceName)
		return nil
	}

	c.logger.Debug("Creating ObjectStore", "name", c.instanceName, "bucket", c.config.BucketName)

	// Create the underlying ObjectStore
	store, err := NewStoreWithConfig(ctx, c.natsClient, c.config)
	if err != nil {
		c.logger.Error(
			"Failed to create ObjectStore",
			"name",
			c.instanceName,
			"bucket",
			c.config.BucketName,
			"error",
			err,
		)
		return fmt.Errorf("failed to create object store: %w", err)
	}
	c.store = store

	c.logger.Debug("ObjectStore created successfully", "name", c.instanceName, "bucket", c.config.BucketName)

	// Get raw NATS connection for subscriptions
	nc := c.natsClient.GetConnection()

	// Subscribe to API requests (Request/Response pattern)
	if c.hasPort("api") {
		apiSubject := c.getPortSubject("api", "storage.%s.api")
		c.logger.Debug("Subscribing to API subject", "name", c.instanceName, "subject", apiSubject)
		c.apiSub, err = nc.Subscribe(apiSubject, c.handleAPIRequest)
		if err != nil {
			c.logger.Error(
				"Failed to subscribe to API subject",
				"name",
				c.instanceName,
				"subject",
				apiSubject,
				"error",
				err,
			)
			return fmt.Errorf("failed to subscribe to API subject %s: %w", apiSubject, err)
		}
	}

	// Subscribe to write requests (async fire-and-forget)
	if c.hasPort("write") {
		writeSubject := c.getPortSubject("write", "storage.%s.write")
		c.logger.Debug("Subscribing to write subject", "name", c.instanceName, "subject", writeSubject)
		c.writeSub, err = nc.Subscribe(writeSubject, c.handleWriteRequest)
		if err != nil {
			c.logger.Error(
				"Failed to subscribe to write subject",
				"name",
				c.instanceName,
				"subject",
				writeSubject,
				"error",
				err,
			)
			return fmt.Errorf("failed to subscribe to write subject %s: %w", writeSubject, err)
		}
	}

	c.started = true
	c.lastActivity.Store(time.Now())
	c.logger.Debug("ObjectStore component fully started", "name", c.instanceName)
	return nil
}

// Stop cleanly shuts down the component
func (c *Component) Stop(_ time.Duration) error {
	if !c.started {
		return nil
	}

	// Close underlying store first to clean up cache resources
	if c.store != nil {
		if err := c.store.Close(); err != nil {
			return fmt.Errorf("failed to close store: %w", err)
		}
	}

	// Then unsubscribe from NATS
	if c.apiSub != nil {
		if err := c.apiSub.Unsubscribe(); err != nil {
			return fmt.Errorf("failed to unsubscribe from API: %w", err)
		}
	}

	if c.writeSub != nil {
		if err := c.writeSub.Unsubscribe(); err != nil {
			return fmt.Errorf("failed to unsubscribe from write: %w", err)
		}
	}

	c.started = false
	return nil
}

// IsStarted returns whether the component is running
func (c *Component) IsStarted() bool {
	return c.started
}

// handleAPIRequest handles synchronous Request/Response operations
func (c *Component) handleAPIRequest(msg *nats.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	var req Request
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.respondWithError(msg, fmt.Errorf("invalid request: %w", err))
		return
	}

	// Use proper timeout context for API requests
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	switch req.Action {
	case "get":
		data, err := c.store.Get(ctx, req.Key)
		if err != nil {
			c.respondWithError(msg, err)
			return
		}

		resp := Response{
			Success: true,
			Key:     req.Key,
			Data:    data,
		}
		c.respond(msg, resp)

	case "store":
		var msgData any
		if err := json.Unmarshal(req.Data, &msgData); err != nil {
			c.respondWithError(msg, fmt.Errorf("invalid data: %w", err))
			return
		}

		key, err := c.store.Store(ctx, msgData)
		if err != nil {
			c.respondWithError(msg, err)
			return
		}

		atomic.AddUint64(&c.messagesStored, 1)
		resp := Response{
			Success: true,
			Key:     key,
		}
		c.respond(msg, resp)

		// Publish stored event
		c.publishEvent(Event{
			Type:      "stored",
			Key:       key,
			Timestamp: time.Now(),
		})

	case "list":
		keys, err := c.store.List(ctx, req.Prefix)
		if err != nil {
			c.respondWithError(msg, err)
			return
		}

		resp := Response{
			Success: true,
			Keys:    keys,
		}
		c.respond(msg, resp)

	default:
		c.respondWithError(msg, fmt.Errorf("unknown action: %s", req.Action))
	}
}

// handleWriteRequest handles async write operations
// core design: Accepts any message, stores it, publishes simple event
func (c *Component) handleWriteRequest(msg *nats.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	// Store the raw data
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key, err := c.store.Store(ctx, msg.Data)
	if err != nil {
		c.logger.Error("Failed to store message",
			slog.String("error", err.Error()))
		return
	}

	atomic.AddUint64(&c.messagesStored, 1)

	// Publish simple storage event
	c.publishEvent(Event{
		Type:      "stored",
		Key:       key,
		Timestamp: time.Now(),
	})
}

// publishEvent publishes a simple storage event to the events subject
func (c *Component) publishEvent(event Event) {
	if !c.hasPort("events") {
		return // No events port configured
	}

	eventSubject := c.getPortSubject("events", "storage.%s.events")
	data, err := json.Marshal(event)
	if err != nil {
		c.logger.Error("Failed to marshal event",
			slog.String("error", err.Error()))
		return
	}

	if err := c.natsClient.GetConnection().Publish(eventSubject, data); err != nil {
		c.logger.Error("Failed to publish event",
			slog.String("subject", eventSubject),
			slog.String("error", err.Error()))
		return
	}
}

// respond sends a response for Request/Response pattern
func (c *Component) respond(msg *nats.Msg, resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		c.logger.Error("Failed to marshal response",
			"error", err,
			"subject", msg.Subject)
		return
	}

	if err := msg.Respond(data); err != nil {
		c.logger.Error("Failed to send response",
			"error", err,
			"subject", msg.Subject)
		return
	}
}

// respondWithError sends an error response
func (c *Component) respondWithError(msg *nats.Msg, err error) {
	resp := Response{
		Success: false,
		Error:   err.Error(),
	}
	c.respond(msg, resp)
}

// hasPort checks if a port with the given name is configured
func (c *Component) hasPort(name string) bool {
	if c.config.Ports == nil {
		return false
	}
	for _, port := range c.config.Ports.Inputs {
		if port.Name == name {
			return true
		}
	}
	for _, port := range c.config.Ports.Outputs {
		if port.Name == name {
			return true
		}
	}
	return false
}

// getPortSubject gets the subject for a named port, or generates a default
func (c *Component) getPortSubject(portName, defaultFormat string) string {
	if c.config.Ports != nil {
		for _, port := range c.config.Ports.Inputs {
			if port.Name == portName {
				return port.Subject
			}
		}
		for _, port := range c.config.Ports.Outputs {
			if port.Name == portName {
				return port.Subject
			}
		}
	}
	return fmt.Sprintf(defaultFormat, c.instanceName)
}

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        c.instanceName,
		Type:        "storage",
		Description: "NATS ObjectStore component for immutable message storage",
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports for this component
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, 0)
	for _, portDef := range c.config.Ports.Inputs {
		var port component.Port
		if portDef.Type == "nats-request" {
			port = component.Port{
				Name:        portDef.Name,
				Direction:   component.DirectionInput,
				Required:    portDef.Required,
				Description: portDef.Description,
				Config: component.NATSRequestPort{
					Subject: portDef.Subject,
					Timeout: "2s",
				},
			}
		} else {
			port = component.Port{
				Name:        portDef.Name,
				Direction:   component.DirectionInput,
				Required:    portDef.Required,
				Description: portDef.Description,
				Config: component.NATSPort{
					Subject: portDef.Subject,
				},
			}
		}
		ports = append(ports, port)
	}
	return ports
}

// OutputPorts returns the output ports for this component
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, 0)
	for _, portDef := range c.config.Ports.Outputs {
		ports = append(ports, component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		})
	}
	return ports
}

// ConfigSchema returns the configuration schema for this component
// References the package-level objectstoreSchema variable for efficient retrieval
func (c *Component) ConfigSchema() component.ConfigSchema {
	return objectstoreSchema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	var lastAct time.Time
	if v := c.lastActivity.Load(); v != nil {
		lastAct = v.(time.Time)
	}

	return component.HealthStatus{
		Healthy:    c.started,
		LastCheck:  time.Now(),
		ErrorCount: 0, // Would need error tracking
		LastError:  "",
		Uptime:     time.Since(lastAct),
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	var lastAct time.Time
	if v := c.lastActivity.Load(); v != nil {
		lastAct = v.(time.Time)
	}

	// Simple metrics - would need rate calculation in production
	return component.FlowMetrics{
		MessagesPerSecond: 0, // Would need rate calculation
		BytesPerSecond:    0, // Would need byte tracking
		ErrorRate:         0, // Would need error tracking
		LastActivity:      lastAct,
	}
}
