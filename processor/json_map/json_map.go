// Package jsonmapprocessor provides a core processor for transforming GenericJSON message fields
package jsonmapprocessor

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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config holds configuration for JSON map processor
type Config struct {
	Ports        *component.PortConfig `json:"ports"         schema:"type:ports,description:Port configuration,category:basic"`
	Mappings     []FieldMapping        `json:"mappings"      schema:"type:array,description:Field mappings,category:basic"`
	AddFields    map[string]any        `json:"add_fields"    schema:"type:object,description:Static fields"`
	RemoveFields []string              `json:"remove_fields" schema:"type:array,description:Field removal"`
}

// FieldMapping defines a single field transformation
type FieldMapping struct {
	SourceField string `json:"source_field" schema:"type:string,description:Source field,required:true"`
	TargetField string `json:"target_field" schema:"type:string,description:Target field,required:true"`
	Transform   string `json:"transform"    schema:"type:enum,enum:copy|uppercase|lowercase|trim,description:Type"`
}

// DefaultConfig returns the default configuration for JSON map processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "raw.>", Interface: &component.InterfaceContract{
				// Require GenericJSON
				Type: "core .json.v1"}}, Required: true,
			Description: "NATS subjects to transform (must be GenericJSON payloads)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "nats_output", Config: component.NATSPort{Subject: "mapped.messages", Interface: &component.InterfaceContract{
				// Output GenericJSON
				Type: "core .json.v1"}}, Required: true,
			Description: "NATS subject for transformed messages",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		Mappings:     []FieldMapping{},
		AddFields:    make(map[string]any),
		RemoveFields: []string{},
	}
}

// jsonMapSchema defines the configuration schema for JSON map processor
var jsonMapSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Processor implements a GenericJSON message field transformer
type Processor struct {
	name               string
	subjects           []string
	outputSubj         string
	mappings           []FieldMapping
	addFields          map[string]any
	removeFields       map[string]bool // Set for fast lookup
	config             Config          // Store full config for port type checking
	inputPorts         []component.Port
	outputPorts        []component.Port
	jetStreamOutputs   map[string]bool
	decoder            *message.Decoder
	natsClient         *natsclient.Client
	logger             *slog.Logger
	waitForStreamInput func(context.Context, string) error
	consumeStream      func(
		context.Context,
		natsclient.PortConsumerContext,
		natsclient.StreamConsumerConfig,
		func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error)

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

	// Metrics (atomic counters for DataFlow)
	messagesProcessed   int64
	messagesTransformed int64
	errors              int64
	lastActivity        time.Time

	// Prometheus metrics
	metrics *mapMetrics
}

type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

// NewProcessor creates a new JSON map processor from configuration
func NewProcessor(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "JSONMapProcessor", "NewProcessor", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}
	if len(config.Ports.Outputs) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "JSONMapProcessor", "NewProcessor", "exactly one output port is required")
	}

	inputPorts, inputSubjects, err := resolveMessagePorts(config.Ports.Inputs, component.DirectionInput)
	if err != nil {
		return nil, errs.WrapInvalid(err, "JSONMapProcessor", "NewProcessor", "resolve input ports")
	}
	outputPorts, outputSubjects, err := resolveMessagePorts(config.Ports.Outputs, component.DirectionOutput)
	if err != nil {
		return nil, errs.WrapInvalid(err, "JSONMapProcessor", "NewProcessor", "resolve output ports")
	}
	outputSubject := outputSubjects[0]

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "JSONMapProcessor", "NewProcessor",
			"no input subjects configured")
	}
	jetStreamOutputs := make(map[string]bool, len(outputPorts))
	for _, port := range outputPorts {
		facts, factsErr := port.Facts()
		if factsErr != nil {
			return nil, errs.WrapInvalid(factsErr, "JSONMapProcessor", "NewProcessor", "project output port facts")
		}
		jetStreamOutputs[facts.NATSSubjects()[0]] = facts.Kind() == component.PortKindJetStream
	}

	// Convert removeFields to set for fast lookup
	removeFieldsSet := make(map[string]bool)
	for _, field := range config.RemoveFields {
		removeFieldsSet[field] = true
	}

	// Initialize metrics if registry provided
	metrics, err := newMapMetrics(deps.MetricsRegistry, "json-map-processor")
	if err != nil {
		deps.GetLogger().Error("Failed to initialize JSON map metrics", "error", err)
		metrics = nil // Continue without metrics
	}

	return &Processor{
		name:             "json-map-processor",
		subjects:         inputSubjects,
		outputSubj:       outputSubject,
		mappings:         config.Mappings,
		addFields:        config.AddFields,
		removeFields:     removeFieldsSet,
		config:           config, // Store full config for port type checking
		inputPorts:       inputPorts,
		outputPorts:      outputPorts,
		jetStreamOutputs: jetStreamOutputs,
		decoder:          message.NewDecoder(deps.PayloadRegistry),
		natsClient:       deps.NATSClient,
		logger:           deps.GetLogger(),
		metrics:          metrics,
	}, nil
}

