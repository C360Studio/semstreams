// Package graphingest query handlers
package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
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
	if err := c.ensureEntityQueriesReady(); err != nil {
		return nil, err
	}
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		// ADR-060: invalid request → invalid class + Code invalid_request, so a
		// consumer reads ce.Code instead of sniffing the message.
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid request: %w", err))
	}

	// Validate before any authority I/O. Exact reads never reinterpret malformed
	// identity as absence.
	if err := semtypes.ValidateEntityID(req.ID); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid request: entity ID: %w", err))
	}

	// Get entity from KV bucket
	entry, err := c.entityBucket.Get(ctx, req.ID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// HTTP semantics (400 vs 404) live at the gateway. ADR-060: not-found
			// classifies as Invalid at the wire boundary and carries the stable
			// Code entity_not_found, so a gateway routes 404 off ce.Code rather
			// than substring-sniffing the message.
			return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound,
				fmt.Errorf("not found: %s", req.ID))
		}
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal,
			fmt.Errorf("internal error: %w", err))
	}

	if err := c.validateEntityQueryValue(ctx, req.ID, entry.Value, entry.Revision); err != nil {
		return nil, err
	}
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value, &entity); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
			fmt.Errorf("decode validated entity %s at revision %d: %w", req.ID, entry.Revision, err))
	}
	if entity.ID != req.ID {
		stateErr := &graph.StateContractError{
			Reason:   graph.GraphStateReasonNoncanonicalEntityID,
			EntityID: req.ID,
			Err:      fmt.Errorf("authority key contains entity %q", entity.ID),
		}
		c.inventoryEntityPoison(ctx, stateErr, entry.Revision)
		return nil, graph.ClassifyStateContractError(stateErr)
	}
	return json.Marshal(graph.ExactEntity{
		Entity:     entity.Clone(),
		KVRevision: entry.Revision,
	})
}

// handleQueryBatchNATS handles batch entity query requests via NATS request/reply
func (c *Component) handleQueryBatchNATS(ctx context.Context, data []byte) ([]byte, error) {
	if err := c.ensureEntityQueriesReady(); err != nil {
		return nil, err
	}
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

	// Fetch entities with bounded concurrency and cache.
	//
	// Timed because this handler's budget and its CALLER's budget are set
	// independently and were inverted: graph-query's loadEntities abandons the
	// request at its QueryTimeout (default 5s) while this handler is allowed
	// 10s, so a batch taking 5–10s is charged to the caller as an opaque
	// "context deadline exceeded" that no server-side log records at all. That
	// is how gh#830 presented — an intermittent e2e failure with nothing on the
	// responder side to look at. Same reasoning as reportBatchMissing below:
	// the symptom reaches the user several hops from the fact.
	fetchStart := time.Now()
	entities, missing, err := c.fetchEntitiesConcurrent(ctx, req.IDs, defaultMaxConcurrent)
	fetchElapsed := time.Since(fetchStart)
	if err != nil {
		c.logger.Warn("entity batch fetch failed",
			slog.Int("requested", len(req.IDs)),
			slog.Duration("elapsed", fetchElapsed),
			slog.String("error", err.Error()))
		return nil, c.classifyEntityQueryError(err)
	}
	c.logger.Debug("entity batch fetch",
		slog.Int("requested", len(req.IDs)),
		slog.Int("returned", len(entities)),
		slog.Int("missing", len(missing)),
		slog.Duration("elapsed", fetchElapsed))
	c.reportBatchMissing(req.IDs, missing)

	resp := graph.EntityBatchResponse{Entities: entities}
	for _, id := range missing {
		resp.Missing = append(resp.Missing, graph.MissingEntity{ID: id, Reason: batchMissingReason(id)})
	}
	return json.Marshal(resp)
}

// batchMissingReason classifies one unhydrated requested ID. An empty ID was never
// looked up, so `not_found` would assert something unobserved — the exact move this
// change exists to stop. It is malformed input, which is a per-ID fault: `error`.
//
// One function so the wire and the counter cannot drift; they did, briefly.
func batchMissingReason(id string) graph.MissingReason {
	if id == "" {
		return graph.MissingError
	}
	return graph.MissingNotFound
}

