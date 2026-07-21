// Package graphindex query handlers
package graphindex

import (
	"bytes"
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
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
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

	// There is deliberately no readiness-status subscription here. ADR-083 removed
	// `graph.index.query.status`: the envelope is published as watchable KV state to
	// GRAPH_STATUS/graph-index on the status tick, so `Get` is the point-in-time probe
	// (`nats kv get GRAPH_STATUS graph-index`), `Watch` is the event feed, and history
	// is the trajectory. A per-decision request/reply died under exactly the load that
	// makes readiness interesting — in a single-binary deployment it shared the
	// connection with the ENTITY_STATES firehose and timed out, failing closed with a
	// log line indistinguishable from a genuine not-ready (gh#590). Restoring it as a
	// convenience would mean two distribution paths to keep honest; it is a clean
	// break, and an unmigrated requester gets a loud no-responders error.

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
	if err := validateEntityLiteralKey(req.EntityID); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid request: entity_id: %w", err))
	}

	// Cutover-readiness gate (P1d/#6): OUTGOING is also rebuilt from ENTITY_STATES on a
	// cold/cutover replay, so don't serve a partial owner keyset while catching up.
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
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
//
// This is graph.GateStickyBootstrap (ADR-083 D4), and it is the mode's IN-PROCESS
// adopter: graph-index computes the envelope it gates on, so there is no transport to
// lose and freshness is unconditionally true. It reads no KV status key — publishing
// its own envelope and then reading it back would add a round-trip and a staleness
// window to a decision it can make from local state.
//
// Two local overrides stay OUTSIDE the canonical helper because they are facts about
// this component's own bookkeeping that the envelope does not carry, and both are
// deliberately evaluated BEFORE the sticky flag so it can never mask them:
//
//   - resetState is fatal and carries its own error code (ErrorCodeGraphStateResetRequired),
//     not the gate's transient one — the caller must stop, not retry.
//   - failedCount > 0 is a known-incomplete index: a required write/delete failed and
//     has not been repaired, so the index is not authoritative even after bootstrap
//     (gh#474 P1b / Codex #2).
//
// The sticky flag is likewise checked before computing status, which is what makes the
// mode "sticky": once bootstrapped this returns without a target-revision read. That
// ordering is load-bearing in both directions — it keeps the hot path off a per-query
// stream lookup, and it preserves the pre-ADR-083 behavior that a post-bootstrap
// degraded (stuck-watermark) index keeps serving. Folding the status compute in ahead
// of the sticky flag would newly defer that case; it is a semantic change, and it is
// not this change.
func (c *Component) ensureQueryReady(ctx context.Context) error {
	if c.resetState.Load() != nil {
		return errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
			errors.New("graph state reset required: export if needed, clear graph/index buckets, and reingest canonical sources"))
	}
	if c.failedCount.Load() > 0 {
		return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("index not ready: unresolved index write/delete failures"))
	}
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
	// BootstrapDone is false here by construction (the sticky flag short-circuited
	// above), so the helper evaluates the bootstrap half of the mode: proceed exactly
	// on Ready, defer on hard stops, an empty/pre-enumeration graph, and lag —
	// bit-identical to the `computeIndexStatus(ctx).Ready` check it replaces, because
	// hard stops and empty graphs both carry Ready == false.
	status := c.computeIndexStatus(ctx)
	proceed, reason := graph.EvaluateReadinessGate(status, true, graph.GateStickyBootstrap,
		graph.GateConfig{BootstrapDone: false})
	if proceed {
		// computeIndexStatus already latched indexBootstrapped (latchBootstrap flips on
		// the same Ready predicate this proceed decision reduces to), so there is no
		// second Store here — one home for the latch keeps the wire bit and this gate
		// from ever disagreeing.
		return nil
	}
	// The typed reason is evidence, not a new wire contract: the classified code and
	// message are unchanged (callers match on ErrorCodeIndexNotReady), and the reason
	// rides the debug log so a bootstrap that will not complete can be attributed
	// without correlating lines.
	c.logger.Debug("reverse-index query gate deferred",
		slog.String("reason", string(reason)),
		slog.String("state", status.State),
		slog.Uint64("indexed_revision", status.IndexedRevision),
		slog.Uint64("target_revision", status.TargetRevision),
		slog.Uint64("lag", status.Lag))
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
	if err := validateEntityLiteralKey(req.EntityID); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
			fmt.Errorf("invalid request: entity_id: %w", err))
	}
	prefix := incomingIndexPrefix(req.EntityID)
	if err := validateKVPrefix(prefix); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, err)
	}

	// Cutover-readiness gate (P1d): don't serve a partial keyset while the index is
	// still catching up after a format cutover / cold replay.
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
	}

	keys, err := c.incomingBucket.KeysByPrefix(ctx, prefix)
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	entries := make([]graph.IncomingEntry, 0, len(keys))
	seen := make(map[graph.IncomingEntry]struct{}, len(keys))
	for _, key := range keys {
		entry, ok := incomingEntryFromKey(key, req.EntityID)
		if !ok {
			c.logger.Debug("incoming query: skipping malformed key",
				slog.String("key", key),
				slog.String("entity_id", req.EntityID))
			continue
		}
		if _, duplicate := seen[entry]; duplicate {
			continue
		}
		seen[entry] = struct{}{}
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
	if err := natsclient.ValidateKVLiteralKey(req.Alias); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, err)
	}
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
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

	if _, err := validatedPredicateIndexExactFilter(req.Predicate); err != nil {
		return nil, invalidPredicateQueryError(err)
	}
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
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
	entities, err := c.queryPredicateEntityIDs(ctx, predicate)
	if err != nil {
		return nil, err
	}

	if value != nil && c.entityStatesBucket != nil {
		// filterEntitiesByPredicateValue handles limit internally so we avoid
		// iterating the full list twice.
		entities, err = c.filterEntitiesByPredicateValue(ctx, entities, predicate, *value, limit)
		if err != nil {
			return nil, err
		}
	} else if limit > 0 && len(entities) > limit {
		entities = entities[:limit]
	}

	return entities, nil
}

