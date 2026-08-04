// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios/stages"
)

// Structural index validation functions for tiered E2E tests

// executeVerifyIndexPopulation validates that all 7 core indexes are populated
func (s *TieredScenario) executeVerifyIndexPopulation(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping index population verification")
		return nil
	}

	// Core indexes that should be populated
	indexes := []struct {
		name     string
		bucket   string
		required bool
	}{
		{"entity_states", client.IndexBuckets.EntityStates, true},
		{"predicate", client.IndexBuckets.Predicate, true},
		{"incoming", client.IndexBuckets.Incoming, true},
		{"outgoing", client.IndexBuckets.Outgoing, true},
		{"alias", client.IndexBuckets.Alias, false},     // May be empty if no aliases
		{"spatial", client.IndexBuckets.Spatial, false}, // May be empty if no geo data
		{"temporal", client.IndexBuckets.Temporal, true},
	}

	indexDetails := make(map[string]any)
	populatedCount := 0
	emptyRequired := []string{}

	for _, idx := range indexes {
		count, err := s.natsClient.CountBucketKeys(ctx, idx.bucket)
		if err != nil {
			indexDetails[idx.name] = map[string]any{
				"bucket":    idx.bucket,
				"error":     err.Error(),
				"populated": false,
			}
			if idx.required {
				emptyRequired = append(emptyRequired, idx.name)
			}
			continue
		}

		populated := count > 0
		if populated {
			populatedCount++
		} else if idx.required {
			emptyRequired = append(emptyRequired, idx.name)
		}

		// Get sample keys for debugging
		sampleKeys, _ := s.natsClient.GetBucketKeysSample(ctx, idx.bucket, 3)

		indexDetails[idx.name] = map[string]any{
			"bucket":      idx.bucket,
			"key_count":   count,
			"populated":   populated,
			"sample_keys": sampleKeys,
		}
	}

	result.Metrics["indexes_populated"] = populatedCount
	result.Metrics["indexes_total"] = len(indexes)

	result.Details["index_population_verification"] = map[string]any{
		"indexes":        indexDetails,
		"populated":      populatedCount,
		"total":          len(indexes),
		"empty_required": emptyRequired,
		"message":        fmt.Sprintf("Populated %d/%d indexes", populatedCount, len(indexes)),
	}

	if len(emptyRequired) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Required indexes empty: %v", emptyRequired))
	}

	return nil
}

// executeVerifyStructuralIndexes validates k-core and pivot indexes (structural tier only)
func (s *TieredScenario) executeVerifyStructuralIndexes(ctx context.Context, result *Result) error {
	verifier := &stages.StructuralIndexVerifier{NATSClient: s.natsClient}
	indexResult, err := verifier.VerifyStructuralIndexes(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Structural index verification failed: %v", err))
		return nil
	}

	result.Details["structural_indexes"] = indexResult
	result.Warnings = append(result.Warnings, indexResult.Warnings...)
	if len(indexResult.Errors) > 0 {
		return fmt.Errorf("structural index errors: %v", indexResult.Errors)
	}
	return nil
}
