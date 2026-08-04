package stages

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// IndexVerifier handles index population verification
type IndexVerifier struct {
	NATSClient *client.NATSValidationClient
}

// IndexSpec defines an index to verify
type IndexSpec struct {
	Name     string `json:"name"`
	Bucket   string `json:"bucket"`
	Required bool   `json:"required"`
}

// DefaultIndexSpecs returns the standard indexes to verify
func DefaultIndexSpecs() []IndexSpec {
	return []IndexSpec{
		{"entity_states", client.IndexBuckets.EntityStates, true},
		{"predicate", client.IndexBuckets.Predicate, true},
		{"incoming", client.IndexBuckets.Incoming, true},
		{"outgoing", client.IndexBuckets.Outgoing, true},
		{"alias", client.IndexBuckets.Alias, false},     // May be empty if no aliases
		{"spatial", client.IndexBuckets.Spatial, false}, // May be empty if no geo data
		{"temporal", client.IndexBuckets.Temporal, true},
		{"structural", client.IndexBuckets.Structural, false}, // Only populated for Tier 1+ (statistical/semantic)
	}
}

// IndexDetail contains details about a single index
type IndexDetail struct {
	Bucket     string   `json:"bucket"`
	KeyCount   int      `json:"key_count"`
	Populated  bool     `json:"populated"`
	SampleKeys []string `json:"sample_keys,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// IndexPopulationResult contains the results of index population verification
type IndexPopulationResult struct {
	Populated     int                    `json:"populated"`
	Total         int                    `json:"total"`
	EmptyRequired []string               `json:"empty_required,omitempty"`
	Indexes       map[string]IndexDetail `json:"indexes"`
	Warnings      []string               `json:"warnings,omitempty"`
}

// VerifyIndexPopulation checks that core indexes are populated
func (v *IndexVerifier) VerifyIndexPopulation(ctx context.Context, specs []IndexSpec) (*IndexPopulationResult, error) {
	if v.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not available")
	}

	result := &IndexPopulationResult{
		Total:   len(specs),
		Indexes: make(map[string]IndexDetail),
	}

	for _, spec := range specs {
		detail := IndexDetail{
			Bucket: spec.Bucket,
		}

		count, err := v.NATSClient.CountBucketKeys(ctx, spec.Bucket)
		if err != nil {
			detail.Error = err.Error()
			detail.Populated = false
			if spec.Required {
				result.EmptyRequired = append(result.EmptyRequired, spec.Name)
			}
			result.Indexes[spec.Name] = detail
			continue
		}

		detail.KeyCount = count
		detail.Populated = count > 0

		if detail.Populated {
			result.Populated++
			// Get sample keys for debugging
			if sampleKeys, err := v.NATSClient.GetBucketKeysSample(ctx, spec.Bucket, 3); err == nil {
				detail.SampleKeys = sampleKeys
			}
		} else if spec.Required {
			result.EmptyRequired = append(result.EmptyRequired, spec.Name)
		}

		result.Indexes[spec.Name] = detail
	}

	if len(result.EmptyRequired) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Required indexes empty: %v", result.EmptyRequired))
	}

	return result, nil
}

// StructuralIndexVerifier handles structural index verification (k-core, pivot)
// for tier 0+ scenarios
type StructuralIndexVerifier struct {
	NATSClient *client.NATSValidationClient
}

// StructuralIndexResult contains the results of structural index verification
type StructuralIndexResult struct {
	BucketExists bool                  `json:"bucket_exists"`
	KeyCount     int                   `json:"key_count"`
	KCore        *client.KCoreMetadata `json:"kcore,omitempty"`
	Pivot        *client.PivotMetadata `json:"pivot,omitempty"`
	KCoreValid   bool                  `json:"kcore_valid"`
	PivotValid   bool                  `json:"pivot_valid"`
	SampleKeys   []string              `json:"sample_keys,omitempty"`
	Warnings     []string              `json:"warnings,omitempty"`
	Errors       []string              `json:"errors,omitempty"`
}

// VerifyStructuralIndexes verifies k-core decomposition and pivot distance indexing
// Available for tier 0+ (structural tier and above)
func (v *StructuralIndexVerifier) VerifyStructuralIndexes(ctx context.Context) (*StructuralIndexResult, error) {
	if v.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not available")
	}

	result := &StructuralIndexResult{}

	// Get structural index info from NATS
	info, err := v.NATSClient.GetStructuralIndexInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get structural index info: %w", err)
	}

	result.BucketExists = info.BucketExists
	result.KeyCount = info.KeyCount
	result.SampleKeys = info.SampleKeys
	result.KCore = info.KCore
	result.Pivot = info.Pivot

	// Validate k-core index
	if info.KCore != nil {
		result.KCoreValid = v.validateKCore(info.KCore, result)
	} else {
		result.Warnings = append(result.Warnings, "K-core index metadata not found")
	}

	// Validate pivot index
	if info.Pivot != nil {
		result.PivotValid = v.validatePivot(info.Pivot, result)
	} else {
		result.Warnings = append(result.Warnings, "Pivot index metadata not found")
	}

	return result, nil
}

// validateKCore validates the k-core index metadata
func (v *StructuralIndexVerifier) validateKCore(kcore *client.KCoreMetadata, result *StructuralIndexResult) bool {
	valid := true

	// Check entity count is reasonable
	if kcore.EntityCount == 0 {
		result.Errors = append(result.Errors, "K-core has 0 entities")
		valid = false
	}

	// MaxCore should be >= 0
	if kcore.MaxCore < 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("Invalid MaxCore: %d", kcore.MaxCore))
		valid = false
	}

	// Core buckets should sum to entity count
	totalInBuckets := 0
	for _, count := range kcore.CoreBuckets {
		totalInBuckets += count
	}
	if totalInBuckets > 0 && totalInBuckets != kcore.EntityCount {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Core buckets sum (%d) != entity count (%d)", totalInBuckets, kcore.EntityCount))
	}

	// Computed timestamp should be present
	if kcore.ComputedAt == "" {
		result.Warnings = append(result.Warnings, "K-core ComputedAt timestamp missing")
	}

	return valid
}

// validatePivot validates the pivot index metadata
func (v *StructuralIndexVerifier) validatePivot(pivot *client.PivotMetadata, result *StructuralIndexResult) bool {
	valid := true

	// Check we have pivots
	if len(pivot.Pivots) == 0 {
		result.Errors = append(result.Errors, "Pivot index has no pivots")
		valid = false
	}

	// Check entity count is reasonable
	if pivot.EntityCount == 0 {
		result.Errors = append(result.Errors, "Pivot index has 0 entities")
		valid = false
	}

	// Pivots should be non-empty strings
	for i, p := range pivot.Pivots {
		if p == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("Pivot %d is empty", i))
			valid = false
		}
	}

	// Computed timestamp should be present
	if pivot.ComputedAt == "" {
		result.Warnings = append(result.Warnings, "Pivot ComputedAt timestamp missing")
	}

	return valid
}
