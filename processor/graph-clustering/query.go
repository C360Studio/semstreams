// Package graphclustering query handlers
package graphclustering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// setupQueryHandlers sets up NATS request/reply subscriptions for query handlers
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	// Subscribe to community query
	sub1, err := c.natsClient.SubscribeForRequests(ctx, "graph.clustering.query.community", c.handleQueryCommunityNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe community query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub1)

	// Subscribe to members query
	sub2, err := c.natsClient.SubscribeForRequests(ctx, "graph.clustering.query.members", c.handleQueryMembersNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe members query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub2)

	// Subscribe to entity community query
	sub3, err := c.natsClient.SubscribeForRequests(ctx, "graph.clustering.query.entity", c.handleQueryEntityNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe entity query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub3)

	// Subscribe to level query
	sub4, err := c.natsClient.SubscribeForRequests(ctx, "graph.clustering.query.level", c.handleQueryLevelNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe level query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub4)

	c.logger.Info("query handlers registered",
		"subjects", []string{
			"graph.clustering.query.community",
			"graph.clustering.query.members",
			"graph.clustering.query.entity",
			"graph.clustering.query.level",
		})

	return nil
}

// CommunityRequest is the request format for community query
type CommunityRequest struct {
	ID string `json:"id"`
}

// CommunityResponse is the response format for community query
type CommunityResponse struct {
	Community *clustering.Community `json:"community"`
}

// MembersRequest is the request format for members query
type MembersRequest struct {
	CommunityID string `json:"community_id"`
}

// MembersResponse is the response format for members query
type MembersResponse struct {
	CommunityID string   `json:"community_id"`
	Members     []string `json:"members"`
	Count       int      `json:"count"`
}

// EntityRequest is the request format for entity community query
type EntityRequest struct {
	EntityID string `json:"entity_id"`
	Level    int    `json:"level"`
}

// EntityResponse is the response format for entity community query
type EntityResponse struct {
	EntityID  string                `json:"entity_id"`
	Level     int                   `json:"level"`
	Community *clustering.Community `json:"community"`
}

// LevelRequest is the request format for level query
type LevelRequest struct {
	Level int `json:"level"`
}

// LevelResponse is the response format for level query
type LevelResponse struct {
	Level       int                     `json:"level"`
	Communities []*clustering.Community `json:"communities"`
	Count       int                     `json:"count"`
}

// handleQueryCommunityNATS handles community query requests via NATS request/reply
func (c *Component) handleQueryCommunityNATS(requestCtx context.Context, data []byte) ([]byte, error) {
	if err := c.graphStateContractError("handleQueryCommunityNATS"); err != nil {
		return nil, err
	}
	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(requestCtx, 10*time.Second)
	defer cancel()

	// Parse request
	var req CommunityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryCommunityNATS", "request unmarshal")
	}

	// Validate request
	if req.ID == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "handleQueryCommunityNATS", "empty community id")
	}

	// Get community from bucket
	community, err := c.getCommunity(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	response := CommunityResponse{
		Community: community,
	}

	return json.Marshal(response)
}

// handleQueryMembersNATS handles members query requests via NATS request/reply
func (c *Component) handleQueryMembersNATS(requestCtx context.Context, data []byte) ([]byte, error) {
	if err := c.graphStateContractError("handleQueryMembersNATS"); err != nil {
		return nil, err
	}
	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(requestCtx, 10*time.Second)
	defer cancel()

	// Parse request
	var req MembersRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryMembersNATS", "request unmarshal")
	}

	// Validate request
	if req.CommunityID == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "handleQueryMembersNATS", "empty community id")
	}

	// Get community
	community, err := c.getCommunity(ctx, req.CommunityID)
	if err != nil {
		return nil, err
	}

	response := MembersResponse{
		CommunityID: req.CommunityID,
		Members:     community.Members,
		Count:       len(community.Members),
	}

	return json.Marshal(response)
}

// handleQueryEntityNATS handles entity community query requests via NATS request/reply
func (c *Component) handleQueryEntityNATS(requestCtx context.Context, data []byte) ([]byte, error) {
	if err := c.graphStateContractError("handleQueryEntityNATS"); err != nil {
		return nil, err
	}
	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(requestCtx, 10*time.Second)
	defer cancel()

	// Parse request
	var req EntityRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryEntityNATS", "request unmarshal")
	}

	// Validate request
	if req.EntityID == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "handleQueryEntityNATS", "empty entity id")
	}

	// Apply defaults
	level := req.Level
	if level < 0 {
		level = 0
	}

	// Get entity community by scanning communities at level
	community, err := c.getEntityCommunity(ctx, req.EntityID, level)
	if err != nil {
		return nil, err
	}

	response := EntityResponse{
		EntityID:  req.EntityID,
		Level:     level,
		Community: community,
	}

	return json.Marshal(response)
}

