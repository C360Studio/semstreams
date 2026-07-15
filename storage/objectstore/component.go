// Package objectstore provides a NATS ObjectStore-based storage component
// for immutable message storage with time-bucketed keys and caching.
package objectstore

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

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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

	// Mutex to protect concurrent access to state fields
	mu      sync.RWMutex
	started bool

	// core dependencies
	store           *Store
	decoder         *message.Decoder
	natsClient      *natsclient.Client
	metricsRegistry *metric.MetricsRegistry
	config          Config
	logger          *slog.Logger

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

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
var _ component.StoreProvider = (*Component)(nil)

// ProvidedStores exposes this component's live store to the ComponentManager for
// registration in the shared StoreRegistry (ADR-063). Keyed by the store's
// stamped StorageInstance (store.InstanceName()) — the same value it writes into
// every StorageReference, so consumers resolving a ref land on this store.
// Returns nil before Start (no store yet).
func (c *Component) ProvidedStores() map[string]storage.StreamableStore {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return nil
	}
	return map[string]storage.StreamableStore{c.store.InstanceName(): c.store}
}

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
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
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
		instanceName:    instanceName,
		enabled:         true,
		config:          cfg,
		decoder:         message.NewDecoder(deps.PayloadRegistry),
		natsClient:      deps.NATSClient,
		metricsRegistry: deps.MetricsRegistry,
		logger:          deps.GetLogger(),
	}, nil
}

// Start initializes the ObjectStore and sets up NATS handlers
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		c.logger.Debug("ObjectStore already started", "name", c.instanceName)
		return nil
	}

	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "Start", "context cannot be nil")
	}

	c.logger.Debug("Creating ObjectStore", "name", c.instanceName, "bucket", c.config.BucketName)

	// gh#400: thread the component instance name into the store so its
	// StoreContent path stamps the SAME StorageInstance as this component's
	// StoredMessage emit path (which already uses c.instanceName). Without this
	// the store would fall back to the bucket name and the two write paths would
	// disagree, leaving a resolver unable to resolve one of them.
	c.config.InstanceName = c.instanceName

	// Create the underlying ObjectStore with metrics support
	store, err := NewStoreWithConfigAndMetrics(ctx, c.natsClient, c.config, c.metricsRegistry)
	if store != nil {
		store.SetDecoder(c.decoder)
	}
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
		return errs.WrapTransient(err, "Component", "Start", "create object store")
	}
	c.store = store

	c.logger.Debug("ObjectStore created successfully", "name", c.instanceName, "bucket", c.config.BucketName)

	// Initialize lifecycle reporter (throttled for high-throughput storing)
	statusBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMPONENT_STATUS",
		Description: "Component lifecycle status tracking",
	})
	if err != nil {
		c.logger.Warn("Failed to create COMPONENT_STATUS bucket, lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
	} else {
		c.lifecycleReporter = component.NewLifecycleReporterFromConfig(component.LifecycleReporterConfig{
			KV:               statusBucket,
			ComponentName:    "objectstore",
			Logger:           c.logger,
			EnableThrottling: true,
		})
	}

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
			return errs.WrapTransient(err, "Component", "Start", fmt.Sprintf("subscribe to API subject %s", apiSubject))
		}
	}

	// Subscribe to write requests (async fire-and-forget)
	// Check port type to determine subscription method (JetStream vs core NATS)
	if c.hasPort("write") {
		writeSubject := c.getPortSubject("write", "storage.%s.write")
		c.logger.Debug("Subscribing to write subject", "name", c.instanceName, "subject", writeSubject)

		if c.isJetStreamInputPort("write") {
			// JetStream subscription - use durable consumer
			if err := c.setupJetStreamConsumer(ctx, "write", writeSubject); err != nil {
				return errs.WrapTransient(err, "Component", "Start", "setup JetStream consumer for write")
			}
		} else {
			// Core NATS subscription
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
				return errs.WrapTransient(err, "Component", "Start", fmt.Sprintf("subscribe to write subject %s", writeSubject))
			}
		}
	}

	// NOTE: Stream creation is handled centrally by config.StreamsManager
	// which derives streams from component port definitions at startup.
	// Components no longer need to create their own streams.

	c.started = true
	c.lastActivity.Store(time.Now())
	c.logger.Debug("ObjectStore component fully started", "name", c.instanceName)

	// Report initial idle state
	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
			c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
		}
	}

	return nil
}

