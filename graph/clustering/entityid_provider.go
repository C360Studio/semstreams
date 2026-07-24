// Package clustering provides community detection algorithms and graph providers.
package clustering

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/c360studio/semstreams/pkg/errs"
)

// EntityIDProvider wraps a base Provider and adds virtual edges
// based on EntityID hierarchy. This enables LPA clustering to find communities
// using the 6-part EntityID structure even when explicit relationship triples don't exist.
//
// EntityID format: org.platform.domain.system.type.instance
// Entities with the same 5-part TypePrefix (org.platform.domain.system.type) are considered siblings.
//
// Virtual edges are computed on-demand and cached for performance.
// These edges are NOT persisted - they're ephemeral hints for the clustering algorithm.
//
// Explicit edges (from base provider) always take precedence over virtual edges.
type EntityIDProvider struct {
	base Provider

	// Configuration
	siblingWeight      float64 // Weight for sibling edges (default: 0.7)
	maxSiblings        int     // Max sibling neighbors per entity (default: 10)
	includeSiblings    bool    // Whether to include sibling edges
	includeSystemPeers bool    // Whether to include system-affinity edges
	systemPeerWeight   float64 // Weight for system-affinity edges (default: 0.3)
	maxSystemPeers     int     // Max system peers per entity (default: 15)

	// Logger for debugging and observability
	logger *slog.Logger

	// Metrics for monitoring virtual edge operations
	siblingEdgeErrors  atomic.Int64
	siblingEdgeSuccess atomic.Int64

	// Cache for type prefix -> entity IDs mapping
	typePrefixCache map[string][]string
	// Cache for system -> entity IDs mapping (system = part[3] of entity ID)
	systemCache      map[string][]string
	cacheMu          sync.RWMutex
	cacheInitialized atomic.Bool
}

// EntityIDProviderConfig holds configuration for EntityIDProvider
type EntityIDProviderConfig struct {
	// SiblingWeight is the edge weight for sibling relationships.
	// Higher values = stronger connection influence in LPA.
	// Recommended: 0.7 (lower than explicit edges at 1.0)
	SiblingWeight float64

	// MaxSiblings limits sibling neighbors per entity to control
	// computation cost during LPA iterations.
	// Recommended: 10
	MaxSiblings int

	// IncludeSiblings enables sibling edge discovery.
	// Set to false to disable EntityID-based edges entirely.
	IncludeSiblings bool

	// IncludeSystemPeers enables system-affinity edges between entities
	// sharing the same system (part[3] of the 6-part entity ID).
	// This biases LPA toward system-coherent communities when the graph
	// contains entities from heterogeneous data sources.
	IncludeSystemPeers bool

	// SystemPeerWeight is the edge weight for system-affinity edges.
	// Lower than SiblingWeight because system is a weaker signal than
	// exact type match, but enough to bias LPA toward system-level coherence.
	// Recommended: 0.3
	SystemPeerWeight float64

	// MaxSystemPeers limits system-affinity neighbors per entity.
	// Recommended: 15
	MaxSystemPeers int
}

// DefaultEntityIDProviderConfig returns sensible defaults for clustering
func DefaultEntityIDProviderConfig() EntityIDProviderConfig {
	return EntityIDProviderConfig{
		SiblingWeight:      0.7,
		MaxSiblings:        10,
		IncludeSiblings:    true,
		IncludeSystemPeers: true,
		SystemPeerWeight:   0.3,
		MaxSystemPeers:     15,
	}
}

