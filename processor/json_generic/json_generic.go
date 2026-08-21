// Package jsongeneric provides a core processor for wrapping plain JSON into GenericJSONPayload
package jsongeneric

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

// Config holds configuration for JSON generic processor
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration for JSON generic processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "raw.>"}, Required: true,
			Description: "NATS subjects with plain JSON data",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "nats_output", Config: component.NATSPort{Subject: "generic.messages", Interface: &component.InterfaceContract{
				// Output GenericJSON
				Type: "core .json.v1"}}, Required: true,
			Description: "NATS subject for GenericJSON wrapped messages",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
	}
}

// jsonGenericSchema defines the configuration schema for JSON generic processor
var jsonGenericSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Processor wraps plain JSON into GenericJSONPayload
type Processor struct {
	name               string
	subjects           []string
	outputSubj         string
	config             Config // Store full config for port type checking
	inputPorts         []component.Port
	outputPorts        []component.Port
	jetStreamOutputs   map[string]bool
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

	// Metrics
	messagesProcessed int64
	messagesWrapped   int64
	errors            int64
	lastActivity      time.Time
}

type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

// NewProcessor creates a new JSON generic processor from configuration
func NewProcessor(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "JSONGenericProcessor", "NewJSONGenericProcessor", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}
	if len(config.Ports.Outputs) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "JSONGenericProcessor", "NewJSONGenericProcessor", "exactly one output port is required")
	}

	inputPorts, inputSubjects, err := resolveMessagePorts(config.Ports.Inputs, component.DirectionInput)
	if err != nil {
		return nil, errs.WrapInvalid(err, "JSONGenericProcessor", "NewJSONGenericProcessor", "resolve input ports")
	}
	outputPorts, outputSubjects, err := resolveMessagePorts(config.Ports.Outputs, component.DirectionOutput)
	if err != nil {
		return nil, errs.WrapInvalid(err, "JSONGenericProcessor", "NewJSONGenericProcessor", "resolve output ports")
	}
	outputSubject := outputSubjects[0]

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "JSONGenericProcessor", "NewJSONGenericProcessor",
			"no input subjects configured")
	}
	jetStreamOutputs := make(map[string]bool, len(outputPorts))
	for _, port := range outputPorts {
		facts, factsErr := port.Facts()
		if factsErr != nil {
			return nil, errs.WrapInvalid(factsErr, "JSONGenericProcessor", "NewJSONGenericProcessor", "project output port facts")
		}
		jetStreamOutputs[facts.NATSSubjects()[0]] = facts.Kind() == component.PortKindJetStream
	}

	return &Processor{
		name:             "json-generic-processor",
		subjects:         inputSubjects,
		outputSubj:       outputSubject,
		config:           config, // Store full config for port type checking
		inputPorts:       inputPorts,
		outputPorts:      outputPorts,
		jetStreamOutputs: jetStreamOutputs,
		natsClient:       deps.NATSClient,
		logger:           deps.GetLogger(),
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

// Initialize prepares the processor (no-op for JSON generic)
func (p *Processor) Initialize() error {
	return nil
}

// Start begins wrapping messages
func (p *Processor) Start(ctx context.Context) (startErr error) {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "JSONGenericProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "JSONGenericProcessor", "Start", "context already cancelled")
	}
	p.lifecycleMu.Lock()
	if p.lifecycleUsed {
		p.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "JSONGenericProcessor", "Start", "check running state")
	}
	if p.natsClient == nil {
		p.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrMissingConfig, "JSONGenericProcessor", "Start", "NATS client required")
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	p.lifecycleUsed = true
	p.cleanupPending = true
	p.cancel = cancel
	p.startDone = startDone
	p.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, p.cleanupFailedStart)
			startErr = errors.Join(startErr, rollbackErr)
			p.lifecycleMu.Lock()
			if rollbackErr == nil {
				p.cleanupPending = false
				p.terminal = true
				p.clearLifecycleHandles()
			}
			close(startDone)
			p.startDone = nil
			p.lifecycleMu.Unlock()
			return
		}
		p.lifecycleMu.Lock()
		p.cleanupPending = false
		close(startDone)
		p.startDone = nil
		p.lifecycleMu.Unlock()
	}()

	// Subscribe to input ports based on port type
	if err := p.setupSubscriptions(runCtx); err != nil {
		return err
	}

	p.mu.Lock()
	p.running = true
	p.startTime = time.Now()
	p.mu.Unlock()
	committed = true

	p.logger.Info("JSON generic processor started",
		"component", p.name,
		"input_subjects", p.subjects,
		"output_subject", p.outputSubj)

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (p *Processor) setupSubscriptions(ctx context.Context) error {
	for _, port := range p.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "JSONGenericProcessor", "Start", "project input port facts")
		}
		subject := facts.NATSSubjects()[0]

		switch facts.Kind() {
		case component.PortKindJetStream:
			if err := p.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "JSONGenericProcessor", "Start",
					fmt.Sprintf("JetStream consumer for %s", subject))
			}

		case component.PortKindNATS:
			sub, err := p.natsClient.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
				p.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				p.logger.Error("Failed to subscribe to NATS subject",
					"component", p.name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "JSONGenericProcessor", "Start",
					fmt.Sprintf("subscribe to %s", subject))
			}
			p.subscriptions = append(p.subscriptions, sub)
			p.logger.Debug("Subscribed to NATS subject successfully",
				"component", p.name,
				"subject", subject,
				"output_subject", p.outputSubj)

		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (p *Processor) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
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

	waitForStream := p.waitForStream
	if p.waitForStreamInput != nil {
		waitForStream = p.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "JSONGenericProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("stream %s availability", streamName))
	}

	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("json-generic-%s", sanitizedSubject)

	p.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "JSONGenericProcessor", "setupJetStreamConsumer", "resolve consumer config")
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

	consumeStream := p.natsClient.ConsumeStreamWithConfig
	if p.consumeStream != nil {
		consumeStream = p.consumeStream
	}
	handle, err := consumeStream(ctx, natsclient.PortConsumerContext{Component: p.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		p.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			p.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "JSONGenericProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("consumer setup for stream %s", streamName))
	}
	p.lifecycleMu.Lock()
	p.consumers = append(p.consumers, streamConsumerBinding{handle: handle})
	p.lifecycleMu.Unlock()

	p.logger.Debug("JSON generic subscribed (JetStream)", "subject", subject, "stream", streamName)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (p *Processor) waitForStream(ctx context.Context, streamName string) error {
	js, err := p.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "JSONGenericProcessor", "waitForStream", "get JetStream context")
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
		errs.ErrStorageUnavailable, "JSONGenericProcessor", "waitForStream",
		fmt.Sprintf("stream %s availability after %d retries", streamName, maxRetries))
}

