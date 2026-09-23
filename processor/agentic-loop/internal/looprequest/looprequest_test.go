package looprequest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseAcceptsTheGrammarItsCallersMeet walks the shapes the loop actually
// mints and the shapes recovery must refuse. The refusals are the half a
// round-trip property cannot see: nothing outside the grammar is ever
// generated, so only a table asserts that anything is rejected at all.
func TestParseAcceptsTheGrammarItsCallersMeet(t *testing.T) {
	t.Parallel()

	accepted := map[string]ID{
		"birth":                     {LoopID: "4f0f0f0f-0f0f-4f0f-8f0f-0f0f0f0f0f0f", Iteration: 1, Retry: 0},
		"advanced iteration":        {LoopID: "loop", Iteration: 12, Retry: 0},
		"within-iteration retry":    {LoopID: "loop", Iteration: 3, Retry: 1},
		"loop id carrying a colon":  {LoopID: "org:tenant:loop", Iteration: 2, Retry: 4},
		"loop id ending in :req":    {LoopID: "loop:req", Iteration: 2, Retry: 0},
		"loop id carrying ordinals": {LoopID: "loop:req:9:9", Iteration: 5, Retry: 6},
	}
	for name, want := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(want.String())
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}

	refused := map[string]string{
		"no separator at all":        "loop-1-0",
		"prefix-only, the old read":  "loop:req:",
		"missing retry part":         "loop:req:3",
		"empty loop id":              ":req:1:0",
		"wrong separator word":       "loop:rq:1:0",
		"iteration below the first":  "loop:req:0:0",
		"negative retry":             "loop:req:1:-1",
		"padded iteration":           "loop:req:007:0",
		"signed retry":               "loop:req:1:+2",
		"non-numeric iteration":      "loop:req:two:0",
		"space inside an ordinal":    "loop:req:1: 0",
		"empty string":               "",
		"separator with no ordinals": "loop:req:",
	}
	for name, input := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(input)
			require.Error(t, err, "input %q must be refused", input)
		})
	}
}

// TestNextReproducesTheMintTheLoopAlreadyPerforms pins Next against the two
// advances the handler path makes: a normal iteration advance and the
// single within-iteration compaction retry.
func TestNextReproducesTheMintTheLoopAlreadyPerforms(t *testing.T) {
	t.Parallel()

	birth := ID{LoopID: "loop", Iteration: 1, Retry: 0}
	require.Equal(t, "loop:req:1:0", birth.String())
	require.Equal(t, "loop:req:1:1", Next(birth, true).String())
	require.Equal(t, "loop:req:2:0", Next(birth, false).String())
	// A retry is not a new iteration, so the advance after it takes the
	// iteration the retry was still serving plus one.
	require.Equal(t, "loop:req:2:0", Next(Next(birth, true), false).String())
}

// TestCompareOrdersWithinALoopAndIgnoresTheLoopID states both halves of
// Compare's contract: the ordinals decide, and the loop ID does not.
func TestCompareOrdersWithinALoopAndIgnoresTheLoopID(t *testing.T) {
	t.Parallel()

	first := ID{LoopID: "a", Iteration: 1, Retry: 0}
	retry := ID{LoopID: "a", Iteration: 1, Retry: 1}
	second := ID{LoopID: "a", Iteration: 2, Retry: 0}

	require.Equal(t, -1, Compare(first, retry))
	require.Equal(t, -1, Compare(retry, second))
	require.Equal(t, 1, Compare(second, first))
	require.Equal(t, 0, Compare(first, ID{LoopID: "b", Iteration: 1, Retry: 0}))
}