// NewEntityIDProvider creates a Provider that augments explicit edges
// with virtual edges based on EntityID hierarchy (6-part dotted format).
//
// Parameters:
//   - base: The underlying Provider for explicit edges (also used to list all entities)
//   - config: Configuration for edge weights and limits
//   - logger: Optional logger for observability (can be nil)
func NewEntityIDProvider(
	base Provider,
	config EntityIDProviderConfig,
	logger *slog.Logger,
) *EntityIDProvider {
	// Apply defaults for zero values
	if config.SiblingWeight <= 0 {
		config.SiblingWeight = 0.7
	}
	if config.MaxSiblings <= 0 {
		config.MaxSiblings = 10
	}
	if config.SystemPeerWeight <= 0 {
		config.SystemPeerWeight = 0.3
	}
	if config.MaxSystemPeers <= 0 {
		config.MaxSystemPeers = 15
	}

	return &EntityIDProvider{
		base:               base,
		siblingWeight:      config.SiblingWeight,
		maxSiblings:        config.MaxSiblings,
		includeSiblings:    config.IncludeSiblings,
		includeSystemPeers: config.IncludeSystemPeers,
		systemPeerWeight:   config.SystemPeerWeight,
		maxSystemPeers:     config.MaxSystemPeers,
		typePrefixCache:    make(map[string][]string),
		systemCache:        make(map[string][]string),
		logger:             logger,
	}
}

// GetAllEntityIDs delegates to the base provider
func (p *EntityIDProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	return p.base.GetAllEntityIDs(ctx)
}

// GetNeighbors returns both explicit neighbors and sibling neighbors from EntityID hierarchy.
// Sibling neighbors are entities that share the same 5-part type prefix.
//
// Direction parameter is respected for explicit edges but ignored for sibling edges
// (sibling relationships are symmetric).
func (p *EntityIDProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if entityID == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "EntityIDProvider", "GetNeighbors", "entityID is empty")
	}

	// 1. Get explicit neighbors from base provider
	explicit, err := p.base.GetNeighbors(ctx, entityID, direction)
	if err != nil {
		return nil, errs.WrapTransient(err, "EntityIDProvider", "GetNeighbors", "base provider error")
	}

	// Create set of explicit neighbors for deduplication
	explicitSet := make(map[string]bool, len(explicit))
	for _, id := range explicit {
		explicitSet[id] = true
	}

	// 2. Get sibling neighbors from EntityID hierarchy
	var siblingNeighbors []string
	if p.includeSiblings {
		siblingNeighbors, err = p.findSiblingNeighbors(ctx, entityID, explicitSet)
	}
	if err != nil {
		// Log warning but don't fail - explicit edges are sufficient
		if p.logger != nil {
			p.logger.Warn("sibling neighbor lookup failed, continuing with explicit edges",
				"entity_id", entityID,
				"explicit_neighbors", len(explicit),
				"error", err)
		}
		err = nil // reset so system peer error check is accurate
	}

	// 3. Find system peer neighbors
	var systemPeerNeighbors []string
	if p.includeSystemPeers {
		// Build combined exclusion set (explicit + siblings)
		combinedSet := make(map[string]bool, len(explicit)+len(siblingNeighbors))
		for _, id := range explicit {
			combinedSet[id] = true
		}
		for _, id := range siblingNeighbors {
			combinedSet[id] = true
		}

		systemPeerNeighbors, err = p.findSystemPeerNeighbors(ctx, entityID, combinedSet)
		if err != nil && p.logger != nil {
			p.logger.Warn("system peer lookup failed, continuing without",
				"entity_id", entityID, "error", err)
		}
	}

	// 4. Combine and return
	result := make([]string, 0, len(explicit)+len(siblingNeighbors)+len(systemPeerNeighbors))
	result = append(result, explicit...)
	result = append(result, siblingNeighbors...)
	result = append(result, systemPeerNeighbors...)

	return result, nil
}

// getTypePrefix extracts the 5-part type prefix from a 6-part EntityID.
// EntityID format: org.platform.domain.system.type.instance
// TypePrefix: org.platform.domain.system.type
//
// Returns empty string if EntityID doesn't have exactly 6 parts.
func getTypePrefix(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) != 6 {
		return "" // Not a valid 6-part EntityID
	}
	// Join first 5 parts
	return strings.Join(parts[:5], ".")
}

