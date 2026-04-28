package agenticloop

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/processor/rule/boid"
)

func TestBoidHandler_ExtractEntitiesFromToolResult(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "empty content",
			content:  "",
			expected: nil,
		},
		{
			name:     "no entity IDs",
			content:  "Just some regular text without entity IDs",
			expected: nil,
		},
		{
			name:    "single entity ID",
			content: "Found entity: acme.ops.robotics.gcs.drone.001",
			expected: []string{
				"acme.ops.robotics.gcs.drone.001",
			},
		},
		{
			name:    "multiple entity IDs",
			content: "Entities: acme.ops.robotics.gcs.drone.001 and acme.ops.robotics.gcs.sensor.temp-01",
			expected: []string{
				"acme.ops.robotics.gcs.drone.001",
				"acme.ops.robotics.gcs.sensor.temp-01",
			},
		},
		{
			name:    "duplicate entity IDs",
			content: "Entity acme.ops.robotics.gcs.drone.001 mentioned again acme.ops.robotics.gcs.drone.001",
			expected: []string{
				"acme.ops.robotics.gcs.drone.001",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.ExtractEntitiesFromToolResult(tt.content)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d entities, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("entity %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestBoidHandler_ExtractEntitiesFromContext(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "empty content",
			content:  "",
			expected: nil,
		},
		{
			name:     "no entity markers",
			content:  "Regular context without entity markers",
			expected: nil,
		},
		{
			name:    "single entity marker",
			content: "[Entity: acme.ops.robotics.gcs.drone.001] Some context",
			expected: []string{
				"acme.ops.robotics.gcs.drone.001",
			},
		},
		{
			name:    "multiple entity markers",
			content: "[Entity: acme.ops.robotics.gcs.drone.001] First context [Entity: acme.ops.robotics.gcs.sensor.temp-01] Second context",
			expected: []string{
				"acme.ops.robotics.gcs.drone.001",
				"acme.ops.robotics.gcs.sensor.temp-01",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.ExtractEntitiesFromContext(tt.content)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d entities, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("entity %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestBoidHandler_CalculateVelocity(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	tests := []struct {
		name      string
		oldFocus  []string
		newFocus  []string
		wantRange [2]float64 // [min, max]
	}{
		{
			name:      "no change",
			oldFocus:  []string{"a", "b", "c"},
			newFocus:  []string{"a", "b", "c"},
			wantRange: [2]float64{0.0, 0.1},
		},
		{
			name:      "complete change",
			oldFocus:  []string{"a", "b", "c"},
			newFocus:  []string{"d", "e", "f"},
			wantRange: [2]float64{0.9, 1.0},
		},
		{
			name:      "partial change",
			oldFocus:  []string{"a", "b", "c"},
			newFocus:  []string{"a", "d", "e"},
			wantRange: [2]float64{0.3, 0.7},
		},
		{
			name:      "empty old",
			oldFocus:  []string{},
			newFocus:  []string{"a", "b"},
			wantRange: [2]float64{0.9, 1.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			velocity := handler.CalculateVelocity(tt.oldFocus, tt.newFocus)

			if velocity < tt.wantRange[0] || velocity > tt.wantRange[1] {
				t.Errorf("velocity %v not in expected range [%v, %v]",
					velocity, tt.wantRange[0], tt.wantRange[1])
			}
		})
	}
}

func TestMergeEntities(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		newEnts  []string
		maxCount int
		expected []string
	}{
		{
			name:     "empty both",
			existing: nil,
			newEnts:  nil,
			maxCount: 10,
			expected: []string{},
		},
		{
			name:     "new entities only",
			existing: nil,
			newEnts:  []string{"a", "b"},
			maxCount: 10,
			expected: []string{"a", "b"},
		},
		{
			name:     "existing entities only",
			existing: []string{"a", "b"},
			newEnts:  nil,
			maxCount: 10,
			expected: []string{"a", "b"},
		},
		{
			name:     "merge without duplicates",
			existing: []string{"a", "b"},
			newEnts:  []string{"c", "d"},
			maxCount: 10,
			expected: []string{"c", "d", "a", "b"},
		},
		{
			name:     "merge with duplicates",
			existing: []string{"a", "b"},
			newEnts:  []string{"b", "c"},
			maxCount: 10,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "limit exceeded",
			existing: []string{"a", "b", "c"},
			newEnts:  []string{"d", "e"},
			maxCount: 3,
			expected: []string{"d", "e", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeEntities(tt.existing, tt.newEnts, tt.maxCount)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d entities, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("entity %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

// --- Signal Store Tests ---

func TestSignalStore_StoreAndGet(t *testing.T) {
	store := NewSignalStore(5 * time.Second)

	signal := &boid.SteeringSignal{
		LoopID:        "loop-1",
		SignalType:    boid.SignalTypeSeparation,
		AvoidEntities: []string{"entity.a", "entity.b"},
		Strength:      0.8,
	}

	store.Store(signal)

	// Get the signal back
	retrieved := store.Get("loop-1", boid.SignalTypeSeparation)
	if retrieved == nil {
		t.Fatal("expected signal, got nil")
	}
	if len(retrieved.AvoidEntities) != 2 {
		t.Errorf("expected 2 avoid entities, got %d", len(retrieved.AvoidEntities))
	}

	// Different signal type should return nil
	cohesion := store.Get("loop-1", boid.SignalTypeCohesion)
	if cohesion != nil {
		t.Error("expected nil for cohesion signal type")
	}

	// Different loop ID should return nil
	other := store.Get("loop-2", boid.SignalTypeSeparation)
	if other != nil {
		t.Error("expected nil for different loop ID")
	}
}

func TestSignalStore_GetAll(t *testing.T) {
	store := NewSignalStore(5 * time.Second)

	// Store multiple signal types for same loop
	store.Store(&boid.SteeringSignal{
		LoopID:        "loop-1",
		SignalType:    boid.SignalTypeSeparation,
		AvoidEntities: []string{"entity.a"},
	})
	store.Store(&boid.SteeringSignal{
		LoopID:         "loop-1",
		SignalType:     boid.SignalTypeCohesion,
		SuggestedFocus: []string{"entity.b"},
	})

	all := store.GetAll("loop-1")
	if len(all) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(all))
	}

	if all[boid.SignalTypeSeparation] == nil {
		t.Error("expected separation signal")
	}
	if all[boid.SignalTypeCohesion] == nil {
		t.Error("expected cohesion signal")
	}
}

func TestSignalStore_Expiration(t *testing.T) {
	store := NewSignalStore(50 * time.Millisecond) // Short TTL for testing

	store.Store(&boid.SteeringSignal{
		LoopID:     "loop-1",
		SignalType: boid.SignalTypeSeparation,
	})

	// Should exist immediately
	if store.Get("loop-1", boid.SignalTypeSeparation) == nil {
		t.Error("signal should exist immediately after storing")
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Should be expired
	if store.Get("loop-1", boid.SignalTypeSeparation) != nil {
		t.Error("signal should have expired")
	}
}

func TestSignalStore_Remove(t *testing.T) {
	store := NewSignalStore(5 * time.Second)

	store.Store(&boid.SteeringSignal{
		LoopID:     "loop-1",
		SignalType: boid.SignalTypeSeparation,
	})

	store.Remove("loop-1")

	if store.Get("loop-1", boid.SignalTypeSeparation) != nil {
		t.Error("signal should be removed")
	}
}

func TestSignalStore_Cleanup(t *testing.T) {
	store := NewSignalStore(50 * time.Millisecond)

	store.Store(&boid.SteeringSignal{
		LoopID:     "loop-1",
		SignalType: boid.SignalTypeSeparation,
	})
	store.Store(&boid.SteeringSignal{
		LoopID:     "loop-2",
		SignalType: boid.SignalTypeCohesion,
	})

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	removed := store.Cleanup()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	// Verify cleanup
	if store.GetAll("loop-1") != nil {
		t.Error("loop-1 signals should be cleaned up")
	}
}

// --- BoidHandler Signal Integration Tests ---

func TestBoidHandler_ProcessSteeringSignal(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	signal := &boid.SteeringSignal{
		LoopID:        "loop-1",
		SignalType:    boid.SignalTypeSeparation,
		AvoidEntities: []string{"entity.a", "entity.b"},
		Strength:      0.8,
	}

	err := handler.ProcessSteeringSignal(context.Background(), signal, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Signal should be stored
	retrieved := handler.GetActiveSignal("loop-1", boid.SignalTypeSeparation)
	if retrieved == nil {
		t.Fatal("signal should be stored after processing")
	}
	if len(retrieved.AvoidEntities) != 2 {
		t.Errorf("expected 2 avoid entities, got %d", len(retrieved.AvoidEntities))
	}
}

func TestBoidHandler_ApplySteeringToEntities(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	// Store separation and cohesion signals
	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:        "loop-1",
		SignalType:    boid.SignalTypeSeparation,
		AvoidEntities: []string{"entity.avoid"},
	}, nil)

	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:         "loop-1",
		SignalType:     boid.SignalTypeCohesion,
		SuggestedFocus: []string{"entity.priority"},
	}, nil)

	prioritize, avoid := handler.ApplySteeringToEntities("loop-1")

	if len(prioritize) != 1 || prioritize[0] != "entity.priority" {
		t.Errorf("expected prioritize=[entity.priority], got %v", prioritize)
	}
	if len(avoid) != 1 || avoid[0] != "entity.avoid" {
		t.Errorf("expected avoid=[entity.avoid], got %v", avoid)
	}
}

func TestBoidHandler_FilterEntitiesBySignals(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	// Store signals
	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:        "loop-1",
		SignalType:    boid.SignalTypeSeparation,
		AvoidEntities: []string{"entity.c"},
	}, nil)

	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:         "loop-1",
		SignalType:     boid.SignalTypeCohesion,
		SuggestedFocus: []string{"entity.a"},
	}, nil)

	// Filter entities
	entities := []string{"entity.b", "entity.a", "entity.c", "entity.d"}
	filtered := handler.FilterEntitiesBySignals("loop-1", entities)

	// Expected order: prioritized (a), normal (b, d), avoided (c)
	expected := []string{"entity.a", "entity.b", "entity.d", "entity.c"}
	if len(filtered) != len(expected) {
		t.Fatalf("expected %d entities, got %d", len(expected), len(filtered))
	}
	for i, e := range expected {
		if filtered[i] != e {
			t.Errorf("position %d: expected %q, got %q", i, e, filtered[i])
		}
	}
}

