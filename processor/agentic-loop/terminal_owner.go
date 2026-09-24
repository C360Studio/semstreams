package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// terminalOutcome is one loop's terminal as its COMPLETE_<loopID> marker
// carries it: exactly one of the three terminal events, or none for a terminal
// result that could not build its event.
type terminalOutcome struct {
	completed *agentic.LoopCompletedEvent
	failed    *agentic.LoopFailedEvent
	cancelled *agentic.LoopCancelledEvent
	// syntheticDecide rides a completion onto the graph (#133). It is a graph
	// stamp, not part of the marker.
	syntheticDecide *SyntheticDecideRequest
}

// terminalOutcomeOf reads the terminal a carrier result produced.
func terminalOutcomeOf(result HandlerResult) terminalOutcome {
	return terminalOutcome{
		completed:       result.CompletionState,
		failed:          result.FailureState,
		syntheticDecide: result.SyntheticDecide,
	}
}

// kind is the terminal kind the marker names, spelled as its `outcome` field.
func (o terminalOutcome) kind() string {
	switch {
	case o.completed != nil:
		return agentic.OutcomeSuccess
	case o.failed != nil:
		return agentic.OutcomeFailed
	case o.cancelled != nil:
		return agentic.OutcomeCancelled
	default:
		return ""
	}
}

// event is the payload the marker stores and the terminal publication carries.
func (o terminalOutcome) event() message.Payload {
	switch {
	case o.completed != nil:
		return o.completed
	case o.failed != nil:
		return o.failed
	case o.cancelled != nil:
		return o.cancelled
	default:
		return nil
	}
}

func (o terminalOutcome) loopID() string {
	switch {
	case o.completed != nil:
		return o.completed.LoopID
	case o.failed != nil:
		return o.failed.LoopID
	case o.cancelled != nil:
		return o.cancelled.LoopID
	default:
		return ""
	}
}

// terminalMarkerKey is the loop's durable terminal: one key per loop, the same
// for every kind, so watchers need not tell success from failure by key.
func terminalMarkerKey(loopID string) string {
	return "COMPLETE_" + loopID
}

// decodeTerminalMarker reads a saved marker back into the event its `outcome`
// names. An unknown or empty outcome is refused rather than defaulted.
func decodeTerminalMarker(data []byte) (terminalOutcome, error) {
	var head struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return terminalOutcome{}, fmt.Errorf("decode terminal marker: %w", err)
	}
	var saved terminalOutcome
	var target any
	switch head.Outcome {
	case agentic.OutcomeSuccess:
		saved.completed = &agentic.LoopCompletedEvent{}
		target = saved.completed
	case agentic.OutcomeFailed:
		saved.failed = &agentic.LoopFailedEvent{}
		target = saved.failed
	case agentic.OutcomeCancelled:
		saved.cancelled = &agentic.LoopCancelledEvent{}
		target = saved.cancelled
	default:
		return terminalOutcome{}, fmt.Errorf("terminal marker names outcome %q, which is no terminal kind", head.Outcome)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return terminalOutcome{}, fmt.Errorf("decode %s terminal marker: %w", head.Outcome, err)
	}
	return saved, nil
}

