// Package graphquery query handlers
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/nats-io/nats.go"
)

const (
	graphQueriesPortName       = "graph_queries"
	graphQuerySubjectFamily    = "graph.query.*"
	graphQueryInterfaceType    = "graph.query"
	graphQueryInterfaceVersion = "v1"
)

type queryEnvelopeShape string

const (
	queryEnvelopeBare     queryEnvelopeShape = "bare"
	queryEnvelopeStandard queryEnvelopeShape = "graph.QueryResponse"
)

type queryOperationSpec struct {
	operation    string
	suffix       string
	requestType  string
	successType  string
	envelope     queryEnvelopeShape
	graphQLField string
	consumers    []string
	availability string
	handler      func(*Component) func(context.Context, []byte) ([]byte, error)
}

// graphQueryOperations is the single internal conformance and registration
// inventory for the admitted graph.query/v1 family. It is deliberately not an
// exported subject catalog: components declare the family or exact operation
// ports, while remote applications discover the admitted GraphQL fields.
var graphQueryOperations = []queryOperationSpec{
	{operation: "entity", suffix: "entity", requestType: "{id:string}", successType: "graph.ExactEntity", envelope: queryEnvelopeBare, graphQLField: "entity", consumers: []string{"graph-gateway", "fusionnats"}, availability: "authority responder required", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryEntity }},
	{operation: "entityByAlias", suffix: "entityByAlias", requestType: "{aliasOrID:string}", successType: "graph.ExactEntity", envelope: queryEnvelopeBare, graphQLField: "entityByAlias", consumers: []string{"graph-gateway"}, availability: "alias view errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryEntityByAlias }},
	{operation: "batch", suffix: "batch", requestType: "{ids:[]string}", successType: "graph.EntityBatchResponse", envelope: queryEnvelopeBare, consumers: []string{"research-graph-execute", "fusionnats"}, availability: "authority responder required", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryBatch }},
	{operation: "relationships", suffix: "relationships", requestType: "{entity_id:string,direction:string}", successType: "[]relationshipWire", envelope: queryEnvelopeBare, graphQLField: "relationships", consumers: []string{"graph-gateway", "research-graph-execute", "fusionnats"}, availability: "graph-index errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryRelationships }},
	{operation: "pathSearch", suffix: "pathSearch", requestType: "graphquery.PathSearchRequest", successType: "graphquery.PathSearchResponse", envelope: queryEnvelopeBare, graphQLField: "pathSearch", consumers: []string{"graph-gateway"}, availability: "required backing responders remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handlePathSearch }},
	{operation: "hierarchyStats", suffix: "hierarchyStats", requestType: "{prefix:string}", successType: "{prefix:string,totalEntities:int,children:[]graphquery.HierarchyChild}", envelope: queryEnvelopeBare, graphQLField: "entityIdHierarchy", consumers: []string{"graph-gateway"}, availability: "authority errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryHierarchyStats }},
	{operation: "prefix", suffix: "prefix", requestType: "graph.PrefixQueryRequest", successType: "graph.PrefixQueryResponse", envelope: queryEnvelopeBare, graphQLField: "entitiesByPrefix", consumers: []string{"graph-gateway", "fusionnats"}, availability: "authority responder required", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryPrefix }},
	{operation: "spatial", suffix: "spatial", requestType: "{north:float64,south:float64,east:float64,west:float64,limit:int}", successType: "[]graphindexspatial.SpatialResult", envelope: queryEnvelopeBare, graphQLField: "spatialSearch", consumers: []string{"graph-gateway"}, availability: "optional index errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQuerySpatial }},
	{operation: "temporal", suffix: "temporal", requestType: "{startTime:string,endTime:string,limit:int}", successType: "[]graphindextemporal.TemporalResult", envelope: queryEnvelopeBare, graphQLField: "temporalSearch", consumers: []string{"graph-gateway", "research-graph-execute"}, availability: "optional index errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryTemporal }},
	{operation: "semantic", suffix: "semantic", requestType: "graphembedding.SearchRequest", successType: "graphembedding.SearchResponse", envelope: queryEnvelopeBare, graphQLField: "semanticSearch", consumers: []string{"graph-gateway", "fusionnats"}, availability: "optional embedding errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQuerySemantic }},
	{operation: "similar", suffix: "similar", requestType: "graphembedding.SimilarRequest", successType: "graphembedding.SimilarResponse", envelope: queryEnvelopeBare, graphQLField: "findSimilar", consumers: []string{"graph-gateway"}, availability: "optional embedding errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQuerySimilar }},
	{operation: "globalSearch", suffix: "globalSearch", requestType: "graphquery.GlobalSearchRequest", successType: "graphquery.GlobalSearchResponse", envelope: queryEnvelopeBare, graphQLField: "globalSearch", consumers: []string{"graph-gateway"}, availability: "community-only tier returns transient index_not_ready; lower tiers preserve results with community_cache_not_ready degradation", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleGlobalSearch }},
	{operation: "summary", suffix: "summary", requestType: "graph.SummaryRequest", successType: "graph.SummaryData", envelope: queryEnvelopeStandard, graphQLField: "graphSummary", consumers: []string{"graph-gateway"}, availability: "required backing responders remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryGraphSummary }},
	{operation: "searchGraph", suffix: "searchGraph", requestType: "graphquery.GlobalSearchRequest", successType: "graphquery.GlobalSearchResponse", envelope: queryEnvelopeBare, graphQLField: "searchGraph", consumers: []string{"graph-gateway", "research-graph-classify", "research-graph-execute"}, availability: "semantic fallback preserves its strategy and reports requested unavailable community enrichment", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleSearchGraph }},
	{operation: "byName", suffix: "byName", requestType: "{name:string,limit:int}", successType: "graph.NameData", envelope: queryEnvelopeStandard, consumers: []string{"fusionnats"}, availability: "name-index errors remain classified", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleQueryByName }},
	{operation: "localSearch", suffix: "localSearch", requestType: "graphquery.LocalSearchRequest", successType: "graphquery.LocalSearchResponse", envelope: queryEnvelopeBare, graphQLField: "localSearch", consumers: []string{"graph-gateway"}, availability: "transient index_not_ready until community cache usable", handler: func(c *Component) func(context.Context, []byte) ([]byte, error) { return c.handleLocalSearch }},
}

func graphQueryInterface() *component.InterfaceContract {
	return &component.InterfaceContract{Type: graphQueryInterfaceType, Version: graphQueryInterfaceVersion}
}

// setupQueryHandlers subscribes to all query request subjects
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	subscribe := c.subscribeForRequests
	if subscribe == nil {
		subscribe = c.natsClient.SubscribeForRequests
	}
	subjects := make([]string, 0, len(graphQueryOperations))
	for _, operation := range graphQueryOperations {
		subject, err := component.ResolveSubject([]component.PortDefinition{{
			Name:   graphQueriesPortName,
			Config: component.NATSRequestPort{Subject: c.queryFamily},
		}}, graphQueriesPortName, operation.suffix)
		if err != nil {
			return errs.WrapInvalid(err, "GraphQuery", "setupQueryHandlers", "resolve "+operation.operation)
		}
		sub, err := subscribe(ctx, subject, operation.handler(c))
		if err != nil {
			return errs.WrapTransient(err, "GraphQuery", "setupQueryHandlers", "subscribe to "+subject)
		}
		c.querySubscriptions = append(c.querySubscriptions, sub)
		subjects = append(subjects, subject)
	}

	c.logger.Info("query handlers registered", "subjects", subjects)

	return nil
}

// handleQueryEntity handles entity query requests (passthrough to graph-ingest)
func (c *Component) handleQueryEntity(ctx context.Context, data []byte) ([]byte, error) {
	// Report querying stage (throttled to avoid KV spam)
	// Parse and validate request
	var req map[string]string
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryEntity", "parse request")
	}

	if req["id"] == "" {
		return nil, errs.WrapInvalid(errors.New("empty id"), "GraphQuery", "handleQueryEntity", "validate request")
	}

	// Route to entity query
	subject := c.router.Route("entity")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("entity query routing not available"), "GraphQuery", "handleQueryEntity", "route query")
	}
	// ADR-060: RequestClassified so a downstream classified error (e.g.
	// entity_not_found/invalid from graph-ingest) propagates UNWRAPPED — returning
	// it as-is lets graph-query's SubscribeForRequests wrapper re-stamp the wire
	// class + code. Plain Request + verbatim passthrough would drop the header and
	// re-emit the error body as success (silent 404→200 at the consumer); wrapping
	// as transient would clobber the class. A transport timeout still classifies
	// transient via the wrapper.
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}
	if err := validateEntityQueryResponse(response); err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}

