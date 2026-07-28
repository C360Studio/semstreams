// Package graphquery community cache implementation
package graphquery

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/nats-io/nats.go/jetstream"
)

// CommunityCache maintains an in-memory cache of communities from COMMUNITY_INDEX KV.
// It watches the KV bucket for changes and updates the cache in real-time.
// This is a consumer-owned cache - graph-query owns and manages its own view of community data.
type CommunityCache struct {
	mu sync.RWMutex

	// communities maps level → community ID → Community.
	//
	// Keyed by (level, ID) because that is the storage identity: COMMUNITY_INDEX
	// keys are "{level}.{community_id}" (graph/clustering/storage.go). Cross-level
	// ID collisions are structural, not incidental — an LPA community ID IS its
	// seed entity ID, and detectHierarchicalLevel re-runs LPA over the same entity
	// set at every level, so levels 0/1/2 draw IDs from one pool. A bare-ID map
	// lets a level-1 write shadow the level-0 record and lets a level-0 delete
	// evict a level-1 one.
	communities map[int]map[string]*clustering.Community

	// entityCommunity maps entityID → level → communityID for fast LocalSearch lookups
	entityCommunity map[string]map[int]string

	// summaries maps the COMMUNITY_SUMMARIES KV key ({level}.{membership_hash}) to
	// its LLM summary record. It is populated by a SECOND watcher on the
	// worker-owned summary bucket (ADR-087). After the ownership split
	// Community.LLMSummary on COMMUNITY_INDEX is always empty, so the read path
	// joins the partition to this map by the community's membership hash. Guarded by
	// the same mu as the partition maps.
	summaries map[string]*clustering.CommunitySummaryRecord

	// Lifecycle
	logger         *slog.Logger
	ready          bool
	watcher        jetstream.KeyWatcher
	summaryWatcher jetstream.KeyWatcher
}

// NewCommunityCache creates a new community cache.
func NewCommunityCache(logger *slog.Logger) *CommunityCache {
	return &CommunityCache{
		communities:     make(map[int]map[string]*clustering.Community),
		entityCommunity: make(map[string]map[int]string),
		summaries:       make(map[string]*clustering.CommunitySummaryRecord),
		logger:          logger,
	}
}

// WatchAndSync starts watching the COMMUNITY_INDEX KV bucket and syncs changes to the cache.
// This method blocks until the context is cancelled.
func (c *CommunityCache) WatchAndSync(ctx context.Context, bucket jetstream.KeyValue) error {
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		return err
	}
	c.watcher = watcher

	c.logger.Info("community cache watcher started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("community cache watcher stopping", "reason", "context cancelled")
			watcher.Stop()
			return ctx.Err()

		case entry := <-watcher.Updates():
			if entry == nil {
				// nil entry indicates initial state enumeration complete
				c.mu.Lock()
				c.ready = true
				total := c.totalCommunitiesLocked()
				c.mu.Unlock()
				c.logger.Info("community cache initial sync complete",
					"communities", total)
				continue
			}

			if entry.Operation() == jetstream.KeyValueDelete {
				c.handleDelete(entry.Key())
				continue
			}

			c.handleUpdate(entry.Key(), entry.Value())
		}
	}
}