// commitTerminal is the one owner of a loop's terminal (#1362, design § 5.7,
// D41/P6; owner ruling 2026-09-18 on #1330). The carrier, the loop-failure
// path and cancel commit through it. The approval-timeout sweeper is the one exception until #1362 task 1.5: it
// publishes and writes a handler result through its own pair
// (approval_sweeper.go), so a terminal its auto-reject produces — a
// max_iterations failure — is published and written with no COMPLETE_ marker.
// The order:
//
//  1. COMPLETE_<loopID> by Create. A refused Create means the loop already has
//     a durable terminal: it is read back and ADOPTED by loop ID and terminal
//     kind, and the saved payload replaces this delivery's candidate for every
//     step below. A content difference is logged at the audit line and never
//     decides the disposition.
//  2. The graph stamps.
//  3. The terminal publication. A republished terminal is an accepted
//     duplicate.
//  4. The loop record, by compare-and-swap Update. The terminal transition
//     clears any pending approval gate on the way.
//
// The marker precedes the event so a watcher that reacts to the event and
// reads COMPLETE_<loopID> finds it, and the record is last so a crash anywhere
// before it leaves a record that is not terminal — which the redelivery
// settles through step 1's adoption — rather than a terminal record whose
// event was never published. A crash after step 4 and before the ACK is
// settled by the lanes' own terminal classification (Q7), outside this owner.
//
// publication carries the lane's terminal messages and any context events; on
// adoption its messages are replaced by the saved terminal's own publication.
//
// A lost compare-and-swap is returned as persistLoopState returned it —
// transient, with the loop already released — so the redelivery re-reads the
// record. Every other failure leaves the terminal commit unknown and is fatal.
func (c *Component) commitTerminal(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {
	err := c.commitTerminalSteps(ctx, candidate, publication)
	if err != nil && !errors.Is(err, natsclient.ErrKVRevisionMismatch) {
		// Memory never holds a terminal the owner did not commit (#1362
		// review, H1). The handler moved this loop terminal before the owner
		// ran; left in memory after a refused or failed commit, that terminal
		// answered every later lane — a redelivered cancel was acknowledged as
		// "already terminal" and never reached its own adoption. Released, the
		// redelivery re-reads the record, which the commit did not make
		// terminal. A lost compare-and-swap has already released the loop.
		c.releaseLoopTransientState(publication.LoopID)
	}
	return err
}

func (c *Component) commitTerminalSteps(ctx context.Context, candidate terminalOutcome, publication HandlerResult) error {
	loopID := publication.LoopID
	outcome, adopted, err := c.createTerminalMarker(ctx, loopID, candidate)
	if err != nil {
		return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "create the loop's durable terminal")
	}
	if adopted {
		messages, err := c.terminalPublication(loopID, outcome)
		if err != nil {
			return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "build the adopted terminal's publication")
		}
		publication.PublishedMessages = messages
	}
	if err := c.stampTerminal(ctx, loopID, outcome); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "stamp the terminal on the graph")
	}
	if err := c.publishResults(ctx, publication); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "published terminal has unknown durability")
	}
	var match *terminalOutcome
	if adopted {
		match = &outcome
	}
	c.handler.loopManager.settleTerminal(loopID, match)
	if err := c.persistLoopState(ctx, loopID); err != nil {
		// The sentinel, never errs.IsTransient: that one matches any error
		// whose text says "timeout", which would hand a commit-unknown write a
		// Retry it has not earned.
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return err
		}
		return errs.WrapFatal(err, "agentic-loop", "commitTerminal", "terminal loop record has unknown durability")
	}
	return nil
}

// createTerminalMarker is step 1 of the terminal owner. It reports the terminal
// every later step commits — the candidate, or the saved terminal it adopted —
// and whether it adopted.
//
// Adoption is by identity: the saved marker's loop ID and terminal kind must
// be this loop's and the candidate's. A marker that decodes to neither is
// conflicting evidence, and refusing it is the fail-closed answer; adopting a
// terminal of another kind would publish one outcome for a delivery that
// derived another.
func (c *Component) createTerminalMarker(
	ctx context.Context, loopID string, candidate terminalOutcome,
) (terminalOutcome, bool, error) {
	event := candidate.event()
	if c.loopsBucket == nil || event == nil {
		return candidate, false, nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return terminalOutcome{}, false, fmt.Errorf("marshal terminal marker for loop %s: %w", loopID, err)
	}
	key := terminalMarkerKey(loopID)
	_, err = c.loopsBucket.Create(ctx, key, data)
	if err == nil {
		return candidate, false, nil
	}
	if !natsclient.IsKVConflictError(err) {
		return terminalOutcome{}, false, fmt.Errorf("create terminal marker for loop %s: %w", loopID, err)
	}

	entry, err := c.loopsBucket.Get(ctx, key)
	if err != nil {
		return terminalOutcome{}, false, fmt.Errorf("read back terminal marker for loop %s: %w", loopID, err)
	}
	saved, err := decodeTerminalMarker(entry.Value())
	if err != nil {
		return terminalOutcome{}, false, fmt.Errorf("loop %s: %w", loopID, err)
	}
	if saved.loopID() != loopID || saved.kind() != candidate.kind() {
		c.logger.WarnContext(ctx, "Terminal refused — the loop's durable terminal is not this terminal's identity",
			slog.String("loop_id", loopID),
			slog.String("marker_loop_id", saved.loopID()),
			slog.String("candidate_kind", candidate.kind()),
			slog.String("marker_kind", saved.kind()))
		return terminalOutcome{}, false, fmt.Errorf(
			"loop %s: durable terminal is (%q, %s), this delivery derived (%q, %s)",
			loopID, saved.loopID(), saved.kind(), loopID, candidate.kind())
	}
	if saved.completed != nil && candidate.syntheticDecide != nil {
		// The synthesized decision's reason is the completion's result; the
		// saved payload replaces the candidate, so it replaces that too.
		saved.syntheticDecide = &SyntheticDecideRequest{LoopID: loopID, Reason: saved.completed.Result}
	}
	c.logger.WarnContext(ctx, "Terminal adopted the loop's durable terminal",
		slog.String("loop_id", loopID),
		slog.String("kind", saved.kind()),
		slog.Uint64("marker_revision", entry.Revision()),
		slog.Bool("content_differs", !bytes.Equal(data, entry.Value())))
	return saved, true, nil
}

