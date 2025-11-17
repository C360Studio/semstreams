package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/natsclient"
)

// NATSClient provides NATS integration for GraphQL gateway
// It wraps the core NATS client with GraphQL-specific methods
type NATSClient struct {
	client   *natsclient.Client
	subjects NATSSubjectsConfig
	timeout  time.Duration
}

// NewNATSClient creates a new NATS client wrapper for GraphQL operations
func NewNATSClient(client *natsclient.Client, subjects NATSSubjectsConfig, timeout time.Duration) *NATSClient {
	return &NATSClient{
		client:   client,
		subjects: subjects,
		timeout:  timeout,
	}
}

// Entity represents a generic entity from the graph
// This is a generic structure - Phase 2 will generate domain-specific types
type Entity struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"created_at,omitempty"`
	UpdatedAt  time.Time              `json:"updated_at,omitempty"`
}

// Relationship represents a generic relationship between entities
type Relationship struct {
	FromEntityID string                 `json:"from_entity_id"`
	ToEntityID   string                 `json:"to_entity_id"`
	EdgeType     string                 `json:"edge_type"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
	CreatedAt    time.Time              `json:"created_at,omitempty"`
}

// RelationshipFilters defines filters for relationship queries
type RelationshipFilters struct {
	EntityID  string   `json:"entity_id"`
	Direction string   `json:"direction"` // "outgoing", "incoming", "both"
	EdgeTypes []string `json:"edge_types,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// SemanticSearchResult represents a semantic search result
type SemanticSearchResult struct {
	Entity *Entity `json:"entity"`
	Score  float64 `json:"score"`
}

// Community represents a community/cluster in the graph
type Community struct {
	ID            string   `json:"id"`
	Level         int      `json:"level"`
	Members       []string `json:"members"`
	Summary       string   `json:"summary,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
	RepEntities   []string `json:"rep_entities,omitempty"`
	Summarizer    string   `json:"summarizer,omitempty"`
	SummaryStatus string   `json:"summary_status,omitempty"`
}

// LocalSearchResult represents the result of a local community search
type LocalSearchResult struct {
	Entities    []*Entity `json:"entities"`
	CommunityID string    `json:"community_id"`
	Count       int       `json:"count"`
}

// GlobalSearchResult represents the result of a global cross-community search
type GlobalSearchResult struct {
	Entities           []*Entity          `json:"entities"`
	CommunitySummaries []CommunitySummary `json:"community_summaries"`
	Count              int                `json:"count"`
}

// CommunitySummary represents a community's summary with relevance score
type CommunitySummary struct {
	CommunityID string   `json:"community_id"`
	Summary     string   `json:"summary"`
	Keywords    []string `json:"keywords"`
	Level       int      `json:"level"`
	Relevance   float64  `json:"relevance"`
}

// QueryEntityByID queries a single entity by ID via NATS
func (nc *NATSClient) QueryEntityByID(ctx context.Context, id string) (*Entity, error) {
	// Create request
	req := map[string]interface{}{
		"entity_id": id,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryEntityByID", "marshal request")
	}

	// Determine timeout from context or use default
	timeout := nc.getTimeout(ctx)

	// Send NATS request
	conn := nc.client.GetConnection()
	if conn == nil {
		return nil, errors.WrapTransient(errors.ErrNoConnection, "NATSClient", "QueryEntityByID",
			"NATS connection not available")
	}

	msg, err := conn.Request(nc.subjects.EntityQuery, reqData, timeout)
	if err != nil {
		return nil, errors.WrapTransient(err, "NATSClient", "QueryEntityByID",
			fmt.Sprintf("NATS request to %s failed", nc.subjects.EntityQuery))
	}

	// Parse response
	var response struct {
		Entity *Entity `json:"entity"`
		Error  string  `json:"error,omitempty"`
	}

	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryEntityByID", "unmarshal response")
	}

	if response.Error != "" {
		return nil, errors.WrapInvalid(
			fmt.Errorf("%s", response.Error),
			"NATSClient",
			"QueryEntityByID",
			"remote service error")
	}

	return response.Entity, nil
}

// QueryEntitiesByIDs queries multiple entities by their IDs via NATS (batch operation)
func (nc *NATSClient) QueryEntitiesByIDs(ctx context.Context, ids []string) ([]*Entity, error) {
	if len(ids) == 0 {
		return []*Entity{}, nil
	}

	// Create request
	req := map[string]interface{}{
		"entity_ids": ids,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryEntitiesByIDs", "marshal request")
	}

	// Determine timeout from context or use default
	timeout := nc.getTimeout(ctx)

	// Send NATS request
	conn := nc.client.GetConnection()
	if conn == nil {
		return nil, errors.WrapTransient(errors.ErrNoConnection, "NATSClient", "QueryEntitiesByIDs",
			"NATS connection not available")
	}

	msg, err := conn.Request(nc.subjects.EntitiesQuery, reqData, timeout)
	if err != nil {
		return nil, errors.WrapTransient(err, "NATSClient", "QueryEntitiesByIDs",
			fmt.Sprintf("NATS request to %s failed", nc.subjects.EntitiesQuery))
	}

	// Parse response
	var response struct {
		Entities []*Entity `json:"entities"`
		Error    string    `json:"error,omitempty"`
	}

	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryEntitiesByIDs", "unmarshal response")
	}

	if response.Error != "" {
		return nil, errors.WrapInvalid(
			fmt.Errorf("%s", response.Error),
			"NATSClient",
			"QueryEntitiesByIDs",
			"remote service error")
	}

	return response.Entities, nil
}