func resolveMessagePorts(definitions []component.PortDefinition, direction component.Direction) ([]component.Port, []string, error) {
	ports := make([]component.Port, len(definitions))
	var subjects []string
	for index, definition := range definitions {
		port, err := definition.Resolve(direction)
		if err != nil {
			return nil, nil, err
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, nil, err
		}
		if facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream {
			return nil, nil, fmt.Errorf("port %q kind %q does not carry messages", port.Name, facts.Kind())
		}
		portSubjects := facts.NATSSubjects()
		if len(portSubjects) != 1 {
			return nil, nil, fmt.Errorf("port %q declares %d NATS subjects, want exactly one", port.Name, len(portSubjects))
		}
		ports[index] = port
		subjects = append(subjects, portSubjects[0])
	}
	return ports, subjects, nil
}

// Initialize prepares the processor (no-op for JSON map)
func (m *Processor) Initialize() error {
	return nil
}

// Start begins transforming messages
func (m *Processor) Start(ctx context.Context) (startErr error) {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "JSONMapProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "JSONMapProcessor", "Start", "context already cancelled")
	}

	m.lifecycleMu.Lock()
	if m.lifecycleUsed {
		m.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "JSONMapProcessor", "Start", "check running state")
	}
	if m.natsClient == nil {
		m.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrMissingConfig, "JSONMapProcessor", "Start", "NATS client required")
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	m.lifecycleUsed = true
	m.cleanupPending = true
	m.cancel = cancel
	m.startDone = startDone
	m.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, m.cleanupFailedStart)
			startErr = errors.Join(startErr, rollbackErr)
			m.lifecycleMu.Lock()
			if rollbackErr == nil {
				m.cleanupPending = false
				m.terminal = true
				m.clearLifecycleHandles()
			}
			close(startDone)
			m.startDone = nil
			m.lifecycleMu.Unlock()
			return
		}
		m.lifecycleMu.Lock()
		m.cleanupPending = false
		close(startDone)
		m.startDone = nil
		m.lifecycleMu.Unlock()
	}()

	// Subscribe to input ports based on port type
	if err := m.setupSubscriptions(runCtx); err != nil {
		return err
	}

	m.mu.Lock()
	m.running = true
	m.startTime = time.Now()
	m.mu.Unlock()
	committed = true

	m.logger.Info("JSON map processor started",
		"component", m.name,
		"input_subjects", m.subjects,
		"output_subject", m.outputSubj,
		"mappings", len(m.mappings),
		"add_fields", len(m.addFields),
		"remove_fields", len(m.removeFields))

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (m *Processor) setupSubscriptions(ctx context.Context) error {
	for _, port := range m.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "JSONMapProcessor", "Start", "project input port facts")
		}
		subject := facts.NATSSubjects()[0]

		switch facts.Kind() {
		case component.PortKindJetStream:
			if err := m.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "JSONMapProcessor", "Start",
					fmt.Sprintf("JetStream consumer for %s", subject))
			}

		case component.PortKindNATS:
			sub, err := m.natsClient.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
				m.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				m.logger.Error("Failed to subscribe to NATS subject",
					"component", m.name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "JSONMapProcessor", "Start",
					fmt.Sprintf("subscribe to %s", subject))
			}
			m.subscriptions = append(m.subscriptions, sub)
			m.logger.Debug("Subscribed to NATS subject successfully",
				"component", m.name,
				"subject", subject,
				"output_subject", m.outputSubj,
				"mappings_count", len(m.mappings))

		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (m *Processor) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok {
		return fmt.Errorf("port %q does not declare JetStream facts", port.Name)
	}
	subject := facts.NATSSubjects()[0]
	streamName := stream.Name()

	waitForStream := m.waitForStream
	if m.waitForStreamInput != nil {
		waitForStream = m.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "JSONMapProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("wait for stream %s", streamName))
	}

	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("json-map-%s", sanitizedSubject)

	m.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "JSONMapProcessor", "setupJetStreamConsumer", "resolve consumer config")
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

	consumeStream := m.natsClient.ConsumeStreamWithConfigHandle
	if m.consumeStream != nil {
		consumeStream = m.consumeStream
	}
	handle, err := consumeStream(ctx, natsclient.PortConsumerContext{Component: m.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		m.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			m.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "JSONMapProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("consumer setup for stream %s", streamName))
	}
	m.lifecycleMu.Lock()
	m.consumers = append(m.consumers, streamConsumerBinding{handle: handle})
	m.lifecycleMu.Unlock()

	m.logger.Debug("JSON map subscribed (JetStream)", "subject", subject, "stream", streamName)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (m *Processor) waitForStream(ctx context.Context, streamName string) error {
	js, err := m.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "JSONMapProcessor", "waitForStream", "get JetStream context")
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
		errs.ErrMaxRetriesExceeded, "JSONMapProcessor", "waitForStream",
		fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries))
}

