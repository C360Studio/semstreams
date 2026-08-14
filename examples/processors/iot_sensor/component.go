package iotsensor

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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ComponentConfig holds configuration for the IoT sensor processor component.
// This wraps the domain-specific processor configuration with port information
// required by the component framework.
type ComponentConfig struct {
	// Ports defines NATS input/output subjects for message routing
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// OrgID is the organization identifier for federated entity IDs
	OrgID string `json:"org_id" schema:"type:string,description:Organization identifier,category:basic,required:true"`

	// Platform is the platform/product identifier for federated entity IDs
	Platform string `json:"platform" schema:"type:string,description:Platform identifier,category:basic,required:true"`
}

// DefaultConfig returns the default configuration for IoT sensor processor
func DefaultConfig() ComponentConfig {
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "raw.sensor.>"}, Required: true,
			Description: "NATS subjects with sensor JSON data",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "nats_output", Config: component.NATSPort{Subject: "events.graph.entity.sensor", Interface: &component.InterfaceContract{Type: "domain.iot.sensor.v1"}}, Required: true,
			Description: "NATS subject for Graphable sensor readings",
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

// iotSensorSchema defines the configuration schema for IoT sensor processor
var iotSensorSchema = component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

// Component wraps the domain-specific IoT sensor processor with component lifecycle.
// It bridges the gap between the stateless domain processor and the stateful
// component framework that handles NATS messaging and lifecycle management.
type Component struct {
	name        string
	subjects    []string
	outputSubj  string
	inputPorts  []component.Port
	outputPorts []component.Port
	config      ComponentConfig // Store full config for port type checking
	natsClient  *natsclient.Client
	logger      *slog.Logger

	// Domain processor (stateless, pure business logic)
	processor *Processor

	// Lifecycle management
	shutdown      chan struct{}
	done          chan struct{}
	running       bool
	startTime     time.Time
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	wg            *sync.WaitGroup
	subscriptions []*natsclient.Subscription

	// Metrics
	messagesProcessed int64
	messagesWrapped   int64
	errors            int64
	lastActivity      time.Time
}

// NewComponent creates a new IoT sensor processor component from configuration.
// This is the factory function registered with the component registry.
func NewComponent(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config ComponentConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "IoTSensorComponent", "NewComponent", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Validate configuration
	if config.OrgID == "" {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent",
			"OrgID is required")
	}

	if config.Platform == "" {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent",
			"Platform is required")
	}
	if len(config.Ports.Outputs) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent", "exactly one output port is required")
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	var outputSubject string
	inputPorts := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		input, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "IoTSensorComponent", "NewComponent", "resolve input port")
		}
		facts, err := input.Facts()
		if err != nil || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent", "input port must declare one NATS subject")
		}
		inputPorts = append(inputPorts, input)
		inputSubjects = append(inputSubjects, facts.NATSSubjects()[0])
	}
	outputPorts := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		output, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "IoTSensorComponent", "NewComponent", "resolve output port")
		}
		facts, err := output.Facts()
		if err != nil || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent", "output port must declare one NATS subject")
		}
		outputPorts = append(outputPorts, output)
	}
	facts, _ := outputPorts[0].Facts()
	outputSubject = facts.NATSSubjects()[0]

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "IoTSensorComponent", "NewComponent",
			"no input subjects configured")
	}

	// Create domain processor with organizational context
	processor := NewProcessor(Config{
		OrgID:    config.OrgID,
		Platform: config.Platform,
	})

	return &Component{
		name:        "iot-sensor-processor",
		subjects:    inputSubjects,
		outputSubj:  outputSubject,
		inputPorts:  inputPorts,
		outputPorts: outputPorts,
		config:      config, // Store full config for port type checking
		natsClient:  deps.NATSClient,
		logger:      deps.GetLogger(),
		processor:   processor,
		shutdown:    make(chan struct{}),
		done:        make(chan struct{}),
		wg:          &sync.WaitGroup{},
	}, nil
}

// Initialize prepares the component (no-op for IoT sensor processor)
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing sensor messages
func (c *Component) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	if c.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "IoTSensorComponent", "Start", "check running state")
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "IoTSensorComponent", "Start", "NATS client required")
	}

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
			if err := c.setupJetStreamConsumer(ctx, port); err != nil {
				c.logger.Error("Failed to setup JetStream consumer",
					"component", c.name,
					"port", port.Name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "IoTSensorComponent", "Start", fmt.Sprintf("setup JetStream consumer for %s", subject))
			}
		} else if facts.Kind() == component.PortKindNATS {
			// Core NATS subscription
			sub, err := c.natsClient.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
				c.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				c.logger.Error("Failed to subscribe to NATS subject",
					"component", c.name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "IoTSensorComponent", "Start", fmt.Sprintf("subscribe to %s", subject))
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

	c.logger.Info("IoT sensor processor started",
		"component", c.name,
		"input_subjects", c.subjects,
		"output_subject", c.outputSubj)

	return nil
}

