package graphql

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/pkg/graphinterfaces"
	"github.com/c360/semstreams/processor/graph/querymanager"
)

// MetricsRecorder interface for recording GraphQL operation metrics
type MetricsRecorder interface {
	RecordMetrics(ctx context.Context, operation string, fn func() error) error
}

// BaseResolver provides generic resolver methods for GraphQL queries
// This is the foundation that Phase 2 will build domain-specific resolvers on
//
// Backend Strategy:
//   - Prefers QueryManager when available (multi-tier caching, direct access)
//   - Falls back to NATSClient for remote queries
type BaseResolver struct {
	queryManager    querymanager.Querier // Preferred: direct access with caching
	natsClient      *NATSClient          // Fallback: remote NATS queries
	metricsRecorder MetricsRecorder      // Optional metrics recording
}

// NewBaseResolver creates a new base resolver with QueryManager backend
func NewBaseResolver(queryManager querymanager.Querier, metricsRecorder MetricsRecorder) *BaseResolver {
	return &BaseResolver{
		queryManager:    queryManager,
		metricsRecorder: metricsRecorder,
	}
}

// NewBaseResolverWithNATS creates a base resolver with NATS backend (legacy)
func NewBaseResolverWithNATS(natsClient *NATSClient, metricsRecorder MetricsRecorder) *BaseResolver {
	return &BaseResolver{
		natsClient:      natsClient,
		metricsRecorder: metricsRecorder,
	}
}

// QueryEntityByID queries a single entity by ID
// Returns nil if entity not found (GraphQL null)
func (r *BaseResolver) QueryEntityByID(ctx context.Context, id string) (*Entity, error) {
	var entity *Entity
	var err error

	queryFn := func() error {
		// Prefer QueryManager (cached, direct access)
		if r.queryManager != nil {
			entityState, qErr := r.queryManager.GetEntity(ctx, id)
			if qErr != nil {
				return qErr
			}
			entity = convertEntityStateToGraphQL(entityState)
			return nil
		}

		// Fallback to NATS
		entity, err = r.natsClient.QueryEntityByID(ctx, id)
		return err
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "QueryEntityByID", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "QueryEntityByID")
	}
	return entity, nil
}

// QueryEntityByAlias queries a single entity by alias or ID
// Returns nil if entity not found (GraphQL null)
// This method supports alias resolution (e.g., "N42" → actual entity ID)
func (r *BaseResolver) QueryEntityByAlias(ctx context.Context, aliasOrID string) (*Entity, error) {
	var entity *Entity
	var err error

	queryFn := func() error {
		// Prefer QueryManager (cached, direct access with alias support)
		if r.queryManager != nil {
			entityState, qErr := r.queryManager.GetEntityByAlias(ctx, aliasOrID)
			if qErr != nil {
				return qErr
			}
			entity = convertEntityStateToGraphQL(entityState)
			return nil
		}

		// Fallback: NATS doesn't have alias resolution built-in, try as ID
		entity, err = r.natsClient.QueryEntityByID(ctx, aliasOrID)
		return err
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "QueryEntityByAlias", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "QueryEntityByAlias")
	}
	return entity, nil
}

// QueryEntitiesByIDs queries multiple entities by their IDs (batch operation)
// This enables efficient data loading and prevents N+1 query problems
func (r *BaseResolver) QueryEntitiesByIDs(ctx context.Context, ids []string) ([]*Entity, error) {
	if len(ids) == 0 {
		return []*Entity{}, nil
	}

	var entities []*Entity
	var err error

	queryFn := func() error {
		// Prefer QueryManager (multi-tier caching)
		if r.queryManager != nil {
			entityStates, qErr := r.queryManager.GetEntities(ctx, ids)
			if qErr != nil {
				return qErr
			}
			entities = convertEntityStatesToGraphQL(entityStates)
			return nil
		}

		// Fallback to NATS
		entities, err = r.natsClient.QueryEntitiesByIDs(ctx, ids)
		return err
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "QueryEntitiesByIDs", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "QueryEntitiesByIDs")
	}
	return entities, nil
}

