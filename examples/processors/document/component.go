package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ComponentConfig holds configuration for the document processor component.
type ComponentConfig struct {
	// Ports defines NATS input/output subjects for message routing
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// OrgID is the organization identifier for federated entity IDs
	OrgID string `json:"org_id" schema:"type:string,description:Organization identifier,category:basic,required:true"`

	// Platform is the platform/product identifier for federated entity IDs
	Platform string `json:"platform" schema:"type:string,description:Platform identifier,category:basic,required:true"`
}

// DefaultConfig returns the default configuration for document processor
func DefaultConfig() ComponentConfig {
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "raw.document.>"}, Required: true,
			Description: "NATS subjects with document JSON data",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "nats_output", Config: component.NATSPort{Subject: "events.graph.entity.document", Interface: &component.InterfaceContract{Type: "domain.content.document.v1"}}, Required: true,
			Description: "NATS subject for Graphable document payloads",
		},
	}

	return ComponentConfig{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		OrgID:    "default-org",
		Platform: "default-platform",
	}
}

// documentSchema defines the configuration schema for document processor
var documentSchema = component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

// Component wraps the domain-specific document processor with component lifecycle.
type Component struct {
	name               string
	subjects           []string
	outputSubj         string
	inputPorts         []component.Port
	outputPorts        []component.Port
	config             ComponentConfig // Store full config for port type checking
	natsClient         *natsclient.Client
	logger             *slog.Logger
	waitForStreamInput func(context.Context, string) error
	consumeStream      func(
		context.Context,
		natsclient.PortConsumerContext,
		natsclient.StreamConsumerConfig,
		func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error)

	// Domain processor (stateless, pure business logic)
	processor *Processor

	// Content storage (optional - when set, stores content before publishing)
	contentStore *objectstore.Store

	// Lifecycle management
	running        bool
	startTime      time.Time
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc
	subscriptions  []*natsclient.Subscription
	consumers      []streamConsumerBinding

	// Metrics
	messagesProcessed int64
	messagesWrapped   int64
	contentStored     int64
	errors            int64
	lastActivity      time.Time
}

type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

// NewComponent creates a new document processor component from configuration.
func NewComponent(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config ComponentConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "DocumentComponent", "NewComponent", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Validate configuration
	if config.OrgID == "" {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "DocumentComponent", "NewComponent",
			"OrgID is required")
	}

	if config.Platform == "" {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "DocumentComponent", "NewComponent",
			"Platform is required")
	}
	if len(config.Ports.Outputs) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "DocumentComponent", "NewComponent", "exactly one output port is required")
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	var outputSubject string
	inputPorts := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		input, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "DocumentComponent", "NewComponent", "resolve input port")
		}
		facts, err := input.Facts()
		if err != nil || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "DocumentComponent", "NewComponent", "input port must declare one NATS subject")
		}
		inputPorts = append(inputPorts, input)
		inputSubjects = append(inputSubjects, facts.NATSSubjects()[0])
	}
	outputPorts := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		output, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "DocumentComponent", "NewComponent", "resolve output port")
		}
		facts, err := output.Facts()
		if err != nil || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "DocumentComponent", "NewComponent", "output port must declare one NATS subject")
		}
		outputPorts = append(outputPorts, output)
	}
	facts, _ := outputPorts[0].Facts()
	outputSubject = facts.NATSSubjects()[0]

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "DocumentComponent", "NewComponent",
			"no input subjects configured")
	}

	// Create domain processor with organizational context
	processor := NewProcessor(Config{
		OrgID:    config.OrgID,
		Platform: config.Platform,
	})

	return &Component{
		name:        "document-processor",
		subjects:    inputSubjects,
		outputSubj:  outputSubject,
		inputPorts:  inputPorts,
		outputPorts: outputPorts,
		config:      config, // Store full config for port type checking
		natsClient:  deps.NATSClient,
		logger:      deps.GetLogger(),
		processor:   processor,
	}, nil
}

