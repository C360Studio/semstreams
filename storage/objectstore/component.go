// Package objectstore provides a NATS ObjectStore-based storage component
// for immutable message storage with time-bucketed keys and caching.
package objectstore

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
	inputPorts      []component.Port
	outputPorts     []component.Port
	portsByName     map[string]component.Port
	portKinds       map[string]component.PortKind
	portSubjects    map[string]string

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
	inputPorts, outputPorts, portsByName, portKinds, portSubjects, err := resolveObjectStorePorts(cfg, instanceName)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve ports")
	}

	return &Component{
		instanceName:    instanceName,
		enabled:         true,
		config:          cfg,
		decoder:         message.NewDecoder(deps.PayloadRegistry),
		natsClient:      deps.NATSClient,
		metricsRegistry: deps.MetricsRegistry,
		logger:          deps.GetLogger(),
		inputPorts:      inputPorts,
		outputPorts:     outputPorts,
		portsByName:     portsByName,
		portKinds:       portKinds,
		portSubjects:    portSubjects,
	}, nil
}

func resolveObjectStorePorts(cfg Config, instanceName string) ([]component.Port, []component.Port, map[string]component.Port, map[string]component.PortKind, map[string]string, error) {
	if cfg.Ports == nil {
		return nil, nil, nil, nil, nil, errors.New("ports configuration is required")
	}
	byName := make(map[string]component.Port, len(cfg.Ports.Inputs)+len(cfg.Ports.Outputs)+1)
	kinds := make(map[string]component.PortKind, len(byName))
	subjects := make(map[string]string, len(byName))
	resolve := func(definitions []component.PortDefinition, direction component.Direction, allowed map[component.PortKind]bool) ([]component.Port, error) {
		ports := make([]component.Port, len(definitions))
		for index, definition := range definitions {
			port, err := definition.Resolve(direction)
			if err != nil {
				return nil, err
			}
			if _, duplicate := byName[port.Name]; duplicate {
				return nil, fmt.Errorf("duplicate port name %q", port.Name)
			}
			facts, err := port.Facts()
			if err != nil {
				return nil, err
			}
			if !allowed[facts.Kind()] {
				return nil, fmt.Errorf("port %q kind %q is not supported", port.Name, facts.Kind())
			}
			portSubjects := facts.NATSSubjects()
			if len(portSubjects) != 1 {
				return nil, fmt.Errorf("port %q declares %d subjects, want one", port.Name, len(portSubjects))
			}
			ports[index] = port
			byName[port.Name] = port
			kinds[port.Name] = facts.Kind()
			subjects[port.Name] = portSubjects[0]
		}
		return ports, nil
	}
	inputs, err := resolve(cfg.Ports.Inputs, component.DirectionInput, map[component.PortKind]bool{
		component.PortKindNATS: true, component.PortKindNATSRequest: true, component.PortKindJetStream: true,
	})
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	outputs, err := resolve(cfg.Ports.Outputs, component.DirectionOutput, map[component.PortKind]bool{
		component.PortKindNATS: true, component.PortKindJetStream: true,
	})
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	provider, err := (component.PortDefinition{
		Name: "store-provide", Description: "Owns the store instance addressable as StorageInstance=" + instanceName,
		Config: component.StoreProvidePort{Instance: instanceName},
	}).Resolve(component.DirectionOutput)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	outputs = append(outputs, provider)
	return inputs, outputs, byName, kinds, subjects, nil
}