// Stop gracefully stops the component
func (c *Component) Stop(timeout time.Duration) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	if !c.running {
		return nil
	}

	// Signal shutdown
	close(c.shutdown)

	// Unsubscribe from all NATS subjects
	for _, sub := range c.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			c.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	c.subscriptions = nil

	// Wait for goroutines with timeout
	waitCh := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errs.WrapTransient(
			fmt.Errorf("shutdown timeout after %v", timeout),
			"IoTSensorComponent", "Stop", "graceful shutdown")
	}

	c.mu.Lock()
	c.running = false
	close(c.done)
	c.mu.Unlock()

	return nil
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

// iotSensorConsumerConfig preserves the IoT processor's replay-safe consumer defaults.
func iotSensorConsumerConfig(port component.Port) (component.ConsumerConfig, error) {
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
	if err := c.waitForStream(ctx, streamName); err != nil {
		return fmt.Errorf("stream %s not available: %w", streamName, err)
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("iot-sensor-%s", sanitizedSubject)

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)
	consumerCfg, err := iotSensorConsumerConfig(port)
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

	err = c.natsClient.ConsumeStreamWithConfig(ctx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleJetStreamMessage(msgCtx, msg)
	})
	if err != nil {
		return fmt.Errorf("consumer setup failed for stream %s: %w", streamName, err)
	}

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

// handleMessage processes incoming sensor JSON messages.
// This is the bridge between NATS transport and domain logic:
//  1. Parse incoming JSON
//  2. Call domain processor (pure business logic)
//  3. Emit Zone entity (referenced entity - upsert)
//  4. Emit SensorReading entity
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
	reading, err := c.processor.Process(data)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Failed to process sensor data",
			"component", c.name,
			"error", err)
		return
	}

	// Emit Zone entity first (referenced entity - graph-ingest handles upsert)
	// This ensures the zone exists before the sensor that references it
	if reading.ZoneEntityID != "" {
		zoneType, zoneID := ParseZoneEntityID(reading.ZoneEntityID)
		if zoneType != "" && zoneID != "" {
			zone := &Zone{
				ZoneID:   zoneID,
				ZoneType: zoneType,
				Name:     zoneID, // Default name to zone ID
				OrgID:    reading.OrgID,
				Platform: reading.Platform,
			}
			c.emitGraphable(ctx, zone, message.Type{
				Domain:   "facility",
				Category: "zone",
				Version:  "v1",
			})
		}
	}

	// Emit SensorReading entity
	c.emitGraphable(ctx, reading, message.Type{
		Domain:   "iot",
		Category: "sensor",
		Version:  "v1",
	})
}

// graphablePayload combines Graphable and Payload interfaces for entities that implement both.
type graphablePayload interface {
	message.Payload
	EntityID() string
}

// emitGraphable wraps a Payload in BaseMessage and publishes to output subject.
func (c *Component) emitGraphable(ctx context.Context, payload graphablePayload, msgType message.Type) {
	baseMsg := message.NewBaseMessage(msgType, payload, c.name)

	wrappedData, err := json.Marshal(baseMsg)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Failed to marshal BaseMessage",
			"component", c.name,
			"entity_id", payload.EntityID(),
			"error", err)
		return
	}

	atomic.AddInt64(&c.messagesWrapped, 1)

	c.logger.Debug("Emitting entity",
		"component", c.name,
		"output_subject", c.outputSubj,
		"entity_id", payload.EntityID(),
		"type", msgType.String())

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
			c.logger.Error("Failed to publish entity",
				"component", c.name,
				"output_subject", c.outputSubj,
				"entity_id", payload.EntityID(),
				"error", publishErr)
		}
	}
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        c.name,
		Type:        "processor",
		Description: "Transforms incoming JSON sensor data into Graphable SensorReading payloads",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports this processor subscribes to.
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputPorts...)
}

// OutputPorts returns the NATS output port for Graphable sensor readings.
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputPorts...)
}

// ConfigSchema returns the configuration schema for this processor.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return iotSensorSchema
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
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}
