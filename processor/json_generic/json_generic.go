// Package jsongeneric provides a core processor for wrapping plain JSON into GenericJSONPayload
package jsongeneric

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
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
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Lifecycle management
	shutdown    chan struct{}
	done        chan struct{}
	running     bool
	startTime   time.Time
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	wg          *sync.WaitGroup

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
		return nil, errors.WrapInvalid(err, "JSONGenericProcessor", "NewJSONGenericProcessor", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	var outputSubject string

	for _, input := range config.Ports.Inputs {
		if input.Type == "nats" {
			inputSubjects = append(inputSubjects, input.Subject)
		}
	}

	if len(config.Ports.Outputs) > 0 {
		outputSubject = config.Ports.Outputs[0].Subject
	}

	if len(inputSubjects) == 0 {
		return nil, errors.WrapInvalid(
			errors.ErrInvalidConfig, "JSONGenericProcessor", "NewJSONGenericProcessor",
			"no input subjects configured")
	}

	return &Processor{
		name:       "json-generic-processor",
		subjects:   inputSubjects,
		outputSubj: outputSubject,
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

	if p.running {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "JSONGenericProcessor", "Start", "check running state")
	}

	if p.natsClient == nil {
		return errors.WrapFatal(errors.ErrMissingConfig, "JSONGenericProcessor", "Start", "NATS client required")
	}

	// Subscribe to input subjects
	for _, subject := range p.subjects {
		p.logger.Debug("Subscribing to NATS subject",
			"component", p.name,
			"subject", subject)

		if err := p.natsClient.Subscribe(ctx, subject, p.handleMessage); err != nil {
			p.logger.Error("Failed to subscribe to NATS subject",
				"component", p.name,
				"subject", subject,
				"error", err)
			return errors.WrapTransient(err, "JSONGenericProcessor", "Start", fmt.Sprintf("subscribe to %s", subject))
		}

		p.logger.Debug("Subscribed to NATS subject successfully",
			"component", p.name,
			"subject", subject,
			"output_subject", p.outputSubj)
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

// Stop gracefully stops the processor
func (p *Processor) Stop(timeout time.Duration) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if !p.running {
		return nil
	}

	// Signal shutdown
	close(p.shutdown)

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
		return errors.WrapTransient(
			fmt.Errorf("shutdown timeout after %v", timeout),
			"JSONGenericProcessor", "Stop", "graceful shutdown")
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
		if err := p.natsClient.Publish(ctx, p.outputSubj, wrappedData); err != nil {
			atomic.AddInt64(&p.errors, 1)
			p.logger.Error("Failed to publish wrapped message",
				"component", p.name,
				"output_subject", p.outputSubj,
				"error", err)
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
