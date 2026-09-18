package agenticloop

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The recovery grammars are "<loopID>:req:<n>" and "<loopID>:tool:<n>", and a
// value with no separator yields itself under a naive split. Without the token
// check a free-form provider call ID would be handed to the loops bucket as if
// it were a loop key, and its "not found" would read as proof of staleness.
func TestLoopIDFromStructuredIDAcceptsOnlyMintedTokens(t *testing.T) {
	t.Parallel()

	const token = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

	for name, testCase := range map[string]struct {
		id        string
		separator string
		want      string
	}{
		"structured request":         {id: token + ":req:2", separator: ":req:", want: token},
		"structured tool call":       {id: token + ":tool:7", separator: ":tool:", want: token},
		"bare token":                 {id: token, separator: ":req:", want: token},
		"provider call id":           {id: "call_abc123", separator: ":tool:", want: ""},
		"provider id with grammar":   {id: "call_abc123:tool:1", separator: ":tool:", want: ""},
		"uppercase is not canonical": {id: "3F2504E0-4F89-41D3-9A0C-0305E82C3301:req:1", separator: ":req:", want: ""},
		"braced is not canonical":    {id: "{" + token + "}:req:1", separator: ":req:", want: ""},
		"empty":                      {id: "", separator: ":req:", want: ""},
		"separator only":             {id: ":req:1", separator: ":req:", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, loopIDFromStructuredID(testCase.id, testCase.separator))
		})
	}
}

// Without a bucket there is no authority to ask, and "I could not look" must
// never render as "it is stale" — that is the fail-open direction.
func TestClassifyMissingLoopWithoutBucketIsUnknownNotStale(t *testing.T) {
	t.Parallel()

	c := &Component{logger: discardLogger()}
	require.Equal(t, loopPresenceUnknown,
		c.classifyMissingLoop(t.Context(), "3f2504e0-4f89-41d3-9a0c-0305e82c3301"))
}

// An input carrying no minted token names no loop any process could hold, so
// there is nothing to lose by acknowledging it.
func TestClassifyMissingLoopWithoutTokenIsStale(t *testing.T) {
	t.Parallel()

	c := &Component{logger: discardLogger()}
	require.Equal(t, loopPresenceStale, c.classifyMissingLoop(t.Context(), ""))
	require.Equal(t, loopPresenceStale, c.classifyMissingLoop(t.Context(), "not-a-loop-token"))
}