func (c *Component) queryPredicateEntityIDs(ctx context.Context, predicate string) ([]string, error) {
	filter, err := validatedPredicateIndexExactFilter(predicate)
	if err != nil {
		return nil, err
	}
	keys, err := c.predicateBucket.KeysByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}
	entities := make([]string, 0, len(keys))
	for _, key := range keys {
		storedPredicate, entityID, ok := predicateMembershipFromKey(key)
		if !ok || storedPredicate != predicate {
			continue
		}
		entities = append(entities, entityID)
	}
	return uniqueSortedStrings(entities), nil
}

// filterEntitiesByPredicateValue filters entity IDs by checking if their entity state
// contains a triple with the given predicate whose Object matches the specified value.
// limit is applied early — iteration stops once enough matches are collected.
// ctx cancellation is also checked on each iteration to allow cooperative cancellation.
func (c *Component) filterEntitiesByPredicateValue(ctx context.Context, entityIDs []string, predicate string, value string, limit int) ([]string, error) {
	var matched []string

	for _, entityID := range entityIDs {
		// Respect context cancellation between iterations.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if err := validateEntityLiteralKey(entityID); err != nil {
			return nil, err
		}
		entry, err := c.entityStatesBucket.Get(ctx, entityID)
		if err != nil {
			if natsclient.IsKVNotFoundError(err) {
				continue
			}
			return nil, fmt.Errorf("value filter: fetch entity %s: %w", entityID, err)
		}

		var state graph.EntityState
		if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
			return nil, err
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

	return matched, nil
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
// Returns predicates in ascending lexical order with their entity counts —
// every predicate, or, when the request carries a Namespace, only those sharing
// that exact domain or domain.category namespace. The fixed-nine-token layout
// makes both namespace scopes exact NATS wildcard filters over membership rows.
func (c *Component) handleQueryPredicateListNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var req graph.PredicateListQuery
	if len(data) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
		}
	}
	if req.Namespace != "" {
		if _, parseErr := validatedPredicateNamespaceFilter(strings.TrimSuffix(req.Namespace, ".")); parseErr != nil {
			return nil, invalidPredicateQueryError(parseErr)
		}
	}
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
	}

	var predicates []graph.PredicateSummary
	var err error
	if req.Namespace != "" {
		predicates, err = c.listPredicatesByNamespace(ctx, req.Namespace)
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

// listAllPredicates groups one scan of the self-describing predicate3.entity6
// membership bucket. No catalog or per-predicate fan-out is required.
func (c *Component) listAllPredicates(ctx context.Context) ([]graph.PredicateSummary, error) {
	keys, err := c.predicateBucket.Keys(ctx)
	if err != nil {
		return nil, err
	}
	return predicateSummariesFromMembershipKeys(keys), nil
}

// listPredicatesByNamespace uses one fixed-nine-token server-side filter for
// either domain.*.*.entity6 or domain.category.*.entity6.
func (c *Component) listPredicatesByNamespace(ctx context.Context, namespace string) ([]graph.PredicateSummary, error) {
	filter, err := validatedPredicateNamespaceFilter(strings.TrimSuffix(namespace, "."))
	if err != nil {
		return nil, err
	}
	keys, err := c.predicateBucket.KeysByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}
	return predicateSummariesFromMembershipKeys(keys), nil
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

	if _, err := validatedPredicateIndexExactFilter(req.Predicate); err != nil {
		return nil, invalidPredicateQueryError(err)
	}
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
	}

	// Default sample limit
	if req.SampleLimit <= 0 {
		req.SampleLimit = 10
	}

	entities, err := c.queryPredicateEntityIDs(ctx, req.Predicate)
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

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
	for _, predicate := range req.Predicates {
		if _, err := validatedPredicateIndexExactFilter(predicate); err != nil {
			return nil, invalidPredicateQueryError(err)
		}
	}

	operator := req.Operator
	if operator != "AND" && operator != "OR" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: operator must be AND or OR"))
	}
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
	}

	// Collect entity sets for each predicate
	entitySets := make([]map[string]struct{}, 0, len(req.Predicates))
	for _, predicate := range req.Predicates {
		entities, err := c.queryPredicateEntityIDs(ctx, predicate)
		if err != nil {
			return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
		}

		entitySet := make(map[string]struct{}, len(entities))
		for _, entityID := range entities {
			entitySet[entityID] = struct{}{}
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
	sort.Strings(entities)

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

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func validatedPredicateIndexExactFilter(predicate string) (string, error) {
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return "", err
	}
	filter := predicateIndexForwardFilter(predicate)
	if err := natsclient.ValidateKVWildcardFilter(filter); err != nil {
		return "", err
	}
	return filter, nil
}

func validatedPredicateNamespaceFilter(namespace string) (string, error) {
	if _, err := vocabulary.ParsePredicateNamespace(namespace); err != nil {
		return "", err
	}
	parts := strings.Split(namespace, ".")
	var filter string
	switch len(parts) {
	case 1:
		filter = namespace + ".*.*." + wildcardPositions(6)
	case 2:
		filter = namespace + ".*." + wildcardPositions(6)
	default:
		return "", errors.New("predicate namespace must contain one or two tokens")
	}
	if err := natsclient.ValidateKVWildcardFilter(filter); err != nil {
		return "", err
	}
	return filter, nil
}

func predicateMembershipFromKey(key string) (predicate, entityID string, ok bool) {
	parts := strings.Split(key, ".")
	if len(parts) != 9 {
		return "", "", false
	}
	predicate = strings.Join(parts[:3], ".")
	entityID = strings.Join(parts[3:], ".")
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return "", "", false
	}
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return "", "", false
	}
	return predicate, entityID, true
}

func predicateSummariesFromMembershipKeys(keys []string) []graph.PredicateSummary {
	entitiesByPredicate := make(map[string]map[string]struct{})
	for _, key := range uniqueSortedStrings(keys) {
		predicate, entityID, ok := predicateMembershipFromKey(key)
		if !ok {
			continue
		}
		entities := entitiesByPredicate[predicate]
		if entities == nil {
			entities = make(map[string]struct{})
			entitiesByPredicate[predicate] = entities
		}
		entities[entityID] = struct{}{}
	}
	names := make([]string, 0, len(entitiesByPredicate))
	for predicate := range entitiesByPredicate {
		names = append(names, predicate)
	}
	sort.Strings(names)
	summaries := make([]graph.PredicateSummary, 0, len(names))
	for _, predicate := range names {
		summaries = append(summaries, graph.PredicateSummary{
			Predicate: predicate, EntityCount: len(entitiesByPredicate[predicate]),
		})
	}
	return summaries
}

func invalidPredicateQueryError(err error) error {
	return errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest,
		fmt.Errorf("invalid predicate query: %w", err))
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
