// Package weatherstation provides an example weather station processor
// demonstrating how to build a domain processor following the tutorial.
package weatherstation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

// ComponentConfig holds configuration for the component.
type ComponentConfig struct {
	Ports    *component.PortConfig `json:"ports"`
	OrgID    string                `json:"org_id"`
	Platform string                `json:"platform"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() ComponentConfig {
	return ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "nats_input", Config: component.NATSPort{Subject: "raw.weather.>"}, Required: true,
					Description: "NATS subjects with weather JSON data",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "nats_output", Config: component.NATSPort{Subject: "events.graph.entity.weather"}, Required: true,
					Description: "NATS subject for Graphable weather readings",
				},
			},
		},
		OrgID:    "default-org",
		Platform: "default-platform",
	}
}

var weatherStationSchema = component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

// Component wraps the domain processor with component lifecycle.
type Component struct {
	name       string
	subjects   []string
	outputSubj string
	inputs     []component.Port
	outputs    []component.Port
	config     ComponentConfig
	natsClient *natsclient.Client
	logger     *slog.Logger
	processor  *Processor

	running       bool
	startTime     time.Time
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	generation    *lifecyclejoin.Generation
	subscriptions []*natsclient.Subscription

	messagesProcessed int64
	errors            int64
	lastActivity      time.Time
}

// NewComponent creates a new component from configuration.
func NewComponent(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config ComponentConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "WeatherStationComponent", "NewComponent", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	if config.OrgID == "" {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "OrgID is required")
	}

	if config.Platform == "" {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "Platform is required")
	}
	if len(config.Ports.Outputs) != 1 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "exactly one output port is required")
	}

	var inputSubjects []string
	var outputSubject string
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))

	for _, definition := range config.Ports.Inputs {
		input, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "WeatherStationComponent", "NewComponent", "resolve input port")
		}
		facts, err := input.Facts()
		if err != nil || facts.Kind() != component.PortKindNATS || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "input ports must each declare one NATS subject")
		}
		inputs = append(inputs, input)
		inputSubjects = append(inputSubjects, facts.NATSSubjects()[0])
	}

	outputs := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		output, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "WeatherStationComponent", "NewComponent", "resolve output port")
		}
		facts, err := output.Facts()
		if err != nil || facts.Kind() != component.PortKindNATS || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "output ports must each declare one NATS subject")
		}
		outputs = append(outputs, output)
	}
	facts, _ := outputs[0].Facts()
	outputSubject = facts.NATSSubjects()[0]

	processor := NewProcessor(Config{
		OrgID:    config.OrgID,
		Platform: config.Platform,
	})

	return &Component{
		name:       "weather-station-processor",
		subjects:   inputSubjects,
		outputSubj: outputSubject,
		inputs:     inputs,
		outputs:    outputs,
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		processor:  processor,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing messages.
func (c *Component) Start(ctx context.Context) (startErr error) {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "Start", "context cannot be nil")
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	if c.generation != nil {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "WeatherStationComponent", "Start", "already running")
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "WeatherStationComponent", "Start", "NATS client required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	generation := lifecyclejoin.NewGeneration(cancel, nil)
	c.generation = generation
	started := false
	defer func() {
		if !started {
			rollbackErr := lifecyclejoin.RunPartialStartRollback(func(ctx context.Context) error {
				return generation.Stop(ctx, nil, c.stopSubscriptions)
			})
			if rollbackErr == nil && c.generation == generation {
				c.generation = nil
			}
			startErr = errors.Join(startErr, rollbackErr)
		}
	}()

	for _, subject := range c.subjects {
		sub, err := c.natsClient.Subscribe(runCtx, subject, func(ctx context.Context, msg *nats.Msg) {
			c.handleMessage(ctx, msg.Data)
		})
		if err != nil {
			return errs.WrapTransient(err, "WeatherStationComponent", "Start",
				fmt.Sprintf("subscribe to %s", subject))
		}
		c.subscriptions = append(c.subscriptions, sub)
	}

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()
	started = true

	c.logger.Info("Weather station processor started",
		"component", c.name,
		"input_subjects", c.subjects,
		"output_subject", c.outputSubj)

	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	c.lifecycleMu.Lock()
	generation := c.generation
	if generation == nil {
		c.lifecycleMu.Unlock()
		return nil
	}
	c.lifecycleMu.Unlock()

	stopErr := generation.Stop(ctx, nil, c.stopSubscriptions)
	if stopErr == nil {
		c.lifecycleMu.Lock()
		if c.generation == generation {
			c.generation = nil
		}
		c.lifecycleMu.Unlock()
	}
	return stopErr
}

func (c *Component) stopSubscriptions(ctx context.Context) error {
	var drainErr error
	for _, sub := range c.subscriptions {
		drainErr = errors.Join(drainErr, sub.Drain(ctx))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return errors.Join(drainErr, ctxErr)
	}
	c.subscriptions = nil
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	return drainErr
}

// handleMessage processes incoming weather JSON messages.
func (c *Component) handleMessage(ctx context.Context, msgData []byte) {
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	var data map[string]any
	if err := json.Unmarshal(msgData, &data); err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Debug("Failed to parse JSON", "error", err)
		return
	}

	reading, err := c.processor.Process(data)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Failed to process weather data", "error", err)
		return
	}

	// Emit WeatherReading entity
	c.emitEntity(ctx, reading, reading.Schema())
}

// emitEntity wraps a payload in BaseMessage and publishes.
func (c *Component) emitEntity(ctx context.Context, payload message.Payload, msgType message.Type) {
	baseMsg := message.NewBaseMessage(msgType, payload, c.name)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		c.logger.Error("Failed to marshal BaseMessage", "error", err)
		return
	}

	if c.outputSubj != "" {
		if err := c.natsClient.Publish(ctx, c.outputSubj, data); err != nil {
			atomic.AddInt64(&c.errors, 1)
			c.logger.Error("Failed to publish entity", "error", err)
		}
	}
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        c.name,
		Type:        "processor",
		Description: "Transforms weather JSON into Graphable payloads",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports this processor subscribes to.
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts returns the NATS output ports for weather readings.
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputs...)
}

// ConfigSchema returns the configuration schema for this processor.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return weatherStationSchema
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
	return component.FlowMetrics{
		LastActivity: c.lastActivity,
	}
}
