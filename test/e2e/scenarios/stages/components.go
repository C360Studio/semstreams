// Package stages contains extracted stage implementations for tiered E2E tests
package stages

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// ComponentVerifier handles component verification stages
type ComponentVerifier struct {
	Client  *client.ObservabilityClient
	Variant string
}

// VerifyComponents checks that all required components are registered
func (v *ComponentVerifier) VerifyComponents(ctx context.Context) (*ComponentResult, error) {
	components, err := v.Client.GetComponents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get components: %w", err)
	}

	required := v.getRequiredComponents()

	foundComponents := make(map[string]bool)
	for _, comp := range components {
		foundComponents[comp.Name] = true
	}

	var missing []string
	for _, req := range required {
		if !foundComponents[req] {
			missing = append(missing, req)
		}
	}

	result := &ComponentResult{
		Variant:  v.Variant,
		Required: required,
		Found:    len(components),
		Missing:  missing,
	}

	if len(missing) > 0 {
		return result, fmt.Errorf("missing components: %v", missing)
	}

	return result, nil
}

// getRequiredComponents returns the list of required components for the variant
func (v *ComponentVerifier) getRequiredComponents() []string {
	if v.Variant == "structural" {
		// Components for structural testing with rule processor
		// Note: "rule" is the instance name (config key), "rule-processor" is the factory name
		return []string{"udp", "iot_sensor", "rule", "graph-ingest", "file"}
	}

	// Full components for statistical/semantic tiers
	return []string{
		// Input
		"udp",
		// Domain processors
		"document_processor", "iot_sensor",
		// Semantic components
		"graph-ingest",
		// Output/storage
		"file", "objectstore",
		// Stateful rule evaluation is part of every tier.
		"rule",
	}
}

// ComponentResult contains the results of component verification
type ComponentResult struct {
	Variant  string   `json:"variant"`
	Required []string `json:"required"`
	Found    int      `json:"found"`
	Missing  []string `json:"missing,omitempty"`
}

// VerifyOutputs checks that all output components are present and healthy
func (v *ComponentVerifier) VerifyOutputs(ctx context.Context) (*OutputResult, error) {
	components, err := v.Client.GetComponents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get components: %w", err)
	}

	expectedOutputs := []string{"file", "objectstore"}
	foundOutputs := make(map[string]bool)

	for _, comp := range components {
		for _, expected := range expectedOutputs {
			if comp.Name == expected {
				foundOutputs[comp.Name] = true
				break
			}
		}
	}

	var missing []string
	for _, expected := range expectedOutputs {
		if !foundOutputs[expected] {
			missing = append(missing, expected)
		}
	}

	result := &OutputResult{
		Expected:   expectedOutputs,
		Found:      len(foundOutputs),
		Missing:    missing,
		AllHealthy: len(missing) == 0,
	}

	if len(missing) > 0 {
		return result, fmt.Errorf("missing output components: %v", missing)
	}

	return result, nil
}

// OutputResult contains the results of output verification
type OutputResult struct {
	Expected   []string `json:"expected"`
	Found      int      `json:"found"`
	Missing    []string `json:"missing,omitempty"`
	AllHealthy bool     `json:"all_healthy"`
}
