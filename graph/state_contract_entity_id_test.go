package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// TestStateContractError_ErrorRendering pins the rendered message with and
// without the additive EntityID stamp (poison-response-scoping D4). The code
// and reason lead in every shape so operators and log greps stay stable.
func TestStateContractError_ErrorRendering(t *testing.T) {
	cause := errors.New("boom")
	cases := []struct {
		name string
		err  *StateContractError
		want string
	}{
		{name: "nil receiver", err: nil, want: ErrorCodeGraphStateResetRequired},
		{
			name: "reason only",
			err:  &StateContractError{Reason: GraphStateReasonUnreadableEntity},
			want: "graph_state_reset_required: unreadable_entity_state",
		},
		{
			name: "reason and cause",
			err:  &StateContractError{Reason: GraphStateReasonNoncanonicalPredicate, Err: cause},
			want: "graph_state_reset_required: noncanonical_predicate: boom",
		},
		{
			name: "entity id without cause",
			err: &StateContractError{
				Reason:   GraphStateReasonNoncanonicalEntityID,
				EntityID: "acme.ops.robotics.gcs.sensor.001",
			},
			want: "graph_state_reset_required: noncanonical_entity_id: entity acme.ops.robotics.gcs.sensor.001",
		},
		{
			name: "entity id with cause",
			err: &StateContractError{
				Reason:   GraphStateReasonNoncanonicalPredicate,
				EntityID: "acme.ops.robotics.gcs.sensor.001",
				Err:      cause,
			},
			want: "graph_state_reset_required: noncanonical_predicate: entity acme.ops.robotics.gcs.sensor.001: boom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStateContractError_JSONShape pins the explicit JSON tags: this type is
// not meant to cross the wire directly (only class/code headers do), but if it
// is ever marshaled the shape must be deterministic — Reason and EntityID with
// snake_case keys, EntityID omitted when empty, and the unmarshalable Err
// interface excluded entirely.
func TestStateContractError_JSONShape(t *testing.T) {
	t.Run("without entity id", func(t *testing.T) {
		data, err := json.Marshal(&StateContractError{
			Reason: GraphStateReasonUnreadableEntity,
			Err:    errors.New("must not appear"),
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		want := `{"reason":"unreadable_entity_state"}`
		if string(data) != want {
			t.Fatalf("marshal = %s, want %s", data, want)
		}
	})
	t.Run("with entity id", func(t *testing.T) {
		data, err := json.Marshal(&StateContractError{
			Reason:   GraphStateReasonNoncanonicalPredicate,
			EntityID: "acme.ops.robotics.gcs.sensor.001",
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		want := `{"reason":"noncanonical_predicate","entity_id":"acme.ops.robotics.gcs.sensor.001"}`
		if string(data) != want {
			t.Fatalf("marshal = %s, want %s", data, want)
		}
	})
}

// TestStateContractError_EntityIDSurvivesClassification asserts the stamped
// entity ID stays reachable via errors.As through the shared classification
// wrapper and additional fmt wrapping — the property aggregate reads and the
// poison inventory rely on (poison-response-scoping D4/D5).
func TestStateContractError_EntityIDSurvivesClassification(t *testing.T) {
	stamped := &StateContractError{
		Reason:   GraphStateReasonNoncanonicalPredicate,
		EntityID: "acme.ops.robotics.gcs.sensor.001",
	}
	wrapped := fmt.Errorf("unmarshal entity state: %w", ClassifyStateContractError(stamped))

	var got *StateContractError
	if !errors.As(wrapped, &got) {
		t.Fatalf("errors.As failed through classification wrap: %v", wrapped)
	}
	if got.EntityID != stamped.EntityID {
		t.Fatalf("EntityID = %q, want %q", got.EntityID, stamped.EntityID)
	}
	if !IsStateContractError(wrapped) {
		t.Fatal("IsStateContractError must hold through the wrap")
	}
}