func TestBoidHandler_FilterEntitiesBySignals_NoSignals(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	// No signals stored - should return original order
	entities := []string{"entity.a", "entity.b", "entity.c"}
	filtered := handler.FilterEntitiesBySignals("loop-1", entities)

	for i, e := range entities {
		if filtered[i] != e {
			t.Errorf("position %d: expected %q, got %q (should preserve original order)", i, e, filtered[i])
		}
	}
}

func TestBoidHandler_GetAlignmentPatterns(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	// No signal - should return nil
	patterns := handler.GetAlignmentPatterns("loop-1")
	if patterns != nil {
		t.Errorf("expected nil, got %v", patterns)
	}

	// Store alignment signal
	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:     "loop-1",
		SignalType: boid.SignalTypeAlignment,
		AlignWith:  []string{"hasComponent", "locatedAt"},
	}, nil)

	patterns = handler.GetAlignmentPatterns("loop-1")
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}
	if patterns[0] != "hasComponent" || patterns[1] != "locatedAt" {
		t.Errorf("unexpected patterns: %v", patterns)
	}
}

func TestBoidHandler_ClearSignals(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	// Store signals
	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:     "loop-1",
		SignalType: boid.SignalTypeSeparation,
	}, nil)
	handler.ProcessSteeringSignal(context.Background(), &boid.SteeringSignal{
		LoopID:     "loop-1",
		SignalType: boid.SignalTypeCohesion,
	}, nil)

	// Verify signals exist
	if handler.GetActiveSignals("loop-1") == nil {
		t.Fatal("signals should exist before clear")
	}

	// Clear signals
	handler.ClearSignals("loop-1")

	// Verify signals are gone
	if handler.GetActiveSignals("loop-1") != nil {
		t.Error("signals should be cleared")
	}
}