// terminalPublication builds the event an adopted terminal republishes, on
// the subject its kind publishes on.
func (c *Component) terminalPublication(loopID string, outcome terminalOutcome) ([]PublishedMessage, error) {
	port := "agent.complete"
	if outcome.failed != nil {
		port = "agent.failed"
	}
	event := outcome.event()
	data, err := json.Marshal(message.NewBaseMessage(event.Schema(), event, "agentic-loop"))
	if err != nil {
		return nil, fmt.Errorf("marshal terminal event for loop %s: %w", loopID, err)
	}
	subject, err := component.ResolveSubject(c.config.Ports.Outputs, port, loopID)
	if err != nil {
		return nil, fmt.Errorf("resolve terminal subject for loop %s: %w", loopID, err)
	}
	return []PublishedMessage{{Subject: subject, Data: data}}, nil
}

// stampTerminal is step 2 of the terminal owner: the terminal's graph triples,
// ahead of the publication so a subscriber that reacts to the event can walk
// the loop entity's triples without racing the writer.
func (c *Component) stampTerminal(ctx context.Context, loopID string, outcome terminalOutcome) error {
	switch {
	case outcome.completed != nil:
		if err := c.stampLoopCompletionWithBudget(ctx, loopID, outcome.completed); err != nil {
			return err
		}
	case outcome.failed != nil:
		if err := c.stampLoopFailureWithBudget(ctx, loopID, outcome.failed); err != nil {
			return err
		}
	case outcome.cancelled != nil:
		// A cancelled loop can have lost evidence too — the terminal
		// observation runs before this write.
		if c.graphWriter != nil {
			c.graphWriter.WriteLoopCancellation(ctx, outcome.cancelled, c.trajectoryAuditLoss.observed(loopID))
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("cancellation graph stamp for loop %s: %w", loopID, err)
			}
		}
	}
	// Terminal-tool-less synthesis (#133): on the graph path so the triples
	// ride the same publish budget as the loop completion stamp and
	// downstream rules see them before the agent.complete.* event.
	if outcome.syntheticDecide != nil {
		if err := c.stampSyntheticDecideWithBudget(ctx, outcome.syntheticDecide); err != nil {
			return err
		}
	}
	return nil
}

// adoptDurableCancel is the cancel lane's cold arm (b) (owner ruling
// 2026-09-23, #1362 issuecomment-5802753726). The terminal owner writes
// COMPLETE_<loopID> before the cancellation event and the record last, so a
// crash between them leaves a cancel marker, a published cancellation and a
// live record. A replacement holds no loop for the redelivered cancel to act
// on, and the lanes that DO rebuild the loop would carry on past a published
// cancel. So when this process does not hold the loop and the record is live,
// the cancel lane reads the marker: a cancel marker is adopted — its saved
// terminal is stamped and republished and the record is written cancelled
// under compare-and-swap — and the delivery ACKs.
//
// It reports whether it adopted. No marker, or a marker of another kind, is
// not this arm's: the caller keeps its existing answer, Retry. A completion or
// failure marker over a live record is the carrier's own crash window, which
// that terminal's redelivery closes; quarantining the cancel there would latch
// the lane on a benign race (owner ruling 1, #1362 issuecomment-5808903072).
// A marker naming
// another loop is poison and fatal. Scoped to the cancel lane by the ruling:
// the non-terminal lanes do not consult the marker.
func (c *Component) adoptDurableCancel(ctx context.Context, loopID string) (bool, error) {
	if c.loopsBucket == nil {
		return false, nil
	}
	entry, err := c.loopsBucket.Get(ctx, terminalMarkerKey(loopID))
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound), errors.Is(err, jetstream.ErrKeyDeleted):
		return false, nil
	case err != nil:
		return false, errs.WrapTransient(fmt.Errorf("read terminal marker for loop %s: %w", loopID, err),
			"agentic-loop", "adoptDurableCancel", "read the loop's durable terminal")
	}
	saved, err := decodeTerminalMarker(entry.Value())
	if err != nil {
		return false, errs.WrapFatal(fmt.Errorf("loop %s: %w", loopID, err),
			"agentic-loop", "adoptDurableCancel", "decode the loop's durable terminal")
	}
	if saved.cancelled == nil {
		return false, nil
	}
	if saved.loopID() != loopID {
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s: durable cancel names loop %q", loopID, saved.loopID()),
			"agentic-loop", "adoptDurableCancel", "match the durable terminal to its loop")
	}

	if err := c.stampTerminal(ctx, loopID, saved); err != nil {
		return false, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "stamp the adopted cancel on the graph")
	}
	messages, err := c.terminalPublication(loopID, saved)
	if err != nil {
		return false, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "build the adopted cancel's publication")
	}
	if err := c.publishResults(ctx, HandlerResult{LoopID: loopID, PublishedMessages: messages}); err != nil {
		return false, errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "published cancel has unknown durability")
	}
	if err := c.writeRecordCancelled(ctx, loopID, saved.cancelled); err != nil {
		return false, err
	}
	c.logger.WarnContext(ctx, "Cancel adopted the loop's durable cancel terminal",
		slog.String("loop_id", loopID),
		slog.Uint64("marker_revision", entry.Revision()))
	return true, nil
}

