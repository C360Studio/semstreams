// Package jsonfilter provides a core processor for filtering GenericJSON messages
package jsonfilter

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
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "raw.>",
			Interface:   "core .json.v1", // Require GenericJSON
			Required:    true,
			Description: "NATS subjects to filter (must be GenericJSON payloads)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "nats_output",
			Type:        "nats",
			Subject:     "filtered.messages",
			Interface:   "core .json.v1", // Output GenericJSON
			Required:    true,
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
	name        string
	subjects    []string
	outputSubjs []string // Support multiple output subjects
	rules       []FilterRule
	config      Config // Store full config for port type checking
	decoder     *message.Decoder
	natsClient  *natsclient.Client
	logger      *slog.Logger

	// Lifecycle management
	shutdown      chan struct{}
	done          chan struct{}
	running       bool
	startTime     time.Time
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	wg            *sync.WaitGroup
	subscriptions []*natsclient.Subscription

	// Metrics (atomic counters for DataFlow)
	messagesProcessed int64
	messagesFiltered  int64
	messagesPassed    int64
	errors            int64
	lastActivity      time.Time

	// Prometheus metrics
	metrics *filterMetrics

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter
}

// NewProcessor creates a new JSON filter processor from configuration
func NewProcessor(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "JSONFilterProcessor", "NewProcessor", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	var outputSubjects []string

	for _, input := range config.Ports.Inputs {
		if input.Type == "nats" || input.Type == "jetstream" {
			inputSubjects = append(inputSubjects, input.Subject)
		}
	}

	// Collect all output subjects (support multiple outputs)
	for _, output := range config.Ports.Outputs {
		if output.Type == "nats" || output.Type == "jetstream" {
			outputSubjects = append(outputSubjects, output.Subject)
		}
	}

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig, "JSONFilterProcessor", "NewProcessor",
			"no input subjects configured")
	}

	// Initialize metrics if registry provided
	metrics, err := newFilterMetrics(deps.MetricsRegistry, "json-filter-processor")
	if err != nil {
		deps.GetLogger().Error("Failed to initialize JSON filter metrics", "error", err)
		metrics = nil // Continue without metrics
	}

	return &Processor{
		name:        "json-filter-processor",
		subjects:    inputSubjects,
		outputSubjs: outputSubjects,
		rules:       config.Rules,
		config:      config, // Store full config for port type checking
		decoder:     message.NewDecoder(deps.PayloadRegistry),
		natsClient:  deps.NATSClient,
		logger:      deps.GetLogger(),
		shutdown:    make(chan struct{}),
		done:        make(chan struct{}),
		wg:          &sync.WaitGroup{},
		metrics:     metrics,
	}, nil
}

// Initialize prepares the processor (no-op for JSON filter)
func (f *Processor) Initialize() error {
	return nil
}

