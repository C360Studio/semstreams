package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/nats-io/nats.go/jetstream"
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
	// and unfinished; this process simply is not the one holding it. Retry:
	// the delivery is still owed to somebody. L4 (#1330) is what makes that
	// somebody actually able to take it; until then Retry is bounded by the
	// lane's MaxDeliver and BackOff rather than being a hot loop.
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
// and reads no retained request or tool-result message. Making the
// loopPresenceLive case actually recoverable is L4's (#1330) subject; this
// only stops the lost case from being acknowledged as if it were stale.
func (c *Component) classifyMissingLoop(ctx context.Context, loopID string) loopPresence {
	if loopID == "" || !looptoken.Valid(loopID) {
		// Nothing to look up. An input that carries no framework-minted loop
		// token names no loop any process could be holding.
		return loopPresenceStale
	}
	if c.loopsBucket == nil {
		return loopPresenceUnknown
	}
	entry, err := c.loopsBucket.Get(ctx, loopID)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound), errors.Is(err, jetstream.ErrKeyDeleted):
		return loopPresenceStale
	case err != nil:
		return loopPresenceUnknown
	}

	var entity agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &entity); err != nil {
		// A record that exists but will not decode is not evidence of
		// staleness. Report unknown and let the bounded retry surface it.
		c.logger.Error("Loop record did not decode while classifying a missing loop",
			"loop_id", loopID, "error", err)
		return loopPresenceUnknown
	}
	if entity.State.IsTerminal() {
		return loopPresenceStale
	}
	return loopPresenceLive
}
