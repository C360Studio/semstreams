// Package inference provides structural anomaly detection and inference
// for identifying potential missing relationships in the knowledge graph.
package inference

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// HierarchyInference creates membership edges to container entities based on
// the 6-part entity ID structure. It operates synchronously - hierarchy triples
// are computed and returned before the entity is written to storage.
//
// Entity ID format: org.platform.domain.system.type.instance
// Example: acme.iot.sensors.hvac.temperature.001
//
// Container entities are auto-created with the following pattern:
//   - Type container:   org.platform.domain.system.type.group (6-part)
//   - System container: org.platform.domain.system.group.container (6-part)
//   - Domain container: org.platform.domain.group.container.level (6-part)
//
// Graph distances via containers:
//   - Same type siblings: 2 hops (entity → type.group ← entity)
//   - Same system, different type: 4 hops
//   - Same domain, different system: 6 hops
//
// This is a stateless utility - no lifecycle methods (Start/Stop).
type HierarchyInference struct {
	// entityManager handles entity existence checks and creation
	entityManager EntityManager

	// tripleAdder adds triples to entities (used for inverse edges on containers)
	tripleAdder TripleAdder

	// Configuration
	config HierarchyConfig

	// Logger for observability
	logger *slog.Logger

	// Cache of known container entities to avoid repeated existence checks
	containerCache   map[string]bool
	containerCacheMu sync.RWMutex

	// Metrics
	containersCreated atomic.Int64
	edgesCreated      atomic.Int64
	edgesFailed       atomic.Int64
}

// EntityManager provides entity existence checks and creation.
// Typically implemented by datamanager.Manager.
type EntityManager interface {
	ExistsEntity(ctx context.Context, id string) (bool, error)
	CreateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error)
	// ListWithPrefix returns entity IDs matching a prefix (for sibling discovery)
	ListWithPrefix(ctx context.Context, prefix string) ([]string, error)
}

// HierarchyConfig configures hierarchy container inference.
type HierarchyConfig struct {
	// Enabled activates hierarchy inference on entity creation
	Enabled bool `json:"enabled"`

	// CreateTypeEdges enables type membership edges (5-part prefix → type container)
	// StandardIRI: skos:broader
	CreateTypeEdges bool `json:"create_type_edges"`

	// CreateSystemEdges enables system membership edges (4-part prefix → system container)
	// StandardIRI: skos:broader
	CreateSystemEdges bool `json:"create_system_edges"`

	// CreateDomainEdges enables domain membership edges (3-part prefix → domain container)
	// StandardIRI: skos:broader
	CreateDomainEdges bool `json:"create_domain_edges"`

	// CreateTypeSiblings enables sibling edges between entities with the same type (5-part prefix)
	// When enabled, creates bidirectional hierarchy.type.sibling edges
	// Cost: O(N) per new entity where N is existing sibling count
	CreateTypeSiblings bool `json:"create_type_siblings"`
}

// DefaultHierarchyConfig returns sensible defaults for hierarchy inference.
func DefaultHierarchyConfig() HierarchyConfig {
	return HierarchyConfig{
		Enabled:           false, // Opt-in feature
		CreateTypeEdges:   true,
		CreateSystemEdges: true,
		CreateDomainEdges: true,
	}
}

// NewHierarchyInference creates a new hierarchy inference component.
//
// Parameters:
//   - entityManager: Component for entity existence checks and creation
//   - tripleAdder: Component that can add triples (for inverse edges on containers)
//   - config: Configuration for edge creation
//   - logger: Logger for observability (can be nil)
func NewHierarchyInference(
	entityManager EntityManager,
	tripleAdder TripleAdder,
	config HierarchyConfig,
	logger *slog.Logger,
) *HierarchyInference {
	if logger == nil {
		logger = slog.Default()
	}

	return &HierarchyInference{
		entityManager:  entityManager,
		tripleAdder:    tripleAdder,
		config:         config,
		logger:         logger,
		containerCache: make(map[string]bool),
	}
}

