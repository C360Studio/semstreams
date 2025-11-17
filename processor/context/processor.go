// Package context provides derived state computation for complex rules
package context

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
	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/graph/query"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Static interface check - compile-time verification
var _ component.LifecycleComponent = (*Processor)(nil)

// schema defines the configuration schema for context processor component
// Generated from Config struct tags using reflection
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Rule defines a configurable context flag computation
type Rule struct {
	Name        string   `json:"name"`         // Flag name (e.g., "in_formation")
	Type        string   `json:"type"`         // "edge", "property", or "status"
	EdgeTypes   []string `json:"edge_types"`   // Edge types to check (for "edge" type)
	TargetTypes []string `json:"target_types"` // Target entity types (optional, for "edge" type)
	Property    string   `json:"property"`     // Property name (for "property" type)
	Value       any      `json:"value"`        // Expected value (for "property" type)
	Statuses    []string `json:"statuses"`     // Entity statuses (for "status" type)
}

// Config holds configuration for the ContextProcessor
type Config struct {
	// Enable/disable context processing
	Enabled bool `json:"enabled" schema:"type:bool,description:Enable context processing,default:true,category:basic"`

	// Maximum query depth for context computation

	MaxQueryDepth int `json:"max_query_depth" schema:"type:int,description:Maximum depth for path queries,default:3,category:advanced"`

	// Maximum nodes to visit per query

	MaxQueryNodes int `json:"max_query_nodes" schema:"type:int,description:Maximum nodes to visit per query,default:100,category:advanced"`

	// Maximum query time limit (stored as duration string like "100ms")

	MaxQueryTime time.Duration `json:"max_query_time" schema:"type:string,description:Maximum time per query,default:100ms,category:advanced"`

	// Edge types to consider for context computation

	EdgeFilter []string `json:"edge_filter" schema:"type:array,description:Edge types to consider for context,category:advanced"`

	// Context state TTL in KV bucket (stored as duration string like "24h")

	StatesTTL time.Duration `json:"states_ttl" schema:"type:string,description:Context state TTL in KV bucket,default:24h,category:advanced"`

	// How often to clean expired states (stored as duration string like "1h")

	CleanupInterval time.Duration `json:"cleanup_interval" schema:"type:string,description:How often to clean expired states,default:1h,category:advanced"`

	// Configurable context rules

	ContextRules []Rule `json:"context_rules" schema:"type:array,description:Configurable context flag rules,category:advanced"`

	// Dynamic port configuration (optional - overrides conventions)

	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration for inputs (NATS: process messages) and outputs (NATS: context-enriched messages),category:basic"`
}

// DefaultConfig returns sensible defaults for edge devices
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		MaxQueryDepth:   3,
		MaxQueryNodes:   100,
		MaxQueryTime:    100 * time.Millisecond,
		EdgeFilter:      []string{"MEMBER_OF_FORMATION", "NEAR", "LOCATED_AT", "POWERED_BY"},
		StatesTTL:       24 * time.Hour,
		CleanupInterval: time.Hour,
		ContextRules: []Rule{
			{
				Name:      "in_formation",
				Type:      "edge",
				EdgeTypes: []string{"MEMBER_OF_FORMATION"},
			},
			{
				Name:        "near_charging",
				Type:        "edge",
				EdgeTypes:   []string{"NEAR"},
				TargetTypes: []string{"robotics.charging_station"},
			},
			{
				Name:     "mission_critical",
				Type:     "property",
				Property: "mission_critical",
				Value:    true,
			},
		},
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "process_messages",
					Type:        "nats",
					Subject:     "process.>",
					Required:    true,
					Description: "All process messages",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "context_output",
					Type:        "nats",
					Subject:     "process.context.output",
					Required:    false,
					Description: "Context-enriched messages",
				},
			},
		},
	}
}

// State represents the derived context flags for an entity
type State struct {
	EntityID  string          `json:"entity_id"`
	Flags     map[string]bool `json:"flags"`
	UpdatedAt time.Time       `json:"updated_at"`
	Version   uint64          `json:"version"`
	Metadata  map[string]any  `json:"metadata,omitempty"`
}

