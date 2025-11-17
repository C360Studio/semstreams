// Package jsonmapprocessor provides a core processor for transforming GenericJSON message fields
package jsonmapprocessor

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
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "raw.>",
			Interface:   "core .json.v1", // Require GenericJSON
			Required:    true,
			Description: "NATS subjects to transform (must be GenericJSON payloads)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "nats_output",
			Type:        "nats",
			Subject:     "mapped.messages",
			Interface:   "core .json.v1", // Output GenericJSON
			Required:    true,
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
	name         string
	subjects     []string
	outputSubj   string
	mappings     []FieldMapping
	addFields    map[string]any
	removeFields map[string]bool // Set for fast lookup
	natsClient   *natsclient.Client
	logger       *slog.Logger

	// Lifecycle management
	shutdown    chan struct{}
	done        chan struct{}
	running     bool
	startTime   time.Time
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	wg          *sync.WaitGroup

	// Metrics (atomic counters for DataFlow)
	messagesProcessed   int64
	messagesTransformed int64
	errors              int64
	lastActivity        time.Time

	// Prometheus metrics
	metrics *mapMetrics
}

// NewProcessor creates a new JSON map processor from configuration
func NewProcessor(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errors.WrapInvalid(err, "JSONMapProcessor", "NewProcessor", "config unmarshal")
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
			errors.ErrInvalidConfig, "JSONMapProcessor", "NewProcessor",
			"no input subjects configured")
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
		name:         "json-map-processor",
		subjects:     inputSubjects,
		outputSubj:   outputSubject,
		mappings:     config.Mappings,
		addFields:    config.AddFields,
		removeFields: removeFieldsSet,
		natsClient:   deps.NATSClient,
		logger:       deps.GetLogger(),
		shutdown:     make(chan struct{}),
		done:         make(chan struct{}),
		wg:           &sync.WaitGroup{},
		metrics:      metrics,
	}, nil
}

// Initialize prepares the processor (no-op for JSON map)
func (m *Processor) Initialize() error {
	return nil
}

// Start begins transforming messages
func (m *Processor) Start(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.running {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "JSONMapProcessor", "Start", "check running state")
	}

	if m.natsClient == nil {
		return errors.WrapFatal(errors.ErrMissingConfig, "JSONMapProcessor", "Start", "NATS client required")
	}

	// Subscribe to input subjects
	for _, subject := range m.subjects {
		m.logger.Debug("Subscribing to NATS subject",
			"component", m.name,
			"subject", subject)

		if err := m.natsClient.Subscribe(ctx, subject, m.handleMessage); err != nil {
			m.logger.Error("Failed to subscribe to NATS subject",
				"component", m.name,
				"subject", subject,
				"error", err)
			return errors.WrapTransient(err, "JSONMapProcessor", "Start", fmt.Sprintf("subscribe to %s", subject))
		}

		m.logger.Debug("Subscribed to NATS subject successfully",
			"component", m.name,
			"subject", subject,
			"output_subject", m.outputSubj,
			"mappings_count", len(m.mappings),
			"add_fields_count", len(m.addFields),
			"remove_fields_count", len(m.removeFields))
	}

	m.mu.Lock()
	m.running = true
	m.startTime = time.Now()
	m.mu.Unlock()

	m.logger.Info("JSON map processor started",
		"component", m.name,
		"input_subjects", m.subjects,
		"output_subject", m.outputSubj,
		"mappings", len(m.mappings),
		"add_fields", len(m.addFields),
		"remove_fields", len(m.removeFields))

	return nil
}

// Stop gracefully stops the processor
func (m *Processor) Stop(timeout time.Duration) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if !m.running {
		return nil
	}

	// Signal shutdown
	close(m.shutdown)

	// Wait for goroutines with timeout
	waitCh := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errors.WrapTransient(
			fmt.Errorf("shutdown timeout after %v", timeout),
			"JSONMapProcessor", "Stop", "graceful shutdown")
	}

	m.mu.Lock()
	m.running = false
	close(m.done)
	m.mu.Unlock()

	return nil
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
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msgData, &baseMsg); err != nil {
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

		if err := m.natsClient.Publish(ctx, m.outputSubj, transformedData); err != nil {
			atomic.AddInt64(&m.errors, 1)
			m.metrics.recordError(m.name, "publish")
			m.logger.Error("Failed to publish transformed message",
				"component", m.name,
				"output_subject", m.outputSubj,
				"error", err)
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
	ports := make([]component.Port, len(m.subjects))
	for i, subj := range m.subjects {
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

// OutputPorts returns the NATS output port for transformed messages.
func (m *Processor) OutputPorts() []component.Port {
	if m.outputSubj == "" {
		return []component.Port{}
	}
	return []component.Port{
		{
			Name:      "output",
			Direction: component.DirectionOutput,
			Required:  false,
			Config: component.NATSPort{
				Subject: m.outputSubj,
				Interface: &component.InterfaceContract{
					Type:    "core .json.v1",
					Version: "v1",
				},
			},
		},
	}
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
