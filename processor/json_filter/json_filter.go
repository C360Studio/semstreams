// Package jsonfilter provides a core processor for filtering GenericJSON messages
package jsonfilter

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
	"github.com/nats-io/nats.go/jetstream"
)

// Config holds configuration for JSON filter processor
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	Rules []FilterRule          `json:"rules" schema:"type:array,description:Filter rules,category:basic"`
}

// FilterRule defines a single filter condition
type FilterRule struct {
	Field    string `json:"field"    schema:"type:string,description:Field path to check,required:true"`
	Operator string `json:"operator" schema:"type:enum,enum:eq|ne|gt|gte|lt|lte|contains,required:true"`
	Value    any    `json:"value"    schema:"type:string,description:Comparison value,required:true"`
}

// DefaultConfig returns the default configuration for JSON filter processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "raw.>", Interface: &component.InterfaceContract{
				// Require GenericJSON
				Type: "core .json.v1"}}, Required: true,
			Description: "NATS subjects to filter (must be GenericJSON payloads)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "nats_output", Config: component.NATSPort{Subject: "filtered.messages", Interface: &component.InterfaceContract{
				// Output GenericJSON
				Type: "core .json.v1"}}, Required: true,
			Description: "NATS subject for matched messages",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		Rules: []FilterRule{},
	}
}

// jsonFilterSchema defines the configuration schema for JSON filter processor
var jsonFilterSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Processor implements a GenericJSON message filter
type Processor struct {
	name               string
	subjects           []string
	outputSubjs        []string // Support multiple output subjects
	rules              []FilterRule
	config             Config // Store full config for port type checking
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
	messagesProcessed int64
	messagesFiltered  int64
	messagesPassed    int64
	errors            int64
	lastActivity      time.Time

	// Prometheus metrics
	metrics *filterMetrics
}

type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

// DeclarePorts is the component.PortDeclarer for json_filter: the ports
// NewProcessor will report for rawConfig, computed without dependencies.
func DeclarePorts(rawConfig json.RawMessage, _ string) (component.PortConfig, error) {
	resolved, err := resolveConfig(rawConfig)
	if err != nil {
		return component.PortConfig{}, err
	}
	return component.PortConfigFrom(resolved.inputPorts, resolved.outputPorts), nil
}

type resolvedConfig struct {
	config         Config
	inputPorts     []component.Port
	outputPorts    []component.Port
	inputSubjects  []string
	outputSubjects []string
}

