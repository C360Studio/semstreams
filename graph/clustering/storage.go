package clustering

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// CommunityBucket is the NATS KV bucket for storing communities
	CommunityBucket = "COMMUNITY_INDEX"

	// Key patterns (COMMUNITY_INDEX is a dedicated bucket, no prefix needed):
	// - {level}.{community_id} - Community data
	// - entity.{level}.{entity_id} - Entity -> Community mapping

	// MaxCommunityLevels is the maximum hierarchy depth for scanning
	MaxCommunityLevels = 10
)

// CommunityStorageConfig configures community storage behavior
type CommunityStorageConfig struct {
	// CreateTriples enables creation of member-of triples during SaveCommunity
	CreateTriples bool

	// TriplePredicate specifies the predicate to use for community membership triples
	// Default: "graph.community.member-of"
	TriplePredicate string
}

// NATSCommunityStorage implements CommunityStorage using NATS KV
type NATSCommunityStorage struct {
	kv             jetstream.KeyValue
	config         CommunityStorageConfig
	createdTriples []message.Triple      // Track created triples for testing
	testStore      map[string]*Community // In-memory store for testing when kv is nil
}

// NewNATSCommunityStorage creates a new NATS-backed community storage
// with default configuration (no triple creation)
func NewNATSCommunityStorage(kv jetstream.KeyValue) *NATSCommunityStorage {
	storage := &NATSCommunityStorage{
		kv: kv,
		config: CommunityStorageConfig{
			CreateTriples: false,
		},
		createdTriples: make([]message.Triple, 0),
	}

	// Initialize in-memory test store if KV is nil
	if kv == nil {
		storage.testStore = make(map[string]*Community)
	}

	return storage
}

// NewNATSCommunityStorageWithConfig creates a new NATS-backed community storage
// with custom configuration for triple creation
func NewNATSCommunityStorageWithConfig(kv jetstream.KeyValue, config CommunityStorageConfig) *NATSCommunityStorage {
	// Apply default predicate if not specified
	if config.CreateTriples && config.TriplePredicate == "" {
		config.TriplePredicate = "graph.community.member-of"
	}

	storage := &NATSCommunityStorage{
		kv:             kv,
		config:         config,
		createdTriples: make([]message.Triple, 0),
	}

	// Initialize in-memory test store if KV is nil
	if kv == nil {
		storage.testStore = make(map[string]*Community)
	}

	return storage
}

// SaveCommunity persists a community to NATS KV
func (s *NATSCommunityStorage) SaveCommunity(ctx context.Context, community *Community) error {
	if community == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSCommunityStorage", "SaveCommunity", "community is nil")
	}

	// If KV is available, persist to NATS
	if s.kv != nil {
		// Serialize community
		data, err := json.Marshal(community)
		if err != nil {
			return errs.WrapInvalid(err, "NATSCommunityStorage", "SaveCommunity", "marshal community")
		}

		// Store community data
		communityKey := communityKey(community.Level, community.ID)
		if _, err := s.kv.Put(ctx, communityKey, data); err != nil {
			return errs.WrapTransient(err, "NATSCommunityStorage", "SaveCommunity", "put community")
		}

		// Index entity -> community mappings
		for _, entityID := range community.Members {
			entityKey := entityCommunityKey(community.Level, entityID)
			if _, err := s.kv.Put(ctx, entityKey, []byte(community.ID)); err != nil {
				return errs.WrapTransient(err, "NATSCommunityStorage", "SaveCommunity", "put entity mapping")
			}
		}
	} else if s.testStore != nil {
		// Use in-memory store for testing
		s.testStore[community.ID] = community
	}

	// Create member-of triples if enabled
	if s.config.CreateTriples {
		triples := s.createCommunityTriples(community)
		s.createdTriples = append(s.createdTriples, triples...)
	}

	return nil
}

// createCommunityTriples generates member_of triples for a community
func (s *NATSCommunityStorage) createCommunityTriples(community *Community) []message.Triple {
	triples := make([]message.Triple, 0, len(community.Members))
	timestamp := time.Now()

	for _, memberID := range community.Members {
		triple := message.Triple{
			Subject:    memberID,
			Predicate:  s.config.TriplePredicate,
			Object:     community.ID,
			Source:     "community_detection",
			Timestamp:  timestamp,
			Confidence: 1.0,
		}
		triples = append(triples, triple)
	}

	return triples
}

// GetCreatedTriples returns all triples created during SaveCommunity operations
// This method is primarily for testing and verification purposes
func (s *NATSCommunityStorage) GetCreatedTriples() []message.Triple {
	return s.createdTriples
}

