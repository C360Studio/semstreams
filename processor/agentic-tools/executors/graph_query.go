// Package executors provides tool executor implementations for the agentic-tools component.
package executors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// KVGetter defines the minimal interface needed to query entities from a KV store.
// This allows for easier testing and decouples the executor from the full jetstream.KeyValue interface.
type KVGetter interface {
	Get(ctx context.Context, key string) (KVEntry, error)
}

// KVEntry defines the minimal interface needed to read an entry from the KV store.
type KVEntry interface {
	Value() []byte
	Revision() uint64
}

// KVKeyLister is the OPTIONAL capability query_by_type needs on top of
// KVGetter: listing the keys of the entity bucket that match a NATS subject
// filter. It is separate from KVGetter so that a binding which can only fetch
// by key keeps working for the other four tools, and so that query_by_type
// fails loudly and specifically when its binding cannot answer at all rather
// than reporting an empty listing.
//
// Implemented in production by graphQueryKVAdapter (register_graph_query.go)
// over graph.CatalogReader.ListKeysFiltered; consumed by queryByType.
type KVKeyLister interface {
	// KeysByPattern returns the keys matching an exact NATS subject filter.
	// It returns no partial result: a cancelled or expired context yields the
	// context error and a nil slice, never the keys collected so far.
	KeysByPattern(ctx context.Context, pattern string) ([]string, error)
}

// ErrKeyNotFound is returned when a key is not found in the KV store.
var ErrKeyNotFound = errs.ErrKeyNotFound

// GraphQueryExecutor executes graph queries against the ENTITY_STATES KV bucket.
type GraphQueryExecutor struct {
	kvGetter KVGetter
}

// NewGraphQueryExecutor creates a new GraphQueryExecutor with the given KV getter.
func NewGraphQueryExecutor(kvGetter KVGetter) *GraphQueryExecutor {
	return &GraphQueryExecutor{
		kvGetter: kvGetter,
	}
}

// ListTools returns the tool definitions provided by this executor.
func (e *GraphQueryExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "query_entity",
			Description: "Query an entity from the knowledge graph by its ID. Returns the entity's properties, relationships, and metadata as JSON.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "The full entity ID to query (e.g., acme.dep1.sensor.environmental.temperature.temp-sensor-001)",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "query_entities",
			Description: "Query multiple entities from the knowledge graph by their IDs in a single batch operation. More efficient than multiple query_entity calls.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Array of entity IDs to query",
					},
				},
				"required": []string{"entity_ids"},
			},
		},
		{
			Name: "query_relationships",
			// The description states what the tool can SEE, not what the
			// graph holds: an entity record carries its own outgoing
			// assertions, so incoming edges live behind a different owner
			// and this tool refuses rather than answering an empty set for
			// them (delta requirement 2).
			Description: "List the OUTGOING relationships recorded on one entity, optionally filtered to a single predicate. " +
				"Relationships are triples whose object is another entity ID; triples with literal values are properties, not relationships. " +
				"An empty result also reports every predicate present on the entity with its kind, so a zero count distinguishes " +
				"'this predicate is absent here' from 'you named a predicate that does not exist'.",
			Effect: agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "The entity ID to query relationships for",
					},
					"direction": map[string]any{
						"type":        "string",
						"enum":        []string{relationshipDirectionOutgoing},
						"description": "Only \"outgoing\" is served, and it is the default. This tool reads the entity's own record, which holds outgoing assertions only.",
					},
					"relationship_type": map[string]any{
						"type":        "string",
						"description": "Optional filter, exactly three dot-separated lower-case segments (domain.category.property, e.g. agent.lineage.parent). Anything else is rejected rather than answered with an empty list.",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name: "query_neighbors",
			Description: "Walk relationship edges out from one entity and return the neighboring entity records, up to `depth` hops. " +
				"Only relationships (triples whose object is an entity ID) are followed. Targets that are not in the graph are " +
				"listed under `unresolved` rather than dropped. The result is capped by a fixed content budget; when it is reached " +
				"the walk stops and reports `truncated` with the number of identities it did not expand — narrow with `depth` or `filter_type`.",
			Effect: agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "The starting entity ID",
					},
					"depth": map[string]any{
						"type":        "integer",
						"description": "Number of hops to traverse (default: 1, max: 3)",
						"minimum":     1,
						"maximum":     3,
					},
					"filter_type": map[string]any{
						"type":        "string",
						"description": entityTypeArgumentDescription("Optional filter keeping only neighbors whose identity matches this type."),
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name: "query_by_type",
			Description: "List the entity IDs whose identity carries a given type segment. Returns identities only — follow up with " +
				"query_entity or query_entities to read any of them. Results are sorted; when more match than fit the page, " +
				"the result carries a continuation token to pass back as `cursor`.",
			Effect:    agentic.ToolEffectReadOnly,
			Paginated: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_type": map[string]any{
						"type":        "string",
						"description": entityTypeArgumentDescription("The type to list."),
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of entity IDs to return (default: 10, max: 100)",
						"minimum":     1,
						"maximum":     100,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "Continuation token. Pass back the `next_cursor` value from a previous call VERBATIM to get the following page; omit it for the first page.",
					},
				},
				"required": []string{"entity_type"},
			},
		},
	}
}

