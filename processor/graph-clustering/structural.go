// Package graphclustering provides structural analysis integration for graph-clustering.
package graphclustering

import (
	"context"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/pkg/errs"
)

// runStructuralComputation computes k-core and pivot indices.
// The results are ephemeral same-cycle anomaly inputs and are never persisted.
func (c *Component) runStructuralComputation(ctx context.Context) (*structural.KCoreIndex, *structural.PivotIndex, error) {
	if c.graphProvider == nil {
		return nil, nil, errs.WrapInvalid(errs.ErrMissingConfig, "Component", "runStructuralComputation", "graph provider is not initialized")
	}

	c.logger.Debug("running structural computation")
	start := time.Now()

	// Compute k-core index
	kcoreComputer := structural.NewKCoreComputer(c.graphProvider, c.logger)
	kcoreIndex, err := kcoreComputer.Compute(ctx)
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "runStructuralComputation", "k-core computation")
	}

	c.logger.Debug("k-core computation complete",
		slog.Int("entity_count", kcoreIndex.EntityCount),
		slog.Int("max_core", kcoreIndex.MaxCore))

	// Compute pivot index
	pivotComputer := structural.NewPivotComputer(c.graphProvider, structural.DefaultPivotCount, c.logger)
	pivotIndex, err := pivotComputer.Compute(ctx)
	if err != nil {
		return nil, nil, errs.Wrap(err, "Component", "runStructuralComputation", "pivot computation")
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
