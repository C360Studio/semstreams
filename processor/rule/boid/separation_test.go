package boid

import (
	"context"
	"fmt"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule"
)

// mockPositionProvider implements PositionProvider for testing.
type mockPositionProvider struct {
	positions []*AgentPosition
}

func (m *mockPositionProvider) Get(_ context.Context, loopID string) (*AgentPosition, error) {
	for _, pos := range m.positions {
		if pos.LoopID == loopID {
			return pos, nil
		}
	}
	return nil, fmt.Errorf("position not found for %s", loopID)
}

func (m *mockPositionProvider) ListOthers(_ context.Context, excludeLoopID string) ([]*AgentPosition, error) {
	result := make([]*AgentPosition, 0)
	for _, pos := range m.positions {
		if pos.LoopID != excludeLoopID {
			result = append(result, pos)
		}
	}
	return result, nil
}

func TestSeparationRule_Name(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeSeparation,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test-separation",
		Name:    "Test Separation Rule",
		Enabled: rule.ModeEnabled,
	}

	r := NewSeparationRule("test-separation", def, config, 0, nil)

	if r.Name() != "Test Separation Rule" {
		t.Errorf("Name() = %s, want Test Separation Rule", r.Name())
	}
}

func TestSeparationRule_Subscribe(t *testing.T) {
	config := &Config{BoidRule: RuleTypeSeparation}
	def := rule.Definition{ID: "test"}
	r := NewSeparationRule("test", def, config, 0, nil)

	subjects := r.Subscribe()
	if len(subjects) != 0 {
		t.Errorf("Subscribe() should return empty slice for KV-based rules, got %v", subjects)
	}
}

func TestSeparationRule_Evaluate(t *testing.T) {
	config := &Config{BoidRule: RuleTypeSeparation}
	def := rule.Definition{ID: "test"}
	r := NewSeparationRule("test", def, config, 0, nil)

	// Boid rules don't use message-based evaluation
	result := r.Evaluate([]message.Message{})
	if result {
		t.Error("Evaluate() should always return false for boid rules")
	}
}

func TestSeparationRule_EvaluateEntityState_Disabled(t *testing.T) {
	config := &Config{BoidRule: RuleTypeSeparation}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeDisabled, // Disabled
	}
	r := NewSeparationRule("test", def, config, 0, nil)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when disabled")
	}
}

func TestSeparationRule_EvaluateEntityState_NoProvider(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeSeparation,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewSeparationRule("test", def, config, 0, nil)
	// No position provider set

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false without position provider")
	}
}

func TestSeparationRule_EvaluateEntityState_NoFocusEntities(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeSeparation,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewSeparationRule("test", def, config, 0, nil)

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
		ID: "loop-1", // ID used to lookup position via provider.Get()
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false without focus entities")
	}
}

func TestSeparationRule_EvaluateEntityState_RoleFilter(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeSeparation,
		RoleFilter:       "architect", // Only fire for architect role
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewSeparationRule("test", def, config, 0, nil)

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

func TestSeparationRule_EvaluateEntityState_NoOverlap(t *testing.T) {
	config := &Config{
		BoidRule:            RuleTypeSeparation,
		SeparationThreshold: 2,
		SteeringStrength:    0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewSeparationRule("test", def, config, 0, nil)

	// Provider with both agents - loop-1 (self) and loop-2 (other)
	// They have different focus entities, no overlap
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1", "entity-2"},
			},
			{
				LoopID:        "loop-2",
				Role:          "general",
				FocusEntities: []string{"entity-100", "entity-101"}, // Different entities
			},
		},
	}
	r.SetPositionProvider(provider)
	// No pivot index - rule will only trigger on exact match (same entity)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when no overlap")
	}
}

func TestSeparationRule_EvaluateEntityState_WithOverlap(t *testing.T) {
	config := &Config{
		BoidRule:            RuleTypeSeparation,
		SeparationThreshold: 2,
		SteeringStrength:    0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewSeparationRule("test", def, config, 0, nil)

	// Provider with both agents - loop-1 (self) and loop-2 (other)
	// They share entity-1, creating overlap
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1", "entity-2"},
			},
			{
				LoopID:        "loop-2",
				Role:          "general",
				FocusEntities: []string{"entity-1"}, // Same entity as loop-1
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if !result {
		t.Error("EvaluateEntityState should return true when overlap detected")
	}

	// Check the generated signal
	signals := r.GetPendingSignals()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.SignalType != SignalTypeSeparation {
		t.Errorf("signal type = %s, want %s", signal.SignalType, SignalTypeSeparation)
	}
	if signal.Strength != 0.8 {
		t.Errorf("signal strength = %f, want 0.8", signal.Strength)
	}
	if len(signal.AvoidEntities) == 0 {
		t.Error("signal should have avoid entities")
	}
}

func TestSeparationRule_Cooldown(t *testing.T) {
	config := &Config{
		BoidRule:            RuleTypeSeparation,
		SeparationThreshold: 2,
		SteeringStrength:    0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	cooldown := 100 * time.Millisecond
	r := NewSeparationRule("test", def, config, cooldown, nil)

	// Provider with both agents sharing entity-1
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:        "loop-1",
				Role:          "general",
				FocusEntities: []string{"entity-1"},
			},
			{
				LoopID:        "loop-2",
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