// entityTypeArgumentDescription is the one place the `entity_type` /
// `filter_type` grammar is spelled for the model. Both arguments are matched
// by the same builder and the same matcher, so they get the same words:
// two spellings of one grammar is how a caller learns two rules for one fact.
func entityTypeArgumentDescription(lead string) string {
	return lead + " One to three dot-separated segments, read RIGHT to LEFT against the entity ID: " +
		"\"temperature\" matches any entity whose type segment is temperature; \"environmental.temperature\" also pins the domain; " +
		"\"gcs.environmental.temperature\" also pins the system. Wildcards are not accepted."
}

// Execute executes a tool call and returns the result.
func (e *GraphQueryExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "query_entity":
		return e.queryEntity(ctx, call)
	case "query_entities":
		return e.queryEntities(ctx, call)
	case "query_relationships":
		return e.queryRelationships(ctx, call)
	case "query_neighbors":
		return e.queryNeighbors(ctx, call)
	case "query_by_type":
		return e.queryByType(ctx, call)
	default:
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "GraphQueryExecutor", "Execute", "find tool")
	}
}

func (e *GraphQueryExecutor) queryEntity(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	// Extract entity_id from arguments
	entityID, ok := call.Arguments["entity_id"].(string)
	if !ok || entityID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Query the KV bucket
	entry, err := e.kvGetter.Get(ctx, entityID)
	if err != nil {
		if isKeyNotFound(err) {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("entity not found: %s", entityID),
				ErrorKind: agentic.ToolErrorNotFound,
			}, nil
		}
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to query entity: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "GraphQueryExecutor", "queryEntity", "get entity from KV")
	}
	if err := validateAuthoritativeEntity(entry.Value()); err != nil {
		return graphStateToolFailure(call.ID, err)
	}

	// Return the entity data as content
	// The value is stored as JSON, so we can return it directly
	content := string(entry.Value())

	// Optionally pretty-print if it's valid JSON
	var jsonData any
	if err := json.Unmarshal(entry.Value(), &jsonData); err == nil {
		if prettyJSON, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			content = string(prettyJSON)
		}
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
		Metadata: map[string]any{
			"entity_id": entityID,
			"revision":  entry.Revision(),
		},
	}, nil
}

// queryEntities performs a batch lookup of multiple entities
func (e *GraphQueryExecutor) queryEntities(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	// Extract entity_ids from arguments
	entityIDsRaw, ok := call.Arguments["entity_ids"]
	if !ok {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_ids is required",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Convert to string slice
	var entityIDs []string
	switch v := entityIDsRaw.(type) {
	case []interface{}:
		for _, id := range v {
			if s, ok := id.(string); ok {
				entityIDs = append(entityIDs, s)
			}
		}
	case []string:
		entityIDs = v
	default:
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_ids must be an array of strings",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	if len(entityIDs) == 0 {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_ids cannot be empty",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Query each entity and collect results
	results := make(map[string]json.RawMessage)
	notFound := []string{}

	for _, entityID := range entityIDs {
		entry, err := e.kvGetter.Get(ctx, entityID)
		if err != nil {
			if isKeyNotFound(err) {
				notFound = append(notFound, entityID)
				continue
			}
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("failed to query entity %s: %v", entityID, err),
				ErrorKind: agentic.ToolErrorNetwork,
			}, errs.WrapTransient(err, "GraphQueryExecutor", "queryEntities", "get entity from KV")
		}
		if validateErr := validateAuthoritativeEntity(entry.Value()); validateErr != nil {
			return graphStateToolFailure(call.ID, validateErr)
		}
		results[entityID] = json.RawMessage(entry.Value())
	}

	// Build response
	response := map[string]any{
		"entities": results,
		"count":    len(results),
	}
	if len(notFound) > 0 {
		response["not_found"] = notFound
	}

	content, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to marshal response: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(content),
		Metadata: map[string]any{
			"entity_count":    len(results),
			"not_found_count": len(notFound),
		},
	}, nil
}

// relationshipDirectionOutgoing is the only direction this tool serves, and
// the value an omitted `direction` is answered as.
const relationshipDirectionOutgoing = "outgoing"

// incomingRelationshipOwner names the operation that DOES read incoming edges,
// so a refusal points somewhere instead of just saying no. It is the admitted
// graph.query.relationships operation over INCOMING_INDEX
// (processor/graph-query/query.go graphQueryOperations "relationships").
const incomingRelationshipOwner = "the graph.query.relationships operation over INCOMING_INDEX"

// relationshipRow is the model-facing shape of one outgoing relationship. The
// key names are unchanged from the shape this tool has always emitted; what
// changed is which triples qualify (delta requirement 1).
type relationshipRow struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// predicatePresence describes one predicate observed on the entity. It is the
// answer to "why is this empty": the model sees the names that ARE there and
// whether each carries edges or literals, instead of guessing at a spelling.
//
// Role and InverseOf are pointers so a registered relationship renders them
// even when the registry left them at their zero value — "declared and empty"
// and "not applicable" are different facts, and omitempty on a plain string
// would collapse them.
type predicatePresence struct {
	Kind        string  `json:"kind"`
	Registered  bool    `json:"registered"`
	Description string  `json:"description,omitempty"`
	Role        *string `json:"role,omitempty"`
	InverseOf   *string `json:"inverse_of,omitempty"`
}

const (
	predicateKindRelationship = "relationship"
	predicateKindProperty     = "property"
)