// handleQueryEntityByAlias resolves an alias to an entity ID, then fetches the entity.
// It first tries to resolve aliasOrID via graph-index alias lookup.
// If found, it fetches the entity using the canonical ID.
// If not found as alias, it tries aliasOrID as a direct entity ID.
func (c *Component) handleQueryEntityByAlias(ctx context.Context, data []byte) ([]byte, error) {
	// Parse request
	var req struct {
		AliasOrID string `json:"aliasOrID"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryEntityByAlias", "parse request")
	}

	if req.AliasOrID == "" {
		return nil, errs.WrapInvalid(errors.New("empty aliasOrID"), "GraphQuery", "handleQueryEntityByAlias", "validate request")
	}

	entityID := req.AliasOrID // Default to using input as entity ID

	// Try to resolve as alias first via graph-index
	aliasReq := map[string]string{"alias": req.AliasOrID}
	aliasReqData, _ := json.Marshal(aliasReq)

	aliasSubject := c.router.Route("alias")
	if aliasSubject == "" {
		return nil, errs.WrapTransient(errors.New("alias query routing not available"), "GraphQuery", "handleQueryEntityByAlias", "route alias query")
	}
	// Best-effort alias resolution via the graph-index alias seam.
	// RequestClassified surfaces handler errors via err (ADR-060), and the
	// success body no longer carries an in-band Error field. A failed alias
	// lookup (transient backend or no alias) intentionally falls through to
	// treating aliasOrID as a direct entity ID — alias resolution must not
	// fail the whole query.
	aliasResp, err := c.natsClient.RequestClassified(ctx, aliasSubject, aliasReqData, c.config.QueryTimeout)
	if err == nil {
		var aliasResult graph.AliasQueryResponse
		if json.Unmarshal(aliasResp, &aliasResult) == nil && aliasResult.Data.CanonicalID != nil {
			entityID = *aliasResult.Data.CanonicalID
		}
	}

	// Now fetch the entity using the resolved (or original) ID
	entityReq := map[string]string{"id": entityID}
	entityReqData, _ := json.Marshal(entityReq)

	entitySubject := c.router.Route("entity")
	if entitySubject == "" {
		return nil, errs.WrapTransient(errors.New("entity query routing not available"), "GraphQuery", "handleQueryEntityByAlias", "route entity query")
	}
	// ADR-060: propagate a downstream classified error (e.g. entity_not_found)
	// UNWRAPPED so the wrapper re-stamps the wire class + code (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, entitySubject, entityReqData, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}
	if err := validateEntityQueryResponse(response); err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}

// handleQueryBatch handles batch entity query requests (passthrough to
// graph-ingest). Removes the N+1 collection-read pattern semconnect
// (and other CS API gateway consumers) hit when hydrating predicate-
// query results: instead of `predicate query → N entity queries`, the
// flow becomes `predicate query → 1 batch query` — two NATS round-
// trips total, regardless of result set size. gh#172 Phase 1.
//
// The internal graph.ingest.query.batch handler this forwards to
// already does bounded concurrency (default 10 parallel KV gets),
// cache-aware reads, and partial success. Partial success now REPORTS
// itself: the reply carries `missing: [{id, reason}]` naming every
// requested ID that did not hydrate, so a consumer can reconcile the
// two lists against what it asked for instead of inferring from a
// shorter list (ADR-084 D4 / gh#597). A fully-hydrated batch omits the
// field, so this passthrough is byte-unchanged for existing consumers.
// This lifts that internal subject to the public graph.query.* surface.
//
// Chunking guidance: batches above ~100 IDs with substantial triple
// sets risk exceeding NATS's default 1MB max_payload on the reply.
// Callers expecting unbounded result-set sizes should chunk client-
// side. Streaming/pagination for unbounded collections is a separate
// design question (filed as a follow-up if the threshold actually
// bites in practice).
func (c *Component) handleQueryBatch(ctx context.Context, data []byte) ([]byte, error) {
	// Validate request shape early — empty IDs is a no-op at the
	// graph-ingest side, but malformed JSON should surface as
	// ErrorInvalid at the caller (matches handleQueryEntity).
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryBatch", "parse request")
	}

	subject := c.router.Route("entityBatch")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("entityBatch query routing not available"), "GraphQuery", "handleQueryBatch", "route query")
	}
	// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}
	if err := validateEntityBatchQueryResponse(response); err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}

// handleQueryPrefix handles prefix query requests (passthrough to graph-ingest).
//
// Uses RequestClassified (gh#93) so that a transient handler error from
// graph-ingest — e.g. "store unavailable" — surfaces to the caller as
// ErrorTransient rather than being mis-classified as ErrorInvalid by the
// legacy "error: " body-prefix fallback in ClassifyReply. Correct
// classification lets the caller decide whether to retry.
func (c *Component) handleQueryPrefix(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.PrefixQueryRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryPrefix", "parse request")
	}
	if req.Prefix != "" {
		if err := semtypes.ValidateEntityIDPrefix(req.Prefix); err != nil {
			return nil, err
		}
	}
	// Forward to graph-ingest
	subject := c.router.Route("entityPrefix")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("entityPrefix query routing not available"), "GraphQuery", "handleQueryPrefix", "route query")
	}
	// RequestClassified: transport AND handler errors both surface via err;
	// body is guaranteed clean on success. Replaces plain Request + body-
	// prefix sniff to preserve error class end-to-end (gh#304).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		if errors.Is(err, nats.ErrTimeout) {
			return nil, errs.WrapTransient(err, "GraphQuery", "handleQueryPrefix", "request timeout")
		}
		return nil, err
	}
	if err := validateEntityPrefixQueryResponse(response); err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}

func validateEntityQueryResponse(data []byte) error {
	var response struct {
		Entity     json.RawMessage `json:"entity"`
		KVRevision uint64          `json:"kvRevision"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("decode exact entity query response: %w", err)
	}
	if len(response.Entity) == 0 || string(response.Entity) == "null" {
		return errors.New("validate exact entity query response: missing entity")
	}
	if response.KVRevision == 0 {
		return errors.New("validate exact entity query response: zero KV revision")
	}
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(response.Entity, &entity); err != nil {
		return fmt.Errorf("validate entity query response: %w", err)
	}
	return nil
}

