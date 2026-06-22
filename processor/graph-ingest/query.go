// Package graphingest query handlers
package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
		// Preserve historic wire body shape "error: invalid request: <inner>"
		// for downstream consumers (semconnect's classifyEntityQueryError
		// HasPrefix-matches "invalid request:", todos.go matches
		// "error: not found", etc.) until Phase 2 retires the sniffers.
		// errs.Classified carries the X-Error-Class header without the
		// Wrap formula's attribution prefix.
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("invalid request: %w", err))
	}

	// Validate request
	if req.ID == "" {
		return nil, errs.Classified(errs.ErrorInvalid, errors.New("invalid request: empty id"))
	}

	// Get entity from KV bucket
	entry, err := c.entityBucket.Get(ctx, req.ID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// HTTP semantics (400 vs 404) live at the gateway —
			// "not found" classifies as Invalid at the wire boundary;
			// the "not found: <id>" body prefix is the contract
			// downstream consumers HasPrefix-match for 404 routing
			// (semconnect:cs-api/systems.go:596,
			// agentic-loop/todos.go:31).
			return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("not found: %s", req.ID))
		}
		return nil, errs.Classified(errs.ErrorTransient, fmt.Errorf("internal error: %w", err))
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
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("invalid request: %w", err))
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

// maxPrefixResponseBytes is the soft ceiling for the marshalled response body.
// Conservative count cap (MaxPrefixQueryLimit = 1000) is the primary guard;
// this byte budget is a secondary safety net for pathologically large entities.
// NATS has a hard max_payload of ~1 MB; we stop well below it so that the
// JSON envelope overhead and any per-entity growth cannot push us over.
//
// NOTE: The current implementation uses the conservative count cap approach
// (MaxPrefixQueryLimit) as the primary page-size guard. Incremental byte
// budgeting within a page is deferred — see gh follow-up for byte-budget
// refinement when entity sizes grow beyond a few KB each.
const maxPrefixResponseBytes = 800 * 1024