// Event represents a context ready event published to other components
type Event struct {
	EntityID string        `json:"entity_id"`
	Context  *State        `json:"context"`
	Metadata EventMetadata `json:"metadata"`
}

// EventMetadata contains metadata about the context event
type EventMetadata struct {
	Timestamp   time.Time `json:"timestamp"`
	Source      string    `json:"source"`
	Version     string    `json:"version"`
	ProcessTime int64     `json:"process_time_ms"` // Processing time in milliseconds
}

// Processor implements derived context computation as a component
type Processor struct {
	// Component interface fields
	metadata    component.Metadata
	inputPorts  []component.Port
	outputPorts []component.Port
	health      component.HealthStatus

	// Core dependencies
	natsClient  *natsclient.Client
	queryClient query.Client
	config      *Config
	logger      *slog.Logger

	// Runtime state
	shutdown  chan struct{}
	done      chan struct{}
	startTime time.Time
	mu        sync.RWMutex

	// NATS KV bucket for context state storage
	contextKV jetstream.KeyValue

	// Statistics (thread-safe atomic counters)
	stats struct {
		entitiesProcessed atomic.Int64
		flagsComputed     atomic.Int64
		queryErrors       atomic.Int64
		publishErrors     atomic.Int64
	}
}

// NewProcessor creates a new ContextProcessor
func NewProcessor(natsClient *natsclient.Client, queryClient query.Client, config *Config) (*Processor, error) {
	if natsClient == nil {
		return nil, fmt.Errorf("NATS client is required")
	}

	if queryClient == nil {
		return nil, fmt.Errorf("QueryClient is required")
	}

	if config == nil {
		cfg := DefaultConfig()
		config = &cfg
	}

	p := &Processor{
		metadata: component.Metadata{
			Name:        "context-processor",
			Type:        "processor",
			Description: "Computes derived context flags for complex rules",
			Version:     "1.0.0",
		},
		health: component.HealthStatus{
			Healthy:    true,
			LastCheck:  time.Now(),
			ErrorCount: 0,
		},
		natsClient:  natsClient,
		queryClient: queryClient,
		config:      config,
		logger:      slog.Default().With("component", "context-processor"),
	}

	// Set up ports
	p.setupPorts()

	return p, nil
}

// setupPorts configures input and output ports
func (p *Processor) setupPorts() {
	// Input port for entity events from GraphProcessor
	p.inputPorts = []component.Port{
		{
			Name:        "entity_events",
			Direction:   component.DirectionInput,
			Required:    true,
			Description: "Entity events from GraphProcessor",
			Config: component.NATSPort{
				Subject: "entity.events.*",
			},
		},
	}

	// Output port for context ready events
	p.outputPorts = []component.Port{
		{
			Name:        "context_ready",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "Context ready events for complex rules",
			Config: component.NATSPort{
				Subject: "context.events.READY",
			},
		},
	}
}

// Initialize prepares the processor and creates KV bucket
func (p *Processor) Initialize() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.config.Enabled {
		p.logger.Info("Context processor disabled by configuration")
		return nil
	}

	// Create CONTEXT_STATE KV bucket
	bucketConfig := jetstream.KeyValueConfig{
		Bucket:      "CONTEXT_STATE",
		Description: "Context state storage for derived entity flags",
		TTL:         p.config.StatesTTL,
		MaxBytes:    100 * 1024 * 1024, // 100MB limit for context states
	}

	contextKV, err := p.natsClient.CreateKeyValueBucket(context.Background(), bucketConfig)
	if err != nil {
		return fmt.Errorf("failed to create CONTEXT_STATE bucket: %w", err)
	}
	p.contextKV = contextKV

	p.logger.Info("Context processor initialized", "states_ttl", p.config.StatesTTL)
	return nil
}