// resolveConfig parses rawConfig (defaults when no ports are configured) and
// resolves the message ports. It is the one derivation DeclarePorts and
// NewProcessor share.
func resolveConfig(rawConfig json.RawMessage) (resolvedConfig, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return resolvedConfig{}, errs.WrapInvalid(err, "JSONFilterProcessor", "NewProcessor", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	inputPorts, inputSubjects, err := resolveMessagePorts(config.Ports.Inputs, component.DirectionInput)
	if err != nil {
		return resolvedConfig{}, errs.WrapInvalid(err, "JSONFilterProcessor", "NewProcessor", "resolve input ports")
	}
	outputPorts, outputSubjects, err := resolveMessagePorts(config.Ports.Outputs, component.DirectionOutput)
	if err != nil {
		return resolvedConfig{}, errs.WrapInvalid(err, "JSONFilterProcessor", "NewProcessor", "resolve output ports")
	}

	if len(inputSubjects) == 0 {
		return resolvedConfig{}, errs.WrapInvalid(
			errs.ErrInvalidConfig, "JSONFilterProcessor", "NewProcessor",
			"no input subjects configured")
	}
	return resolvedConfig{
		config: config, inputPorts: inputPorts, outputPorts: outputPorts,
		inputSubjects: inputSubjects, outputSubjects: outputSubjects,
	}, nil
}

// NewProcessor creates a new JSON filter processor from configuration
func NewProcessor(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	resolved, err := resolveConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	config, inputPorts, outputPorts := resolved.config, resolved.inputPorts, resolved.outputPorts
	inputSubjects, outputSubjects := resolved.inputSubjects, resolved.outputSubjects
	jetStreamOutputs := make(map[string]bool, len(outputPorts))
	for _, port := range outputPorts {
		facts, factsErr := port.Facts()
		if factsErr != nil {
			return nil, errs.WrapInvalid(factsErr, "JSONFilterProcessor", "NewProcessor", "project output port facts")
		}
		jetStreamOutputs[facts.NATSSubjects()[0]] = facts.Kind() == component.PortKindJetStream
	}

	// Initialize metrics if registry provided
	metrics, err := newFilterMetrics(deps.MetricsRegistry, "json-filter-processor")
	if err != nil {
		deps.GetLogger().Error("Failed to initialize JSON filter metrics", "error", err)
		metrics = nil // Continue without metrics
	}

	return &Processor{
		name:             "json-filter-processor",
		subjects:         inputSubjects,
		outputSubjs:      outputSubjects,
		rules:            config.Rules,
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

// Initialize prepares the processor (no-op for JSON filter)
func (f *Processor) Initialize() error {
	return nil
}

// Start begins filtering messages
func (f *Processor) Start(ctx context.Context) (startErr error) {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "JSONFilterProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "JSONFilterProcessor", "Start", "context already cancelled")
	}

	f.lifecycleMu.Lock()
	if f.lifecycleUsed {
		f.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, "JSONFilterProcessor", "Start", "check running state")
	}
	if f.natsClient == nil {
		f.lifecycleMu.Unlock()
		return errs.WrapFatal(errs.ErrMissingConfig, "JSONFilterProcessor", "Start", "NATS client required")
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	f.lifecycleUsed = true
	f.cleanupPending = true
	f.cancel = cancel
	f.startDone = startDone
	f.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, f.cleanupFailedStart)
			startErr = errors.Join(startErr, rollbackErr)
			f.lifecycleMu.Lock()
			if rollbackErr == nil {
				f.cleanupPending = false
				f.terminal = true
				f.clearLifecycleHandles()
			}
			close(startDone)
			f.startDone = nil
			f.lifecycleMu.Unlock()
			return
		}
		f.lifecycleMu.Lock()
		f.cleanupPending = false
		close(startDone)
		f.startDone = nil
		f.lifecycleMu.Unlock()
	}()

	// Subscribe to input ports based on port type
	if err := f.setupSubscriptions(runCtx); err != nil {
		return err
	}

	f.mu.Lock()
	f.running = true
	f.startTime = time.Now()
	f.mu.Unlock()
	committed = true

	f.logger.Info("JSON filter processor started",
		"component", f.name,
		"input_subjects", f.subjects,
		"output_subjects", f.outputSubjs,
		"rules", len(f.rules))

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (f *Processor) setupSubscriptions(ctx context.Context) error {
	for _, port := range f.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "JSONFilterProcessor", "Start", "project input port facts")
		}
		subject := facts.NATSSubjects()[0]

		switch facts.Kind() {
		case component.PortKindJetStream:
			if err := f.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "JSONFilterProcessor", "Start",
					fmt.Sprintf("JetStream consumer for %s", subject))
			}

		case component.PortKindNATS:
			sub, err := f.natsClient.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
				f.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				f.logger.Error("Failed to subscribe to NATS subject",
					"component", f.name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "JSONFilterProcessor", "Start",
					fmt.Sprintf("subscribe to %s", subject))
			}
			f.subscriptions = append(f.subscriptions, sub)
			f.logger.Debug("Subscribed to NATS subject successfully",
				"component", f.name,
				"subject", subject,
				"output_subjects", f.outputSubjs,
				"rules_count", len(f.rules))

		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (f *Processor) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
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

	waitForStream := f.waitForStream
	if f.waitForStreamInput != nil {
		waitForStream = f.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "JSONFilterProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("wait for stream %s", streamName))
	}

	sanitizedSubject := sanitizeSubject(subject)
	consumerName := fmt.Sprintf("json-filter-%s", sanitizedSubject)

	f.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "JSONFilterProcessor", "setupJetStreamConsumer", "resolve consumer config")
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

	consumeStream := f.natsClient.ConsumeStreamWithConfig
	if f.consumeStream != nil {
		consumeStream = f.consumeStream
	}
	handle, err := consumeStream(ctx, natsclient.PortConsumerContext{Component: f.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		f.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			f.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "JSONFilterProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("setup consumer for stream %s", streamName))
	}
	f.lifecycleMu.Lock()
	f.consumers = append(f.consumers, streamConsumerBinding{handle: handle})
	f.lifecycleMu.Unlock()

	f.logger.Debug("JSON filter subscribed (JetStream)", "subject", subject, "stream", streamName)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (f *Processor) waitForStream(ctx context.Context, streamName string) error {
	js, err := f.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "JSONFilterProcessor", "waitForStream", "get JetStream context")
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
		errs.ErrStorageUnavailable,
		"JSONFilterProcessor",
		"waitForStream",
		fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries))
}

