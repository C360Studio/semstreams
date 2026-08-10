// Package graphquery community cache implementation
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/nats-io/nats.go/jetstream"
)

// communityCache maintains an in-memory cache of communities from COMMUNITY_INDEX KV.
// It watches the KV bucket for changes and updates the cache in real-time.
// This is a consumer-owned cache - graph-query owns and manages its own view of community data.
type communityCache struct {
	mu     sync.RWMutex
	active *communityGeneration

	// summaries maps the COMMUNITY_SUMMARIES KV key ({level}.{membership_hash}) to
	// its LLM summary record. It is populated by a SECOND watcher on the
	// worker-owned summary bucket (ADR-087). After the ownership split
	// Community.LLMSummary on COMMUNITY_INDEX is always empty, so the read path
	// joins the partition to this map by the community's membership hash. Guarded by
	// the same mu as the partition maps.
	summaries map[string]*clustering.CommunitySummaryRecord

	// Lifecycle
	logger         *slog.Logger
	summaryWatcher jetstream.KeyWatcher
	onPublished    func(uint64)
	onApplied      func(uint64, string)
	onUnpublished  func(uint64)
}

// communityGeneration is one fully independent COMMUNITY_INDEX projection.
// Its maps are never copied into a later generation.
type communityGeneration struct {
	id uint64
	mu sync.RWMutex

	// Keyed by (level, ID), matching COMMUNITY_INDEX storage identity.
	communities     map[int]map[string]*clustering.Community
	entityCommunity map[string]map[int]string
}

// communityLease is private package state carried by one community-backed
// request. It identifies both the generation number and exact map owner.
type communityLease struct {
	cache      *communityCache
	generation *communityGeneration
}