// Start begins context processing
func (p *Processor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.config.Enabled {
		p.logger.Info("Context processor disabled, not starting")
		return nil
	}

	if p.shutdown != nil {
		return fmt.Errorf("processor already started")
	}

	// Create shutdown channels for coordinated shutdown
	p.shutdown = make(chan struct{})
	p.done = make(chan struct{})
	p.startTime = time.Now()

	// Subscribe to entity events with context passed through callback
	err := p.natsClient.Subscribe(ctx, "entity.events.*", func(msgCtx context.Context, data []byte) {
		// Use the provided context for proper cancellation propagation
		p.handleEntityEvent(msgCtx, data)
	})
	if err != nil {
		close(p.shutdown)
		p.shutdown = nil
		p.done = nil
		return fmt.Errorf("failed to subscribe to entity events: %w", err)
	}

	// Start cleanup routine for expired states
	if p.config.CleanupInterval > 0 {
		go p.runCleanup(ctx)
	}

	p.health.Healthy = true
	p.logger.Info("Context processor started", "monitoring_pattern", "entity.events.*")
	return nil
}

// handleEntityEvent processes incoming entity events and computes context
func (p *Processor) handleEntityEvent(ctx context.Context, data []byte) {
	// Check for shutdown
	select {
	case <-p.shutdown:
		return
	default:
	}

	startTime := time.Now()

	// Parse entity event (simplified - assume it contains EntityID)
	var event struct {
		EntityID string `json:"entity_id"`
		Type     string `json:"type"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		p.logger.Error("Failed to parse entity event", "error", err)
		p.stats.queryErrors.Add(1)
		p.updateHealthOnError("Failed to parse entity event")
		return
	}

	if event.EntityID == "" {
		p.logger.Warn("Entity event missing entity_id")
		return
	}

	// Process the entity event
	if err := p.processEntityEvent(ctx, event.EntityID, startTime); err != nil {
		p.logger.Error("Failed to process entity", "entity_id", event.EntityID, "error", err)
		p.stats.queryErrors.Add(1)
		p.updateHealthOnError(fmt.Sprintf("Failed to process entity %s", event.EntityID))
		return
	}

	p.stats.entitiesProcessed.Add(1)

	// Track processing time
	processTime := time.Since(startTime)
	p.logger.Debug("Processed entity", "entity_id", event.EntityID, "process_time", processTime)
}

// processEntityEvent computes context flags for an entity
func (p *Processor) processEntityEvent(ctx context.Context, entityID string, processStartTime time.Time) error {
	// Compute context flags using PathQuery
	flags, err := p.computeContextFlags(ctx, entityID)
	if err != nil {
		return fmt.Errorf("failed to compute context flags: %w", err)
	}

	// Get existing context state for version tracking
	existingState, err := p.getContextState(ctx, entityID)
	var version uint64 = 1
	if err == nil && existingState != nil {
		version = existingState.Version + 1
	}

	// Create new context state
	contextState := &State{
		EntityID:  entityID,
		Flags:     flags,
		UpdatedAt: time.Now(),
		Version:   version,
		Metadata:  make(map[string]any),
	}

	// Store in KV bucket
	if err := p.updateContextState(ctx, entityID, contextState); err != nil {
		return fmt.Errorf("failed to update context state: %w", err)
	}

	// Publish context ready event with actual processing time
	if err := p.publishContextReady(ctx, entityID, contextState, processStartTime); err != nil {
		p.logger.Error("Failed to publish context ready", "entity_id", entityID, "error", err)
		p.stats.publishErrors.Add(1)
		p.updateHealthOnError(fmt.Sprintf("Failed to publish context ready for %s", entityID))
		return err
	}

	p.stats.flagsComputed.Add(1)
	return nil
}

// computeContextFlags uses PathQuery to derive context flags
func (p *Processor) computeContextFlags(ctx context.Context, entityID string) (map[string]bool, error) {
	flags := make(map[string]bool)

	// QueryClient is guaranteed to be available since it's required in constructor

	// Create path query with resource limits
	pathQuery := query.PathQuery{
		StartEntity: entityID,
		MaxDepth:    p.config.MaxQueryDepth,
		MaxNodes:    p.config.MaxQueryNodes,
		MaxTime:     p.config.MaxQueryTime,
		EdgeFilter:  p.config.EdgeFilter,
		DecayFactor: 0.8, // Default decay factor for context queries
		MaxPaths:    100, // Limit paths for performance
	}

	// Execute path query using QueryClient
	result, err := p.queryClient.ExecutePathQuery(ctx, pathQuery)
	if err != nil {
		return nil, fmt.Errorf("path query failed: %w", err)
	}

	// Use configured rules if available, otherwise use hardcoded defaults for backward compatibility
	if len(p.config.ContextRules) > 0 {
		// Iterate through configured rules
		for _, rule := range p.config.ContextRules {
			switch rule.Type {
			case "edge":
				flags[rule.Name] = p.evaluateEdgeRule(result, rule)
			case "property":
				flags[rule.Name] = p.evaluatePropertyRule(result, entityID, rule)
			case "status":
				flags[rule.Name] = p.evaluateStatusRule(result, entityID, rule)
			default:
				p.logger.Warn("Unknown rule type", "rule_type", rule.Type, "rule_name", rule.Name)
			}
		}
	} else {
		// Backward compatibility - use hardcoded flags
		flags["in_formation"] = p.hasEdgeType(result, "MEMBER_OF_FORMATION")
		flags["near_charging"] = p.hasNearChargingStation(result)
		flags["mission_critical"] = p.isMissionCritical(result, entityID)
	}

	return flags, nil
}

// evaluateEdgeRule checks for edges matching the rule criteria
func (p *Processor) evaluateEdgeRule(result *query.PathResult, rule Rule) bool {
	for _, entity := range result.Entities {
		for _, edge := range entity.Edges {
			if p.edgeMatchesRule(result.Entities, edge, rule) {
				return true
			}
		}
	}
	return false
}

// edgeMatchesRule checks if a single edge matches the rule criteria
func (p *Processor) edgeMatchesRule(entities []*gtypes.EntityState, edge gtypes.Edge, rule Rule) bool {
	// Check if edge type matches
	if !p.edgeTypeMatches(edge.EdgeType, rule.EdgeTypes) {
		return false
	}

	// Check target type if specified
	if len(rule.TargetTypes) > 0 {
		return p.targetTypeMatches(entities, edge.ToEntityID, rule.TargetTypes)
	}

	return true
}

// edgeTypeMatches checks if an edge type is in the allowed list
func (p *Processor) edgeTypeMatches(edgeType string, allowedTypes []string) bool {
	for _, allowedType := range allowedTypes {
		if edgeType == allowedType {
			return true
		}
	}
	return false
}

// targetTypeMatches checks if the target entity type matches allowed target types
func (p *Processor) targetTypeMatches(entities []*gtypes.EntityState, targetID string, allowedTypes []string) bool {
	targetEntity := p.findEntityByID(entities, targetID)
	if targetEntity == nil {
		return false
	}

	for _, allowedType := range allowedTypes {
		if targetEntity.Node.Type == allowedType {
			return true
		}
	}
	return false
}

// evaluatePropertyRule checks entity properties against the rule criteria
func (p *Processor) evaluatePropertyRule(result *query.PathResult, entityID string, rule Rule) bool {
	entity := p.findEntityByID(result.Entities, entityID)
	if entity == nil {
		return false
	}

	// Check if entity has the specified property with the expected value
	if value, exists := entity.Node.Properties[rule.Property]; exists {
		return p.compareValues(value, rule.Value)
	}

	return false
}

// evaluateStatusRule checks entity status against the rule criteria
func (p *Processor) evaluateStatusRule(result *query.PathResult, entityID string, rule Rule) bool {
	entity := p.findEntityByID(result.Entities, entityID)
	if entity == nil {
		return false
	}

	// Check if entity status matches any of the specified statuses
	for _, status := range rule.Statuses {
		if string(entity.Node.Status) == status {
			return true
		}
	}

	return false
}

// hasEdgeType checks if any entity in the result has the specified edge type (backward compatibility)
func (p *Processor) hasEdgeType(result *query.PathResult, edgeType string) bool {
	for _, entity := range result.Entities {
		for _, edge := range entity.Edges {
			if edge.EdgeType == edgeType {
				return true
			}
		}
	}
	return false
}

// hasNearChargingStation checks for NEAR edges to charging station entities
func (p *Processor) hasNearChargingStation(result *query.PathResult) bool {
	for _, entity := range result.Entities {
		for _, edge := range entity.Edges {
			if edge.EdgeType == "NEAR" {
				// Check if the target entity is a charging station
				targetEntity := p.findEntityByID(result.Entities, edge.ToEntityID)
				if targetEntity != nil && targetEntity.Node.Type == "robotics.charging_station" {
					return true
				}
			}
		}
	}
	return false
}

// isMissionCritical checks entity properties for mission critical status
func (p *Processor) isMissionCritical(result *query.PathResult, entityID string) bool {
	entity := p.findEntityByID(result.Entities, entityID)
	if entity == nil {
		return false
	}

	// Check if entity has mission_critical property set to true
	if critical, exists := entity.Node.Properties["mission_critical"]; exists {
		if criticalBool, ok := critical.(bool); ok {
			return criticalBool
		}
	}

	// Check if entity status is critical or emergency
	return entity.Node.Status == gtypes.StatusCritical || entity.Node.Status == gtypes.StatusEmergency
}

// findEntityByID finds an entity in the result by ID
func (p *Processor) findEntityByID(entities []*gtypes.EntityState, entityID string) *gtypes.EntityState {
	for _, entity := range entities {
		if entity.Node.ID == entityID {
			return entity
		}
	}
	return nil
}

// getContextState retrieves existing context state from KV bucket
func (p *Processor) getContextState(ctx context.Context, entityID string) (*State, error) {
	entry, err := p.contextKV.Get(ctx, entityID)
	if err != nil {
		return nil, err
	}

	var contextState State
	if err := json.Unmarshal(entry.Value(), &contextState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal context state: %w", err)
	}

	return &contextState, nil
}

// updateContextState stores context state in KV bucket
func (p *Processor) updateContextState(ctx context.Context, entityID string, contextState *State) error {
	data, err := json.Marshal(contextState)
	if err != nil {
		return fmt.Errorf("failed to marshal context state: %w", err)
	}

	_, err = p.contextKV.Put(ctx, entityID, data)
	return err
}

// publishContextReady publishes a context ready event
func (p *Processor) publishContextReady(
	ctx context.Context, entityID string, contextState *State, processStartTime time.Time,
) error {
	processTime := time.Since(processStartTime)

	event := Event{
		EntityID: entityID,
		Context:  contextState,
		Metadata: EventMetadata{
			Timestamp:   time.Now(),
			Source:      "context-processor",
			Version:     "1.0.0",
			ProcessTime: processTime.Milliseconds(),
		},
	}

	// Convert event to map for GenericJSON wrapping
	var eventMap map[string]any
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal context event: %w", err)
	}
	if err := json.Unmarshal(eventJSON, &eventMap); err != nil {
		return fmt.Errorf("failed to convert event to map: %w", err)
	}

	// Wrap in GenericJSON payload
	payload := message.NewGenericJSON(eventMap)

	// Wrap in BaseMessage for transport (enforces clean architecture)
	baseMsg := message.NewBaseMessage(
		payload.Schema(), // message type: "core.json.v1"
		payload,          // the GenericJSONPayload
		"context-processor", // source component
	)

	// Marshal BaseMessage
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal BaseMessage: %w", err)
	}

	subject := fmt.Sprintf("context.events.READY.%s", entityID)
	return p.natsClient.Publish(ctx, subject, data)
}

// runCleanup periodically cleans expired context states
func (p *Processor) runCleanup(ctx context.Context) {
	defer func() {
		select {
		case <-p.done:
		default:
			close(p.done)
		}
	}()

	if !p.config.Enabled || p.config.CleanupInterval <= 0 {
		return
	}

	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.shutdown:
			return
		case <-ticker.C:
			if err := p.cleanupExpiredStates(ctx); err != nil {
				p.logger.Error("Failed to cleanup expired states", "error", err)
			}
		}
	}
}

// cleanupExpiredStates removes expired context states from KV bucket
func (p *Processor) cleanupExpiredStates(_ context.Context) error {
	// KV bucket with TTL handles expiration automatically
	// This could be extended to do manual cleanup if needed
	p.logger.Debug("Cleanup routine executed", "type", "ttl_based")
	return nil
}

// updateHealthOnError updates health status when errors occur
func (p *Processor) updateHealthOnError(errorMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.health.LastError = errorMsg
	p.health.ErrorCount++

	// Set unhealthy if error count is too high
	if p.health.ErrorCount > 100 {
		p.health.Healthy = false
	}
}

// Stop stops the processor with timeout
func (p *Processor) Stop(timeout time.Duration) error {
	p.mu.Lock()
	if p.shutdown == nil {
		p.mu.Unlock()
		return nil
	}
	close(p.shutdown)
	p.mu.Unlock()

	// Wait for graceful shutdown with provided timeout
	select {
	case <-p.done:
		p.mu.Lock()
		p.shutdown = nil
		p.done = nil
		p.health.Healthy = false
		p.mu.Unlock()
		p.logger.Info("Context processor stopped")
		return nil
	case <-time.After(timeout):
		p.mu.Lock()
		p.shutdown = nil
		p.done = nil
		p.health.Healthy = false
		p.mu.Unlock()
		return fmt.Errorf("shutdown timeout after %v", timeout)
	}
}

// Component interface implementations

// Meta returns component metadata
func (p *Processor) Meta() component.Metadata {
	return p.metadata
}

// InputPorts returns input port configuration
func (p *Processor) InputPorts() []component.Port {
	return p.inputPorts
}

// OutputPorts returns output port configuration
func (p *Processor) OutputPorts() []component.Port {
	return p.outputPorts
}

// ConfigSchema returns configuration schema
func (p *Processor) ConfigSchema() component.ConfigSchema {
	return schema
}

// Health returns health status
func (p *Processor) Health() component.HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	p.health.LastCheck = time.Now()
	if !p.startTime.IsZero() {
		p.health.Uptime = time.Since(p.startTime)
	}

	// Check if we've had too many errors
	queryErrors := p.stats.queryErrors.Load()
	publishErrors := p.stats.publishErrors.Load()
	if queryErrors > 100 {
		p.health.LastError = "High number of query errors"
		p.health.Healthy = false
	}
	p.health.ErrorCount = int(queryErrors + publishErrors)

	return p.health
}

// DataFlow returns data flow metrics
func (p *Processor) DataFlow() component.FlowMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	uptime := time.Since(p.startTime).Seconds()

	metrics := component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      time.Now(),
	}

	if uptime > 0 {
		entitiesProcessed := p.stats.entitiesProcessed.Load()
		metrics.MessagesPerSecond = float64(entitiesProcessed) / uptime

		if entitiesProcessed > 0 {
			queryErrors := p.stats.queryErrors.Load()
			metrics.ErrorRate = float64(queryErrors) / float64(entitiesProcessed)
		}
	}

	return metrics
}

// parseConfig parses configuration data into Config struct
func parseConfig(configData map[string]any) Config {
	config := DefaultConfig()

	if configData == nil {
		return config
	}

	if enabled, ok := configData["enabled"].(bool); ok {
		config.Enabled = enabled
	}

	if maxDepth, ok := configData["max_query_depth"].(float64); ok {
		config.MaxQueryDepth = int(maxDepth)
	}

	if maxNodes, ok := configData["max_query_nodes"].(float64); ok {
		config.MaxQueryNodes = int(maxNodes)
	}

	if maxTime, ok := configData["max_query_time"].(string); ok {
		if d, err := time.ParseDuration(maxTime); err == nil {
			config.MaxQueryTime = d
		}
	}

	if edgeFilter, ok := configData["edge_filter"].([]any); ok {
		config.EdgeFilter = make([]string, len(edgeFilter))
		for i, edge := range edgeFilter {
			if str, ok := edge.(string); ok {
				config.EdgeFilter[i] = str
			}
		}
	}

	if ttl, ok := configData["states_ttl"].(string); ok {
		if d, err := time.ParseDuration(ttl); err == nil {
			config.StatesTTL = d
		}
	}

	if interval, ok := configData["cleanup_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			config.CleanupInterval = d
		}
	}

	// Parse context rules if provided
	config.ContextRules = parseContextRules(configData["context_rules"])

	return config
}

// parseContextRules parses context rules from configuration data
func parseContextRules(rulesData any) []Rule {
	if rulesArray, ok := rulesData.([]any); ok {
		rules := make([]Rule, 0, len(rulesArray))
		for _, ruleData := range rulesArray {
			if ruleMap, ok := ruleData.(map[string]any); ok {
				rule := parseRule(ruleMap)
				rules = append(rules, rule)
			}
		}
		return rules
	}
	return nil
}

// parseRule parses a single rule from a map
func parseRule(ruleMap map[string]any) Rule {
	rule := Rule{}

	if name, ok := ruleMap["name"].(string); ok {
		rule.Name = name
	}
	if ruleType, ok := ruleMap["type"].(string); ok {
		rule.Type = ruleType
	}
	if property, ok := ruleMap["property"].(string); ok {
		rule.Property = property
	}
	if value := ruleMap["value"]; value != nil {
		rule.Value = value
	}

	rule.EdgeTypes = parseStringArray(ruleMap["edge_types"])
	rule.TargetTypes = parseStringArray(ruleMap["target_types"])
	rule.Statuses = parseStringArray(ruleMap["statuses"])

	// Validate the rule before returning
	if err := rule.Validate(); err != nil {
		slog.Warn("Invalid rule configuration", "error", err)
	}

	return rule
}

// parseStringArray parses an array of strings from configuration
func parseStringArray(data any) []string {
	if array, ok := data.([]any); ok {
		strings := make([]string, 0, len(array))
		for _, item := range array {
			if str, ok := item.(string); ok {
				strings = append(strings, str)
			}
		}
		return strings
	}
	return nil
}

// compareValues compares values with type safety for property rules
func (p *Processor) compareValues(actual, expected any) bool {
	switch exp := expected.(type) {
	case bool:
		if act, ok := actual.(bool); ok {
			return act == exp
		}
	case string:
		if act, ok := actual.(string); ok {
			return act == exp
		}
	case float64:
		if act, ok := actual.(float64); ok {
			return act == exp
		}
		// Handle int to float64 conversion
		if act, ok := actual.(int); ok {
			return float64(act) == exp
		}
	case int:
		if act, ok := actual.(int); ok {
			return act == exp
		}
		// Handle float64 to int comparison
		if act, ok := actual.(float64); ok {
			return int(act) == exp
		}
	}
	return false
}

// Validate validates rule configuration
func (r Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("rule name cannot be empty")
	}

	switch r.Type {
	case "edge":
		if len(r.EdgeTypes) == 0 {
			return fmt.Errorf("edge rule must specify edge_types")
		}
	case "property":
		if r.Property == "" {
			return fmt.Errorf("property rule must specify property name")
		}
		if r.Value == nil {
			return fmt.Errorf("property rule must specify expected value")
		}
	case "status":
		if len(r.Statuses) == 0 {
			return fmt.Errorf("status rule must specify statuses")
		}
	default:
		return fmt.Errorf("unknown rule type: %s", r.Type)
	}
	return nil
}

// Register registers the context processor component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "context-processor",
		Factory:     CreateContextProcessor,
		Schema:      schema,
		Type:        "processor",
		Protocol:    "context",
		Domain:      "semantic",
		Description: "Context processor for derived state computation and complex rules",
		Version:     "1.0.0",
	})
}

// CreateContextProcessor creates a context processor with ComponentConfig
func CreateContextProcessor(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Parse configuration with defaults
	cfg := DefaultConfig()

	// Parse raw config if provided
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}

		// Preserve default ports if not explicitly configured
		// This prevents JSON unmarshaling empty config {} from wiping out defaults
		if cfg.Ports == nil {
			cfg.Ports = DefaultConfig().Ports
		}
	}

	// Validate required dependencies
	if deps.NATSClient == nil {
		return nil, fmt.Errorf("NATS client is required")
	}

	// Create query client internally - NewClient doesn't do I/O, just creates cache
	// We'll pass a background context since this is just for cache initialization
	queryClient, err := query.NewClient(context.Background(), deps.NATSClient, query.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create query client: %w", err)
	}

	// Create processor and set logger from config
	processor, err := NewProcessor(deps.NATSClient, queryClient, &cfg)
	if err != nil {
		return nil, err
	}

	// Set logger from Dependencies
	processor.logger = deps.GetLoggerWithComponent("context-processor")

	return processor, nil
}

// Registration implements component.Registerable interface
func (p *Processor) Registration() component.Registration {
	return component.Registration{
		Name:        "context-processor",
		Type:        "processor",
		Protocol:    "context",
		Description: "Computes derived context flags for complex rules",
		Version:     "1.0.0",
		Factory:     nil, // Set by registry when registering
	}
}
