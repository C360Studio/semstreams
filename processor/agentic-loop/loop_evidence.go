package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/nats-io/nats.go/jetstream"
)

// loopEvidenceReader reads the durable evidence a loop left behind, so a
// process with no memory of that loop can classify a redelivered input against
// facts rather than against its own absent state.
//
// Two reads, one per question recovery asks.
//
// The newest AgentRequest retained for a loop answers identity adoption's
// question — "is the request I am about to publish already on the stream?" —
// by RequestID equality, never by comparing bodies, and it is what the cold
// rebuild replays the conversation from.
//
// The retained response for a request answers the one thing the record cannot:
// how many tool calls the assistant asked for. The record carries which
// executions are APPLIED; only the response carries the batch, and
// AllToolsComplete is undecidable without it (design § 5.3 step 3).
//
// It is an interface so a unit test can drive every arm — retained-equals-next,
// retained-is-older, retained-absent, retained-is-newer — without a broker.
type loopEvidenceReader interface {
	ReadRetainedRequest(ctx context.Context, streamName, subject string) ([]byte, bool, error)
	ReadRetainedResponse(ctx context.Context, streamName, subject string) ([]byte, bool, error)
}

// natsLoopEvidenceReader is the production reader: the newest message on a
// subject, which for agent.request.<loopID> is the loop's newest request.
type natsLoopEvidenceReader struct {
	client *natsclient.Client
}

func (r natsLoopEvidenceReader) ReadRetainedRequest(
	ctx context.Context,
	streamName string,
	subject string,
) ([]byte, bool, error) {
	return r.newestOn(ctx, streamName, subject)
}

func (r natsLoopEvidenceReader) ReadRetainedResponse(
	ctx context.Context,
	streamName string,
	subject string,
) ([]byte, bool, error) {
	return r.newestOn(ctx, streamName, subject)
}

// newestOn is both reads. They differ only in the subject they address — one
// per-loop, one per-request — and giving each its own copy of a direct get
// would be two spellings of one operation.
func (r natsLoopEvidenceReader) newestOn(
	ctx context.Context,
	streamName string,
	subject string,
) ([]byte, bool, error) {
	stream, err := r.client.GetStream(ctx, streamName)
	if err != nil {
		return nil, false, fmt.Errorf("read stream %s: %w", streamName, err)
	}
	raw, err := stream.GetLastMsgForSubject(ctx, subject)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read retained message %s: %w", subject, err)
	}
	return append([]byte(nil), raw.Data...), true, nil
}

// requestAddress resolves the subject a loop's requests are published to and
// the stream that retains them, from the same output-port declaration the
// publish side uses. Asking the port rather than formatting a subject is what
// keeps the read and the write addressing the same place after a config change.
func requestAddress(ports []component.PortDefinition, loopID string) (subject, stream string, err error) {
	subject, err = component.ResolveSubject(ports, "agent.request", loopID)
	if err != nil {
		return "", "", err
	}
	for _, definition := range ports {
		if definition.Name != "agent.request" {
			continue
		}
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return "", "", err
		}
		facts, err := port.Facts()
		if err != nil {
			return "", "", err
		}
		declared, ok := facts.Stream()
		if !ok || declared.Name() == "" {
			return "", "", fmt.Errorf("agent.request output does not declare a JetStream stream")
		}
		return subject, declared.Name(), nil
	}
	return "", "", fmt.Errorf("agent.request output not found")
}

// responseAddress resolves the subject a request's response is retained on and
// the stream that holds it, from the agent.response INPUT port — the same
// declaration the delivery this component consumes arrives through, so the
// recovery read and the live subscription address the same place after a
// config change.
//
// The mirror of requestAddress on the other direction: requests are this
// component's output, responses are its input.
func responseAddress(ports []component.PortDefinition, requestID string) (subject, stream string, err error) {
	subject, err = component.ResolveSubject(ports, "agent.response", requestID)
	if err != nil {
		return "", "", err
	}
	for _, definition := range ports {
		if definition.Name != "agent.response" {
			continue
		}
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return "", "", err
		}
		facts, err := port.Facts()
		if err != nil {
			return "", "", err
		}
		declared, ok := facts.Stream()
		if !ok || declared.Name() == "" {
			return "", "", fmt.Errorf("agent.response input does not declare a JetStream stream")
		}
		return subject, declared.Name(), nil
	}
	return "", "", fmt.Errorf("agent.response input not found")
}