// Stop cleanly shuts down the component
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	// Close underlying store first to clean up cache resources
	if c.store != nil {
		if err := c.store.Close(); err != nil {
			return errs.WrapTransient(err, "Component", "Stop", "close store")
		}
	}

	// Then unsubscribe from NATS
	if c.apiSub != nil {
		if err := c.apiSub.Unsubscribe(); err != nil {
			return errs.WrapTransient(err, "Component", "Stop", "unsubscribe from API")
		}
	}

	if c.writeSub != nil {
		if err := c.writeSub.Unsubscribe(); err != nil {
			return errs.WrapTransient(err, "Component", "Stop", "unsubscribe from write")
		}
	}

	c.started = false
	return nil
}

// IsStarted returns whether the component is running
func (c *Component) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// handleAPIRequest handles synchronous Request/Response operations
func (c *Component) handleAPIRequest(msg *nats.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	var req Request
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.respondWithError(msg, errs.WrapInvalid(err, "Component", "handleAPIRequest", "unmarshal request"))
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
			c.respondWithError(msg, errs.WrapInvalid(err, "Component", "handleAPIRequest", "unmarshal data"))
			return
		}

		// Report storing stage (throttled)
		c.reportStoring(ctx)

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
		c.respondWithError(msg, errs.WrapInvalid(errs.ErrInvalidData, "Component", "handleAPIRequest", fmt.Sprintf("unknown action: %s", req.Action)))
	}
}

// handleWriteRequest handles async write operations via core NATS
// Stores message and emits StoredMessage with StorageRef for downstream processors
func (c *Component) handleWriteRequest(msg *nats.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c.processWriteMessage(ctx, msg.Data)
}

// emitStoredMessage attempts to parse the incoming message and emit a StoredMessage
// with StorageRef for downstream semantic processing
func (c *Component) emitStoredMessage(data []byte, storageKey string) {
	if !c.hasPort("stored") {
		return // No stored output port configured
	}

	// Try to parse as BaseMessage to extract Graphable payload
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Debug("Message not a BaseMessage, skipping StoredMessage emit",
			slog.String("error", err.Error()))
		return
	}

	// Extract Graphable payload
	payload := baseMsg.Payload()
	graphable, ok := payload.(graph.Graphable)
	if !ok {
		c.logger.Debug("Payload not Graphable, skipping StoredMessage emit",
			slog.String("payload_type", fmt.Sprintf("%T", payload)))
		return
	}

	// Create StorageReference
	storageRef := &message.StorageReference{
		StorageInstance: c.instanceName,
		Key:             storageKey,
		ContentType:     "application/json",
		Size:            int64(len(data)),
	}

	// Create StoredMessage wrapping original Graphable + StorageRef
	storedMsg := NewStoredMessage(graphable, storageRef, baseMsg.Type().Key())

	// Wrap in BaseMessage for transport
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(),
		storedMsg,
		c.instanceName, // source
	)

	// Marshal and publish
	msgData, err := wrappedMsg.MarshalJSON()
	if err != nil {
		c.logger.Error("Failed to marshal StoredMessage",
			slog.String("error", err.Error()))
		return
	}

	storedSubject := c.getPortSubject("stored", "storage.%s.stored")

	// Use JetStream publishing when port type is "jetstream" for durability
	if c.isJetStreamPort("stored") {
		if err := c.natsClient.PublishToStream(context.Background(), storedSubject, msgData); err != nil {
			c.logger.Error("Failed to publish StoredMessage to JetStream",
				slog.String("subject", storedSubject),
				slog.String("error", err.Error()))
			return
		}
	} else {
		// Fallback to core NATS for non-JetStream ports
		if err := c.natsClient.GetConnection().Publish(storedSubject, msgData); err != nil {
			c.logger.Error("Failed to publish StoredMessage",
				slog.String("subject", storedSubject),
				slog.String("error", err.Error()))
			return
		}
	}

	c.logger.Debug("Emitted StoredMessage",
		slog.String("entity_id", graphable.EntityID()),
		slog.String("storage_key", storageKey),
		slog.String("subject", storedSubject))
}