// GetCommunity retrieves a community by ID.
// Since community IDs no longer embed the level, this scans all levels to find the community.
func (s *NATSCommunityStorage) GetCommunity(ctx context.Context, id string) (*Community, error) {
	if id == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "NATSCommunityStorage", "GetCommunity", "id is empty")
	}

	// If using test store, return from memory
	if s.kv == nil && s.testStore != nil {
		community, ok := s.testStore[id]
		if !ok {
			return nil, nil
		}
		return community, nil
	}

	// If KV is nil without test store, return nil
	if s.kv == nil {
		return nil, nil
	}

	// Scan all levels to find the community (typically levels 0-2)
	for level := 0; level < MaxCommunityLevels; level++ {
		key := communityKey(level, id)
		entry, err := s.kv.Get(ctx, key)
		if err != nil {
			if err == jetstream.ErrKeyNotFound {
				continue // Try next level
			}
			return nil, errs.WrapTransient(err, "NATSCommunityStorage", "GetCommunity", "get community")
		}

		// Deserialize
		var community Community
		if err := json.Unmarshal(entry.Value(), &community); err != nil {
			return nil, errs.WrapInvalid(err, "NATSCommunityStorage", "GetCommunity", "unmarshal community")
		}

		return &community, nil
	}

	// Not found in any level
	return nil, nil
}

// GetCommunitiesByLevel retrieves all communities at a level
func (s *NATSCommunityStorage) GetCommunitiesByLevel(ctx context.Context, level int) ([]*Community, error) {
	communities := make([]*Community, 0)

	// Use server-side prefix filtering: community keys are "{level}.{id}"
	pattern := fmt.Sprintf("%d.>", level)
	keys, err := natsclient.FilteredKeys(ctx, s.kv, pattern)
	if err != nil {
		return nil, errs.WrapTransient(err, "NATSCommunityStorage", "GetCommunitiesByLevel", "list keys")
	}

	for _, key := range keys {
		entry, err := s.kv.Get(ctx, key)
		if err != nil {
			continue // Skip errors for individual entries
		}

		var community Community
		if err := json.Unmarshal(entry.Value(), &community); err != nil {
			continue
		}

		communities = append(communities, &community)
	}

	return communities, nil
}

// GetAllCommunities returns all communities across all levels
// Used by the LPA detector to archive enhanced communities before a rebuild
func (s *NATSCommunityStorage) GetAllCommunities(ctx context.Context) ([]*Community, error) {
	// If using test store, return from memory
	if s.kv == nil && s.testStore != nil {
		communities := make([]*Community, 0, len(s.testStore))
		for _, c := range s.testStore {
			communities = append(communities, c)
		}
		return communities, nil
	}

	// If KV is nil without test store, return empty
	if s.kv == nil {
		return nil, nil
	}

	keys, err := s.kv.Keys(ctx)
	if err != nil {
		// Empty bucket returns ErrKeyNotFound or "no keys found" error
		if stderrors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "no keys found") {
			return nil, nil
		}
		// Context cancellation during shutdown - return empty (archival is best-effort)
		if stderrors.Is(err, context.Canceled) {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "NATSCommunityStorage", "GetAllCommunities", "list keys")
	}

	var communities []*Community
	for _, key := range keys {
		// Skip entity mapping keys (format: entity.{level}.{entityID})
		if strings.HasPrefix(key, "entity.") {
			continue
		}

		entry, err := s.kv.Get(ctx, key)
		if err != nil {
			continue // Skip errors for individual entries
		}

		var community Community
		if err := json.Unmarshal(entry.Value(), &community); err != nil {
			continue // Skip unparseable entries
		}

		communities = append(communities, &community)
	}

	return communities, nil
}

// GetEntityCommunity retrieves the community for an entity at a level
func (s *NATSCommunityStorage) GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error) {
	if entityID == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "NATSCommunityStorage", "GetEntityCommunity", "entityID is empty")
	}

	// Lookup entity -> community mapping
	key := entityCommunityKey(level, entityID)
	entry, err := s.kv.Get(ctx, key)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			// Entity not in any community is not an error - return nil
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "NATSCommunityStorage", "GetEntityCommunity", "get entity mapping")
	}

	communityID := string(entry.Value())

	// Fetch community data
	return s.GetCommunity(ctx, communityID)
}

// DeleteCommunity removes a community
func (s *NATSCommunityStorage) DeleteCommunity(ctx context.Context, id string) error {
	if id == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSCommunityStorage", "DeleteCommunity", "id is empty")
	}

	// Get community to find members
	community, err := s.GetCommunity(ctx, id)
	if err != nil {
		return err
	}

	// Handle case where community doesn't exist
	if community == nil {
		return nil // Already deleted
	}

	// Delete entity mappings - accumulate errors
	var deleteErrs []error
	for _, entityID := range community.Members {
		entityKey := entityCommunityKey(community.Level, entityID)
		if err := s.kv.Delete(ctx, entityKey); err != nil {
			deleteErrs = append(deleteErrs, fmt.Errorf("failed to delete mapping for %s: %w", entityID, err))
		}
	}

	// Delete community data
	communityKey := communityKey(community.Level, id)
	if err := s.kv.Delete(ctx, communityKey); err != nil {
		deleteErrs = append(deleteErrs, fmt.Errorf("failed to delete community: %w", err))
	}

	// Return combined error if any occurred
	if len(deleteErrs) > 0 {
		return errs.WrapTransient(
			fmt.Errorf("%d deletion errors: %v", len(deleteErrs), deleteErrs),
			"NATSCommunityStorage",
			"DeleteCommunity",
			"partial deletion failure",
		)
	}

	return nil
}