// readRetainedAgentRequest returns the newest AgentRequest retained for the
// loop, and whether there is one.
//
// Identity only. The body is decoded to recover its RequestID and nothing
// else: recovery orders request identities, and a decision made by comparing
// rendered conversation content is the shape this whole change removes.
func (c *Component) readRetainedAgentRequest(ctx context.Context, loopID string) (agentic.AgentRequest, bool, error) {
	if c.natsClient == nil && c.requestEvidence == nil {
		// Not "nothing is retained" — nothing can be ASKED. Both callers read
		// a false `found` as "this request has not gone out, publish it", so
		// answering absence here would let a component with no reader at all
		// stand in for a stream that holds the request, which is the
		// absence-for-unknown shape this change exists to remove.
		return agentic.AgentRequest{}, false, errs.WrapTransient(
			fmt.Errorf("loop %s: no retained-request reader is configured", loopID),
			"agentic-loop", "readRetainedAgentRequest", "read retained request evidence")
	}
	subject, stream, err := requestAddress(c.outputPortDefs(), loopID)
	if err != nil {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentRequest", "resolve request address")
	}
	reader := c.requestEvidence
	if reader == nil {
		reader = natsLoopEvidenceReader{client: c.natsClient}
	}
	data, found, err := reader.ReadRetainedRequest(ctx, stream, subject)
	if err != nil {
		return agentic.AgentRequest{}, false, errs.WrapTransient(
			err, "agentic-loop", "readRetainedAgentRequest", "read retained request evidence")
	}
	if !found {
		return agentic.AgentRequest{}, false, nil
	}

	decoded, err := c.decoder.Decode(data)
	if err != nil {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentRequest", "decode retained request envelope")
	}
	request, ok := decoded.Payload().(*agentic.AgentRequest)
	if !ok {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			fmt.Errorf("retained request payload is %T, not *agentic.AgentRequest", decoded.Payload()),
			"agentic-loop", "readRetainedAgentRequest", "decode retained request payload")
	}
	return *request, true, nil
}

// outputPortDefs returns the component's declared output ports.
func (c *Component) outputPortDefs() []component.PortDefinition {
	return c.config.Ports.Outputs
}

// inputPortDefs returns the component's declared input ports.
func (c *Component) inputPortDefs() []component.PortDefinition {
	return c.config.Ports.Inputs
}

// readRetainedAgentResponse returns the retained response for one request, and
// whether there is one.
//
// It is read for its TOOL CALLS, which is the batch size the loop record does
// not carry. Nothing else in the body is consulted: the rebuild seats the
// assistant turn the batch belongs to and re-derives each execution identity
// from the request ID and the provider call ID, never from content.
func (c *Component) readRetainedAgentResponse(
	ctx context.Context, requestID string,
) (agentic.AgentResponse, bool, error) {
	if c.natsClient == nil && c.requestEvidence == nil {
		return agentic.AgentResponse{}, false, errs.WrapTransient(
			fmt.Errorf("request %s: no retained-response reader is configured", requestID),
			"agentic-loop", "readRetainedAgentResponse", "read retained response evidence")
	}
	subject, stream, err := responseAddress(c.inputPortDefs(), requestID)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentResponse", "resolve response address")
	}
	reader := c.requestEvidence
	if reader == nil {
		reader = natsLoopEvidenceReader{client: c.natsClient}
	}
	data, found, err := reader.ReadRetainedResponse(ctx, stream, subject)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapTransient(
			err, "agentic-loop", "readRetainedAgentResponse", "read retained response evidence")
	}
	if !found {
		return agentic.AgentResponse{}, false, nil
	}
	decoded, err := c.decoder.Decode(data)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentResponse", "decode retained response envelope")
	}
	response, ok := decoded.Payload().(*agentic.AgentResponse)
	if !ok {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			fmt.Errorf("retained response payload is %T, not *agentic.AgentResponse", decoded.Payload()),
			"agentic-loop", "readRetainedAgentResponse", "decode retained response payload")
	}
	return *response, true, nil
}

