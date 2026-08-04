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