// queryRelationships lists the outgoing relationships recorded on one entity.
func (e *GraphQueryExecutor) queryRelationships(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	entityID, ok := call.Arguments["entity_id"].(string)
	if !ok || entityID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Both argument checks run BEFORE the read: a direction this tool cannot
	// serve and a filter that is not a predicate are answers the tool already
	// holds, and reading first would spend a round trip to return the same
	// refusal (delta requirements 1 and 2).
	if refusal, refused := refuseUnservedDirection(call); refused {
		return refusal, nil
	}

	relType := ""
	if rt, ok := call.Arguments["relationship_type"].(string); ok {
		relType = rt
	}
	if relType != "" {
		if _, err := vocabulary.ParsePredicate(relType); err != nil {
			return agentic.ToolResult{
				CallID: call.ID,
				Error: fmt.Sprintf("relationship_type %q is not a canonical predicate "+
					"(exactly three lower-case dot-separated segments, domain.category.property): %v", relType, err),
				ErrorKind: agentic.ToolErrorInvalidArgs,
			}, nil
		}
	}

	entity, failure := e.readEntityState(ctx, call, entityID, "queryRelationships")
	if failure != nil {
		return failure.result, failure.err
	}

	relationships := outgoingRelationships(entity, relType)
	present := predicatesPresent(entity)

	response := map[string]any{
		"entity_id":          entityID,
		"relationships":      relationships,
		"count":              len(relationships),
		"direction":          relationshipDirectionOutgoing,
		"predicates_present": present,
	}
	if relType != "" {
		response["filter_type"] = relType
		// filter_registered reports THIS PROCESS's vocabulary registry and
		// nothing else. An unregistered predicate can be legitimately minted
		// under a namespace delegation (vocabulary.PredicateAuthority), and a
		// registered one can be absent from every entity — so this is never an
		// authorization or existence verdict (delta requirement 1).
		response["filter_registered"] = vocabulary.GetPredicateMetadata(relType) != nil
	}

	content, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to marshal response: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	toolResult := agentic.ToolResult{
		CallID:  call.ID,
		Content: string(content),
		Metadata: map[string]any{
			"relationship_count": len(relationships),
		},
	}
	if len(relationships) == 0 {
		toolResult.ResultHint = agentic.HintEmpty
	}
	return toolResult, nil
}

// refuseUnservedDirection applies the narrowing in delta requirement 2. An
// omitted or empty `direction` is SERVED as outgoing rather than refused:
// after the narrowing, outgoing is the only value there is, so demanding the
// caller type it would make them predict a fact the tool owns.
func refuseUnservedDirection(call agentic.ToolCall) (agentic.ToolResult, bool) {
	raw, present := call.Arguments["direction"]
	if !present {
		return agentic.ToolResult{}, false
	}
	direction, ok := raw.(string)
	if !ok {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "direction must be a string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, true
	}
	if direction == "" || direction == relationshipDirectionOutgoing {
		return agentic.ToolResult{}, false
	}
	if direction == "incoming" || direction == "both" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error: fmt.Sprintf("direction %q is not served by query_relationships: it reads the entity's own record, "+
				"which holds outgoing assertions only. Incoming relationships are owned by %s.",
				direction, incomingRelationshipOwner),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, true
	}
	// An unknown enum member is refused explicitly rather than silently
	// falling back to the default: a silent fallback answers a question the
	// caller did not ask.
	return agentic.ToolResult{
		CallID:    call.ID,
		Error:     fmt.Sprintf("direction %q is not a known value; the only direction served is %q", direction, relationshipDirectionOutgoing),
		ErrorKind: agentic.ToolErrorInvalidArgs,
	}, true
}

// outgoingRelationships selects the entity's own-subject relationship triples,
// optionally narrowed to one predicate.
//
// message.Triple.IsRelationship() is the authority on what a relationship is —
// an object that is a canonical entity ID under a reference-compatible
// datatype. Before this, every triple was reported as a relationship, so a
// literal temperature reading arrived at the model as an edge.
func outgoingRelationships(entity *graph.EntityState, relType string) []relationshipRow {
	rows := []relationshipRow{}
	for index := range entity.Triples {
		triple := &entity.Triples[index]
		if relType != "" && triple.Predicate != relType {
			continue
		}
		if triple.Subject != entity.ID {
			continue
		}
		if !triple.IsRelationship() {
			continue
		}
		target, _ := triple.Object.(string)
		rows = append(rows, relationshipRow{
			Type:   triple.Predicate,
			Source: triple.Subject,
			Target: target,
		})
	}
	return rows
}

// predicatesPresent reports every predicate carried on the entity's record
// with its kind and this process's registry metadata.
//
// A predicate that appears on both a relationship and a property triple is
// reported as a relationship: the invariant the delta states is that every
// relationship predicate appears here with kind relationship, so the edge
// spelling wins.
func predicatesPresent(entity *graph.EntityState) map[string]predicatePresence {
	present := make(map[string]predicatePresence, len(entity.Triples))
	for index := range entity.Triples {
		triple := &entity.Triples[index]
		kind := predicateKindProperty
		if triple.IsRelationship() {
			kind = predicateKindRelationship
		}
		if existing, seen := present[triple.Predicate]; seen && existing.Kind == predicateKindRelationship {
			continue
		}
		present[triple.Predicate] = describePredicate(triple.Predicate, kind)
	}
	return present
}

