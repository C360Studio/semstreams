package graph

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

func TestPredicateValidationErrorSupportsWrappedCauses(t *testing.T) {
	t.Parallel()

	want := &vocabulary.PredicateValidationError{Reason: vocabulary.PredicateReasonArity}
	got, err := predicateViolationFromError("bad.two", fmt.Errorf("wrapped parser error: %w", want))
	if err != nil || got.Reason != want.Reason || got.Predicate != "bad.two" {
		t.Fatalf("predicateViolationFromError() = %#v, %v; want wrapped typed cause", got, err)
	}
}

func TestPredicateValidationErrorFailsClosedOnUnexpectedCause(t *testing.T) {
	t.Parallel()

	got, err := predicateViolationFromError("bad.two", errors.New("unexpected parser failure"))
	if err == nil {
		t.Fatalf("predicateViolationFromError() = %#v, nil; want fail-closed error", got)
	}
}

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
			name:   "noncanonical entity id",
			data:   []byte(`{"id":"bad","triples":[]}`),
			reason: GraphStateReasonNoncanonicalEntityID,
		},
		{
			name:   "noncanonical predicate",
			data:   []byte(`{"id":"acme.ops.test.system.widget.001","triples":[{"subject":"acme.ops.test.system.widget.001","predicate":"bad.two"}]}`),
			reason: GraphStateReasonNoncanonicalPredicate,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entity EntityState
			err := UnmarshalEntityState(tt.data, &entity)
			var classified *errs.ClassifiedError
			if !errors.As(err, &classified) {
				t.Fatalf("UnmarshalEntityState() error = %T %v, want *errs.ClassifiedError", err, err)
			}
			if classified.Class != errs.ErrorFatal || classified.Code != ErrorCodeGraphStateResetRequired {
				t.Fatalf("classification = %s/%q, want fatal/%q", classified.Class, classified.Code, ErrorCodeGraphStateResetRequired)
			}
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

func TestValidateEntityStateContractIdentityAndReferenceRules(t *testing.T) {
	t.Parallel()

	maxID := "a.b.c.d.e." + strings.Repeat("f", semtypes.MaxEntityIDBytes-10)
	validID := "acme.ops.test.system.widget.001"
	validRef := "acme.ops.test.system.widget.002"

	tests := []struct {
		name      string
		entity    *EntityState
		wantField EntityStateContractField
		wantIndex int
	}{
		{
			name: "maximum root and subject",
			entity: &EntityState{ID: maxID, Triples: []message.Triple{{
				Subject: maxID, Predicate: "test.state.value", Object: "ok",
			}}},
			wantIndex: -1,
		},
		{
			name:      "invalid root",
			entity:    &EntityState{ID: "bad", Triples: []message.Triple{{Subject: validID, Predicate: "test.state.value"}}},
			wantField: EntityStateContractFieldID,
			wantIndex: -1,
		},
		{
			name:      "empty subject",
			entity:    &EntityState{ID: validID, Triples: []message.Triple{{Subject: "", Predicate: "test.state.value"}}},
			wantField: EntityStateContractFieldSubject,
			wantIndex: 0,
		},
		{
			name:      "malformed subject",
			entity:    &EntityState{ID: validID, Triples: []message.Triple{{Subject: "bad", Predicate: "test.state.value"}}},
			wantField: EntityStateContractFieldSubject,
			wantIndex: 0,
		},
		{
			name: "canonical explicit reference",
			entity: &EntityState{ID: validID, Triples: []message.Triple{{
				Subject: validID, Predicate: "test.state.target", Object: validRef, Datatype: message.EntityReferenceDatatype,
			}}},
			wantIndex: -1,
		},
		{
			name: "malformed explicit reference",
			entity: &EntityState{ID: validID, Triples: []message.Triple{{
				Subject: validID, Predicate: "test.state.target", Object: "bad", Datatype: message.EntityReferenceDatatype,
			}}},
			wantField: EntityStateContractFieldReference,
			wantIndex: 0,
		},
		{
			name: "non-string explicit reference",
			entity: &EntityState{ID: validID, Triples: []message.Triple{{
				Subject: validID, Predicate: "test.state.target", Object: 42, Datatype: message.EntityReferenceDatatype,
			}}},
			wantField: EntityStateContractFieldReference,
			wantIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEntityStateContract(tt.entity)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateEntityStateContract() error = %v, want nil", err)
				}
				return
			}
			var contractErr *EntityStateContractError
			if !errors.As(err, &contractErr) {
				t.Fatalf("error = %T %v, want *EntityStateContractError", err, err)
			}
			if contractErr.Field != tt.wantField || contractErr.TripleIndex != tt.wantIndex {
				t.Fatalf("contract error = %#v, want field=%q triple=%d", contractErr, tt.wantField, tt.wantIndex)
			}
		})
	}
}