// getSystem extracts the system segment (part[3]) from a 6-part EntityID.
// EntityID format: org.platform.domain.system.type.instance
//
// Using part[3] (system) rather than part[2] (domain) provides more granular
// affinity: "document" vs "sensor" vs "work" rather than broad categories
// like "content" or "environmental" where many unrelated entity types share
// the same domain.
//
// Returns empty string if EntityID doesn't have exactly 6 parts.
func getSystem(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) != 6 {
		return ""
	}
	return parts[3]
}

// findSiblingNeighbors returns entities with the same type prefix that aren't already explicit neighbors.
func (p *EntityIDProvider) findSiblingNeighbors(
	ctx context.Context,
	entityID string,
	explicitSet map[string]bool,
) ([]string, error) {
	// Check context before expensive operation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Get type prefix
	typePrefix := getTypePrefix(entityID)
	if typePrefix == "" {
		return nil, nil // Not a valid 6-part EntityID
	}

	// Ensure cache is initialized
	if err := p.ensureTypePrefixCache(ctx); err != nil {
		p.siblingEdgeErrors.Add(1)
		return nil, err
	}

	// Get siblings from cache
	p.cacheMu.RLock()
	siblings := p.typePrefixCache[typePrefix]
	p.cacheMu.RUnlock()

	// Filter out self and explicit neighbors, apply limit
	var result []string
	for _, siblingID := range siblings {
		if siblingID == entityID {
			continue // Skip self
		}
		if explicitSet[siblingID] {
			continue // Already an explicit neighbor
		}
		result = append(result, siblingID)
		if len(result) >= p.maxSiblings {
			break
		}
	}

	p.siblingEdgeSuccess.Add(1)
	return result, nil
}

// findSystemPeerNeighbors returns entities in the same system that aren't already neighbors.
func (p *EntityIDProvider) findSystemPeerNeighbors(
	ctx context.Context,
	entityID string,
	excludeSet map[string]bool,
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	system := getSystem(entityID)
	if system == "" {
		return nil, nil
	}

	if err := p.ensureTypePrefixCache(ctx); err != nil {
		return nil, err
	}

	p.cacheMu.RLock()
	peers := p.systemCache[system]
	p.cacheMu.RUnlock()

	var result []string
	for _, peerID := range peers {
		if peerID == entityID {
			continue
		}
		if excludeSet[peerID] {
			continue
		}
		result = append(result, peerID)
		if len(result) >= p.maxSystemPeers {
			break
		}
	}

	return result, nil
}

// areSystemPeers returns true if two entities share the same system (part[3]).
func areSystemPeers(entityA, entityB string) bool {
	systemA := getSystem(entityA)
	systemB := getSystem(entityB)
	return systemA != "" && systemA == systemB
}