func validateEntityBatchQueryResponse(data []byte) error {
	var response struct {
		Entities []*graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("decode entity batch query response: %w", err)
	}
	if err := graph.ValidateDecodedEntityStatePointers(response.Entities); err != nil {
		return fmt.Errorf("validate entity batch query response: %w", err)
	}
	return nil
}

func validateEntityPrefixQueryResponse(data []byte) error {
	var response graph.PrefixQueryResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("decode entity prefix query response: %w", err)
	}
	if err := graph.ValidateDecodedEntityStates(response.Entities); err != nil {
		return fmt.Errorf("validate entity prefix query response: %w", err)
	}
	return nil
}

// relationshipWire is the bare graph.query.relationships success row consumed
// by graph-gateway, research execute, and fusion. EdgeType is intentionally
// spelled edge_type on the wire; the GraphRAG Relationship type is a different
// internal representation with a predicate field.
type relationshipWire struct {
	EdgeType     string `json:"edge_type"`
	FromEntityID string `json:"from_entity_id"`
	ToEntityID   string `json:"to_entity_id"`
}

// handleQueryRelationships handles relationship query requests (passthrough to graph-index)
func (c *Component) handleQueryRelationships(ctx context.Context, data []byte) ([]byte, error) {
	// Report querying stage (throttled to avoid KV spam)
	// Parse and validate request
	var req struct {
		EntityID  string `json:"entity_id"`
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryRelationships", "parse request")
	}

	if req.EntityID == "" {
		return nil, errs.WrapInvalid(errors.New("empty entity_id"), "GraphQuery", "handleQueryRelationships", "validate request")
	}

	// Route based on direction
	isIncoming := req.Direction == "incoming"
	queryType := "outgoing"
	if isIncoming {
		queryType = "incoming"
	}
	subject := c.router.Route(queryType)
	if subject == "" {
		return nil, errs.WrapTransient(errors.New(queryType+" query routing not available"), "GraphQuery", "handleQueryRelationships", "route query")
	}
	// RequestClassified surfaces graph-index handler errors via err as a
	// classified *errs.ClassifiedError (ADR-060 PR-B). Wrap with %w so the
	// inner class (invalid vs transient) survives for upstream errs.Is*
	// branching instead of being flattened to transient.
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, fmt.Errorf("GraphQuery.handleQueryRelationships: query relationships failed: %w", err)
	}

	// Transform envelope response to normalized relationship format
	// graph-index returns QueryResponse envelope with relationships array
	// Return a bare array; graph-gateway adds its GraphQL response wrapper.
	var relationships []relationshipWire

	if isIncoming {
		// Parse incoming relationships from envelope
		var envelope graph.IncomingQueryResponse
		if err := json.Unmarshal(response, &envelope); err != nil {
			return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryRelationships", "parse incoming entries")
		}
		relationships = make([]relationshipWire, len(envelope.Data.Relationships))
		for i, e := range envelope.Data.Relationships {
			relationships[i] = relationshipWire{
				EdgeType: e.Predicate, FromEntityID: e.FromEntityID, ToEntityID: req.EntityID,
			}
		}
	} else {
		// Parse outgoing relationships from envelope
		var envelope graph.OutgoingQueryResponse
		if err := json.Unmarshal(response, &envelope); err != nil {
			return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryRelationships", "parse outgoing entries")
		}
		relationships = make([]relationshipWire, len(envelope.Data.Relationships))
		for i, e := range envelope.Data.Relationships {
			relationships[i] = relationshipWire{
				EdgeType: e.Predicate, FromEntityID: req.EntityID, ToEntityID: e.ToEntityID,
			}
		}
	}

	// Return just the array - gateway will wrap in {"data": {"relationships": ...}}
	responseData, err := json.Marshal(relationships)
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "handleQueryRelationships", "marshal response")
	}

	c.recordSuccess(len(data), len(responseData))
	return responseData, nil
}

