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
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

// ComponentConfig holds configuration for the component.
type ComponentConfig struct {
	Ports *component.PortConfig `json:"ports"`
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
	}
}

// removedConfigFields maps every field withdrawn from this component's
// operator surface to the guidance that replaces it. encoding/json silently
// DROPS a key with no matching struct field, so without this probe an operator
// upgrading past ADR-102 would keep org_id/platform in their config, see no
// error, and watch every entity this processor mints move to a different
// authority. A removed knob must fail at load.
//
// The probe is targeted rather than a blanket DisallowUnknownFields: it names
// the replacement, and it cannot reject unrelated keys other tooling may
// legitimately carry in the block. Same shape as graph-clustering's
// rejectRemovedConfigKeys and config.rejectRemovedPlatformFields.
var removedConfigFields = map[string]string{
	"org_id":   `removed (ADR-102 d2, BREAKING): positions 1-2 of every minted entity ID are the composition root's platform.org / platform.id and nothing else — never an operator knob on a component. Delete the field; set platform.org at the top level of the config`,
	"platform": `removed (ADR-102 d2, BREAKING): positions 1-2 of every minted entity ID are the composition root's platform.org / platform.id and nothing else — never a product name and never an operator knob on a component. Delete the field; set platform.id at the top level of the config`,
}

// rejectRemovedConfigKeys fails the load when a withdrawn field is present,
// naming its replacement. Called from resolveConfig, the one derivation both
// entry paths share (DeclarePorts for offline composition validation,
// NewComponent at boot), so neither can accept what the other refuses.
func rejectRemovedConfigKeys(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		// Not an object, or malformed — the caller's own decode reports that.
		return nil
	}
	for field, guidance := range removedConfigFields {
		if _, found := present[field]; found {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "rejectRemovedConfigKeys",
				fmt.Sprintf("config field %q was %s", field, guidance))
		}
	}
	return nil
}

var weatherStationSchema = component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

// Component wraps the domain processor with component lifecycle.
type Component struct {
	name           string
	subjects       []string
	outputSubj     string
	inputs         []component.Port
	outputs        []component.Port
	config         ComponentConfig
	natsClient     *natsclient.Client
	logger         *slog.Logger
	processor      *Processor
	subscribeInput func(
		context.Context,
		string,
		func(context.Context, *nats.Msg),
	) (*natsclient.Subscription, error)

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

	messagesProcessed int64
	errors            int64
	lastActivity      time.Time
}

// DeclarePorts is the component.PortDeclarer for weather_station: the ports
// NewComponent will report for rawConfig, computed without dependencies.
func DeclarePorts(rawConfig json.RawMessage, _ string) (component.PortConfig, error) {
	_, inputs, outputs, _, err := resolveConfig(rawConfig)
	if err != nil {
		return component.PortConfig{}, err
	}
	return component.PortConfigFrom(inputs, outputs), nil
}

// resolveConfig parses rawConfig (defaults when no ports are configured),
// validates, and resolves the ports with their one-NATS-subject rule. It is
// the one derivation DeclarePorts and NewComponent share.
func resolveConfig(rawConfig json.RawMessage) (ComponentConfig, []component.Port, []component.Port, []string, error) {
	if err := rejectRemovedConfigKeys(rawConfig); err != nil {
		return ComponentConfig{}, nil, nil, nil, err
	}
	var config ComponentConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return ComponentConfig{}, nil, nil, nil, errs.WrapInvalid(err, "WeatherStationComponent", "NewComponent", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	if len(config.Ports.Outputs) != 1 {
		return ComponentConfig{}, nil, nil, nil, errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "exactly one output port is required")
	}

	var inputSubjects []string
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		input, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return ComponentConfig{}, nil, nil, nil, errs.WrapInvalid(err, "WeatherStationComponent", "NewComponent", "resolve input port")
		}
		facts, err := input.Facts()
		if err != nil || facts.Kind() != component.PortKindNATS || len(facts.NATSSubjects()) != 1 {
			return ComponentConfig{}, nil, nil, nil, errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "input ports must each declare one NATS subject")
		}
		inputs = append(inputs, input)
		inputSubjects = append(inputSubjects, facts.NATSSubjects()[0])
	}

	outputs := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		output, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return ComponentConfig{}, nil, nil, nil, errs.WrapInvalid(err, "WeatherStationComponent", "NewComponent", "resolve output port")
		}
		facts, err := output.Facts()
		if err != nil || facts.Kind() != component.PortKindNATS || len(facts.NATSSubjects()) != 1 {
			return ComponentConfig{}, nil, nil, nil, errs.WrapInvalid(errs.ErrInvalidConfig, "WeatherStationComponent", "NewComponent", "output ports must each declare one NATS subject")
		}
		outputs = append(outputs, output)
	}
	return config, inputs, outputs, inputSubjects, nil
}

// NewComponent creates a new component from configuration.
func NewComponent(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	config, inputs, outputs, inputSubjects, err := resolveConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	facts, _ := outputs[0].Facts()
	outputSubject := facts.NATSSubjects()[0]

	// The deployment authority comes from the composition root and nowhere
	// else — the component never reads it from its own config (ADR-102 d2).
	processor := NewProcessor(deps.Platform)

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
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "WeatherStationComponent", "Start", "context already cancelled")
	}
	c.lifecycleMu.Lock()
	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "WeatherStationComponent", "Start", "already running")
	}
	if c.natsClient == nil {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrMissingConfig, "WeatherStationComponent", "Start", "NATS client required")
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

	for _, subject := range c.subjects {
		subscribeInput := c.natsClient.Subscribe
		if c.subscribeInput != nil {
			subscribeInput = c.subscribeInput
		}
		sub, err := subscribeInput(runCtx, subject, func(ctx context.Context, msg *nats.Msg) {
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
	committed = true

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
			return errs.WrapTransient(errors.New("stop already in progress"), "WeatherStationComponent", "Stop", "concurrent Stop is unsupported")
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
	var drainErr error
	for _, sub := range c.subscriptions {
		drainErr = errors.Join(drainErr, sub.Drain(ctx))
	}
	if c.cancel != nil {
		c.cancel()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		drainErr = errors.Join(drainErr, ctxErr)
	}
	return drainErr
}

func (c *Component) clearLifecycleHandles() {
	c.subscriptions = nil
	c.cancel = nil
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
