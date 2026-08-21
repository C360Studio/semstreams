package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// componentSchema defines the configuration schema.
var componentSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Ensure Component implements required interfaces.
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Component implements the OTEL exporter component.
// It collects spans and metrics from agent events and exports them to OTEL collectors.
type Component struct {
	name       string
	config     Config
	inputs     []component.Port
	natsClient *natsclient.Client
	logger     *slog.Logger
	decoder    *message.Decoder

	// Span collection
	spanCollector *SpanCollector

	// Metric mapping
	metricMapper *MetricMapper

	// JetStream consumer
	consumer       jetstream.Consumer
	policyCleanups []func()
	observePolicy  func(context.Context, natsclient.PortConsumerContext, jetstream.ConsumerConfig, jetstream.Consumer) (func(), error)
	consumeFrom    func(context.Context, jetstream.Consumer)

	// Export client. Configuration validation guarantees a supported exporter.
	exporter Exporter

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// Private one-shot lifecycle authority. Stop waits for startDone before
	// reading the exact cancel/join records published by Start.
	lifecycleMu   sync.Mutex
	lifecycleUsed bool
	lifecycleDone bool
	stopping      bool
	startDone     chan struct{}
	cancel        context.CancelFunc
	runtimeDone   chan struct{}
	claims        []otelConsumerClaim
	wg            sync.WaitGroup

	// Metrics tracking
	eventsProcessed int64
	spansExported   int64
	metricsExported int64
	errors          int64
	lastActivity    time.Time
}

type observedSubscription struct {
	consumer jetstream.Consumer
	cleanup  func()
	claim    otelConsumerClaim
}

type otelConsumerIdentity struct {
	stream  string
	durable string
}

type otelConsumerClaim struct {
	identity otelConsumerIdentity
	token    *struct{}
}

var localOTELConsumerClaims = struct {
	sync.Mutex
	active map[otelConsumerIdentity]*struct{}
}{active: make(map[otelConsumerIdentity]*struct{})}

// Exporter defines the interface for OTEL export operations.
type Exporter interface {
	// ExportSpans exports spans to the OTEL collector.
	ExportSpans(ctx context.Context, spans []*SpanData) error

	// ExportMetrics exports metrics to the OTEL collector.
	ExportMetrics(ctx context.Context, metrics []*MetricData) error

	// Shutdown gracefully shuts down the exporter.
	Shutdown(ctx context.Context) error
}

// NewComponent creates a new OTEL exporter component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := decodeConfig(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := decodeConfig(rawConfig, &config); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}
	if len(config.Ports.Inputs) == 0 {
		config.Ports.Inputs = []component.PortDefinition{{
			Name: "agent_events", Config: component.JetStreamPort{Subjects: []string{"agent.>"}, StreamName: "AGENT"},
		}}
	}
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "resolve input port")
		}
		facts, err := port.Facts()
		if err != nil || facts.Kind() != component.PortKindJetStream {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "NewComponent", "input ports must be JetStream ports")
		}
		inputs = append(inputs, port)
	}

	return &Component{
		name:       "otel-exporter",
		config:     config,
		inputs:     inputs,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		decoder:    message.NewDecoder(deps.PayloadRegistry),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	// Create span collector
	c.spanCollector = newSpanCollector(
		c.config.ServiceName,
		c.config.ServiceVersion,
		c.config.SamplingRate,
		c.decoder,
	)

	// Create metric mapper
	c.metricMapper = NewMetricMapper(
		c.config.ServiceName,
		c.config.ServiceVersion,
	)

	c.exporter = NewOTLPExporter(
		c.config.Endpoint,
		c.config.Headers,
		c.logger,
	)

	return nil
}