// loopRecord is one observation of a loop's AGENT_LOOPS record: the decoded
// entity, the revision it was observed at, and what that observation means for
// an input naming the loop.
//
// The three travel together because every caller needs all three: the entity
// to classify against, the revision to compare-and-swap against, and the
// presence to decide whether there is anything to classify at all. Returning
// them apart is how the revision came to be discarded at the one read that had
// it (loop_presence.go's entry.Revision()).
type loopRecord struct {
	entity   agentic.LoopEntity
	revision uint64
	presence loopPresence
}

// readLoopRecord reads one loop record and reports it with the revision it was
// observed at. It performs no recovery and writes nothing.
//
// The revision travels in the returned record and is NOT retained on the
// component. Every caller here is asking about a loop this process does not
// hold — a settled one, a foreign one, one that never existed — and a
// per-loop entry is released only with the loop's own transient state, which
// such a loop never acquires. Retaining one per read is an unbounded map keyed
// by every loop this process was ever asked about. A caller that goes on to
// WRITE the record compare-and-swaps against the revision in this record; a
// caller that goes on to HOLD the loop retains its revision at the write that
// makes it the holder.
func (c *Component) readLoopRecord(ctx context.Context, loopID string) loopRecord {
	if loopID == "" || !looptoken.Valid(loopID) {
		// Nothing to look up. An input that carries no framework-minted loop
		// token names no loop any process could be holding.
		return loopRecord{presence: loopPresenceStale}
	}
	if c.loopsBucket == nil {
		return loopRecord{presence: loopPresenceUnknown}
	}
	entry, err := c.loopsBucket.Get(ctx, loopID)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound), errors.Is(err, jetstream.ErrKeyDeleted):
		return loopRecord{presence: loopPresenceStale}
	case err != nil:
		return loopRecord{presence: loopPresenceUnknown}
	}

	var entity agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &entity); err != nil {
		// A record that exists but will not decode is not evidence of
		// staleness. Report unknown and let the bounded retry surface it.
		c.logger.Error("Loop record did not decode while classifying a missing loop",
			"loop_id", loopID, "error", err)
		return loopRecord{presence: loopPresenceUnknown}
	}
	if entity.State.IsTerminal() {
		return loopRecord{entity: entity, revision: entry.Revision(), presence: loopPresenceStale}
	}
	return loopRecord{entity: entity, revision: entry.Revision(), presence: loopPresenceLive}
}

// adoptRetainedRequest decides whether a minted request still has to be
// published, by identity (owner ruling Q4 on #1330).
//
// It reports true when the request this process is about to publish is ALREADY
// retained under that exact RequestID — the crash window where the publish
// landed and the record update did not. Republishing there is not merely
// wasteful: the retained message is the authoritative body, agentic-model
// answers it from its retained response rather than calling the provider
// again, and a second copy minted from a rebuilt context is a different
// conversation under the same name.
//
// Absent, or older than the request being minted, means this request has not
// gone out: publish.
//
// Newer than the request being minted, or unparseable, or naming a different
// loop, is a conflict this delivery cannot resolve — a request beyond the one
// this process believes it is minting means some other writer advanced the
// loop — so it is quarantined rather than resolved by guessing.
//
// Simplification recorded (#1330, standing simplicity rule): the design
// separates "retained equals the record's CURRENT request" (publish) from
// "retained is older than the record's current request" (quarantine). Both are
// "older than the request being minted" here, and both publish. Reaching the
// second requires I1 to be broken already — the current request is retained by
// definition while the record exists — and publishing the request the loop
// actually needs is the better answer for a stream that lost it than refusing
// the loop.
func (c *Component) adoptRetainedRequest(ctx context.Context, loopID, requestID string) (bool, error) {
	minted, err := looprequest.Parse(requestID)
	if err != nil || minted.LoopID != loopID {
		// Not this grammar. Nothing to compare against, so publish as before.
		return false, nil
	}
	retained, found, err := c.readRetainedAgentRequest(ctx, loopID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if retained.RequestID == requestID {
		c.logger.InfoContext(ctx, "Request already retained under this identity — adopting instead of republishing",
			slog.String("loop_id", loopID), slog.String("request_id", requestID))
		return true, nil
	}
	parsed, err := looprequest.Parse(retained.RequestID)
	if err != nil || parsed.LoopID != loopID {
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s retains request %q, which is not a request of this loop", loopID, retained.RequestID),
			"agentic-loop", "adoptRetainedRequest", "order retained request against the minted one")
	}
	if looprequest.Compare(parsed, minted) > 0 {
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s retains request %q, beyond the %q this process is minting",
				loopID, retained.RequestID, requestID),
			"agentic-loop", "adoptRetainedRequest", "order retained request against the minted one")
	}
	return false, nil
}

