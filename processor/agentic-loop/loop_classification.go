package agenticloop

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
)

// requestOrder orders one redelivered input against the request its loop
// record names (LoopEntity.PublishedRequestID, invariant I1 of #1330).
//
// It is the whole of the recovery decision this change is built on: an input
// is applied, current, or ahead of the record, and that is decided by ORDERING
// two names — never by comparing rendered conversation content, tool output or
// terminal payloads. One function so the four sites that ask (warm and cold, on
// the model-response and tool-result lanes) cannot drift into four readings of
// the same grammar.
type requestOrder int

const (
	// requestOrderUnnamed — one side carries no request name at all, so there
	// is nothing to order. A record written before this field existed and a
	// result from a producer that does not correlate both read this way, and
	// both take the pre-#1330 path: the input is handled, and the handler's
	// own guards decide it. Absence of evidence is not evidence of staleness,
	// and refusing an input this change cannot classify would strand loops
	// that were working before it landed.
	requestOrderUnnamed requestOrder = iota
	// requestOrderApplied — the input names an EARLIER request than the record
	// does. The loop advanced past it, which it can only do by applying it, so
	// the work is done: acknowledge without effect.
	requestOrderApplied
	// requestOrderCurrent — the input names the request the record names.
	// This is the ordinary delivery and every redelivery of it.
	requestOrderCurrent
	// requestOrderAhead — the input names a LATER request than the record
	// does: the publish landed and the record update that names it has not.
	// Not yet observable — retry until the record catches up, rather than
	// applying an input against a record that cannot yet account for it.
	requestOrderAhead
	// requestOrderForeign — the input names something that is not a request of
	// this loop. Nothing orders it, no later delivery will make it order, and
	// applying it would act on another loop's (or no loop's) identity.
	requestOrderForeign
)

// orderAgainstPublished orders incoming against published for one loop.
//
// Equality is decided on the raw strings first, deliberately: an identity is
// what it arrived as, and two identical names are the same request whether or
// not this package can parse them. Only an inequality has to be ordered, and
// only then does the grammar matter.
func orderAgainstPublished(loopID, published, incoming string) requestOrder {
	if published == "" || incoming == "" {
		return requestOrderUnnamed
	}
	if published == incoming {
		return requestOrderCurrent
	}
	incomingID, err := looprequest.Parse(incoming)
	if err != nil || incomingID.LoopID != loopID {
		return requestOrderForeign
	}
	publishedID, err := looprequest.Parse(published)
	if err != nil || publishedID.LoopID != loopID {
		// The RECORD is the unreadable side. Refusing the input would punish
		// the delivery for the record's state; the cold adopt (§ 3.6) is what
		// quarantines a record naming a request that is not this loop's.
		return requestOrderUnnamed
	}
	switch looprequest.Compare(incomingID, publishedID) {
	case -1:
		return requestOrderApplied
	case 0:
		return requestOrderCurrent
	default:
		return requestOrderAhead
	}
}

// taskDisposition is what a task delivery means for a loop whose record may
// already exist — the task lane's half of the redelivery question (#1330,
// design § 5.1, owner ruling Q1).
type taskDisposition int

const (
	// taskBirth — no record names this task's loop, so this delivery is the
	// birth: the ordinary path, unchanged.
	taskBirth taskDisposition = iota
	// taskRepublishFirstRequest — a record exists, still names the loop's FIRST
	// request and has done nothing since. The birth that wrote it did not get
	// that request onto the stream, or did and died before acknowledging. R1 is
	// rebuilt from the task and republished under its own identity; the record
	// is NOT written again.
	taskRepublishFirstRequest
	// taskApplied — the loop moved past this task: it advanced beyond its first
	// iteration, retried that iteration under a later ordinal, applied part of
	// its first batch, or settled. Acknowledge without effect.
	taskApplied
)