// emitStoredMessageFromContentStorable emits a StoredMessage for ContentStorable payloads
// This is used when we've already stored via StoreContent and have a proper StorageRef
func (c *Component) emitStoredMessageFromContentStorable(
	baseMsg *message.BaseMessage,
	cs message.ContentStorable,
	storageRef *message.StorageReference,
) {
	if !c.hasPort("stored") {
		return
	}

	// ContentStorable must also be Graphable for downstream processing
	graphable, ok := cs.(graph.Graphable)
	if !ok {
		c.logger.Debug("ContentStorable not Graphable, skipping StoredMessage emit",
			slog.String("entity_id", cs.EntityID()))
		return
	}

	// Create StoredMessage wrapping original Graphable + StorageRef
	storedMsg := NewStoredMessage(graphable, storageRef, baseMsg.Type().Key())

	// Wrap in BaseMessage for transport
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(),
		storedMsg,
		c.instanceName,
	)

	// Marshal and publish
	msgData, err := wrappedMsg.MarshalJSON()
	if err != nil {
		c.logger.Error("Failed to marshal StoredMessage",
			slog.String("error", err.Error()))
		return
	}

	storedSubject := c.getPortSubject("stored", "storage.%s.stored")

	// Use JetStream publishing when port type is "jetstream" for durability
	if c.isJetStreamPort("stored") {
		if err := c.natsClient.PublishToStream(context.Background(), storedSubject, msgData); err != nil {
			c.logger.Error("Failed to publish StoredMessage to JetStream",
				slog.String("subject", storedSubject),
				slog.String("error", err.Error()))
			return
		}
	} else {
		if err := c.natsClient.GetConnection().Publish(storedSubject, msgData); err != nil {
			c.logger.Error("Failed to publish StoredMessage",
				slog.String("subject", storedSubject),
				slog.String("error", err.Error()))
			return
		}
	}

	c.logger.Debug("Emitted StoredMessage for ContentStorable",
		slog.String("entity_id", cs.EntityID()),
		slog.String("storage_key", storageRef.Key),
		slog.String("subject", storedSubject))
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

// isJetStreamPort checks if an output port is configured for JetStream
func (c *Component) isJetStreamPort(portName string) bool {
	if c.config.Ports == nil {
		return false
	}
	for _, port := range c.config.Ports.Outputs {
		if port.Name == portName {
			return port.Type == "jetstream"
		}
	}
	return false
}

// isJetStreamInputPort checks if an input port is configured for JetStream
func (c *Component) isJetStreamInputPort(portName string) bool {
	if c.config.Ports == nil {
		return false
	}
	for _, port := range c.config.Ports.Inputs {
		if port.Name == portName {
			return port.Type == "jetstream"
		}
	}
	return false
}

// getInputPortDef returns the port definition for an input port
func (c *Component) getInputPortDef(portName string) *component.PortDefinition {
	if c.config.Ports == nil {
		return nil
	}
	for _, port := range c.config.Ports.Inputs {
		if port.Name == portName {
			return &port
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (c *Component) setupJetStreamConsumer(ctx context.Context, portName, subject string) error {
	portDef := c.getInputPortDef(portName)
	if portDef == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "setupJetStreamConsumer", fmt.Sprintf("port %s not found", portName))
	}

	// Derive stream name from subject or use explicit stream name
	streamName := portDef.StreamName
	if streamName == "" {
		streamName = config.DeriveStreamName(subject)
	}
	if streamName == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "setupJetStreamConsumer", fmt.Sprintf("could not derive stream name for subject %s", subject))
	}

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Component", "setupJetStreamConsumer", fmt.Sprintf("stream %s not available", streamName))
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("objectstore-%s-%s", c.instanceName, sanitizedSubject)

	c.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration).
	// objectstore is idempotent (content-addressed storage overwrites), so it
	// defaults to "all": it MUST catch up on messages published before its
	// consumer bound. A JSON config that omits deliver_policy would otherwise
	// fall to the framework "new" default and silently drop the first document
	// (the startup first-message race). An explicit deliver_policy still wins.
	consumerCfg := component.GetConsumerConfigFromDefinitionWithDefault(*portDef, "all")

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleJetStreamWriteRequest(msgCtx, msg)
	})
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupJetStreamConsumer", fmt.Sprintf("consumer setup failed for stream %s", streamName))
	}

	return nil
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "Component", "waitForStream", "get JetStream context")
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

	return errs.WrapTransient(errs.ErrStorageUnavailable, "Component", "waitForStream", fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries))
}