// Start begins filtering messages
func (f *Processor) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "JSONFilterProcessor", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "JSONFilterProcessor", "Start", "context already cancelled")
	}

	f.lifecycleMu.Lock()
	defer f.lifecycleMu.Unlock()

	if f.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "JSONFilterProcessor", "Start", "check running state")
	}

	if f.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "JSONFilterProcessor", "Start", "NATS client required")
	}

	// Recreate channels if component is being restarted
	f.shutdown = make(chan struct{})
	f.done = make(chan struct{})

	// Subscribe to input ports based on port type
	if err := f.setupSubscriptions(ctx); err != nil {
		return err
	}

	// Initialize lifecycle reporter for observability
	f.lifecycleReporter = component.NewCatalogLifecycleReporter(ctx, f.natsClient, f.name, f.logger)

	f.mu.Lock()
	f.running = true
	f.startTime = time.Now()
	f.mu.Unlock()

	// Report idle state after startup
	if f.lifecycleReporter != nil {
		if err := f.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
			f.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
		}
	}

	f.logger.Info("JSON filter processor started",
		"component", f.name,
		"input_subjects", f.subjects,
		"output_subjects", f.outputSubjs,
		"rules", len(f.rules))

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (f *Processor) setupSubscriptions(ctx context.Context) error {
	for _, port := range f.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

		switch port.Type {
		case "jetstream":
			if err := f.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "JSONFilterProcessor", "Start",
					fmt.Sprintf("JetStream consumer for %s", port.Subject))
			}

		case "nats":
			sub, err := f.natsClient.Subscribe(ctx, port.Subject, func(ctx context.Context, msg *nats.Msg) {
				f.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				f.logger.Error("Failed to subscribe to NATS subject",
					"component", f.name,
					"subject", port.Subject,
					"error", err)
				return errs.WrapTransient(err, "JSONFilterProcessor", "Start",
					fmt.Sprintf("subscribe to %s", port.Subject))
			}
			f.subscriptions = append(f.subscriptions, sub)
			f.logger.Debug("Subscribed to NATS subject successfully",
				"component", f.name,
				"subject", port.Subject,
				"output_subjects", f.outputSubjs,
				"rules_count", len(f.rules))

		default:
			f.logger.Warn("Unknown port type, skipping", "port", port.Name, "type", port.Type)
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (f *Processor) setupJetStreamConsumer(ctx context.Context, port component.PortDefinition) error {
	streamName := port.StreamName
	if streamName == "" {
		streamName = f.deriveStreamName(port.Subject)
	}
	if streamName == "" {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"JSONFilterProcessor",
			"setupJetStreamConsumer",
			fmt.Sprintf("derive stream name for subject %s", port.Subject))
	}

	if err := f.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "JSONFilterProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("wait for stream %s", streamName))
	}

	sanitizedSubject := sanitizeSubject(port.Subject)
	consumerName := fmt.Sprintf("json-filter-%s", sanitizedSubject)

	f.logger.Debug("Setting up JetStream consumer",
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

	err := f.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		f.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			f.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "JSONFilterProcessor", "setupJetStreamConsumer",
			fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	f.logger.Debug("JSON filter subscribed (JetStream)", "subject", port.Subject, "stream", streamName)
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

// deriveStreamName extracts stream name from subject convention
func (f *Processor) deriveStreamName(subject string) string {
	// Handle wildcard subjects
	subject = strings.TrimPrefix(subject, "*.")
	subject = strings.TrimSuffix(subject, ".>")
	subject = strings.TrimSuffix(subject, ".*")

	parts := strings.Split(subject, ".")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "*" || parts[0] == ">" {
		return ""
	}
	return strings.ToUpper(parts[0])
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
func (f *Processor) Stop(timeout time.Duration) error {
	f.lifecycleMu.Lock()
	defer f.lifecycleMu.Unlock()

	if !f.running {
		return nil
	}

	// Signal shutdown
	close(f.shutdown)

	// Unsubscribe from all NATS subjects
	for _, sub := range f.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			f.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	f.subscriptions = nil

	// Wait for goroutines with timeout
	waitCh := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errs.WrapTransient(
			errs.ErrConnectionTimeout,
			"JSONFilterProcessor", "Stop",
			fmt.Sprintf("graceful shutdown timeout after %v", timeout))
	}

	f.mu.Lock()
	f.running = false
	close(f.done)
	f.mu.Unlock()

	return nil
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (f *Processor) isJetStreamPortBySubject(subject string) bool {
	if f.config.Ports == nil {
		return false
	}
	for _, port := range f.config.Ports.Outputs {
		if port.Subject == subject {
			return port.Type == "jetstream"
		}
	}
	return false
}

// reportFiltering reports the filtering stage (throttled to avoid KV spam)
func (f *Processor) reportFiltering(ctx context.Context) {
	if f.lifecycleReporter != nil {
		if err := f.lifecycleReporter.ReportStage(ctx, "filtering"); err != nil {
			f.logger.Debug("failed to report lifecycle stage", slog.String("stage", "filtering"), slog.Any("error", err))
		}
	}
}

// handleMessage processes incoming GenericJSON messages
func (f *Processor) handleMessage(ctx context.Context, msgData []byte) {
	// Report filtering stage for lifecycle observability
	f.reportFiltering(ctx)

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
	ports := make([]component.Port, len(f.subjects))
	for i, subj := range f.subjects {
		ports[i] = component.Port{
			Name:      fmt.Sprintf("input_%d", i),
			Direction: component.DirectionInput,
			Required:  true,
			Config: component.NATSPort{
				Subject: subj,
				Interface: &component.InterfaceContract{
					Type:    "core .json.v1",
					Version: "v1",
				},
			},
		}
	}
	return ports
}

// OutputPorts returns the NATS output port for filtered messages.
func (f *Processor) OutputPorts() []component.Port {
	if len(f.outputSubjs) == 0 {
		return []component.Port{}
	}

	ports := make([]component.Port, 0, len(f.outputSubjs))
	for i, subject := range f.outputSubjs {
		ports = append(ports, component.Port{
			Name:      fmt.Sprintf("output_%d", i),
			Direction: component.DirectionOutput,
			Required:  false,
			Config: component.NATSPort{
				Subject: subject,
				Interface: &component.InterfaceContract{
					Type:    "core .json.v1",
					Version: "v1",
				},
			},
		})
	}
	return ports
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
		Schema:      jsonFilterSchema,
		Type:        "processor",
		Protocol:    "json_filter",
		Domain:      "processing",
		Description: "GenericJSON (core .json.v1) filter for field-based filtering",
		Version:     "0.1.0",
	})
}