// startStoreError classifies a store-constructor error for Component.Start,
// PRESERVING its class. The D2 retention guard (reconcileNoLifecycleRetention,
// #600/#616) returns a FATAL ErrGraphBucketRetention when a content store's backing
// stream keeps lifecycle eviction it cannot strip — that must fail Start CLOSED. Every
// other constructor error stays transient (retryable). errs.IsFatal inspects the
// OUTERMOST classification, so an unconditional WrapTransient here would silently
// downgrade the fatal (the #632 defect); the IsFatal branch is load-bearing. Returns
// nil for a nil error. Extracted so Start's fail-closed decision is directly testable.
func startStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errs.IsFatal(err) {
		return errs.WrapFatal(err, "Component", "Start", "create object store")
	}
	return errs.WrapTransient(err, "Component", "Start", "create object store")
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

	// Thread this component's logger so the D2 retention-reconcile guard (#600)
	// attributes its boot WARN to this store rather than the process default.
	c.config.Logger = c.logger

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
		return startStoreError(err)
	}
	c.store = store

	c.logger.Debug("ObjectStore created successfully", "name", c.instanceName, "bucket", c.config.BucketName)

	// Get raw NATS connection for subscriptions
	nc := c.natsClient.GetConnection()

	// Subscribe to API requests (Request/Response pattern)
	if c.hasPort("api") {
		apiSubject := c.getPortSubject("api")
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
		writeSubject := c.getPortSubject("write")
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
// Stores message and emits StoredMessage with StorageRef for downstream processors.
// Core NATS has no ack/redelivery semantics, so a failed write can only be
// surfaced — logged once here (the shared processWriteMessage returns errors
// rather than logging, per the return-vs-log convention).
func (c *Component) handleWriteRequest(msg *nats.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.processWriteMessage(ctx, msg.Data); err != nil {
		c.logger.Error("Failed to process write message",
			slog.String("subject", msg.Subject),
			slog.String("error", err.Error()))
	}
}

// emitStoredMessage emits a StoredMessage with StorageRef for downstream
// semantic processing, from the decode result processWriteMessage already
// holds (a single decode feeds both the storage key and this emit, so the
// two can never disagree about what the message is — #741).
//
// Returns nil for by-design skips (no "stored" port configured, message not a
// BaseMessage, payload not Graphable) — those messages never owe downstream a
// StorageReference. Returns a classified error when a REQUIRED publication
// fails (marshal or publish): the "stored" port is configured and the payload
// qualifies, so downstream losing the reference is the same loss shape as a
// failed store — the caller must not ack the delivery (#727). Errors are
// returned, not logged here; the transport caller logs once.
func (c *Component) emitStoredMessage(
	baseMsg *message.BaseMessage, decodeErr error, data []byte, storageKey string,
) error {
	if !c.hasPort("stored") {
		return nil // No stored output port configured
	}

	if decodeErr != nil {
		c.logger.Debug("Message not a BaseMessage, skipping StoredMessage emit",
			slog.String("error", decodeErr.Error()))
		return nil
	}

	// Extract Graphable payload
	payload := baseMsg.Payload()
	graphable, ok := payload.(graph.Graphable)
	if !ok {
		c.logger.Debug("Payload not Graphable, skipping StoredMessage emit",
			slog.String("payload_type", fmt.Sprintf("%T", payload)))
		return nil
	}

	// Create StorageReference
	storageRef := &message.StorageReference{
		StorageInstance: c.instanceName,
		Key:             storageKey,
		ContentType:     "application/json",
		Size:            int64(len(data)),
	}

	if err := c.publishStoredMessage(graphable, storageRef, baseMsg.Type().Key()); err != nil {
		return err
	}

	c.logger.Debug("Emitted StoredMessage",
		slog.String("entity_id", graphable.EntityID()),
		slog.String("storage_key", storageKey))
	return nil
}

// emitStoredMessageFromContentStorable emits a StoredMessage for ContentStorable payloads
// This is used when we've already stored via StoreContent and have a proper StorageRef.
//
// Same contract as emitStoredMessage: nil for by-design skips (no "stored"
// port, ContentStorable not Graphable), a classified error when the required
// publication fails so the caller can refuse the ack (#727).
func (c *Component) emitStoredMessageFromContentStorable(
	baseMsg *message.BaseMessage,
	cs message.ContentStorable,
	storageRef *message.StorageReference,
) error {
	if !c.hasPort("stored") {
		return nil
	}

	// ContentStorable must also be Graphable for downstream processing
	graphable, ok := cs.(graph.Graphable)
	if !ok {
		c.logger.Debug("ContentStorable not Graphable, skipping StoredMessage emit",
			slog.String("entity_id", cs.EntityID()))
		return nil
	}

	if err := c.publishStoredMessage(graphable, storageRef, baseMsg.Type().Key()); err != nil {
		return err
	}

	c.logger.Debug("Emitted StoredMessage for ContentStorable",
		slog.String("entity_id", cs.EntityID()),
		slog.String("storage_key", storageRef.Key))
	return nil
}

// publishStoredMessage wraps a Graphable + StorageReference in a StoredMessage
// envelope and publishes it on the "stored" port. Shared tail of the two emit
// paths. Marshal failure is Invalid (retrying the same message cannot fix it);
// publish failure is Transient (broker or stream unavailability).
func (c *Component) publishStoredMessage(
	graphable graph.Graphable,
	storageRef *message.StorageReference,
	originalType string,
) error {
	storedMsg := NewStoredMessage(graphable, storageRef, originalType)

	// Wrap in BaseMessage for transport
	wrappedMsg := message.NewBaseMessage(
		storedMsg.Schema(),
		storedMsg,
		c.instanceName, // source
	)

	msgData, err := wrappedMsg.MarshalJSON()
	if err != nil {
		return errs.WrapInvalid(err, "Component", "publishStoredMessage", "marshal StoredMessage")
	}

	storedSubject := c.getPortSubject("stored")

	// Use JetStream publishing when port type is "jetstream" for durability
	if c.isJetStreamPort("stored") {
		if err := c.natsClient.PublishToStream(context.Background(), storedSubject, msgData); err != nil {
			return errs.WrapTransient(err, "Component", "publishStoredMessage",
				fmt.Sprintf("publish StoredMessage to JetStream subject %s", storedSubject))
		}
		return nil
	}
	// Fallback to core NATS for non-JetStream ports
	if err := c.natsClient.GetConnection().Publish(storedSubject, msgData); err != nil {
		return errs.WrapTransient(err, "Component", "publishStoredMessage",
			fmt.Sprintf("publish StoredMessage to subject %s", storedSubject))
	}
	return nil
}

// publishEvent publishes a simple storage event to the events subject
func (c *Component) publishEvent(event Event) {
	if !c.hasPort("events") {
		return // No events port configured
	}

	eventSubject := c.getPortSubject("events")
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
	_, found := c.portsByName[name]
	return found
}

// isJetStreamPort checks if an output port is configured for JetStream
func (c *Component) isJetStreamPort(portName string) bool {
	return c.portKinds[portName] == component.PortKindJetStream
}

// isJetStreamInputPort checks if an input port is configured for JetStream
func (c *Component) isJetStreamInputPort(portName string) bool {
	return c.portKinds[portName] == component.PortKindJetStream
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (c *Component) setupJetStreamConsumer(ctx context.Context, portName, subject string) error {
	port, found := c.portsByName[portName]
	if !found || port.Direction != component.DirectionInput {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "setupJetStreamConsumer", fmt.Sprintf("port %s not found", portName))
	}
	facts, err := port.Facts()
	if err != nil {
		return errs.WrapInvalid(err, "Component", "setupJetStreamConsumer", "project input port facts")
	}
	stream, ok := facts.Stream()
	if !ok {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "setupJetStreamConsumer", fmt.Sprintf("port %s is not JetStream", portName))
	}

	streamName := stream.Name()

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
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "ObjectStoreComponent", "setupJetStreamConsumer", "resolve consumer config")
	}
	if stream.DeliverPolicy() == "" {
		consumerCfg.DeliverPolicy = "all"
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	err = c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
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

// Nak delays mirror the documented house precedent in
// natsclient.ConsumeWithHeartbeat (natsclient/heartbeat.go): 30s breathing room
// before retrying failed work, 5s for graceful shutdown/cancellation. They are
// deliberately NOT operator-configurable (#727).
const (
	transientNakDelay = 30 * time.Second
	shutdownNakDelay  = 5 * time.Second
)

// writeDisposition is the ack decision for one JetStream write delivery.
type writeDisposition int

const (
	dispositionAck          writeDisposition = iota // positive ack: work fully committed
	dispositionNakShutdown                          // NakWithDelay(shutdownNakDelay): cancelled mid-flight
	dispositionTerm                                 // Term: structurally invalid, retry can never succeed
	dispositionNakTransient                         // NakWithDelay(transientNakDelay): transient or unclassified
)

// classifyWriteDisposition maps a processWriteMessage outcome onto the ack
// decision table:
//
//	nil error                  -> Ack
//	ctx cancelled + any error  -> NakWithDelay(5s)  (graceful shutdown)
//	errs.IsInvalid             -> Term              (poison message, do not retry)
//	anything else              -> NakWithDelay(30s) (transient/unclassified: retry)
//
// The delays and the Term/NAK split follow natsclient.ConsumeWithHeartbeat,
// with one DELIBERATE divergence: heartbeat lets cancellation own the outcome
// even when work returned nil (NAK -> redeliver -> duplicate), because its
// select can race a success report against ctx.Done(). Here a nil error means
// the store commit and any required emit fully completed, so nil Acks even
// under a cancelled context — NAKing completed work would guarantee a
// duplicate object on redelivery for zero durability gain.
//
// For NON-nil errors, cancellation is checked BEFORE the invalid class
// (heartbeat precedent: cancellation owns the delivery outcome;
// select-race-on-pre-cancelled-ctx discipline): an error observed under a
// cancelled context may be the cancellation itself surfacing through the
// store, and terminating it would permanently drop a retryable message during
// shutdown. Unclassified errors deliberately fall to the transient NAK —
// retrying is the safe default; terminating loses data.
func classifyWriteDisposition(ctxErr, procErr error) writeDisposition {
	if procErr == nil {
		return dispositionAck
	}
	if ctxErr != nil {
		return dispositionNakShutdown
	}
	if errs.IsInvalid(procErr) {
		return dispositionTerm
	}
	return dispositionNakTransient
}

// handleJetStreamWriteRequest handles JetStream messages for write operations.
//
// Delivery contract: at-least-once. The delivery is positively acked ONLY after
// the store commit AND any required StorageReference publication both succeed;
// failures NAK (transient) or Term (structurally invalid) per
// classifyWriteDisposition. Redelivery after a partial failure re-runs the
// store. On the raw lane, DefaultKeyGenerator keys every write with a
// per-write UUID nonce (#741), so a redelivered raw message ALWAYS stores a
// NEW object rather than overwriting — a visible duplicate; duplicates beat
// loss (#727). (Before #741 the seconds-granularity key suffix made
// same-second redelivery an idempotent overwrite only by the same collision
// that silently LOST distinct same-second messages.) On the ContentStorable
// lane, Store.generateContentKey still keys with UnixNano: a redelivery
// landing on the same clock reading overwrites rather than duplicates — but
// that key embeds the entity ID, so the casualty is the SAME entity's
// just-written content with identical bytes (an idempotent overwrite), never
// a distinct message.
//
// Retry is BOUNDED, not indefinite: the write consumer's canonical port
// declaration defaults MaxDeliver to 3, which caps delivery
// attempts — roughly 60s of backend-outage tolerance at the 30s transient NAK
// delay — after which the message parks un-acked awaiting operator action;
// parked-message visibility is tracked as the MaxDeliver parking follow-up.
func (c *Component) handleJetStreamWriteRequest(ctx context.Context, msg jetstream.Msg) {
	atomic.AddUint64(&c.messagesReceived, 1)
	c.lastActivity.Store(time.Now())

	c.settleJetStreamWrite(ctx, msg, c.processWriteMessage(ctx, msg.Data()))
}

// settleJetStreamWrite applies the ack decision for one delivery and logs the
// processing error exactly once (processWriteMessage returns errors rather
// than logging, per the return-vs-log convention). Split from
// handleJetStreamWriteRequest so the decision table is drivable with a fake
// jetstream.Msg in unit tests.
func (c *Component) settleJetStreamWrite(ctx context.Context, msg jetstream.Msg, procErr error) {
	switch classifyWriteDisposition(ctx.Err(), procErr) {
	case dispositionAck:
		if err := msg.Ack(); err != nil {
			c.logger.Error("Failed to ack JetStream message",
				slog.String("error", err.Error()))
		}
	case dispositionNakShutdown:
		// Same NAK verb for both cancellation causes, different visibility:
		// DeadlineExceeded here is the per-message processing deadline
		// (natsclient's messageHandlerContext default, 30s) expiring over a
		// hung backend — a real failure class that can silently park the
		// message once MaxDeliver exhausts, so it must be loud. Canceled is
		// genuine shutdown/restart noise and stays Debug.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.logger.Error("Write processing deadline exceeded; NAK for redelivery",
				slog.String("subject", msg.Subject()),
				slog.String("error", procErr.Error()))
		} else {
			c.logger.Debug("Write processing cancelled; NAK for redelivery",
				slog.String("subject", msg.Subject()),
				slog.String("error", procErr.Error()))
		}
		_ = msg.NakWithDelay(shutdownNakDelay)
	case dispositionTerm:
		c.logger.Error("Terminating JetStream write delivery: structurally invalid message",
			slog.String("subject", msg.Subject()),
			slog.String("error", procErr.Error()))
		if err := msg.Term(); err != nil {
			c.logger.Error("Failed to terminate JetStream message",
				slog.String("error", err.Error()))
		}
	case dispositionNakTransient:
		c.logger.Error("Failed to process JetStream write; NAK for redelivery",
			slog.String("subject", msg.Subject()),
			slog.String("error", procErr.Error()))
		_ = msg.NakWithDelay(transientNakDelay)
	}
}

