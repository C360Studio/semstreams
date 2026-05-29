// Package graphindex query handlers
package graphindex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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

	c.logger.Info("query handlers registered",
		slog.Any("subjects", []string{
			"graph.index.query.outgoing",
			"graph.index.query.incoming",
			"graph.index.query.alias",
			"graph.index.query.predicate",
			"graph.index.query.predicateList",
			"graph.index.query.predicateStats",
			"graph.index.query.predicateCompound",
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
		return json.Marshal(graph.NewQueryError[graph.OutgoingRelationshipsData]("invalid request"))
	}

	if req.EntityID == "" {
		return json.Marshal(graph.NewQueryError[graph.OutgoingRelationshipsData]("invalid request: empty entity_id"))
	}

	entry, err := c.outgoingBucket.Get(ctx, req.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.OutgoingRelationshipsData{
				Relationships: []graph.OutgoingEntry{},
			}))
		}
		return json.Marshal(graph.NewQueryError[graph.OutgoingRelationshipsData]("internal error"))
	}

	var entries []graph.OutgoingEntry
	if err := json.Unmarshal(entry.Value, &entries); err != nil {
		return json.Marshal(graph.NewQueryError[graph.OutgoingRelationshipsData]("internal error"))
	}

	return json.Marshal(graph.NewQueryResponse(graph.OutgoingRelationshipsData{
		Relationships: entries,
	}))
}

// handleQueryIncomingNATS handles incoming relationship query requests via NATS request/reply
func (c *Component) handleQueryIncomingNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(graph.NewQueryError[graph.IncomingRelationshipsData]("invalid request"))
	}

	if req.EntityID == "" {
		return json.Marshal(graph.NewQueryError[graph.IncomingRelationshipsData]("invalid request: empty entity_id"))
	}

	entry, err := c.incomingBucket.Get(ctx, req.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.IncomingRelationshipsData{
				Relationships: []graph.IncomingEntry{},
			}))
		}
		return json.Marshal(graph.NewQueryError[graph.IncomingRelationshipsData]("internal error"))
	}

	var entries []graph.IncomingEntry
	if err := json.Unmarshal(entry.Value, &entries); err != nil {
		return json.Marshal(graph.NewQueryError[graph.IncomingRelationshipsData]("internal error"))
	}

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
		return json.Marshal(graph.NewQueryError[graph.AliasData]("invalid request"))
	}

	if req.Alias == "" {
		return json.Marshal(graph.NewQueryError[graph.AliasData]("invalid request: empty alias"))
	}

	entry, err := c.aliasBucket.Get(ctx, req.Alias)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.AliasData{
				CanonicalID: nil,
			}))
		}
		return json.Marshal(graph.NewQueryError[graph.AliasData]("internal error"))
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
		return json.Marshal(graph.NewQueryError[graph.PredicateData]("invalid request"))
	}

	if req.Predicate == "" {
		return json.Marshal(graph.NewQueryError[graph.PredicateData]("invalid request: empty predicate"))
	}

	entities, err := c.queryPredicateEntities(ctx, req.Predicate, req.Value, req.Limit)
	if err != nil {
		return json.Marshal(graph.NewQueryError[graph.PredicateData]("internal error"))
	}

	return json.Marshal(graph.NewQueryResponse(graph.PredicateData{
		Entities: entities,
	}))
}

// queryMsg + the handleQuery* methods + respondError + respondJSON
// below form a parallel msg-style handler family that is NEVER
// registered for production traffic. Only the *NATS-suffixed
// handlers above are wired via SubscribeForRequests; the msg-style
// handlers exist solely for the in-process unit tests in this
// package and emit a `{"error": "..."}` JSON envelope (a third
// divergent error shape, distinct from both the legacy `error: `
// body prefix and the gh#93 X-Status header convention).
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

// handleQueryOutgoing handles outgoing relationship query requests
func (c *Component) handleQueryOutgoing(msg queryMsg) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Validate request
	if req.EntityID == "" {
		c.respondError(msg, "invalid request")
		return
	}

	// Get outgoing relationships from KV bucket
	entry, err := c.outgoingBucket.Get(ctx, req.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// Return empty array for not found
			c.respondJSON(msg, []graph.OutgoingEntry{})
			return
		}
		c.respondError(msg, "internal error")
		return
	}

	// Parse outgoing entries
	var entries []graph.OutgoingEntry
	if err := json.Unmarshal(entry.Value, &entries); err != nil {
		c.respondError(msg, "internal error")
		return
	}

	// Respond with entries array
	c.respondJSON(msg, entries)
}

// handleQueryIncoming handles incoming relationship query requests
func (c *Component) handleQueryIncoming(msg queryMsg) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Validate request
	if req.EntityID == "" {
		c.respondError(msg, "invalid request")
		return
	}

	// Get incoming relationships from KV bucket
	entry, err := c.incomingBucket.Get(ctx, req.EntityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// Return empty array for not found
			c.respondJSON(msg, []graph.IncomingEntry{})
			return
		}
		c.respondError(msg, "internal error")
		return
	}

	// Parse incoming entries
	var entries []graph.IncomingEntry
	if err := json.Unmarshal(entry.Value, &entries); err != nil {
		c.respondError(msg, "internal error")
		return
	}

	// Respond with entries array
	c.respondJSON(msg, entries)
}

// handleQueryAlias handles alias resolution query requests
func (c *Component) handleQueryAlias(msg queryMsg) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Validate request
	if req.Alias == "" {
		c.respondError(msg, "invalid request")
		return
	}

	// Get canonical entity ID from KV bucket
	entry, err := c.aliasBucket.Get(ctx, req.Alias)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			c.respondError(msg, "not found")
			return
		}
		c.respondError(msg, "internal error")
		return
	}

	// Respond with canonical ID
	response := map[string]string{
		"canonical_id": string(entry.Value),
	}
	c.respondJSON(msg, response)
}

