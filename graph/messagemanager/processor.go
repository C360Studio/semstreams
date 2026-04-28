// Package messagemanager handles all message processing business logic
package messagemanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// Manager implements the MessageHandler interface
type Manager struct {
	// Dependencies
	deps    Dependencies
	config  Config
	decoder *message.Decoder

	// Prometheus metrics for observability
	metrics *Metrics

	// Statistics tracking
	messagesProcessed int64
	lastActivity      time.Time
	mu                sync.RWMutex

	// Error handling
	errorCallback func(string)
}

// NewManager creates a new message manager
func NewManager(config Config, deps Dependencies, errorCallback func(string)) *Manager {
	return &Manager{
		deps:          deps,
		config:        config,
		decoder:       message.NewDecoder(deps.PayloadRegistry),
		metrics:       NewMetrics(deps.MetricsRegistry),
		errorCallback: errorCallback,
		lastActivity:  time.Now(),
	}
}

// ProcessWork processes raw message data from worker pool
func (mp *Manager) ProcessWork(ctx context.Context, data []byte) error {
	// Add panic recovery to prevent worker crashes
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Errorf("panic: %v", r)
			err := errs.WrapFatal(msg, "MessageManager", "ProcessMessage",
				"message processing panic")
			mp.deps.Logger.Error("Message processing panic", "panic", r, "data_len", len(data))
			mp.recordError(err.Error())
		}
	}()

	// Create context for this message processing
	msgCtx, cancel, err := mp.createMessageContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	// Update message processing stats
	mp.recordMessageProcessed()

	// Parse and extract payload from transport envelope
	payload, messageType, err := mp.parseBaseMessage(data)
	if err != nil {
		return err
	}

	// Handle StoredMessage payloads (from ObjectStore)
	if storedMsg, ok := payload.(*objectstore.StoredMessage); ok {
		return mp.processStoredMessage(msgCtx, storedMsg, messageType)
	}

	// Handle other payload types (generic processing)
	return mp.processGenericPayload(msgCtx, payload, messageType)
}

// createMessageContext creates a context with appropriate timeout for message processing.
func (mp *Manager) createMessageContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	const defaultTimeout = 30 * time.Second

	// Check if parent has a deadline and respect it
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil, context.DeadlineExceeded
		}
		if remaining > defaultTimeout {
			remaining = defaultTimeout
		}
		msgCtx, cancel := context.WithTimeout(ctx, remaining)
		return msgCtx, cancel, nil
	}

	// No parent deadline, use default timeout
	msgCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	return msgCtx, cancel, nil
}

// recordMessageProcessed updates message processing stats and metrics.
func (mp *Manager) recordMessageProcessed() {
	atomic.AddInt64(&mp.messagesProcessed, 1)
	mp.mu.Lock()
	mp.lastActivity = time.Now()
	mp.mu.Unlock()

	if mp.metrics != nil {
		mp.metrics.MessagesProcessed.Inc()
	}
}

// parseBaseMessage unmarshals raw data into a BaseMessage and extracts the payload.
func (mp *Manager) parseBaseMessage(data []byte) (any, string, error) {
	baseMsg, err := mp.decoder.Decode(data)
	if err != nil {
		mp.recordError(fmt.Sprintf("failed to unmarshal BaseMessage: %v", err))
		if mp.metrics != nil {
			mp.metrics.MessagesFailed.Inc()
		}
		return nil, "", err
	}

	// Extract message type for logging
	messageType := "unknown"
	if msgType := baseMsg.Type(); msgType.Domain != "" && msgType.Category != "" {
		messageType = msgType.Domain + "." + msgType.Category
	}

	return baseMsg.Payload(), messageType, nil
}

// processStoredMessage handles StoredMessage payloads from ObjectStore.
func (mp *Manager) processStoredMessage(ctx context.Context, storedMsg *objectstore.StoredMessage, messageType string) error {
	mp.deps.Logger.Debug("Processing StoredMessage from BaseMessage",
		"entity_id", storedMsg.EntityID(),
		"message_type", storedMsg.MessageType(),
		"has_storage_ref", storedMsg.StorageRef() != nil,
		"transport_type", messageType)

	entityStates, err := mp.ProcessMessage(ctx, storedMsg)
	if err != nil {
		mp.recordError(fmt.Sprintf("failed to process StoredMessage: %v", err))
		return err
	}

	mp.deps.Logger.Debug("Processed StoredMessage into entity states", "count", len(entityStates))
	mp.storeEntityStates(ctx, entityStates)
	return nil
}