// processWriteMessage contains the shared logic for processing write messages.
// Used by both core NATS and JetStream handlers. Returns the store or
// required-emit error (already classified and attributed by the store/emit
// layer) so each transport caller can apply its own delivery semantics — the
// JetStream handler's ack decision, the core NATS handler's log-and-drop.
// Errors are returned, not logged here (return-vs-log convention).
func (c *Component) processWriteMessage(ctx context.Context, data []byte) error {
	// Try to parse as BaseMessage to check for ContentStorable payload
	baseMsg, decodeErr := c.decoder.Decode(data)
	if decodeErr == nil {
		// Successfully parsed - check if payload is ContentStorable
		if cs, ok := baseMsg.Payload().(message.ContentStorable); ok {
			// Use StoreContent for proper key generation and StoredContent envelope
			storageRef, err := c.store.StoreContent(ctx, cs)
			if err != nil {
				return err
			}

			atomic.AddUint64(&c.messagesStored, 1)

			// Publish storage event (best-effort monitoring signal, never gates the ack)
			c.publishEvent(Event{
				Type:      "stored",
				Key:       storageRef.Key,
				Timestamp: time.Now(),
			})

			// Emit StoredMessage with proper StorageRef — a REQUIRED publication
			// when the "stored" port is configured; its failure gates the ack.
			return c.emitStoredMessageFromContentStorable(baseMsg, cs, storageRef)
		}
	}

	// Fallback: store raw bytes for non-ContentStorable messages. The stored
	// payload stays the ORIGINAL wire bytes (re-marshaling the decoded
	// envelope would base64-encode []byte fields and corrupt the JSON), but
	// the KEY derives from the decoded envelope when decode succeeded:
	// keying from the opaque bytes sent every decodable-but-not-
	// ContentStorable message — the PRIMARY lane for JSONMap-style outputs —
	// into message/.../unknown_<ts>, where two distinct messages in the same
	// instant collided and ObjectStore Put silently replaced the first
	// (#741). Only true undecodable bytes take the unknown key family, which
	// DefaultKeyGenerator disambiguates with a per-write UUID nonce (a clock
	// is never a nonce — the wall clock feeds only the date partitions).
	var keySource any = data
	if decodeErr == nil {
		keySource = baseMsg
	}
	key, err := c.store.storeWithKeySource(ctx, keySource, data)
	if err != nil {
		return err
	}

	atomic.AddUint64(&c.messagesStored, 1)

	// Publish simple storage event (best-effort monitoring signal, never gates the ack)
	c.publishEvent(Event{
		Type:      "stored",
		Key:       key,
		Timestamp: time.Now(),
	})

	// Emit StoredMessage if we have a "stored" output port — required
	// publication when configured; decode/non-Graphable skips return nil.
	return c.emitStoredMessage(baseMsg, decodeErr, data, key)
}

// getPortSubject gets the canonical subject for a configured named port.
func (c *Component) getPortSubject(portName string) string {
	return c.portSubjects[portName]
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
	return append([]component.Port(nil), c.inputPorts...)
}

// OutputPorts returns the output ports for this component
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputPorts...)
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
