// Package graphingest query handlers
package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// defaultMaxConcurrent is the default bounded concurrency for entity fetches
const defaultMaxConcurrent = 10

// setupQueryHandlers sets up NATS request/reply subscriptions for query handlers
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	// Subscribe to entity query
	sub, err := c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.entity", c.handleQueryEntityNATS)
	if err != nil {
		return fmt.Errorf("subscribe entity query: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	// Subscribe to batch query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.batch", c.handleQueryBatchNATS)
	if err != nil {
		return fmt.Errorf("subscribe batch query: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	// Subscribe to prefix query (for hierarchy listing)
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.prefix", c.handleQueryPrefixNATS)
	if err != nil {
		return fmt.Errorf("subscribe prefix query: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	// Subscribe to suffix query (for partial entity ID resolution)
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.suffix", c.handleQuerySuffixNATS)
	if err != nil {
		return fmt.Errorf("subscribe suffix query: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	c.logger.Info("query handlers registered",
		"subjects", []string{"graph.ingest.query.entity", "graph.ingest.query.batch", "graph.ingest.query.prefix", "graph.ingest.query.suffix"})

	return nil
}

// handleQueryEntityNATS handles single entity query requests via NATS request/reply
func (c *Component) handleQueryEntityNATS(ctx context.Context, data []byte) ([]byte, error) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryEntityNATS", "unmarshal request")
	}

	// Validate request
	if req.ID == "" {
		return nil, errs.WrapInvalid(errors.New("empty id"), "Component", "handleQueryEntityNATS", "validate request")
	}

	// Get entity from KV bucket
	entry, err := c.entityBucket.Get(ctx, req.ID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// HTTP semantics (400 vs 404) live at the gateway —
			// "not found" classifies as Invalid at the wire boundary;
			// callers can substring-match the body for 404 routing.
			return nil, errs.WrapInvalid(errors.New(req.ID), "Component", "handleQueryEntityNATS", "entity not found")
		}
		return nil, errs.WrapTransient(err, "Component", "handleQueryEntityNATS", "kv get")
	}

	return entry.Value, nil
}

// handleQueryBatchNATS handles batch entity query requests via NATS request/reply
func (c *Component) handleQueryBatchNATS(ctx context.Context, data []byte) ([]byte, error) {
	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryBatchNATS", "unmarshal request")
	}

	// Handle empty IDs (return empty entities)
	if len(req.IDs) == 0 {
		return []byte(`{"entities":[]}`), nil
	}

	// Fetch entities with bounded concurrency and cache
	entities := c.fetchEntitiesConcurrent(ctx, req.IDs, defaultMaxConcurrent)

	// Return entities wrapped in a struct for consistency with loadEntities expectations
	return json.Marshal(map[string]any{
		"entities": entities,
	})
}

// handleQueryPrefixNATS handles prefix-based entity listing for hierarchy queries
func (c *Component) handleQueryPrefixNATS(ctx context.Context, data []byte) ([]byte, error) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		Prefix string `json:"prefix"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryPrefixNATS", "unmarshal request")
	}

	// Build prefix for server-side filtering
	prefixDot := req.Prefix
	if req.Prefix != "" {
		prefixDot = req.Prefix + "."
	}

	// Use server-side prefix filtering instead of loading all keys
	keys, err := c.entityBucket.KeysByPrefix(ctx, prefixDot)
	if err != nil {
		return nil, errs.WrapTransient(err, "Component", "handleQueryPrefixNATS", "kv keys by prefix")
	}

	// Also check exact match for full entity ID queries (6-part IDs
	// where KeysByPrefix("org.plat.dom.sys.type.inst.") finds nothing)
	if req.Prefix != "" && len(keys) == 0 {
		if _, getErr := c.entityBucket.Get(ctx, req.Prefix); getErr == nil {
			keys = []string{req.Prefix}
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 1000 // Default limit
	}

	// Apply limit
	matched := keys
	if len(matched) > limit {
		matched = matched[:limit]
	}

	// Fetch full entities with bounded concurrency and cache
	entities := c.fetchEntitiesConcurrent(ctx, matched, defaultMaxConcurrent)

	// Return entities wrapped in envelope for consistency with batch query
	return json.Marshal(map[string]any{
		"entities": entities,
	})
}

// handleQuerySuffixNATS handles suffix-based entity ID resolution.
// Uses a three-tier lookup: TTL cache → KV suffix index → fallback full scan.
// This enables NL queries to use partial entity IDs like "temp-sensor-001" which
// get resolved to full 6-part IDs like "c360.logistics.environmental.sensor.temperature.temp-sensor-001".
func (c *Component) handleQuerySuffixNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		Suffix string `json:"suffix"` // e.g., "temp-sensor-001"
	}
	if err := json.Unmarshal(data, &req); err != nil {
		c.logger.Error("suffix query unmarshal failed", "error", err)
		return nil, errs.WrapInvalid(err, "Component", "handleQuerySuffixNATS", "unmarshal request")
	}

	c.logger.Debug("suffix query received", "suffix", req.Suffix)

	if req.Suffix == "" {
		return nil, errs.WrapInvalid(errors.New("empty suffix"), "Component", "handleQuerySuffixNATS", "validate request")
	}

	// Tier 1: Check TTL cache (O(1) memory lookup)
	if c.suffixCache != nil {
		if fullID, ok := c.suffixCache.Get(req.Suffix); ok {
			c.logger.Debug("suffix query cache hit", "suffix", req.Suffix, "matched", fullID)
			return json.Marshal(map[string]string{"id": fullID})
		}
	}

	// Tier 2: Check KV suffix index (O(1) KV get)
	if c.suffixBucket != nil {
		if matchedID := c.lookupSuffixIndex(ctx, req.Suffix); matchedID != "" {
			// Populate cache on hit
			if c.suffixCache != nil {
				c.suffixCache.Set(req.Suffix, matchedID) //nolint:errcheck
			}
			return json.Marshal(map[string]string{"id": matchedID})
		}
	}

	// Tier 3: Fallback full scan (migration period — index may be incomplete)
	matchedID := c.suffixFallbackScan(ctx, req.Suffix)

	// If found via scan, populate index + cache for next time
	if matchedID != "" {
		c.updateSuffixIndex(ctx, matchedID)
		if c.suffixCache != nil {
			c.suffixCache.Set(req.Suffix, matchedID) //nolint:errcheck
		}
	}

	return json.Marshal(map[string]string{"id": matchedID})
}

// lookupSuffixIndex checks the KV suffix index for a matching entity ID.
func (c *Component) lookupSuffixIndex(ctx context.Context, suffix string) string {
	entry, err := c.suffixBucket.Get(ctx, suffix)
	if err != nil {
		return ""
	}

	var indexEntry struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(entry.Value, &indexEntry); err != nil {
		return ""
	}

	c.logger.Debug("suffix query index hit", "suffix", suffix, "matched", indexEntry.ID)
	return indexEntry.ID
}

// suffixFallbackScan performs a full key scan for suffix matching.
// This is the fallback path during migration when the suffix index may be incomplete.
func (c *Component) suffixFallbackScan(ctx context.Context, suffix string) string {
	keys, err := c.entityBucket.Keys(ctx)
	if err != nil || keys == nil {
		return ""
	}

	c.logger.Debug("suffix query fallback scan", "suffix", suffix, "key_count", len(keys))

	suffixWithDot := "." + suffix
	for _, key := range keys {
		if strings.HasSuffix(key, suffixWithDot) || key == suffix {
			c.logger.Debug("suffix query matched via scan", "suffix", suffix, "matched", key)
			return key
		}
	}

	return ""
}

// queryMsg + the handleQueryEntity / handleQueryBatch methods +
// respondError + respondJSON below form a parallel msg-style handler
// family that is NEVER registered for production traffic. Only the
// *NATS-suffixed handlers above are wired via SubscribeForRequests;
// the msg-style handlers exist solely for the in-process unit tests
// in this package and emit a `{"error": "..."}` JSON envelope
// (a third divergent error shape, distinct from both the legacy
// `error: ` body prefix and the gh#93 X-Status header convention).
//
// This duplication is a refactoring debt — the *NATS handlers
// should be exercised directly by tests. Out of scope for gh#93's
// wire-format fix (no production wire impact). Tracked as a separate
// followup; see the gh#93 PR description's "Deferred" section.
//
// queryMsg is an interface for query request messages.
// This accommodates both real NATS messages and test mocks.
type queryMsg interface {
	Data() []byte
	Respond(data []byte) error
}

// handleQueryEntity handles single entity query requests
func (c *Component) handleQueryEntity(msg queryMsg) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Validate request
	if req.ID == "" {
		c.respondError(msg, "invalid request")
		return
	}

	// Get entity from KV bucket
	entry, err := c.entityBucket.Get(ctx, req.ID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			c.respondError(msg, "not found")
			return
		}
		c.respondError(msg, "internal error")
		return
	}

	// Respond with entity JSON
	if err := msg.Respond(entry.Value); err != nil {
		c.logger.Error("failed to respond to query", "error", err)
	}
}

// handleQueryBatch handles batch entity query requests
func (c *Component) handleQueryBatch(msg queryMsg) {
	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First unmarshal into map to detect explicit null values
	var rawReq map[string]any
	if err := json.Unmarshal(msg.Data(), &rawReq); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Check if ids field exists and is explicitly null
	idsValue, hasIDs := rawReq["ids"]
	if hasIDs && idsValue == nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Now parse into typed struct
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Handle empty or missing IDs (return empty array)
	if len(req.IDs) == 0 {
		c.respondJSON(msg, []graph.EntityState{})
		return
	}

	// Fetch entities with bounded concurrency and cache
	entities := c.fetchEntitiesConcurrent(ctx, req.IDs, defaultMaxConcurrent)

	// Respond with entities array
	c.respondJSON(msg, entities)
}

// fetchEntitiesConcurrent fetches entities by IDs using bounded concurrency with cache.
// Cache hits skip KV entirely; cache misses are fetched with bounded concurrency.
// Returns entities in non-deterministic order (callers process as sets).
func (c *Component) fetchEntitiesConcurrent(ctx context.Context, ids []string, maxConcurrent int) []graph.EntityState {
	if len(ids) == 0 {
		return nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrent
	}

	// Phase 1: Check cache for all IDs, collect misses
	var cached []graph.EntityState
	var missIDs []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if c.entityCache != nil {
			if entity, ok := c.entityCache.Get(id); ok {
				cached = append(cached, entity)
				continue
			}
		}
		missIDs = append(missIDs, id)
	}

	// Phase 2: Fetch cache misses with bounded concurrency
	if len(missIDs) == 0 {
		return cached
	}

	type fetchResult struct {
		entity graph.EntityState
		ok     bool
	}

	results := make([]fetchResult, len(missIDs))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, id := range missIDs {
		if err := ctx.Err(); err != nil {
			break
		}

		wg.Add(1)
		go func(idx int, entityID string) {
			defer wg.Done()

			// Acquire semaphore (with context cancellation)
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			// Check context after acquiring semaphore
			if ctx.Err() != nil {
				return
			}

			entry, err := c.entityBucket.Get(ctx, entityID)
			if err != nil {
				return // Skip not found / errors (partial success)
			}

			var entity graph.EntityState
			if err := json.Unmarshal(entry.Value, &entity); err != nil {
				return // Skip unmarshal errors
			}

			// Populate cache
			if c.entityCache != nil {
				c.entityCache.Set(entityID, entity) //nolint:errcheck
			}

			results[idx] = fetchResult{entity: entity, ok: true}
		}(i, id)
	}

	wg.Wait()

	// Phase 3: Merge cached + fetched results
	entities := make([]graph.EntityState, 0, len(cached)+len(missIDs))
	entities = append(entities, cached...)
	for _, r := range results {
		if r.ok {
			entities = append(entities, r.entity)
		}
	}

	return entities
}

// respondError sends an error response
func (c *Component) respondError(msg queryMsg, errorMsg string) {
	response := map[string]string{"error": errorMsg}
	data, _ := json.Marshal(response)
	if err := msg.Respond(data); err != nil {
		c.logger.Error("failed to respond with error", "error", err)
	}
}

// respondJSON sends a JSON response
func (c *Component) respondJSON(msg queryMsg, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		c.respondError(msg, "internal error")
		return
	}
	if err := msg.Respond(data); err != nil {
		c.logger.Error("failed to respond with JSON", "error", err)
	}
}