func describePredicate(predicate, kind string) predicatePresence {
	presence := predicatePresence{Kind: kind}
	meta := vocabulary.GetPredicateMetadata(predicate)
	if meta == nil {
		return presence
	}
	presence.Registered = true
	presence.Description = meta.Description
	if kind == predicateKindRelationship {
		role := string(meta.Role)
		inverse := meta.InverseOf
		presence.Role = &role
		presence.InverseOf = &inverse
	}
	return presence
}

// neighborMaxContentBytes bounds one query_neighbors result. It is a
// MODEL-FACING content cap in the same class as bashMaxOutputBytes (bash.go)
// and httpMaxTextSize (httprequest.go), and like both of those the number it
// bounds is the length of the string the model receives — len(Content) after
// marshalling, not the sum of the parts that went into it. It is NOT a
// prediction of the transport bound: a result under this cap that still trips
// the NATS payload limit takes the component's existing oversize path
// unchanged; the two compose.
//
// 64KB: the recorded failure this class exists for was a 102KB graph result
// mid-tier models retried three times
// (docs/concepts/24-tool-result-hints-and-pagination.md). A cap just under
// that leaves no headroom for the hint preamble the loop prepends, and a
// neighbor map is dense JSON the model must parse rather than stdout it skims.
const neighborMaxContentBytes = 64 * 1024

// queryNeighbors walks relationship edges out from one entity, bounded by
// depth and by the model-facing content budget.
func (e *GraphQueryExecutor) queryNeighbors(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	entityID, ok := call.Arguments["entity_id"].(string)
	if !ok || entityID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	depth := 1
	if d, ok := call.Arguments["depth"].(float64); ok && d >= 1 && d <= 3 {
		depth = int(d)
	}

	// filter_type shares entity_type's grammar, builder, and matcher. Before
	// this it compared a `type` key the graph authority never writes, so the
	// filter silently kept everything (proposal § Why, fifth finding).
	filterType := ""
	filterPattern := ""
	if ft, ok := call.Arguments["filter_type"].(string); ok {
		filterType = ft
	}
	if filterType != "" {
		pattern, err := buildTypePattern(filterType)
		if err != nil {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("filter_type %q is not usable: %v", filterType, err),
				ErrorKind: agentic.ToolErrorInvalidArgs,
			}, nil
		}
		filterPattern = pattern
	}

	walk := &neighborWalk{
		executor: e,
		sourceID: entityID,
		pattern:  filterPattern,
	}
	if failure := walk.run(ctx, call, depth); failure != nil {
		return failure.result, failure.err
	}
	// An absent START entity is not an empty neighborhood. Before this change
	// the traversal skipped the failed read and answered `count: 0`, which was
	// merely uninformative; classifying that same zero as HintEmpty would make
	// it actively wrong — "try a broader filter" for an entity that does not
	// exist. It takes the not-found classification query_entity and
	// query_relationships already give the same input.
	if walk.sourceMissing {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("entity not found: %s", entityID),
			ErrorKind: agentic.ToolErrorNotFound,
		}, nil
	}

	// The renderer reads the walk's CURRENT state, so the trim below can
	// re-render a smaller set whose truncated / frontier_remaining / count
	// describe that smaller set rather than the one before the trim.
	render := func() ([]byte, error) {
		response := map[string]any{
			"source_entity":      entityID,
			"neighbors":          walk.neighbors,
			"count":              len(walk.neighbors),
			"depth":              depth,
			"unresolved":         walk.unresolved,
			"truncated":          walk.truncated,
			"frontier_remaining": walk.frontierRemaining,
		}
		if filterType != "" {
			response["filter_type"] = filterType
			response["pattern"] = filterPattern
		}
		return json.MarshalIndent(response, "", "  ")
	}

	content, err := walk.fitEmitted(render)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to marshal response: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	result := agentic.ToolResult{
		CallID:  call.ID,
		Content: string(content),
		// Deliberately NO has_more here, and query_neighbors does not declare
		// Paginated. A BFS frontier is not a resumable position and this
		// executor holds no server-side traversal state, so a continuation
		// flag would make the loop render "pass the continuation token from
		// this call's metadata" for a token that does not exist. Width is
		// reported through truncated + frontier_remaining instead, and the
		// caller narrows with depth or filter_type.
		Metadata: map[string]any{
			"neighbor_count":     len(walk.neighbors),
			"depth":              depth,
			"unresolved_count":   len(walk.unresolved),
			"frontier_remaining": walk.frontierRemaining,
		},
	}
	switch {
	// An over-budget body is ALWAYS signalled, whether or not anything was
	// given back. fitEmitted returns early when there is nothing left to give
	// back, which happens when no record was ever admitted — every target
	// unresolved, so the envelope alone exceeds the cap. That path sets
	// neither truncated (nothing was given back) nor HintEmpty (unresolved is
	// non-empty and the split above is right to decline), and it would
	// otherwise hand the model an over-cap result reported as fine: the exact
	// failure the budget exists to prevent, at a rarer input. The residual is
	// that the unresolved list is unbounded — narrowing it is a model-facing
	// decision no ruling covers — but the caller is never told the body fits
	// when it does not.
	case len(content) > neighborMaxContentBytes:
		result.ResultHint = agentic.HintTooLarge
	case walk.truncated:
		result.ResultHint = agentic.HintTooLarge
	case len(walk.neighbors) == 0 && len(walk.unresolved) == 0:
		// Empty means the neighborhood is empty, not that its members could
		// not be read. A walk whose every target exists as an edge but is
		// absent from ENTITY_STATES answers zero neighbors WITH an unresolved
		// list, and HintEmpty over that would tell the model "nothing here,
		// broaden your filter" when the truth is "the targets are not
		// resident" — collapsing the three-way split (present / not resident /
		// genuinely empty) this tool exists to keep apart.
		result.ResultHint = agentic.HintEmpty
	}
	return result, nil
}