// reportBatchMissing makes partial hydration observable. It is the gh#597 soak
// instrumentation: the open question there is whether a dropped ID's KV read was
// genuinely not-found at the moment of failure, and until now nothing recorded that a
// batch came back short at all — the symptom reached the user as a missing search
// result several hops away.
//
// The log carries the IDs (bounded) because "which one" is the whole question; the
// counter carries only counts, since entity IDs are an unbounded label space.
func (c *Component) reportBatchMissing(requested, missing []string) {
	if len(missing) == 0 {
		return
	}
	if c.batchMissing != nil {
		// Counted by the SAME reason the wire reports, not a blanket not_found: the
		// empty-ID case added alongside this counter is reported as `error`, and a
		// metric that disagrees with the response is worse than no metric.
		byReason := make(map[graph.MissingReason]int, 2)
		for _, id := range missing {
			byReason[batchMissingReason(id)]++
		}
		for reason, n := range byReason {
			c.batchMissing.WithLabelValues(string(reason)).Add(float64(n))
		}
	}
	logged := missing
	if len(logged) > maxLoggedMissingIDs {
		logged = logged[:maxLoggedMissingIDs]
	}
	// Debug, not Warn: with reads now serving under lag (ADR-084), a semantic index
	// ranking an entity whose ENTITY_STATES write has not landed yet is ordinary
	// operation, and warning on it would train operators to ignore the line. The
	// COUNTER above is the alertable signal — it has a rate, which is what
	// distinguishes normal churn from the gh#597 pathology.
	c.logger.Debug("batch entity query did not hydrate every requested ID",
		"requested", len(requested),
		"missing", len(missing),
		"reason", string(graph.MissingNotFound),
		"missing_ids", logged)
}

// maxLoggedMissingIDs bounds the ID list on the defer log. A batch is already capped
// well below this in practice; the bound exists so a pathological caller cannot turn
// one log line into a payload.
const maxLoggedMissingIDs = 20

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
	if err := c.ensureEntityQueriesReady(); err != nil {
		return nil, err
	}
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Parse request — PrefixQueryRequest is a superset of the old anonymous
	// struct so old {"prefix","limit"} payloads unmarshal cleanly.
	var req graph.PrefixQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("invalid request: %w", err))
	}
	if req.Prefix != "" {
		if err := semtypes.ValidateEntityIDPrefix(req.Prefix); err != nil {
			return nil, err
		}
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
		} else if !natsclient.IsKVNotFoundError(getErr) {
			return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal,
				fmt.Errorf("failed to check exact entity key: %w", getErr))
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

	// Fetch full entities with bounded concurrency and cache. Missing IDs are
	// deliberately NOT reported here: pageKeys came from a live key scan moments ago,
	// so an absence is a concurrent delete rather than an unanswerable question about a
	// caller-supplied ID. The prefix contract is "what is under this prefix now", and a
	// key deleted mid-page is honestly not under it.
	entities, _, err := c.fetchEntitiesConcurrent(ctx, pageKeys, defaultMaxConcurrent)
	if err != nil {
		return nil, c.classifyEntityQueryError(err)
	}

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
// Suffix resolution deliberately serves IDs WITHOUT decoding entity bytes
// (design D5: resolution is not serving state) — a subsequent read of a
// poisoned entity's state still fails with the typed classification.
func (c *Component) handleQuerySuffixNATS(ctx context.Context, data []byte) ([]byte, error) {
	if err := c.ensureEntityQueriesReady(); err != nil {
		return nil, err
	}
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
		matchedID, err := c.lookupSuffixIndex(ctx, req.Suffix)
		if err != nil {
			return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, err)
		}
		if matchedID != "" {
			// Populate cache on hit
			if c.suffixCache != nil {
				c.suffixCache.Set(req.Suffix, matchedID) //nolint:errcheck
			}
			return json.Marshal(map[string]string{"id": matchedID})
		}
	}

	// Tier 3: Fallback full scan (migration period — index may be incomplete)
	matchedID, err := c.suffixFallbackScan(ctx, req.Suffix)
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, err)
	}

	// If found via scan, populate index + cache for next time
	if matchedID != "" {
		c.updateSuffixIndex(ctx, matchedID)
		if c.suffixCache != nil {
			c.suffixCache.Set(req.Suffix, matchedID) //nolint:errcheck
		}
	}

	return json.Marshal(map[string]string{"id": matchedID})
}