// QueryEntitiesByType queries all entities of a specific type
// This is useful for list queries (e.g., all specs, all docs, etc.)
// Limit parameter controls maximum number of entities returned (0 = no limit)
func (r *BaseResolver) QueryEntitiesByType(ctx context.Context, entityType string, limit int) ([]*Entity, error) {
	var entities []*Entity
	var err error

	queryFn := func() error {
		// Prefer QueryManager (uses predicate index)
		if r.queryManager != nil {
			// Query predicate index for entity IDs of this type
			entityIDs, qErr := r.queryManager.QueryByPredicate(ctx, entityType)
			if qErr != nil {
				return qErr
			}

			// Apply limit
			if limit > 0 && len(entityIDs) > limit {
				entityIDs = entityIDs[:limit]
			}

			// Batch load entities (with caching)
			entityStates, qErr := r.queryManager.GetEntities(ctx, entityIDs)
			if qErr != nil {
				return qErr
			}

			entities = convertEntityStatesToGraphQL(entityStates)
			return nil
		}

		// Fallback to NATS
		entities, err = r.natsClient.QueryEntitiesByType(ctx, entityType, limit)
		return err
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "QueryEntitiesByType", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "QueryEntitiesByType")
	}
	return entities, nil
}

// QueryRelationships queries relationships based on filters
func (r *BaseResolver) QueryRelationships(ctx context.Context, filters RelationshipFilters) ([]*Relationship, error) {
	var relationships []*Relationship
	var err error

	queryFn := func() error {
		// Prefer QueryManager (relationship index)
		if r.queryManager != nil {
			// Map direction
			direction := querymanager.DirectionOutgoing
			switch filters.Direction {
			case "incoming":
				direction = querymanager.DirectionIncoming
			case "both":
				direction = querymanager.DirectionBoth
			}

			// Query relationships
			rels, qErr := r.queryManager.QueryRelationships(ctx, filters.EntityID, direction)
			if qErr != nil {
				return qErr
			}

			// Convert and filter by edge types if specified
			relationships = make([]*Relationship, 0, len(rels))
			for _, rel := range rels {
				// Filter by edge types if specified
				if len(filters.EdgeTypes) > 0 {
					found := false
					for _, edgeType := range filters.EdgeTypes {
						if rel.EdgeType == edgeType {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				relationships = append(relationships, &Relationship{
					FromEntityID: rel.FromEntityID,
					ToEntityID:   rel.ToEntityID,
					EdgeType:     rel.EdgeType,
				})
			}
			return nil
		}

		// Fallback to NATS
		relationships, err = r.natsClient.QueryRelationships(ctx, filters)
		return err
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "QueryRelationships", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "QueryRelationships")
	}
	return relationships, nil
}

// SemanticSearch performs semantic similarity search
func (r *BaseResolver) SemanticSearch(ctx context.Context, query string, limit int) ([]*SemanticSearchResult, error) {
	if limit <= 0 {
		limit = 10 // Default limit
	}
	if limit > 100 {
		limit = 100 // Max limit
	}

	var results []*SemanticSearchResult
	var err error

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "SemanticSearch", func() error {
			results, err = r.natsClient.SemanticSearch(ctx, query, limit)
			return err
		})
	} else {
		results, err = r.natsClient.SemanticSearch(ctx, query, limit)
	}

	if err != nil {
		return nil, wrapError(err, "SemanticSearch")
	}
	return results, nil
}

// Property Extraction Helpers
// These methods extract typed properties from the generic entity properties map
// Path supports dot notation for nested properties (e.g., "metadata.title")