// Stop gracefully stops the processor
func (m *Processor) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		m.lifecycleMu.Lock()
		if !m.lifecycleUsed {
			m.lifecycleUsed = true
			m.terminal = true
			m.lifecycleMu.Unlock()
			return nil
		}
		if m.terminal {
			m.lifecycleMu.Unlock()
			return nil
		}
		if m.startDone != nil {
			startDone := m.startDone
			m.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if m.stopping {
			m.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "JSONMapProcessor", "Stop", "concurrent Stop is unsupported")
		}
		retryable := m.cleanupPending
		m.stopping = true
		m.lifecycleMu.Unlock()

		stopErr := m.cleanup(ctx)
		m.lifecycleMu.Lock()
		m.stopping = false
		if retryable && stopErr != nil {
			m.lifecycleMu.Unlock()
			return stopErr
		}
		m.cleanupPending = false
		m.terminal = true
		m.clearLifecycleHandles()
		m.lifecycleMu.Unlock()
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		return stopErr
	}
}

func (m *Processor) cleanupFailedStart(ctx context.Context) error { return m.cleanup(ctx) }

func (m *Processor) cleanup(ctx context.Context) error {
	var stopErr error
	for _, sub := range m.subscriptions {
		stopErr = errors.Join(stopErr, sub.Drain(ctx))
	}
	for index := range m.consumers {
		binding := &m.consumers[index]
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
	if m.cancel != nil {
		m.cancel()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		stopErr = errors.Join(stopErr, ctxErr)
	}
	return stopErr
}

func (m *Processor) clearLifecycleHandles() {
	m.subscriptions = nil
	m.consumers = nil
	m.cancel = nil
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (m *Processor) isJetStreamPortBySubject(subject string) bool {
	return m.jetStreamOutputs[subject]
}

// handleMessage processes incoming GenericJSON messages
func (m *Processor) handleMessage(ctx context.Context, msgData []byte) {
	atomic.AddInt64(&m.messagesProcessed, 1)
	m.mu.Lock()
	m.lastActivity = time.Now()
	m.mu.Unlock()

	m.logger.Debug("Received message",
		"component", m.name,
		"size_bytes", len(msgData))

	// Parse as BaseMessage
	baseMsg, err := m.decoder.Decode(msgData)
	if err != nil {
		atomic.AddInt64(&m.errors, 1)
		m.metrics.recordError(m.name, "parse")
		m.logger.Debug("Failed to parse message as BaseMessage",
			"component", m.name,
			"error", err)
		return
	}

	// Extract GenericJSON payload
	payload := baseMsg.Payload()
	genericJSON, ok := payload.(*message.GenericJSONPayload)
	if !ok {
		atomic.AddInt64(&m.errors, 1)
		m.metrics.recordError(m.name, "type")
		m.logger.Debug("Payload is not GenericJSON",
			"component", m.name,
			"actual_type", fmt.Sprintf("%T", payload))
		return
	}

	// Validate the payload
	if err := genericJSON.Validate(); err != nil {
		atomic.AddInt64(&m.errors, 1)
		m.metrics.recordError(m.name, "validation")
		m.logger.Debug("Message validation failed",
			"component", m.name,
			"error", err)
		return
	}

	// Apply transformations to GenericJSON.Data with timing
	start := time.Now()
	transformed := m.transformMessage(genericJSON.Data)
	duration := time.Since(start)
	atomic.AddInt64(&m.messagesTransformed, 1)

	m.logger.Debug("Message transformed",
		"component", m.name,
		"output_subject", m.outputSubj,
		"original_fields", len(genericJSON.Data),
		"transformed_fields", len(transformed),
		"transformation_time_us", duration.Microseconds())

	// Create new GenericJSON payload with transformed data
	newPayload := message.NewGenericJSON(transformed)

	// Wrap in BaseMessage for transport (enforces clean architecture)
	outputMsg := message.NewBaseMessage(
		newPayload.Schema(), // message type: "core.json.v1"
		newPayload,          // the GenericJSONPayload (already a pointer)
		m.name,              // source component name
	)

	// Marshal and publish
	if m.outputSubj != "" {
		transformedData, err := json.Marshal(outputMsg)
		if err != nil {
			atomic.AddInt64(&m.errors, 1)
			m.metrics.recordError(m.name, "marshal")
			m.logger.Error("Failed to marshal BaseMessage",
				"component", m.name,
				"error", err)
			return
		}

		// Record transformation metrics
		m.metrics.recordTransformation(m.name, duration, len(transformedData))

		var publishErr error
		if m.isJetStreamPortBySubject(m.outputSubj) {
			publishErr = m.natsClient.PublishToStream(ctx, m.outputSubj, transformedData)
		} else {
			publishErr = m.natsClient.Publish(ctx, m.outputSubj, transformedData)
		}
		if publishErr != nil {
			atomic.AddInt64(&m.errors, 1)
			m.metrics.recordError(m.name, "publish")
			m.logger.Error("Failed to publish transformed message",
				"component", m.name,
				"output_subject", m.outputSubj,
				"error", publishErr)
		} else {
			m.logger.Debug("Published BaseMessage with transformed GenericJSON payload",
				"component", m.name,
				"output_subject", m.outputSubj)
		}
	}
}

// transformMessage applies all transformations to a message
func (m *Processor) transformMessage(data map[string]any) map[string]any {
	result := make(map[string]any)

	// Count field operations
	removedCount := 0
	mappedCount := 0

	// Copy existing fields (excluding ones to be removed)
	for key, value := range data {
		if !m.removeFields[key] {
			result[key] = value
		} else {
			removedCount++
		}
	}

	// Apply field mappings
	for _, mapping := range m.mappings {
		if value, exists := data[mapping.SourceField]; exists {
			transformedValue := m.applyTransform(value, mapping.Transform)
			result[mapping.TargetField] = transformedValue
			mappedCount++
			m.metrics.recordFieldExtraction(m.name)

			// Remove source if it's different from target
			if mapping.SourceField != mapping.TargetField {
				delete(result, mapping.SourceField)
			}
		} else {
			m.metrics.recordExtractionError(m.name, "missing_field")
		}
	}

	// Add static fields
	addedCount := len(m.addFields)
	for key, value := range m.addFields {
		result[key] = value
	}

	// Record field operations
	m.metrics.recordFieldOperations(m.name, addedCount, removedCount, mappedCount)

	return result
}

// applyTransform applies a transformation to a value
func (m *Processor) applyTransform(value any, transform string) any {
	if transform == "" || transform == "copy" {
		return value
	}

	// Only apply string transforms to string values
	strValue, ok := value.(string)
	if !ok {
		return value
	}

	switch transform {
	case "uppercase":
		return toUpperCase(strValue)
	case "lowercase":
		return toLowerCase(strValue)
	case "trim":
		return trimSpaces(strValue)
	default:
		return value
	}
}

// Simple string helpers to avoid imports
func toUpperCase(s string) string {
	result := make([]rune, len(s))
	for i, r := range s {
		if r >= 'a' && r <= 'z' {
			result[i] = r - 32
		} else {
			result[i] = r
		}
	}
	return string(result)
}

func toLowerCase(s string) string {
	result := make([]rune, len(s))
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + 32
		} else {
			result[i] = r
		}
	}
	return string(result)
}

