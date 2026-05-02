package boid

import (
	"context"
	"sync"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/processor/rule"
)

// mockCentralityProvider implements CentralityProvider for testing.
type mockCentralityProvider struct {
	scores map[string]float64
	err    error
}

func (m *mockCentralityProvider) GetPageRankScores(_ context.Context, entityIDs []string) (map[string]float64, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := make(map[string]float64)
	for _, id := range entityIDs {
		if score, ok := m.scores[id]; ok {
			result[id] = score
		}
	}
	return result, nil
}

func TestCohesionRule_Name(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test-cohesion",
		Name:    "Test Cohesion Rule",
		Enabled: rule.ModeEnabled,
	}

	r := NewCohesionRule("test-cohesion", def, config, 0, nil)

	if r.Name() != "Test Cohesion Rule" {
		t.Errorf("Name() = %s, want Test Cohesion Rule", r.Name())
	}
}

func TestCohesionRule_EvaluateEntityState_Disabled(t *testing.T) {
	config := &Config{BoidRule: RuleTypeCohesion}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeDisabled, // Disabled
	}
	r := NewCohesionRule("test", def, config, 0, nil)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when disabled")
	}
}

func TestCohesionRule_EvaluateEntityState_NoProvider(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil)
	// No position provider set

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false without position provider")
	}
}

func TestCohesionRule_EvaluateEntityState_NoFocusEntities(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil)

	// Provider with position that has no focus entities
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{}, // No focus entities
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false without focus entities")
	}
}

func TestCohesionRule_EvaluateEntityState_RoleFilter(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		RoleFilter:       "architect", // Only fire for architect role
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil)

	// Provider with position that has role "general" (not architect)
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general", // Not architect
				FocusEntities: []string{"entity-1"},
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when role doesn't match filter")
	}
}

func TestCohesionRule_EvaluateEntityState_NoCentralityProvider(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
		CentralityWeight: 0.7,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil)

	// Provider with position that has focus entities
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1", "entity-2"},
			},
		},
	}
	r.SetPositionProvider(provider)
	// No centrality provider - should still work (returns focus entities sorted by ID)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if !result {
		t.Error("EvaluateEntityState should return true even without centrality provider")
	}

	signals := r.GetPendingSignals()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.SignalType != SignalTypeCohesion {
		t.Errorf("signal type = %s, want %s", signal.SignalType, SignalTypeCohesion)
	}
	if len(signal.SuggestedFocus) == 0 {
		t.Error("signal should have suggested focus entities")
	}
}

func TestCohesionRule_EvaluateEntityState_WithCentralityProvider(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
		CentralityWeight: 0.5, // Lower threshold to include more entities
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil)

	// Position provider with focus entities
	posProvider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1"},
			},
		},
	}
	r.SetPositionProvider(posProvider)

	// Centrality provider with scores
	centralityProvider := &mockCentralityProvider{
		scores: map[string]float64{
			"entity-1": 0.8, // High centrality
			"entity-2": 0.1, // Lower centrality
		},
	}
	r.SetCentralityProvider(centralityProvider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if !result {
		t.Error("EvaluateEntityState should return true with centrality provider")
	}

	signals := r.GetPendingSignals()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.SignalType != SignalTypeCohesion {
		t.Errorf("signal type = %s, want %s", signal.SignalType, SignalTypeCohesion)
	}
	if signal.Strength != 0.8 {
		t.Errorf("signal strength = %f, want 0.8", signal.Strength)
	}
}

func TestCohesionRule_EvaluateEntityState_WithPivotIndex(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
		CentralityWeight: 0.5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil)

	// Position provider with focus entities
	posProvider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1"},
			},
		},
	}
	r.SetPositionProvider(posProvider)

	// Create a simple pivot index
	pivotIndex := &structural.PivotIndex{
		Pivots: []string{"pivot-1"},
		DistanceVectors: map[string][]int{
			"entity-1": {0}, // pivot-1 is 0 hops from entity-1
			"entity-2": {1}, // pivot-1 is 1 hop from entity-2
			"entity-3": {2}, // pivot-1 is 2 hops from entity-3
		},
		EntityCount: 3,
	}
	r.SetPivotIndex(pivotIndex)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if !result {
		t.Error("EvaluateEntityState should return true with pivot index")
	}

	signals := r.GetPendingSignals()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
}

func TestCohesionRule_Cooldown(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	cooldown := 100 * time.Millisecond
	r := NewCohesionRule("test", def, config, cooldown, nil)

	// Provider with position
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1"},
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	// First evaluation should trigger
	result1 := r.EvaluateEntityState(entityState)
	if !result1 {
		t.Error("first evaluation should trigger")
	}
	r.GetPendingSignals() // Clear signals

	// Immediate second evaluation should be blocked by cooldown
	result2 := r.EvaluateEntityState(entityState)
	if result2 {
		t.Error("second evaluation should be blocked by cooldown")
	}

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// Third evaluation should trigger
	result3 := r.EvaluateEntityState(entityState)
	if !result3 {
		t.Error("third evaluation should trigger after cooldown")
	}
}

func TestCohesionRule_ConcurrentEvaluation(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeCohesion,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewCohesionRule("test", def, config, 0, nil) // No cooldown for concurrent test

	// Provider with multiple positions
	positions := make([]*AgentPosition, 10)
	for i := 0; i < 10; i++ {
		positions[i] = &AgentPosition{
			LoopID:        "loop-" + string(rune('0'+i)),
			Role:          "general",
			FocusEntities: []string{"entity-" + string(rune('0'+i))},
		}
	}
	provider := &mockPositionProvider{positions: positions}
	r.SetPositionProvider(provider)

	// Run concurrent evaluations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entityState := &gtypes.EntityState{
				ID: "loop-" + string(rune('0'+idx)),
			}
			r.EvaluateEntityState(entityState)
		}(i)
	}

	wg.Wait()

	// Should have generated signals without panicking
	signals := r.GetPendingSignals()
	if len(signals) == 0 {
		t.Error("expected at least some signals from concurrent evaluations")
	}
}

func TestCohesionRule_SettersAreSafe(t *testing.T) {
	config := &Config{BoidRule: RuleTypeCohesion}
	def := rule.Definition{ID: "test", Enabled: rule.ModeEnabled}
	r := NewCohesionRule("test", def, config, 0, nil)

	// Concurrent setter calls should not panic
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			r.SetPositionProvider(&mockPositionProvider{})
		}()
		go func() {
			defer wg.Done()
			r.SetCentralityProvider(&mockCentralityProvider{})
		}()
		go func() {
			defer wg.Done()
			r.SetPivotIndex(&structural.PivotIndex{})
		}()
	}
	wg.Wait()

	// Verify providers were set (test passes if we reach here without race/panic)
	if r.positionProvider == nil {
		t.Error("expected position provider to be set after concurrent calls")
	}
}