type communityWatchReader interface {
	WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

func newCommunityGeneration(id uint64) *communityGeneration {
	return &communityGeneration{
		id:              id,
		communities:     make(map[int]map[string]*clustering.Community),
		entityCommunity: make(map[string]map[int]string),
	}
}

func newCommunityCache(logger *slog.Logger) *communityCache {
	return &communityCache{
		summaries: make(map[string]*clustering.CommunitySummaryRecord),
		logger:    logger,
	}
}

// watchGeneration stages a fresh projection, publishes it only at the initial
// enumeration sentinel, and unpublishes exactly it on unexpected watch loss.
func (c *communityCache) watchGeneration(ctx context.Context, reader communityWatchReader, generation *communityGeneration) error {
	watcher, err := reader.WatchAll(ctx)
	if err != nil {
		return err
	}
	defer watcher.Stop()

	c.logger.Info("community cache generation watcher started", "generation", generation.id)
	published := false
	defer func() {
		if published {
			c.unpublish(generation)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("community cache generation watcher stopping",
				"generation", generation.id, "reason", "context cancelled")
			return ctx.Err()

		case entry, ok := <-watcher.Updates():
			if !ok {
				return errors.New("community index watch closed")
			}
			if entry == nil {
				if !published {
					c.publish(generation)
					published = true
					c.logger.Info("community cache generation published",
						"generation", generation.id,
						"communities", generation.totalCommunities())
				}
				continue
			}

			if entry.Operation() == jetstream.KeyValueDelete {
				if !published || c.isCurrent(generation) {
					generation.applyDelete(entry.Key())
					c.notifyApplied(generation.id, entry.Key())
				}
				continue
			}

			if !published || c.isCurrent(generation) {
				generation.applyUpdate(entry.Key(), entry.Value(), c.logger)
				c.notifyApplied(generation.id, entry.Key())
			}
		}
	}
}

func (c *communityCache) notifyApplied(generation uint64, key string) {
	c.mu.RLock()
	onApplied := c.onApplied
	c.mu.RUnlock()
	if onApplied != nil {
		onApplied(generation, key)
	}
}

func (c *communityCache) publish(generation *communityGeneration) {
	c.mu.Lock()
	c.active = generation
	onPublished := c.onPublished
	c.mu.Unlock()
	if onPublished != nil {
		onPublished(generation.id)
	}
}

func (c *communityCache) unpublish(generation *communityGeneration) bool {
	c.mu.Lock()
	if c.active != generation {
		c.mu.Unlock()
		return false
	}
	c.active = nil
	onUnpublished := c.onUnpublished
	c.mu.Unlock()
	if onUnpublished != nil {
		onUnpublished(generation.id)
	}
	return true
}

func (c *communityCache) isCurrent(generation *communityGeneration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.active == generation
}

// acquire returns one lease over the currently published generation.
func (c *communityCache) acquire() *communityLease {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.active == nil {
		return nil
	}
	return &communityLease{cache: c, generation: c.active}
}

// valid proves the exact generation remains published.
func (l *communityLease) valid() bool {
	return l != nil && l.cache != nil && l.cache.isCurrent(l.generation)
}

// completeSuccess linearizes exact-generation validation with successful
// completion accounting. While record runs, unpublish/replacement is excluded by
// the cache read lock; when this returns true, the response completed against the
// exact generation at that single observable point.
func (l *communityLease) completeSuccess(record func()) bool {
	if l == nil || l.cache == nil || l.generation == nil {
		return false
	}
	l.cache.mu.RLock()
	defer l.cache.mu.RUnlock()
	if l.cache.active != l.generation {
		return false
	}
	record()
	return true
}

func (l *communityLease) generationID() uint64 {
	if l == nil || l.generation == nil {
		return 0
	}
	return l.generation.id
}

func (g *communityGeneration) applyUpdate(key string, data []byte, loggers ...*slog.Logger) {
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
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
		logger.Warn("failed to unmarshal community",
			"key", key,
			"error", err)
		return
	}

	if community.ID != communityID || community.Level != level {
		// Normalize to the key. The record is reachable only under its key, so the
		// key must win or a later delete for that key would miss this entry.
		logger.Warn("community payload disagrees with its KV key — normalizing to the key",
			"key", key,
			"payload_id", community.ID,
			"payload_level", community.Level)
		community.ID = communityID
		community.Level = level
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Remove old membership mappings if this (level, ID) existed.
	if old, exists := g.communities[level][communityID]; exists {
		g.removeMembershipMappings(level, old)
	}

	if g.communities[level] == nil {
		g.communities[level] = make(map[string]*clustering.Community)
	}
	g.communities[level][communityID] = &community

	// Update entity→community mappings
	for _, entityID := range community.Members {
		if g.entityCommunity[entityID] == nil {
			g.entityCommunity[entityID] = make(map[int]string)
		}
		g.entityCommunity[entityID][level] = communityID
	}

	logger.Debug("community cache updated",
		"id", communityID,
		"level", level,
		"members", len(community.Members))
}

func (g *communityGeneration) applyDelete(key string) {
	level, communityID, ok := parseCommunityKey(key)
	if !ok {
		return // Not a community key (e.g., entity mapping key)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	byID, exists := g.communities[level]
	if !exists {
		return
	}
	community, exists := byID[communityID]
	if !exists {
		return
	}

	// Scoped to the level named by the DELETED KEY, never the level of whatever
	// record a bare-ID lookup happened to find.
	g.removeMembershipMappings(level, community)

	delete(byID, communityID)
	if len(byID) == 0 {
		delete(g.communities, level)
	}
}

// watchSummaries starts watching the COMMUNITY_SUMMARIES KV bucket and syncs LLM
// summary records into the cache. It runs as a second watcher alongside the
// private community generation watch and blocks until the context is cancelled.
//
// It deliberately does not publish or revoke community generations: generation
// availability is gated on the partition bucket only (ADR-087). A summary miss is a graceful
// statistical fallback, not an unready state, so GraphRAG availability is
// decoupled from the LLM summary pipeline — an empty COMMUNITY_SUMMARIES bucket
// completes its (empty) initial sync immediately without blocking anything.
func (c *communityCache) watchSummaries(ctx context.Context, bucket jetstream.KeyValue) error {
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
func (c *communityCache) handleSummaryUpdate(key string, data []byte) {
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
func (c *communityCache) handleSummaryDelete(key string) {
	c.mu.Lock()
	delete(c.summaries, key)
	c.mu.Unlock()
}

// summaryCount returns the number of cached summary records.
func (c *communityCache) summaryCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.summaries)
}

// SummaryFor returns the LLM summary joined to a community by its membership hash,
// with ok=true only when an llm-enhanced, non-empty record exists for that exact
// membership. A miss (no record, a failed record, or an empty summary) returns
// ("", false) so the caller degrades to the statistical floor — the join never
// yields an empty summary that masquerades as an answer.
func (c *communityCache) summaryFor(comm *clustering.Community) (string, bool) {
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
func (g *communityGeneration) removeMembershipMappings(level int, community *clustering.Community) {
	for _, entityID := range community.Members {
		if levels, exists := g.entityCommunity[entityID]; exists {
			delete(levels, level)
			if len(levels) == 0 {
				delete(g.entityCommunity, entityID)
			}
		}
	}
}

// totalCommunitiesLocked counts communities across all levels.
// Must be called with mu held.
func (g *communityGeneration) totalCommunitiesLocked() int {
	total := 0
	for _, byID := range g.communities {
		total += len(byID)
	}
	return total
}

func (g *communityGeneration) totalCommunities() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.totalCommunitiesLocked()
}

func (l *communityLease) getCommunity(level int, id string) *clustering.Community {
	if l == nil || l.generation == nil {
		return nil
	}
	l.generation.mu.RLock()
	defer l.generation.mu.RUnlock()
	return l.generation.communities[level][id]
}

func (l *communityLease) getEntityCommunity(entityID string, level int) *clustering.Community {
	if l == nil || l.generation == nil {
		return nil
	}
	l.generation.mu.RLock()
	defer l.generation.mu.RUnlock()

	levels, exists := l.generation.entityCommunity[entityID]
	if !exists {
		return nil
	}

	communityID, exists := levels[level]
	if !exists {
		return nil
	}

	// Resolve within the same level — an entity mapping is level-scoped.
	return l.generation.communities[level][communityID]
}

func (l *communityLease) getCommunitiesByLevel(level int) []*clustering.Community {
	if l == nil || l.generation == nil {
		return nil
	}
	l.generation.mu.RLock()
	defer l.generation.mu.RUnlock()

	byID := l.generation.communities[level]
	result := make([]*clustering.Community, 0, len(byID))
	for _, comm := range byID {
		result = append(result, comm)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (l *communityLease) getAllCommunities() []*clustering.Community {
	if l == nil || l.generation == nil {
		return nil
	}
	l.generation.mu.RLock()
	defer l.generation.mu.RUnlock()

	result := make([]*clustering.Community, 0, l.generation.totalCommunitiesLocked())
	for _, byID := range l.generation.communities {
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

func (l *communityLease) stats() communityStats {
	if l == nil || l.generation == nil {
		return communityStats{ByLevel: map[int]int{}}
	}
	l.generation.mu.RLock()
	defer l.generation.mu.RUnlock()

	levelCounts := make(map[int]int, len(l.generation.communities))
	for level, byID := range l.generation.communities {
		levelCounts[level] = len(byID)
	}

	return communityStats{
		TotalCommunities: l.generation.totalCommunitiesLocked(),
		TotalEntities:    len(l.generation.entityCommunity),
		ByLevel:          levelCounts,
		Ready:            l.valid(),
	}
}

type communityStats struct {
	TotalCommunities int         `json:"total_communities"`
	TotalEntities    int         `json:"total_entities"`
	ByLevel          map[int]int `json:"by_level"`
	Ready            bool        `json:"ready"`
}

func (c *communityCache) stop() {
	if c.summaryWatcher != nil {
		c.summaryWatcher.Stop()
	}
}