// handlePathSearch handles PathRAG traversal queries
func (c *Component) handlePathSearch(ctx context.Context, data []byte) ([]byte, error) {
	// Report querying stage (throttled to avoid KV spam)
	var req PathSearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handlePathSearch", "parse request")
	}

	// Ensure pathSearcher is initialized (for testing with direct component construction)
	searcher := c.pathSearcher
	if searcher == nil {
		searcher = NewPathSearcher(c.natsClient, c.config.QueryTimeout, c.config.MaxDepth, c.logger)
	}

	result, err := searcher.Search(ctx, req)
	if err != nil {
		c.recordError(err)
		return nil, err
	}

	// Return raw PathSearch response - gateway wraps in GraphQL format
	responseData, err := json.Marshal(result)
	if err != nil {
		c.recordError(err)
		return nil, errs.Wrap(err, "GraphQuery", "handlePathSearch", "marshal response")
	}

	c.recordSuccess(len(data), len(responseData))
	return responseData, nil
}

// HierarchyChild represents a child node in the hierarchy
type HierarchyChild struct {
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
}

// handleQueryHierarchyStats handles hierarchy stats queries by orchestrating graph-ingest
func (c *Component) handleQueryHierarchyStats(ctx context.Context, data []byte) ([]byte, error) {
	// Parse request
	var req struct {
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryHierarchyStats", "parse request")
	}
	if req.Prefix != "" {
		if err := semtypes.ValidateEntityIDPrefix(req.Prefix); err != nil {
			return nil, err
		}
	}

	// Get all entity IDs with prefix from graph-ingest.
	// Use typed PrefixQueryRequest to participate in the pagination contract;
	// 10000 is intentionally above MaxPrefixQueryLimit so the server clamps
	// it — hierarchy stats is a best-effort scan and accepts the clamped page.
	prefixReq, err := json.Marshal(graph.PrefixQueryRequest{Prefix: req.Prefix, Limit: 10000})
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "handleQueryHierarchyStats", "marshal prefix request")
	}

	subject := c.router.Route("entityPrefix")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("entityPrefix query routing not available"), "GraphQuery", "handleQueryHierarchyStats", "route query")
	}
	// RequestClassified: handler errors (e.g. store unavailable) surface via
	// err with the correct class instead of arriving as a "error: <msg>" body
	// that json.Unmarshal would then mis-classify as WrapInvalid (gh#304).
	response, err := c.natsClient.RequestClassified(ctx, subject, prefixReq, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		if errors.Is(err, nats.ErrTimeout) {
			return nil, errs.WrapTransient(err, "GraphQuery", "handleQueryHierarchyStats", "request timeout")
		}
		return nil, err
	}

	// Parse prefix response (entities envelope from graph-ingest)
	var envelope graph.PrefixQueryResponse
	if err := json.Unmarshal(response, &envelope); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleQueryHierarchyStats", "parse prefix response")
	}
	if err := graph.ValidateDecodedEntityStates(envelope.Entities); err != nil {
		return nil, err
	}

	// Extract entity IDs and group by next hierarchy level
	childCounts := make(map[string]int)
	for _, entity := range envelope.Entities {
		nextLevel := extractNextLevel(entity.ID, req.Prefix)
		if nextLevel != "" {
			childCounts[nextLevel]++
		}
	}

	// Build sorted children array
	children := buildSortedChildren(childCounts)

	// Build response
	result := map[string]any{
		"prefix":        req.Prefix,
		"totalEntities": len(envelope.Entities),
		"children":      children,
	}

	responseData, err := json.Marshal(result)
	if err != nil {
		c.recordError(err)
		return nil, errs.Wrap(err, "GraphQuery", "handleQueryHierarchyStats", "marshal response")
	}

	c.recordSuccess(len(data), len(responseData))
	return responseData, nil
}