// ensureTypePrefixCache builds the type prefix -> entity IDs mapping if not already done.
func (p *EntityIDProvider) ensureTypePrefixCache(ctx context.Context) error {
	if p.cacheInitialized.Load() {
		return nil // Already initialized
	}

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	// Double-check after acquiring lock
	if p.cacheInitialized.Load() {
		return nil
	}

	// Get all entity IDs from base provider
	allEntities, err := p.base.GetAllEntityIDs(ctx)
	if err != nil {
		return errs.WrapTransient(err, "EntityIDProvider", "ensureTypePrefixCache", "get all entity IDs")
	}

	// Build prefix -> entities and system -> entities mappings
	prefixMap := make(map[string][]string)
	systemMap := make(map[string][]string)
	for _, entityID := range allEntities {
		prefix := getTypePrefix(entityID)
		if prefix == "" {
			continue // Skip invalid EntityIDs
		}
		prefixMap[prefix] = append(prefixMap[prefix], entityID)

		system := getSystem(entityID)
		if system != "" {
			systemMap[system] = append(systemMap[system], entityID)
		}
	}

	// Canonicalize each candidate list. findSiblingNeighbors / findSystemPeerNeighbors
	// keep the first maxSiblings / maxSystemPeers entries, so if the base-provider
	// order varies (kvProvider's JetStream Keys() is watcher-delivery order, not
	// sorted), WHICH candidates survive the cap would vary and the synthesized edge
	// set — and thus the LPA partition — would flip run-to-run. Sorting here makes
	// the capped edge set deterministic regardless of base order. This changes which
	// siblings/peers are kept vs. the previous unsorted-then-cap behavior, but does
	// NOT change weights or caps.
	for prefix := range prefixMap {
		sort.Strings(prefixMap[prefix])
	}
	for system := range systemMap {
		sort.Strings(systemMap[system])
	}

	p.typePrefixCache = prefixMap
	p.systemCache = systemMap
	p.cacheInitialized.Store(true)

	if p.logger != nil {
		p.logger.Debug("EntityID cache initialized",
			"total_entities", len(allEntities),
			"unique_prefixes", len(prefixMap),
			"unique_systems", len(systemMap))
	}

	return nil
}

// GetEdgeWeight returns the weight of an edge between two entities.
//
// For explicit edges: delegates to base provider
// For sibling edges: returns configured sibling weight (default 0.7)
//
// Explicit edges always take precedence - if base returns weight > 0,
// that's used directly. Sibling edge weight is only used when no explicit edge exists.
func (p *EntityIDProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	if fromID == "" || toID == "" {
		return 0.0, errs.WrapInvalid(errs.ErrMissingConfig, "EntityIDProvider", "GetEdgeWeight", "entity IDs are empty")
	}

	// 1. Try explicit edge first (always takes precedence)
	weight, err := p.base.GetEdgeWeight(ctx, fromID, toID)
	if err != nil {
		return 0.0, errs.WrapTransient(err, "EntityIDProvider", "GetEdgeWeight", "base provider error")
	}
	if weight > 0 {
		return weight, nil // Explicit edge exists
	}

	// 2. Check if entities are siblings (same type prefix)
	if p.includeSiblings && p.areSiblings(fromID, toID) {
		return p.siblingWeight, nil
	}

	// 3. Check if entities are system peers (same system segment)
	if p.includeSystemPeers && areSystemPeers(fromID, toID) {
		return p.systemPeerWeight, nil
	}

	// No edge found
	return 0.0, nil
}

// areSiblings returns true if two entities have the same type prefix.
func (p *EntityIDProvider) areSiblings(entityA, entityB string) bool {
	prefixA := getTypePrefix(entityA)
	prefixB := getTypePrefix(entityB)
	return prefixA != "" && prefixA == prefixB
}

// ClearCache clears the type prefix cache and propagates to wrapped providers.
// Call this when entities are added/removed.
func (p *EntityIDProvider) ClearCache() {
	p.cacheMu.Lock()
	p.typePrefixCache = make(map[string][]string)
	p.systemCache = make(map[string][]string)
	p.cacheInitialized.Store(false)
	p.cacheMu.Unlock()

	// Propagate cache clear to wrapped provider
	if cacheClearer, ok := p.base.(interface{ ClearCache() }); ok {
		cacheClearer.ClearCache()
	}
}

// GetCacheStats returns statistics about the type prefix cache for monitoring.
func (p *EntityIDProvider) GetCacheStats() (prefixes int, entities int) {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

	prefixes = len(p.typePrefixCache)
	for _, entityList := range p.typePrefixCache {
		entities += len(entityList)
	}
	return
}

// GetSiblingEdgeMetrics returns metrics for sibling edge operations.
func (p *EntityIDProvider) GetSiblingEdgeMetrics() (successes, errors int64) {
	return p.siblingEdgeSuccess.Load(), p.siblingEdgeErrors.Load()
}
