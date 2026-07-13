// Package graphindex query handlers
package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// setupQueryHandlers sets up NATS request/reply subscriptions for query handlers
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	// Subscribe to outgoing query
	sub, err := c.natsClient.SubscribeForRequests(ctx, "graph.index.query.outgoing", c.handleQueryOutgoingNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe outgoing query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to incoming query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.incoming", c.handleQueryIncomingNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe incoming query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to alias query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.alias", c.handleQueryAliasNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe alias query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to predicate query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.predicate", c.handleQueryPredicateNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe predicate query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to predicate list query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.predicateList", c.handleQueryPredicateListNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe predicateList query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to predicate stats query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.predicateStats", c.handleQueryPredicateStatsNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe predicateStats query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to compound predicate query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.predicateCompound", c.handleQueryPredicateCompoundNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe predicateCompound query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to name query (gh#376 — deterministic name→ranked-IDs)
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.byName", c.handleQueryByNameNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe byName query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to index-readiness status query (gh#397 — deterministic-fusion
	// honesty envelope; Ready = NAME_INDEX populated).
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.index.query.status", c.handleQueryStatusNATS)
	if err != nil {
		return errs.Wrap(err, "Component", "setupQueryHandlers", "subscribe status query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	c.logger.Info("query handlers registered",
		slog.Any("subjects", []string{
			"graph.index.query.outgoing",
			"graph.index.query.incoming",
			"graph.index.query.alias",
			"graph.index.query.predicate",
			"graph.index.query.predicateList",
			"graph.index.query.predicateStats",
			"graph.index.query.predicateCompound",
			"graph.index.query.byName",
			"graph.index.query.status",
		}))

	return nil
}

// handleQueryOutgoingNATS handles outgoing relationship query requests via NATS request/reply
func (c *Component) handleQueryOutgoingNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}

	if req.EntityID == "" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty entity_id"))
	}

	entry, err := c.outgoingBucket.Get(ctx, req.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.OutgoingRelationshipsData{
				Relationships: []graph.OutgoingEntry{},
			}))
		}
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	var entries []graph.OutgoingEntry
	if err := json.Unmarshal(entry.Value, &entries); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	return json.Marshal(graph.NewQueryResponse(graph.OutgoingRelationshipsData{
		Relationships: entries,
	}))
}

// ensureQueryReady gates the composite-key reverse-index query handlers (incoming,
// byName) on the index having caught up to ENTITY_STATES at least once after Start
// (gh#474 Codex P1d). Sticky-fast once caught-up; otherwise it does one revision-lag
// probe. Returns a transient ErrorCodeIndexNotReady while still building so a caller
// retries rather than acting on the partial keyset a format cutover / cold replay is
// still materialising (old aggregate keys are inert, new keys incomplete).
func (c *Component) ensureQueryReady(ctx context.Context) error {
	if c.indexBootstrapped.Load() {
		return nil
	}
	// No watermark wired means the watcher never started (unit tests, pre-Start) —
	// there is no cutover replay to gate on, so treat as ready. In production Start
	// always wires the watermark before subscribing these handlers, so a live request
	// takes the honest revision-lag path below.
	if c.watermark == nil {
		return nil
	}
	if c.computeIndexStatus(ctx).Ready {
		c.indexBootstrapped.Store(true)
		return nil
	}
	return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
		errors.New("index not ready: still catching up to ENTITY_STATES"))
}