// getProperty extracts a property value from an entity using path notation
// Returns nil if property doesn't exist
func getProperty(entity *Entity, path string) interface{} {
	if entity == nil || entity.Properties == nil {
		return nil
	}

	// Split path by dots for nested access
	parts := strings.Split(path, ".")
	var current interface{} = entity.Properties

	for _, part := range parts {
		// Try to access as map
		if m, ok := current.(map[string]interface{}); ok {
			val, exists := m[part]
			if !exists {
				return nil
			}
			current = val
		} else {
			// Not a map, can't traverse further
			return nil
		}
	}

	return current
}

// GetStringProp extracts a string property from entity
// Returns empty string if property doesn't exist or is wrong type
func GetStringProp(entity *Entity, path string) string {
	val := getProperty(entity, path)
	if val == nil {
		return ""
	}

	// Try direct string conversion
	if s, ok := val.(string); ok {
		return s
	}

	// Try converting other types to string
	return fmt.Sprintf("%v", val)
}

// GetIntProp extracts an int property from entity
// Returns 0 if property doesn't exist or is wrong type
func GetIntProp(entity *Entity, path string) int {
	val := getProperty(entity, path)
	if val == nil {
		return 0
	}

	// Try different numeric types
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		// Try parsing string as int
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}

	return 0
}

// GetFloatProp extracts a float64 property from entity
// Returns 0.0 if property doesn't exist or is wrong type
func GetFloatProp(entity *Entity, path string) float64 {
	val := getProperty(entity, path)
	if val == nil {
		return 0.0
	}

	// Try different numeric types
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case string:
		// Try parsing string as float
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}

	return 0.0
}

// GetBoolProp extracts a bool property from entity
// Returns false if property doesn't exist or is wrong type
func GetBoolProp(entity *Entity, path string) bool {
	val := getProperty(entity, path)
	if val == nil {
		return false
	}

	// Try direct bool conversion
	if b, ok := val.(bool); ok {
		return b
	}

	// Try string conversion
	if s, ok := val.(string); ok {
		// Common boolean string representations
		switch strings.ToLower(s) {
		case "true", "yes", "1", "t", "y":
			return true
		case "false", "no", "0", "f", "n":
			return false
		}
	}

	// Try numeric conversion (0 = false, non-zero = true)
	if n := GetIntProp(entity, path); n != 0 {
		return true
	}

	return false
}

// GetArrayProp extracts an array property from entity
// Returns empty slice if property doesn't exist or is wrong type
func GetArrayProp(entity *Entity, path string) []interface{} {
	val := getProperty(entity, path)
	if val == nil {
		return []interface{}{}
	}

	// Try direct slice conversion
	if arr, ok := val.([]interface{}); ok {
		return arr
	}

	return []interface{}{}
}

// GetStringArrayProp extracts a string array property from entity
// Returns empty slice if property doesn't exist or is wrong type
func GetStringArrayProp(entity *Entity, path string) []string {
	val := getProperty(entity, path)
	if val == nil {
		return []string{}
	}

	// Try direct []string conversion first
	if arr, ok := val.([]string); ok {
		return arr
	}

	// Try []interface{} conversion
	if arr, ok := val.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}

	return []string{}
}

// GetStringPtrProp extracts a string property and returns a pointer
// Returns nil if property doesn't exist or is empty
func GetStringPtrProp(entity *Entity, path string) *string {
	val := getProperty(entity, path)
	if val == nil {
		return nil
	}

	// Try string conversion
	if str, ok := val.(string); ok {
		if str == "" {
			return nil // Treat empty string as nil
		}
		return &str
	}

	return nil
}

// GetIntPtrProp extracts an int property and returns a pointer
// Returns nil if property doesn't exist or is zero
func GetIntPtrProp(entity *Entity, path string) *int {
	val := getProperty(entity, path)
	if val == nil {
		return nil
	}

	// Try different numeric types
	switch v := val.(type) {
	case int:
		if v == 0 {
			return nil // Treat zero as nil
		}
		return &v
	case float64:
		if v == 0 {
			return nil
		}
		i := int(v)
		return &i
	case int64:
		if v == 0 {
			return nil
		}
		i := int(v)
		return &i
	}

	return nil
}

