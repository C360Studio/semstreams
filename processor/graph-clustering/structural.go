// Package graphclustering provides structural analysis integration for graph-clustering.
package graphclustering

import (
	"context"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/pkg/errs"
)

// initStructural initializes structural analysis resources.
// Called during Start() when EnableStructural is true.
func (c *Component) initStructural(ctx context.Context) error {
	// STRUCTURAL_INDEX bucket (we are the WRITER) — acquired through the
	// catalog owner seam.
	structuralBucket, err := graph.EnsureCatalogBucket(ctx, c.natsClient, graph.BucketStructuralIndex)
	if err != nil {
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "initStructural", "context cancelled during bucket creation")
		}
		return errs.Wrap(err, "Component", "initStructural", "create STRUCTURAL_INDEX bucket")
	}
	c.structuralBucket = structuralBucket

	// Create storage for structural indices
	c.structuralStorage = structural.NewNATSStructuralIndexStorage(structuralBucket)

	c.logger.Info("structural analysis initialized",
		slog.Int("pivot_count", c.config.PivotCount),
		slog.Int("max_hop_distance", c.config.MaxHopDistance))

	return nil
}

// runStructuralComputation computes k-core and pivot indices.
// Called after community detection completes when EnableStructural is true.
// Returns the computed indices for use by anomaly detection.
func (c *Component) runStructuralComputation(ctx context.Context) (*structural.KCoreIndex, *structural.PivotIndex, error) {
	if c.structuralStorage == nil || c.graphProvider == nil {
		return nil, nil, nil // Not initialized
	}

	c.logger.Debug("running structural computation")
	start := time.Now()

	// Compute k-core index
	kcoreComputer := structural.NewKCoreComputer(c.graphProvider, c.logger)
	kcoreIndex, err := kcoreComputer.Compute(ctx)
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "runStructuralComputation", "k-core computation")
	}

	// Save k-core index
	if err := c.structuralStorage.SaveKCoreIndex(ctx, kcoreIndex); err != nil {
		return nil, nil, errs.Wrap(err, "Component", "runStructuralComputation", "save k-core index")
	}

	c.logger.Debug("k-core computation complete",
		slog.Int("entity_count", kcoreIndex.EntityCount),
		slog.Int("max_core", kcoreIndex.MaxCore))

	// Compute pivot index
	pivotComputer := structural.NewPivotComputer(c.graphProvider, c.config.PivotCount, c.logger)
	pivotIndex, err := pivotComputer.Compute(ctx)
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "runStructuralComputation", "pivot computation")
	}

	// Save pivot index
	if err := c.structuralStorage.SavePivotIndex(ctx, pivotIndex); err != nil {
		return nil, nil, errs.Wrap(err, "Component", "runStructuralComputation", "save pivot index")
	}

	c.logger.Debug("pivot computation complete",
		slog.Int("entity_count", pivotIndex.EntityCount),
		slog.Int("pivot_count", len(pivotIndex.Pivots)))

	c.logger.Debug("structural computation complete",
		slog.Duration("duration", time.Since(start)),
		slog.Int("entities", kcoreIndex.EntityCount),
		slog.Int("max_core", kcoreIndex.MaxCore),
		slog.Int("pivots", len(pivotIndex.Pivots)))

	return kcoreIndex, pivotIndex, nil
}
