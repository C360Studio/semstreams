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
// D41/P6; owner ruling 2026-09-18 on #1330). Every terminal lane — the carrier,
// the loop-failure path and cancel — commits through it, in this order:
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