// neighborWalk carries the traversal state for one query_neighbors call. It is
// per-call scratch, constructed and discarded inside the executor method, so
// it holds no context and outlives nothing.
type neighborWalk struct {
	executor *GraphQueryExecutor
	sourceID string
	pattern  string

	neighbors map[string]json.RawMessage
	// admitted is the admission ORDER of the keys in neighbors, which a map
	// cannot carry. The emitted-size trim drops from its tail, so which
	// records survive an over-budget result is deterministic (breadth-first,
	// nearest first) rather than whatever the map iterator happened to yield.
	admitted   []string
	unresolved []string
	// truncated and frontierRemaining move together: the walk only stops early
	// when a record it wanted did not fit, so a truncated result always names
	// at least one identity it did not expand.
	truncated bool
	// pending is every identity the caller is still owed — the frontier the
	// walk did not reach, plus anything the emitted-size trim gave back.
	// frontierRemaining is its cardinality, so the two cannot drift.
	pending           map[string]bool
	frontierRemaining int
	// sourceMissing records that the START entity itself was absent, which is
	// a not-found answer rather than an empty neighborhood.
	sourceMissing bool

	visited    map[string]bool
	bytesTaken int
}

func (w *neighborWalk) run(ctx context.Context, call agentic.ToolCall, depth int) *entityReadFailure {
	w.neighbors = make(map[string]json.RawMessage)
	w.unresolved = []string{}
	w.visited = make(map[string]bool)
	w.pending = make(map[string]bool)

	// The source occupies ring 0 and is never itself a neighbor, so a walk of
	// `depth` hops needs depth+1 rings: ring 0 reads the START entity and
	// queues its targets, ring N reads the entities N hops out. Looping
	// `hop < depth` spent the whole budget on the seeding ring at the
	// advertised default and returned an EMPTY neighbor map for an entity
	// whose neighbors were right there — silent before this change, and a
	// confident HintEmpty after it, which is why it is fixed here rather
	// than left (owner ruling 2026-09-09, #1261).
	frontier := []string{w.sourceID}
	for ring := 0; ring <= depth && len(frontier) > 0; ring++ {
		var next []string
		for index, id := range frontier {
			if w.visited[id] {
				continue
			}
			w.visited[id] = true

			entity, raw, failure := w.executor.readNeighborRecord(ctx, call, id)
			if failure != nil {
				return failure
			}
			if entity == nil {
				// The START entity being absent is a not-found answer, not an
				// empty neighborhood; the caller turns this flag into one.
				if id == w.sourceID {
					w.sourceMissing = true
					return nil
				}
				// A TARGET being absent is reported, never dropped. A silent
				// omission reads to the model as "this edge does not exist"
				// when the truth is "its target is not resident".
				w.unresolved = append(w.unresolved, id)
				continue
			}

			if id != w.sourceID {
				matched, matchErr := w.matchesFilter(id)
				if matchErr != nil {
					return &entityReadFailure{
						result: agentic.ToolResult{
							CallID:    call.ID,
							Error:     fmt.Sprintf("cannot match neighbor %q against filter_type: %v", id, matchErr),
							ErrorKind: agentic.ToolErrorInternal,
						},
						err: matchErr,
					}
				}
				// A filtered-out neighbor is not RETURNED, but the walk still
				// expands through it: filter_type narrows the answer, it does
				// not shorten the graph. It also costs no budget, because
				// nothing was stored.
				if matched && !w.admit(id, raw) {
					w.stopAt(id, frontier[index+1:], next)
					return nil
				}
			}

			for _, row := range outgoingRelationships(entity, "") {
				if row.Target != "" && !w.visited[row.Target] {
					next = append(next, row.Target)
				}
			}
		}
		frontier = next
	}
	return nil
}

// admit adds one neighbor record if the raw bytes so far are still under the
// cap. This is the walk's READ bound, not the contract: the emitted result is
// indented JSON, which is strictly larger than the compact records it embeds,
// so a raw sum over the cap guarantees an emitted result over it too. That
// makes this a sound early stop — it never withholds a record the emitted
// measurement would have kept — while the authoritative check stays on the
// string that actually reaches the model (fitEmitted).
func (w *neighborWalk) admit(id string, raw []byte) bool {
	if w.bytesTaken+len(raw) > neighborMaxContentBytes {
		return false
	}
	w.bytesTaken += len(raw)
	w.neighbors[id] = json.RawMessage(raw)
	w.admitted = append(w.admitted, id)
	return true
}

