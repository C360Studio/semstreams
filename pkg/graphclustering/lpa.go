package graphclustering

import (
	"context"
	"fmt"
	"math/rand"
	"sync"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

const (
	// DefaultMaxIterations is the default maximum iteration count
	DefaultMaxIterations = 100

	// MaxIterationsLimit is the maximum allowed iteration count
	MaxIterationsLimit = 10000

	// DefaultLevels is the default number of hierarchical levels
	DefaultLevels = 3

	// MaxLevelsLimit is the maximum allowed hierarchical levels
	MaxLevelsLimit = 10
)

// EntityProvider interface for fetching full entity states for summarization
type EntityProvider interface {
	GetEntities(ctx context.Context, ids []string) ([]*gtypes.EntityState, error)
}

// LPADetector implements community detection using Label Propagation Algorithm
type LPADetector struct {
	graphProvider GraphProvider
	storage       CommunityStorage

	// Configuration
	maxIterations int // Maximum iterations before forced convergence
	levels        int // Number of hierarchical levels (default: 3)

	// Progressive summarization (optional)
	summarizer     CommunitySummarizer // Optional: generates summaries for communities
	entityProvider EntityProvider      // Optional: fetches entities for summarization

	// State
	mu sync.RWMutex
}

// NewLPADetector creates a new Label Propagation Algorithm detector
func NewLPADetector(provider GraphProvider, storage CommunityStorage) *LPADetector {
	return &LPADetector{
		graphProvider: provider,
		storage:       storage,
		maxIterations: DefaultMaxIterations,
		levels:        DefaultLevels,
	}
}

// WithMaxIterations sets the maximum iteration count with validation
func (d *LPADetector) WithMaxIterations(max int) *LPADetector {
	// Validate and apply bounds
	if max <= 0 {
		max = DefaultMaxIterations
	}
	if max > MaxIterationsLimit {
		max = MaxIterationsLimit
	}
	d.maxIterations = max
	return d
}

// WithLevels sets the number of hierarchical levels with validation
func (d *LPADetector) WithLevels(levels int) *LPADetector {
	// Validate and apply bounds
	if levels <= 0 {
		levels = DefaultLevels
	}
	if levels > MaxLevelsLimit {
		levels = MaxLevelsLimit
	}
	d.levels = levels
	return d
}

// WithProgressiveSummarization enables progressive summarization with LLM enhancement
// summarizer: generates statistical summaries immediately
// entityProvider: fetches full entity states for summarization
// Note: EnhancementWorker watches COMMUNITY_INDEX KV for async LLM enhancement (no NATS events needed)
func (d *LPADetector) WithProgressiveSummarization(
	summarizer CommunitySummarizer,
	entityProvider EntityProvider,
) *LPADetector {
	d.summarizer = summarizer
	d.entityProvider = entityProvider
	return d
}

// DetectCommunities runs full community detection across all hierarchical levels
func (d *LPADetector) DetectCommunities(ctx context.Context) (map[int][]*Community, error) {
	// Validate dependencies
	if d.graphProvider == nil {
		return nil, errors.WrapFatal(errors.ErrMissingConfig, "LPADetector", "DetectCommunities", "graphProvider is nil")
	}
	if d.storage == nil {
		return nil, errors.WrapFatal(errors.ErrMissingConfig, "LPADetector", "DetectCommunities", "storage is nil")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear existing communities
	if err := d.storage.Clear(ctx); err != nil {
		return nil, errors.WrapTransient(err, "LPADetector", "DetectCommunities", "clear storage")
	}

	// Get all entities
	entityIDs, err := d.graphProvider.GetAllEntityIDs(ctx)
	if err != nil {
		return nil, errors.WrapTransient(err, "LPADetector", "DetectCommunities", "get entities")
	}

	if len(entityIDs) == 0 {
		return make(map[int][]*Community), nil
	}

	result := make(map[int][]*Community)

	// Level 0: Fine-grained communities
	level0Communities, err := d.detectCommunitiesAtLevel(ctx, entityIDs, 0, nil)
	if err != nil {
		return nil, err
	}
	result[0] = level0Communities

	// Higher levels: Hierarchical clustering
	prevCommunities := level0Communities
	for level := 1; level < d.levels; level++ {
		communities, err := d.detectHierarchicalLevel(ctx, prevCommunities, level)
		if err != nil {
			return nil, err
		}
		result[level] = communities
		prevCommunities = communities
	}

	return result, nil
}

// detectCommunitiesAtLevel runs LPA on a set of entities
func (d *LPADetector) detectCommunitiesAtLevel(
	ctx context.Context,
	entityIDs []string,
	level int,
	parentID *string,
) ([]*Community, error) {
	// Initialize: Each entity gets unique label
	labels := make(map[string]string)
	for _, id := range entityIDs {
		labels[id] = id // Entity's own ID is initial label
	}

	// Iterate until convergence
	for iter := 0; iter < d.maxIterations; iter++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, errors.WrapTransient(ctx.Err(), "LPADetector", "detectCommunitiesAtLevel", "context cancelled")
		default:
		}

		changed := false

		// Shuffle entity processing order (reduces oscillation)
		shuffledIDs := make([]string, len(entityIDs))
		copy(shuffledIDs, entityIDs)
		rand.Shuffle(len(shuffledIDs), func(i, j int) {
			shuffledIDs[i], shuffledIDs[j] = shuffledIDs[j], shuffledIDs[i]
		})

		// Update labels based on neighbor voting
		for _, entityID := range shuffledIDs {
			newLabel, err := d.computeNewLabel(ctx, entityID, labels)
			if err != nil {
				return nil, err
			}

			if newLabel != labels[entityID] {
				labels[entityID] = newLabel
				changed = true
			}
		}

		// Check convergence
		if !changed {
			break
		}
	}

	// Build communities from labels
	communities := d.buildCommunities(labels, level, parentID)

	// Persist communities (with optional summarization and event publishing)
	for _, community := range communities {
		// Generate summary if summarizer is configured
		if d.summarizer != nil && d.entityProvider != nil {
			entities, err := d.entityProvider.GetEntities(ctx, community.Members)
			if err != nil {
				// Log warning but continue - community will have no summary
				fmt.Printf("WARN: Failed to fetch entities for summarization: %v\n", err)
			} else {
				// Generate statistical summary
				summarized, err := d.summarizer.SummarizeCommunity(ctx, community, entities)
				if err != nil {
					fmt.Printf("WARN: Failed to generate summary: %v\n", err)
				} else {
					// Update community with summary
					community = summarized
				}
			}
		}

		// Save community
		if err := d.storage.SaveCommunity(ctx, community); err != nil {
			return nil, errors.WrapTransient(err, "LPADetector", "detectCommunitiesAtLevel", "save community")
		}

		// Note: Communities saved with summary_status="statistical" will be picked up
		// by EnhancementWorker via KV watcher for async LLM enhancement
	}

	return communities, nil
}