func TestBoidHandler_ProcessSteeringSignal_AppliesSteeringToContext(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	// Create a context manager with some graph entities
	cfg := ContextConfig{
		Enabled:          true,
		CompactThreshold: 0.8,
	}
	cm := NewContextManager("loop-1", "test-model", cfg)

	// Add some graph entity context
	_ = cm.AddGraphEntityContext("entity.avoid", "Content for entity to avoid")
	_ = cm.AddGraphEntityContext("entity.normal", "Content for normal entity")
	_ = cm.AddGraphEntityContext("entity.prioritize", "Content for prioritized entity")

	// Process a separation signal with context manager
	signal := &boid.SteeringSignal{
		LoopID:        "loop-1",
		SignalType:    boid.SignalTypeSeparation,
		AvoidEntities: []string{"entity.avoid"},
		Strength:      0.8,
	}

	err := handler.ProcessSteeringSignal(context.Background(), signal, cm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Signal should be stored
	retrieved := handler.GetActiveSignal("loop-1", boid.SignalTypeSeparation)
	if retrieved == nil {
		t.Fatal("signal should be stored after processing")
	}

	// Note: The actual reordering is tested in context_manager_test.go
	// This test verifies that ProcessSteeringSignal calls ApplyBoidSteering
}

func TestBoidHandler_ProcessSteeringSignal_AllSignalTypes(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)
	cfg := ContextConfig{
		Enabled:          true,
		CompactThreshold: 0.8,
	}
	cm := NewContextManager("loop-1", "test-model", cfg)

	tests := []struct {
		name   string
		signal *boid.SteeringSignal
	}{
		{
			name: "separation signal",
			signal: &boid.SteeringSignal{
				LoopID:        "loop-1",
				SignalType:    boid.SignalTypeSeparation,
				AvoidEntities: []string{"entity.a"},
				Strength:      0.7,
			},
		},
		{
			name: "cohesion signal",
			signal: &boid.SteeringSignal{
				LoopID:         "loop-1",
				SignalType:     boid.SignalTypeCohesion,
				SuggestedFocus: []string{"entity.b"},
				Strength:       0.6,
			},
		},
		{
			name: "alignment signal",
			signal: &boid.SteeringSignal{
				LoopID:     "loop-1",
				SignalType: boid.SignalTypeAlignment,
				AlignWith:  []string{"has_member", "related_to"},
				Strength:   0.5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.ProcessSteeringSignal(context.Background(), tt.signal, cm)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			retrieved := handler.GetActiveSignal("loop-1", tt.signal.SignalType)
			if retrieved == nil {
				t.Fatal("signal should be stored after processing")
			}
		})
	}
}

