package ownership

import (
	"errors"
	"testing"
)

func TestEdgeMode_requiresInverse(t *testing.T) {
	if !EdgeConditional.requiresInverse() {
		t.Error("conditional must require an inverse (deferred-apply)")
	}
	if !EdgeBackfill.requiresInverse() {
		t.Error("backfill must require an inverse (re-derives from forward)")
	}
	if EdgeStrict.requiresInverse() {
		t.Error("strict drops-if-absent — no inverse needed")
	}
	if EdgeNoBirthStub.requiresInverse() {
		t.Error("no-birth-stub materialises a stub — no inverse needed (the sensorml case)")
	}
}

func TestCheckInverseGate(t *testing.T) {
	// A resolver that knows one predicate's inverse (mirrors the 8 hierarchy/
	// delegation predicates that actually carry WithInverseOf).
	resolve := func(p string) (string, bool) {
		if p == "sensorml.system.hosts" {
			return "sensorml.component.isHostedBy", true
		}
		return "", false
	}
	const noInverse = "sensorml.component.isHostedBy" // WithIRI only, no inverse

	t.Run("conditional with registered inverse passes", func(t *testing.T) {
		if err := CheckInverseGate(resolve, fe("o", "sensorml.system.hosts", sysPat, EdgeConditional)); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})
	t.Run("conditional without inverse fails", func(t *testing.T) {
		if err := CheckInverseGate(resolve, fe("o", noInverse, sysPat, EdgeConditional)); !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("want ErrInvalidClaim, got %v", err)
		}
	})
	t.Run("backfill without inverse fails", func(t *testing.T) {
		if err := CheckInverseGate(resolve, fe("o", noInverse, sysPat, EdgeBackfill)); !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("want ErrInvalidClaim, got %v", err)
		}
	})
	t.Run("strict without inverse passes (not gated)", func(t *testing.T) {
		if err := CheckInverseGate(resolve, fe("o", noInverse, sysPat, EdgeStrict)); err != nil {
			t.Errorf("strict must never be gated, got %v", err)
		}
	})
	t.Run("no-birth-stub without inverse passes (the sensorml case)", func(t *testing.T) {
		if err := CheckInverseGate(resolve, fe("o", noInverse, sysPat, EdgeNoBirthStub)); err != nil {
			t.Errorf("no-birth-stub must never be gated, got %v", err)
		}
	})
	t.Run("nil resolver errors", func(t *testing.T) {
		if err := CheckInverseGate(nil, fe("o", "p", sysPat, EdgeConditional)); !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("nil resolver must error, got %v", err)
		}
	})
	t.Run("first offending claim in a batch is reported", func(t *testing.T) {
		err := CheckInverseGate(resolve,
			fe("o", "sensorml.system.hosts", sysPat, EdgeConditional), // ok
			fe("o", noInverse, sysPat, EdgeConditional),               // offends
		)
		if !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("want ErrInvalidClaim for the offending claim, got %v", err)
		}
	})
}