// Prune removes every stored key that does not belong to the supplied partition.
//
// The keep set is derived here rather than passed in as raw keys so the KV key
// format stays private to this file: a caller hands over the communities it just
// wrote, and this method reconstructs both the community keys ({level}.{id}) and
// the entity mapping keys (entity.{level}.{entity_id}) they imply.
//
// Passing an empty keep set deletes everything, which is the correct end state
// for a graph that genuinely has no entities.
//
// Best-effort with respect to shutdown: context cancellation mid-delete is
// skipped rather than reported, matching Clear.
func (s *NATSCommunityStorage) Prune(ctx context.Context, keep []*Community) error {
	// In-memory test store path (kv == nil).
	if s.kv == nil {
		if s.testStore == nil {
			return nil
		}
		keepIDs := make(map[string]struct{}, len(keep))
		for _, c := range keep {
			if c != nil {
				keepIDs[c.ID] = struct{}{}
			}
		}
		for id := range s.testStore {
			if _, ok := keepIDs[id]; !ok {
				delete(s.testStore, id)
			}
		}
		return nil
	}

	// Reconstruct every key the new partition owns.
	keepKeys := make(map[string]struct{}, len(keep)*2)
	for _, c := range keep {
		if c == nil {
			continue
		}
		keepKeys[communityKey(c.Level, c.ID)] = struct{}{}
		for _, entityID := range c.Members {
			keepKeys[entityCommunityKey(c.Level, entityID)] = struct{}{}
		}
	}

	keys, err := s.kv.Keys(ctx)
	if err != nil {
		// Empty bucket returns ErrKeyNotFound or "no keys found" error
		if stderrors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "no keys found") {
			return nil
		}
		// Context cancellation during shutdown is acceptable
		if stderrors.Is(err, context.Canceled) {
			return nil
		}
		return errs.WrapTransient(err, "NATSCommunityStorage", "Prune", "list keys")
	}

	var deleteErrs []error
	for _, key := range keys {
		if _, ok := keepKeys[key]; ok {
			continue
		}
		if err := s.kv.Delete(ctx, key); err != nil {
			// Skip context cancellation errors during shutdown
			if stderrors.Is(err, context.Canceled) {
				continue
			}
			deleteErrs = append(deleteErrs, fmt.Errorf("failed to delete %s: %w", key, err))
		}
	}

	if len(deleteErrs) > 0 {
		return errs.WrapTransient(
			fmt.Errorf("%d deletion errors: %v", len(deleteErrs), deleteErrs),
			"NATSCommunityStorage",
			"Prune",
			"partial prune failure",
		)
	}

	return nil
}

// Clear removes all communities and entity mappings.
//
// This is for teardown and explicit operator-driven reset only. Rebuilds use
// SaveCommunity + Prune so the index is never transiently empty (see Prune).
//
// This is a best-effort operation - context cancellation during cleanup is ignored
// since partial cleanup is acceptable during shutdown.
func (s *NATSCommunityStorage) Clear(ctx context.Context) error {
	keys, err := s.kv.Keys(ctx)
	if err != nil {
		// Empty bucket returns ErrKeyNotFound or "no keys found" error
		if stderrors.Is(err, jetstream.ErrKeyNotFound) || strings.Contains(err.Error(), "no keys found") {
			return nil
		}
		// Context cancellation during shutdown is acceptable
		if stderrors.Is(err, context.Canceled) {
			return nil
		}
		return errs.WrapTransient(err, "NATSCommunityStorage", "Clear", "list keys")
	}

	// Delete all keys in the bucket (dedicated COMMUNITY_INDEX bucket)
	var deleteErrs []error
	for _, key := range keys {
		if err := s.kv.Delete(ctx, key); err != nil {
			// Skip context cancellation errors during shutdown
			if stderrors.Is(err, context.Canceled) {
				continue
			}
			deleteErrs = append(deleteErrs, fmt.Errorf("failed to delete %s: %w", key, err))
		}
	}

	// Return combined error if any occurred (excluding context cancellation)
	if len(deleteErrs) > 0 {
		return errs.WrapTransient(
			fmt.Errorf("%d deletion errors: %v", len(deleteErrs), deleteErrs),
			"NATSCommunityStorage",
			"Clear",
			"partial clear failure",
		)
	}

	return nil
}

// Key generation helpers
// COMMUNITY_INDEX is a dedicated bucket, so no "graph.community." prefix needed.
// Format: {level}.{community_id} for communities, entity.{level}.{entity_id} for mappings

func communityKey(level int, communityID string) string {
	return fmt.Sprintf("%d.%s", level, communityID)
}

func entityCommunityKey(level int, entityID string) string {
	return fmt.Sprintf("entity.%d.%s", level, entityID)
}