// Initialize prepares the component (no-op for document processor)
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing document messages
func (c *Component) Start(ctx context.Context) (startErr error) {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "DocumentComponent", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "DocumentComponent", "Start", "context already cancelled")
	}
	c.lifecycleMu.Lock()
	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "DocumentComponent", "Start", "check running state")
	}
	if c.natsClient == nil {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrMissingConfig, "DocumentComponent", "Start", "NATS client required")
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	c.lifecycleUsed = true
	c.cleanupPending = true
	c.cancel = cancel
	c.startDone = startDone
	c.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanupFailedStart)
			startErr = errors.Join(startErr, rollbackErr)
			c.lifecycleMu.Lock()
			if rollbackErr == nil {
				c.cleanupPending = false
				c.terminal = true
				c.clearLifecycleHandles()
			}
			close(startDone)
			c.startDone = nil
			c.lifecycleMu.Unlock()
			return
		}
		c.lifecycleMu.Lock()
		c.cleanupPending = false
		close(startDone)
		c.startDone = nil
		c.lifecycleMu.Unlock()
	}()

	// Subscribe to input subjects - check port type for each
	for _, port := range c.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return err
		}
		subject := facts.NATSSubjects()[0]

		c.logger.Debug("Setting up subscription",
			"component", c.name,
			"port", port.Name,
			"subject", subject,
			"type", facts.Kind())

		if facts.Kind() == component.PortKindJetStream {
			// JetStream subscription - use durable consumer
			if err := c.setupJetStreamConsumer(runCtx, port); err != nil {
				c.logger.Error("Failed to setup JetStream consumer",
					"component", c.name,
					"port", port.Name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "DocumentComponent", "Start", fmt.Sprintf("setup JetStream consumer for %s", subject))
			}
		} else if facts.Kind() == component.PortKindNATS {
			// Core NATS subscription
			sub, err := c.natsClient.Subscribe(runCtx, subject, func(ctx context.Context, msg *nats.Msg) {
				c.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				c.logger.Error("Failed to subscribe to NATS subject",
					"component", c.name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "DocumentComponent", "Start", fmt.Sprintf("subscribe to %s", subject))
			}
			c.subscriptions = append(c.subscriptions, sub)
		} else {
			return fmt.Errorf("unsupported input port %s kind %s", port.Name, facts.Kind())
		}

		c.logger.Debug("Subscription setup successfully",
			"component", c.name,
			"subject", subject,
			"type", facts.Kind(),
			"output_subject", c.outputSubj)
	}

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()
	committed = true

	c.logger.Info("Document processor started",
		"component", c.name,
		"input_subjects", c.subjects,
		"output_subject", c.outputSubj)

	return nil
}

// Stop gracefully stops the component
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		c.lifecycleMu.Lock()
		if !c.lifecycleUsed {
			c.lifecycleUsed = true
			c.terminal = true
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.terminal {
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.startDone != nil {
			startDone := c.startDone
			c.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if c.stopping {
			c.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "DocumentComponent", "Stop", "concurrent Stop is unsupported")
		}
		retryable := c.cleanupPending
		c.stopping = true
		c.lifecycleMu.Unlock()

		stopErr := c.cleanup(ctx)
		c.lifecycleMu.Lock()
		c.stopping = false
		if retryable && stopErr != nil {
			c.lifecycleMu.Unlock()
			return stopErr
		}
		c.cleanupPending = false
		c.terminal = true
		c.clearLifecycleHandles()
		c.lifecycleMu.Unlock()
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return stopErr
	}
}

func (c *Component) cleanupFailedStart(ctx context.Context) error { return c.cleanup(ctx) }

func (c *Component) cleanup(ctx context.Context) error {
	var stopErr error
	for _, sub := range c.subscriptions {
		stopErr = errors.Join(stopErr, sub.Drain(ctx))
	}
	for index := range c.consumers {
		binding := &c.consumers[index]
		if !binding.drainIssued {
			binding.handle.Drain()
			binding.drainIssued = true
		}
		select {
		case <-binding.handle.Closed():
		case <-ctx.Done():
			stopErr = errors.Join(stopErr, ctx.Err())
		}
	}
	if c.cancel != nil {
		c.cancel()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		stopErr = errors.Join(stopErr, ctxErr)
	}
	return stopErr
}

func (c *Component) clearLifecycleHandles() {
	c.subscriptions = nil
	c.consumers = nil
	c.cancel = nil
}

// IsStarted returns whether the component is running
func (c *Component) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (c *Component) isJetStreamPortBySubject(subject string) bool {
	for _, port := range c.outputPorts {
		facts, err := port.Facts()
		if err == nil && len(facts.NATSSubjects()) == 1 && facts.NATSSubjects()[0] == subject {
			return facts.Kind() == component.PortKindJetStream
		}
	}
	return false
}

