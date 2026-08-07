// Package jsongeneric provides a core processor for wrapping plain JSON into GenericJSONPayload
package jsongeneric

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

// Config holds configuration for JSON generic processor
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration for JSON generic processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "raw.>",
			Required:    true,
			Description: "NATS subjects with plain JSON data",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "nats_output",
			Type:        "nats",
			Subject:     "generic.messages",
			Interface:   "core .json.v1", // Output GenericJSON
			Required:    true,
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
	name       string
	subjects   []string
	outputSubj string
	config     Config // Store full config for port type checking
	natsClient *natsclient.Client
	logger     *slog.Logger

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

	// Extract subjects from port configuration
	var inputSubjects []string
	var outputSubject string

	for _, input := range config.Ports.Inputs {
		if input.Type == "nats" || input.Type == "jetstream" {
			inputSubjects = append(inputSubjects, input.Subject)
		}
	}

	if len(config.Ports.Outputs) > 0 {
		outputSubject = config.Ports.Outputs[0].Subject
	}

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "JSONGenericProcessor", "NewJSONGenericProcessor",
			"no input subjects configured")
	}

	return &Processor{
		name:       "json-generic-processor",
		subjects:   inputSubjects,
		outputSubj: outputSubject,
		config:     config, // Store full config for port type checking
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		wg:         &sync.WaitGroup{},
	}, nil
}

// Initialize prepares the processor (no-op for JSON generic)
func (p *Processor) Initialize() error {
	return nil
}

// Start begins wrapping messages
func (p *Processor) Start(ctx context.Context) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "JSONGenericProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "JSONGenericProcessor", "Start", "context already cancelled")
	}

	if p.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "JSONGenericProcessor", "Start", "check running state")
	}

	if p.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "JSONGenericProcessor", "Start", "NATS client required")
	}

	// Recreate channels if component is being restarted
	p.shutdown = make(chan struct{})
	p.done = make(chan struct{})

	// Subscribe to input ports based on port type
	if err := p.setupSubscriptions(ctx); err != nil {
		return err
	}

	p.mu.Lock()
	p.running = true
	p.startTime = time.Now()
	p.mu.Unlock()

	p.logger.Info("JSON generic processor started",
		"component", p.name,
		"input_subjects", p.subjects,
		"output_subject", p.outputSubj)

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (p *Processor) setupSubscriptions(ctx context.Context) error {
	for _, port := range p.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

		switch port.Type {
		case "jetstream":
			if err := p.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "JSONGenericProcessor", "Start",
					fmt.Sprintf("JetStream consumer for %s", port.Subject))
			}

		case "nats":
			sub, err := p.natsClient.Subscribe(ctx, port.Subject, func(ctx context.Context, msg *nats.Msg) {
				p.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				p.logger.Error("Failed to subscribe to NATS subject",
					"component", p.name,
					"subject", port.Subject,
					"error", err)
				return errs.WrapTransient(err, "JSONGenericProcessor", "Start",
					fmt.Sprintf("subscribe to %s", port.Subject))
			}
			p.subscriptions = append(p.subscriptions, sub)
			p.logger.Debug("Subscribed to NATS subject successfully",
				"component", p.name,
				"subject", port.Subject,
				"output_subject", p.outputSubj)

		default:
			p.logger.Warn("Unknown port type, skipping", "port", port.Name, "type", port.Type)
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (p *Processor) setupJetStreamConsumer(ctx context.Context, port component.PortDefinition) error {
	streamName := port.StreamName
	if streamName == "" {
		streamName = p.deriveStreamName(port.Subject)
	}
	if streamName == "" {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig, "JSONGenericProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("derive stream name for subject %s", port.Subject))
	}

	if err := p.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "JSONGenericProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("stream %s availability", streamName))
	}

	sanitizedSubject := strings.ReplaceAll(port.Subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("json-generic-%s", sanitizedSubject)

	p.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject)

	// Get consumer config from port definition (allows user configuration)
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

	err := p.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		p.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			p.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "JSONGenericProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("consumer setup for stream %s", streamName))
	}

	p.logger.Debug("JSON generic subscribed (JetStream)", "subject", port.Subject, "stream", streamName)
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

// deriveStreamName extracts stream name from subject convention
func (p *Processor) deriveStreamName(subject string) string {
	subject = strings.TrimPrefix(subject, "*.")
	subject = strings.TrimSuffix(subject, ".>")
	subject = strings.TrimSuffix(subject, ".*")

	parts := strings.Split(subject, ".")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "*" || parts[0] == ">" {
		return ""
	}
	return strings.ToUpper(parts[0])
}

// Stop gracefully stops the processor
func (p *Processor) Stop(timeout time.Duration) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if !p.running {
		return nil
	}

	// Signal shutdown
	close(p.shutdown)

	// Unsubscribe from all NATS subjects
	for _, sub := range p.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			p.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	p.subscriptions = nil

	// Wait for goroutines with timeout
	waitCh := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errs.WrapTransient(
			errs.ErrConnectionTimeout, "JSONGenericProcessor", "Stop",
			fmt.Sprintf("graceful shutdown (timeout after %v)", timeout))
	}

	p.mu.Lock()
	p.running = false
	close(p.done)
	p.mu.Unlock()

	return nil
}

// IsStarted returns whether the processor is running
func (p *Processor) IsStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (p *Processor) isJetStreamPortBySubject(subject string) bool {
	if p.config.Ports == nil {
		return false
	}
	for _, port := range p.config.Ports.Outputs {
		if port.Subject == subject {
			return port.Type == "jetstream"
		}
	}
	return false
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
	ports := make([]component.Port, len(p.subjects))
	for i, subj := range p.subjects {
		ports[i] = component.Port{
			Name:      fmt.Sprintf("input_%d", i),
			Direction: component.DirectionInput,
			Required:  true,
			Config: component.NATSPort{
				Subject: subj,
			},
		}
	}
	return ports
}

// OutputPorts returns the NATS output port for wrapped GenericJSON messages.
func (p *Processor) OutputPorts() []component.Port {
	if p.outputSubj == "" {
		return []component.Port{}
	}
	return []component.Port{
		{
			Name:      "output",
			Direction: component.DirectionOutput,
			Required:  false,
			Config: component.NATSPort{
				Subject: p.outputSubj,
				Interface: &component.InterfaceContract{
					Type:    "core .json.v1",
					Version: "v1",
				},
			},
		},
	}
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