// extractNextLevel extracts the next hierarchy segment after the given prefix
// For example: extractNextLevel("c360.a.b.c", "c360") returns "c360.a"
func extractNextLevel(entityID, prefix string) string {
	// Handle empty prefix (root level)
	if prefix == "" {
		// Return first segment
		parts := strings.SplitN(entityID, ".", 2)
		if len(parts) > 0 {
			return parts[0]
		}
		return ""
	}

	// Entity must start with prefix
	prefixDot := prefix + "."
	if !strings.HasPrefix(entityID, prefixDot) {
		// Check for exact match (entity IS the prefix)
		if entityID == prefix {
			return ""
		}
		return ""
	}

	// Get the part after the prefix
	remainder := strings.TrimPrefix(entityID, prefixDot)
	if remainder == "" {
		return ""
	}

	// Get next segment
	parts := strings.SplitN(remainder, ".", 2)
	if len(parts) > 0 && parts[0] != "" {
		return prefix + "." + parts[0]
	}

	return ""
}

// extractLastSegment returns the last segment of a prefix for display name
func extractLastSegment(prefix string) string {
	if prefix == "" {
		return ""
	}
	parts := strings.Split(prefix, ".")
	return parts[len(parts)-1]
}

// buildSortedChildren builds a sorted slice of HierarchyChild from counts map
func buildSortedChildren(childCounts map[string]int) []HierarchyChild {
	children := make([]HierarchyChild, 0, len(childCounts))

	for prefix, count := range childCounts {
		children = append(children, HierarchyChild{
			Prefix: prefix,
			Name:   extractLastSegment(prefix),
			Count:  count,
		})
	}

	// Sort by count descending, then by name ascending
	sort.Slice(children, func(i, j int) bool {
		if children[i].Count != children[j].Count {
			return children[i].Count > children[j].Count
		}
		return children[i].Name < children[j].Name
	})

	return children
}

