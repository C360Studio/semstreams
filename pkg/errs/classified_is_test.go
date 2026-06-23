package errs

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// These tests lock the two LOAD-BEARING guards in (*ClassifiedError).Is
// (ADR-060). Removing either guard re-opens the silent-corruption class
// the unified RPC error contract closes:
//
//  1. target must resolve to a *ClassifiedError (else any non-classified
//     sentinel — context.Canceled, a gateway's plain errEntityNotFound —
//     would be mishandled here instead of falling through to Unwrap).
//  2. the target sentinel's Code must be NON-EMPTY (else "" == "" makes
//     every uncoded ClassifiedError false-match — and every
//     wire-reconstructed error was uncoded before ADR-060).
//
// If a future refactor drops a guard, the corresponding test below fails.

func TestClassifiedError_Is_SentinelMatch(t *testing.T) {
	t.Parallel()
	reconstructed := ClassifiedCode(ErrorInvalid, "revision_mismatch", errors.New("revision mismatch: expected 5, current 7"))
	if !errors.Is(reconstructed, ErrRevisionMismatch) {
		t.Fatal("errors.Is(reconstructed revision_mismatch, ErrRevisionMismatch) = false, want true")
	}
}

func TestClassifiedError_Is_DifferentCodeNoMatch(t *testing.T) {
	t.Parallel()
	notFound := ClassifiedCode(ErrorInvalid, "entity_not_found", errors.New("not found: x"))
	if errors.Is(notFound, ErrRevisionMismatch) {
		t.Fatal("errors.Is(entity_not_found, ErrRevisionMismatch) = true, want false (different code)")
	}
}

// Guard 2: the non-empty-Code guard. Without it, every uncoded
// ClassifiedError would match an empty-Code sentinel-shaped target.
func TestClassifiedError_Is_EmptyCodeNoFalseMatch(t *testing.T) {
	t.Parallel()
	emptySentinel := &ClassifiedError{Class: ErrorInvalid} // Code "", Err nil — the dangerous shape
	uncoded := Classified(ErrorInvalid, errors.New("some handler error"))
	if errors.Is(uncoded, emptySentinel) {
		t.Fatal("errors.Is(uncoded, emptyCodeSentinel) = true, want false — the empty-Code guard is gone")
	}
	// Two coded errors with the SAME code but a real (non-nil Err) target
	// must not match either — only a sentinel (Err==nil) is a valid target.
	realTargetSameCode := ClassifiedCode(ErrorInvalid, "revision_mismatch", errors.New("other"))
	reconstructed := ClassifiedCode(ErrorInvalid, "revision_mismatch", errors.New("x"))
	if errors.Is(reconstructed, realTargetSameCode) {
		t.Fatal("errors.Is(coded, non-sentinel-coded) = true, want false — the Err==nil guard is gone")
	}
}

// Guard 1: the type-assert guard. errors.Is against a non-ClassifiedError
// target must return false from Is and fall through to the Unwrap walk.
func TestClassifiedError_Is_NonClassifiedTarget(t *testing.T) {
	t.Parallel()
	coded := ClassifiedCode(ErrorInvalid, "revision_mismatch", errors.New("x"))
	if errors.Is(coded, context.Canceled) {
		t.Fatal("errors.Is(coded, context.Canceled) = true, want false")
	}
	if errors.Is(coded, errors.New("unrelated plain error")) {
		t.Fatal("errors.Is(coded, plainError) = true, want false")
	}
}

func TestClassifiedError_Is_WrappedSentinel(t *testing.T) {
	t.Parallel()
	reconstructed := ClassifiedCode(ErrorInvalid, "revision_mismatch", errors.New("x"))
	wrapped := fmt.Errorf("transition acme.x: %w", reconstructed)
	if !errors.Is(wrapped, ErrRevisionMismatch) {
		t.Fatal("errors.Is(fmt.Errorf(%%w), ErrRevisionMismatch) = false, want true — class/code must survive the wrap")
	}
}

// The new Is method must NOT break the pre-existing Unwrap-based matching:
// a ClassifiedError wrapping a plain sentinel still matches that sentinel.
func TestClassifiedError_Is_UnwrapStillWorks(t *testing.T) {
	t.Parallel()
	wrapping := ClassifiedCode(ErrorTransient, "internal", ErrKeyNotFound)
	if !errors.Is(wrapping, ErrKeyNotFound) {
		t.Fatal("errors.Is(ClassifiedCode(.., ErrKeyNotFound), ErrKeyNotFound) = false, want true (Unwrap)")
	}
}

// Error() must be nil-safe: the sentinel carries no wrapped Err.
func TestClassifiedError_Error_SentinelNoPanic(t *testing.T) {
	t.Parallel()
	if got := ErrRevisionMismatch.Error(); got != "revision_mismatch" {
		t.Fatalf("ErrRevisionMismatch.Error() = %q, want %q", got, "revision_mismatch")
	}
	// A bare classified error with neither Message nor Err nor Code.
	if got := (&ClassifiedError{}).Error(); got != "classified error" {
		t.Fatalf("empty ClassifiedError.Error() = %q, want %q", got, "classified error")
	}
}

// Consumers' existing IsInvalid/IsTransient branches keep working on the
// coded constructors (the whole point of extending ClassifiedError rather
// than minting a new type).
func TestClassifiedCode_ClassDetectionUnchanged(t *testing.T) {
	t.Parallel()
	if !IsInvalid(ClassifiedCode(ErrorInvalid, "entity_not_found", errors.New("x"))) {
		t.Error("IsInvalid(ClassifiedCode(ErrorInvalid, ...)) = false, want true")
	}
	if !IsTransient(ClassifiedCode(ErrorTransient, "internal", errors.New("x"))) {
		t.Error("IsTransient(ClassifiedCode(ErrorTransient, ...)) = false, want true")
	}
}

func TestClassifiedCode_NilErrReturnsNil(t *testing.T) {
	t.Parallel()
	if ClassifiedCode(ErrorInvalid, "x", nil) != nil {
		t.Error("ClassifiedCode(.., nil) != nil, want nil")
	}
	if ClassifiedCodeDetail(ErrorInvalid, "x", map[string]any{"a": 1}, nil) != nil {
		t.Error("ClassifiedCodeDetail(.., nil) != nil, want nil")
	}
}

func TestClassifiedCodeDetail_CodeAndDetailReachable(t *testing.T) {
	t.Parallel()
	err := ClassifiedCodeDetail(ErrorInvalid, "entity_not_found",
		map[string]any{"entity": "acme.ops.robotics.gcs.drone.001"},
		errors.New("not found"))
	var ce *ClassifiedError
	if !errors.As(err, &ce) {
		t.Fatal("errors.As failed")
	}
	if ce.Code != "entity_not_found" {
		t.Errorf("ce.Code = %q, want entity_not_found", ce.Code)
	}
	if got, _ := ce.Detail["entity"].(string); got != "acme.ops.robotics.gcs.drone.001" {
		t.Errorf("ce.Detail[entity] = %v, want the drone id", ce.Detail["entity"])
	}
}