// ensureEntityQueriesReady is the plain atomic readiness check at query
// handler entry. The flags settle inside Start before any subscription
// registers, so no lock discipline is needed (design D2 — the old
// entityQueryMu commit-point ceremony existed only to serialize the retired
// surface-global poison latch and was deleted with it). Poison refusal is
// per-entity and lives at each read's validating decode, not here.
func (c *Component) ensureEntityQueriesReady() error {
	if c.entityWatchLost.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("graph-ingest query not ready: ENTITY_STATES snapshot sweep unavailable"))
	}
	if c.entityBootstrapStarted.Load() && !c.entityBootstrapComplete.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("graph-ingest query not ready: ENTITY_STATES bootstrap validating"))
	}
	return nil
}

// validateEntityQueryValue enforces the per-read canonical decode on a single
// entity read. Refusal derives solely from the bytes actually stored — the
// poison inventory is observability-only and is never consulted. A poisoned
// decode stamps the entity ID and records it in the inventory; a successful
// decode clears any stale inventory entry for the key (D3c), so an
// out-of-band repair recovers Health on its next read without a restart.
func (c *Component) validateEntityQueryValue(ctx context.Context, entityID string, data []byte, revision uint64) error {
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(data, &state); err != nil {
		var contractErr *graph.StateContractError
		if errors.As(err, &contractErr) {
			contractErr.EntityID = entityID
			c.inventoryEntityPoison(ctx, contractErr, revision)
		}
		return c.classifyEntityQueryError(err)
	}
	c.clearEntityPoisonOnValidRead(entityID, revision)
	return nil
}

func (c *Component) classifyEntityQueryError(err error) error {
	if graph.IsStateContractError(err) {
		// Already fatal/graph_state_reset_required passes through unchanged
		// (aggregate errors arrive pre-classified naming every poisoned entity).
		return graph.ClassifyStateContractError(err)
	}
	return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, err)
}

// lookupSuffixIndex checks the KV suffix index for a matching entity ID.
func (c *Component) lookupSuffixIndex(ctx context.Context, suffix string) (string, error) {
	entry, err := c.suffixBucket.Get(ctx, suffix)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return "", nil
		}
		return "", fmt.Errorf("read suffix index: %w", err)
	}

	var indexEntry struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(entry.Value, &indexEntry); err != nil {
		return "", fmt.Errorf("decode suffix index: %w", err)
	}

	c.logger.Debug("suffix query index hit", "suffix", suffix, "matched", indexEntry.ID)
	return indexEntry.ID, nil
}

// suffixFallbackScan performs a full key scan for suffix matching.
// This is the fallback path during migration when the suffix index may be incomplete.
func (c *Component) suffixFallbackScan(ctx context.Context, suffix string) (string, error) {
	keys, err := c.entityBucket.Keys(ctx)
	if err != nil {
		return "", fmt.Errorf("scan entity keys for suffix: %w", err)
	}
	if keys == nil {
		return "", nil
	}

	c.logger.Debug("suffix query fallback scan", "suffix", suffix, "key_count", len(keys))

	suffixWithDot := "." + suffix
	for _, key := range keys {
		if strings.HasSuffix(key, suffixWithDot) || key == suffix {
			c.logger.Debug("suffix query matched via scan", "suffix", suffix, "matched", key)
			return key, nil
		}
	}

	return "", nil
}

