// Package rule - State tracking for stateful ECA rules
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Transition represents the type of state change detected
type Transition string

const (
	// TransitionNone indicates no state change
	TransitionNone Transition = ""
	// TransitionEntered indicates condition became true
	TransitionEntered Transition = "entered"
	// TransitionExited indicates condition became false
	TransitionExited Transition = "exited"
)

// MatchState tracks whether a rule's condition is currently matching
// for a specific entity or entity pair.
type MatchState struct {
	RuleID         string    `json:"rule_id"`
	EntityKey      string    `json:"entity_key"`
	IsMatching     bool      `json:"is_matching"`
	LastTransition string    `json:"last_transition"` // ""|"entered"|"exited"
	TransitionAt   time.Time `json:"transition_at,omitempty"`
	SourceRevision uint64    `json:"source_revision"`
	LastChecked    time.Time `json:"last_checked"`

	// Iteration tracks how many times this rule has entered the matching state
	// for this entity. Incremented on each TransitionEntered, preserved through exits.
	Iteration int `json:"iteration"`

	// MaxIterations is the configured limit from the rule Definition.
	// 0 means no limit. Used with $state.iteration in When clauses for retry budgets.
	MaxIterations int `json:"max_iterations,omitempty"`

	// FieldValues tracks the last-seen values of fields used in transition conditions.
	// Keys are field names (e.g., "workflow.plan.status"), values are the string
	// representation of the last observed value. Used by the transition operator
	// to detect field-level state changes across evaluations.
	FieldValues map[string]string `json:"field_values,omitempty"`

	// ActionIterations tracks per-action firing counts for the
	// Action.MaxIterations cap. Keys are Action.effectiveID(rule_id) —
	// either the author's explicit Action.ID or the deterministic hash
	// of (rule_id, action_type, subject_or_predicate, role). Counts
	// persist across rule entries/exits so the cap covers the entire
	// match-cycle for an entity, not just one transition.
	//
	// When an action's effectiveID isn't yet a key in this map, its
	// count is treated as 0 (unfired). The first fire writes the entry.
	ActionIterations map[string]int `json:"action_iterations,omitempty"`
}

// StateTracker manages rule match state persistence in NATS KV.
// It tracks which rules are currently matching for which entities
// to enable proper transition detection for stateful ECA rules.
type StateTracker struct {
	bucket jetstream.KeyValue
	logger *slog.Logger
}

// ErrStateNotFound is returned when no rule state exists for the given key
var ErrStateNotFound = errors.New("rule state not found")

// NewStateTracker creates a new StateTracker with the given KV bucket.
func NewStateTracker(bucket jetstream.KeyValue, logger *slog.Logger) *StateTracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &StateTracker{
		bucket: bucket,
		logger: logger,
	}
}

// DetectTransition compares previous and current match state to determine transition type.
// Returns TransitionEntered if condition changed from false to true.
// Returns TransitionExited if condition changed from true to false.
// Returns TransitionNone if state did not change.
func DetectTransition(wasMatching, nowMatching bool) Transition {
	if !wasMatching && nowMatching {
		return TransitionEntered
	}
	if wasMatching && !nowMatching {
		return TransitionExited
	}
	return TransitionNone
}

// Get retrieves the current match state for a rule and entity key.
// Returns ErrStateNotFound if no state exists for this combination.
func (st *StateTracker) Get(ctx context.Context, ruleID, entityKey string) (MatchState, error) {
	// Validate inputs
	if ruleID == "" {
		return MatchState{}, fmt.Errorf("rule ID cannot be empty")
	}
	if entityKey == "" {
		return MatchState{}, fmt.Errorf("entity key cannot be empty")
	}

	// Handle nil bucket (for tests)
	if st.bucket == nil {
		return MatchState{}, fmt.Errorf("state tracker bucket is nil")
	}

	key := buildStateKey(ruleID, entityKey)

	entry, err := st.bucket.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return MatchState{}, ErrStateNotFound
		}
		return MatchState{}, fmt.Errorf("get rule state: %w", err)
	}

	var state MatchState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return MatchState{}, fmt.Errorf("unmarshal rule state: %w", err)
	}

	return state, nil
}

// Set persists the match state for a rule and entity.
func (st *StateTracker) Set(ctx context.Context, state MatchState) error {
	// Validate inputs
	if state.RuleID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}
	if state.EntityKey == "" {
		return fmt.Errorf("entity key cannot be empty")
	}

	// Handle nil bucket (for tests)
	if st.bucket == nil {
		return fmt.Errorf("state tracker bucket is nil")
	}

	key := buildStateKey(state.RuleID, state.EntityKey)

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal rule state: %w", err)
	}

	_, err = st.bucket.Put(ctx, key, data)
	if err != nil {
		return fmt.Errorf("put rule state: %w", err)
	}

	return nil
}

// Delete removes the match state for a rule and entity key.
func (st *StateTracker) Delete(ctx context.Context, ruleID, entityKey string) error {
	// Handle nil bucket (for tests)
	if st.bucket == nil {
		return fmt.Errorf("state tracker bucket is nil")
	}

	key := buildStateKey(ruleID, entityKey)

	err := st.bucket.Delete(ctx, key)
	if err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return fmt.Errorf("delete rule state: %w", err)
	}

	return nil
}

// DeleteAllForEntity removes all rule states associated with an entity.
// This is called when an entity is deleted to clean up orphaned state.
func (st *StateTracker) DeleteAllForEntity(ctx context.Context, entityID string) error {
	// Handle nil bucket (for tests)
	if st.bucket == nil {
		return fmt.Errorf("state tracker bucket is nil")
	}

	// List all keys and delete those containing the entityID
	keys, err := st.bucket.Keys(ctx)
	if err != nil {
		return fmt.Errorf("list rule state keys: %w", err)
	}

	for _, key := range keys {
		// Check if key contains the entityID (either as single entity or in pair)
		if containsEntityID(key, entityID) {
			if err := st.bucket.Delete(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
				st.logger.Warn("Failed to delete rule state",
					"key", key,
					"entity_id", entityID,
					"error", err)
			}
		}
	}

	return nil
}

// buildStateKey creates a key for storing rule state in KV.
// Format: ruleID.entityKey (using dots as separators since NATS KV doesn't allow colons)
func buildStateKey(ruleID, entityKey string) string {
	return ruleID + "." + entityKey
}

// containsEntityID checks if a state key contains the given entity ID.
// Key format: ruleID.entityKey where entityKey is either "entityID" or "entityID1_entityID2"
// Since both ruleID and entityKey may contain dots, we just check if the key contains the entityID.
func containsEntityID(key, entityID string) bool {
	return strings.Contains(key, entityID)
}

// buildPairKey creates a canonical entity pair key with IDs sorted alphabetically.
// This ensures the same key is generated regardless of which entity is "entity" vs "related".
// Uses underscore as separator to avoid conflicting with dots in entity IDs.
func buildPairKey(entityID1, entityID2 string) string {
	if entityID1 < entityID2 {
		return entityID1 + "_" + entityID2
	}
	return entityID2 + "_" + entityID1
}
