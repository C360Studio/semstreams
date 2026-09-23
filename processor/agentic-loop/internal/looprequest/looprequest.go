// Package looprequest is the one reader and minter of the agentic loop's
// model-request identity.
//
// The grammar is `<loopID>:req:<iteration>:<retry>` (owner ruling Q4 on #1330,
// shipped by #1328). Before this package the grammar had two readers, both of
// which recovered only the loop-ID prefix by splitting on the FIRST `:req:`
// (LoopManager.ExtractLoopIDFromRequest and loopIDFromStructuredID), and no
// reader at all for the two ordinals — so the within-iteration retry ordinal
// lived in a process-local counter and did not survive a process replacement.
// Restart safety needs the ordinals back out of the name, which is what makes
// one reader worth having.
//
// Parsing runs from the RIGHT. A loop ID is a framework-minted UUID today, but
// the grammar does not forbid a colon inside it, and a left-to-right split
// would silently mis-read the first such ID rather than refuse it. The two
// ordinals and the literal `:req:` separator are always the last three fields,
// so the right-hand read is exact for every loop ID the grammar admits.
package looprequest

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// separator is the literal that joins a loop ID to its request ordinals.
	separator = ":req:"
	// loopSuffix is what remains of the separator once the last colon has been
	// consumed as the iteration's own delimiter by the right-hand read.
	loopSuffix = ":req"
)

// ID is a parsed model-request identity.
//
// The three parts travel together and are meaningless apart — an iteration
// without its loop names no request — so they are one value rather than three
// positional returns.
type ID struct {
	// LoopID owns the request. Never empty in a parsed ID.
	LoopID string
	// Iteration is the 1-based ordinal of the request within the loop:
	// LoopEntity.Iterations at mint time plus one.
	Iteration int
	// Retry is the 0-based within-iteration truncation-retry ordinal. A
	// compaction retry of iteration N is `:N:1`.
	Retry int
}

// String renders the canonical wire form of the identity.
func (id ID) String() string {
	return fmt.Sprintf("%s%s%d:%d", id.LoopID, separator, id.Iteration, id.Retry)
}

// Parse reads a minted request ID back into its parts.
//
// Every part is required and every ordinal must be in the canonical form
// String writes: a non-negative decimal with no sign, no padding and no
// surrounding space. A value that merely looks numeric to strconv but renders
// differently (`007`, `+1`) is refused rather than accepted into a different
// name than it arrived as, because the ID is an identity — recovery compares
// it for equality and orders it.
func Parse(requestID string) (ID, error) {
	head, retryText, ok := cutLast(requestID)
	if !ok {
		return ID{}, fmt.Errorf("request id %q: want <loopID>%s<iteration>:<retry>", requestID, separator)
	}
	loopPart, iterationText, ok := cutLast(head)
	if !ok {
		return ID{}, fmt.Errorf("request id %q: want <loopID>%s<iteration>:<retry>", requestID, separator)
	}
	loopID, ok := strings.CutSuffix(loopPart, loopSuffix)
	if !ok || loopID == "" {
		return ID{}, fmt.Errorf("request id %q: missing the %q separator or its loop id", requestID, separator)
	}
	iteration, err := ordinal(iterationText)
	if err != nil {
		return ID{}, fmt.Errorf("request id %q iteration: %w", requestID, err)
	}
	if iteration < 1 {
		return ID{}, fmt.Errorf("request id %q iteration: %d is below the first iteration", requestID, iteration)
	}
	retry, err := ordinal(retryText)
	if err != nil {
		return ID{}, fmt.Errorf("request id %q retry: %w", requestID, err)
	}
	return ID{LoopID: loopID, Iteration: iteration, Retry: retry}, nil
}

// Next names the request that follows prev.
//
// A retry stays in the same iteration and takes the next retry ordinal; any
// other advance takes the next iteration and resets the retry ordinal to zero.
// This reproduces what the loop already does with its own counters
// (LoopManager.GenerateRequestID: `iteration = entity.Iterations + 1`), with
// the difference that both ordinals now come from a durable name instead of a
// process-local map.
func Next(prev ID, retry bool) ID {
	if retry {
		return ID{LoopID: prev.LoopID, Iteration: prev.Iteration, Retry: prev.Retry + 1}
	}
	return ID{LoopID: prev.LoopID, Iteration: prev.Iteration + 1, Retry: 0}
}

// Compare orders two request identities of the SAME loop by (iteration, retry),
// returning -1, 0 or +1. It is a total order on parsed IDs and is consistent
// with Next: every ID Next produces sorts after the one it was derived from.
//
// The loop ID takes no part in the order. Two loops' request names are not
// comparable, so the caller establishes loop identity first — recovery reads
// one loop's record and one loop's retained request, and an ID naming a
// different loop is a conflict there, not an older or newer request.
func Compare(a, b ID) int {
	switch {
	case a.Iteration < b.Iteration:
		return -1
	case a.Iteration > b.Iteration:
		return 1
	case a.Retry < b.Retry:
		return -1
	case a.Retry > b.Retry:
		return 1
	default:
		return 0
	}
}

// cutLast splits text at its last colon.
func cutLast(text string) (head, tail string, ok bool) {
	index := strings.LastIndexByte(text, ':')
	if index < 0 {
		return "", "", false
	}
	return text[:index], text[index+1:], true
}

// ordinal accepts only the canonical rendering of a non-negative integer.
func ordinal(text string) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%q is not a decimal ordinal", text)
	}
	if strconv.Itoa(value) != text {
		return 0, fmt.Errorf("%q is not the canonical rendering of %d", text, value)
	}
	if value < 0 {
		return 0, fmt.Errorf("%q is negative", text)
	}
	return value, nil
}