// sanitizeSubject replaces invalid consumer name characters
func sanitizeSubject(subject string) string {
	result := ""
	for _, c := range subject {
		switch c {
		case '.':
			result += "-"
		case '*':
			result += "all"
		case '>':
			result += "wildcard"
		default:
			result += string(c)
		}
	}
	return result
}

// Stop gracefully stops the processor
func (f *Processor) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		f.lifecycleMu.Lock()
		if !f.lifecycleUsed {
			f.lifecycleUsed = true
			f.terminal = true
			f.lifecycleMu.Unlock()
			return nil
		}
		if f.terminal {
			f.lifecycleMu.Unlock()
			return nil
		}
		if f.startDone != nil {
			startDone := f.startDone
			f.lifecycleMu.Unlock()
			select {
			case <-startDone:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if f.stopping {
			f.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "JSONFilterProcessor", "Stop", "concurrent Stop is unsupported")
		}
		retryable := f.cleanupPending
		f.stopping = true
		f.lifecycleMu.Unlock()

		stopErr := f.cleanup(ctx)
		f.lifecycleMu.Lock()
		f.stopping = false
		if retryable && stopErr != nil {
			f.lifecycleMu.Unlock()
			return stopErr
		}
		f.cleanupPending = false
		f.terminal = true
		f.clearLifecycleHandles()
		f.lifecycleMu.Unlock()
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
		return stopErr
	}
}

func (f *Processor) cleanupFailedStart(ctx context.Context) error { return f.cleanup(ctx) }

func (f *Processor) cleanup(ctx context.Context) error {
	var stopErr error
	for _, sub := range f.subscriptions {
		stopErr = errors.Join(stopErr, sub.Drain(ctx))
	}
	for index := range f.consumers {
		binding := &f.consumers[index]
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
	if f.cancel != nil {
		f.cancel()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		stopErr = errors.Join(stopErr, ctxErr)
	}
	return stopErr
}

func (f *Processor) clearLifecycleHandles() {
	f.subscriptions = nil
	f.consumers = nil
	f.cancel = nil
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (f *Processor) isJetStreamPortBySubject(subject string) bool {
	return f.jetStreamOutputs[subject]
}

// handleMessage processes incoming GenericJSON messages
func (f *Processor) handleMessage(ctx context.Context, msgData []byte) {
	atomic.AddInt64(&f.messagesProcessed, 1)
	f.mu.Lock()
	f.lastActivity = time.Now()
	f.mu.Unlock()

	f.logger.Debug("Received message",
		"component", f.name,
		"size_bytes", len(msgData))

	// Parse as BaseMessage
	baseMsg, err := f.decoder.Decode(msgData)
	if err != nil {
		atomic.AddInt64(&f.errors, 1)
		f.metrics.recordError(f.name, "parse")
		f.logger.Debug("Failed to parse message as BaseMessage",
			"component", f.name,
			"error", err)
		return
	}

	// Extract GenericJSON payload
	payload := baseMsg.Payload()
	genericJSON, ok := payload.(*message.GenericJSONPayload)
	if !ok {
		atomic.AddInt64(&f.errors, 1)
		f.metrics.recordError(f.name, "type")
		f.logger.Debug("Payload is not GenericJSON",
			"component", f.name,
			"actual_type", fmt.Sprintf("%T", payload))
		return
	}

	// Validate the payload
	if err := genericJSON.Validate(); err != nil {
		atomic.AddInt64(&f.errors, 1)
		f.metrics.recordError(f.name, "validation")
		f.logger.Debug("Message validation failed",
			"component", f.name,
			"error", err)
		return
	}

	// Apply filter rules to GenericJSON.Data with timing
	start := time.Now()
	matched := f.matchesRules(genericJSON.Data)
	duration := time.Since(start)

	// Record evaluation metrics
	f.metrics.recordEvaluation(f.name, matched, duration)

	if matched {
		atomic.AddInt64(&f.messagesPassed, 1)

		f.logger.Debug("Message passed filter",
			"component", f.name,
			"output_subjects", f.outputSubjs,
			"evaluation_time_us", duration.Microseconds())

		// Publish to all output subjects
		for _, subject := range f.outputSubjs {
			if subject != "" {
				var publishErr error
				if f.isJetStreamPortBySubject(subject) {
					publishErr = f.natsClient.PublishToStream(ctx, subject, msgData)
				} else {
					publishErr = f.natsClient.Publish(ctx, subject, msgData)
				}
				if publishErr != nil {
					atomic.AddInt64(&f.errors, 1)
					f.metrics.recordError(f.name, "publish")
					f.logger.Error("Failed to publish filtered message",
						"component", f.name,
						"output_subject", subject,
						"error", publishErr)
				} else {
					f.logger.Debug("Published filtered message",
						"component", f.name,
						"output_subject", subject)
				}
			}
		}
	} else {
		atomic.AddInt64(&f.messagesFiltered, 1)
		f.logger.Debug("Message filtered out",
			"component", f.name,
			"rules_count", len(f.rules),
			"evaluation_time_us", duration.Microseconds())
	}

	// Update match rate periodically (every 100 messages)
	if atomic.LoadInt64(&f.messagesProcessed)%100 == 0 {
		f.metrics.updateMatchRate(
			atomic.LoadInt64(&f.messagesPassed),
			atomic.LoadInt64(&f.messagesProcessed),
		)
	}
}

// matchesRules checks if data matches all filter rules
func (f *Processor) matchesRules(data map[string]any) bool {
	// If no rules, pass all messages
	if len(f.rules) == 0 {
		return true
	}

	// All rules must match (AND logic)
	for _, rule := range f.rules {
		if !f.matchesRule(data, rule) {
			return false
		}
	}

	return true
}

// matchesRule checks if data matches a single rule
func (f *Processor) matchesRule(data map[string]any, rule FilterRule) bool {
	// Get field value (supports nested fields with dot notation)
	value := getNestedField(data, rule.Field)
	if value == nil {
		return false
	}

	// Apply operator
	switch rule.Operator {
	case "eq":
		return fmt.Sprint(value) == fmt.Sprint(rule.Value)
	case "ne":
		return fmt.Sprint(value) != fmt.Sprint(rule.Value)
	case "gt":
		return compareNumbers(value, rule.Value) > 0
	case "gte":
		return compareNumbers(value, rule.Value) >= 0
	case "lt":
		return compareNumbers(value, rule.Value) < 0
	case "lte":
		return compareNumbers(value, rule.Value) <= 0
	case "contains":
		valueStr := fmt.Sprint(value)
		ruleStr := fmt.Sprint(rule.Value)
		return contains(valueStr, ruleStr)
	default:
		return false
	}
}

// getNestedField retrieves a nested field value using dot notation
func getNestedField(data map[string]any, field string) any {
	// Simple case: direct field
	if val, ok := data[field]; ok {
		return val
	}

	// TODO: Support dot notation for nested fields (e.g., "position.lat")
	// For now, just return nil if not a direct field
	return nil
}

// compareNumbers compares two numeric values
func compareNumbers(a, b any) int {
	aNum := toFloat64(a)
	bNum := toFloat64(b)

	if aNum < bNum {
		return -1
	} else if aNum > bNum {
		return 1
	}
	return 0
}

// toFloat64 converts any to float64 for comparison
func toFloat64(val any) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return 0
	}
}