// fetchEntitiesConcurrent fetches entities by IDs using bounded concurrency with cache.
// Cache hits skip KV entirely; cache misses are fetched with bounded concurrency.
//
// It returns the entities found AND the IDs whose read came back not-found, because the
// caller cannot recover the second set from the first: the entity slice is ordered
// cache-hits-then-misses, so it carries no correspondence to the requested order, and a
// caller diffing the sets would have to re-derive what this function already knew. That
// gap is the gh#597 drop path — a requested ID silently absent from a shorter list.
//
// Order remains non-deterministic by contract; callers that need request order restore
// it themselves (fusionnats.Entities does, because fusion ranks by position).
func (c *Component) fetchEntitiesConcurrent(ctx context.Context, ids []string, maxConcurrent int) ([]graph.EntityState, []string, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrent
	}

	// Phase 1: Check cache for all IDs, collect misses
	var cached []graph.EntityState
	var missIDs []string
	var invalid []string
	for _, id := range ids {
		if id == "" {
			// Reported, not skipped. The response contract is that every requested ID
			// appears exactly once across entities+missing; silently dropping the empty
			// string put it in neither and made the accounting a lie for the one input
			// a caller is most likely to send by accident.
			invalid = append(invalid, id)
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
		return cached, invalid, nil
	}

	type fetchResult struct {
		entity   graph.EntityState
		err      error
		ok       bool
		notFound bool
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

			// Capture the per-key invalidation generation BEFORE the KV Get so
			// the repopulating Set below can detect an invalidate that raced our
			// read window (read-after-write coherence guard — see
			// invalidateEntityCacheEntry). This load happens-before the Get in
			// program order.
			gen0 := c.loadEntityCacheGen(entityID)

			entry, err := c.entityBucket.Get(ctx, entityID)
			if err != nil {
				if natsclient.IsKVNotFoundError(err) {
					// Not an error — this ID simply is not in the bucket. It is
					// REPORTED rather than dropped so the caller can tell a short
					// list from a partial one (gh#597).
					results[idx].notFound = true
				} else {
					results[idx].err = err
				}
				return
			}

			var entity graph.EntityState
			if err := graph.UnmarshalEntityState(entry.Value, &entity); err != nil {
				// Stamp the entity ID here — the goroutine is where the
				// identity is in scope (design D4) — and record the poison in
				// the inventory with the failing revision so a single batch
				// attempt inventories EVERY poisoned entity it touched (D5).
				var contractErr *graph.StateContractError
				if errors.As(err, &contractErr) {
					contractErr.EntityID = entityID
					c.inventoryEntityPoison(ctx, contractErr, entry.Revision)
				}
				results[idx].err = err
				return
			}

			// A validating read succeeded — clear any stale inventory entry
			// for this key (D3c). Steady-state cost: one atomic load.
			c.clearEntityPoisonOnValidRead(entityID, entry.Revision)

			// Test-only seam: deterministically inject a concurrent invalidation
			// into the read window (between the KV Get and the repopulating Set).
			// nil in production.
			if c.repopulateHook != nil {
				c.repopulateHook(entityID)
			}

			// Repopulate the read-through cache, but drop the Set if this key was
			// invalidated during the read window (gen0 no longer current) — a slow
			// reader must not resurrect pre-write state past a concurrent
			// invalidate. See invalidateEntityCacheEntry for the contract.
			c.repopulateEntityCacheEntry(entityID, entity, gen0)

			results[idx] = fetchResult{entity: entity, ok: true}
		}(i, id)
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// Phase 3: Merge cached + fetched results. Poison fails the WHOLE read with one
	// typed error naming every poisoned entity encountered in this attempt (design D5),
	// and never a B-only response that hides A's poison.
	//
	// A not-found ID is different in kind: it does not fail the call, it is collected
	// and REPORTED. Partiality was never the bug — invisibility was.
	var poisoned []*graph.StateContractError
	var firstOtherErr error
	missing := invalid
	entities := make([]graph.EntityState, 0, len(cached)+len(missIDs))
	entities = append(entities, cached...)
	for i, r := range results {
		if r.err != nil {
			var contractErr *graph.StateContractError
			if errors.As(r.err, &contractErr) {
				poisoned = append(poisoned, contractErr)
			} else if firstOtherErr == nil {
				firstOtherErr = r.err
			}
			continue
		}
		if r.notFound {
			missing = append(missing, missIDs[i])
			continue
		}
		if r.ok {
			entities = append(entities, r.entity)
		}
	}
	if len(poisoned) > 0 {
		return nil, nil, aggregateEntityPoisonError(poisoned)
	}
	if firstOtherErr != nil {
		return nil, nil, firstOtherErr
	}

	return entities, missing, nil
}

// aggregateEntityPoisonError builds the single typed failure for a
// multi-entity read that encountered poisoned entities: fatal
// graph_state_reset_required naming every poisoned entity (bounded list),
// wrapping one typed cause so errors.As(*graph.StateContractError) is
// preserved across the seam. Kills the one-repair-per-round-trip discovery
// loop (design D5).
func aggregateEntityPoisonError(poisoned []*graph.StateContractError) error {
	ids := make([]string, 0, len(poisoned))
	for _, p := range poisoned {
		ids = append(ids, p.EntityID)
	}
	sort.Strings(ids)
	sample := ids
	suffix := ""
	if len(sample) > entityPoisonSampleCap {
		sample = sample[:entityPoisonSampleCap]
		suffix = fmt.Sprintf(" (+%d more)", len(ids)-entityPoisonSampleCap)
	}
	return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
		fmt.Errorf("read encountered %d poisoned entities: %s%s: %w",
			len(ids), strings.Join(sample, ", "), suffix, poisoned[0]))
}