// GetFloatPtrProp extracts a float64 property and returns a pointer
// Returns nil if property doesn't exist or is zero
func GetFloatPtrProp(entity *Entity, path string) *float64 {
	val := getProperty(entity, path)
	if val == nil {
		return nil
	}

	// Try different numeric types
	switch v := val.(type) {
	case float64:
		if v == 0 {
			return nil
		}
		return &v
	case int:
		if v == 0 {
			return nil
		}
		f := float64(v)
		return &f
	case int64:
		if v == 0 {
			return nil
		}
		f := float64(v)
		return &f
	}

	return nil
}

// GetBoolPtrProp extracts a bool property and returns a pointer
// Returns nil if property doesn't exist
func GetBoolPtrProp(entity *Entity, path string) *bool {
	val := getProperty(entity, path)
	if val == nil {
		return nil
	}

	// Try bool conversion
	if b, ok := val.(bool); ok {
		return &b
	}

	return nil
}

// GetMapProp extracts a map property from entity
// Returns empty map if property doesn't exist or is wrong type
func GetMapProp(entity *Entity, path string) map[string]interface{} {
	val := getProperty(entity, path)
	if val == nil {
		return map[string]interface{}{}
	}

	// Try direct map conversion
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}

	return map[string]interface{}{}
}

// HasProperty checks if an entity has a property at the given path
func HasProperty(entity *Entity, path string) bool {
	return getProperty(entity, path) != nil
}

// GetPropertyOrDefault extracts a property or returns a default value
func GetPropertyOrDefault(entity *Entity, path string, defaultValue interface{}) interface{} {
	val := getProperty(entity, path)
	if val == nil {
		return defaultValue
	}
	return val
}

// Converter helpers for EntityState → GraphQL Entity

// convertEntityStateToGraphQL converts a graph.EntityState to GraphQL Entity type
func convertEntityStateToGraphQL(state *graph.EntityState) *Entity {
	if state == nil {
		return nil
	}

	// Extract created_at from properties if available
	var createdAt time.Time
	if state.Node.Properties != nil {
		if ct, ok := state.Node.Properties["created_at"].(time.Time); ok {
			createdAt = ct
		}
	}

	return &Entity{
		ID:         state.Node.ID,
		Type:       state.Node.Type,
		Properties: state.Node.Properties,
		CreatedAt:  createdAt,
		UpdatedAt:  state.UpdatedAt,
	}
}

// convertEntityStatesToGraphQL converts multiple EntityStates to GraphQL Entities
func convertEntityStatesToGraphQL(states []*graph.EntityState) []*Entity {
	entities := make([]*Entity, 0, len(states))
	for _, state := range states {
		if entity := convertEntityStateToGraphQL(state); entity != nil {
			entities = append(entities, entity)
		}
	}
	return entities
}

// GraphRAG Community Search Resolvers

// LocalSearch performs a local search within an entity's community
// Returns entities from the same community as the specified entity
func (r *BaseResolver) LocalSearch(ctx context.Context, entityID string, query string, level int) (*LocalSearchResult, error) {
	var result *LocalSearchResult
	var err error

	queryFn := func() error {
		// LocalSearch requires QueryManager (not available via NATS)
		if r.queryManager == nil {
			return wrapError(fmt.Errorf("LocalSearch requires QueryManager backend"), "LocalSearch")
		}

		// Call QueryManager's LocalSearch
		qmResult, qErr := r.queryManager.LocalSearch(ctx, entityID, query, level)
		if qErr != nil {
			return qErr
		}

		// Convert to GraphQL type
		result = &LocalSearchResult{
			Entities:    convertEntityStatesToGraphQL(qmResult.Entities),
			CommunityID: qmResult.CommunityID,
			Count:       qmResult.Count,
		}
		return nil
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "LocalSearch", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "LocalSearch")
	}
	return result, nil
}