// handleQuerySpatial handles spatial query requests (passthrough to graph-index-spatial)
func (c *Component) handleQuerySpatial(ctx context.Context, data []byte) ([]byte, error) {
	// Route to spatial query
	subject := c.router.Route("spatial")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("spatial query routing not available"), "GraphQuery", "handleQuerySpatial", "route query")
	}
	// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}

// handleQueryTemporal handles temporal query requests (passthrough to graph-index-temporal)
func (c *Component) handleQueryTemporal(ctx context.Context, data []byte) ([]byte, error) {
	// Route to temporal query
	subject := c.router.Route("temporal")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("temporal query routing not available"), "GraphQuery", "handleQueryTemporal", "route query")
	}
	// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}

// handleQueryByName handles deterministic name→ranked-IDs requests (gh#376),
// a passthrough to graph-index's graph.index.query.byName.
func (c *Component) handleQueryByName(ctx context.Context, data []byte) ([]byte, error) {
	subject := c.router.Route("byName")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("byName query routing not available"), "GraphQuery", "handleQueryByName", "route query")
	}
	// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}
	c.recordSuccess(len(data), len(response))
	return response, nil
}

// handleQuerySemantic handles semantic search requests (passthrough to graph-embedding)
func (c *Component) handleQuerySemantic(ctx context.Context, data []byte) ([]byte, error) {
	response, err := c.querySemantic(ctx, data)
	if err != nil {
		c.recordError(err)
		return nil, err
	}
	c.recordSuccess(len(data), len(response))
	return response, nil
}

// querySemantic performs the semantic transport without recording a top-level
// operation completion. Composite handlers such as SearchGraph account once at
// their own outer boundary.
func (c *Component) querySemantic(ctx context.Context, data []byte) ([]byte, error) {
	// Route to semantic query
	subject := c.router.Route("semantic")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("semantic query routing not available"), "GraphQuery", "handleQuerySemantic", "route query")
	}
	// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// handleQuerySimilar handles similar entity requests (passthrough to graph-embedding)
func (c *Component) handleQuerySimilar(ctx context.Context, data []byte) ([]byte, error) {
	// Forward to graph-embedding's similar handler
	subject := c.router.Route("similar")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("similar query routing not available"), "GraphQuery", "handleQuerySimilar", "route query")
	}
	// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).
	response, err := c.natsClient.RequestClassified(ctx, subject, data, c.config.QueryTimeout)
	if err != nil {
		c.recordError(err)
		return nil, err
	}

	c.recordSuccess(len(data), len(response))
	return response, nil
}