// QueryEntitiesByType queries all entities of a specific type via NATS
// This is a two-step operation: query by type to get IDs, then batch load entities
func (nc *NATSClient) QueryEntitiesByType(ctx context.Context, entityType string, limit int) ([]*Entity, error) {
	// Step 1: Query for entity IDs by type
	req := map[string]interface{}{
		"entity_type": entityType,
		"limit":       limit,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryEntitiesByType", "marshal request")
	}

	// Determine timeout from context or use default
	timeout := nc.getTimeout(ctx)

	// Send NATS request for type query
	conn := nc.client.GetConnection()
	if conn == nil {
		return nil, errors.WrapTransient(errors.ErrNoConnection, "NATSClient", "QueryEntitiesByType",
			"NATS connection not available")
	}

	msg, err := conn.Request(nc.subjects.TypeQuery, reqData, timeout)
	if err != nil {
		return nil, errors.WrapTransient(err, "NATSClient", "QueryEntitiesByType",
			fmt.Sprintf("NATS request to %s failed", nc.subjects.TypeQuery))
	}

	// Parse response to get entity IDs
	var typeResponse struct {
		EntityIDs []string `json:"entity_ids"`
		Error     string   `json:"error,omitempty"`
	}

	if err := json.Unmarshal(msg.Data, &typeResponse); err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryEntitiesByType", "unmarshal type response")
	}

	if typeResponse.Error != "" {
		return nil, errors.WrapInvalid(
			fmt.Errorf("%s", typeResponse.Error),
			"NATSClient",
			"QueryEntitiesByType",
			"remote service error")
	}

	// If no entities found, return empty slice
	if len(typeResponse.EntityIDs) == 0 {
		return []*Entity{}, nil
	}

	// Step 2: Batch load entities by their IDs
	return nc.QueryEntitiesByIDs(ctx, typeResponse.EntityIDs)
}

// QueryRelationships queries relationships based on filters via NATS
func (nc *NATSClient) QueryRelationships(ctx context.Context, filters RelationshipFilters) ([]*Relationship, error) {
	// Create request
	reqData, err := json.Marshal(filters)
	if err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryRelationships", "marshal request")
	}

	// Determine timeout from context or use default
	timeout := nc.getTimeout(ctx)

	// Send NATS request
	conn := nc.client.GetConnection()
	if conn == nil {
		return nil, errors.WrapTransient(errors.ErrNoConnection, "NATSClient", "QueryRelationships",
			"NATS connection not available")
	}

	msg, err := conn.Request(nc.subjects.RelationshipQuery, reqData, timeout)
	if err != nil {
		return nil, errors.WrapTransient(err, "NATSClient", "QueryRelationships",
			fmt.Sprintf("NATS request to %s failed", nc.subjects.RelationshipQuery))
	}

	// Parse response
	var response struct {
		Relationships []*Relationship `json:"relationships"`
		Error         string          `json:"error,omitempty"`
	}

	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "QueryRelationships", "unmarshal response")
	}

	if response.Error != "" {
		return nil, errors.WrapInvalid(
			fmt.Errorf("%s", response.Error),
			"NATSClient",
			"QueryRelationships",
			"remote service error")
	}

	return response.Relationships, nil
}

// SemanticSearch performs semantic similarity search via NATS
func (nc *NATSClient) SemanticSearch(ctx context.Context, query string, limit int) ([]*SemanticSearchResult, error) {
	// Create request
	req := map[string]interface{}{
		"query": query,
		"limit": limit,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "SemanticSearch", "marshal request")
	}

	// Determine timeout from context or use default
	timeout := nc.getTimeout(ctx)

	// Send NATS request
	conn := nc.client.GetConnection()
	if conn == nil {
		return nil, errors.WrapTransient(errors.ErrNoConnection, "NATSClient", "SemanticSearch",
			"NATS connection not available")
	}

	msg, err := conn.Request(nc.subjects.SemanticSearch, reqData, timeout)
	if err != nil {
		return nil, errors.WrapTransient(err, "NATSClient", "SemanticSearch",
			fmt.Sprintf("NATS request to %s failed", nc.subjects.SemanticSearch))
	}

	// Parse response
	var response struct {
		Results []*SemanticSearchResult `json:"results"`
		Error   string                  `json:"error,omitempty"`
	}

	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return nil, errors.WrapInvalid(err, "NATSClient", "SemanticSearch", "unmarshal response")
	}

	if response.Error != "" {
		return nil, errors.WrapInvalid(
			fmt.Errorf("%s", response.Error),
			"NATSClient",
			"SemanticSearch",
			"remote service error")
	}

	return response.Results, nil
}

// getTimeout extracts timeout from context or returns default
// If the context deadline has already passed, returns a minimal timeout
// so that NATS returns immediately and the context error can be caught.
func (nc *NATSClient) getTimeout(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nc.timeout
	}

	timeout := time.Until(deadline)
	if timeout <= 0 {
		// Context already expired - use minimal timeout so NATS returns immediately
		// The context error will be caught by the caller
		return 1 * time.Nanosecond
	}

	return timeout
}