// writeRecordCancelled writes a record this process does not hold to match an
// adopted cancel, under compare-and-swap against the revision it read. A
// record that moved retries; a record that is already terminal is settled.
func (c *Component) writeRecordCancelled(ctx context.Context, loopID string, cancelled *agentic.LoopCancelledEvent) error {
	c.loopRecordMu.Lock()
	defer c.loopRecordMu.Unlock()

	record := c.readLoopRecord(ctx, loopID)
	switch record.presence {
	case loopPresenceStale:
		return nil
	case loopPresenceUnknown:
		return errs.WrapTransient(fmt.Errorf("loop %s: the loop record could not be read", loopID),
			"agentic-loop", "adoptDurableCancel", "read the loop record before writing it cancelled")
	}
	entity := record.entity
	entity.State = agentic.LoopStateCancelled
	entity.Outcome = agentic.OutcomeCancelled
	entity.CancelledBy = cancelled.CancelledBy
	entity.CancelledAt = cancelled.CancelledAt
	entity.CompletedAt = cancelled.CancelledAt
	entity.Error = "cancelled by user"
	entity.PendingApproval = nil
	entity.StateBeforeApproval = ""
	data, err := json.Marshal(entity)
	if err != nil {
		return errs.WrapFatal(err, "agentic-loop", "adoptDurableCancel", "marshal the cancelled loop record")
	}
	if _, err := c.loopsBucket.Update(ctx, loopID, data, record.revision); err != nil {
		if natsclient.IsKVConflictError(err) {
			return errs.WrapTransient(
				fmt.Errorf("loop %s record moved past revision %d: %w", loopID, record.revision, natsclient.ErrKVRevisionMismatch),
				"agentic-loop", "adoptDurableCancel", "compare-and-swap loop record")
		}
		return errs.WrapFatal(fmt.Errorf("persist cancelled loop state %s: %w", loopID, err),
			"agentic-loop", "adoptDurableCancel", "cancelled loop record has unknown durability")
	}
	return nil
}

// settleTerminalGuard decides a result the handler returned from a terminal
// guard: the loop was terminal in memory, and the handler touched nothing.
// The record answers it, not memory (#1362 re-review M1). A terminal record
// — or none — is settled: the delivery is acknowledged without effect, with
// an audit line and the lane's drop metric (Q7). A live record means the
// terminal in memory is a commit still in flight on another lane: the
// delivery is retried, and the redelivery finds that commit landed (a
// terminal record) or failed and released the loop (a live record to rebuild
// from). An unreadable record is retried.
func (c *Component) settleTerminalGuard(ctx context.Context, result HandlerResult, recordDrop func()) error {
	record := c.readLoopRecord(ctx, result.LoopID)
	if record.presence == loopPresenceStale {
		c.logger.WarnContext(ctx, "Delivery acknowledged without effect — the loop's record is terminal",
			slog.String("loop_id", result.LoopID),
			slog.String("state", result.State.String()))
		if recordDrop != nil {
			recordDrop()
		}
		return nil
	}
	return errs.WrapTransient(
		fmt.Errorf("loop %s is terminal in memory and its record is not: the terminal commit is not settled",
			result.LoopID),
		"agentic-loop", "settleTerminalGuard", "leave an uncommitted terminal to its owner")
}

// recordTerminalToolResultDropped counts a tool result a terminal loop can no
// longer apply, on the reason Q7 already names.
func (c *Component) recordTerminalToolResultDropped() {
	if c.metrics != nil {
		c.metrics.recordToolResultDropped("terminal_unproven")
	}
}