// isContainerEntity returns true if the entityID represents a container entity.
// Container entities end with .group, .container, or .level and have exactly 6 parts.
func isContainerEntity(entityID string) bool {
	if entityID == "" {
		return false
	}

	parts := strings.Split(entityID, ".")
	if len(parts) != 6 {
		return false
	}

	lastPart := parts[5]
	return lastPart == "group" || lastPart == "container" || lastPart == "level"
}

// GetHierarchyTriples returns hierarchy membership triples for the given entity ID.
// This method has NO side effects - it only computes triples, it doesn't write them.
// The caller must include these triples in the entity before writing to storage.
//
// For each enabled level (type, system, domain), it:
// 1. Computes the container entity ID
// 2. Auto-creates the container if it doesn't exist (side effect on containers only)
// 3. Returns a membership triple from entity to container
// 4. Adds inverse edge to container (container → contains → entity)
//
// Returns empty slice if hierarchy is disabled or entity ID is invalid.
func (h *HierarchyInference) GetHierarchyTriples(ctx context.Context, entityID string) ([]message.Triple, error) {
	if !h.config.Enabled {
		return nil, nil
	}

	// Skip container entities to prevent infinite cascade
	if isContainerEntity(entityID) {
		return nil, nil
	}

	// Parse entity ID to validate 6-part structure
	parts := strings.Split(entityID, ".")
	if len(parts) != 6 {
		// Not a valid 6-part EntityID, skip silently
		return nil, nil
	}

	var triples []message.Triple
	var errs []error

	// Create type membership: entity → type.group
	if h.config.CreateTypeEdges {
		typeContainerID := h.buildTypeContainerID(parts)
		triple, err := h.ensureContainerAndReturnEdge(ctx, entityID, typeContainerID, vocabulary.HierarchyTypeMember)
		if err != nil {
			errs = append(errs, err)
		} else if triple != nil {
			triples = append(triples, *triple)
		}
	}

	// Create sibling edges to entities with same type (5-part prefix)
	if h.config.CreateTypeSiblings {
		siblingTriples, err := h.createSiblingEdges(ctx, entityID, parts)
		if err != nil {
			// Log but don't fail - sibling edges are supplementary
			h.logger.Warn("Failed to create sibling edges",
				"entity_id", entityID,
				"error", err)
		} else {
			triples = append(triples, siblingTriples...)
		}
	}

	// Create system membership: entity → system.group.container
	if h.config.CreateSystemEdges {
		systemContainerID := h.buildSystemContainerID(parts)
		triple, err := h.ensureContainerAndReturnEdge(ctx, entityID, systemContainerID, vocabulary.HierarchySystemMember)
		if err != nil {
			errs = append(errs, err)
		} else if triple != nil {
			triples = append(triples, *triple)
		}
	}

	// Create domain membership: entity → domain.group.container.level
	if h.config.CreateDomainEdges {
		domainContainerID := h.buildDomainContainerID(parts)
		triple, err := h.ensureContainerAndReturnEdge(ctx, entityID, domainContainerID, vocabulary.HierarchyDomainMember)
		if err != nil {
			errs = append(errs, err)
		} else if triple != nil {
			triples = append(triples, *triple)
		}
	}

	if len(errs) > 0 {
		return triples, errors.Join(errs...)
	}

	return triples, nil
}