// documentConsumerConfig preserves the document processor's replay-safe consumer defaults.
func documentConsumerConfig(port component.Port) (component.ConsumerConfig, error) {
	consumerCfg, err := component.GetConsumerConfig(port)
	if err != nil {
		return component.ConsumerConfig{}, err
	}
	facts, err := port.Facts()
	if err != nil {
		return component.ConsumerConfig{}, err
	}
	stream, ok := facts.Stream()
	if !ok {
		return component.ConsumerConfig{}, fmt.Errorf("port kind %q does not declare JetStream consumer configuration", facts.Kind())
	}
	if stream.DeliverPolicy() == "" {
		consumerCfg.DeliverPolicy = "all"
	}
	if stream.MaxDeliver() == 0 {
		consumerCfg.MaxDeliver = 5
	}
	return consumerCfg, nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (c *Component) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok || len(stream.Subjects()) != 1 {
		return fmt.Errorf("port %s must declare one JetStream subject", port.Name)
	}
	streamName := stream.Name()
	subject := stream.Subjects()[0]

	// Wait for stream to be available
	waitForStream := c.waitForStream
	if c.waitForStreamInput != nil {
		waitForStream = c.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return fmt.Errorf("stream %s not available: %w", streamName, err)
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("document-processor-%s", sanitizedSubject)

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)
	consumerCfg, err := documentConsumerConfig(port)
	if err != nil {
		return fmt.Errorf("resolve consumer config for port %q: %w", port.Name, err)
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		MaxAckPending: consumerCfg.MaxAckPending,
		AutoCreate:    false,
	}

	consumeStream := c.natsClient.ConsumeStreamWithConfig
	if c.consumeStream != nil {
		consumeStream = c.consumeStream
	}
	handle, err := consumeStream(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleJetStreamMessage(msgCtx, msg)
	})
	if err != nil {
		return fmt.Errorf("consumer setup failed for stream %s: %w", streamName, err)
	}
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, streamConsumerBinding{handle: handle})
	c.lifecycleMu.Unlock()

	return nil
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	// Retry with backoff
	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			c.logger.Debug("Stream available", "stream", streamName)
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

	return fmt.Errorf("stream %s not available after %d retries", streamName, maxRetries)
}

// handleJetStreamMessage handles JetStream messages and delegates to handleMessage
func (c *Component) handleJetStreamMessage(ctx context.Context, msg jetstream.Msg) {
	// Process the message using existing logic
	c.handleMessage(ctx, msg.Data())

	// Acknowledge the message
	if err := msg.Ack(); err != nil {
		c.logger.Error("Failed to ack JetStream message",
			"component", c.name,
			"error", err)
	}
}