// fitEmitted renders the response and, while the EMITTED string is over the
// model-facing budget, gives back the most recently admitted neighbor and
// renders again.
//
// Metering the assembled raw bytes instead would meter a proxy: the result
// ships as json.MarshalIndent, which re-indents every embedded record, and a
// record set measured at 64,740 raw bytes emitted 96,658 — 47% over a cap the
// caller was told it was under. The house rule is to observe the outcome
// rather than predict it, so the number checked here is len(content) itself.
//
// render reads the walk's CURRENT state on every call, so truncated,
// frontier_remaining and count in the rendered body always describe the set
// being measured.
//
// The loop is bounded by the number of admitted records, which admit's raw
// budget already caps: an ENTITY_STATES record carries an identity, a message
// type and its triples, so 64KB of them is a few hundred at most. Re-rendering
// beats predicting how many to drop, because the drop count depends on the
// indentation of the specific records being given back.
//
// Residual, deliberately not policy here: if the envelope alone — a very wide
// unresolved list — exceeds the cap, the loop runs out of neighbors to give
// back and returns an over-budget body flagged truncated. Bounding unresolved
// is a separate model-facing decision this change was not ruled on.
func (w *neighborWalk) fitEmitted(render func() ([]byte, error)) ([]byte, error) {
	for {
		content, err := render()
		if err != nil {
			return nil, err
		}
		if len(content) <= neighborMaxContentBytes || len(w.admitted) == 0 {
			return content, nil
		}
		w.giveBackLastAdmitted()
	}
}

// giveBackLastAdmitted removes the newest neighbor from the answer and moves
// it into the pending set, so a record dropped for emitted size is reported
// exactly like one the walk never reached.
func (w *neighborWalk) giveBackLastAdmitted() {
	last := w.admitted[len(w.admitted)-1]
	w.admitted = w.admitted[:len(w.admitted)-1]
	w.bytesTaken -= len(w.neighbors[last])
	delete(w.neighbors, last)
	w.truncated = true
	w.pending[last] = true
	w.frontierRemaining = len(w.pending)
}

// stopAt records the budget stop. stoppedOn is the identity whose record did
// not fit — it was read but neither returned nor expanded, so it is the first
// thing still owed; the rest of the current hop and everything already queued
// for the next one follow it. Counting deduplicates, so frontier_remaining is
// a number of distinct identities the model can act on rather than a bare
// flag, and it is >= 1 by construction, which is what makes
// `truncated <=> frontier_remaining > 0` hold.
func (w *neighborWalk) stopAt(stoppedOn string, remainingFrontier, nextFrontier []string) {
	w.truncated = true
	w.pending[stoppedOn] = true
	for _, id := range append(append([]string{}, remainingFrontier...), nextFrontier...) {
		if !w.visited[id] {
			w.pending[id] = true
		}
	}
	w.frontierRemaining = len(w.pending)
}

// readNeighborRecord reads one record for the traversal. Absence is a
// (nil, nil, nil) answer the caller records as unresolved; every other failure
// class — transient read, unreadable authoritative state — fails the whole
// call, matching query_entity. Before this, EVERY read failure was skipped
// with a bare `continue`, so a transient NATS error produced a smaller graph
// reported as complete.
func (e *GraphQueryExecutor) readNeighborRecord(
	ctx context.Context, call agentic.ToolCall, id string,
) (*graph.EntityState, []byte, *entityReadFailure) {
	entry, err := e.kvGetter.Get(ctx, id)
	if err != nil {
		if isKeyNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, &entityReadFailure{
			result: agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("failed to read neighbor %s: %v", id, err),
				ErrorKind: agentic.ToolErrorNetwork,
			},
			err: errs.WrapTransient(err, "GraphQueryExecutor", "queryNeighbors", "get entity from KV"),
		}
	}
	raw := entry.Value()
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(raw, &entity); err != nil {
		result, stateErr := graphStateToolFailure(call.ID, err)
		return nil, nil, &entityReadFailure{result: result, err: stateErr}
	}
	return &entity, raw, nil
}

// matchesFilter applies filter_type to one neighbor identity through the same
// exact six-position matcher query_by_type uses. An empty pattern keeps
// everything.
func (w *neighborWalk) matchesFilter(id string) (bool, error) {
	if w.pattern == "" {
		return true, nil
	}
	return semtypes.MatchEntityIDPattern(w.pattern, id)
}

func validateAuthoritativeEntity(data []byte) error {
	var entity graph.EntityState
	return graph.UnmarshalEntityState(data, &entity)
}

func decodeAuthoritativeEntityData(data []byte) (map[string]any, error) {
	if err := validateAuthoritativeEntity(data); err != nil {
		return nil, err
	}
	var entityData map[string]any
	if err := json.Unmarshal(data, &entityData); err != nil {
		return nil, err
	}
	return entityData, nil
}

func graphStateToolFailure(callID string, err error) (agentic.ToolResult, error) {
	return agentic.ToolResult{
		CallID:    callID,
		Error:     fmt.Sprintf("graph state reset required: %v", err),
		ErrorKind: agentic.ToolErrorInternal,
	}, err
}

// entityReadFailure carries a classified read failure as the pair the executor
// contract returns: the model-facing result AND the error beside it. It exists
// so a helper can refuse on behalf of its caller without the caller having to
// re-derive the classification.
type entityReadFailure struct {
	result agentic.ToolResult
	err    error
}

// isKeyNotFound recognises an absent key across both spellings this executor
// has always accepted: the package sentinel and the raw NATS string that
// reaches it when a binding did not map the store's own error.
func isKeyNotFound(err error) bool {
	return errors.Is(err, ErrKeyNotFound) || err.Error() == "nats: key not found"
}

