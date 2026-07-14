package inference

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
)

// SemanticGapDetector detects entities that are semantically similar
// but structurally distant in the graph.
type SemanticGapDetector struct {
	config SemanticGapConfig
	deps   *DetectorDependencies
	logger *slog.Logger
}

// NewSemanticGapDetector creates a new semantic gap detector.
func NewSemanticGapDetector(deps *DetectorDependencies) *SemanticGapDetector {
	logger := slog.Default()
	if deps != nil && deps.Logger != nil {
		logger = deps.Logger
	}

	return &SemanticGapDetector{
		config: DefaultConfig().SemanticGap,
		deps:   deps,
		logger: logger,
	}
}

// Name returns the detector identifier.
func (d *SemanticGapDetector) Name() string {
	return "semantic_gap"
}

// Configure updates the detector configuration.
func (d *SemanticGapDetector) Configure(config interface{}) error {
	if cfg, ok := config.(SemanticGapConfig); ok {
		d.config = cfg
		return nil
	}
	return errs.WrapInvalid(errs.ErrInvalidConfig, "SemanticGapDetector", "Configure",
		"expected SemanticGapConfig")
}

// SetDependencies updates the detector's dependencies.
func (d *SemanticGapDetector) SetDependencies(deps *DetectorDependencies) {
	d.deps = deps
	if deps != nil && deps.Logger != nil {
		d.logger = deps.Logger
	}
}

// Detect finds semantic-structural gaps in the graph.
func (d *SemanticGapDetector) Detect(ctx context.Context) ([]*StructuralAnomaly, error) {
	if !d.config.Enabled {
		return nil, nil
	}

	if err := d.validateDependencies(); err != nil {
		return nil, err
	}

	pivotIndex := d.deps.StructuralIndices.Pivot
	if pivotIndex == nil {
		d.logger.Warn("pivot index not available, skipping semantic gap detection")
		return nil, nil
	}

	// Get all entities from the pivot index
	entities := d.getEntitiesFromIndex(pivotIndex)
	if len(entities) == 0 {
		d.logger.Debug("no entities in pivot index")
		return nil, nil
	}

	d.logger.Debug("starting semantic gap detection", "entities", len(entities))

	anomalies := make([]*StructuralAnomaly, 0)
	seen := make(map[string]bool) // Track seen pairs to avoid duplicates

	for _, entityA := range entities {
		select {
		case <-ctx.Done():
			d.logger.Debug("semantic gap detection cancelled")
			return anomalies, ctx.Err()
		default:
		}

		entityAnomalies := d.detectGapsForEntity(ctx, entityA, pivotIndex, seen)
		anomalies = append(anomalies, entityAnomalies...)
	}

	d.logger.Debug("semantic gap detection complete", "anomalies", len(anomalies))
	return anomalies, nil
}

// validateDependencies ensures required dependencies are available.
func (d *SemanticGapDetector) validateDependencies() error {
	if d.deps == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "SemanticGapDetector", "validateDependencies",
			"dependencies not set")
	}
	if d.deps.StructuralIndices == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "SemanticGapDetector", "validateDependencies",
			"structural indices not available")
	}
	if d.deps.SimilarityFinder == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "SemanticGapDetector", "validateDependencies",
			"similarity finder not available")
	}
	return nil
}

// getEntitiesFromIndex extracts entity IDs from the pivot index.
func (d *SemanticGapDetector) getEntitiesFromIndex(pivotIndex *structural.PivotIndex) []string {
	entities := make([]string, 0, len(pivotIndex.DistanceVectors))
	for entityID := range pivotIndex.DistanceVectors {
		entities = append(entities, entityID)
	}
	return entities
}

