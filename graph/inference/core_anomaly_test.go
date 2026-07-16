package inference

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/internal/semantictest"
)

func TestCoreAnomalyDetector_Name(t *testing.T) {
	detector := NewCoreAnomalyDetector(nil)
	if detector.Name() != "core_anomaly" {
		t.Errorf("expected name 'core_anomaly', got %s", detector.Name())
	}
}

func TestCoreAnomalyDetector_Configure(t *testing.T) {
	detector := NewCoreAnomalyDetector(nil)

	cfg := CoreAnomalyConfig{
		Enabled:               true,
		MinCoreForHubAnalysis: 3,
		HubIsolationThreshold: 0.5,
		TrackCoreDemotions:    true,
		MinDemotionDelta:      2,
	}

	err := detector.Configure(cfg)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if !detector.config.Enabled {
		t.Error("expected config.Enabled to be true")
	}
	if detector.config.MinCoreForHubAnalysis != 3 {
		t.Errorf("expected MinCoreForHubAnalysis=3, got %d", detector.config.MinCoreForHubAnalysis)
	}
}

func TestCoreAnomalyDetector_Configure_InvalidType(t *testing.T) {
	detector := NewCoreAnomalyDetector(nil)

	err := detector.Configure("invalid")
	if err == nil {
		t.Error("expected error for invalid config type")
	}
}

func TestCoreAnomalyDetector_Detect_Disabled(t *testing.T) {
	detector := NewCoreAnomalyDetector(nil)
	detector.config.Enabled = false

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	if len(anomalies) != 0 {
		t.Errorf("expected no anomalies when disabled, got %d", len(anomalies))
	}
}

