package clustering

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
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

	// SummaryTransferThreshold is the minimum Jaccard overlap for transferring LLM summaries
	// between archived and newly detected communities
	SummaryTransferThreshold = 0.8
)

// EntityProvider interface for fetching full entity states for summarization
type EntityProvider interface {
	GetEntities(ctx context.Context, ids []string) ([]*gtypes.EntityState, error)
}

// LPADetector implements community detection using Label Propagation Algorithm
type LPADetector struct {
	graphProvider Provider
	storage       CommunityStorage

	// Configuration
	maxIterations int // Maximum iterations before forced convergence
	levels        int // Number of hierarchical levels (default: 3)

	// Progressive summarization (optional)
	summarizer     CommunitySummarizer // Optional: generates summaries for communities
	entityProvider EntityProvider      // Optional: fetches entities for summarization

	// Logging
	logger *slog.Logger

	// State
	mu sync.RWMutex
}

// NewLPADetector creates a new Label Propagation Algorithm detector
func NewLPADetector(provider Provider, storage CommunityStorage) *LPADetector {
	return &LPADetector{
		graphProvider: provider,
		storage:       storage,
		maxIterations: DefaultMaxIterations,
		levels:        DefaultLevels,
		logger:        slog.Default(),
	}
}

// WithLogger sets the logger for the detector
func (d *LPADetector) WithLogger(logger *slog.Logger) *LPADetector {
	d.logger = logger
	return d
}

// WithMaxIterations sets the maximum iteration count with validation
func (d *LPADetector) WithMaxIterations(maxN int) *LPADetector {
	// Validate and apply bounds
	if maxN <= 0 {
		maxN = DefaultMaxIterations
	}
	if maxN > MaxIterationsLimit {
		maxN = MaxIterationsLimit
	}
	d.maxIterations = maxN
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

// WithSummarizer sets the summarizer without requiring an entity provider.
// Use SetEntityProvider() later to enable summarization once the provider is available.
// This supports deferred initialization patterns where the entity provider
// isn't available at detector creation time.
func (d *LPADetector) WithSummarizer(summarizer CommunitySummarizer) *LPADetector {
	d.summarizer = summarizer
	return d
}

// SetEntityProvider sets the entity provider for fetching entities during summarization.
// This method supports deferred initialization - call after the entity provider becomes available.
// Both summarizer and entityProvider must be set for summarization to occur.
func (d *LPADetector) SetEntityProvider(provider EntityProvider) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entityProvider = provider
}