// handleQueryLevelNATS handles level query requests via NATS request/reply
func (c *Component) handleQueryLevelNATS(requestCtx context.Context, data []byte) ([]byte, error) {
	if err := c.graphStateContractError("handleQueryLevelNATS"); err != nil {
		return nil, err
	}
	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(requestCtx, 10*time.Second)
	defer cancel()

	// Parse request
	var req LevelRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryLevelNATS", "request unmarshal")
	}

	// Apply defaults
	level := req.Level
	if level < 0 {
		level = 0
	}

	// Get communities at level
	communities, err := c.getCommunitiesByLevel(ctx, level)
	if err != nil {
		return nil, err
	}

	response := LevelResponse{
		Level:       level,
		Communities: communities,
		Count:       len(communities),
	}

	return json.Marshal(response)
}

// getCommunity retrieves a community by ID from the KV bucket
func (c *Component) getCommunity(ctx context.Context, id string) (*clustering.Community, error) {
	if c.communityBucket == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "Component", "getCommunity", "community bucket not initialized")
	}

	entry, err := c.communityBucket.Get(ctx, id)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, errs.WrapInvalid(errs.ErrKeyNotFound, "Component", "getCommunity", fmt.Sprintf("community not found: %s", id))
		}
		return nil, errs.WrapTransient(err, "Component", "getCommunity", "KV bucket read")
	}

	var community clustering.Community
	if err := json.Unmarshal(entry.Value(), &community); err != nil {
		return nil, errs.Wrap(err, "Component", "getCommunity", "community unmarshal")
	}

	return &community, nil
}

// getEntityCommunity finds the community containing the given entity at the specified level
// Uses the indexed entity mapping (entity.{level}.{entity_id} -> community_id) for O(1) lookup
func (c *Component) getEntityCommunity(ctx context.Context, entityID string, level int) (*clustering.Community, error) {
	if c.communityBucket == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "Component", "getEntityCommunity", "community bucket not initialized")
	}

	// Use indexed entity -> community mapping for O(1) lookup
	entityKey := fmt.Sprintf("entity.%d.%s", level, entityID)
	entry, err := c.communityBucket.Get(ctx, entityKey)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // Entity not in any community at this level
		}
		return nil, errs.WrapTransient(err, "Component", "getEntityCommunity", "get entity mapping")
	}

	communityID := string(entry.Value())

	// Fetch the community data
	communityKey := fmt.Sprintf("%d.%s", level, communityID)
	communityEntry, err := c.communityBucket.Get(ctx, communityKey)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // Mapping exists but community was deleted
		}
		return nil, errs.WrapTransient(err, "Component", "getEntityCommunity", "get community")
	}

	var community clustering.Community
	if err := json.Unmarshal(communityEntry.Value(), &community); err != nil {
		return nil, errs.Wrap(err, "Component", "getEntityCommunity", "community unmarshal")
	}

	return &community, nil
}

// getCommunitiesByLevel retrieves all communities at a given level
func (c *Component) getCommunitiesByLevel(ctx context.Context, level int) ([]*clustering.Community, error) {
	if c.communityBucket == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "Component", "getCommunitiesByLevel", "community bucket not initialized")
	}

	// Use server-side prefix filtering: community keys are "{level}.{id}"
	pattern := fmt.Sprintf("%d.>", level)
	keys, err := natsclient.FilteredKeys(ctx, c.communityBucket, pattern)
	if err != nil {
		return nil, errs.WrapTransient(err, "Component", "getCommunitiesByLevel", "list keys")
	}

	var communities []*clustering.Community

	for _, key := range keys {
		entry, err := c.communityBucket.Get(ctx, key)
		if err != nil {
			// A key can disappear between the filtered listing and the point read.
			// That proven not-found race is safe to omit; every other failure means
			// the scan is incomplete and must not be returned as authoritative.
			if errors.Is(err, jetstream.ErrKeyNotFound) {
				continue
			}
			return nil, errs.WrapTransient(err, "Component", "getCommunitiesByLevel", "get community")
		}

		var community clustering.Community
		if err := json.Unmarshal(entry.Value(), &community); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "getCommunitiesByLevel", "community unmarshal")
		}

		communities = append(communities, &community)
	}

	return communities, nil
}