func TestValidateEntityStateContractDeterministicPrecedence(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	entity := &EntityState{
		ID: "bad",
		Triples: []message.Triple{{
			Subject:   "also-bad",
			Predicate: "bad.two",
			Object:    42,
			Datatype:  message.EntityReferenceDatatype,
		}},
	}

	err := ValidateEntityStateContract(entity)
	var contractErr *EntityStateContractError
	if !errors.As(err, &contractErr) || contractErr.Field != EntityStateContractFieldID {
		t.Fatalf("first error = %T %#v, want root ID", err, contractErr)
	}

	entity.ID = validID
	err = ValidateEntityStateContract(entity)
	if !errors.As(err, &contractErr) || contractErr.Field != EntityStateContractFieldSubject {
		t.Fatalf("second error = %T %#v, want subject", err, contractErr)
	}

	entity.Triples[0].Subject = validID
	err = ValidateEntityStateContract(entity)
	if !errors.As(err, &contractErr) || contractErr.Field != EntityStateContractFieldReference {
		t.Fatalf("third error = %T %#v, want reference", err, contractErr)
	}

	entity.Triples[0].Datatype = ""
	err = ValidateEntityStateContract(entity)
	var predicateErr *EntityPredicateContractError
	if !errors.As(err, &predicateErr) {
		t.Fatalf("fourth error = %T %v, want predicate contract", err, err)
	}
}

func TestMarshalAndUnmarshalEntityStateRejectIdentityViolations(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	for _, entity := range []*EntityState{
		{ID: "bad", Triples: []message.Triple{{Subject: validID, Predicate: "test.state.value"}}},
		{ID: validID, Triples: []message.Triple{{Subject: "", Predicate: "test.state.value"}}},
		{ID: validID, Triples: []message.Triple{{Subject: validID, Predicate: "test.state.target", Object: "bad", Datatype: message.EntityReferenceDatatype}}},
	} {
		data, err := MarshalEntityState(entity)
		if err == nil || data != nil {
			t.Fatalf("MarshalEntityState(%#v) = %q, %v; want nil error result", entity, data, err)
		}
	}

	data := []byte(`{"id":"acme.ops.test.system.widget.001","triples":[{"subject":"","predicate":"test.state.value"}]}`)
	var decoded EntityState
	err := UnmarshalEntityState(data, &decoded)
	var stateErr *StateContractError
	if !errors.As(err, &stateErr) || stateErr.Reason != GraphStateReasonNoncanonicalEntityID {
		t.Fatalf("UnmarshalEntityState() error = %T %v, want entity-ID reset reason", err, err)
	}
}

func TestAuthoritativeEntityStateContractRejectsNilCandidate(t *testing.T) {
	t.Parallel()

	err := ValidateEntityStateContract(nil)
	var contractErr *EntityStateContractError
	if !errors.As(err, &contractErr) || contractErr.Field != EntityStateContractFieldID {
		t.Fatalf("ValidateEntityStateContract(nil) error = %T %v, want root-ID contract error", err, err)
	}
	data, err := MarshalEntityState(nil)
	if err == nil || data != nil {
		t.Fatalf("MarshalEntityState(nil) = %q, %v; want nil bytes and error", data, err)
	}
}

func TestValidateDecodedEntityStateCollectionsRejectWholeReply(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	tests := []struct {
		name     string
		entities []EntityState
		field    EntityStateContractField
	}{
		{
			name: "root",
			entities: []EntityState{
				{ID: validID},
				{ID: invalidEntityID},
			},
			field: EntityStateContractFieldID,
		},
		{
			name: "subject",
			entities: []EntityState{{
				ID:      validID,
				Triples: []message.Triple{{Subject: invalidEntityID, Predicate: "test.state.value"}},
			}},
			field: EntityStateContractFieldSubject,
		},
		{
			name: "reference",
			entities: []EntityState{{
				ID: validID,
				Triples: []message.Triple{{
					Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
				}},
			}},
			field: EntityStateContractFieldReference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDecodedEntityStates(tt.entities)
			assertGraphResetContract(t, err, GraphStateReasonNoncanonicalEntityID)
			var contractErr *EntityStateContractError
			if !errors.As(err, &contractErr) || contractErr.Field != tt.field {
				t.Fatalf("error = %T %v, want field %q", err, err, tt.field)
			}
		})
	}

	valid := []EntityState{{ID: validID}}
	if err := ValidateDecodedEntityStates(valid); err != nil {
		t.Fatalf("ValidateDecodedEntityStates(valid) error = %v", err)
	}
	pointers := []*EntityState{&valid[0], nil}
	err := ValidateDecodedEntityStatePointers(pointers)
	assertGraphResetContract(t, err, GraphStateReasonNoncanonicalEntityID)
}

func TestValidateDecodedEntityIDsRejectsWholeReply(t *testing.T) {
	t.Parallel()

	invalidEntityID := "bad"
	err := ValidateDecodedEntityIDs([]string{
		"acme.ops.test.system.widget.001",
		invalidEntityID,
	})
	assertGraphResetContract(t, err, GraphStateReasonNoncanonicalEntityID)
}

func assertGraphResetContract(t *testing.T, err error, reason StateResetReason) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want graph reset contract")
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("error = %T %v, want *errs.ClassifiedError", err, err)
	}
	if classified.Class != errs.ErrorFatal || classified.Code != ErrorCodeGraphStateResetRequired {
		t.Fatalf("classification = %s/%q, want fatal/%q", classified.Class, classified.Code, ErrorCodeGraphStateResetRequired)
	}
	var stateErr *StateContractError
	if !errors.As(err, &stateErr) || stateErr.Reason != reason {
		t.Fatalf("error = %T %v, want reason %q", err, err, reason)
	}
}

// entity-id-audit:classify intentional-malformed "bad" line=283 column=21 surface=go-assignment:invalidEntityID aggregate root subject and reference poison fixture
// entity-id-audit:classify intentional-malformed "bad" line=340 column=21 surface=go-assignment:invalidEntityID identity-only aggregate poison fixture