// readEntityState fetches one entity and decodes it through the authoritative
// contract decoder. Absence is ToolErrorNotFound, a transient failure is
// ToolErrorNetwork, and unreadable authoritative state takes the reset path —
// the same three classifications query_entity has always made.
func (e *GraphQueryExecutor) readEntityState(
	ctx context.Context, call agentic.ToolCall, entityID, operation string,
) (*graph.EntityState, *entityReadFailure) {
	entry, err := e.kvGetter.Get(ctx, entityID)
	if err != nil {
		if isKeyNotFound(err) {
			return nil, &entityReadFailure{result: agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("entity not found: %s", entityID),
				ErrorKind: agentic.ToolErrorNotFound,
			}}
		}
		return nil, &entityReadFailure{
			result: agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("failed to query entity: %v", err),
				ErrorKind: agentic.ToolErrorNetwork,
			},
			err: errs.WrapTransient(err, "GraphQueryExecutor", operation, "get entity from KV"),
		}
	}
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &entity); err != nil {
		result, stateErr := graphStateToolFailure(call.ID, err)
		return nil, &entityReadFailure{result: result, err: stateErr}
	}
	return &entity, nil
}

// typeSegmentIndex is the ADR-102 position of the `type` segment inside a
// six-part entity ID (org.platform.system.domain.type.instance).
const typeSegmentIndex = 4

// entityTypeMaxTokens is the arity bound on entity_type / filter_type: the
// type segment, optionally preceded by domain, optionally preceded by system.
const entityTypeMaxTokens = 3

// errEntityTypeWildcard is the refusal a caller-supplied wildcard earns.
// ValidateEntityIDPattern ACCEPTS "*" at any position — that is its job — so a
// bare "*" in entity_type would build a pattern that validates and lists the
// whole bucket. The rejection has to happen on the caller's tokens, before the
// pattern is assembled.
var errEntityTypeWildcard = errors.New("entity type segments are literal; wildcards are not accepted")

// buildTypePattern turns one to three RIGHT-ANCHORED canonical segments into
// the exact six-position entity-ID pattern that selects them, and validates
// the result through the canonical pattern contract.
//
//	temperature                    -> *.*.*.*.temperature.*
//	environmental.temperature      -> *.*.*.environmental.temperature.*
//	gcs.environmental.temperature  -> *.*.gcs.environmental.temperature.*
//
// One builder serves entity_type and filter_type, and the matcher on both is
// pkg/types.MatchEntityIDPattern. graph-query's own type selector
// (graphrag.filterEntityIDsByType, ADR-071) is deliberately NOT reused: it
// folds case and widens to the unfiltered input when a non-empty set filters
// to empty, which is the right recall choice for a classifier guess over a
// semantic hit list and exactly the silent substitution a model-requested
// selection must never make.
func buildTypePattern(entityType string) (string, error) {
	if entityType == "" {
		return "", errors.New("entity type is empty")
	}
	tokens := strings.Split(entityType, ".")
	if len(tokens) > entityTypeMaxTokens {
		return "", fmt.Errorf("entity type has %d segments; at most %d are accepted (system.domain.type)",
			len(tokens), entityTypeMaxTokens)
	}
	parts := []string{"*", "*", "*", "*", "*", "*"}
	for offset, token := range tokens {
		if strings.ContainsAny(token, "*>") {
			return "", errEntityTypeWildcard
		}
		parts[typeSegmentIndex-(len(tokens)-1-offset)] = token
	}
	pattern := strings.Join(parts, ".")
	if err := semtypes.ValidateEntityIDPattern(pattern); err != nil {
		return "", err
	}
	return pattern, nil
}