// OnEntityCreated is the legacy method kept for backwards compatibility.
// It calls GetHierarchyTriples and adds them using tripleAdder.
// New code should use GetHierarchyTriples directly to avoid cascading writes.
func (h *HierarchyInference) OnEntityCreated(ctx context.Context, entityID string) error {
	triples, err := h.GetHierarchyTriples(ctx, entityID)
	if err != nil {
		return err
	}

	// Add triples using tripleAdder (legacy behavior - causes cascade)
	var errs []error
	for _, triple := range triples {
		if err := h.tripleAdder.AddTriple(ctx, triple); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// buildTypeContainerID creates a 6-part type container ID.
// Input: [org, platform, domain, system, type, instance]
// Output: org.platform.domain.system.type.group
func (h *HierarchyInference) buildTypeContainerID(parts []string) string {
	return strings.Join(parts[:5], ".") + ".group"
}

// buildSystemContainerID creates a 6-part system container ID.
// Input: [org, platform, domain, system, type, instance]
// Output: org.platform.domain.system.group.container
func (h *HierarchyInference) buildSystemContainerID(parts []string) string {
	return strings.Join(parts[:4], ".") + ".group.container"
}

// buildDomainContainerID creates a 6-part domain container ID.
// Input: [org, platform, domain, system, type, instance]
// Output: org.platform.domain.group.container.level
func (h *HierarchyInference) buildDomainContainerID(parts []string) string {
	return strings.Join(parts[:3], ".") + ".group.container.level"
}

// createSiblingEdges creates bidirectional sibling edges between the new entity
// and all existing entities with the same type (5-part prefix).
//
// For each existing sibling:
//   - Returns a forward edge: newEntity → hierarchy.type.sibling → existingSibling
//   - Adds inverse edge directly: existingSibling → hierarchy.type.sibling → newEntity
//
// Cost: O(N) per new entity where N is existing sibling count.
func (h *HierarchyInference) createSiblingEdges(ctx context.Context, entityID string, parts []string) ([]message.Triple, error) {
	// Build 5-part prefix for sibling lookup (excludes instance part)
	prefix := strings.Join(parts[:5], ".")

	// Find existing members with same prefix
	existingMembers, err := h.entityManager.ListWithPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	var triples []message.Triple
	for _, siblingID := range existingMembers {
		// Skip self and container entities
		if siblingID == entityID || isContainerEntity(siblingID) {
			continue
		}

		// Forward edge: new entity → sibling → existing
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  vocabulary.HierarchyTypeSibling,
			Object:     siblingID,
			Context:    "inference.hierarchy",
			Confidence: 1.0, // Structural inference has perfect confidence
		})
		h.edgesCreated.Add(1)

		// Inverse edge: existing → sibling → new entity (update existing entity)
		// Since HierarchyTypeSibling is symmetric, both directions use same predicate
		inverseTriple := message.Triple{
			Subject:    siblingID,
			Predicate:  vocabulary.HierarchyTypeSibling,
			Object:     entityID,
			Context:    "inference.hierarchy",
			Confidence: 1.0,
		}

		if err := h.tripleAdder.AddTriple(ctx, inverseTriple); err != nil {
			// Log warning but don't fail - forward edge will still be returned
			h.logger.Warn("Failed to add inverse sibling edge",
				"sibling_id", siblingID,
				"new_entity_id", entityID,
				"error", err)
			h.edgesFailed.Add(1)
		} else {
			h.edgesCreated.Add(1)
		}
	}

	if len(triples) > 0 {
		h.logger.Debug("Created sibling edges",
			"entity_id", entityID,
			"sibling_count", len(triples))
	}

	return triples, nil
}

