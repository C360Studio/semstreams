// Package executors provides tool executor implementations for the agentic-tools component.
package executors

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
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
			Name:        "query_relationships",
			Description: "Query relationships for an entity, optionally filtering by direction and type.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "The entity ID to query relationships for",
					},
					"direction": map[string]any{
						"type":        "string",
						"enum":        []string{"outgoing", "incoming", "both"},
						"description": "Direction of relationships to return (default: both)",
					},
					"relationship_type": map[string]any{
						"type":        "string",
						"description": "Optional filter for specific relationship type",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "query_neighbors",
			Description: "Query neighboring entities within N hops of a given entity.",
			Effect:      agentic.ToolEffectReadOnly,
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
						"description": "Optional filter for entity type",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "query_by_type",
			Description: "Query all entities of a specific type with optional limit.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_type": map[string]any{
						"type":        "string",
						"description": "The entity type to query (e.g., drone, sensor, mission)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of entities to return (default: 10, max: 100)",
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{"entity_type"},
			},
		},
	}
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
		// Check if it's a not found error
		if err == ErrKeyNotFound || err.Error() == "nats: key not found" {
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
			if err == ErrKeyNotFound || err.Error() == "nats: key not found" {
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

// queryRelationships queries relationships for an entity
func (e *GraphQueryExecutor) queryRelationships(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	entityID, ok := call.Arguments["entity_id"].(string)
	if !ok || entityID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "entity_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	direction := "both"
	if d, ok := call.Arguments["direction"].(string); ok && d != "" {
		direction = d
	}

	relType := ""
	if rt, ok := call.Arguments["relationship_type"].(string); ok {
		relType = rt
	}

	// Query the entity to find relationships in its stored data
	entry, err := e.kvGetter.Get(ctx, entityID)
	if err != nil {
		if err == ErrKeyNotFound || err.Error() == "nats: key not found" {
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
		}, errs.WrapTransient(err, "GraphQueryExecutor", "queryRelationships", "get entity from KV")
	}

	// Parse entity data to extract relationships
	entityData, err := decodeAuthoritativeEntityData(entry.Value())
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to parse entity data: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, err
	}

	// Extract relationships from entity data
	relationships := extractRelationships(entityID, entityData, direction, relType)

	response := map[string]any{
		"entity_id":     entityID,
		"relationships": relationships,
		"count":         len(relationships),
		"direction":     direction,
	}
	if relType != "" {
		response["filter_type"] = relType
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
			"relationship_count": len(relationships),
		},
	}, nil
}

// queryNeighbors queries entities within N hops
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

	filterType := ""
	if ft, ok := call.Arguments["filter_type"].(string); ok {
		filterType = ft
	}

	// Start with the source entity
	visited := make(map[string]bool)
	neighbors := make(map[string]json.RawMessage)
	frontier := []string{entityID}

	for d := 0; d < depth && len(frontier) > 0; d++ {
		nextFrontier := []string{}

		for _, id := range frontier {
			if visited[id] {
				continue
			}
			visited[id] = true

			entry, err := e.kvGetter.Get(ctx, id)
			if err != nil {
				continue
			}

			entityData, decodeErr := decodeAuthoritativeEntityData(entry.Value())
			if decodeErr != nil {
				return graphStateToolFailure(call.ID, decodeErr)
			}

			// Store in neighbors (skip source entity)
			if id != entityID {
				// Apply type filter if specified
				if filterType != "" {
					if entityType, ok := entityData["type"].(string); ok && entityType != filterType {
						continue
					}
				}
				neighbors[id] = json.RawMessage(entry.Value())
			}

			// Find connected entities for next hop
			for _, rel := range extractRelationships(id, entityData, "both", "") {
				if relMap, ok := rel.(map[string]any); ok {
					if target, ok := relMap["target"].(string); ok && !visited[target] {
						nextFrontier = append(nextFrontier, target)
					}
				}
			}
		}

		frontier = nextFrontier
	}

	response := map[string]any{
		"source_entity": entityID,
		"neighbors":     neighbors,
		"count":         len(neighbors),
		"depth":         depth,
	}
	if filterType != "" {
		response["filter_type"] = filterType
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
			"neighbor_count": len(neighbors),
			"depth":          depth,
		},
	}, nil
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

// queryByType queries entities by type (placeholder - requires index)
func (e *GraphQueryExecutor) queryByType(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
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

	// Note: This requires a type index to be efficient. For now, return an informational response.
	// In production, this would query an index like "ENTITY_TYPE_INDEX.{type}" bucket.
	response := map[string]any{
		"entity_type":   entityType,
		"limit":         limit,
		"entities":      []any{},
		"count":         0,
		"note":          "Type-based queries require entity type index. Use query_entity or query_entities with known IDs.",
		"suggested_ids": []string{},
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
			"entity_type": entityType,
			"limit":       limit,
		},
	}, nil
}

// extractRelationships extracts relationship data from an entity's stored JSON
func extractRelationships(entityID string, entityData map[string]any, direction, relType string) []any {
	var relationships []any

	// Check for explicit relationships field
	if rels, ok := entityData["relationships"].([]interface{}); ok {
		for _, rel := range rels {
			if relMap, ok := rel.(map[string]any); ok {
				// Apply type filter
				if relType != "" {
					if t, ok := relMap["type"].(string); ok && t != relType {
						continue
					}
				}
				// Apply direction filter
				if direction == "outgoing" {
					if source, ok := relMap["source"].(string); ok && source != entityID {
						continue
					}
				} else if direction == "incoming" {
					if target, ok := relMap["target"].(string); ok && target != entityID {
						continue
					}
				}
				relationships = append(relationships, rel)
			}
		}
	}

	// Check for triples (subject-predicate-object)
	if triples, ok := entityData["triples"].([]interface{}); ok {
		for _, triple := range triples {
			if tripleMap, ok := triple.(map[string]any); ok {
				predicate, _ := tripleMap["predicate"].(string)
				if relType != "" && predicate != relType {
					continue
				}

				subject, _ := tripleMap["subject"].(string)
				object, _ := tripleMap["object"].(string)

				rel := map[string]any{
					"type":   predicate,
					"source": subject,
					"target": object,
				}

				if direction == "outgoing" && subject != entityID {
					continue
				}
				if direction == "incoming" && object != entityID {
					continue
				}

				relationships = append(relationships, rel)
			}
		}
	}

	return relationships
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