// handleQueryIncomingNATS handles incoming relationship query requests via NATS request/reply.
//
// After composite-key sharding (gh#474): scans the prefix entityID.">" to enumerate
// all incoming edges, then reconstructs graph.IncomingEntry from each composite key.
// The wire response type (graph.IncomingRelationshipsData, graph.IncomingEntry) is
// unchanged — only the storage format changed.
func (c *Component) handleQueryIncomingNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}

	if req.EntityID == "" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty entity_id"))
	}

	// Cutover-readiness gate (P1d): don't serve a partial keyset while the index is
	// still catching up after a format cutover / cold replay.
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
	}

	keys, err := c.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(req.EntityID))
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	entries := make([]graph.IncomingEntry, 0, len(keys))
	for _, key := range keys {
		entry, ok := incomingEntryFromKey(key, req.EntityID)
		if !ok {
			c.logger.Debug("incoming query: skipping malformed key",
				slog.String("key", key),
				slog.String("entity_id", req.EntityID))
			continue
		}
		entries = append(entries, entry)
	}

	// Deterministic order (gh#474 Codex P1c): KeysByPrefix returns storage order,
	// which a no-op replay can reshuffle with worker scheduling. PathRAG stops at
	// max_nodes/max_paths, so an unsorted result makes the capped set depend on
	// write timing rather than graph state. Sort by (FromEntityID, Predicate).
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].FromEntityID != entries[j].FromEntityID {
			return entries[i].FromEntityID < entries[j].FromEntityID
		}
		return entries[i].Predicate < entries[j].Predicate
	})

	return json.Marshal(graph.NewQueryResponse(graph.IncomingRelationshipsData{
		Relationships: entries,
	}))
}

// handleQueryAliasNATS handles alias resolution query requests via NATS request/reply
func (c *Component) handleQueryAliasNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}

	if req.Alias == "" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty alias"))
	}

	entry, err := c.aliasBucket.Get(ctx, req.Alias)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.AliasData{
				CanonicalID: nil,
			}))
		}
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	canonicalID := string(entry.Value)
	return json.Marshal(graph.NewQueryResponse(graph.AliasData{
		CanonicalID: &canonicalID,
	}))
}

