package inference

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/internal/semantictest"
)

// mockRelationshipQuerier implements RelationshipQuerier for testing.
type mockRelationshipQuerier struct {
	outgoing map[string][]RelationshipInfo
	incoming map[string][]RelationshipInfo
}

func (m *mockRelationshipQuerier) GetOutgoingRelationships(_ context.Context, entityID string) ([]RelationshipInfo, error) {
	return m.outgoing[entityID], nil
}

func (m *mockRelationshipQuerier) GetIncomingRelationships(_ context.Context, entityID string) ([]RelationshipInfo, error) {
	return m.incoming[entityID], nil
}

func TestTransitivityDetector_Name(t *testing.T) {
	detector := NewTransitivityDetector(nil)
	if detector.Name() != "transitivity" {
		t.Errorf("expected name 'transitivity', got %s", detector.Name())
	}
}

func TestTransitivityDetector_Configure(t *testing.T) {
	detector := NewTransitivityDetector(nil)

	cfg := TransitivityConfig{
		Enabled:                 true,
		MaxIntermediateHops:     3,
		MinExpectedTransitivity: 2,
		TransitivePredicates:    []string{"worksFor", "memberOf"},
	}

	err := detector.Configure(cfg)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if !detector.config.Enabled {
		t.Error("expected config.Enabled to be true")
	}
	if detector.config.MaxIntermediateHops != 3 {
		t.Errorf("expected MaxIntermediateHops=3, got %d", detector.config.MaxIntermediateHops)
	}
	if len(detector.config.TransitivePredicates) != 2 {
		t.Errorf("expected 2 predicates, got %d", len(detector.config.TransitivePredicates))
	}
}

func TestTransitivityDetector_Configure_InvalidType(t *testing.T) {
	detector := NewTransitivityDetector(nil)

	err := detector.Configure("invalid")
	if err == nil {
		t.Error("expected error for invalid config type")
	}
}

func TestTransitivityDetector_Detect_Disabled(t *testing.T) {
	detector := NewTransitivityDetector(nil)
	detector.config.Enabled = false

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if len(anomalies) != 0 {
		t.Errorf("expected no anomalies when disabled, got %d", len(anomalies))
	}
}

func TestTransitivityDetector_Detect_NoDependencies(t *testing.T) {
	detector := NewTransitivityDetector(nil)
	detector.config.Enabled = true

	_, err := detector.Detect(context.Background())
	if err == nil {
		t.Error("expected error when dependencies not set")
	}
}

func TestTransitivityDetector_Detect_NoPredicates(t *testing.T) {
	querier := &mockRelationshipQuerier{}
	pivotIndex := &structural.PivotIndex{
		Pivots:          []string{"pivot1"},
		DistanceVectors: map[string][]int{"A": {0}, "B": {1}, "C": {2}},
	}

	detector := NewTransitivityDetector(&DetectorDependencies{
		StructuralIndices:   &structural.Indices{Pivot: pivotIndex},
		RelationshipQuerier: querier,
	})
	detector.config.Enabled = true
	detector.config.TransitivePredicates = []string{} // No predicates

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if len(anomalies) != 0 {
		t.Errorf("expected no anomalies with no predicates, got %d", len(anomalies))
	}
}