// adoptNewerRetainedRequest is step 0 of every cold read but the task lane
// (design § 3.6, owner ruling Q4 applied to the cold path).
//
// A process with no memory of the loop cannot classify a redelivered input
// until the record tells the truth about which request is outstanding. The
// crash window this closes is W4: the predecessor published the loop's NEXT
// request and died before the record update, so the record names R while the
// stream retains R(N+1). Classifying against R there would re-apply an input
// the loop has already moved past.
//
// So the newest retained request is adopted into the record FIRST, under
// compare-and-swap, and only then is the redelivered input classified — against
// a record that is now current. The adopt writes exactly the shape the normal
// advance writes: the new request name, its iteration, and an empty applied set
// (the advance drains the set before it mints, so nothing is evicted that the
// normal path would have kept). A pending approval gate is cleared in the same
// update, because a request newer than the gate's own can only have been minted
// after that gate's batch completed.
//
// Nothing is synthesised and no message body is read beyond its RequestID.
//
// The record is read and written inside ONE critical section (loopRecordMu),
// which is the rule every loop-record write in this process follows — the
// carrier's is at persistLoopState, birth's at createLoopState. A revision
// observed outside the lock is stale the moment another lane of this process
// commits, and this write's compare-and-swap exists to report a SECOND
// PROCESS, not a sibling lane. The retained-request read is inside it too:
// step 0's whole decision is "does the stream hold something newer than the
// record", and reading the two either side of a concurrent advance would order
// a record against a stream snapshot taken before it.
//
// It returns the record the classification that follows must use: the adopted
// one at the revision the adopting write committed, or the one it read when
// there was nothing to adopt. Reporting the CAS's own resulting revision
// rather than leaving the caller to re-read is what keeps the classification
// and the write bound to the same observation (#1330, I1–I4).
//
// Residual, stated rather than discovered: this write is deliberately not
// remembered as a revision for the loop, because the process running step 0
// does not hold the loop. When it happens to hold it anyway — the tool lane
// reaches the cold arm whenever the execution's routing entry has been drained
// — the warm lane's next compare-and-swap is refused and the loop is released,
// exactly as a genuine foreign writer's commit would leave it. That is the
// designed outcome of a lost CAS, and the delivery it releases is rebuilt by
// the cold rebuild (task 1.2), not by a second in-memory path here.
func (c *Component) adoptNewerRetainedRequest(ctx context.Context, loopID string) (loopRecord, error) {
	c.loopRecordMu.Lock()
	defer c.loopRecordMu.Unlock()

	record := c.readLoopRecord(ctx, loopID)
	switch record.presence {
	case loopPresenceStale:
		// No record, or a settled one. There is nothing to bring forward and
		// nothing any process can still apply; the caller acknowledges.
		return record, nil
	case loopPresenceUnknown:
		// Step 0 adopts INTO a record, so a record that could not be READ —
		// no bucket, a transient KV error, bytes that did not decode — has
		// nothing to adopt into. What it has instead is a ZERO entity, and
		// writing the adopted request onto that produces a record with no id
		// and no state: Validate refuses it, the refusal is fatal, and a
		// delivery is quarantined because a KV read failed. Never assume
		// anything from a failed read — retry it, which is the answer
		// classifyRedeliveredTask already gives on the task lane.
		return record, errs.WrapTransient(
			fmt.Errorf("loop %s: the loop record could not be read, so its retained request cannot be adopted", loopID),
			"agentic-loop", "adoptNewerRetainedRequest", "read the loop record before adopting")
	}

	retained, found, err := c.readRetainedAgentRequest(ctx, loopID)
	if err != nil {
		return record, err
	}
	if !found {
		// No retained request means nothing to adopt. It is not evidence that
		// the record is wrong — a loop whose birth publish never landed is the
		// task lane's case, and I1 is scoped to records that exist.
		return record, nil
	}

	parsed, err := looprequest.Parse(retained.RequestID)
	if err != nil || parsed.LoopID != loopID {
		return record, errs.WrapFatal(
			fmt.Errorf("loop %s retains request %q, which is not a request of this loop", loopID, retained.RequestID),
			"agentic-loop", "adoptNewerRetainedRequest", "order the retained request")
	}

	if record.entity.PublishedRequestID != "" {
		published, perr := looprequest.Parse(record.entity.PublishedRequestID)
		if perr != nil || published.LoopID != loopID {
			return record, errs.WrapFatal(
				fmt.Errorf("loop %s record names request %q, which is not a request of this loop",
					loopID, record.entity.PublishedRequestID),
				"agentic-loop", "adoptNewerRetainedRequest", "order the retained request")
		}
		switch looprequest.Compare(parsed, published) {
		case 0:
			return record, nil
		case -1:
			// The record names a request NEWER than anything retained. I1 says
			// that cannot happen while the record exists, so something outside
			// this loop's own writers moved one of the two. Refuse rather than
			// roll the record backwards onto an older name.
			return record, errs.WrapFatal(
				fmt.Errorf("loop %s record names request %q but the stream retains only %q",
					loopID, record.entity.PublishedRequestID, retained.RequestID),
				"agentic-loop", "adoptNewerRetainedRequest", "order the retained request")
		}
	}

	adopted := record.entity
	adopted.PublishedRequestID = retained.RequestID
	// A request's iteration ordinal is the record's Iterations plus one at mint
	// time, so the record that named it carried one less.
	adopted.Iterations = parsed.Iteration - 1
	adopted.PendingToolResults = nil
	if adopted.PendingApproval != nil {
		if err := adopted.ResolveApproval(); err != nil {
			return record, errs.WrapFatal(err, "agentic-loop", "adoptNewerRetainedRequest",
				"clear the approval gate the adopted request advanced past")
		}
	}
	if err := adopted.Validate(); err != nil {
		return record, errs.WrapFatal(err, "agentic-loop", "adoptNewerRetainedRequest",
			"validate the adopted record")
	}

	data, err := json.Marshal(adopted)
	if err != nil {
		return record, errs.WrapFatal(err, "agentic-loop", "adoptNewerRetainedRequest",
			"marshal the adopted record")
	}
	// The committed revision is deliberately not retained ON THE COMPONENT:
	// this process does not hold the loop, and an entry for a loop nobody
	// holds is never released. It travels in the returned record instead, to
	// the classification that runs next against the record this write left.
	committed, err := c.loopsBucket.Update(ctx, loopID, data, record.revision)
	if err != nil {
		if natsclient.IsKVConflictError(err) {
			// Somebody else wrote the record between the read and this update.
			// Whatever they wrote, the redelivery re-reads and re-decides; the
			// retained request is durable and is not going anywhere.
			return record, errs.WrapTransient(
				fmt.Errorf("loop %s record moved past revision %d while adopting %q: %w",
					loopID, record.revision, retained.RequestID, natsclient.ErrKVRevisionMismatch),
				"agentic-loop", "adoptNewerRetainedRequest", "adopt the retained request")
		}
		return record, errs.WrapTransient(err, "agentic-loop", "adoptNewerRetainedRequest",
			"adopt the retained request")
	}
	c.logger.InfoContext(ctx, "Adopted the loop's newest retained request before classifying a redelivered input",
		slog.String("loop_id", loopID),
		slog.String("was", record.entity.PublishedRequestID),
		slog.String("now", retained.RequestID),
		slog.Int("iterations", adopted.Iterations))
	return loopRecord{entity: adopted, revision: committed, presence: record.presence}, nil
}

