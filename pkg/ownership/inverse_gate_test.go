package ownership

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
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
			return "sensorml.component.is-hosted-by", true
		}
		return "", false
	}
	t.Run("conditional with registered inverse passes", func(t *testing.T) {
		if err := CheckInverseGate(resolve, ForeignEdgeClaim{Owner: "o", Predicate: "sensorml.system.hosts", TargetPattern: sysPat, Mode: EdgeConditional}); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})
	t.Run("conditional without inverse fails", func(t *testing.T) {
		if err := CheckInverseGate(resolve, ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeConditional}); !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("want ErrInvalidClaim, got %v", err)
		}
	})
	t.Run("backfill without inverse fails", func(t *testing.T) {
		if err := CheckInverseGate(resolve, ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeBackfill}); !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("want ErrInvalidClaim, got %v", err)
		}
	})
	t.Run("strict without inverse passes (not gated)", func(t *testing.T) {
		if err := CheckInverseGate(resolve, ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeStrict}); err != nil {
			t.Errorf("strict must never be gated, got %v", err)
		}
	})
	t.Run("no-birth-stub without inverse passes (the sensorml case)", func(t *testing.T) {
		if err := CheckInverseGate(resolve, ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeNoBirthStub}); err != nil {
			t.Errorf("no-birth-stub must never be gated, got %v", err)
		}
	})
	t.Run("nil resolver errors", func(t *testing.T) {
		if err := CheckInverseGate(nil, ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "test", "edge", "p"), TargetPattern: sysPat, Mode: EdgeConditional}); !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("nil resolver must error, got %v", err)
		}
	})
	t.Run("first offending claim in a batch is reported", func(t *testing.T) {
		err := CheckInverseGate(resolve,
			ForeignEdgeClaim{Owner: "o", Predicate: "sensorml.system.hosts", TargetPattern: sysPat, Mode: EdgeConditional},
			ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeConditional},
		)
		if !errors.Is(err, ErrInvalidClaim) {
			t.Errorf("want ErrInvalidClaim for the offending claim, got %v", err)
		}
	})
}