// handleQueryPredicate handles predicate entity query requests
func (c *Component) handleQueryPredicate(msg queryMsg) {
	// Create context with timeout for KV operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		Predicate string  `json:"predicate"`
		Value     *string `json:"value,omitempty"`
		Limit     int     `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.respondError(msg, "invalid request")
		return
	}

	// Validate request
	if req.Predicate == "" {
		c.respondError(msg, "invalid request")
		return
	}

	entities, err := c.queryPredicateEntities(ctx, req.Predicate, req.Value, req.Limit)
	if err != nil {
		c.respondError(msg, "internal error")
		return
	}

	// Respond with entities array
	c.respondJSON(msg, map[string][]string{
		"entities": entities,
	})
}

// queryPredicateEntities is the shared helper used by both NATS and msg-based predicate
// handlers. It looks up the predicate index, optionally filters by value, and applies
// the limit in a single place so both call sites stay consistent.
func (c *Component) queryPredicateEntities(ctx context.Context, predicate string, value *string, limit int) ([]string, error) {
	entry, err := c.predicateBucket.Get(ctx, predicate)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var indexEntry graph.PredicateIndexEntry
	if err := json.Unmarshal(entry.Value, &indexEntry); err != nil {
		return nil, err
	}

	entities := indexEntry.Entities

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

// handleQueryPredicateListNATS handles predicate list query requests via NATS request/reply.
// Returns all predicates with their entity counts.
func (c *Component) handleQueryPredicateListNATS(ctx context.Context, _ []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Get all predicate keys from the bucket
	keys, err := c.predicateBucket.Keys(ctx)
	if err != nil {
		return json.Marshal(graph.NewQueryError[graph.PredicateListData]("internal error"))
	}
	if len(keys) == 0 {
		return json.Marshal(graph.NewQueryResponse(graph.PredicateListData{
			Predicates: []graph.PredicateSummary{},
			Total:      0,
		}))
	}

	predicates := make([]graph.PredicateSummary, 0, len(keys))
	for _, predicate := range keys {
		entry, err := c.predicateBucket.Get(ctx, predicate)
		if err != nil {
			continue // Skip predicates we can't read
		}

		var indexEntry graph.PredicateIndexEntry
		if err := json.Unmarshal(entry.Value, &indexEntry); err != nil {
			continue // Skip malformed entries
		}

		predicates = append(predicates, graph.PredicateSummary{
			Predicate:   predicate,
			EntityCount: len(indexEntry.Entities),
		})
	}

	return json.Marshal(graph.NewQueryResponse(graph.PredicateListData{
		Predicates: predicates,
		Total:      len(predicates),
	}))
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
		return json.Marshal(graph.NewQueryError[graph.PredicateStatsData]("invalid request"))
	}

	if req.Predicate == "" {
		return json.Marshal(graph.NewQueryError[graph.PredicateStatsData]("invalid request: empty predicate"))
	}

	// Default sample limit
	if req.SampleLimit <= 0 {
		req.SampleLimit = 10
	}

	entry, err := c.predicateBucket.Get(ctx, req.Predicate)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.PredicateStatsData{
				Predicate:      req.Predicate,
				EntityCount:    0,
				SampleEntities: []string{},
			}))
		}
		return json.Marshal(graph.NewQueryError[graph.PredicateStatsData]("internal error"))
	}

	var indexEntry graph.PredicateIndexEntry
	if err := json.Unmarshal(entry.Value, &indexEntry); err != nil {
		return json.Marshal(graph.NewQueryError[graph.PredicateStatsData]("internal error"))
	}

	// Get sample entities
	sampleEntities := indexEntry.Entities
	if len(sampleEntities) > req.SampleLimit {
		sampleEntities = sampleEntities[:req.SampleLimit]
	}

	return json.Marshal(graph.NewQueryResponse(graph.PredicateStatsData{
		Predicate:      req.Predicate,
		EntityCount:    len(indexEntry.Entities),
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
		return json.Marshal(graph.NewQueryError[graph.CompoundPredicateData]("invalid request"))
	}

	if len(req.Predicates) == 0 {
		return json.Marshal(graph.NewQueryError[graph.CompoundPredicateData]("invalid request: empty predicates"))
	}

	operator := req.Operator
	if operator != "AND" && operator != "OR" {
		return json.Marshal(graph.NewQueryError[graph.CompoundPredicateData]("invalid request: operator must be AND or OR"))
	}

	// Collect entity sets for each predicate
	entitySets := make([]map[string]struct{}, 0, len(req.Predicates))
	for _, predicate := range req.Predicates {
		entry, err := c.predicateBucket.Get(ctx, predicate)
		if err != nil {
			if natsclient.IsKVNotFoundError(err) {
				// Predicate not found - empty set
				entitySets = append(entitySets, make(map[string]struct{}))
				continue
			}
			return json.Marshal(graph.NewQueryError[graph.CompoundPredicateData]("internal error"))
		}

		var indexEntry graph.PredicateIndexEntry
		if err := json.Unmarshal(entry.Value, &indexEntry); err != nil {
			return json.Marshal(graph.NewQueryError[graph.CompoundPredicateData]("internal error"))
		}

		entitySet := make(map[string]struct{}, len(indexEntry.Entities))
		for _, e := range indexEntry.Entities {
			entitySet[e] = struct{}{}
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