// handleQueryPredicateNATS handles predicate entity query requests via NATS request/reply
func (c *Component) handleQueryPredicateNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		Predicate string  `json:"predicate"`
		Value     *string `json:"value,omitempty"`
		Limit     int     `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}

	if req.Predicate == "" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty predicate"))
	}

	entities, err := c.queryPredicateEntities(ctx, req.Predicate, req.Value, req.Limit)
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	return json.Marshal(graph.NewQueryResponse(graph.PredicateData{
		Entities: entities,
	}))
}

// queryPredicateEntities is the shared helper used by the NATS predicate
// handlers. It looks up the predicate index, optionally filters by value, and applies
// the limit in a single place so both call sites stay consistent.
func (c *Component) queryPredicateEntities(ctx context.Context, predicate string, value *string, limit int) ([]string, error) {
	keys, err := c.predicateBucket.KeysByPrefix(ctx, predicateIndexPrefix(predicate))
	if err != nil {
		return nil, err
	}

	entities := make([]string, 0, len(keys))
	for _, key := range keys {
		entities = append(entities, entityIDFromPredicateKey(key, predicate))
	}

	if value != nil && c.entityStatesBucket != nil {
		// filterEntitiesByPredicateValue handles limit internally so we avoid
		// iterating the full list twice.
		entities = c.filterEntitiesByPredicateValue(ctx, entities, predicate, *value, limit)
	} else if limit > 0 && len(entities) > limit {
		entities = entities[:limit]
	}

	return entities, nil
}

// filterEntitiesByPredicateValue filters entity IDs by checking if their entity state
// contains a triple with the given predicate whose Object matches the specified value.
// limit is applied early — iteration stops once enough matches are collected.
// ctx cancellation is also checked on each iteration to allow cooperative cancellation.
func (c *Component) filterEntitiesByPredicateValue(ctx context.Context, entityIDs []string, predicate string, value string, limit int) []string {
	var matched []string

	for _, entityID := range entityIDs {
		// Respect context cancellation between iterations.
		if ctx.Err() != nil {
			break
		}

		entry, err := c.entityStatesBucket.Get(ctx, entityID)
		if err != nil {
			c.logger.Debug("value filter: skip entity on fetch",
				slog.String("entity_id", entityID),
				slog.Any("error", err))
			continue
		}

		var state graph.EntityState
		if err := json.Unmarshal(entry.Value(), &state); err != nil {
			c.logger.Debug("value filter: skip entity on unmarshal",
				slog.String("entity_id", entityID),
				slog.Any("error", err))
			continue
		}

		for _, triple := range state.Triples {
			if triple.Predicate == predicate && normalizeToString(triple.Object) == value {
				matched = append(matched, entityID)
				break
			}
		}

		// Stop as soon as the caller's limit is satisfied.
		if limit > 0 && len(matched) >= limit {
			break
		}
	}

	return matched
}

// normalizeToString converts a triple Object value to a string for comparison.
// Numeric values stored as float64 (the default JSON number type) are formatted
// without a trailing decimal point when the value is integral, matching how callers
// typically express integer quantities (e.g. "85" rather than "85.0").
func normalizeToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// handleQueryPredicateListNATS handles predicate list query requests via NATS request/reply.
// Returns predicates with their entity counts — every predicate, or, when
// the request carries a Prefix, only those sharing that dotted namespace
// (ADR-065: a deliberate, safe namespace query against PREDICATE_CATALOG's
// unhashed keys — unlike a prefix query against PREDICATE_INDEX's hashed
// membership keys, this can't corrupt which entities carry which
// predicate, since the catalog carries no membership data).
func (c *Component) handleQueryPredicateListNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var req graph.PredicateListQuery
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
		}
	}

	var predicates []graph.PredicateSummary
	var err error
	if req.Prefix != "" {
		predicates, err = c.listPredicatesByNamespace(ctx, req.Prefix)
	} else {
		predicates, err = c.listAllPredicates(ctx)
	}
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	return json.Marshal(graph.NewQueryResponse(graph.PredicateListData{
		Predicates: predicates,
		Total:      len(predicates),
	}))
}

// listAllPredicates enumerates every predicate with its entity count via
// ONE grouped scan of the membership bucket (not a per-predicate
// KeysByPrefix fan-out, which would cost one bound ephemeral-consumer
// round trip per catalog entry — see ADR-065). Membership keys are
// hash(predicate).entityID; grouping on the first token before the first
// "." buckets every key by its predicate hash without needing to know the
// predicate strings up front. Catalog names are then forward-hashed to
// join into that count map — catalog entries whose hash has no members
// simply report a zero count.
func (c *Component) listAllPredicates(ctx context.Context) ([]graph.PredicateSummary, error) {
	names, err := c.predicateCatalogBucket.Keys(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return []graph.PredicateSummary{}, nil
	}

	memberKeys, err := c.predicateBucket.Keys(ctx)
	if err != nil {
		return nil, err
	}
	// A pre-cutover blob-format key (bare predicate string, e.g.
	// "code.artifact.type") almost always contains a "." too, so it does
	// NOT reliably fail the Cut below — most real predicates are
	// multi-token. What actually keeps it inert is the join step: its
	// first token (e.g. "code") is never a genuine 64-hex-char hash, so
	// countsByHash[that token] is written but never read by the
	// predicateHashHex(name) lookup below — it's a dead map entry, not a
	// skipped one.
	countsByHash := make(map[string]int, len(names))
	for _, key := range memberKeys {
		hash, _, ok := strings.Cut(key, ".")
		if !ok {
			continue // single-token key with no "." at all — can't be any predicate's composite key
		}
		countsByHash[hash]++
	}

	predicates := make([]graph.PredicateSummary, 0, len(names))
	for _, name := range names {
		predicates = append(predicates, graph.PredicateSummary{
			Predicate:   name,
			EntityCount: countsByHash[predicateHashHex(name)],
		})
	}
	return predicates, nil
}

// listPredicatesByNamespace answers a namespace-scoped predicateList
// request: PREDICATE_CATALOG.KeysByPrefix(prefix) bounds the candidate
// predicate set server-side before any membership lookup, so a
// per-predicate KeysByPrefix fan-out against the membership bucket here
// is fine — the namespace filter, not this loop, is what keeps it cheap.
func (c *Component) listPredicatesByNamespace(ctx context.Context, prefix string) ([]graph.PredicateSummary, error) {
	// KeysByPrefix appends NATS wildcard ">" directly onto prefix; ">" is
	// only meaningful as its own token after a ".", so a prefix missing
	// its trailing dot silently matches nothing instead of erroring —
	// exactly the class of prefix-matching footgun this ADR exists to
	// eliminate elsewhere. Normalize here rather than push the "must end
	// in a dot" contract onto every future caller of predicateList.
	if !strings.HasSuffix(prefix, ".") {
		prefix += "."
	}

	names, err := c.predicateCatalogBucket.KeysByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	predicates := make([]graph.PredicateSummary, 0, len(names))
	for _, name := range names {
		keys, err := c.predicateBucket.KeysByPrefix(ctx, predicateIndexPrefix(name))
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, graph.PredicateSummary{
			Predicate:   name,
			EntityCount: len(keys),
		})
	}
	return predicates, nil
}

// handleQueryPredicateStatsNATS handles predicate stats query requests via NATS request/reply.
// Returns detailed statistics for a single predicate including sample entities.
func (c *Component) handleQueryPredicateStatsNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		Predicate   string `json:"predicate"`
		SampleLimit int    `json:"sample_limit"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}

	if req.Predicate == "" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty predicate"))
	}

	// Default sample limit
	if req.SampleLimit <= 0 {
		req.SampleLimit = 10
	}

	keys, err := c.predicateBucket.KeysByPrefix(ctx, predicateIndexPrefix(req.Predicate))
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	entities := make([]string, 0, len(keys))
	for _, key := range keys {
		entities = append(entities, entityIDFromPredicateKey(key, req.Predicate))
	}
	sort.Strings(entities) // deterministic sample order

	sampleEntities := entities
	if len(sampleEntities) > req.SampleLimit {
		sampleEntities = sampleEntities[:req.SampleLimit]
	}

	return json.Marshal(graph.NewQueryResponse(graph.PredicateStatsData{
		Predicate:      req.Predicate,
		EntityCount:    len(entities),
		SampleEntities: sampleEntities,
	}))
}