// handleMessage processes incoming document JSON messages.
func (c *Component) handleMessage(ctx context.Context, msgData []byte) {
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	c.logger.Debug("Received message",
		"component", c.name,
		"size_bytes", len(msgData))

	// Parse incoming JSON into map
	var data map[string]any
	if err := json.Unmarshal(msgData, &data); err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Debug("Failed to parse message as JSON",
			"component", c.name,
			"error", err)
		return
	}

	// Use domain processor to transform data
	payload, err := c.processor.Process(data)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Failed to process document data",
			"component", c.name,
			"error", err)
		return
	}

	// Store content for ContentStorable payloads (if contentStore is configured)
	if c.contentStore != nil {
		if err := c.storeContentIfNeeded(ctx, payload); err != nil {
			c.logger.Error("Failed to store content",
				"component", c.name,
				"entity_id", payload.EntityID(),
				"error", err)
			// Continue processing - content storage is optional
		}
	}

	// Determine message type and payload based on concrete type
	var msgType message.Type
	var msgPayload message.Payload
	switch p := payload.(type) {
	case *Document:
		msgType = message.Type{Domain: "content", Category: "document", Version: "v1"}
		msgPayload = p
	case *Maintenance:
		msgType = message.Type{Domain: "content", Category: "maintenance", Version: "v1"}
		msgPayload = p
	case *Observation:
		msgType = message.Type{Domain: "content", Category: "observation", Version: "v1"}
		msgPayload = p
	case *SensorDocument:
		msgType = message.Type{Domain: "content", Category: "sensor_doc", Version: "v1"}
		msgPayload = p
	default:
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Unknown payload type from processor",
			"component", c.name,
			"type", fmt.Sprintf("%T", payload))
		return
	}

	// Wrap payload in BaseMessage for transport
	baseMsg := message.NewBaseMessage(
		msgType,
		msgPayload,
		c.name, // source component name
	)

	// Marshal the BaseMessage
	wrappedData, err := json.Marshal(baseMsg)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Failed to marshal BaseMessage",
			"component", c.name,
			"error", err)
		return
	}

	atomic.AddInt64(&c.messagesWrapped, 1)

	c.logger.Debug("Message wrapped in BaseMessage with Document payload",
		"component", c.name,
		"output_subject", c.outputSubj,
		"original_size", len(msgData),
		"wrapped_size", len(wrappedData),
		"entity_id", payload.EntityID())

	// Publish to output subject
	if c.outputSubj != "" {
		var publishErr error
		if c.isJetStreamPortBySubject(c.outputSubj) {
			publishErr = c.natsClient.PublishToStream(ctx, c.outputSubj, wrappedData)
		} else {
			publishErr = c.natsClient.Publish(ctx, c.outputSubj, wrappedData)
		}
		if publishErr != nil {
			atomic.AddInt64(&c.errors, 1)
			c.logger.Error("Failed to publish wrapped message",
				"component", c.name,
				"output_subject", c.outputSubj,
				"error", publishErr)
		} else {
			c.logger.Debug("Published wrapped message",
				"component", c.name,
				"output_subject", c.outputSubj)
		}
	}
}

// storeContentIfNeeded stores content for ContentStorable payloads and sets StorageRef.
// This enables the "process → store → graph" pattern where large content is stored
// separately from triples.
func (c *Component) storeContentIfNeeded(ctx context.Context, payload interface {
	EntityID() string
}) error {
	// Type switch to detect ContentStorable and call SetStorageRef
	switch p := payload.(type) {
	case *Document:
		ref, err := c.contentStore.StoreContent(ctx, p)
		if err != nil {
			return err
		}
		p.SetStorageRef(ref)
		atomic.AddInt64(&c.contentStored, 1)
		c.logger.Debug("Stored document content",
			"entity_id", p.EntityID(),
			"storage_key", ref.Key)
	case *Maintenance:
		ref, err := c.contentStore.StoreContent(ctx, p)
		if err != nil {
			return err
		}
		p.SetStorageRef(ref)
		atomic.AddInt64(&c.contentStored, 1)
		c.logger.Debug("Stored maintenance content",
			"entity_id", p.EntityID(),
			"storage_key", ref.Key)
	case *Observation:
		ref, err := c.contentStore.StoreContent(ctx, p)
		if err != nil {
			return err
		}
		p.SetStorageRef(ref)
		atomic.AddInt64(&c.contentStored, 1)
		c.logger.Debug("Stored observation content",
			"entity_id", p.EntityID(),
			"storage_key", ref.Key)
	case *SensorDocument:
		ref, err := c.contentStore.StoreContent(ctx, p)
		if err != nil {
			return err
		}
		p.SetStorageRef(ref)
		atomic.AddInt64(&c.contentStored, 1)
		c.logger.Debug("Stored sensor document content",
			"entity_id", p.EntityID(),
			"storage_key", ref.Key)
	}
	return nil
}

// SetContentStore sets the ObjectStore for content storage.
// When set, ContentStorable payloads will have their content stored before publishing.
func (c *Component) SetContentStore(store *objectstore.Store) {
	c.contentStore = store
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        c.name,
		Type:        "processor",
		Description: "Transforms incoming JSON documents into Graphable payloads",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports this processor subscribes to.
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputPorts...)
}

// OutputPorts returns the NATS output port for Graphable documents.
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputPorts...)
}

// ConfigSchema returns the configuration schema for this processor.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return documentSchema
}

// Health returns the current health status of this processor.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&c.errors)),
		Uptime:     time.Since(c.startTime),
	}
}

// DataFlow returns current data flow metrics for this processor.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	processed := atomic.LoadInt64(&c.messagesProcessed)
	errorCount := atomic.LoadInt64(&c.errors)

	var errorRate float64
	if processed > 0 {
		errorRate = float64(errorCount) / float64(processed)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}