// queryByType lists the identities whose entity ID carries the requested type
// segment, through the catalog reader's filtered key listing.
func (e *GraphQueryExecutor) queryByType(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	entityType, ok := call.Arguments["entity_type"].(string)
	if !ok || entityType == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_type is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	limit := 10
	if l, ok := call.Arguments["limit"].(float64); ok && l >= 1 && l <= 100 {
		limit = int(l)
	}

	pattern, err := buildTypePattern(entityType)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("entity_type %q is not usable: %v", entityType, err),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Both argument checks complete BEFORE the listing: a cursor this tool did
	// not issue is never quietly reset to page 1, which would page the model
	// over page 1 forever, and a rejected entity_type never costs a full key
	// scan.
	cursorKey := ""
	if raw, present := call.Arguments["cursor"]; present {
		cursor, isString := raw.(string)
		if !isString {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     "cursor must be a string",
				ErrorKind: agentic.ToolErrorInvalidArgs,
			}, nil
		}
		decoded, decodeErr := graph.DecodeCursor(cursor)
		if decodeErr != nil {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("cursor is not a token this tool issued: %v", decodeErr),
				ErrorKind: agentic.ToolErrorInvalidArgs,
			}, nil
		}
		// Decoding is NOT validation. graph.DecodeCursor is
		// base64.RawURLEncoding.DecodeString and nothing else
		// (graph/query_prefix_types.go), so without this check any
		// accidentally-valid base64 becomes a keyset position: "MQ" decodes to
		// "1", which sorts before every canonical key and would hand back page
		// one with a fresh next_cursor forever — the silent reset this
		// argument order exists to prevent. The position is a key in
		// ENTITY_STATES, and a key in that bucket that is not a canonical
		// six-part identity is authoritative-state corruption this tool
		// already refuses on the listing side, so "decodes to a canonical
		// entity ID" is exactly the set of tokens this tool issues.
		// An EMPTY cursor is the first page, not a bad token: graph.DecodeCursor
		// returns ("", nil) for it by documented contract, the advertised
		// description says "omit it for the first page", and this executor
		// already reads an empty `direction` and an empty `relationship_type`
		// as omitted. Validating "" would make `cursor` the one optional
		// string here that a caller cannot send empty — and would refuse the
		// FIRST page while telling the model its cursor "decodes to \"\"".
		if decoded != "" {
			if validateErr := semtypes.ValidateEntityID(decoded); validateErr != nil {
				return agentic.ToolResult{
					CallID: call.ID,
					Error: fmt.Sprintf(
						"cursor is not a token this tool issued: it decodes to %q, which is not a canonical entity ID: %v",
						decoded, validateErr),
					ErrorKind: agentic.ToolErrorInvalidArgs,
				}, nil
			}
		}
		cursorKey = decoded
	}

	lister, ok := e.kvGetter.(KVKeyLister)
	if !ok {
		// Loud, never an empty listing: a binding without key listing cannot
		// answer this question, and reporting zero matches would be a
		// positive signal ("nothing of that type exists") the tool never
		// established.
		err := fmt.Errorf("query_by_type requires a KV binding implementing KVKeyLister; %T does not", e.kvGetter)
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapFatal(err, "GraphQueryExecutor", "queryByType", "resolve key lister")
	}

	keys, err := lister.KeysByPattern(ctx, pattern)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to list entity keys: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "GraphQueryExecutor", "queryByType", "list keys by pattern")
	}

	matched := make([]string, 0, len(keys))
	for _, key := range keys {
		hit, matchErr := semtypes.MatchEntityIDPattern(pattern, key)
		if matchErr != nil {
			// A key in ENTITY_STATES that is not a canonical entity ID is the
			// same authoritative-state failure query_entity refuses on an
			// unreadable record, and it takes the same path. Skipping it
			// would be a silent drop from a set reported as complete.
			return graphStateToolFailure(call.ID, matchErr)
		}
		if hit {
			matched = append(matched, key)
		}
	}

	// MANDATORY: sort before the cursor is applied. natsclient.FilteredKeys
	// appends in channel-arrival order and sorts nothing
	// (natsclient/kv.go collectFilteredKeys), so the deterministic order the
	// cursor rests on is ours to establish — the same step the prefix
	// responder performs (processor/graph-ingest/query.go).
	sort.Strings(matched)

	remaining := matched
	if cursorKey != "" {
		index := sort.SearchStrings(remaining, cursorKey)
		for index < len(remaining) && remaining[index] == cursorKey {
			index++
		}
		remaining = remaining[index:]
	}

	page := remaining
	if len(page) > limit {
		page = page[:limit]
	}
	hasMore := len(page) < len(remaining)

	response := map[string]any{
		"entity_type": entityType,
		"pattern":     pattern,
		"limit":       limit,
		"matched":     len(matched),
		"entity_ids":  page,
		"count":       len(page),
	}
	content, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to marshal response: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, nil
	}

	metadata := map[string]any{
		"entity_type": entityType,
		"limit":       limit,
		// has_more is set on EVERY successful result, false included: the
		// pagination contract calls its absence on a Paginated tool a
		// violation worth a Warn log (agentic/tools.go MetadataKeyHasMore).
		agentic.MetadataKeyHasMore: hasMore,
	}
	hint := agentic.ToolResultHint("")
	if hasMore {
		// Opaque, and encoded by the graph package's own cursor codec so one
		// format serves keyset continuation over ENTITY_STATES.
		metadata[agentic.MetadataKeyNextCursor] = graph.EncodeCursor(page[len(page)-1])
		// too_large composes with the cursor rather than competing with it:
		// the model is told both to narrow and that it may continue.
		hint = agentic.HintTooLarge
	}
	if len(matched) == 0 {
		hint = agentic.HintEmpty
	}
	return agentic.ToolResult{
		CallID:     call.ID,
		Content:    string(content),
		Metadata:   metadata,
		ResultHint: hint,
	}, nil
}

// JetStreamKVAdapter adapts a jetstream.KeyValue to our KVGetter interface.
type JetStreamKVAdapter struct {
	kv interface {
		Get(ctx context.Context, key string) (interface {
			Value() []byte
			Revision() uint64
		}, error)
	}
}

// NewJetStreamKVAdapter creates a new adapter for jetstream.KeyValue.
// Usage: NewJetStreamKVAdapter(kvBucket) where kvBucket is a jetstream.KeyValue
func NewJetStreamKVAdapter(kv any) *JetStreamKVAdapter {
	return &JetStreamKVAdapter{kv: kv.(interface {
		Get(ctx context.Context, key string) (interface {
			Value() []byte
			Revision() uint64
		}, error)
	})}
}

// Get implements KVGetter.
func (a *JetStreamKVAdapter) Get(ctx context.Context, key string) (KVEntry, error) {
	entry, err := a.kv.Get(ctx, key)
	if err != nil {
		// Convert jetstream not found error to our error
		if err.Error() == "nats: key not found" {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	return &kvEntryAdapter{entry: entry}, nil
}

type kvEntryAdapter struct {
	entry interface {
		Value() []byte
		Revision() uint64
	}
}

func (e *kvEntryAdapter) Value() []byte    { return e.entry.Value() }
func (e *kvEntryAdapter) Revision() uint64 { return e.entry.Revision() }