// handleQueryPrefixNATS handles prefix-based entity listing for hierarchy queries.
//
// Wire contract (additive, non-breaking):
//   - Accepts both old {"prefix":"…","limit":N} and new PrefixQueryRequest
//     (which adds "cursor") — both unmarshal cleanly into PrefixQueryRequest.
//   - Returns graph.PrefixQueryResponse {"entities":[…],"next_cursor":"…"}.
//     Old consumers reading only "entities" are unaffected; "next_cursor" is
//     omitempty so it doesn't appear on the final page or single-page results.
//   - Keys are sorted lexicographically before cursor application. This is a
//     behaviour change from pre-pagination (where order was KV-scan order,
//     i.e. non-deterministic) but is required for cursor correctness and is
//     safe because callers have always treated the result as a set.
func (c *Component) handleQueryPrefixNATS(ctx context.Context, data []byte) ([]byte, error) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Parse request — PrefixQueryRequest is a superset of the old anonymous
	// struct so old {"prefix","limit"} payloads unmarshal cleanly.
	var req graph.PrefixQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("invalid request: %w", err))
	}

	// Clamp limit.
	limit := req.Limit
	if limit <= 0 {
		limit = graph.DefaultPrefixQueryLimit
	}
	if limit > graph.MaxPrefixQueryLimit {
		limit = graph.MaxPrefixQueryLimit
	}

	// Build prefix for server-side filtering.
	prefixDot := req.Prefix
	if req.Prefix != "" {
		prefixDot = req.Prefix + "."
	}

	// Use server-side prefix filtering instead of loading all keys.
	keys, err := c.entityBucket.KeysByPrefix(ctx, prefixDot)
	if err != nil {
		return nil, errs.Classified(errs.ErrorTransient, fmt.Errorf("failed to get keys: %w", err))
	}

	// Also check exact match for full entity ID queries (6-part IDs
	// where KeysByPrefix("org.plat.dom.sys.type.inst.") finds nothing).
	if req.Prefix != "" && len(keys) == 0 {
		if _, getErr := c.entityBucket.Get(ctx, req.Prefix); getErr == nil {
			keys = []string{req.Prefix}
		}
	}

	// MANDATORY: sort before cursor application — cursor is meaningless
	// without a deterministic key order.
	sort.Strings(keys)

	// Apply cursor: advance past keys up to and including lastKey.
	if req.Cursor != "" {
		lastKey, decodeErr := graph.DecodeCursor(req.Cursor)
		if decodeErr != nil {
			return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("invalid cursor: %w", decodeErr))
		}
		// SearchStrings finds first index where keys[i] >= lastKey.
		idx := sort.SearchStrings(keys, lastKey)
		// Advance past any key equal to lastKey (inclusive skip).
		for idx < len(keys) && keys[idx] == lastKey {
			idx++
		}
		keys = keys[idx:]
	}

	// Slice the page — we'll potentially trim further by byte budget below.
	pageKeys := keys
	if len(pageKeys) > limit {
		pageKeys = pageKeys[:limit]
	}

	// Fetch full entities with bounded concurrency and cache.
	entities := c.fetchEntitiesConcurrent(ctx, pageKeys, defaultMaxConcurrent)

	// fetchEntitiesConcurrent returns cache-hits-then-misses order, NOT sorted.
	// Re-sort by ID so the page is a deterministic sorted prefix of pageKeys and
	// the byte-trim cursor below (derived from the last RETURNED entity) is the
	// true lexicographic max. Without this, a byte-trimmed page would set a
	// cursor on an arbitrary key and skip entities on the next page.
	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })

	// Byte-budget guard: if the marshalled response would exceed
	// maxPrefixResponseBytes, trim the entity slice until it fits and set
	// a cursor so the caller can fetch the remainder.
	//
	// This is a secondary safety net; the primary guard is the count cap
	// above. applyPrefixByteLimit marshals each entity once to sum sizes
	// (O(N) in the page's entities) — acceptable since the page is already
	// count-capped at MaxPrefixQueryLimit.
	byteBudget := maxPrefixResponseBytes
	if c.maxPrefixResponseBytesOverride > 0 {
		byteBudget = c.maxPrefixResponseBytesOverride // test-only hook; 0 in production
	}
	trimmedEntities, byteLimited := applyPrefixByteLimit(entities, byteBudget)

	// Determine next cursor. A cursor is needed when:
	//   (a) there are remaining keys beyond this page (len(keys) > len(pageKeys)), OR
	//   (b) the byte budget trimmed the entity set within the page.
	var nextCursor string
	if byteLimited && len(trimmedEntities) > 0 {
		// Cursor points to the last entity we actually returned.
		nextCursor = graph.EncodeCursor(trimmedEntities[len(trimmedEntities)-1].ID)
	} else if len(keys) > len(pageKeys) {
		// More pages exist beyond this one.
		if len(pageKeys) > 0 {
			nextCursor = graph.EncodeCursor(pageKeys[len(pageKeys)-1])
		}
	}

	resp := graph.PrefixQueryResponse{
		Entities:   trimmedEntities,
		NextCursor: nextCursor,
	}

	return json.Marshal(resp)
}

// applyPrefixByteLimit trims an entity slice to fit within byteLimit bytes
// when marshalled. Returns the (possibly trimmed) slice and a boolean
// indicating whether trimming occurred.
//
// The byte estimate is computed incrementally using json.Marshal on each
// entity. This is correct but O(N) in entities. At MaxPrefixQueryLimit=1000
// it runs in well under 1 ms for typical entity sizes.
func applyPrefixByteLimit(entities []graph.EntityState, byteLimit int) ([]graph.EntityState, bool) {
	if len(entities) == 0 {
		return entities, false
	}

	// Overhead: {"entities":[ ... ],"next_cursor":"..."} — we reserve a
	// conservative 256 bytes for the envelope so we don't have to track it
	// precisely.
	const envelopeOverhead = 256
	budget := byteLimit - envelopeOverhead
	accumulated := 0

	for i, e := range entities {
		b, err := json.Marshal(e)
		if err != nil {
			// Marshal failure on an individual entity: skip the rest to be safe.
			return entities[:i], i > 0
		}
		accumulated += len(b) + 1 // +1 for comma separator
		if accumulated > budget {
			if i == 0 {
				// Even the first entity exceeds budget — return it anyway
				// rather than an empty page. Report byteLimited=true so the
				// caller sets a cursor on THIS entity's key: the next page
				// resumes strictly after it (no infinite loop, and no skip of
				// the rest of the page that a count-boundary cursor would cause).
				return entities[:1], true
			}
			return entities[:i], true
		}
	}

	return entities, false
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
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("invalid request: %w", err))
	}

	c.logger.Debug("suffix query received", "suffix", req.Suffix)

	if req.Suffix == "" {
		return nil, errs.Classified(errs.ErrorInvalid, errors.New("invalid request: empty suffix"))
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