// computeNewLabel determines the new label for an entity based on neighbor votes
func (d *LPADetector) computeNewLabel(
	ctx context.Context,
	entityID string,
	labels map[string]string,
) (string, error) {
	// Get neighbors
	neighbors, err := d.graphProvider.GetNeighbors(ctx, entityID, "both")
	if err != nil {
		return "", errors.WrapTransient(err, "LPADetector", "computeNewLabel", "get neighbors")
	}

	if len(neighbors) == 0 {
		// Isolated node keeps its own label
		return labels[entityID], nil
	}

	// Count label frequencies (weighted by edge weights)
	labelVotes := make(map[string]float64)
	for _, neighborID := range neighbors {
		neighborLabel, exists := labels[neighborID]
		if !exists {
			continue // Skip neighbors not in current entity set
		}

		// Get edge weight (default: 1.0)
		weight, err := d.graphProvider.GetEdgeWeight(ctx, entityID, neighborID)
		if err != nil {
			weight = 1.0 // Default to unweighted
		}

		labelVotes[neighborLabel] += weight
	}

	// Find label with maximum votes
	maxVotes := 0.0
	var winningLabel string
	for label, votes := range labelVotes {
		if votes > maxVotes {
			maxVotes = votes
			winningLabel = label
		}
	}

	// If no votes (shouldn't happen), keep current label
	if winningLabel == "" {
		return labels[entityID], nil
	}

	return winningLabel, nil
}

// buildCommunities creates Community objects from label assignments
func (d *LPADetector) buildCommunities(
	labels map[string]string,
	level int,
	parentID *string,
) []*Community {
	// Group entities by label
	labelToMembers := make(map[string][]string)
	for entityID, label := range labels {
		labelToMembers[label] = append(labelToMembers[label], entityID)
	}

	// Create communities
	communities := make([]*Community, 0, len(labelToMembers))
	for label, members := range labelToMembers {
		communityID := fmt.Sprintf("comm-%d-%s", level, label)

		community := &Community{
			ID:       communityID,
			Level:    level,
			Members:  members,
			ParentID: parentID,
			Metadata: map[string]interface{}{
				"size": len(members),
			},
		}
		communities = append(communities, community)
	}

	return communities
}

// detectHierarchicalLevel creates next-level communities by clustering previous level
func (d *LPADetector) detectHierarchicalLevel(
	ctx context.Context,
	prevCommunities []*Community,
	level int,
) ([]*Community, error) {
	// Treat communities as super-nodes
	// Build connectivity graph between communities

	// For simplicity, we'll use a coarsening approach:
	// Merge small communities and re-run LPA on community graph

	// Extract all entity IDs from previous level
	allEntities := make([]string, 0)
	for _, comm := range prevCommunities {
		allEntities = append(allEntities, comm.Members...)
	}

	// Run LPA with larger convergence threshold (fewer communities)
	communities, err := d.detectCommunitiesAtLevel(ctx, allEntities, level, nil)
	if err != nil {
		return nil, err
	}

	// Link communities to their parents (future enhancement)
	// For now, top-level communities don't track parent references

	return communities, nil
}

// UpdateCommunities incrementally updates communities based on changed entities
func (d *LPADetector) UpdateCommunities(ctx context.Context, entityIDs []string) error {
	// Don't lock here - DetectCommunities handles its own locking
	// For MVP, we'll do full recomputation
	// Future optimization: local label propagation only around changed entities
	_, err := d.DetectCommunities(ctx)
	return err
}

// GetCommunity retrieves a community by ID
func (d *LPADetector) GetCommunity(ctx context.Context, id string) (*Community, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.storage.GetCommunity(ctx, id)
}

// GetEntityCommunity returns the community for an entity at a specific level
func (d *LPADetector) GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.storage.GetEntityCommunity(ctx, entityID, level)
}

// GetCommunitiesByLevel returns all communities at a level
func (d *LPADetector) GetCommunitiesByLevel(ctx context.Context, level int) ([]*Community, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.storage.GetCommunitiesByLevel(ctx, level)
}