// GlobalSearch performs a global search across community summaries
// Finds top-N most relevant communities and searches within them
func (r *BaseResolver) GlobalSearch(ctx context.Context, query string, level int, maxCommunities int) (*GlobalSearchResult, error) {
	var result *GlobalSearchResult
	var err error

	queryFn := func() error {
		// GlobalSearch requires QueryManager (not available via NATS)
		if r.queryManager == nil {
			return wrapError(fmt.Errorf("GlobalSearch requires QueryManager backend"), "GlobalSearch")
		}

		// Call QueryManager's GlobalSearch
		qmResult, qErr := r.queryManager.GlobalSearch(ctx, query, level, maxCommunities)
		if qErr != nil {
			return qErr
		}

		// Convert to GraphQL type
		summaries := make([]CommunitySummary, len(qmResult.CommunitySummaries))
		for i, s := range qmResult.CommunitySummaries {
			summaries[i] = CommunitySummary{
				CommunityID: s.CommunityID,
				Summary:     s.Summary,
				Keywords:    s.Keywords,
				Level:       s.Level,
				Relevance:   s.Relevance,
			}
		}

		result = &GlobalSearchResult{
			Entities:           convertEntityStatesToGraphQL(qmResult.Entities),
			CommunitySummaries: summaries,
			Count:              qmResult.Count,
		}
		return nil
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "GlobalSearch", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "GlobalSearch")
	}
	return result, nil
}

// GetCommunity retrieves a community by ID
func (r *BaseResolver) GetCommunity(ctx context.Context, communityID string) (*Community, error) {
	var community *Community
	var err error

	queryFn := func() error {
		// GetCommunity requires QueryManager (not available via NATS)
		if r.queryManager == nil {
			return wrapError(fmt.Errorf("GetCommunity requires QueryManager backend"), "GetCommunity")
		}

		// Call QueryManager's GetCommunity
		comm, qErr := r.queryManager.GetCommunity(ctx, communityID)
		if qErr != nil {
			return qErr
		}

		community = convertCommunityToGraphQL(comm)
		return nil
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "GetCommunity", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "GetCommunity")
	}
	return community, nil
}

// GetEntityCommunity retrieves the community containing a specific entity
// Returns the community at the specified hierarchical level
func (r *BaseResolver) GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error) {
	var community *Community
	var err error

	queryFn := func() error {
		// GetEntityCommunity requires QueryManager (not available via NATS)
		if r.queryManager == nil {
			return wrapError(fmt.Errorf("GetEntityCommunity requires QueryManager backend"), "GetEntityCommunity")
		}

		// Call QueryManager's GetEntityCommunity
		comm, qErr := r.queryManager.GetEntityCommunity(ctx, entityID, level)
		if qErr != nil {
			return qErr
		}

		community = convertCommunityToGraphQL(comm)
		return nil
	}

	if r.metricsRecorder != nil {
		err = r.metricsRecorder.RecordMetrics(ctx, "GetEntityCommunity", queryFn)
	} else {
		err = queryFn()
	}

	if err != nil {
		return nil, wrapError(err, "GetEntityCommunity")
	}
	return community, nil
}

// convertCommunityToGraphQL converts a graphinterfaces.Community to GraphQL Community type
func convertCommunityToGraphQL(comm graphinterfaces.Community) *Community {
	if comm == nil {
		return nil
	}

	// Prefer LLM summary if available, fallback to statistical
	summary := comm.GetLLMSummary()
	if summary == "" {
		summary = comm.GetStatisticalSummary()
	}

	return &Community{
		ID:            comm.GetID(),
		Level:         comm.GetLevel(),
		Members:       comm.GetMembers(),
		Summary:       summary,
		Keywords:      comm.GetKeywords(),
		RepEntities:   comm.GetRepEntities(),
		SummaryStatus: comm.GetSummaryStatus(),
	}
}
