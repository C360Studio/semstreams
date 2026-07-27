package vocabulary

// predicate_memo_test.go — gh#562 follow-up: the validated-predicate memo must
// never change ParsePredicate's verdict, and an INVALID string must never
// enter the memo (cache poisoning impossible), cold or warm, sequential or
// concurrent.

import (
	"strings"
	"sync"
	"testing"
)

// memoTestPredicates are valid predicates used to warm the memo. Distinct from
// other fixtures so tests stay deterministic regardless of execution order
// within the package (the memo is package-global and only ever accumulates
// VALID predicates).
var memoTestPredicates = []string{
	"memo.test.alpha",
	"memo.test.beta-2",
	"memo.test.gamma",
}

func warmPredicateMemo(t *testing.T) {
	t.Helper()
	for _, predicate := range memoTestPredicates {
		if _, err := ParsePredicate(predicate); err != nil {
			t.Fatalf("warming memo with valid predicate %q: %v", predicate, err)
		}
	}
}

func TestParsePredicate_MemoizedHitMatchesColdParse(t *testing.T) {
	for _, predicate := range memoTestPredicates {
		cold, err := ParsePredicate(predicate)
		if err != nil {
			t.Fatalf("cold parse %q: %v", predicate, err)
		}
		warm, err := ParsePredicate(predicate)
		if err != nil {
			t.Fatalf("warm parse %q: %v", predicate, err)
		}
		if cold != warm {
			t.Fatalf("memoized parts diverge for %q: cold=%+v warm=%+v", predicate, cold, warm)
		}
		if warm.String() != predicate {
			t.Fatalf("memoized parts do not round-trip %q: got %q", predicate, warm.String())
		}
		if _, cached := validParsedPredicates.Load(predicate); !cached {
			t.Fatalf("valid predicate %q missing from memo after successful parse", predicate)
		}
	}
}

func TestParsePredicate_InvalidNeverEntersMemo(t *testing.T) {
	warmPredicateMemo(t)

	invalid := []struct {
		name      string
		predicate string
		reason    PredicateValidationReason
	}{
		{name: "empty", predicate: "", reason: PredicateReasonEmpty},                                                                              // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{name: "arity two", predicate: "memo.test", reason: PredicateReasonArity},                                                                 // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.test","reason":"arity"}
		{name: "arity four", predicate: "memo.test.too.many", reason: PredicateReasonArity},                                                       // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.test.too.many","reason":"arity"}
		{name: "upper segment", predicate: "memo.Test.upper", reason: PredicateReasonSegmentStart},                                                // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.Test.upper","reason":"segment_start"}
		{name: "forbidden char", predicate: "memo.te_st.underscore", reason: PredicateReasonSegmentCharacter},                                     // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.te_st.underscore","reason":"segment_character"}
		{name: "trailing hyphen", predicate: "memo.test.tail-", reason: PredicateReasonSegmentHyphen},                                             // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.test.tail-","reason":"segment_hyphen"}
		{name: "empty segment", predicate: "memo..test", reason: PredicateReasonSegmentEmpty},                                                     // predicate-audit:invalid {"kind":"stored-predicate","value":"memo..test","reason":"segment_empty"}
		{name: "segment length", predicate: "memo.test." + strings.Repeat("a", MaxPredicateSegmentBytes+1), reason: PredicateReasonSegmentLength}, // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.test.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","reason":"segment_length"}
		{name: "total length", predicate: strings.Repeat("a", MaxPredicateBytes) + ".b.c", reason: PredicateReasonLength},                         // predicate-audit:invalid {"kind":"stored-predicate","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.b.c","reason":"length"}
	}

	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			assertInvalid := func(phase string) {
				_, err := ParsePredicate(tt.predicate)
				if err == nil {
					t.Fatalf("%s: invalid predicate %q accepted", phase, tt.predicate)
				}
				validationErr, ok := err.(*PredicateValidationError)
				if !ok {
					t.Fatalf("%s: unexpected error type %T", phase, err)
				}
				if validationErr.Reason != tt.reason {
					t.Fatalf("%s: reason = %q, want %q", phase, validationErr.Reason, tt.reason)
				}
				if _, cached := validParsedPredicates.Load(tt.predicate); cached {
					t.Fatalf("%s: invalid predicate %q entered the memo", phase, tt.predicate)
				}
			}
			assertInvalid("cold")
			assertInvalid("warm") // second call: memo warm from valid fixtures AND the first call
		})
	}
}

func TestParsePredicate_ConcurrentMixedValidInvalid(t *testing.T) {
	valid := memoTestPredicates
	invalid := []string{"memo.test", "memo.Test.upper", "memo.test.tail-", ""}

	const goroutines = 16
	const iterations = 200
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				vp := valid[(seed+i)%len(valid)]
				if parts, err := ParsePredicate(vp); err != nil || parts.String() != vp {
					errCh <- err
					return
				}
				ip := invalid[(seed+i)%len(invalid)]
				if _, err := ParsePredicate(ip); err == nil {
					errCh <- errInvalidAccepted(ip)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent parse violated the contract: %v", err)
	}

	for _, ip := range invalid {
		if _, cached := validParsedPredicates.Load(ip); cached {
			t.Fatalf("invalid predicate %q entered the memo under concurrency", ip)
		}
	}
}

type errInvalidAccepted string

func (e errInvalidAccepted) Error() string { return "invalid predicate accepted: " + string(e) }

// TestParsePredicate_MemoCapStopsInsertionNotValidation pins the defensive
// size cap: at capacity the memo stops ADMITTING new predicates but
// ParsePredicate keeps validating correctly. Restores the counter afterwards.
// Not parallel: it manipulates the package-global admission counter.
func TestParsePredicate_MemoCapStopsInsertionNotValidation(t *testing.T) {
	before := validParsedPredicateCount.Load()
	validParsedPredicateCount.Store(maxMemoizedPredicates)
	defer validParsedPredicateCount.Store(before)

	const fresh = "memo.test.capped-fresh"
	parts, err := ParsePredicate(fresh)
	if err != nil {
		t.Fatalf("valid predicate rejected at memo capacity: %v", err)
	}
	if parts.String() != fresh {
		t.Fatalf("parts corrupted at memo capacity: %+v", parts)
	}
	if _, cached := validParsedPredicates.Load(fresh); cached {
		t.Fatalf("predicate admitted past the memo capacity cap")
	}
	if _, err := ParsePredicate("memo.Bad.at-cap"); err == nil { // predicate-audit:invalid {"kind":"stored-predicate","value":"memo.Bad.at-cap","reason":"segment_start"}
		t.Fatal("invalid predicate accepted at memo capacity")
	}
}

// TestParsePredicate_WarmHitDoesNotAllocate pins the hot-path property the
// memo exists for: a warm valid parse is a single lock-free map load with no
// heap allocation (the cold path allocates via strings.Split).
func TestParsePredicate_WarmHitDoesNotAllocate(t *testing.T) {
	const predicate = "memo.test.alloc-free"
	if _, err := ParsePredicate(predicate); err != nil {
		t.Fatalf("warming parse: %v", err)
	}
	allocs := testing.AllocsPerRun(200, func() {
		if _, err := ParsePredicate(predicate); err != nil {
			t.Fatalf("warm parse: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("warm memoized parse allocated %.1f objects/op, want 0", allocs)
	}
}
