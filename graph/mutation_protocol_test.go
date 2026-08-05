package graph

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
)

func TestFourMutationRequestShapesExcludeRetiredFields(t *testing.T) {
	entity := &EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
	triple := message.Triple{Subject: entity.ID, Predicate: "sensor.state.value", Object: "ready"}
	tests := []struct {
		name    string
		request any
		want    []string
		retired []string
	}{
		{
			name: "create",
			request: CreateEntityRequest{
				Entity: entity, Triples: []message.Triple{triple}, IndexingProfile: "signal", RequestID: "req-1",
			},
			want:    []string{"entity", "triples", "indexing_profile", "request_id"},
			retired: []string{"owner_token", "expected_revision", "add_triples", "remove_triples"},
		},
		{
			name: "reconcile",
			request: ReconcilePredicatesRequest{
				EntityID: entity.ID, ExpectedRevision: 7, Predicates: []string{triple.Predicate},
				Desired: []message.Triple{triple}, RequestID: "req-2",
			},
			want:    []string{"entity_id", "expected_revision", "predicates", "desired", "request_id"},
			retired: []string{"owner_token", "add_triples", "remove_triples", "indexing_profile"},
		},
		{
			name:    "append",
			request: AppendTriplesRequest{Triples: []message.Triple{triple}, RequestID: "req-3"},
			want:    []string{"triples", "request_id"},
			retired: []string{"owner_token", "expected_revision", "failed_subjects"},
		},
		{
			name:    "delete",
			request: DeleteEntityRequest{EntityID: entity.ID, ExpectedRevision: 9, RequestID: "req-4"},
			want:    []string{"entity_id", "expected_revision", "request_id"},
			retired: []string{"owner_token", "deleted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("Unmarshal fields: %v", err)
			}
			for _, field := range tt.want {
				if _, ok := fields[field]; !ok {
					t.Errorf("missing field %q in %s", field, data)
				}
			}
			for _, field := range tt.retired {
				if _, ok := fields[field]; ok {
					t.Errorf("retired field %q present in %s", field, data)
				}
			}
		})
	}
}

func TestFourMutationResponseShapesExcludeRetiredFields(t *testing.T) {
	entity := &EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
	tests := []struct {
		name     string
		response any
		want     []string
		retired  []string
	}{
		{name: "create", response: CreateEntityResponse{Outcome: MutationApplied, Entity: entity, KVRevision: 1},
			want: []string{"outcome", "entity", "kv_revision"}, retired: []string{"degraded", "deleted", "changed", "error_code"}},
		{name: "reconcile", response: ReconcilePredicatesResponse{Outcome: MutationUnchanged, Entity: entity, KVRevision: 1},
			want: []string{"outcome", "entity", "kv_revision"}, retired: []string{"degraded", "deleted", "changed", "error_code"}},
		{name: "append", response: AppendTriplesResponse{Results: []AppendSubjectResult{{EntityID: entity.ID, Outcome: MutationUnchanged}}},
			want: []string{"results"}, retired: []string{"degraded", "failed_subjects", "written_count", "error_code"}},
		{name: "delete", response: DeleteEntityResponse{EntityID: entity.ID, Outcome: MutationApplied, ExpectedRevision: 1},
			want: []string{"entity_id", "outcome", "expected_revision"}, retired: []string{"degraded", "deleted", "kv_revision", "error_code"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			for _, field := range tt.want {
				if _, ok := fields[field]; !ok {
					t.Errorf("missing field %q in %s", field, data)
				}
			}
			for _, field := range tt.retired {
				if _, ok := fields[field]; ok {
					t.Errorf("retired field %q present in %s", field, data)
				}
			}
		})
	}
}

func TestMutationOutcomeVocabularyIsClosed(t *testing.T) {
	for _, outcome := range []MutationOutcome{
		MutationApplied, MutationUnchanged,
		MutationEntityNotFound, MutationEntityAlreadyExists,
		MutationRevisionMismatch, MutationInvalid, MutationFailed,
	} {
		if !isServerMutationOutcome(outcome) {
			t.Fatalf("declared outcome %q rejected", outcome)
		}
	}
	for _, outcome := range []MutationOutcome{"", "commit_unknown", "unavailable", "anything"} {
		if isServerMutationOutcome(outcome) {
			t.Fatalf("undeclared server outcome %q accepted", outcome)
		}
	}
}
