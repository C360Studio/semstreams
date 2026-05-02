package boid

import (
	"sync"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/processor/rule"
)

func TestAlignmentRule_Name(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test-alignment",
		Name:    "Test Alignment Rule",
		Enabled: rule.ModeEnabled,
	}

	r := NewAlignmentRule("test-alignment", def, config, 0, nil)

	if r.Name() != "Test Alignment Rule" {
		t.Errorf("Name() = %s, want Test Alignment Rule", r.Name())
	}
}

func TestAlignmentRule_EvaluateEntityState_Disabled(t *testing.T) {
	config := &Config{BoidRule: RuleTypeAlignment}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeDisabled, // Disabled
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when disabled")
	}
}

func TestAlignmentRule_EvaluateEntityState_NoProvider(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)
	// No position provider set

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false without position provider")
	}
}

func TestAlignmentRule_EvaluateEntityState_NoSameRoleAgents(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

	// Provider with only one agent (no same-role agents to align with)
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:          "loop-1",
				Role:            "general",
				FocusEntities:   []string{"entity-1"},
				TraversalVector: []string{"has_member", "related_to"},
			},
			{
				LoopID:          "loop-2",
				Role:            "architect", // Different role
				FocusEntities:   []string{"entity-2"},
				TraversalVector: []string{"depends_on"},
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when no same-role agents exist")
	}
}

func TestAlignmentRule_EvaluateEntityState_RoleFilter(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		RoleFilter:       "architect", // Only fire for architect role
		SteeringStrength: 0.8,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

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

func TestAlignmentRule_EvaluateEntityState_NoTraversalVectors(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

	// Provider with same-role agents but no traversal vectors
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:          "loop-1",
				Role:            "general",
				FocusEntities:   []string{"entity-1"},
				TraversalVector: []string{}, // Empty
			},
			{
				LoopID:          "loop-2",
				Role:            "general", // Same role
				FocusEntities:   []string{"entity-2"},
				TraversalVector: []string{}, // Empty
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when no traversal vectors exist")
	}
}

func TestAlignmentRule_EvaluateEntityState_CommonPredicatesFound(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

	// Provider with same-role agents sharing traversal predicates
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:          "loop-1",
				Role:            "general",
				FocusEntities:   []string{"entity-1"},
				TraversalVector: []string{}, // Agent doesn't have these predicates yet
			},
			{
				LoopID:          "loop-2",
				Role:            "general", // Same role
				FocusEntities:   []string{"entity-2"},
				TraversalVector: []string{"has_member", "related_to", "depends_on"},
			},
			{
				LoopID:          "loop-3",
				Role:            "general", // Same role
				FocusEntities:   []string{"entity-3"},
				TraversalVector: []string{"has_member", "related_to", "part_of"},
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if !result {
		t.Error("EvaluateEntityState should return true when common predicates found")
	}

	signals := r.GetPendingSignals()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	signal := signals[0]
	if signal.SignalType != SignalTypeAlignment {
		t.Errorf("signal type = %s, want %s", signal.SignalType, SignalTypeAlignment)
	}
	if signal.Strength != 0.8 {
		t.Errorf("signal strength = %f, want 0.8", signal.Strength)
	}
	if len(signal.AlignWith) == 0 {
		t.Error("signal should have align_with predicates")
	}

	// has_member and related_to should be in align_with (used by multiple agents)
	alignWithSet := make(map[string]bool)
	for _, p := range signal.AlignWith {
		alignWithSet[p] = true
	}
	if !alignWithSet["has_member"] {
		t.Error("expected 'has_member' in align_with (used by 2 agents)")
	}
	if !alignWithSet["related_to"] {
		t.Error("expected 'related_to' in align_with (used by 2 agents)")
	}
}