// restoreLoopFromEvidence gives THIS process the loop a redelivered input names,
// so the delivery can be applied instead of refused (#1330, design § 5.2 step 2
// and § 5.3 step 3; task 1.2).
//
// It runs only on a CURRENT input — one naming the request the record names,
// after step 0 has brought that record forward. An older input is answered
// without a loop at all, and a newer one is not yet observable, so neither has
// anything to rebuild for.
//
// The reads, in the order their answers are needed:
//
//  1. The newest retained request. It must be the one the record names — step 0
//     just made that true, so a disagreement means the stream moved between the
//     two reads, and the redelivery re-decides against whatever won.
//  2. The retained response for that request, ONLY when a tool result is being
//     applied. That is the one case where the batch must be re-seated before the
//     result can be classified as complete or not. A redelivered model RESPONSE
//     needs no such read: applying it IS what creates the batch, and seating one
//     first would replay the assistant turn into the conversation twice.
//
// Last, the record's revision is taken as this process's own. Without it the
// rebuilt holder's first compare-and-swap has nothing to compare against and
// fails closed — a loop recovered and then immediately stranded.
//
// A failure after the loop is seated releases it. A half-built loop in memory
// is worse than none: the redelivery would find it warm and skip the rebuild
// that failed.
func (c *Component) restoreLoopFromEvidence(
	ctx context.Context, loopID string, record loopRecord, inFlightExecutionID string,
) error {
	request, found, err := c.readRetainedAgentRequest(ctx, loopID)
	if err != nil {
		return err
	}
	if !found {
		// I1 says this cannot happen while the record exists. It is transient
		// rather than fatal because the loop is real and unfinished: refusing
		// it forever on one unreadable stream state would settle a live loop.
		return errs.WrapTransient(
			fmt.Errorf("loop %s: its record names request %q and the stream retains none",
				loopID, record.entity.PublishedRequestID),
			"agentic-loop", "restoreLoopFromEvidence", "read the request to rebuild from")
	}
	if request.RequestID != record.entity.PublishedRequestID {
		return errs.WrapTransient(
			fmt.Errorf("loop %s: the record names request %q and the stream now retains %q",
				loopID, record.entity.PublishedRequestID, request.RequestID),
			"agentic-loop", "restoreLoopFromEvidence", "match the retained request to the record")
	}

	if err := c.handler.loopManager.restoreLoopFromRequest(record.entity, request); err != nil {
		return err
	}

	if inFlightExecutionID != "" {
		response, found, err := c.readRetainedAgentResponse(ctx, request.RequestID)
		if err != nil {
			c.releaseLoopTransientState(loopID)
			return err
		}
		if !found {
			c.releaseLoopTransientState(loopID)
			return errs.WrapTransient(
				fmt.Errorf("loop %s: a tool result for request %q arrived and the stream retains no response for it",
					loopID, request.RequestID),
				"agentic-loop", "restoreLoopFromEvidence", "read the batch to rebuild from")
		}
		if err := c.handler.loopManager.restoreToolBatch(
			loopID, response, record.entity.PendingToolResults, inFlightExecutionID); err != nil {
			c.releaseLoopTransientState(loopID)
			return err
		}
	}

	// The in-memory trajectory aggregate starts here, empty. It is an
	// active-loop execution convenience, not the authority: the immutable KV
	// fact log holds what this loop has done, and the predecessor's steps are
	// already in it. Without the aggregate every step this process records
	// warns instead of landing.
	if _, err := c.handler.trajectoryManager.startTrajectory(loopID); err != nil {
		c.logger.WarnContext(ctx, "Rebuilt loop has no trajectory aggregate — its steps will not be aggregated",
			slog.String("loop_id", loopID), slog.String("error", err.Error()))
	}

	c.rememberLoopRevision(loopID, record.revision)
	c.logger.InfoContext(ctx, "Rebuilt a loop this process never started, from its record and its retained request",
		slog.String("loop_id", loopID),
		slog.String("published_request_id", request.RequestID),
		slog.Int("iterations", record.entity.Iterations),
		slog.Int("applied_tool_results", len(record.entity.PendingToolResults)))
	return nil
}
