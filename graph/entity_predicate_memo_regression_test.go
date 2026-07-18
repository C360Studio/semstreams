package graph

// entity_predicate_memo_regression_test.go — gh#562 write-cost follow-up.
// vocabulary.ParsePredicate memoizes VALID predicates; the ENTITY_STATES
// contract seam must keep rejecting a poisoned predicate with the memo warm —
// a poisoned predicate is never cached, so the MarshalEntityState write gate's
// merged-union validation still catches resident poison riding a valid delta.

import (
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func TestValidateEntityPredicates_WarmMemoStillRejectsPoison(t *testing.T) {
	const entityID = "acme.ops.test.system.widget.001"
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	triple := func(predicate string) message.Triple {
		return message.Triple{
			Subject: entityID, Predicate: predicate, Object: "v",
			Timestamp: now, Confidence: 1.0,
		}
	}
	valid := &EntityState{ID: entityID, Triples: []message.Triple{
		triple("test.state.value"),
		triple("test.state.phase"),
	}}

	// Warm the predicate memo through the production contract seam.
	if err := ValidateEntityStateContract(valid); err != nil {
		t.Fatalf("valid entity rejected while warming: %v", err)
	}

	// The poisoned union (valid, memo-warm predicates + one noncanonical
	// predicate — the resident-poison shape of the no-laundering matrix) must
	// keep rejecting on every pass, cold and warm.
	poisoned := &EntityState{ID: entityID, Triples: []message.Triple{
		triple("test.state.value"),
		triple("Test.State.Value"), // predicate-audit:invalid {"kind":"stored-predicate","value":"Test.State.Value","reason":"segment_start"}
		triple("test.state.phase"),
	}}
	for _, phase := range []string{"cold", "warm"} {
		err := ValidateEntityStateContract(poisoned)
		if err == nil {
			t.Fatalf("%s: poisoned union accepted with memo warm", phase)
		}
		var predicateErr *EntityPredicateContractError
		if !errors.As(err, &predicateErr) {
			t.Fatalf("%s: unexpected error type %T: %v", phase, err, err)
		}
		if len(predicateErr.Violations) != 1 || predicateErr.Violations[0].Predicate != "Test.State.Value" {
			t.Fatalf("%s: violations = %+v, want exactly the poisoned predicate", phase, predicateErr.Violations)
		}
	}

	// And MarshalEntityState — the authoritative write gate — must refuse to
	// serialize the poisoned union.
	if _, err := MarshalEntityState(poisoned); err == nil {
		t.Fatal("write gate serialized a poisoned union with memo warm")
	}
}