func decodeConfig(raw json.RawMessage, target *Config) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// Start begins processing agent events and exporting OTEL data.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}
	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "check NATS client")
	}
	c.lifecycleMu.Lock()
	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "component instance already used")
	}
	c.lifecycleUsed = true
	startDone := make(chan struct{})
	c.startDone = startDone
	c.lifecycleMu.Unlock()

	finishStart := func(terminal bool) {
		c.lifecycleMu.Lock()
		c.lifecycleDone = terminal
		close(startDone)
		c.startDone = nil
		c.lifecycleMu.Unlock()
	}

	// Derive background work from Start and retain only private cancellation.
	runtimeCtx, cancel := context.WithCancel(ctx)

	// Subscribe to agent events
	if err := c.subscribeToEvents(runtimeCtx); err != nil {
		cancel()
		finishStart(true)
		return errs.Wrap(err, "Component", "Start", "subscribe to events")
	}

	// Start export loop
	c.wg.Add(1)
	go c.exportLoop(runtimeCtx)
	runtimeDone := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(runtimeDone)
	}()
	c.mu.Lock()
	c.lifecycleMu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.cancel = cancel
	c.runtimeDone = runtimeDone
	close(startDone)
	c.startDone = nil
	c.lifecycleMu.Unlock()
	c.mu.Unlock()

	c.logger.Info("OTEL exporter started",
		slog.String("endpoint", c.config.Endpoint),
		slog.String("protocol", c.config.Protocol),
		slog.Bool("export_traces", c.config.ExportTraces),
		slog.Bool("export_metrics", c.config.ExportMetrics))

	return nil
}

// subscribeToEvents sets up a JetStream consumer for each configured input port.
// Each port gets its own durable consumer so subjects are independently tracked.
func (c *Component) subscribeToEvents(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return err
	}

	baseConsumerName := "otel-exporter"
	if c.config.ConsumerNameSuffix != "" {
		baseConsumerName += "-" + c.config.ConsumerNameSuffix
	}

	subscriptions := make([]observedSubscription, 0, len(c.inputs))
	rollback := func() {
		for i := len(subscriptions) - 1; i >= 0; i-- {
			created := subscriptions[i]
			created.cleanup()
			releaseOTELConsumerClaim(created.claim)
		}
	}

	for _, port := range c.inputs {
		facts, err := port.Facts()
		if err != nil {
			rollback()
			return err
		}
		stream, ok := facts.Stream()
		if !ok || len(stream.Subjects()) != 1 {
			rollback()
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "subscribeToEvents", "input must declare one JetStream subject")
		}
		streamName := stream.Name()
		subject := stream.Subjects()[0]

		// Verify the stream exists before creating a consumer.
		if _, err := js.Stream(ctx, streamName); err != nil {
			c.logger.Debug("Stream not found, skipping port subscription",
				slog.String("stream", streamName),
				slog.String("port", port.Name))
			continue
		}

		// Each port gets a unique consumer name derived from the base name and port name.
		consumerName := baseConsumerName + "-" + port.Name
		consumerConfig, err := component.GetConsumerConfig(port)
		if err != nil {
			rollback()
			return err
		}
		finalConfig := jetstream.ConsumerConfig{
			Name:          consumerName,
			Durable:       consumerName,
			FilterSubject: subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverNewPolicy,
			MaxAckPending: consumerConfig.MaxAckPending,
		}
		claim, err := reserveOTELConsumerClaim(streamName, consumerName)
		if err != nil {
			rollback()
			return err
		}
		consumer, err := js.CreateOrUpdateConsumer(ctx, streamName, finalConfig)
		if err != nil {
			releaseOTELConsumerClaim(claim)
			rollback()
			return natsclient.ClassifyConsumerPolicyError(err, "otel.CreateOrUpdateConsumer")
		}
		observed, err := c.prepareObservedSubscription(ctx,
			natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name}, finalConfig, consumer)
		if err != nil {
			releaseOTELConsumerClaim(claim)
			rollback()
			return err
		}
		observed.claim = claim
		subscriptions = append(subscriptions, observed)
	}
	if err := ctx.Err(); err != nil {
		rollback()
		return err
	}

	for _, created := range subscriptions {
		c.startObservedSubscription(ctx, created)
	}

	return nil
}

func (c *Component) prepareObservedSubscription(
	ctx context.Context,
	owner natsclient.PortConsumerContext,
	finalConfig jetstream.ConsumerConfig,
	consumer jetstream.Consumer,
) (observedSubscription, error) {
	observer := c.observePolicy
	if observer == nil {
		observer = c.natsClient.ObserveDirectPortConsumerPolicy
	}
	cleanup, err := observer(ctx, owner, finalConfig, consumer)
	if err != nil {
		return observedSubscription{}, err
	}
	return observedSubscription{consumer: consumer, cleanup: cleanup}, nil
}

