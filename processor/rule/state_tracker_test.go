// Package rule - Tests for State Tracker (TDD - RED Phase)
package rule

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateKeyBelongsToEntity covers the EXACT single-entity key match used by
// DeleteAllForEntity. The load-bearing cases are the over-deletion guards: a
// 6-part ID must not match a dotted prefix-sibling (drone.1 vs drone.10) NOR an
// underscore prefix-sibling (drone.a vs the live entity drone.a_x — "_" is legal
// inside an instance segment). Pair keys are deliberately not matched.
func TestStateKeyBelongsToEntity(t *testing.T) {
	t.Parallel()

	const a = "org.plat.dom.sys.drone.1"
	const a10 = "org.plat.dom.sys.drone.10" // dotted prefix sibling
	const au = "org.plat.dom.sys.drone.a"
	const aux = "org.plat.dom.sys.drone.a_x" // underscore prefix sibling (live entity)
	const b = "org.plat.dom.sys.drone.2"

	tests := []struct {
		name     string
		key      string
		entityID string
		want     bool
	}{
		{"single entity exact", buildStateKey("rule-x", a), a, true},
		{"single entity, dotted ruleID", buildStateKey("rule.with.dots", a), a, true},
		{"dotted prefix sibling must NOT match", buildStateKey("rule-x", a10), a, false},
		{"underscore prefix sibling must NOT match", buildStateKey("rule-x", aux), au, false},
		{"underscore sibling matches itself", buildStateKey("rule-x", aux), aux, true},
		{"different entity", buildStateKey("rule-x", b), a, false},
		// Pair keys are intentionally NOT matched (key scheme can't split them
		// unambiguously; over-matching would risk a live entity's budget).
		{"pair key first component not matched", buildStateKey("rule-x", buildPairKey(a, b)), a, false},
		{"pair key second component not matched", buildStateKey("rule-x", buildPairKey(a, b)), b, false},
		{"empty entityID", buildStateKey("rule-x", a), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stateKeyBelongsToEntity(tt.key, tt.entityID),
				"key=%q entityID=%q", tt.key, tt.entityID)
		})
	}
}

// T032: Test MatchState struct
func TestMatchState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state MatchState
	}{
		{
			name: "single entity state",
			state: MatchState{
				RuleID:         "low-battery",
				EntityKey:      "c360.platform1.robotics.mav1.drone.001",
				IsMatching:     true,
				LastTransition: "entered",
				TransitionAt:   time.Now(),
				SourceRevision: 42,
				LastChecked:    time.Now(),
			},
		},
		{
			name: "entity pair state",
			state: MatchState{
				RuleID:         "proximity",
				EntityKey:      "c360.platform1.robotics.mav1.drone.001:c360.platform1.robotics.mav1.drone.002",
				IsMatching:     false,
				LastTransition: "exited",
				TransitionAt:   time.Now(),
				SourceRevision: 10,
				LastChecked:    time.Now(),
			},
		},
		{
			name: "no transition state",
			state: MatchState{
				RuleID:         "armed-check",
				EntityKey:      "c360.platform1.robotics.mav1.drone.003",
				IsMatching:     true,
				LastTransition: "", // TransitionNone
				SourceRevision: 1,
				LastChecked:    time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify struct fields are accessible
			if tt.state.RuleID == "" {
				t.Error("RuleID should not be empty")
			}
			if tt.state.EntityKey == "" {
				t.Error("EntityKey should not be empty")
			}
			// Verify LastTransition is valid
			validTransitions := []string{"", "entered", "exited"}
			assert.Contains(t, validTransitions, tt.state.LastTransition)
		})
	}
}

// T033: Test StateTracker.Get
func TestStateTracker_Get(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name      string
		ruleID    string
		entityKey string
		setup     func(*StateTracker) error
		wantErr   bool
		validate  func(*testing.T, MatchState)
	}{
		{
			name:      "existing state",
			ruleID:    "low-battery",
			entityKey: "c360.platform1.robotics.mav1.drone.001",
			setup: func(st *StateTracker) error {
				// Pre-populate state
				state := MatchState{
					RuleID:         "low-battery",
					EntityKey:      "c360.platform1.robotics.mav1.drone.001",
					IsMatching:     true,
					LastTransition: "entered",
					TransitionAt:   time.Now(),
					SourceRevision: 42,
					LastChecked:    time.Now(),
				}
				return st.Set(ctx, state)
			},
			wantErr: false,
			validate: func(t *testing.T, state MatchState) {
				assert.Equal(t, "low-battery", state.RuleID)
				assert.Equal(t, "c360.platform1.robotics.mav1.drone.001", state.EntityKey)
				assert.True(t, state.IsMatching)
				assert.Equal(t, "entered", state.LastTransition)
			},
		},
		{
			name:      "not found",
			ruleID:    "unknown-rule",
			entityKey: "c360.platform1.robotics.mav1.drone.999",
			setup:     func(_ *StateTracker) error { return nil },
			wantErr:   true,
			validate:  nil,
		},
		{
			name:      "empty rule ID",
			ruleID:    "",
			entityKey: "c360.platform1.robotics.mav1.drone.001",
			setup:     func(_ *StateTracker) error { return nil },
			wantErr:   true,
			validate:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create state tracker with mock bucket
			bucket := newMockKVBucket()
			st := NewStateTracker(bucket, nil)

			// Setup
			if tt.setup != nil {
				err := tt.setup(st)
				require.NoError(t, err, "Setup should not fail")
			}

			// Execute
			state, err := st.Get(ctx, tt.ruleID, tt.entityKey)

			// Verify
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, state)
				}
			}
		})
	}
}