func TestAlignmentRule_EvaluateEntityState_FiltersCurrentPredicates(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

	// Provider where agent already has the common predicates
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:          "loop-1",
				Role:            "general",
				FocusEntities:   []string{"entity-1"},
				TraversalVector: []string{"has_member", "related_to"}, // Already following these
			},
			{
				LoopID:          "loop-2",
				Role:            "general", // Same role
				FocusEntities:   []string{"entity-2"},
				TraversalVector: []string{"has_member", "related_to"}, // Same predicates
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if result {
		t.Error("EvaluateEntityState should return false when agent already follows all common predicates")
	}
}

func TestAlignmentRule_Cooldown(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	cooldown := 100 * time.Millisecond
	r := NewAlignmentRule("test", def, config, cooldown, nil)

	// Provider with same-role agents sharing predicates
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:          "loop-1",
				Role:            "general",
				FocusEntities:   []string{"entity-1"},
				TraversalVector: []string{},
			},
			{
				LoopID:          "loop-2",
				Role:            "general",
				FocusEntities:   []string{"entity-2"},
				TraversalVector: []string{"has_member"},
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

func TestAlignmentRule_ConcurrentEvaluation(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  5,
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil) // No cooldown for concurrent test

	// Provider with multiple same-role agents
	positions := make([]*AgentPosition, 20)
	for i := range 20 {
		positions[i] = &AgentPosition{
			LoopID:          "loop-" + string(rune('a'+i)),
			Role:            "general", // All same role
			FocusEntities:   []string{"entity-" + string(rune('a'+i))},
			TraversalVector: []string{"has_member", "related_to"}, // Common predicates
		}
	}
	// Make first 10 have empty traversal vectors to generate signals
	for i := range 10 {
		positions[i].TraversalVector = []string{}
	}
	provider := &mockPositionProvider{positions: positions}
	r.SetPositionProvider(provider)

	// Run concurrent evaluations
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entityState := &gtypes.EntityState{
				ID: "loop-" + string(rune('a'+idx)),
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

func TestAlignmentRule_SetterIsSafe(t *testing.T) {
	config := &Config{BoidRule: RuleTypeAlignment}
	def := rule.Definition{ID: "test", Enabled: rule.ModeEnabled}
	r := NewAlignmentRule("test", def, config, 0, nil)

	// Concurrent setter calls should not panic
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.SetPositionProvider(&mockPositionProvider{})
		}()
	}
	wg.Wait()

	// Verify a provider was set (test passes if we reach here without race/panic)
	if r.positionProvider == nil {
		t.Error("expected position provider to be set after concurrent calls")
	}
}

func TestAlignmentRule_AlignmentWindowLimit(t *testing.T) {
	config := &Config{
		BoidRule:         RuleTypeAlignment,
		SteeringStrength: 0.8,
		AlignmentWindow:  2, // Only return top 2 predicates
	}
	def := rule.Definition{
		ID:      "test",
		Enabled: rule.ModeEnabled,
	}
	r := NewAlignmentRule("test", def, config, 0, nil)

	// Provider with agents using many predicates
	provider := &mockPositionProvider{
		positions: []*AgentPosition{
			{
				LoopID:          "loop-1",
				Role:            "general",
				FocusEntities:   []string{"entity-1"},
				TraversalVector: []string{}, // Will receive alignment suggestions
			},
			{
				LoopID:          "loop-2",
				Role:            "general",
				FocusEntities:   []string{"entity-2"},
				TraversalVector: []string{"has_member", "related_to", "depends_on", "part_of", "owned_by"},
			},
			{
				LoopID:          "loop-3",
				Role:            "general",
				FocusEntities:   []string{"entity-3"},
				TraversalVector: []string{"has_member", "related_to", "depends_on"}, // Top 3 shared
			},
		},
	}
	r.SetPositionProvider(provider)

	entityState := &gtypes.EntityState{
		ID: "loop-1",
	}

	result := r.EvaluateEntityState(entityState)
	if !result {
		t.Error("EvaluateEntityState should return true")
	}

	signals := r.GetPendingSignals()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}

	// Should be limited to alignment window (2)
	signal := signals[0]
	if len(signal.AlignWith) > 2 {
		t.Errorf("expected at most 2 predicates (alignment_window), got %d", len(signal.AlignWith))
	}
}