func (c *Component) startObservedSubscription(ctx context.Context, subscription observedSubscription) {
	c.policyCleanups = append(c.policyCleanups, subscription.cleanup)
	c.claims = append(c.claims, subscription.claim)
	if c.consumer == nil {
		c.consumer = subscription.consumer
	}
	runner := c.consumeFrom
	if runner == nil {
		runner = c.consumeEventsFromConsumer
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		runner(ctx, subscription.consumer)
	}()
}

func reserveOTELConsumerClaim(stream, durable string) (otelConsumerClaim, error) {
	identity := otelConsumerIdentity{stream: stream, durable: durable}
	localOTELConsumerClaims.Lock()
	defer localOTELConsumerClaims.Unlock()
	if _, exists := localOTELConsumerClaims.active[identity]; exists {
		return otelConsumerClaim{}, errs.WrapInvalid(
			fmt.Errorf("OTEL consumer %s:%s already has a local owner", stream, durable),
			"Component", "Start", "duplicate local durable identity")
	}
	token := &struct{}{}
	localOTELConsumerClaims.active[identity] = token
	return otelConsumerClaim{identity: identity, token: token}, nil
}

func releaseOTELConsumerClaim(claim otelConsumerClaim) {
	if claim.token == nil {
		return
	}
	localOTELConsumerClaims.Lock()
	if localOTELConsumerClaims.active[claim.identity] == claim.token {
		delete(localOTELConsumerClaims.active, claim.identity)
	}
	localOTELConsumerClaims.Unlock()
}

// consumeEventsFromConsumer processes incoming agent events from a specific consumer.
func (c *Component) consumeEventsFromConsumer(ctx context.Context, consumer jetstream.Consumer) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch messages with timeout
		msgs, err := consumer.Fetch(10, jetstream.FetchMaxWait(time.Second))
		if err != nil {
			// Timeout is expected, continue
			continue
		}

		for msg := range msgs.Messages() {
			// Check context during message iteration to avoid goroutine leak
			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := c.processEvent(ctx, msg); err != nil {
				c.logger.Warn("Failed to process event",
					slog.Any("error", err))
				c.incrementErrors()
				if termErr := msg.Term(); termErr != nil {
					c.logger.Warn("Failed to terminate invalid event", slog.Any("error", termErr))
				}
				continue
			}
			if err := msg.Ack(); err != nil {
				c.logger.Warn("Failed to ack message", slog.Any("error", err))
			}
		}
	}
}

// processEvent processes a single agent event.
func (c *Component) processEvent(ctx context.Context, msg jetstream.Msg) error {
	// Process event through span collector using the typed BaseMessage format.
	if c.config.ExportTraces {
		if err := c.spanCollector.ProcessMessage(ctx, msg.Subject(), msg.Data()); err != nil {
			return err
		}
	}

	c.mu.Lock()
	c.eventsProcessed++
	c.lastActivity = time.Now()
	c.mu.Unlock()

	return nil
}

// exportLoop periodically exports collected data.
func (c *Component) exportLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.GetBatchTimeout())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.exportData(ctx)
		}
	}
}

// exportData exports collected spans and metrics.
func (c *Component) exportData(ctx context.Context) {
	exportCtx, cancel := context.WithTimeout(ctx, c.config.GetExportTimeout())
	defer cancel()

	exporter := c.getExporter()

	// Export spans
	if c.config.ExportTraces {
		spans := c.spanCollector.FlushCompleted()
		if len(spans) > 0 {
			if exporter != nil {
				if err := exporter.ExportSpans(exportCtx, spans); err != nil {
					c.logger.Warn("Failed to export spans",
						slog.Int("count", len(spans)),
						slog.Any("error", err))
					c.incrementErrors()
				} else {
					c.mu.Lock()
					c.spansExported += int64(len(spans))
					c.mu.Unlock()

					c.logger.Debug("Exported spans",
						slog.Int("count", len(spans)))
				}
			} else {
				c.logger.Error("OTEL exporter unavailable; spans were not exported",
					slog.Int("count", len(spans)))
				c.incrementErrors()
			}
		}
	}

	// Export metrics
	if c.config.ExportMetrics {
		metrics := c.metricMapper.FlushMetrics()
		if len(metrics) > 0 {
			if exporter != nil {
				if err := exporter.ExportMetrics(exportCtx, metrics); err != nil {
					c.logger.Warn("Failed to export metrics",
						slog.Int("count", len(metrics)),
						slog.Any("error", err))
					c.incrementErrors()
				} else {
					c.mu.Lock()
					c.metricsExported += int64(len(metrics))
					c.mu.Unlock()

					c.logger.Debug("Exported metrics",
						slog.Int("count", len(metrics)))
				}
			} else {
				c.logger.Error("OTEL exporter unavailable; metrics were not exported",
					slog.Int("count", len(metrics)))
				c.incrementErrors()
			}
		}
	}
}