// detectGapsForEntity finds semantic-structural gaps for a single entity.
func (d *SemanticGapDetector) detectGapsForEntity(
	ctx context.Context,
	entityA string,
	pivotIndex *structural.PivotIndex,
	seen map[string]bool,
) []*StructuralAnomaly {
	// Find semantically similar entities
	similar, err := d.deps.SimilarityFinder.FindSimilar(
		ctx,
		entityA,
		d.config.MinSemanticSimilarity,
		d.config.MaxCandidatesPerEntity,
	)
	if err != nil {
		d.logger.Debug("failed to find similar entities", "entity", entityA, "error", err)
		return nil
	}

	// Score and filter gaps
	gaps := make([]semanticGap, 0)

	for _, sim := range similar {
		entityB := sim.EntityID

		// Skip self
		if entityB == entityA {
			continue
		}

		// Skip already seen pairs (check both directions)
		pairKey := makePairKey(entityA, entityB)
		if seen[pairKey] {
			continue
		}
		seen[pairKey] = true

		// Skip pairs that have been previously dismissed or auto-applied
		if d.isDismissedPair(ctx, entityA, entityB) {
			continue
		}

		// Check structural distance
		lower, upper := pivotIndex.EstimateDistance(entityA, entityB)

		// If lower bound is less than min structural distance, not a gap
		if lower < d.config.MinStructuralDistance {
			continue
		}

		// Calculate confidence score
		confidence := d.calculateConfidence(sim.Similarity, lower, entityA, entityB)

		gaps = append(gaps, semanticGap{
			entityA:       entityA,
			entityB:       entityB,
			similarity:    sim.Similarity,
			distanceLower: lower,
			distanceUpper: upper,
			confidence:    confidence,
		})
	}

	// Sort by confidence and take top N
	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].confidence > gaps[j].confidence
	})

	maxGaps := d.config.MaxGapsPerEntity
	if len(gaps) > maxGaps {
		gaps = gaps[:maxGaps]
	}

	// Convert to anomalies
	anomalies := make([]*StructuralAnomaly, len(gaps))
	for i, gap := range gaps {
		anomalies[i] = d.createAnomaly(gap)
	}

	return anomalies
}

// semanticGap represents a detected semantic-structural gap.
type semanticGap struct {
	entityA       string
	entityB       string
	similarity    float64
	distanceLower int
	distanceUpper int
	confidence    float64
}

// calculateConfidence computes the confidence score for a gap.
func (d *SemanticGapDetector) calculateConfidence(
	similarity float64,
	distanceLower int,
	entityA, entityB string,
) float64 {
	// Base confidence from similarity score
	base := similarity

	// Boost for higher structural distance (up to 0.2 boost)
	distanceBoost := math.Min(float64(distanceLower-d.config.MinStructuralDistance)/10.0, 0.2)

	// Boost if both entities are in high k-core (indicates importance)
	coreBoost := 0.0
	if d.deps.StructuralIndices.KCore != nil {
		kcore := d.deps.StructuralIndices.KCore
		coreA := kcore.GetCore(entityA)
		coreB := kcore.GetCore(entityB)
		if coreA >= 2 && coreB >= 2 {
			coreBoost = 0.1
		}
	}

	return math.Min(base+distanceBoost+coreBoost, 1.0)
}

// createAnomaly creates a StructuralAnomaly from a detected gap.
func (d *SemanticGapDetector) createAnomaly(gap semanticGap) *StructuralAnomaly {
	now := time.Now()

	evidence := Evidence{
		Similarity:         gap.similarity,
		StructuralDistance: gap.distanceLower,
		DistanceLowerBound: gap.distanceLower,
		DistanceUpperBound: gap.distanceUpper,
	}

	// Add k-core info if available (use CoreLevel for the primary entity)
	if d.deps.StructuralIndices.KCore != nil {
		kcore := d.deps.StructuralIndices.KCore
		// Store primary entity's core level in CoreLevel field
		evidence.CoreLevel = kcore.GetCore(gap.entityA)
	}

	// Generate suggested predicate based on entity types if possible
	predicate := "inferred.semantic.related-to"

	return &StructuralAnomaly{
		ID:         uuid.New().String(),
		Type:       AnomalySemanticStructuralGap,
		EntityA:    gap.entityA,
		EntityB:    gap.entityB,
		Confidence: gap.confidence,
		Evidence:   evidence,
		Suggestion: &RelationshipSuggestion{
			FromEntity: gap.entityA,
			ToEntity:   gap.entityB,
			Predicate:  predicate,
			Confidence: gap.confidence,
			Reasoning:  fmt.Sprintf("High semantic similarity (%.2f) but structurally distant (%d+ hops)", gap.similarity, gap.distanceLower),
		},
		Status:     StatusPending,
		DetectedAt: now,
	}
}

// isDismissedPair checks if an entity pair has been previously dismissed or auto-applied.
func (d *SemanticGapDetector) isDismissedPair(ctx context.Context, entityA, entityB string) bool {
	if d.deps == nil || d.deps.AnomalyStorage == nil {
		return false
	}

	dismissed, err := d.deps.AnomalyStorage.IsDismissedPair(ctx, entityA, entityB)
	if err != nil {
		d.logger.Debug("failed to check dismissed pair", "entityA", entityA, "entityB", entityB, "error", err)
		return false
	}
	return dismissed
}

// makePairKey creates a canonical key for an entity pair (order-independent).
func makePairKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}