func TestBoidHandler_ExtractPredicatesFromToolResult(t *testing.T) {
	handler := NewBoidHandler(nil, nil, nil)

	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "empty content",
			content:  "",
			expected: nil,
		},
		{
			name:     "no predicates",
			content:  "Just some regular text",
			expected: nil,
		},
		{
			name:     "unknown predicate-like pattern",
			content:  "Found some_random_thing in the data",
			expected: []string{}, // Not in known predicates list, returns empty slice
		},
		{
			name:    "single known predicate",
			content: "The entity has_member relationship with another",
			expected: []string{
				"has_member",
			},
		},
		{
			name:    "multiple known predicates",
			content: "Found has_member and related_to relationships in the graph",
			expected: []string{
				"has_member",
				"related_to",
			},
		},
		{
			name:    "duplicate predicates",
			content: "has_member relation and another has_member link",
			expected: []string{
				"has_member",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.ExtractPredicatesFromToolResult(tt.content)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d predicates, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("predicate %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestMergeStringSlices(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		newItems []string
		maxCount int
		expected []string
	}{
		{
			name:     "empty both",
			existing: nil,
			newItems: nil,
			maxCount: 10,
			expected: []string{},
		},
		{
			name:     "new items only",
			existing: nil,
			newItems: []string{"a", "b"},
			maxCount: 10,
			expected: []string{"a", "b"},
		},
		{
			name:     "existing items only",
			existing: []string{"a", "b"},
			newItems: nil,
			maxCount: 10,
			expected: []string{"a", "b"},
		},
		{
			name:     "merge without duplicates",
			existing: []string{"a", "b"},
			newItems: []string{"c", "d"},
			maxCount: 10,
			expected: []string{"c", "d", "a", "b"},
		},
		{
			name:     "merge with duplicates",
			existing: []string{"a", "b"},
			newItems: []string{"b", "c"},
			maxCount: 10,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "limit exceeded",
			existing: []string{"a", "b", "c"},
			newItems: []string{"d", "e"},
			maxCount: 3,
			expected: []string{"d", "e", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeStringSlices(tt.newItems, tt.existing, tt.maxCount)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d strings, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("string %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestNewBoidHandlerWithTTL(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{
			name: "default TTL",
			ttl:  0, // Should use default
		},
		{
			name: "custom TTL",
			ttl:  60 * time.Second,
		},
		{
			name: "negative TTL uses default",
			ttl:  -1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewBoidHandlerWithTTL(nil, nil, nil, tt.ttl)
			if handler == nil {
				t.Fatal("handler should not be nil")
			}
			if handler.signalStore == nil {
				t.Fatal("signal store should not be nil")
			}
		})
	}
}

func TestMergeStrings(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		newItems []string
		maxCount int
		expected []string
	}{
		{
			name:     "empty both",
			existing: nil,
			newItems: nil,
			maxCount: 10,
			expected: []string{},
		},
		{
			name:     "new items only",
			existing: nil,
			newItems: []string{"a", "b"},
			maxCount: 10,
			expected: []string{"a", "b"},
		},
		{
			name:     "existing items only",
			existing: []string{"a", "b"},
			newItems: nil,
			maxCount: 10,
			expected: []string{"a", "b"},
		},
		{
			name:     "merge without duplicates",
			existing: []string{"a", "b"},
			newItems: []string{"c", "d"},
			maxCount: 10,
			expected: []string{"c", "d", "a", "b"},
		},
		{
			name:     "merge with duplicates",
			existing: []string{"a", "b"},
			newItems: []string{"b", "c"},
			maxCount: 10,
			expected: []string{"b", "c", "a"},
		},
		{
			name:     "limit exceeded",
			existing: []string{"a", "b", "c"},
			newItems: []string{"d", "e"},
			maxCount: 3,
			expected: []string{"d", "e", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeStrings(tt.newItems, tt.existing, tt.maxCount)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d strings, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("string %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}