// classifyRedeliveredTask decides what a task delivery means for a loop this
// process has no memory of.
//
// Warm is not its business: a process holding the loop answers a redelivered
// task through HandleTask's own dedup, which is untouched. This is the COLD
// fork — the case where memory says "new loop" and the record says otherwise.
// Without it a replacement process would try to birth a loop that already
// exists, be refused by the record's Create, and retry to MaxDeliver while the
// loop it was supposed to resume sat waiting for a request nobody republished.
//
// It returns the record it read as well, because the caller that goes on to
// HOLD this loop must compare-and-swap against the revision this read
// observed — there is no other write on the republish path that could seed it.
func (c *Component) classifyRedeliveredTask(
	ctx context.Context, loopID string,
) (taskDisposition, loopRecord, error) {
	if loopID == "" {
		return taskBirth, loopRecord{}, nil
	}
	if c.loopsBucket == nil {
		// No record store is configured, so there is no record that could say
		// this task was already applied — the same answer createLoopState
		// gives a birth with no bucket, and the pre-#1330 behaviour.
		return taskBirth, loopRecord{}, nil
	}
	if _, err := c.handler.GetLoop(loopID); err == nil {
		return taskBirth, loopRecord{}, nil
	}
	record := c.readLoopRecord(ctx, loopID)
	switch record.presence {
	case loopPresenceUnknown:
		// The record could not be read. Never birth on a failed read: that is
		// the fail-open shape — a second loop under a name that may already
		// have one, with its own conversation and its own requests.
		return taskBirth, record, errs.WrapTransient(
			fmt.Errorf("loop %s: the loop record could not be read, so this task cannot be classified", loopID),
			"agentic-loop", "handleTaskMessage", "classify the task against the loop record")
	case loopPresenceStale:
		if record.entity.State.IsTerminal() {
			// The loop ran and settled. Re-birthing it would overwrite a
			// finished conversation; the task is done (owner ruling Q7).
			return taskApplied, record, nil
		}
		return taskBirth, record, nil
	}
	// Every fact the delta's GIVEN names, because no one of them is the
	// untouched birth it looks like on its own.
	//
	// The applied set: a loop advances its iteration only when a whole tool
	// batch is in, so the entire FIRST batch runs at zero while its applied set
	// fills. Republishing over that seats a fresh loop with no batch on top of
	// a record that carries one, and the sibling result then has no execution
	// to route to.
	//
	// The NAME: a length-truncated first response self-heals by re-asking the
	// same iteration under the next retry ordinal, which publishes :req:1:1 and
	// deliberately leaves both of the other two facts untouched. Rebuilding
	// from the task there mints :req:1:0 under a NEWER retained request, and
	// the cold adopt refuses that backward name as Fatal — a routine
	// at-least-once redelivery quarantining the task lane. So the arm runs only
	// for a record naming the loop's FIRST request, which is what the delta's
	// GIVEN has always said (published_request_id = R1).
	//
	// Everything else is a loop that moved past its task: its batch is rebuilt
	// by the next tool result and its outstanding request is answered by its
	// own response, each on the lane that owns it. A record naming NO request
	// falls here too — it is not the R1 birth this arm rebuilds, and I1 says a
	// live record of this build always names one.
	firstRequest := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	if record.entity.PublishedRequestID == firstRequest &&
		record.entity.Iterations == 0 && len(record.entity.PendingToolResults) == 0 {
		return taskRepublishFirstRequest, record, nil
	}
	return taskApplied, record, nil
}

// classifyRedeliveredToolResult decides, at component entry, whether a tool
// result may still be applied to the loop this process holds.
//
// It runs BEFORE HandleToolResult because HandleToolResult mutates first and
// guards later: StoreToolResult (handlers.go) writes the result into the
// applied set and a StopLoop result completes the loop, both ahead of the
// lane's only terminal guard. A result the loop has already moved past would
// therefore land in PendingToolResults and be re-sent to the model as a
// duplicate tool message in the next turn's request — the redelivery damage
// this classification exists to prevent (#1330, design § 5.3).
//
// It reports whether the caller should go on to apply the result. False with a
// nil error is an effect-free acknowledgement: the delivery is settled, the
// drop is counted, and an audit line names what was dropped and why.
//
// The identity it classifies against is the loop's own entity, which is the
// record's content: the entity is what the carrier marshals into AGENT_LOOPS,
// and this process holds this loop. The cold arms read the record itself,
// because there the entity is exactly what is missing.
func (c *Component) classifyRedeliveredToolResult(
	ctx context.Context, loopID string, entity agentic.LoopEntity, toolResult agentic.ToolResult,
) (bool, error) {
	if entity.State.IsTerminal() {
		// Owner ruling Q7 (#1330): a terminal loop cannot apply anything, and
		// whether this particular result was applied before the loop settled
		// is not re-derived — it would change nothing. Effect-free ACK.
		c.logger.WarnContext(ctx, "Tool result acknowledged without effect — the loop is already terminal",
			slog.String("loop_id", loopID),
			slog.String("execution_id", toolResult.ExecutionID),
			slog.String("call_id", toolResult.CallID),
			slog.String("state", entity.State.String()))
		if c.metrics != nil {
			c.metrics.recordToolResultDropped("terminal_unproven")
		}
		return false, nil
	}

	switch orderAgainstPublished(loopID, entity.PublishedRequestID, toolResult.RequestID) {
	case requestOrderApplied:
		// The loop cannot advance past a request until every tool result of
		// its batch is in, so a result naming an earlier request is one this
		// loop already applied.
		c.logger.WarnContext(ctx, "Tool result acknowledged without effect — its request is older than the loop's",
			slog.String("loop_id", loopID),
			slog.String("execution_id", toolResult.ExecutionID),
			slog.String("result_request_id", toolResult.RequestID),
			slog.String("published_request_id", entity.PublishedRequestID))
		if c.metrics != nil {
			c.metrics.recordToolResultDropped("older_request")
		}
		return false, nil
	case requestOrderAhead:
		return false, errs.WrapTransient(
			fmt.Errorf("loop %s: tool result names request %q, which its record does not yet name (%q)",
				loopID, toolResult.RequestID, entity.PublishedRequestID),
			"agentic-loop", "handleToolResultMessage", "classify the tool result against the loop record")
	case requestOrderForeign:
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s: tool result names request %q, which is not a request of this loop",
				loopID, toolResult.RequestID),
			"agentic-loop", "handleToolResultMessage", "classify the tool result against the loop record")
	default:
		return true, nil
	}
}