// DetectCommunities runs full community detection across all hierarchical levels
func (d *LPADetector) DetectCommunities(ctx context.Context) (map[int][]*Community, error) {
	// Validate dependencies
	if d.graphProvider == nil {
		return nil, errs.WrapFatal(errs.ErrMissingConfig, "LPADetector", "DetectCommunities", "graphProvider is nil")
	}
	if d.storage == nil {
		return nil, errs.WrapFatal(errs.ErrMissingConfig, "LPADetector", "DetectCommunities", "storage is nil")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// PHASE 1: Archive LLM-enhanced communities before clearing
	// This preserves expensive LLM summaries (typically 5-20s per community) that can be
	// transferred to new communities with similar membership (≥80% overlap).
	var archivedEnhanced []*Community
	allComms, err := d.storage.GetAllCommunities(ctx)
	if err != nil {
		// Log warning but continue - archival failure shouldn't block detection
		d.logger.Warn("Failed to archive communities for preservation", "error", err)
	} else {
		for _, c := range allComms {
			if c.SummaryStatus == "llm-enhanced" && c.LLMSummary != "" {
				archivedEnhanced = append(archivedEnhanced, c)
			}
		}
		if len(archivedEnhanced) > 0 {
			d.logger.Info("Archived LLM-enhanced communities for preservation", "count", len(archivedEnhanced))
		}
	}

	// Clear existing communities
	if err := d.storage.Clear(ctx); err != nil {
		return nil, errs.WrapTransient(err, "LPADetector", "DetectCommunities", "clear storage")
	}

	// Get all entities
	entityIDs, err := d.graphProvider.GetAllEntityIDs(ctx)
	if err != nil {
		return nil, errs.WrapTransient(err, "LPADetector", "DetectCommunities", "get entities")
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

	// PHASE 2: Transfer LLM summaries from archived communities to new ones
	// This preserves expensive LLM work when community structure is stable
	if len(archivedEnhanced) > 0 {
		transferred := 0
		failed := 0
		for _, levelCommunities := range result {
			for _, newComm := range levelCommunities {
				if transferSummary(newComm, archivedEnhanced, SummaryTransferThreshold) {
					// Re-save the community with transferred summary
					if err := d.storage.SaveCommunity(ctx, newComm); err != nil {
						d.logger.Warn("Failed to save community with transferred summary",
							"community_id", newComm.ID, "error", err)
						failed++
					} else {
						transferred++
					}
				}
			}
		}
		if transferred > 0 || failed > 0 {
			d.logger.Info("Transferred LLM summaries to new communities",
				"transferred", transferred, "failed", failed, "archived", len(archivedEnhanced))
		}
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
			return nil, errs.WrapTransient(ctx.Err(), "LPADetector", "detectCommunitiesAtLevel", "context cancelled")
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

	// Pre-compute corpus-wide document frequency once for the level so the
	// statistical summarizer's keyword extraction can weight by IDF instead
	// of TF only. Without this, predicates ("location") and entity-type
	// parts ("sensor") that appear on nearly every entity dominate the
	// top-N keyword cap and crowd out distinctive low-frequency terms
	// like "hydraulic" — the surface globalSearch's known-answer probes
	// depend on. See StatisticalSummarizer.corpusDF doc + 2026-05-07 bug.
	if d.summarizer != nil && d.entityProvider != nil {
		if dfBuilder, ok := d.summarizer.(corpusDFBuilder); ok {
			allEntities, err := d.entityProvider.GetEntities(ctx, entityIDs)
			if err != nil {
				d.logger.Warn("Failed to fetch entities for corpus DF — falling back to TF-only keywords",
					"level", level, "error", err)
			} else {
				dfBuilder.BuildCorpusDF(allEntities)
			}
		}
	}

	// Persist communities (with optional summarization and event publishing)
	for _, community := range communities {
		// Generate summary if summarizer is configured
		if d.summarizer != nil && d.entityProvider != nil {
			entities, err := d.entityProvider.GetEntities(ctx, community.Members)
			if err != nil {
				// Log warning but continue - community will have no summary
				d.logger.Warn("Failed to fetch entities for summarization",
					"community_id", community.ID, "error", err)
			} else {
				// Generate statistical summary
				summarized, err := d.summarizer.SummarizeCommunity(ctx, community, entities)
				if err != nil {
					d.logger.Warn("Failed to generate summary",
						"community_id", community.ID, "error", err)
				} else {
					// Update community with summary
					community = summarized
				}
			}
		}

		// Save community
		if err := d.storage.SaveCommunity(ctx, community); err != nil {
			return nil, errs.WrapTransient(err, "LPADetector", "detectCommunitiesAtLevel", "save community")
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
		return "", errs.WrapTransient(err, "LPADetector", "computeNewLabel", "get neighbors")
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
		// Community ID is just the seed entity ID (label) - level is stored in Level field
		// and used in KV key format: {level}.{community_id}
		community := &Community{
			ID:       label,
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
func (d *LPADetector) UpdateCommunities(ctx context.Context, _ []string) error {
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

// InferenceConfig holds configuration for relationship inference
type InferenceConfig struct {
	// MinCommunitySize is the minimum community size for generating inferences
	// Singleton communities (size=1) never produce inferences
	MinCommunitySize int

	// MaxInferredPerCommunity limits inferred relationships per community
	// Prevents O(n²) explosion in large communities
	MaxInferredPerCommunity int
}

// DefaultInferenceConfig returns sensible defaults for relationship inference
func DefaultInferenceConfig() InferenceConfig {
	return InferenceConfig{
		MinCommunitySize:        2,
		MaxInferredPerCommunity: 50,
	}
}

// InferRelationshipsFromCommunities generates inferred triples from community co-membership.
// For each community with >= minCommunitySize members, this creates bidirectional
// "inferred.clustered_with" triples between members.
//
// Parameters:
//   - level: Hierarchical level to process (0 = most granular)
//   - config: Inference configuration (min size, max pairs)
//
// Returns triples suitable for persistence via graph.mutation.triple.add.
// The caller is responsible for persisting these triples.
//
// Confidence scoring:
//   - Base confidence: 0.5 (inferred relationships)
//   - Adjusted by community tightness: +0.0 to +0.3 based on internal similarity
//   - Final range: 0.5-0.8 for inferred relationships
func (d *LPADetector) InferRelationshipsFromCommunities(
	ctx context.Context,
	level int,
	config InferenceConfig,
) ([]InferredTriple, error) {
	// Apply defaults
	if config.MinCommunitySize <= 0 {
		config.MinCommunitySize = 2
	}
	if config.MaxInferredPerCommunity <= 0 {
		config.MaxInferredPerCommunity = 50
	}

	// Get communities at level
	communities, err := d.storage.GetCommunitiesByLevel(ctx, level)
	if err != nil {
		return nil, err
	}

	var triples []InferredTriple
	now := time.Now()

	for _, community := range communities {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Skip communities below minimum size
		if len(community.Members) < config.MinCommunitySize {
			continue
		}

		// Compute community tightness for confidence adjustment
		tightness := d.computeCommunityTightness(ctx, community)

		// Generate bidirectional pairs with limit
		pairsGenerated := 0
		for i := 0; i < len(community.Members) && pairsGenerated < config.MaxInferredPerCommunity; i++ {
			for j := i + 1; j < len(community.Members) && pairsGenerated < config.MaxInferredPerCommunity; j++ {
				entityA := community.Members[i]
				entityB := community.Members[j]

				// Skip if explicit edge exists (don't duplicate)
				if d.hasExplicitEdge(ctx, entityA, entityB) {
					continue
				}

				// Calculate confidence: base 0.5 + tightness bonus (0.0-0.3)
				confidence := 0.5 + (tightness * 0.3)

				// Create bidirectional triples
				triples = append(triples,
					InferredTriple{
						Subject:     entityA,
						Predicate:   "inferred.cluster.clustered-with",
						Object:      entityB,
						Source:      "lpa_community_detection",
						Confidence:  confidence,
						Timestamp:   now,
						CommunityID: community.ID,
						Level:       level,
					},
					InferredTriple{
						Subject:     entityB,
						Predicate:   "inferred.cluster.clustered-with",
						Object:      entityA,
						Source:      "lpa_community_detection",
						Confidence:  confidence,
						Timestamp:   now,
						CommunityID: community.ID,
						Level:       level,
					},
				)
				pairsGenerated++
			}
		}
	}

	return triples, nil
}

// InferredTriple represents a relationship inferred from community detection.
// This is a lightweight struct for returning inference results.
// The caller converts these to message.Triple for persistence.
type InferredTriple struct {
	Subject     string
	Predicate   string
	Object      string
	Source      string
	Confidence  float64
	Timestamp   time.Time
	CommunityID string // Community that produced this inference
	Level       int    // Hierarchical level
}

// computeCommunityTightness computes how tightly connected a community is.
// Returns a value between 0.0 (loose) and 1.0 (very tight) as explicit-edge density.
func (d *LPADetector) computeCommunityTightness(ctx context.Context, community *Community) float64 {
	if len(community.Members) < 2 {
		return 0.0
	}

	// Count explicit edges vs possible edges
	explicitEdges := 0
	possibleEdges := 0

	for i := 0; i < len(community.Members); i++ {
		for j := i + 1; j < len(community.Members); j++ {
			possibleEdges++
			weight, _ := d.graphProvider.GetEdgeWeight(ctx, community.Members[i], community.Members[j])
			if weight > 0 {
				explicitEdges++
			}
		}
	}

	if possibleEdges == 0 {
		return 0.0
	}

	// Return edge density as tightness measure
	return float64(explicitEdges) / float64(possibleEdges)
}

// hasExplicitEdge checks if there's already an explicit edge between two entities.
// Returns true if edge exists (to avoid creating duplicate inferred relationships).
func (d *LPADetector) hasExplicitEdge(ctx context.Context, entityA, entityB string) bool {
	// Check both directions
	weightAB, _ := d.graphProvider.GetEdgeWeight(ctx, entityA, entityB)
	if weightAB >= 0.8 { // Only count high-confidence edges as "explicit"
		return true
	}
	weightBA, _ := d.graphProvider.GetEdgeWeight(ctx, entityB, entityA)
	return weightBA >= 0.8
}

// transferSummary transfers an LLM summary from an archived community to a new one
// if their membership overlap exceeds the threshold (using Jaccard index).
// Uses best-match logic: if multiple archived communities exceed threshold, picks the one
// with highest overlap to ensure the most relevant summary is transferred.
// Returns true if a summary was transferred.
func transferSummary(newComm *Community, archived []*Community, threshold float64) bool {
	var bestMatch *Community
	var bestOverlap float64

	for _, old := range archived {
		// Must be same level
		if old.Level != newComm.Level {
			continue
		}

		overlap := jaccardIndex(newComm.Members, old.Members)
		if overlap >= threshold && overlap > bestOverlap {
			bestMatch = old
			bestOverlap = overlap
		}
	}

	if bestMatch == nil {
		return false
	}

	// Transfer the LLM summary from best match
	newComm.LLMSummary = bestMatch.LLMSummary
	newComm.SummaryStatus = "llm-enhanced"

	// Initialize metadata if nil (atomic assignment pattern)
	metadata := newComm.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["summary_transferred_from"] = bestMatch.ID
	metadata["membership_overlap"] = bestOverlap
	newComm.Metadata = metadata

	return true
}

// jaccardIndex computes the Jaccard similarity index between two sets of members.
// Jaccard index = |A ∩ B| / |A ∪ B|
// Returns 0.0 if both sets are empty, otherwise a value between 0.0 and 1.0.
func jaccardIndex(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]bool, len(a))
	for _, id := range a {
		setA[id] = true
	}

	intersection := 0
	for _, id := range b {
		if setA[id] {
			intersection++
		}
	}

	union := len(setA) + len(b) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}