// processGenericPayload handles non-StoredMessage payload types.
func (mp *Manager) processGenericPayload(ctx context.Context, payload any, messageType string) error {
	mp.deps.Logger.Debug("Processing generic payload from BaseMessage", "type", messageType)

	entityStates, err := mp.ProcessMessage(ctx, payload)
	if err != nil {
		mp.recordError(fmt.Sprintf("failed to process payload: %v", err))
		return err
	}

	mp.deps.Logger.Debug("Processed entity states from payload", "count", len(entityStates))
	mp.storeEntityStates(ctx, entityStates)
	return nil
}

// storeEntityStates persists entity states and records metrics.
func (mp *Manager) storeEntityStates(ctx context.Context, entityStates []*gtypes.EntityState) {
	for _, state := range entityStates {
		if mp.metrics != nil {
			mp.metrics.EntitiesExtracted.Inc()
			mp.metrics.EntitiesUpdateAttempts.Inc()
		}
		// Use UpsertEntity for atomic create-or-update semantics
		if _, err := mp.deps.EntityManager.UpsertEntity(ctx, state); err != nil {
			mp.recordError(fmt.Sprintf("failed to store entity %s: %v", state.ID, err))
			if mp.metrics != nil {
				mp.metrics.EntitiesUpdateFailed.Inc()
			}
			continue
		}
		if mp.metrics != nil {
			mp.metrics.EntitiesUpdateSuccess.Inc()
		}
	}
}

// ProcessMessage processes any message type into entity states
func (mp *Manager) ProcessMessage(ctx context.Context, msg any) ([]*gtypes.EntityState, error) {
	// Default nil storage reference
	var storageRef *message.StorageReference

	// Check if message implements Storable interface (has storage reference)
	if storable, ok := msg.(message.Storable); ok {
		// Extract storage reference from Storable
		storageRef = storable.StorageRef()
		mp.deps.Logger.Debug("Message implements Storable, processing via Storable path",
			"msg_type", fmt.Sprintf("%T", msg),
			"entity_id", storable.EntityID(),
			"has_storage_ref", storageRef != nil)
		if storageRef != nil {
			mp.deps.Logger.Debug("StorageReference details",
				"storage_key", storageRef.Key,
				"storage_instance", storageRef.StorageInstance)
		}
		// Process as Graphable (Storable embeds Graphable)
		return mp.processSimpleGraphable(ctx, storable, storageRef)
	}

	// Check if message implements Graphable interface (no storage reference)
	mp.deps.Logger.Debug("Checking Graphable assertion",
		"msg_type", fmt.Sprintf("%T", msg),
		"msg_value", fmt.Sprintf("%+v", msg))
	if graphable, ok := msg.(gtypes.Graphable); ok {
		mp.deps.Logger.Debug("Processing Graphable without storage reference",
			"entity_id", graphable.EntityID())
		return mp.processSimpleGraphable(ctx, graphable, storageRef)
	}
	mp.deps.Logger.Debug("Graphable assertion failed, falling back to legacy processing",
		"msg_type", fmt.Sprintf("%T", msg))

	// Fall back to basic entity extraction for backward compatibility
	return mp.processNonGraphableMessage(ctx, msg, storageRef)
}

