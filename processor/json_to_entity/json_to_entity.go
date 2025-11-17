// Package jsontoentity converts GenericJSON messages to Entity messages for graph processing
package jsontoentity

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

// Config holds configuration for JSON to Entity converter
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// EntityIDField specifies which JSON field contains the entity ID
	EntityIDField string `json:"entity_id_field" schema:"type:string,description:JSON field for entity ID,default:entity_id"`

	// EntityTypeField specifies which JSON field contains the entity type
	EntityTypeField string `json:"entity_type_field" schema:"type:string,description:JSON field for entity type,default:entity_type"`

	// EntityClass specifies the default entity class if not in JSON
	EntityClass message.EntityClass `json:"entity_class" schema:"type:string,description:Default entity class,default:Thing"`

	// EntityRole specifies the default entity role
	EntityRole message.EntityRole `json:"entity_role" schema:"type:string,description:Default entity role,default:primary"`

	// SourceField specifies the source identifier for triple provenance
	SourceField string `json:"source_field" schema:"type:string,description:Source identifier,default:json_to_entity"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "generic.messages",
			Interface:   "core.json.v1", // Require GenericJSON
			Required:    true,
			Description: "NATS subjects with GenericJSON messages",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "nats_output",
			Type:        "nats",
			Subject:     "events.graph.entity",
			Interface:   "graph.Entity.v1", // Output Entity
			Required:    true,
			Description: "NATS subject for Entity messages",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		EntityIDField:   "entity_id",
		EntityTypeField: "entity_type",
		EntityClass:     message.ClassThing,
		EntityRole:      message.RolePrimary,
		SourceField:     "json_to_entity",
	}
}

// jsonToEntitySchema defines the configuration schema
var jsonToEntitySchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Processor converts GenericJSON to Entity messages
type Processor struct {
	name       string
	subjects   []string
	outputSubj string
	config     Config
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
	messagesConverted int64
	errors            int64
	lastActivity      time.Time
}

// NewProcessor creates a new JSON to Entity processor
func NewProcessor(
	rawConfig json.RawMessage, deps component.Dependencies,
) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errors.WrapInvalid(err, "JSONToEntityProcessor", "NewProcessor", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Set defaults for empty fields
	if config.EntityIDField == "" {
		config.EntityIDField = "entity_id"
	}
	if config.EntityTypeField == "" {
		config.EntityTypeField = "entity_type"
	}
	if config.EntityClass == "" {
		config.EntityClass = message.ClassThing
	}
	if config.EntityRole == "" {
		config.EntityRole = message.RolePrimary
	}
	if config.SourceField == "" {
		config.SourceField = "json_to_entity"
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
			errors.ErrInvalidConfig, "JSONToEntityProcessor", "NewProcessor",
			"no input subjects configured")
	}

	return &Processor{
		name:       "json-to-entity-processor",
		subjects:   inputSubjects,
		outputSubj: outputSubject,
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		wg:         &sync.WaitGroup{},
	}, nil
}

// Initialize prepares the processor
func (p *Processor) Initialize() error {
	return nil
}

// Start begins converting messages
func (p *Processor) Start(ctx context.Context) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if p.running {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "JSONToEntityProcessor", "Start", "check running state")
	}

	if p.natsClient == nil {
		return errors.WrapFatal(errors.ErrMissingConfig, "JSONToEntityProcessor", "Start", "NATS client required")
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
			return errors.WrapTransient(err, "JSONToEntityProcessor", "Start", fmt.Sprintf("subscribe to %s", subject))
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

	p.logger.Info("JSON to Entity processor started",
		"component", p.name,
		"input_subjects", p.subjects,
		"output_subject", p.outputSubj,
		"entity_id_field", p.config.EntityIDField,
		"entity_type_field", p.config.EntityTypeField)

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
			"JSONToEntityProcessor", "Stop", "graceful shutdown")
	}

	p.mu.Lock()
	p.running = false
	close(p.done)
	p.mu.Unlock()

	return nil
}

// handleMessage processes incoming GenericJSON messages and converts to Entity
func (p *Processor) handleMessage(ctx context.Context, msgData []byte) {
	atomic.AddInt64(&p.messagesProcessed, 1)
	p.mu.Lock()
	p.lastActivity = time.Now()
	p.mu.Unlock()

	p.logger.Debug("Received message",
		"component", p.name,
		"size_bytes", len(msgData))

	// Parse as BaseMessage containing GenericJSONPayload
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msgData, &baseMsg); err != nil {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Debug("Failed to parse message as BaseMessage",
			"component", p.name,
			"error", err)
		return
	}

	// Extract GenericJSON payload
	payload := baseMsg.Payload()
	genericJSON, ok := payload.(*message.GenericJSONPayload)
	if !ok {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Debug("Payload is not GenericJSON",
			"component", p.name,
			"payload_type", fmt.Sprintf("%T", payload))
		return
	}

	// Convert to Entity
	entity, err := p.convertToEntity(genericJSON)
	if err != nil {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Debug("Failed to convert to Entity",
			"component", p.name,
			"error", err)
		return
	}

	// Validate the entity payload
	if err := entity.Validate(); err != nil {
		atomic.AddInt64(&p.errors, 1)
		p.logger.Error("Entity validation failed",
			"component", p.name,
			"error", err)
		return
	}

	atomic.AddInt64(&p.messagesConverted, 1)

	// Wrap in BaseMessage for transport
	entityMsg := message.NewBaseMessage(
		entity.Schema(), // "graph.Entity.v1"
		entity,          // the EntityPayload (already a pointer)
		p.name,          // source component name
	)

	// Marshal and publish
	if p.outputSubj != "" {
		entityData, err := json.Marshal(entityMsg)
		if err != nil {
			atomic.AddInt64(&p.errors, 1)
			p.logger.Error("Failed to marshal BaseMessage",
				"component", p.name,
				"error", err)
			return
		}

		p.logger.Debug("Message converted to Entity",
			"component", p.name,
			"output_subject", p.outputSubj,
			"entity_id", entity.ID,
			"entity_type", entity.Type,
			"property_count", len(entity.Properties))

		if err := p.natsClient.Publish(ctx, p.outputSubj, entityData); err != nil {
			atomic.AddInt64(&p.errors, 1)
			p.logger.Error("Failed to publish Entity message",
				"component", p.name,
				"output_subject", p.outputSubj,
				"error", err)
		} else {
			p.logger.Debug("Published Entity message",
				"component", p.name,
				"output_subject", p.outputSubj,
				"entity_id", entity.ID)
		}
	}
}

// convertToEntity extracts entity data from GenericJSON
func (p *Processor) convertToEntity(genericJSON *message.GenericJSONPayload) (*message.EntityPayload, error) {
	data := genericJSON.Data

	// Extract entity_id
	entityID, ok := data[p.config.EntityIDField].(string)
	if !ok || entityID == "" {
		return nil, fmt.Errorf("missing or invalid %s field", p.config.EntityIDField)
	}

	// Extract entity_type
	entityType, ok := data[p.config.EntityTypeField].(string)
	if !ok || entityType == "" {
		return nil, fmt.Errorf("missing or invalid %s field", p.config.EntityTypeField)
	}

	// Create properties map (exclude entity_id and entity_type)
	properties := make(map[string]any)
	for key, value := range data {
		if key != p.config.EntityIDField && key != p.config.EntityTypeField {
			properties[key] = value
		}
	}

	// Create EntityPayload
	entity := message.NewEntityPayload(entityID, entityType, properties)
	entity.Class = p.config.EntityClass
	entity.Role = p.config.EntityRole
	entity.Source = p.config.SourceField
	entity.Timestamp = time.Now()
	entity.Confidence = 1.0

	return entity, nil
}

// Discoverable interface implementation

// Meta returns metadata describing this processor component
func (p *Processor) Meta() component.Metadata {
	return component.Metadata{
		Name:        p.name,
		Type:        "processor",
		Description: "Converts GenericJSON (core.json.v1) to Entity (graph.Entity.v1) for semantic graph processing",
		Version:     "0.1.0",
	}
}

// InputPorts returns the NATS input ports
func (p *Processor) InputPorts() []component.Port {
	ports := make([]component.Port, len(p.subjects))
	for i, subj := range p.subjects {
		ports[i] = component.Port{
			Name:      fmt.Sprintf("input_%d", i),
			Direction: component.DirectionInput,
			Required:  true,
			Config: component.NATSPort{
				Subject: subj,
				Interface: &component.InterfaceContract{
					Type:    "core.json.v1",
					Version: "v1",
				},
			},
		}
	}
	return ports
}

// OutputPorts returns the NATS output port
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
					Type:    "graph.Entity.v1",
					Version: "v1",
				},
			},
		},
	}
}

// ConfigSchema returns the configuration schema
func (p *Processor) ConfigSchema() component.ConfigSchema {
	return jsonToEntitySchema
}

// Health returns the current health status
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

// DataFlow returns current data flow metrics
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

// Register registers the JSON to Entity processor with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "json_to_entity",
		Factory:     NewProcessor,
		Schema:      jsonToEntitySchema,
		Type:        "processor",
		Protocol:    "json_to_entity",
		Domain:      "processing",
		Description: "Converts GenericJSON (core.json.v1) to Entity (graph.Entity.v1) for semantic graph processing",
		Version:     "0.1.0",
	})
}