// handleJetStreamWriteRequest handles JetStream messages for write operations
func (c *Component) handleJetStreamWriteRequest(ctx context.Context, msg jetstream.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	// Process the message using existing logic
	c.processWriteMessage(ctx, msg.Data())

	// Acknowledge the message
	if err := msg.Ack(); err != nil {
		c.logger.Error("Failed to ack JetStream message",
			slog.String("error", err.Error()))
	}
}

// processWriteMessage contains the shared logic for processing write messages
// Used by both core NATS and JetStream handlers
func (c *Component) processWriteMessage(ctx context.Context, data []byte) {
	// Report storing stage (throttled)
	c.reportStoring(ctx)

	// Try to parse as BaseMessage to check for ContentStorable payload
	if baseMsg, err := c.decoder.Decode(data); err == nil {
		// Successfully parsed - check if payload is ContentStorable
		if cs, ok := baseMsg.Payload().(message.ContentStorable); ok {
			// Use StoreContent for proper key generation and StoredContent envelope
			storageRef, err := c.store.StoreContent(ctx, cs)
			if err != nil {
				c.logger.Error("Failed to store ContentStorable",
					slog.String("error", err.Error()))
				return
			}

			atomic.AddUint64(&c.messagesStored, 1)

			// Publish storage event
			c.publishEvent(Event{
				Type:      "stored",
				Key:       storageRef.Key,
				Timestamp: time.Now(),
			})

			// Emit StoredMessage with proper StorageRef
			c.emitStoredMessageFromContentStorable(baseMsg, cs, storageRef)
			return
		}
	}

	// Fallback: store raw bytes for non-ContentStorable messages
	key, err := c.store.Store(ctx, data)
	if err != nil {
		c.logger.Error("Failed to store message",
			slog.String("error", err.Error()))
		return
	}

	atomic.AddUint64(&c.messagesStored, 1)

	// Publish simple storage event (for monitoring/audit)
	c.publishEvent(Event{
		Type:      "stored",
		Key:       key,
		Timestamp: time.Now(),
	})

	// Try to emit StoredMessage if we have a "stored" output port
	c.emitStoredMessage(data, key)
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
	ports := make([]component.Port, 0)
	if c.config.Ports != nil {
		for _, portDef := range c.config.Ports.Outputs {
			// Use BuildPortFromDefinition to properly handle different port types (nats, jetstream, etc.)
			port := component.BuildPortFromDefinition(portDef, component.DirectionOutput)
			ports = append(ports, port)
		}
	}

	// Declare store ownership (ADR-063): this component owns the store instance
	// it stamps into every StorageReference (c.instanceName). Non-exclusive; the
	// ComponentManager reads ProvidedStores() to populate the shared
	// StoreRegistry that content-fetch consumers resolve against.
	ports = append(ports, component.Port{
		Name:        "store-provide",
		Direction:   component.DirectionOutput,
		Description: "Owns the store instance addressable as StorageInstance=" + c.instanceName,
		Config:      component.StoreProvidePort{Instance: c.instanceName},
	})

	return ports
}

// ConfigSchema returns the configuration schema for this component
// References the package-level objectstoreSchema variable for efficient retrieval
func (c *Component) ConfigSchema() component.ConfigSchema {
	return objectstoreSchema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	var lastAct time.Time
	if v := c.lastActivity.Load(); v != nil {
		lastAct = v.(time.Time)
	}

	return component.HealthStatus{
		Healthy:    started,
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

// reportStoring reports the storing stage (throttled to avoid KV spam)
func (c *Component) reportStoring(ctx context.Context) {
	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportStage(ctx, "storing"); err != nil {
			c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "storing"), slog.Any("error", err))
		}
	}
}
