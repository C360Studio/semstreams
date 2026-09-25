package agenticloop

import (
	"context"
	"strings"

	"github.com/c360studio/semstreams/internal/looptoken"
)

// loopPresence answers one question, asked at six call sites in different
// words before this existed: an input names a loop this process does not hold
// in memory — is the input STALE, or did this process LOSE the loop?
//
// The two cases settle in opposite directions and were previously
// indistinguishable, which is why an expected settled-drop and a
// process-replacement casualty both took the same branch. A stale input can
// never be applied by anyone, so redelivery is pure cost and it is
// acknowledged. A live loop means some process can still apply this input, so
// acknowledging it destroys work that was never done.
//
// Memory is not the authority here: memory is exactly what is known to be
// missing at every one of these call sites. The loops bucket is.
type loopPresence int

const (
	// loopPresenceStale — no record, or a record in a terminal state. The
	// input cannot be applied by this or any other process. Ack.
	loopPresenceStale loopPresence = iota
	// loopPresenceLive — a non-terminal record exists, so the loop is real
	// and unfinished; this process simply is not the one holding it. A live
	// presence alone is never grounds to acknowledge: a lane may still
	// acknowledge or terminate the delivery after classifying it against the
	// record (an older or already-applied request, a mismatched answer, a
	// foreign verdict). A lane with a cold branch takes it here: the model-response, tool-result and approval
	// lanes rebuild the loop from its record and retained evidence, and a
	// cancel adopts a durable cancel. Whatever a lane cannot yet take is
	// retried, bounded by the lane's MaxDeliver and BackOff rather than being
	// a hot loop.
	loopPresenceLive
	// loopPresenceUnknown — the record could not be read. Never assume
	// stale from a failed read: that is the fail-open shape this whole
	// change exists to remove. Retry.
	loopPresenceUnknown
)

// loopIDFromStructuredID recovers the loop ID a structured request or
// tool-call ID carries. Both grammars are "<loopID>:<kind>:<n>", so a bare
// string with no separator yields itself — hence the token check: a value
// that is not a framework-minted loop token is not a loop ID, and treating it
// as one would key the bucket read on garbage.
func loopIDFromStructuredID(structuredID, separator string) string {
	candidate := structuredID
	if index := strings.Index(structuredID, separator); index >= 0 {
		candidate = structuredID[:index]
	}
	if !looptoken.Valid(candidate) {
		return ""
	}
	return candidate
}

// classifyMissingLoop reads one loop record and classifies it. It performs no
// recovery: it reconstructs no in-memory state, re-registers no routing entry,
// and reads no retained request or tool-result message.
//
// It keeps its signature and delegates to readLoopRecord, which is the same
// read plus the revision this one used to discard (#1330). Callers that must
// WRITE the record — identity adoption, the carrier — need the revision;
// callers that only need to decide stale-versus-live keep asking this.
func (c *Component) classifyMissingLoop(ctx context.Context, loopID string) loopPresence {
	return c.readLoopRecord(ctx, loopID).presence
}