func trimSpaces(s string) string {
	start := 0
	end := len(s)

	// Trim leading spaces
	for start < end && s[start] == ' ' {
		start++
	}

	// Trim trailing spaces
	for end > start && s[end-1] == ' ' {
		end--
	}

	return s[start:end]
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component.
func (m *Processor) Meta() component.Metadata {
	return component.Metadata{
		Name:        m.name,
		Type:        "processor",
		Description: "GenericJSON (core .json.v1) field transformer",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports this processor subscribes to.
func (m *Processor) InputPorts() []component.Port {
	return append([]component.Port(nil), m.inputPorts...)
}

// OutputPorts returns the NATS output port for transformed messages.
func (m *Processor) OutputPorts() []component.Port {
	return append([]component.Port(nil), m.outputPorts...)
}

// ConfigSchema returns the configuration schema for this processor.
func (m *Processor) ConfigSchema() component.ConfigSchema {
	return jsonMapSchema
}

// Health returns the current health status of this processor.
func (m *Processor) Health() component.HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    m.running,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&m.errors)),
		Uptime:     time.Since(m.startTime),
	}
}

// DataFlow returns current data flow metrics for this processor.
func (m *Processor) DataFlow() component.FlowMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	processed := atomic.LoadInt64(&m.messagesProcessed)
	errorCount := atomic.LoadInt64(&m.errors)

	var errorRate float64
	if processed > 0 {
		errorRate = float64(errorCount) / float64(processed)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      m.lastActivity,
	}
}

// Register registers the JSON map processor component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "json_map",
		Factory:     NewProcessor,
		Schema:      jsonMapSchema,
		Type:        "processor",
		Protocol:    "json_map",
		Domain:      "processing",
		Description: "GenericJSON (core .json.v1) field transformer for renaming, adding, and removing fields",
		Version:     "0.1.0",
	})
}