// T034: Test StateTracker.Set
func TestStateTracker_Set(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		state   MatchState
		wantErr bool
	}{
		{
			name: "valid state",
			state: MatchState{
				RuleID:         "low-battery",
				EntityKey:      "c360.platform1.robotics.mav1.drone.001",
				IsMatching:     true,
				LastTransition: "entered",
				TransitionAt:   time.Now(),
				SourceRevision: 42,
				LastChecked:    time.Now(),
			},
			wantErr: false,
		},
		{
			name: "update existing state",
			state: MatchState{
				RuleID:         "proximity",
				EntityKey:      "c360.platform1.robotics.mav1.drone.001:c360.platform1.robotics.mav1.drone.002",
				IsMatching:     false,
				LastTransition: "exited",
				TransitionAt:   time.Now(),
				SourceRevision: 100,
				LastChecked:    time.Now(),
			},
			wantErr: false,
		},
		{
			name: "empty rule ID should fail",
			state: MatchState{
				RuleID:     "",
				EntityKey:  "c360.platform1.robotics.mav1.drone.001",
				IsMatching: true,
			},
			wantErr: true,
		},
		{
			name: "empty entity key should fail",
			state: MatchState{
				RuleID:     "test-rule",
				EntityKey:  "",
				IsMatching: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create state tracker with mock bucket
			bucket := newMockKVBucket()
			st := NewStateTracker(bucket, nil)

			// Execute
			err := st.Set(ctx, tt.state)

			// Verify
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify we can retrieve what we set
				retrieved, err := st.Get(ctx, tt.state.RuleID, tt.state.EntityKey)
				require.NoError(t, err)
				assert.Equal(t, tt.state.RuleID, retrieved.RuleID)
				assert.Equal(t, tt.state.EntityKey, retrieved.EntityKey)
				assert.Equal(t, tt.state.IsMatching, retrieved.IsMatching)
				assert.Equal(t, tt.state.LastTransition, retrieved.LastTransition)
			}
		})
	}
}

// T035: Test DetectTransition function
func TestDetectTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wasMatching bool
		nowMatching bool
		want        Transition
	}{
		{
			name:        "false to true = entered",
			wasMatching: false,
			nowMatching: true,
			want:        TransitionEntered,
		},
		{
			name:        "true to false = exited",
			wasMatching: true,
			nowMatching: false,
			want:        TransitionExited,
		},
		{
			name:        "true to true = none",
			wasMatching: true,
			nowMatching: true,
			want:        TransitionNone,
		},
		{
			name:        "false to false = none",
			wasMatching: false,
			nowMatching: false,
			want:        TransitionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectTransition(tt.wasMatching, tt.nowMatching)
			if got != tt.want {
				t.Errorf("DetectTransition(%v, %v) = %v, want %v",
					tt.wasMatching, tt.nowMatching, got, tt.want)
			}
		})
	}
}

// T035a: Test Transition constants
func TestTransitionConstants(t *testing.T) {
	t.Parallel()

	// Verify constants exist and have correct values
	assert.Equal(t, Transition(""), TransitionNone)
	assert.Equal(t, Transition("entered"), TransitionEntered)
	assert.Equal(t, Transition("exited"), TransitionExited)

	// Verify they can be used in comparisons
	trans := TransitionEntered
	assert.True(t, trans == TransitionEntered)
	assert.False(t, trans == TransitionExited)
	assert.False(t, trans == TransitionNone)
}

// T036: Test StateTracker.Delete
func TestStateTracker_Delete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name      string
		ruleID    string
		entityKey string
		setup     func(*StateTracker) error
		wantErr   bool
	}{
		{
			name:      "delete existing state",
			ruleID:    "low-battery",
			entityKey: "c360.platform1.robotics.mav1.drone.001",
			setup: func(st *StateTracker) error {
				state := MatchState{
					RuleID:     "low-battery",
					EntityKey:  "c360.platform1.robotics.mav1.drone.001",
					IsMatching: true,
				}
				return st.Set(ctx, state)
			},
			wantErr: false,
		},
		{
			name:      "delete non-existent state (no error)",
			ruleID:    "unknown",
			entityKey: "c360.platform1.robotics.mav1.drone.999",
			setup:     func(_ *StateTracker) error { return nil },
			wantErr:   false, // Deleting non-existent should be idempotent
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create state tracker with mock bucket
			bucket := newMockKVBucket()
			st := NewStateTracker(bucket, nil)

			if tt.setup != nil {
				err := tt.setup(st)
				require.NoError(t, err)
			}

			err := st.Delete(ctx, tt.ruleID, tt.entityKey)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify it's really deleted
				_, err := st.Get(ctx, tt.ruleID, tt.entityKey)
				assert.Error(t, err, "Get should fail after delete")
			}
		})
	}
}