// handleQueryPredicateCompoundNATS handles compound predicate query requests via NATS request/reply.
// Performs set intersection (AND) or union (OR) across multiple predicates.
func (c *Component) handleQueryPredicateCompoundNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req graph.CompoundPredicateQuery
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}

	if len(req.Predicates) == 0 {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty predicates"))
	}

	operator := req.Operator
	if operator != "AND" && operator != "OR" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: operator must be AND or OR"))
	}

	// Collect entity sets for each predicate
	entitySets := make([]map[string]struct{}, 0, len(req.Predicates))
	for _, predicate := range req.Predicates {
		keys, err := c.predicateBucket.KeysByPrefix(ctx, predicateIndexPrefix(predicate))
		if err != nil {
			return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
		}

		entitySet := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			entitySet[entityIDFromPredicateKey(key, predicate)] = struct{}{}
		}
		entitySets = append(entitySets, entitySet)
	}

	var result map[string]struct{}
	if operator == "AND" {
		result = intersectSets(entitySets)
	} else {
		result = unionSets(entitySets)
	}

	// Convert to slice
	entities := make([]string, 0, len(result))
	for e := range result {
		entities = append(entities, e)
	}

	// Apply limit if specified
	if req.Limit > 0 && len(entities) > req.Limit {
		entities = entities[:req.Limit]
	}

	return json.Marshal(graph.NewQueryResponse(graph.CompoundPredicateData{
		Entities: entities,
		Operator: operator,
		Matched:  len(result),
	}))
}

// intersectSets returns the intersection of all entity sets.
func intersectSets(sets []map[string]struct{}) map[string]struct{} {
	if len(sets) == 0 {
		return make(map[string]struct{})
	}

	// Start with the first set
	result := make(map[string]struct{})
	for e := range sets[0] {
		result[e] = struct{}{}
	}

	// Intersect with remaining sets
	for i := 1; i < len(sets); i++ {
		for e := range result {
			if _, exists := sets[i][e]; !exists {
				delete(result, e)
			}
		}
	}

	return result
}

// unionSets returns the union of all entity sets.
func unionSets(sets []map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	for _, set := range sets {
		for e := range set {
			result[e] = struct{}{}
		}
	}
	return result
}
