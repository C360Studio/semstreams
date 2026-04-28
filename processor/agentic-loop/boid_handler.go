package agenticloop

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/boid"
	"github.com/nats-io/nats.go/jetstream"
)

// Default signal TTL before expiration
const defaultSignalTTL = 30 * time.Second

// Pre-compiled regex patterns for entity extraction
var entityContextPattern = regexp.MustCompile(`\[Entity:\s*([^\]]+)\]`)

// Pre-compiled pattern for predicate-like patterns in tool results
// Matches snake_case patterns like "has_member", "related_to", "depends_on"
var predicatePattern = regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z][a-z0-9]*)+`)

// Known predicates used in graph traversal
// This is a curated list - add predicates as they become relevant
var knownPredicates = map[string]bool{
	// Common relationship predicates
	"has_member":      true,
	"member_of":       true,
	"related_to":      true,
	"depends_on":      true,
	"dependency_of":   true,
	"part_of":         true,
	"contains":        true,
	"owned_by":        true,
	"created_by":      true,
	"modified_by":     true,
	"assigned_to":     true,
	"belongs_to":      true,
	"connected_to":    true,
	"links_to":        true,
	"references":      true,
	"referenced_by":   true,
	"parent_of":       true,
	"child_of":        true,
	"sibling_of":      true,
	"located_at":      true,
	"located_in":      true,
	"works_with":      true,
	"reports_to":      true,
	"manages":         true,
	"implements":      true,
	"extends":         true,
	"uses":            true,
	"used_by":         true,
	"calls":           true,
	"called_by":       true,
	"instance_of":     true,
	"type_of":         true,
	"associated_with": true,
}

// activeSignal wraps a steering signal with its expiration time
type activeSignal struct {
	Signal    *boid.SteeringSignal
	ExpiresAt time.Time
}

// SignalStore maintains active steering signals per loop, organized by signal type.
// Signals are time-limited and automatically expire after TTL.
type SignalStore struct {
	mu      sync.RWMutex
	signals map[string]map[string]*activeSignal // loopID -> signalType -> signal
	ttl     time.Duration
}

// NewSignalStore creates a new signal store with the given TTL.
func NewSignalStore(ttl time.Duration) *SignalStore {
	if ttl == 0 {
		ttl = defaultSignalTTL
	}
	return &SignalStore{
		signals: make(map[string]map[string]*activeSignal),
		ttl:     ttl,
	}
}

// Store adds or updates a steering signal for a loop.
// Signals are keyed by loop ID and signal type (only most recent per type kept).
func (s *SignalStore) Store(signal *boid.SteeringSignal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.signals[signal.LoopID]; !exists {
		s.signals[signal.LoopID] = make(map[string]*activeSignal)
	}

	s.signals[signal.LoopID][signal.SignalType] = &activeSignal{
		Signal:    signal,
		ExpiresAt: time.Now().Add(s.ttl),
	}
}

// Get retrieves an active signal by loop ID and signal type.
// Returns nil if no signal exists or if it has expired.
func (s *SignalStore) Get(loopID, signalType string) *boid.SteeringSignal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	loopSignals, exists := s.signals[loopID]
	if !exists {
		return nil
	}

	active, exists := loopSignals[signalType]
	if !exists {
		return nil
	}

	// Check expiration
	if time.Now().After(active.ExpiresAt) {
		return nil
	}

	return active.Signal
}

// GetAll retrieves all active signals for a loop.
// Returns a map of signal type -> signal, excluding expired signals.
func (s *SignalStore) GetAll(loopID string) map[string]*boid.SteeringSignal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	loopSignals, exists := s.signals[loopID]
	if !exists {
		return nil
	}

	now := time.Now()
	result := make(map[string]*boid.SteeringSignal)
	for signalType, active := range loopSignals {
		if now.Before(active.ExpiresAt) {
			result[signalType] = active.Signal
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// Remove removes all signals for a loop (called on loop completion).
func (s *SignalStore) Remove(loopID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.signals, loopID)
}

// Cleanup removes expired signals (can be called periodically).
func (s *SignalStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for loopID, loopSignals := range s.signals {
		for signalType, active := range loopSignals {
			if now.After(active.ExpiresAt) {
				delete(loopSignals, signalType)
				removed++
			}
		}
		if len(loopSignals) == 0 {
			delete(s.signals, loopID)
		}
	}

	return removed
}

// BoidHandler manages agent position tracking and steering signal processing
// for Boids-style local coordination rules.
type BoidHandler struct {
	positionsBucket jetstream.KeyValue
	logger          *slog.Logger
	decoder         *message.Decoder

	// Entity ID extraction patterns
	entityIDPattern *regexp.Regexp

	// Active steering signals per loop
	signalStore *SignalStore
}

// NewBoidHandler creates a new boid handler with the default signal TTL.
// decoder may be nil for tests that don't exercise message decoding paths;
// HandleSteeringSignalMessage will fail loudly if invoked with a nil decoder.
func NewBoidHandler(positionsBucket jetstream.KeyValue, logger *slog.Logger, decoder *message.Decoder) *BoidHandler {
	return NewBoidHandlerWithTTL(positionsBucket, logger, decoder, defaultSignalTTL)
}

// NewBoidHandlerWithTTL creates a new boid handler with a custom signal TTL.
func NewBoidHandlerWithTTL(positionsBucket jetstream.KeyValue, logger *slog.Logger, decoder *message.Decoder, signalTTL time.Duration) *BoidHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if signalTTL <= 0 {
		signalTTL = defaultSignalTTL
	}
	return &BoidHandler{
		positionsBucket: positionsBucket,
		logger:          logger,
		decoder:         decoder,
		// Pattern to extract entity IDs from content (6-part federated IDs)
		entityIDPattern: regexp.MustCompile(`([a-z0-9_-]+\.){5}[a-z0-9_-]+`),
		signalStore:     NewSignalStore(signalTTL),
	}
}

// UpdatePosition updates an agent's position in the AGENT_POSITIONS KV bucket.
func (h *BoidHandler) UpdatePosition(ctx context.Context, pos *boid.AgentPosition) error {
	if h.positionsBucket == nil {
		h.logger.Debug("Positions bucket not configured, skipping position update")
		return nil
	}

	pos.LastUpdate = time.Now()

	data, err := json.Marshal(pos)
	if err != nil {
		return err
	}

	_, err = h.positionsBucket.Put(ctx, pos.LoopID, data)
	if err != nil {
		h.logger.Error("Failed to update position",
			"loop_id", pos.LoopID,
			"error", err)
		return err
	}

	h.logger.Debug("Position updated",
		"loop_id", pos.LoopID,
		"role", pos.Role,
		"focus_count", len(pos.FocusEntities),
		"velocity", pos.Velocity)

	return nil
}

// GetPosition retrieves an agent's position from the KV bucket.
func (h *BoidHandler) GetPosition(ctx context.Context, loopID string) (*boid.AgentPosition, error) {
	if h.positionsBucket == nil {
		return nil, nil
	}

	entry, err := h.positionsBucket.Get(ctx, loopID)
	if err != nil {
		return nil, err
	}

	var pos boid.AgentPosition
	if err := json.Unmarshal(entry.Value(), &pos); err != nil {
		return nil, err
	}

	return &pos, nil
}

// DeletePosition removes an agent's position when the loop completes.
func (h *BoidHandler) DeletePosition(ctx context.Context, loopID string) error {
	if h.positionsBucket == nil {
		return nil
	}

	if err := h.positionsBucket.Delete(ctx, loopID); err != nil {
		// Ignore "key not found" errors
		if !strings.Contains(err.Error(), "key not found") {
			h.logger.Warn("Failed to delete position",
				"loop_id", loopID,
				"error", err)
			return err
		}
	}

	h.logger.Debug("Position deleted", "loop_id", loopID)
	return nil
}

// ExtractEntitiesFromToolResult extracts entity IDs from tool result content.
// This captures which entities an agent accessed during tool execution.
func (h *BoidHandler) ExtractEntitiesFromToolResult(content string) []string {
	if content == "" {
		return nil
	}

	matches := h.entityIDPattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}

	// Deduplicate
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}

	return result
}

// ExtractPredicatesFromToolResult extracts predicate patterns from tool result content.
// This captures which relationship types an agent is following during graph traversal.
// Only known predicates are returned to avoid noise from random snake_case strings.
func (h *BoidHandler) ExtractPredicatesFromToolResult(content string) []string {
	if content == "" {
		return nil
	}

	matches := predicatePattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}

	// Deduplicate and filter to known predicates only
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, m := range matches {
		if !seen[m] && knownPredicates[m] {
			seen[m] = true
			result = append(result, m)
		}
	}

	return result
}

// ExtractEntitiesFromContext extracts entity IDs from context messages.
// Looks for "[Entity: {id}]" prefixes added by AddGraphEntityContext.
func (h *BoidHandler) ExtractEntitiesFromContext(content string) []string {
	if content == "" {
		return nil
	}

	// Use pre-compiled pattern for "[Entity: {id}]" format
	matches := entityContextPattern.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return nil
	}

	result := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			result = append(result, strings.TrimSpace(m[1]))
		}
	}

	return result
}

// ProcessSteeringSignal processes an incoming boid steering signal.
// Stores the signal for use during context building and tool prioritization,
// and applies steering to the context manager if provided.
func (h *BoidHandler) ProcessSteeringSignal(_ context.Context, signal *boid.SteeringSignal, cm *ContextManager) error {
	if signal == nil {
		return nil
	}

	// Store the signal for later use
	h.signalStore.Store(signal)

	h.logger.Debug("Stored steering signal",
		"loop_id", signal.LoopID,
		"signal_type", signal.SignalType,
		"strength", signal.Strength)

	switch signal.SignalType {
	case boid.SignalTypeSeparation:
		h.logger.Debug("Separation signal stored",
			"avoid_count", len(signal.AvoidEntities),
			"avoid_entities", signal.AvoidEntities)

	case boid.SignalTypeCohesion:
		if len(signal.SuggestedFocus) > 0 {
			h.logger.Debug("Cohesion signal stored",
				"suggested_count", len(signal.SuggestedFocus),
				"suggested_focus", signal.SuggestedFocus)
		}

	case boid.SignalTypeAlignment:
		if len(signal.AlignWith) > 0 {
			h.logger.Debug("Alignment signal stored",
				"align_patterns", len(signal.AlignWith),
				"align_with", signal.AlignWith)
		}
	}

	// Apply steering to context manager if provided
	if cm != nil {
		steering := BoidSteeringConfig{}

		switch signal.SignalType {
		case boid.SignalTypeSeparation:
			steering.AvoidEntities = signal.AvoidEntities
		case boid.SignalTypeCohesion:
			steering.PrioritizeEntities = signal.SuggestedFocus
		case boid.SignalTypeAlignment:
			steering.AlignPatterns = signal.AlignWith
		}

		cm.ApplyBoidSteering(steering)
	}

	return nil
}

// GetActiveSignal retrieves the most recent signal of a specific type for a loop.
func (h *BoidHandler) GetActiveSignal(loopID, signalType string) *boid.SteeringSignal {
	return h.signalStore.Get(loopID, signalType)
}

// GetActiveSignals retrieves all active signals for a loop.
func (h *BoidHandler) GetActiveSignals(loopID string) map[string]*boid.SteeringSignal {
	return h.signalStore.GetAll(loopID)
}

// ClearSignals removes all signals for a loop (called on loop completion).
func (h *BoidHandler) ClearSignals(loopID string) {
	h.signalStore.Remove(loopID)
}

// HandleSteeringSignalMessage handles incoming boid steering signal messages from NATS.
// Returns the signal type if successfully processed, empty string otherwise.
func (h *BoidHandler) HandleSteeringSignalMessage(ctx context.Context, data []byte, getContextManager func(loopID string) *ContextManager) string {
	baseMsg, err := h.decoder.Decode(data)
	if err != nil {
		h.logger.Error("Failed to unmarshal steering signal", "error", err)
		return ""
	}

	signalPtr, ok := baseMsg.Payload().(*boid.SteeringSignal)
	if !ok {
		h.logger.Warn("Unexpected payload type for steering signal",
			"type_got", baseMsg.Type().String())
		return ""
	}

	signal := *signalPtr
	cm := getContextManager(signal.LoopID)

	if err := h.ProcessSteeringSignal(ctx, &signal, cm); err != nil {
		h.logger.Error("Failed to process steering signal",
			"loop_id", signal.LoopID,
			"error", err)
		return ""
	}

	return signal.SignalType
}

// CalculateVelocity computes velocity based on position changes.
func (h *BoidHandler) CalculateVelocity(oldFocus, newFocus []string) float64 {
	return boid.CalculateVelocity(oldFocus, newFocus)
}

// ApplySteeringToEntities applies active steering signals to prioritize/deprioritize entities.
// Returns two lists: prioritized entities (from cohesion) and avoided entities (from separation).
func (h *BoidHandler) ApplySteeringToEntities(loopID string) (prioritize, avoid []string) {
	signals := h.signalStore.GetAll(loopID)
	if signals == nil {
		return nil, nil
	}

	// Separation signals: entities to avoid
	if sep, ok := signals[boid.SignalTypeSeparation]; ok && sep != nil {
		avoid = sep.AvoidEntities
	}

	// Cohesion signals: entities to prioritize
	if coh, ok := signals[boid.SignalTypeCohesion]; ok && coh != nil {
		prioritize = coh.SuggestedFocus
	}

	return prioritize, avoid
}

// GetAlignmentPatterns returns the alignment patterns (predicates) to follow.
func (h *BoidHandler) GetAlignmentPatterns(loopID string) []string {
	signal := h.signalStore.Get(loopID, boid.SignalTypeAlignment)
	if signal == nil {
		return nil
	}
	return signal.AlignWith
}

// FilterEntitiesBySignals filters a list of entities based on active steering signals.
// Entities in the avoid list are moved to the end, entities in prioritize list go first.
func (h *BoidHandler) FilterEntitiesBySignals(loopID string, entities []string) []string {
	prioritize, avoid := h.ApplySteeringToEntities(loopID)
	if len(prioritize) == 0 && len(avoid) == 0 {
		return entities
	}

	// Build lookup sets
	prioritizeSet := make(map[string]bool)
	for _, e := range prioritize {
		prioritizeSet[e] = true
	}
	avoidSet := make(map[string]bool)
	for _, e := range avoid {
		avoidSet[e] = true
	}

	// Partition entities: prioritized, normal, avoided
	var prioritized, normal, avoided []string
	for _, e := range entities {
		switch {
		case prioritizeSet[e]:
			prioritized = append(prioritized, e)
		case avoidSet[e]:
			avoided = append(avoided, e)
		default:
			normal = append(normal, e)
		}
	}

	// Reassemble: prioritized first, then normal, then avoided
	result := make([]string, 0, len(entities))
	result = append(result, prioritized...)
	result = append(result, normal...)
	result = append(result, avoided...)
	return result
}