// Stop gracefully stops the processor
func (p *Processor) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		p.lifecycleMu.Lock()
		if !p.lifecycleUsed {
			p.lifecycleUsed = true
			p.terminal = true
			p.lifecycleMu.Unlock()
			return nil
		}
		if p.terminal {
			p.lifecycleMu.Unlock()
			return nil
		}
		if p.startDone != nil {
			startDone := p.startDone
			p.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if p.stopping {
			p.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "JSONGenericProcessor", "Stop", "concurrent Stop is unsupported")
		}
		retryable := p.cleanupPending
		p.stopping = true
		p.lifecycleMu.Unlock()

		stopErr := p.cleanup(ctx)
		p.lifecycleMu.Lock()
		p.stopping = false
		if retryable && stopErr != nil {
			p.lifecycleMu.Unlock()
			return stopErr
		}
		p.cleanupPending = false
		p.terminal = true
		p.clearLifecycleHandles()
		p.lifecycleMu.Unlock()
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		return stopErr
	}
}

func (p *Processor) cleanupFailedStart(ctx context.Context) error { return p.cleanup(ctx) }

func (p *Processor) cleanup(ctx context.Context) error {
	var stopErr error
	for _, sub := range p.subscriptions {
		stopErr = errors.Join(stopErr, sub.Drain(ctx))
	}
	for index := range p.consumers {
		binding := &p.consumers[index]
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
	if p.cancel != nil {
		p.cancel()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		stopErr = errors.Join(stopErr, ctxErr)
	}
	return stopErr
}

func (p *Processor) clearLifecycleHandles() {
	p.subscriptions = nil
	p.consumers = nil
	p.cancel = nil
}

// IsStarted returns whether the processor is running
func (p *Processor) IsStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (p *Processor) isJetStreamPortBySubject(subject string) bool {
	return p.jetStreamOutputs[subject]
}

// handleMessage processes incoming plain JSON messages and wraps them
func (p *Processor) handleMessage(ctx context.Context, msgData []byte) {
	atomic.AddInt64(&p.messagesProcessed, 1)
	p.mu.Lock()
	p.lastActivity = time.Now()
	p.mu.Unlock()

	p.logger.Debug("Received message",
		"component", p.name,
		"size_bytes", len(msgData))

	// Parse plain JSON into map
	var data map[string]any
	if err := json.Unmarshal(msgData, &data); err != nil {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Debug("Failed to parse message as JSON",
			"component", p.name,
			"error", err)
		return
	}

	// Wrap in GenericJSONPayload
	payload := message.NewGenericJSON(data)

	// Validate the wrapped payload
	if err := payload.Validate(); err != nil {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Error("Wrapped payload validation failed",
			"component", p.name,
			"error", err)
		return
	}

	// Wrap in BaseMessage for transport (enforces clean architecture)
	baseMsg := message.NewBaseMessage(
		payload.Schema(), // message type: "core.json.v1"
		payload,          // the GenericJSONPayload (already a pointer)
		p.name,           // source component name
	)

	// Marshal the BaseMessage (not the payload directly)
	wrappedData, err := json.Marshal(baseMsg)
	if err != nil {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Error("Failed to marshal BaseMessage",
			"component", p.name,
			"error", err)
		return
	}

	atomic.AddInt64(&p.messagesWrapped, 1)

	p.logger.Debug("Message wrapped in BaseMessage with GenericJSON payload",
		"component", p.name,
		"output_subject", p.outputSubj,
		"original_size", len(msgData),
		"wrapped_size", len(wrappedData))

	// Publish to output subject
	if p.outputSubj != "" {
		var publishErr error
		if p.isJetStreamPortBySubject(p.outputSubj) {
			publishErr = p.natsClient.PublishToStream(ctx, p.outputSubj, wrappedData)
		} else {
			publishErr = p.natsClient.Publish(ctx, p.outputSubj, wrappedData)
		}
		if publishErr != nil {
			atomic.AddInt64(&p.errors, 1)
			p.logger.Error("Failed to publish wrapped message",
				"component", p.name,
				"output_subject", p.outputSubj,
				"error", publishErr)
		} else {
			p.logger.Debug("Published wrapped message",
				"component", p.name,
				"output_subject", p.outputSubj)
		}
	}
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component.
func (p *Processor) Meta() component.Metadata {
	return component.Metadata{
		Name:        p.name,
		Type:        "processor",
		Description: "Wraps plain JSON into GenericJSON (core .json.v1) format",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports this processor subscribes to.
func (p *Processor) InputPorts() []component.Port {
	return append([]component.Port(nil), p.inputPorts...)
}

// OutputPorts returns the NATS output port for wrapped GenericJSON messages.
func (p *Processor) OutputPorts() []component.Port {
	return append([]component.Port(nil), p.outputPorts...)
}

// ConfigSchema returns the configuration schema for this processor.
func (p *Processor) ConfigSchema() component.ConfigSchema {
	return jsonGenericSchema
}

// Health returns the current health status of this processor.
func (p *Processor) Health() component.HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    p.running,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&p.errors)),
		Uptime:     time.Since(p.startTime),
	}
}

// DataFlow returns current data flow metrics for this processor.
func (p *Processor) DataFlow() component.FlowMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	processed := atomic.LoadInt64(&p.messagesProcessed)
	errorCount := atomic.LoadInt64(&p.errors)

	var errorRate float64
	if processed > 0 {
		errorRate = float64(errorCount) / float64(processed)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      p.lastActivity,
	}
}

// Register registers the JSON generic processor component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "json_generic",
		Factory:     NewProcessor,
		Schema:      jsonGenericSchema,
		Type:        "processor",
		Protocol:    "json_generic",
		Domain:      "processing",
		Description: "Wraps plain JSON into GenericJSON (core .json.v1) format",
		Version:     "0.1.0",
	})
}
