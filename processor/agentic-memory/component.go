// Package agenticmemory provides a graph-backed agent memory processor component
// that manages context hydration, fact extraction, and memory checkpointing for agentic loops.
package agenticmemory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// agenticMemorySchema defines the configuration schema
var agenticMemorySchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the agentic-memory processor
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	hydrator      *Hydrator
	extractor     *LLMExtractor
	profileReader atomic.Pointer[operatingmodel.ProfileReader]

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Metrics
	eventsProcessed int64
	errors          int64
	lastActivity    time.Time
}

// NewComponent creates a new agentic-memory processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config with defaults")
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}

	// Create hydrator (graph client will be provided later during initialization)
	hydrator, err := NewHydrator(config.Hydration, nil)
	if err != nil {
		return nil, errs.Wrap(err, "Component", "NewComponent", "create hydrator")
	}

	// Create LLM extractor (LLM client will be provided later during initialization)
	extractor, err := NewLLMExtractor(config.Extraction, nil)
	if err != nil {
		return nil, errs.Wrap(err, "Component", "NewComponent", "create extractor")
	}

	comp := &Component{
		name:       "agentic-memory",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
		hydrator:   hydrator,
		extractor:  extractor,
	}
	initProfileReader(&comp.profileReader)
	return comp, nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing memory events
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errs.ErrAlreadyStarted
	}
	// Mark as running immediately to prevent concurrent Start calls
	c.running = true
	c.mu.Unlock()

	// NATS client is optional for unit tests
	if c.natsClient != nil {
		if err := c.setupInputConsumers(ctx); err != nil {
			// Reset running state on failure
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			return errs.Wrap(err, "Component", "Start", "setup input consumers")
		}
	}

	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()

	return nil
}

// setupInputConsumers sets up JetStream consumers for all input ports
func (c *Component) setupInputConsumers(ctx context.Context) error {
	for _, port := range c.config.Ports.Inputs {
		subject := port.Subject
		if subject == "" {
			continue
		}

		var handler func(context.Context, []byte)

		// Route to appropriate handler based on port name
		switch port.Name {
		case "compaction_events":
			handler = c.handleCompactionEvent
		case "hydrate_requests":
			handler = c.handleHydrateRequest
		default:
			c.logger.Debug("Skipping unknown input port", "port", port.Name)
			continue
		}

		if err := c.setupConsumer(ctx, port, handler); err != nil {
			return errs.Wrap(err, "Component", "setupInputConsumers", fmt.Sprintf("setup consumer for %s", port.Name))
		}
	}

	return nil
}

// setupConsumer sets up a JetStream consumer for an input port
func (c *Component) setupConsumer(ctx context.Context, port component.PortDefinition, handler func(context.Context, []byte)) error {
	// Determine stream name
	streamName := c.config.StreamName
	if streamName == "" {
		streamName = "AGENT"
	}

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name
	consumerName := fmt.Sprintf("agentic-memory-%s", sanitizeSubject(port.Subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject,
		"port", port.Name)

	// Get consumer config from port definition (allows user configuration)
	// Defaults to "new" - only process new messages, don't replay old ones
	consumerCfg := component.GetConsumerConfigFromDefinition(port)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: port.Subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		handler(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupConsumer", fmt.Sprintf("consumer setup for stream %s", streamName))
	}

	c.logger.Info("Subscribed (JetStream)",
		"subject", port.Subject,
		"stream", streamName,
		"consumer", consumerName,
		"port", port.Name)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "Component", "waitForStream", "get JetStream context")
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}

	return errs.WrapTransient(
		fmt.Errorf("stream %s not found after %d retries", streamName, maxRetries),
		"Component",
		"waitForStream",
		"find stream",
	)
}

// sanitizeSubject converts a subject pattern to a valid consumer name suffix
func sanitizeSubject(subject string) string {
	s := strings.ReplaceAll(subject, ".", "-")
	s = strings.ReplaceAll(s, ">", "all")
	s = strings.ReplaceAll(s, "*", "any")
	return s
}

// Stop gracefully stops the component within the given timeout
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Cleanup operations would happen here in integration mode
	// For now, just mark as stopped
	c.running = false
	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "agentic-memory",
		Type:        "processor",
		Description: "Graph-backed agent memory with context hydration and fact extraction",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = buildInputPort(portDef)
	}
	return ports
}

// OutputPorts returns configured output port definitions
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = buildOutputPort(portDef)
	}
	return ports
}

// buildInputPort creates a Port from a PortDefinition for input
func buildInputPort(portDef component.PortDefinition) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   component.DirectionInput,
		Required:    portDef.Required,
		Description: portDef.Description,
	}

	// Create appropriate port config based on type
	switch portDef.Type {
	case "jetstream":
		port.Config = component.JetStreamPort{
			StreamName: portDef.StreamName,
			Subjects:   []string{portDef.Subject},
		}
	case "kv-watch", "kvwatch":
		port.Config = component.KVWatchPort{
			Bucket: portDef.Bucket,
		}
	default:
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	}

	return port
}

// buildOutputPort creates a Port from a PortDefinition for output
func buildOutputPort(portDef component.PortDefinition) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   component.DirectionOutput,
		Required:    portDef.Required,
		Description: portDef.Description,
	}

	// Create appropriate port config based on type
	switch portDef.Type {
	case "jetstream":
		port.Config = component.JetStreamPort{
			StreamName: portDef.StreamName,
			Subjects:   []string{portDef.Subject},
		}
	default:
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	}

	return port
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return agenticMemorySchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	// Read atomic counters without lock
	errors := atomic.LoadInt64(&c.errors)

	// Lock only for non-atomic fields
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	status := c.getStatus()
	c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(errors),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// getStatus returns a status string
func (c *Component) getStatus() string {
	if c.running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	// Read atomic counters without lock
	eventsProcessed := atomic.LoadInt64(&c.eventsProcessed)
	errors := atomic.LoadInt64(&c.errors)

	// Lock only for non-atomic fields
	c.mu.RLock()
	lastActivity := c.lastActivity
	c.mu.RUnlock()

	var errorRate float64
	total := eventsProcessed + errors
	if total > 0 {
		errorRate = float64(errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      lastActivity,
	}
}

// outputPortDefs returns the output port definitions slice, or nil when Ports is unset.
// This lets ResolveSubject fall back gracefully to portName + "." + suffix.
func (c *Component) outputPortDefs() []component.PortDefinition {
	if c.config.Ports == nil {
		return nil
	}
	return c.config.Ports.Outputs
}