// handleUpdate processes a community create/update from KV watch.
func (c *CommunityCache) handleUpdate(key string, data []byte) {
	// The KV key is the authority for level, on both the update and the delete
	// path — handleDelete only ever has the key, so keying updates off the payload
	// instead would make the two paths disagree and leak entries.
	level, communityID, ok := parseCommunityKey(key)
	if !ok {
		// Entity mapping keys (entity.{level}.{entityID}) carry a bare community
		// ID string, not a Community; anything else is not ours to index.
		return
	}

	var community clustering.Community
	if err := json.Unmarshal(data, &community); err != nil {
		c.logger.Warn("failed to unmarshal community",
			"key", key,
			"error", err)
		return
	}

	if community.ID != communityID || community.Level != level {
		// Normalize to the key. The record is reachable only under its key, so the
		// key must win or a later delete for that key would miss this entry.
		c.logger.Warn("community payload disagrees with its KV key — normalizing to the key",
			"key", key,
			"payload_id", community.ID,
			"payload_level", community.Level)
		community.ID = communityID
		community.Level = level
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove old membership mappings if this (level, ID) existed.
	if old, exists := c.communities[level][communityID]; exists {
		c.removeMembershipMappings(level, old)
	}

	if c.communities[level] == nil {
		c.communities[level] = make(map[string]*clustering.Community)
	}
	c.communities[level][communityID] = &community

	// Update entity→community mappings
	for _, entityID := range community.Members {
		if c.entityCommunity[entityID] == nil {
			c.entityCommunity[entityID] = make(map[int]string)
		}
		c.entityCommunity[entityID][level] = communityID
	}

	c.logger.Debug("community cache updated",
		"id", communityID,
		"level", level,
		"members", len(community.Members))
}

// handleDelete processes a community deletion from KV watch.
func (c *CommunityCache) handleDelete(key string) {
	level, communityID, ok := parseCommunityKey(key)
	if !ok {
		return // Not a community key (e.g., entity mapping key)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	byID, exists := c.communities[level]
	if !exists {
		return
	}
	community, exists := byID[communityID]
	if !exists {
		return
	}

	// Scoped to the level named by the DELETED KEY, never the level of whatever
	// record a bare-ID lookup happened to find.
	c.removeMembershipMappings(level, community)

	delete(byID, communityID)
	if len(byID) == 0 {
		delete(c.communities, level)
	}

	c.logger.Debug("community cache deleted", "id", communityID, "level", level)
}

// WatchSummaries starts watching the COMMUNITY_SUMMARIES KV bucket and syncs LLM
// summary records into the cache. It runs as a SECOND watcher alongside
// WatchAndSync and blocks until the context is cancelled.
//
// It deliberately does NOT participate in readiness (IsReady): cache readiness is
// gated on the PARTITION bucket only (ADR-087). A summary miss is a graceful
// statistical fallback, not an unready state, so GraphRAG availability is
// decoupled from the LLM summary pipeline — an empty COMMUNITY_SUMMARIES bucket
// completes its (empty) initial sync immediately without blocking anything.
func (c *CommunityCache) WatchSummaries(ctx context.Context, bucket jetstream.KeyValue) error {
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		return err
	}
	c.summaryWatcher = watcher

	c.logger.Info("community summary cache watcher started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("community summary cache watcher stopping", "reason", "context cancelled")
			watcher.Stop()
			return ctx.Err()

		case entry := <-watcher.Updates():
			if entry == nil {
				// Initial summary-store enumeration complete. Does NOT flip readiness.
				c.logger.Info("community summary cache initial sync complete",
					"summaries", c.summaryCount())
				continue
			}

			if entry.Operation() == jetstream.KeyValueDelete {
				c.handleSummaryDelete(entry.Key())
				continue
			}

			c.handleSummaryUpdate(entry.Key(), entry.Value())
		}
	}
}

// handleSummaryUpdate processes a summary create/update from the summary watch.
// The map is keyed by the raw KV key ({level}.{membership_hash}), which is exactly
// what SummaryFor reconstructs, so no key parsing is needed here.
func (c *CommunityCache) handleSummaryUpdate(key string, data []byte) {
	var rec clustering.CommunitySummaryRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		c.logger.Warn("failed to unmarshal community summary", "key", key, "error", err)
		return
	}

	c.mu.Lock()
	c.summaries[key] = &rec
	c.mu.Unlock()
}

// handleSummaryDelete removes a summary record from the cache.
func (c *CommunityCache) handleSummaryDelete(key string) {
	c.mu.Lock()
	delete(c.summaries, key)
	c.mu.Unlock()
}

// summaryCount returns the number of cached summary records.
func (c *CommunityCache) summaryCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.summaries)
}

// SummaryFor returns the LLM summary joined to a community by its membership hash,
// with ok=true only when an llm-enhanced, non-empty record exists for that exact
// membership. A miss (no record, a failed record, or an empty summary) returns
// ("", false) so the caller degrades to the statistical floor — the join never
// yields an empty summary that masquerades as an answer.
func (c *CommunityCache) SummaryFor(comm *clustering.Community) (string, bool) {
	if comm == nil || len(comm.Members) == 0 {
		return "", false
	}
	key := clustering.SummaryKey(comm.Level, clustering.MembershipHash(comm.Members))

	c.mu.RLock()
	defer c.mu.RUnlock()
	rec, ok := c.summaries[key]
	if !ok || rec.Status != clustering.SummaryStatusEnhanced || rec.LLMSummary == "" {
		return "", false
	}
	return rec.LLMSummary, true
}

