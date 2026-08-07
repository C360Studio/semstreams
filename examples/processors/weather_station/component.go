// Package weatherstation provides an example weather station processor
// demonstrating how to build a domain processor following the tutorial.
package weatherstation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
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
	config     ComponentConfig
	natsClient *natsclient.Client
	logger     *slog.Logger
	processor  *Processor

	shutdown      chan struct{}
	done          chan struct{}
	running       bool
	startTime     time.Time
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	wg            *sync.WaitGroup
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

	processor := NewProcessor(Config{
		OrgID:    config.OrgID,
		Platform: config.Platform,
	})

	return &Component{
		name:       "weather-station-processor",
		subjects:   inputSubjects,
		outputSubj: outputSubject,
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		processor:  processor,
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		wg:         &sync.WaitGroup{},
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing messages.
func (c *Component) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	if c.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "WeatherStationComponent", "Start", "already running")
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "WeatherStationComponent", "Start", "NATS client required")
	}

	for _, subject := range c.subjects {
		sub, err := c.natsClient.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
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

	c.logger.Info("Weather station processor started",
		"component", c.name,
		"input_subjects", c.subjects,
		"output_subject", c.outputSubj)

	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(timeout time.Duration) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	if !c.running {
		return nil
	}

	close(c.shutdown)

	for _, sub := range c.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			c.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	c.subscriptions = nil

	waitCh := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
	case <-time.After(timeout):
		return fmt.Errorf("shutdown timeout after %v", timeout)
	}

	c.mu.Lock()
	c.running = false
	close(c.done)
	c.mu.Unlock()

	return nil
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
	ports := make([]component.Port, len(c.subjects))
	for i, subj := range c.subjects {
		ports[i] = component.Port{
			Name:      fmt.Sprintf("input_%d", i),
			Direction: component.DirectionInput,
			Required:  true,
			Config:    component.NATSPort{Subject: subj},
		}
	}
	return ports
}

// OutputPorts returns the NATS output ports for weather readings.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{
		{
			Name:      "output",
			Direction: component.DirectionOutput,
			Required:  true,
			Config:    component.NATSPort{Subject: c.outputSubj},
		},
	}
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