// processSimpleGraphable processes a message using the Graphable interface
func (mp *Manager) processSimpleGraphable(
	ctx context.Context, graphable gtypes.Graphable, storageRef *message.StorageReference,
) ([]*gtypes.EntityState, error) {
	entityID := graphable.EntityID()
	mp.deps.Logger.Debug("processSimpleGraphable called",
		"graphable_type", fmt.Sprintf("%T", graphable),
		"entity_id", entityID,
		"has_storage_ref", storageRef != nil)
	if entityID == "" {
		mp.deps.Logger.Debug("Empty entityID, returning empty result")
		return []*gtypes.EntityState{}, nil
	}

	// Resolve alias to actual entity ID
	actualEntityID, err := mp.deps.IndexManager.ResolveAlias(ctx, entityID)
	if err != nil {
		mp.deps.Logger.Debug("Failed to resolve alias, using original ID", "entity_id", entityID, "error", err)
		actualEntityID = entityID
	}

	if actualEntityID != entityID {
		mp.deps.Logger.Debug("Resolved alias to entity ID", "alias", entityID, "entity_id", actualEntityID)
	}

	// Get triples - triples are the single source of truth
	triples := graphable.Triples()
	mp.deps.Logger.Debug("Extracted triples from Graphable",
		"entity_id", actualEntityID,
		"triple_count", len(triples))

	// Extract message type if available (for semantic search filtering)
	var msgType message.Type
	if msg, ok := graphable.(message.Message); ok {
		msgType = msg.Type() // message.Type struct (domain.category.version)
	}

	// Create entity state - triples are single source of truth for both properties and relationships
	state := &gtypes.EntityState{
		ID:          actualEntityID,
		Triples:     triples,    // Triples contain all properties and relationships
		StorageRef:  storageRef, // Optional storage reference
		MessageType: msgType,    // Original message type for filtering
		Version:     1,
		UpdatedAt:   time.Now(),
	}

	// Try to merge with existing entity if present (best-effort, non-blocking)
	// This avoids TOCTOU race conditions by using upsert semantics
	existing, _ := mp.deps.EntityManager.GetEntity(ctx, actualEntityID)
	if existing != nil {
		// Entity exists, merge triples and increment version
		mp.deps.Logger.Debug("Entity exists, merging triples",
			"entity_id", actualEntityID,
			"existing_triples", len(existing.Triples),
			"new_triples", len(state.Triples))

		// Merge triples - triples are single source of truth for properties and relationships
		state.Triples = gtypes.MergeTriples(existing.Triples, state.Triples)
		state.Version = existing.Version + 1

		mp.deps.Logger.Debug("Merged triples complete",
			"entity_id", actualEntityID,
			"final_triple_count", len(state.Triples))
	}

	// Use upsert to atomically create or update - avoids TOCTOU race
	if mp.metrics != nil {
		mp.metrics.EntitiesUpdateAttempts.Inc()
	}
	if _, err := mp.deps.EntityManager.UpsertEntity(ctx, state); err != nil {
		if mp.metrics != nil {
			mp.metrics.EntitiesUpdateFailed.Inc()
		}
		return nil, errs.WrapTransient(err, "MessageManager",
			"processSimpleGraphable", "entity upsert failed")
	}
	if mp.metrics != nil {
		mp.metrics.EntitiesUpdateSuccess.Inc()
	}

	return []*gtypes.EntityState{state}, nil
}

// processNonGraphableMessage handles messages that don't implement Graphable
func (mp *Manager) processNonGraphableMessage(
	ctx context.Context, msg any, storageRef *message.StorageReference,
) ([]*gtypes.EntityState, error) {
	// Check for basic EntityID interface for backward compatibility
	identifiable, ok := msg.(interface{ EntityID() string })
	if !ok {
		// Try to handle map[string]any from JSON unmarshaling (common case)
		if msgMap, isMap := msg.(map[string]any); isMap {
			return mp.processMapMessage(ctx, msgMap, storageRef)
		}
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "message manager",
			"processNonGraphableMessage", "message missing required interfaces")
	}

	entityID := identifiable.EntityID()

	// Create basic metadata triples for non-Graphable messages
	// Note: entity_class and entity_role removed per ADR - domains own classification
	now := time.Now()
	triples := []message.Triple{
		{Subject: entityID, Predicate: "confidence", Object: 0.5, Timestamp: now},
		{Subject: entityID, Predicate: "source", Object: "legacy_interface", Timestamp: now},
	}

	// Extract message type if available
	var msgType message.Type
	if typedMsg, ok := msg.(message.Message); ok {
		msgType = typedMsg.Type()
	}

	// Create entity state - triples are single source of truth
	state := &gtypes.EntityState{
		ID:          entityID,
		Triples:     triples,
		StorageRef:  storageRef,
		MessageType: msgType,
		Version:     1,
		UpdatedAt:   now,
	}

	// Try to merge with existing entity if present (best-effort, non-blocking)
	existing, _ := mp.deps.EntityManager.GetEntity(ctx, entityID)
	if existing != nil {
		// Entity exists, merge triples and increment version
		state.Triples = gtypes.MergeTriples(existing.Triples, state.Triples)
		state.Version = existing.Version + 1
	}

	// Use upsert to atomically create or update - avoids TOCTOU race
	if mp.metrics != nil {
		mp.metrics.EntitiesUpdateAttempts.Inc()
	}
	if _, err := mp.deps.EntityManager.UpsertEntity(ctx, state); err != nil {
		if mp.metrics != nil {
			mp.metrics.EntitiesUpdateFailed.Inc()
		}
		return nil, errs.WrapTransient(err, "MessageManager", "processNonGraphableMessage", "entity upsert failed")
	}
	if mp.metrics != nil {
		mp.metrics.EntitiesUpdateSuccess.Inc()
	}

	return []*gtypes.EntityState{state}, nil
}

