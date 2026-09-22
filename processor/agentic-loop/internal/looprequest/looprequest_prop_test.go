package looprequest

import (
	"testing"

	"pgregory.net/rapid"
)

// The grammar's generator, written from the grammar and not from Parse.
//
// Loop IDs are framework-minted UUIDs today, but the grammar admits a colon
// inside one and the right-hand read is what makes that exact, so the
// generator draws colons deliberately rather than only the shapes production
// happens to mint. The ordinal draws hug their boundaries — iteration at its
// first legal value 1, retry at its first legal value 0 — because a wide
// uniform range catches an off-by-one at a bound only probabilistically
// (measured on PR #1213).
var (
	loopIDGen = rapid.OneOf(
		rapid.StringMatching(`[a-z0-9][a-z0-9-]{0,20}`),
		rapid.StringMatching(`[a-z0-9]{1,6}(:[a-z0-9]{1,6}){1,3}`),
		rapid.Just("loop:req"),
		rapid.Just("loop:req:9:9"),
	)
	iterationGen = rapid.OneOf(rapid.Just(1), rapid.Just(2), rapid.IntRange(1, 1<<20))
	retryGen     = rapid.OneOf(rapid.Just(0), rapid.Just(1), rapid.IntRange(0, 1<<20))
)

func drawID(t *rapid.T, label string) ID {
	return ID{
		LoopID:    loopIDGen.Draw(t, label+" loop id"),
		Iteration: iterationGen.Draw(t, label+" iteration"),
		Retry:     retryGen.Draw(t, label+" retry"),
	}
}

// TestPropParseRoundTripsEveryMintedName: every name the grammar can mint
// parses back into the exact parts it was built from. This is what lets
// recovery compare a retained request's identity against the record's
// published_request_id for equality instead of comparing bodies.
// spec: agentic-loop / The loop record names its outstanding request
func TestPropParseRoundTripsEveryMintedName(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		want := drawID(t, "id")
		got, err := Parse(want.String())
		if err != nil {
			t.Fatalf("Parse(%q) refused a name the grammar mints: %v", want.String(), err)
		}
		if got != want {
			t.Fatalf("Parse(%q) = %+v, want %+v", want.String(), got, want)
		}
	})
}

// TestPropCompareIsATotalOrderConsistentWithNext: the requirement classifies a
// redelivered input as older, current or newer than published_request_id, so
// the order behind those three words must be total (exactly one of <, =, >
// holds for every pair, and it is transitive) and must agree with the direction
// the loop advances — every request Next mints sorts after the one it followed.
// spec: agentic-loop / The loop record names its outstanding request
func TestPropCompareIsATotalOrderConsistentWithNext(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		a := drawID(t, "a")
		b := drawID(t, "b")
		c := drawID(t, "c")

		if Compare(a, b) != -Compare(b, a) {
			t.Fatalf("Compare is not antisymmetric on %+v / %+v", a, b)
		}
		if Compare(a, b) <= 0 && Compare(b, c) <= 0 && Compare(a, c) > 0 {
			t.Fatalf("Compare is not transitive on %+v / %+v / %+v", a, b, c)
		}
		if Compare(a, a) != 0 {
			t.Fatalf("Compare(%+v, itself) is not zero", a)
		}

		retried := Next(a, true)
		if Compare(a, retried) != -1 {
			t.Fatalf("a retry of %+v did not sort after it", a)
		}
		advanced := Next(a, false)
		if Compare(a, advanced) != -1 {
			t.Fatalf("the advance of %+v did not sort after it", a)
		}
		// The advance outranks every retry of the iteration it leaves, which
		// is what makes "adopt the newer retained request" pick the advance
		// over a retry that was minted later in wall-clock time.
		if Compare(retried, advanced) != -1 {
			t.Fatalf("the advance of %+v did not outrank its retry", a)
		}
	})
}

// FuzzParseNeverPanicsAndRoundTrips is the oracle half: Parse is total over
// arbitrary bytes (it answers, it never panics), and whatever it accepts
// renders back to the exact input. The seeds carry both grammar classes —
// accepted names and each rejection class the table names — because a corpus
// of valid inputs only proves the accept path.
func FuzzParseNeverPanicsAndRoundTrips(f *testing.F) {
	for _, seed := range []string{
		"loop:req:1:0",
		"loop:req:12:3",
		"org:tenant:loop:req:2:4",
		"loop:req:req:1:0",
		"",
		":req:1:0",
		"loop:req:",
		"loop:req:0:0",
		"loop:req:007:0",
		"loop:req:1:+2",
		"loop:req:1:-1",
		"loop:req:two:0",
		"loop-1-0",
		"::::",
		"loop:req:9223372036854775808:0",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, requestID string) {
		parsed, err := Parse(requestID)
		if err != nil {
			return
		}
		if parsed.String() != requestID {
			t.Fatalf("Parse accepted %q but it renders as %q", requestID, parsed.String())
		}
		if parsed.LoopID == "" || parsed.Iteration < 1 || parsed.Retry < 0 {
			t.Fatalf("Parse accepted %q into an out-of-grammar value %+v", requestID, parsed)
		}
	})
}
