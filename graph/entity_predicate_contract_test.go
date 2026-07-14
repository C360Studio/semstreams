package graph

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

func TestValidateEntityPredicatesReportsAllUniqueViolations(t *testing.T) {
	t.Parallel()

	entity := &EntityState{Triples: []message.Triple{
		{Predicate: "valid.fact.name"},
		{Predicate: "bad.two"},
		{Predicate: "bad.two"},
		{Predicate: "bad.fact.has_underscore"},
	}}

	err := ValidateEntityPredicates(entity)
	var contractErr *EntityPredicateContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("ValidateEntityPredicates() error = %T %v, want *EntityPredicateContractError", err, err)
	}
	want := []InvalidEntityPredicate{
		{Predicate: "bad.fact.has_underscore", Reason: vocabulary.PredicateReasonSegmentCharacter},
		{Predicate: "bad.two", Reason: vocabulary.PredicateReasonArity},
	}
	if len(contractErr.Violations) != len(want) {
		t.Fatalf("violations = %#v, want %#v", contractErr.Violations, want)
	}
	for i := range want {
		if contractErr.Violations[i] != want[i] {
			t.Fatalf("violations[%d] = %#v, want %#v", i, contractErr.Violations[i], want[i])
		}
	}
}

func TestMarshalEntityStateRejectsInvalidFinalCandidate(t *testing.T) {
	t.Parallel()

	entity := &EntityState{Triples: []message.Triple{{Predicate: "legacy.invalid_name"}}}
	data, err := MarshalEntityState(entity)
	if err == nil {
		t.Fatal("MarshalEntityState() error = nil, want predicate contract error")
	}
	if data != nil {
		t.Fatalf("MarshalEntityState() data = %q, want nil", data)
	}
}

func TestUnmarshalEntityStateReturnsTypedResetReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   []byte
		reason StateResetReason
	}{
		{name: "unreadable", data: []byte("{"), reason: GraphStateReasonUnreadableEntity},
		{
			name:   "noncanonical predicate",
			data:   []byte(`{"id":"acme.ops.test.system.widget.001","triples":[{"predicate":"bad.two"}]}`),
			reason: GraphStateReasonNoncanonicalPredicate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entity EntityState
			err := UnmarshalEntityState(tt.data, &entity)
			var stateErr *StateContractError
			if !errors.As(err, &stateErr) {
				t.Fatalf("UnmarshalEntityState() error = %T %v, want *StateContractError", err, err)
			}
			if stateErr.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", stateErr.Reason, tt.reason)
			}
		})
	}
}