func TestTransitivityDetector_Detect_FindsGaps(t *testing.T) {
	// Graph: A --worksFor--> B --worksFor--> C
	// No direct A --> C relationship
	// This should be detected as a transitivity gap

	entityA := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "a")
	entityB := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "b")
	entityC := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "c")
	pivotID := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "pivot", "001")
	querier := &mockRelationshipQuerier{
		outgoing: map[string][]RelationshipInfo{
			entityA: {{FromEntityID: entityA, ToEntityID: entityB, Predicate: "worksFor"}},
			entityB: {{FromEntityID: entityB, ToEntityID: entityC, Predicate: "worksFor"}},
			entityC: {},
		},
		incoming: map[string][]RelationshipInfo{
			entityA: {},
			entityB: {{FromEntityID: entityA, ToEntityID: entityB, Predicate: "worksFor"}},
			entityC: {{FromEntityID: entityB, ToEntityID: entityC, Predicate: "worksFor"}},
		},
	}

	// Set up pivot index where A-C distance is 3 (greater than expected)
	pivotIndex := &structural.PivotIndex{
		Pivots: []string{pivotID},
		DistanceVectors: map[string][]int{
			entityA: {0},
			entityB: {1},
			entityC: {3}, // Distance suggests gap
			pivotID: {0},
		},
		ComputedAt:  time.Now(),
		EntityCount: 4,
	}

	detector := NewTransitivityDetector(&DetectorDependencies{
		StructuralIndices:   &structural.Indices{Pivot: pivotIndex},
		RelationshipQuerier: querier,
	})

	err := detector.Configure(TransitivityConfig{
		Enabled:                 true,
		MaxIntermediateHops:     3,
		MinExpectedTransitivity: 1, // Expect transitivity within 1 hop
		TransitivePredicates:    []string{"worksFor"},
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Should find at least one gap (A->C)
	if len(anomalies) == 0 {
		t.Fatal("expected at least one transitivity gap anomaly")
	}

	found := false
	for _, a := range anomalies {
		if a.Type == AnomalyTransitivityGap && a.EntityA == entityA && a.EntityB == entityC {
			found = true

			// Verify suggestion is populated
			if a.Suggestion == nil {
				t.Error("expected Suggestion to be populated")
			} else {
				if a.Suggestion.FromEntity != entityA {
					t.Errorf("expected Suggestion.FromEntity='A', got %s", a.Suggestion.FromEntity)
				}
				if a.Suggestion.ToEntity != entityC {
					t.Errorf("expected Suggestion.ToEntity='C', got %s", a.Suggestion.ToEntity)
				}
				if a.Suggestion.Predicate != "worksFor" {
					t.Errorf("expected Suggestion.Predicate='worksFor', got %s", a.Suggestion.Predicate)
				}
				if a.Suggestion.Reasoning == "" {
					t.Error("expected Suggestion.Reasoning to be non-empty")
				}
			}

			// Verify evidence
			if a.Evidence.Predicate != "worksFor" {
				t.Errorf("expected Evidence.Predicate='worksFor', got %s", a.Evidence.Predicate)
			}
			if len(a.Evidence.ChainPath) < 3 {
				t.Errorf("expected ChainPath with at least 3 nodes, got %v", a.Evidence.ChainPath)
			}
		}
	}

	if !found {
		t.Error("expected to find A->C transitivity gap")
	}
}

func TestTransitivityDetector_Detect_FiltersPredicates(t *testing.T) {
	// Graph has relationships with different predicates
	// Only "worksFor" is configured as transitive
	// "friendOf" should be ignored

	entityA := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "a")
	entityB := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "b")
	entityC := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "c")
	entityD := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "d")
	entityE := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "e")
	pivotID := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "pivot", "001")
	querier := &mockRelationshipQuerier{
		outgoing: map[string][]RelationshipInfo{
			entityA: {
				{FromEntityID: entityA, ToEntityID: entityB, Predicate: "friendOf"},
				{FromEntityID: entityA, ToEntityID: entityD, Predicate: "worksFor"},
			},
			entityB: {{FromEntityID: entityB, ToEntityID: entityC, Predicate: "friendOf"}},
			entityD: {{FromEntityID: entityD, ToEntityID: entityE, Predicate: "worksFor"}},
			entityC: {},
			entityE: {},
		},
	}

	pivotIndex := &structural.PivotIndex{
		Pivots: []string{pivotID},
		DistanceVectors: map[string][]int{
			entityA: {0},
			entityB: {1},
			entityC: {5}, // High distance, but friendOf is not transitive
			entityD: {1},
			entityE: {5}, // High distance, worksFor IS transitive
			pivotID: {0},
		},
		ComputedAt:  time.Now(),
		EntityCount: 6,
	}

	detector := NewTransitivityDetector(&DetectorDependencies{
		StructuralIndices:   &structural.Indices{Pivot: pivotIndex},
		RelationshipQuerier: querier,
	})

	err := detector.Configure(TransitivityConfig{
		Enabled:                 true,
		MaxIntermediateHops:     3,
		MinExpectedTransitivity: 1,
		TransitivePredicates:    []string{"worksFor"}, // Only worksFor
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Should only find worksFor gaps, not friendOf gaps
	for _, a := range anomalies {
		if a.Evidence.Predicate == "friendOf" {
			t.Error("should not detect gaps for non-transitive predicate 'friendOf'")
		}
	}
}

func TestTransitivityDetector_Detect_RespectsMinExpectedTransitivity(t *testing.T) {
	// Graph: A --worksFor--> B --worksFor--> C
	// If A-C distance is <= MinExpectedTransitivity, no gap should be flagged

	entityA := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "a")
	entityB := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "b")
	entityC := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "c")
	pivotID := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "pivot", "001")
	querier := &mockRelationshipQuerier{
		outgoing: map[string][]RelationshipInfo{
			entityA: {{FromEntityID: entityA, ToEntityID: entityB, Predicate: "worksFor"}},
			entityB: {{FromEntityID: entityB, ToEntityID: entityC, Predicate: "worksFor"}},
			entityC: {},
		},
	}

	// A-C distance is 1, which is <= MinExpectedTransitivity of 2
	pivotIndex := &structural.PivotIndex{
		Pivots: []string{pivotID},
		DistanceVectors: map[string][]int{
			entityA: {0},
			entityB: {1},
			entityC: {1}, // Close distance - no gap
			pivotID: {0},
		},
		ComputedAt:  time.Now(),
		EntityCount: 4,
	}

	detector := NewTransitivityDetector(&DetectorDependencies{
		StructuralIndices:   &structural.Indices{Pivot: pivotIndex},
		RelationshipQuerier: querier,
	})

	err := detector.Configure(TransitivityConfig{
		Enabled:                 true,
		MaxIntermediateHops:     3,
		MinExpectedTransitivity: 2, // Distance 1 is within expected
		TransitivePredicates:    []string{"worksFor"},
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Should not find gaps since distance is within expected
	if len(anomalies) != 0 {
		t.Errorf("expected no anomalies when distance is within threshold, got %d", len(anomalies))
	}
}

func TestTransitivityDetector_Detect_ContextCancellation(t *testing.T) {
	entityA := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "a")
	entityB := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "b")
	entityC := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "node", "c")
	pivotID := semantictest.EntityID(t, "test", "inference", "graph", "transitivity", "pivot", "001")
	querier := &mockRelationshipQuerier{
		outgoing: map[string][]RelationshipInfo{
			entityA: {{FromEntityID: entityA, ToEntityID: entityB, Predicate: "worksFor"}},
			entityB: {{FromEntityID: entityB, ToEntityID: entityC, Predicate: "worksFor"}},
			entityC: {},
		},
	}

	pivotIndex := &structural.PivotIndex{
		Pivots:          []string{pivotID},
		DistanceVectors: map[string][]int{entityA: {0}, entityB: {1}, entityC: {5}, pivotID: {0}},
	}

	detector := NewTransitivityDetector(&DetectorDependencies{
		StructuralIndices:   &structural.Indices{Pivot: pivotIndex},
		RelationshipQuerier: querier,
	})

	err := detector.Configure(TransitivityConfig{
		Enabled:                 true,
		MaxIntermediateHops:     3,
		MinExpectedTransitivity: 1,
		TransitivePredicates:    []string{"worksFor"},
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	// Cancel context before detection
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = detector.Detect(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestTransitivityDetector_SetDependencies(t *testing.T) {
	detector := NewTransitivityDetector(nil)

	if detector.deps != nil {
		t.Error("expected nil deps initially")
	}

	querier := &mockRelationshipQuerier{}
	deps := &DetectorDependencies{
		RelationshipQuerier: querier,
	}

	detector.SetDependencies(deps)

	if detector.deps != deps {
		t.Error("expected deps to be set")
	}
}