// contains checks if s contains substr (case-sensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component.
func (f *Processor) Meta() component.Metadata {
	return component.Metadata{
		Name:        f.name,
		Type:        "processor",
		Description: "GenericJSON (core .json.v1) message filter",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports this processor subscribes to.
func (f *Processor) InputPorts() []component.Port {
	return append([]component.Port(nil), f.inputPorts...)
}

// OutputPorts returns the NATS output port for filtered messages.
func (f *Processor) OutputPorts() []component.Port {
	return append([]component.Port(nil), f.outputPorts...)
}

// ConfigSchema returns the configuration schema for this processor.
func (f *Processor) ConfigSchema() component.ConfigSchema {
	return jsonFilterSchema
}

// Health returns the current health status of this processor.
func (f *Processor) Health() component.HealthStatus {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    f.running,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&f.errors)),
		Uptime:     time.Since(f.startTime),
	}
}

// DataFlow returns current data flow metrics for this processor.
func (f *Processor) DataFlow() component.FlowMetrics {
	f.mu.RLock()
	defer f.mu.RUnlock()

	processed := atomic.LoadInt64(&f.messagesProcessed)
	errorCount := atomic.LoadInt64(&f.errors)

	var errorRate float64
	if processed > 0 {
		errorRate = float64(errorCount) / float64(processed)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      f.lastActivity,
	}
}

// Register registers the JSON filter processor component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "json_filter",
		Factory:     NewProcessor,
		Ports:       DeclarePorts,
		Schema:      jsonFilterSchema,
		Type:        "processor",
		Protocol:    "json_filter",
		Domain:      "processing",
		Description: "GenericJSON (core .json.v1) filter for field-based filtering",
		Version:     "0.1.0",
	})
}