func TestCoreAnomalyDetector_IsolationAnomaly_HasSuggestion(t *testing.T) {
	highCoreID := semantictest.EntityID(t, "test", "inference", "graph", "core", "entity", "high")
	lowCoreID := semantictest.EntityID(t, "test", "inference", "graph", "core", "entity", "low")
	// Create a mock k-core index with a high-core entity
	// Must set both CoreNumbers (for GetCore) and CoreBuckets (for GetEntitiesAboveCore)
	kcoreIndex := &structural.KCoreIndex{
		CoreNumbers: map[string]int{
			highCoreID: 5,
			lowCoreID:  1,
		},
		CoreBuckets: map[int][]string{
			5: {highCoreID},
			1: {lowCoreID},
		},
	}

	// Create a mock relationship querier that returns no same-core peers
	// This simulates core isolation - high k-core with few same-core neighbors
	querier := &mockRelationshipQuerier{
		outgoing: map[string][]RelationshipInfo{
			highCoreID: {
				{FromEntityID: highCoreID, ToEntityID: lowCoreID, Predicate: "knows"},
			},
		},
		incoming: map[string][]RelationshipInfo{},
	}

	deps := &DetectorDependencies{
		StructuralIndices:   &structural.Indices{KCore: kcoreIndex},
		RelationshipQuerier: querier,
	}

	detector := NewCoreAnomalyDetector(deps)
	err := detector.Configure(CoreAnomalyConfig{
		Enabled:               true,
		MinCoreForHubAnalysis: 3, // Entity with core 5 qualifies
		HubIsolationThreshold: 0.5,
		TrackCoreDemotions:    false,
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Should detect isolation anomaly for high-core-entity
	var isolationAnomaly *StructuralAnomaly
	for _, a := range anomalies {
		if a.Type == AnomalyCoreIsolation && a.EntityA == highCoreID {
			isolationAnomaly = a
			break
		}
	}

	if isolationAnomaly == nil {
		t.Fatal("expected to find core isolation anomaly for high-core-entity")
	}

	// Key assertion: Suggestion must be populated
	if isolationAnomaly.Suggestion == nil {
		t.Fatal("expected Suggestion to be populated for isolation anomaly")
	}

	// Verify Suggestion fields
	if isolationAnomaly.Suggestion.FromEntity != highCoreID {
		t.Errorf("expected Suggestion.FromEntity=%q, got %s", highCoreID, isolationAnomaly.Suggestion.FromEntity)
	}

	if isolationAnomaly.Suggestion.Predicate != "inference.suggested.peer" {
		t.Errorf("expected Suggestion.Predicate='inference.suggested.peer', got %s", isolationAnomaly.Suggestion.Predicate)
	}

	if isolationAnomaly.Suggestion.Reasoning == "" {
		t.Error("expected Suggestion.Reasoning to be non-empty")
	}

	// Verify reasoning mentions k-core level
	if !strings.Contains(isolationAnomaly.Suggestion.Reasoning, "k-core") {
		t.Error("expected Suggestion.Reasoning to mention k-core")
	}

	if isolationAnomaly.Suggestion.Confidence <= 0 {
		t.Error("expected Suggestion.Confidence to be positive")
	}
}

func TestCoreAnomalyDetector_DemotionAnomaly_HasSuggestion(t *testing.T) {
	// Create previous k-core index with entity at high core
	previousKCore := &structural.KCoreIndex{
		CoreNumbers: map[string]int{
			"demoted-entity": 5,
		},
	}

	// Create current k-core index with same entity at lower core
	currentKCore := &structural.KCoreIndex{
		CoreNumbers: map[string]int{
			"demoted-entity": 2, // Dropped from 5 to 2 (delta = 3)
		},
	}

	deps := &DetectorDependencies{
		StructuralIndices: &structural.Indices{KCore: currentKCore},
		PreviousKCore:     previousKCore,
	}

	detector := NewCoreAnomalyDetector(deps)
	err := detector.Configure(CoreAnomalyConfig{
		Enabled:               true,
		MinCoreForHubAnalysis: 10, // Set high to avoid isolation detection
		HubIsolationThreshold: 0.1,
		TrackCoreDemotions:    true,
		MinDemotionDelta:      2, // Entity dropped by 3, which exceeds 2
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	anomalies, err := detector.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Should detect demotion anomaly for demoted-entity
	var demotionAnomaly *StructuralAnomaly
	for _, a := range anomalies {
		if a.Type == AnomalyCoreDemotion && a.EntityA == "demoted-entity" {
			demotionAnomaly = a
			break
		}
	}

	if demotionAnomaly == nil {
		t.Fatal("expected to find core demotion anomaly for demoted-entity")
	}

	// Key assertion: Suggestion must be populated
	if demotionAnomaly.Suggestion == nil {
		t.Fatal("expected Suggestion to be populated for demotion anomaly")
	}

	// Verify Suggestion fields
	if demotionAnomaly.Suggestion.FromEntity != "demoted-entity" {
		t.Errorf("expected Suggestion.FromEntity='demoted-entity', got %s", demotionAnomaly.Suggestion.FromEntity)
	}

	if demotionAnomaly.Suggestion.Predicate != "inference.suggested.support" {
		t.Errorf("expected Suggestion.Predicate='inference.suggested.support', got %s", demotionAnomaly.Suggestion.Predicate)
	}

	if demotionAnomaly.Suggestion.Reasoning == "" {
		t.Error("expected Suggestion.Reasoning to be non-empty")
	}

	// Verify reasoning mentions the demotion
	if !strings.Contains(demotionAnomaly.Suggestion.Reasoning, "dropped") {
		t.Error("expected Suggestion.Reasoning to mention 'dropped'")
	}

	// Verify reasoning mentions core levels
	if !strings.Contains(demotionAnomaly.Suggestion.Reasoning, "5") || !strings.Contains(demotionAnomaly.Suggestion.Reasoning, "2") {
		t.Error("expected Suggestion.Reasoning to mention core levels 5 and 2")
	}

	if demotionAnomaly.Suggestion.Confidence <= 0 {
		t.Error("expected Suggestion.Confidence to be positive")
	}
}

func TestCoreAnomalyDetector_SetDependencies(t *testing.T) {
	detector := NewCoreAnomalyDetector(nil)

	if detector.deps != nil {
		t.Error("expected nil deps initially")
	}

	deps := &DetectorDependencies{
		StructuralIndices: &structural.Indices{},
	}

	detector.SetDependencies(deps)

	if detector.deps != deps {
		t.Error("expected deps to be set")
	}
}