// parseCommunityKey splits a COMMUNITY_INDEX key into its level and community ID.
//
// Key format: {level}.{communityID} — see clustering.communityKey. Community IDs
// are entity IDs and contain dots, so only the first segment is the level.
// Returns ok=false for entity mapping keys (entity.{level}.{entityID}) and for
// anything whose first segment is not a level number.
func parseCommunityKey(key string) (level int, communityID string, ok bool) {
	dotIdx := strings.Index(key, ".")
	if dotIdx <= 0 || dotIdx == len(key)-1 {
		return 0, "", false
	}
	level, err := strconv.Atoi(key[:dotIdx])
	if err != nil {
		return 0, "", false
	}
	return level, key[dotIdx+1:], true
}

// removeMembershipMappings removes entity→community mappings for a community at a level.
// The level is passed explicitly rather than read off the community so callers cannot
// silently strip the wrong level's mappings.
// Must be called with mu held.
func (c *CommunityCache) removeMembershipMappings(level int, community *clustering.Community) {
	for _, entityID := range community.Members {
		if levels, exists := c.entityCommunity[entityID]; exists {
			delete(levels, level)
			if len(levels) == 0 {
				delete(c.entityCommunity, entityID)
			}
		}
	}
}

// totalCommunitiesLocked counts communities across all levels.
// Must be called with mu held.
func (c *CommunityCache) totalCommunitiesLocked() int {
	total := 0
	for _, byID := range c.communities {
		total += len(byID)
	}
	return total
}

// GetCommunity retrieves a community by level and ID.
//
// The level is required: community IDs are only unique WITHIN a level, so an
// ID-only lookup would return an arbitrary level's record. Callers hold the level
// on CommunitySummary.Level, which is populated from the same record.
// Returns nil if not found.
func (c *CommunityCache) GetCommunity(level int, id string) *clustering.Community {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.communities[level][id]
}

// GetEntityCommunity retrieves the community containing an entity at a specific level.
// Returns nil if the entity is not in any community at that level.
func (c *CommunityCache) GetEntityCommunity(entityID string, level int) *clustering.Community {
	c.mu.RLock()
	defer c.mu.RUnlock()

	levels, exists := c.entityCommunity[entityID]
	if !exists {
		return nil
	}

	communityID, exists := levels[level]
	if !exists {
		return nil
	}

	// Resolve within the same level — an entity mapping is level-scoped.
	return c.communities[level][communityID]
}

// GetCommunitiesByLevel retrieves all communities at a specific level, ordered by
// community ID.
//
// Built from the authoritative map rather than a maintained byLevel index: the
// derived index is what went stale when a delete rebuilt it from an already-shadowed
// map. The order is sorted because downstream ranking (scoreCommunitySummaries) uses
// an unstable sort with no tie-break, so an unordered input reorders equal-scoring
// communities between identical queries.
// Returns empty slice if no communities exist at that level.
func (c *CommunityCache) GetCommunitiesByLevel(level int) []*clustering.Community {
	c.mu.RLock()
	defer c.mu.RUnlock()

	byID := c.communities[level]
	result := make([]*clustering.Community, 0, len(byID))
	for _, comm := range byID {
		result = append(result, comm)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// GetAllCommunities retrieves all communities across every level, ordered by
// (level, community ID). A community ID that exists at more than one level yields
// one entry per level.
// Returns empty slice if no communities exist.
func (c *CommunityCache) GetAllCommunities() []*clustering.Community {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*clustering.Community, 0, c.totalCommunitiesLocked())
	for _, byID := range c.communities {
		for _, comm := range byID {
			result = append(result, comm)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Level != result[j].Level {
			return result[i].Level < result[j].Level
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// IsReady returns true if the initial sync from KV is complete.
func (c *CommunityCache) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Stats returns cache statistics.
func (c *CommunityCache) Stats() CommunityStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	levelCounts := make(map[int]int, len(c.communities))
	for level, byID := range c.communities {
		levelCounts[level] = len(byID)
	}

	return CommunityStats{
		TotalCommunities: c.totalCommunitiesLocked(),
		TotalEntities:    len(c.entityCommunity),
		ByLevel:          levelCounts,
		Ready:            c.ready,
	}
}

// CommunityStats provides cache statistics.
type CommunityStats struct {
	TotalCommunities int         `json:"total_communities"`
	TotalEntities    int         `json:"total_entities"`
	ByLevel          map[int]int `json:"by_level"`
	Ready            bool        `json:"ready"`
}

// Stop stops both watchers if running.
func (c *CommunityCache) Stop() {
	if c.watcher != nil {
		c.watcher.Stop()
	}
	if c.summaryWatcher != nil {
		c.summaryWatcher.Stop()
	}
}