// ensureContainerAndReturnEdge creates the container entity if needed, then returns
// the membership edge triple WITHOUT adding it to the entity (caller must do that).
// Also adds inverse edge to container (container → contains → entity) for bidirectional traversal.
func (h *HierarchyInference) ensureContainerAndReturnEdge(ctx context.Context, entityID, containerID, predicate string) (*message.Triple, error) {
	// Ensure container exists (with caching)
	if err := h.ensureContainerExists(ctx, containerID); err != nil {
		h.edgesFailed.Add(1)
		return nil, err
	}

	// Create forward membership edge: entity → predicate → container
	forwardTriple := message.Triple{
		Subject:    entityID,
		Predicate:  predicate,
		Object:     containerID, // Real 6-part entity ID - IsRelationship() returns true
		Context:    "inference.hierarchy",
		Confidence: 1.0, // Structural inference has perfect confidence
	}

	h.edgesCreated.Add(1)

	// Create inverse edge: container → contains → entity
	// This enables direct traversal from container to its members without using IncomingIndex
	// NOTE: This IS a side effect - we're updating the container entity
	inversePredicate := vocabulary.GetInversePredicate(predicate)
	if inversePredicate != "" {
		inverseTriple := message.Triple{
			Subject:    containerID,
			Predicate:  inversePredicate, // e.g., hierarchy.type.contains
			Object:     entityID,
			Context:    "inference.hierarchy",
			Confidence: 1.0,
		}

		if err := h.tripleAdder.AddTriple(ctx, inverseTriple); err != nil {
			// Log warning but don't fail - forward edge will still be returned.
			// Count it on edgesFailed for parity with the sibling-inverse drop
			// above — ADR-056 Decision 4 flagged this container-inverse drop as
			// the least-observable (Warn-only, no counter). NOTE: the container
			// is materialised by ensureContainerExists above before this write,
			// so an absent target is not the cause here — this is a CAS/storage
			// failure, not a must-exist drop (the flip does not touch this path).
			h.edgesFailed.Add(1)
			h.logger.Warn("Failed to create inverse edge",
				"container_id", containerID,
				"entity_id", entityID,
				"inverse_predicate", inversePredicate,
				"error", err)
		} else {
			h.edgesCreated.Add(1)
		}
	}

	return &forwardTriple, nil
}

// ensureContainerExists creates a minimal container entity if it doesn't exist.
// Uses an in-memory cache to avoid repeated existence checks.
func (h *HierarchyInference) ensureContainerExists(ctx context.Context, containerID string) error {
	// Check cache first
	h.containerCacheMu.RLock()
	exists := h.containerCache[containerID]
	h.containerCacheMu.RUnlock()

	if exists {
		return nil
	}

	// Check if container exists in storage
	existsInStorage, err := h.entityManager.ExistsEntity(ctx, containerID)
	if err != nil {
		return err
	}

	if existsInStorage {
		// Update cache and return
		h.containerCacheMu.Lock()
		h.containerCache[containerID] = true
		h.containerCacheMu.Unlock()
		return nil
	}

	// Create minimal container entity
	containerEntity := &gtypes.EntityState{
		ID: containerID,
		Triples: []message.Triple{
			{
				Subject:    containerID,
				Predicate:  "entity.type.class",
				Object:     "hierarchy.container",
				Context:    "inference.hierarchy",
				Confidence: 1.0,
			},
		},
	}

	_, err = h.entityManager.CreateEntity(ctx, containerEntity)
	if err != nil {
		// Check if it's a "already exists" error (race condition with another goroutine)
		// In that case, it's not a failure - the container exists
		if strings.Contains(err.Error(), "exists") {
			h.containerCacheMu.Lock()
			h.containerCache[containerID] = true
			h.containerCacheMu.Unlock()
			return nil
		}
		return err
	}

	// Update cache and metrics
	h.containerCacheMu.Lock()
	h.containerCache[containerID] = true
	h.containerCacheMu.Unlock()

	h.containersCreated.Add(1)

	h.logger.Debug("Created hierarchy container entity",
		"container_id", containerID)

	return nil
}

// ClearCache resets the container cache. Call when containers might be deleted.
func (h *HierarchyInference) ClearCache() {
	h.containerCacheMu.Lock()
	h.containerCache = make(map[string]bool)
	h.containerCacheMu.Unlock()
}

// GetMetrics returns metrics for hierarchy inference operations.
func (h *HierarchyInference) GetMetrics() (containersCreated, edgesCreated, edgesFailed int64) {
	return h.containersCreated.Load(), h.edgesCreated.Load(), h.edgesFailed.Load()
}

// GetCacheStats returns statistics about the container cache.
func (h *HierarchyInference) GetCacheStats() int {
	h.containerCacheMu.RLock()
	defer h.containerCacheMu.RUnlock()

	return len(h.containerCache)
}