// processMapMessage processes a map[string]any message
func (mp *Manager) processMapMessage(
	ctx context.Context, msgMap map[string]any, storageRef *message.StorageReference,
) ([]*gtypes.EntityState, error) {
	// Extract entity information from map structure
	var entityID string

	// Try to extract standard fields
	if id, exists := msgMap["id"]; exists {
		entityID = fmt.Sprintf("%v", id)
	}

	// Use defaults if not provided
	if entityID == "" {
		entityID = fmt.Sprintf("%s.%s.map.%d",
			mp.config.DefaultNamespace, mp.config.DefaultPlatform, time.Now().UnixNano())
	}

	// Convert map entries to triples (excluding standard fields)
	now := time.Now()
	triples := []message.Triple{}
	for key, value := range msgMap {
		if key != "id" && key != "type" {
			triples = append(triples, message.Triple{
				Subject:   entityID,
				Predicate: key,
				Object:    value,
				Timestamp: now,
			})
		}
	}

	// Add processing metadata triples
	triples = append(triples,
		message.Triple{Subject: entityID, Predicate: "processed_at", Object: now.Format(time.RFC3339), Timestamp: now},
		message.Triple{Subject: entityID, Predicate: "source_type", Object: "map_message", Timestamp: now},
	)

	// Create entity state with triples as single source of truth
	state := &gtypes.EntityState{
		ID:          entityID,
		Triples:     triples,
		StorageRef:  storageRef,
		MessageType: message.Type{}, // Empty type for map messages
		Version:     1,
		UpdatedAt:   now,
	}

	// Try to merge with existing entity if present (best-effort, non-blocking)
	existing, _ := mp.deps.EntityManager.GetEntity(ctx, entityID)
	if existing != nil {
		// Entity exists, merge triples and increment version
		state.Triples = gtypes.MergeTriples(existing.Triples, state.Triples)
		state.Version = existing.Version + 1
	}

	// Use upsert to atomically create or update - avoids TOCTOU race
	if mp.metrics != nil {
		mp.metrics.EntitiesUpdateAttempts.Inc()
	}
	if _, err := mp.deps.EntityManager.UpsertEntity(ctx, state); err != nil {
		if mp.metrics != nil {
			mp.metrics.EntitiesUpdateFailed.Inc()
		}
		return nil, errs.WrapTransient(err, "MessageManager", "processMapMessage", "entity upsert failed")
	}
	if mp.metrics != nil {
		mp.metrics.EntitiesUpdateSuccess.Inc()
	}

	return []*gtypes.EntityState{state}, nil
}

// NOTE: extractPropertiesAndRelationships and buildEdgesFromRelationships have been removed
// as part of the greenfield migration to triples as single source of truth.
// All properties and relationships are now stored directly as triples in EntityState.Triples

// extractTypeFromEntityID extracts entity type from fully qualified entity ID
func (mp *Manager) extractTypeFromEntityID(entityID string) string {
	// Entity ID format: org.platform.domain.system.type.instance
	// Example: c360.platform1.robotics.mav1.battery.0
	parts := strings.Split(entityID, ".")
	if len(parts) >= 5 {
		return parts[4] // type is the 5th part (0-indexed)
	}
	return "entity" // fallback for malformed IDs
}

// recordError records an error for debugging
func (mp *Manager) recordError(errorMsg string) {
	if mp.errorCallback != nil {
		mp.errorCallback(errorMsg)
	}
	mp.deps.Logger.Error("Message manager error", "error", errorMsg)
}

// GetStats returns processing statistics
func (mp *Manager) GetStats() ProcessingStats {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	return ProcessingStats{
		MessagesProcessed: atomic.LoadInt64(&mp.messagesProcessed),
		LastActivity:      mp.lastActivity,
	}
}

// SetIndexManager sets the index manager dependency (for circular dependency resolution)
func (mp *Manager) SetIndexManager(indexManager IndexManager) {
	mp.deps.IndexManager = indexManager
}

// ProcessingStats holds processing statistics
type ProcessingStats struct {
	MessagesProcessed int64     `json:"messages_processed"`
	LastActivity      time.Time `json:"last_activity"`
}