// incrementErrors safely increments the error counter.
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// SetExporter sets the OTEL exporter (for testing).
func (c *Component) SetExporter(exp Exporter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exporter = exp
}

// getExporter safely retrieves the exporter.
func (c *Component) getExporter() Exporter {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.exporter
}

// GetSpanCollector returns the span collector (for testing).
func (c *Component) GetSpanCollector() *SpanCollector {
	return c.spanCollector
}

// GetMetricMapper returns the metric mapper (for testing).
func (c *Component) GetMetricMapper() *MetricMapper {
	return c.metricMapper
}

// Stop gracefully stops the component.
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "LifecycleComponent", "Stop", "context already cancelled")
	}
	for {
		c.lifecycleMu.Lock()
		if !c.lifecycleUsed {
			c.lifecycleUsed = true
			c.lifecycleDone = true
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.lifecycleDone {
			c.lifecycleMu.Unlock()
			return nil
		}
		startDone := c.startDone
		if startDone != nil {
			c.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return fmt.Errorf("wait for OTEL Start completion: %w", ctx.Err())
			}
		}
		if c.stopping {
			c.lifecycleMu.Unlock()
			return errs.WrapTransient(fmt.Errorf("OTEL Stop already in progress"),
				"Component", "Stop", "concurrent Stop is unsupported")
		}
		c.stopping = true
		cancel := c.cancel
		runtimeDone := c.runtimeDone
		policyCleanups := append([]func(){}, c.policyCleanups...)
		claims := append([]otelConsumerClaim(nil), c.claims...)
		c.lifecycleMu.Unlock()

		cancel()
		select {
		case <-runtimeDone:
		case <-ctx.Done():
			c.finishTerminalStop(false)
			return fmt.Errorf("join OTEL runtime: %w", ctx.Err())
		}

		// Flush only after the pull loop and exporter loop have joined. Policy
		// observation then retires before exporter shutdown.
		c.exportData(ctx)
		for _, cleanup := range policyCleanups {
			cleanup()
		}
		exporter := c.getExporter()
		var shutdownErr error
		if exporter != nil {
			if err := exporter.Shutdown(ctx); err != nil {
				shutdownErr = fmt.Errorf("shutdown OTEL exporter: %w", err)
			}
		}
		for _, claim := range claims {
			releaseOTELConsumerClaim(claim)
		}
		c.finishTerminalStop(true)
		return shutdownErr
	}
}

func (c *Component) finishTerminalStop(clearResources bool) {
	c.lifecycleMu.Lock()
	c.stopping = false
	c.lifecycleDone = true
	c.cancel = nil
	c.runtimeDone = nil
	if clearResources {
		c.policyCleanups = nil
		c.claims = nil
		c.consumer = nil
	}
	c.lifecycleMu.Unlock()

	c.mu.Lock()
	c.running = false
	eventsProcessed := c.eventsProcessed
	spansExported := c.spansExported
	metricsExported := c.metricsExported
	c.mu.Unlock()
	c.logger.Info("OTEL exporter stopped",
		slog.Int64("events_processed", eventsProcessed),
		slog.Int64("spans_exported", spansExported),
		slog.Int64("metrics_exported", metricsExported))
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "otel-exporter",
		Type:        "output",
		Description: "Exports agent telemetry to OpenTelemetry collectors",
		Version:     "1.0.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	// OTEL exporter has no NATS output ports (exports to external collector)
	return nil
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return componentSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := "stopped"
	if c.running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors),
		Uptime:     time.Since(c.startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var errorRate float64
	total := c.eventsProcessed + c.errors
	if total > 0 {
		errorRate = float64(c.errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}
