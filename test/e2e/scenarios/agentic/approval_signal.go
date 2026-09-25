package agentic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// This file walks the two loop-naming lanes the tier used to skip entirely
// (#1238): the approval round trip (ApprovalPendingEvent → ApprovalResponse)
// and the one live signal lane (the /cancel chat command → agentic.UserSignal).
// PR #1231 deleted POST /loops/{id}/signal and the SignalMessage type, so the
// chat command is the whole signal surface — there is no second lane to cover.
//
// Both lanes are walked twice: once for the admit, once for the refusal a
// non-canonical loop token earns at the admission gate
// (processor/agentic-dispatch/loop_admission.go). The refusal is asserted as a
// CLASSIFICATION, never as an absence: the HTTP status the gate's one mapping
// chose plus the reason label it moved. A canonical-but-absent token is asserted
// beside it so a gate that refused everything the same way fails here — an
// assertion that only ever sees one refusal cannot tell a classifier from a
// constant.
const (
	// approvalGatedTool is the tool configs/agentic.json lists under
	// agentic-tools approval_required. It is deliberately NOT the tool the
	// primary task drives: gating query_entity would put every loop in the tier
	// behind an approval.
	approvalGatedTool = "query_by_type"

	// approvalLoopOwner is the user_id the approval-path task carries, and
	// approvalRequester is the identity that answers it. They DIFFER on purpose:
	// the gate does not consult ownership for approve — a second-party reviewer
	// is the point of an approval — so a same-identity test would pass even if
	// an owner check were added back.
	approvalLoopOwner = "e2e-loop-owner"
	approvalRequester = "e2e-approver"

	// signalLoopOwner owns the loop the /cancel walk cancels. Cancel DOES
	// consult ownership (cancel_any is empty in this tier), so the requester and
	// the loop's recorded user must be the same identity here.
	signalLoopOwner = "e2e-signal-owner"

	// dispatchRoutePrefix is where the service manager mounts a gateway
	// component's routes: "/" + the component's configured name
	// (service/service_manager.go registerComponentHandlers).
	dispatchRoutePrefix = "/agentic-dispatch"

	// agentStream is the JetStream stream carrying every agent.> subject in
	// this tier (configs/agentic.json streams.AGENT), and agentLoopsBucket is
	// the KV bucket agentic-loop declares as loops_bucket in the same file.
	agentStream      = "AGENT"
	agentLoopsBucket = "AGENT_LOOPS"

	// loopAdmissionRefusalsMetric is the single series every refused
	// loop-naming request moves, labelled by the seam it arrived on and the one
	// mapped reason (processor/agentic-dispatch/metrics.go).
	loopAdmissionRefusalsMetric = "semstreams_router_loop_admission_refusals_total"

	// toolExecutionsMetric counts executor invocations by tool name AND status.
	// For approvalGatedTool it can only be nonzero via an approved re-dispatch:
	// the approval filter refuses every un-approved call to it.
	//
	// The status label is never omitted from a filter on this series. The e2e
	// metrics client matches a label map as a SUBSET, so {tool_name} alone also
	// counts status="error" — and query_by_type answers exactly that when its
	// arguments are missing, which is what the mock would send if its pinned
	// args were dropped. Filtering on success is what keeps this assertion
	// about an execution that WORKED.
	toolExecutionsMetric   = "semstreams_agentic_tools_executions_total"
	toolExecutionSucceeded = "success"

	// toolStream carries tool.execute.> and tool.result.> in this tier
	// (configs/agentic.json streams.TOOL). The approved call's RESULT lands on
	// tool.result.<execution_id> (#1328 moved the result address off the
	// provider call id), which is what lets this walk read what the tool
	// actually answered instead of only that it answered.
	toolStream = "TOOL"

	// servedTypeSegments is the entity_type the mock's pinned call carries:
	// two right-anchored segments (domain `agent`, type `execution`) over the
	// loop-execution entities this tier actually writes to ENTITY_STATES.
	//
	// It is deliberately NOT "temperature", which the tier writes none of — a
	// pinned type that matches nothing makes a listing assertion pass
	// identically over a served listing, an empty one, and a stub.
	servedTypeSegments = "agent.execution"

	// servedTypePattern is the six-position pattern that entity_type must
	// build (org.platform.system.domain.type.instance, right-anchored on the
	// type segment). Asserting the pattern rather than only a count is what
	// makes this an assertion about the ADR-102 type axis instead of about any
	// listing at all.
	servedTypePattern = "*.*.*." + servedTypeSegments + ".*"

	// ApprovalGatedToolArgs is the argument JSON the mock LLM sends for the
	// approval-gated call. It is EXPORTED so the mock binary and this walk read
	// one source instead of each predicting the other's string — the drift that
	// let the old pinned "temperature" sit here unnoticed. The mock consumes
	// scenario constants this way already (crudtools.PersonaMarker,
	// opsscenario.SeedLoop1ID, researchgraph.ControlledSeedSuffix).
	ApprovalGatedToolArgs = `{"entity_type": "` + servedTypeSegments + `", "limit": 5}`
)

// nonCanonicalToken returns the uppercase spelling of a loop token: 36 bytes
// that parse as a UUID but are not the canonical form the framework mints, so
// internal/looptoken.Valid refuses them.
//
// Taking the LIVE loop's own token and re-spelling it is what makes the
// assertion sharp: the refusal cannot be about existence, because the loop
// exists — it can only be about form, which is the check order the gate pins
// (form, then existence, then ownership).
func nonCanonicalToken(loopID string) string {
	return strings.ToUpper(loopID)
}

// newApprovalGatedTask builds the task whose only advertised tool is the
// approval-gated one. The mock LLM answers a first turn with a call to
// Tools[0], so advertising exactly one tool is what makes the gated call
// deterministic.
func newApprovalGatedTask(now time.Time, suffix, userID string) agentic.TaskMessage {
	taskID := fmt.Sprintf("e2e-agentic-%s-%d", suffix, now.UnixNano())
	return agentic.TaskMessage{
		// Framework-minted canonical UUID (ADR-105, #1192).
		LoopID:      uuid.NewString(),
		TaskID:      taskID,
		Role:        "general",
		Model:       "mock",
		Prompt:      "List the agent execution entities on record. Use the query_by_type tool.",
		ChannelType: "e2e",
		ChannelID:   taskID,
		UserID:      userID,
		Tools: []agentic.ToolDefinition{{
			Name:        approvalGatedTool,
			Description: "List the entity IDs whose identity carries a given type segment.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_type": map[string]any{"type": "string"},
					"limit":       map[string]any{"type": "integer"},
				},
				"required": []string{"entity_type"},
			},
		}},
		ToolChoice: &agentic.ToolChoice{Mode: "function", FunctionName: approvalGatedTool},
	}
}

// walkApprovalPath drives one loop from submission to a human approval and out
// the far side: the gated call is refused by the approval filter, the loop
// parks in awaiting_approval and publishes ApprovalPendingEvent, the HTTP
// approval seam admits a second-party approver and publishes ApprovalResponse,
// and the re-dispatched call actually executes.
func (s *Scenario) walkApprovalPath(ctx context.Context, result *scenarios.Result) error {
	executionLabels := map[string]string{
		"tool_name": approvalGatedTool,
		"status":    toolExecutionSucceeded,
	}
	executionsBefore, err := s.metricWithLabels(ctx, toolExecutionsMetric, executionLabels)
	if err != nil {
		return fmt.Errorf("read gated tool execution baseline: %w", err)
	}
	if executionsBefore != 0 {
		return fmt.Errorf("%s%v = %v before any approval; the gated tool ran without one",
			toolExecutionsMetric, executionLabels, executionsBefore)
	}

	task := newApprovalGatedTask(time.Now(), "approval", approvalLoopOwner)
	if err := s.publishTask(ctx, "agent.task.e2e-approval", task); err != nil {
		return err
	}
	result.Details["approval_loop_id"] = task.LoopID

	pending, err := s.awaitApprovalPending(ctx, task.LoopID)
	if err != nil {
		return err
	}
	if pending.ToolName != approvalGatedTool || pending.CallID == "" {
		return fmt.Errorf("approval-pending event = tool:%q call:%q, want tool %q and a call id",
			pending.ToolName, pending.CallID, approvalGatedTool)
	}
	if !agentic.IsApprovalRequired(pending.Reason) {
		return fmt.Errorf("approval-pending reason = %q, want the approval-required prefix", pending.Reason)
	}

	parked, err := s.awaitLoopState(ctx, task.LoopID, agentic.LoopStateAwaitingApproval)
	if err != nil {
		return err
	}
	if parked.UserID != approvalLoopOwner {
		return fmt.Errorf("parked loop user_id = %q, want %q", parked.UserID, approvalLoopOwner)
	}
	// The durable pending record is the only carrier of the gated call's
	// EXECUTION id: ApprovalPendingEvent publishes the provider call id, and
	// #1328 addresses the result off the execution id the loop re-dispatches
	// under (approval_response_handler.go rebuilds the call from this record).
	if parked.PendingApproval == nil || parked.PendingApproval.ExecutionID == "" {
		return fmt.Errorf("parked loop %q carries no pending execution identity to read a result under", task.LoopID)
	}
	gatedExecutionID := parked.PendingApproval.ExecutionID
	if parked.PendingApproval.CallID != pending.CallID {
		return fmt.Errorf("parked pending call %q does not match the published approval-pending call %q",
			parked.PendingApproval.CallID, pending.CallID)
	}

	if err := s.submitApproval(ctx, task.LoopID, gatedExecutionID, agentic.ApprovalDecisionApprove, ""); err != nil {
		return err
	}
	if err := s.verifyApprovalResponsePublished(ctx, task.LoopID, pending.CallID); err != nil {
		return err
	}

	// The approved re-dispatch carries ApprovedBy, which is the only way a call
	// to this tool reaches an executor at all — and it must land on the SUCCESS
	// status, so a call that reached the executor and was refused by it cannot
	// satisfy this.
	if err := s.waitMetricWithLabels(ctx, toolExecutionsMetric,
		executionLabels, executionsBefore+1, 30*time.Second); err != nil {
		return fmt.Errorf("approved tool call did not execute successfully: %w", err)
	}
	outcome, err := s.awaitTerminalOutcome(ctx, task.LoopID)
	if err != nil {
		return err
	}
	if outcome != agentic.OutcomeSuccess {
		return fmt.Errorf("approved loop outcome = %q, want %q", outcome, agentic.OutcomeSuccess)
	}

	if err := s.verifyServedTypeListing(ctx, result, gatedExecutionID); err != nil {
		return err
	}

	result.Details["approval_call_id"] = pending.CallID
	result.Details["approval_outcome"] = outcome
	return nil
}

// verifyServedTypeListing is the booted-binary half of the RC-6 walked path for
// KVKeyLister (#1261 task 4.5).
//
// The success counter above cannot carry this weight on its own: a zero-key
// listing is also status="success", so that assertion passes identically over a
// working listing, an empty one, and the advertised-absent stub the tool used
// to be. This reads the approved call's actual RESULT off the TOOL stream and
// asserts the three facts only a served listing can produce — the pattern the
// ADR-102 type axis built, a non-zero match, and the primary loop's execution
// entity among the identities returned. That entity is the one
// verify-graph-triples proved resident five stages earlier, so the assertion
// closes over a fact this tier already established rather than a new one.
func (s *Scenario) verifyServedTypeListing(ctx context.Context, result *scenarios.Result, executionID string) error {
	wantID, _ := result.Details["graph_loop_entity_id"].(string)
	if wantID == "" {
		return fmt.Errorf("served-listing proof requires the loop entity id verify-graph-triples recorded")
	}

	toolResult, err := s.awaitToolResult(ctx, executionID)
	if err != nil {
		return err
	}
	if toolResult.Error != "" {
		return fmt.Errorf("approved %s returned %q (kind %q)", approvalGatedTool, toolResult.Error, toolResult.ErrorKind)
	}

	var listing struct {
		EntityType string   `json:"entity_type"`
		Pattern    string   `json:"pattern"`
		Matched    int      `json:"matched"`
		EntityIDs  []string `json:"entity_ids"`
	}
	if err := json.Unmarshal([]byte(toolResult.Content), &listing); err != nil {
		return fmt.Errorf("decode %s content: %w (content %q)", approvalGatedTool, err, toolResult.Content)
	}
	if listing.Pattern != servedTypePattern {
		return fmt.Errorf("%s pattern = %q, want %q — the type axis is not what the tool matched on",
			approvalGatedTool, listing.Pattern, servedTypePattern)
	}
	if listing.Matched < 1 {
		return fmt.Errorf("%s matched = %d over %q; the tier's loop-execution entities are resident, "+
			"so a zero match means the listing did not read them", approvalGatedTool, listing.Matched, listing.Pattern)
	}
	found := false
	for _, id := range listing.EntityIDs {
		if id == wantID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%s returned %v, which does not contain the loop execution entity %q "+
			"verify-graph-triples proved present", approvalGatedTool, listing.EntityIDs, wantID)
	}

	result.Details["approval_listing_pattern"] = listing.Pattern
	result.Metrics["approval_listing_matched"] = listing.Matched
	return nil
}

// awaitToolResult polls the TOOL stream for the result of one tool execution
// and decodes the ToolResult out of its envelope. Absence is retried; any
// other read failure returns immediately.
func (s *Scenario) awaitToolResult(ctx context.Context, executionID string) (*agentic.ToolResult, error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return nil, fmt.Errorf("open JetStream: %w", err)
	}
	stream, err := js.Stream(ctx, toolStream)
	if err != nil {
		return nil, fmt.Errorf("open %s stream: %w", toolStream, err)
	}
	subject := "tool.result." + executionID
	deadline := time.Now().Add(s.config.TaskTimeout)
	for {
		stored, getErr := stream.GetLastMsgForSubject(ctx, subject)
		if getErr == nil {
			var envelope struct {
				Payload agentic.ToolResult `json:"payload"`
			}
			if err := json.Unmarshal(stored.Data, &envelope); err != nil {
				return nil, fmt.Errorf("decode ToolResult on %s: %w", subject, err)
			}
			return &envelope.Payload, nil
		}
		if !isMsgNotFound(getErr) {
			return nil, fmt.Errorf("read %s: %w", subject, getErr)
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("no tool result on %s within %s", subject, s.config.TaskTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// verifyApprovalResponsePublished reads back the ApprovalResponse the HTTP seam
// published on the framework's own subject and decodes it through the
// production payload registry — the wire payload the loop consumes, not the
// endpoint's success envelope.
func (s *Scenario) verifyApprovalResponsePublished(ctx context.Context, loopID, callID string) error {
	baseMsg, err := s.awaitStreamPayload(ctx, "agent.approval_response."+loopID, 20*time.Second)
	if err != nil {
		return fmt.Errorf("read approval response for loop %s: %w", loopID, err)
	}
	response, ok := baseMsg.Payload().(*agentic.ApprovalResponse)
	if !ok {
		return fmt.Errorf("approval response payload type = %T, want *agentic.ApprovalResponse", baseMsg.Payload())
	}
	if response.LoopID != loopID || response.CallID != callID {
		return fmt.Errorf("approval response = loop:%q call:%q, want loop:%q call:%q",
			response.LoopID, response.CallID, loopID, callID)
	}
	if response.Decision != agentic.ApprovalDecisionApprove || response.ApprovedBy != approvalRequester {
		return fmt.Errorf("approval response = decision:%q by:%q, want %q by %q",
			response.Decision, response.ApprovedBy, agentic.ApprovalDecisionApprove, approvalRequester)
	}
	return nil
}

// refuseNonCanonicalApproval asserts the approval endpoint's two classified
// refusals: a token that is not in canonical form is 400/form_malformed, and a
// canonical token naming no loop is 404/existence_absent. Asserting both is
// what proves the gate classifies rather than blanket-refuses.
func (s *Scenario) refuseNonCanonicalApproval(ctx context.Context, result *scenarios.Result) error {
	loopID, _ := result.Details["approval_loop_id"].(string)
	if loopID == "" {
		return fmt.Errorf("approval refusal proof requires the admitted loop id")
	}
	refusals := []loopRefusalCase{
		{
			name:   "non_canonical",
			loopID: nonCanonicalToken(loopID),
			status: http.StatusBadRequest,
			seam:   "http_loop_approval",
			reason: "form_malformed",
		},
		{
			name:   "absent",
			loopID: uuid.NewString(),
			status: http.StatusNotFound,
			seam:   "http_loop_approval",
			reason: "existence_absent",
		},
	}
	for _, refusal := range refusals {
		before, err := s.metricWithLabels(ctx, loopAdmissionRefusalsMetric,
			map[string]string{"seam": refusal.seam, "reason": refusal.reason})
		if err != nil {
			return fmt.Errorf("read %s refusal baseline: %w", refusal.name, err)
		}
		status, body, err := s.postJSON(ctx,
			fmt.Sprintf("%s/loops/%s/approval", dispatchRoutePrefix, refusal.loopID),
			agenticdispatch.ApprovalRequest{
				Decision: agentic.ApprovalDecisionApprove,
				// Well-formed on purpose. The endpoint also refuses a body that
				// names no execution, and this walk is about the ADMISSION
				// gate's classification — the refusal-reason metric below is
				// what tells the two apart, and a body missing a required field
				// would leave the status ambiguous.
				ExecutionID: uuid.NewString(),
				UserID:      approvalRequester,
			})
		if err != nil {
			return fmt.Errorf("post %s approval: %w", refusal.name, err)
		}
		if status != refusal.status {
			return fmt.Errorf("%s approval status = %d, want %d (body %s)",
				refusal.name, status, refusal.status, strings.TrimSpace(string(body)))
		}
		if err := s.waitMetricWithLabels(ctx, loopAdmissionRefusalsMetric,
			map[string]string{"seam": refusal.seam, "reason": refusal.reason},
			before+1, 15*time.Second); err != nil {
			return fmt.Errorf("%s approval refusal was not counted: %w", refusal.name, err)
		}
		result.Details["approval_refusal_"+refusal.name+"_status"] = status
	}
	return nil
}

// refuseNonCanonicalApprovalCarrier asserts the ApprovalResponse CARRIER's own
// loop-token refusal (ApprovalResponse.Validate, agentic/approval.go), which
// refuse-non-canonical-approval cannot reach: the dispatch admission gate
// refuses a malformed token before any ApprovalResponse is built (#1238).
//
// The carrier is the one the loop validates on receipt
// (HandleApprovalResponse), and the answer lane is open to any publisher — a
// product approval UI answers on it directly. So the answer is published
// straight onto the lane, with the completed approval loop's token re-spelled
// out of canonical form. The production marshaller refuses to author that
// payload (it runs the same Validate), so the bytes are built by marshalling
// the canonical answer and re-spelling its one token occurrence.
//
// Two observations, both required:
//   - the loop TERMINATES the delivery — the agentic-loop spec's disposition
//     for an input the lane cannot accept — read from the server's
//     MSG_TERMINATED advisory for the exact stream sequence published, on the
//     exact consumer the loop runs over the lane;
//   - the answer is NOT acknowledged as an inapplicable answer. Without the
//     carrier refusal the re-spelled token is an ordinary key naming no record,
//     so the loop's cold branch acknowledges it and moves
//     approval_inapplicable; that is the effect whose absence is asserted.
func (s *Scenario) refuseNonCanonicalApprovalCarrier(ctx context.Context, result *scenarios.Result) error {
	loopID, _ := result.Details["approval_loop_id"].(string)
	if loopID == "" {
		return fmt.Errorf("approval carrier refusal proof requires the admitted loop id")
	}
	malformed := nonCanonicalToken(loopID)
	answer := agentic.ApprovalResponse{
		LoopID:      loopID,
		CallID:      uuid.NewString(),
		ExecutionID: uuid.NewString(),
		Decision:    agentic.ApprovalDecisionApprove,
		ApprovedBy:  approvalRequester,
		Reason:      "e2e #1238: an answer whose loop token is not in canonical form",
		DecidedAt:   time.Now().UTC(),
	}
	canonical, err := json.Marshal(message.NewBaseMessage(answer.Schema(), &answer, "e2e-test"))
	if err != nil {
		return fmt.Errorf("marshal the canonical answer: %w", err)
	}
	if n := bytes.Count(canonical, []byte(loopID)); n != 1 {
		return fmt.Errorf("canonical answer carries loop token %s %d times, want exactly 1", loopID, n)
	}
	data := bytes.Replace(canonical, []byte(loopID), []byte(malformed), 1)

	js, err := s.nats.Client().JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream: %w", err)
	}
	stream, err := js.Stream(ctx, agentStream)
	if err != nil {
		return fmt.Errorf("open %s stream: %w", agentStream, err)
	}
	consumer, err := laneConsumerName(ctx, stream, consumerLane{
		stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.approval_response",
	})
	if err != nil {
		return fmt.Errorf("find the loop's approval-answer consumer: %w", err)
	}

	inapplicable := map[string]string{"reason": "approval_inapplicable"}
	before, err := s.metricWithLabels(ctx, toolResultsDroppedMetric, inapplicable)
	if err != nil {
		return fmt.Errorf("read inapplicable approval answers before the malformed answer: %w", err)
	}

	// Subscribed and flushed BEFORE the publish, so the advisory cannot be
	// emitted ahead of the subscription that is waiting for it.
	conn := s.nats.Client().GetConnection()
	advisories, err := conn.SubscribeSync(msgTerminatedAdvisoryPrefix + agentStream + "." + consumer)
	if err != nil {
		return fmt.Errorf("subscribe to %s terminations: %w", consumer, err)
	}
	defer func() { _ = advisories.Unsubscribe() }()
	// FlushWithContext refuses a context without a deadline.
	flushCtx, cancelFlush := context.WithTimeout(ctx, 5*time.Second)
	err = conn.FlushWithContext(flushCtx)
	cancelFlush()
	if err != nil {
		return fmt.Errorf("flush the advisory subscription: %w", err)
	}

	ack, err := js.Publish(ctx, "agent.approval_response."+malformed, data)
	if err != nil {
		return fmt.Errorf("publish the malformed answer: %w", err)
	}
	if err := awaitTermination(ctx, advisories, ack.Sequence, 15*time.Second); err != nil {
		return fmt.Errorf("the loop did not terminate an answer naming non-canonical loop token %s "+
			"(stream %s seq %d, consumer %s): %w", malformed, agentStream, ack.Sequence, consumer, err)
	}

	after, err := s.metricWithLabels(ctx, toolResultsDroppedMetric, inapplicable)
	if err != nil {
		return fmt.Errorf("read inapplicable approval answers after the malformed answer: %w", err)
	}
	if after != before {
		return fmt.Errorf("%s%v moved %v -> %v: the malformed answer was acknowledged as an inapplicable "+
			"answer instead of refused", toolResultsDroppedMetric, inapplicable, before, after)
	}
	result.Details["approval_carrier_refusal_stream_seq"] = ack.Sequence
	return nil
}

// msgTerminatedAdvisoryPrefix is the JetStream advisory subject a consumer's
// Term of a delivery is announced on, completed by "<stream>.<consumer>".
const msgTerminatedAdvisoryPrefix = "$JS.EVENT.ADVISORY.CONSUMER.MSG_TERMINATED."

// awaitTermination reads MSG_TERMINATED advisories until one names the stream
// sequence the caller published. A termination of any other sequence is not
// the answer and is skipped.
func awaitTermination(ctx context.Context, advisories *nats.Subscription, seq uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("no termination advisory for stream seq %d within %v", seq, timeout)
		}
		waitCtx, cancel := context.WithTimeout(ctx, remaining)
		msg, err := advisories.NextMsgWithContext(waitCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("no termination advisory for stream seq %d within %v: %w", seq, timeout, err)
		}
		var advisory struct {
			Type      string `json:"type"`
			StreamSeq uint64 `json:"stream_seq"`
		}
		if err := json.Unmarshal(msg.Data, &advisory); err != nil {
			return fmt.Errorf("decode termination advisory: %w", err)
		}
		if advisory.Type == "io.nats.jetstream.advisory.v1.terminated" && advisory.StreamSeq == seq {
			return nil
		}
	}
}

// loopRefusalCase is one classified refusal to prove: the token that earns it,
// the status an HTTP seam answers with (zero on the chat lane, which answers
// 200 with a typed error either way), and the seam/reason label pair the gate
// moves. The values are correlated, so they travel together.
type loopRefusalCase struct {
	name   string
	loopID string
	status int
	seam   string
	reason string
}

// walkSignalPath drives the one live signal lane end to end: the /cancel chat
// command over the dispatch HTTP message endpoint, the agentic.UserSignal it
// publishes, and the cancellation the loop performs.
//
// The target loop is parked in awaiting_approval first. That is not incidental:
// a loop cancelled mid-flight races its own completion, while a loop waiting on
// a human stays cancellable for as long as the assertion needs.
func (s *Scenario) walkSignalPath(ctx context.Context, result *scenarios.Result) error {
	task := newApprovalGatedTask(time.Now(), "signal", signalLoopOwner)
	if err := s.publishTask(ctx, "agent.task.e2e-signal", task); err != nil {
		return err
	}
	result.Details["signal_loop_id"] = task.LoopID

	if _, err := s.awaitApprovalPending(ctx, task.LoopID); err != nil {
		return fmt.Errorf("park the signal-path loop: %w", err)
	}
	if _, err := s.awaitLoopState(ctx, task.LoopID, agentic.LoopStateAwaitingApproval); err != nil {
		return err
	}

	response, err := s.chatCommand(ctx, signalLoopOwner, "/cancel "+task.LoopID)
	if err != nil {
		return err
	}
	if response.Type != agentic.ResponseTypeStatus {
		return fmt.Errorf("/cancel response type = %q content = %q, want %q",
			response.Type, response.Content, agentic.ResponseTypeStatus)
	}
	if !strings.Contains(response.Content, task.LoopID) {
		return fmt.Errorf("/cancel response %q does not name loop %s", response.Content, task.LoopID)
	}
	// Only the branch that PUBLISHED a signal sets in_reply_to; the
	// already-settled answer carries the same status type and names the same
	// loop, and this is what tells them apart
	// (processor/agentic-dispatch/commands.go handleCancelCommand).
	if response.InReplyTo != task.LoopID {
		return fmt.Errorf("/cancel response in_reply_to = %q, want %q — the loop was answered, not signalled",
			response.InReplyTo, task.LoopID)
	}

	baseMsg, err := s.awaitStreamPayload(ctx, "agent.signal."+task.LoopID, 20*time.Second)
	if err != nil {
		return fmt.Errorf("read cancel signal for loop %s: %w", task.LoopID, err)
	}
	signal, ok := baseMsg.Payload().(*agentic.UserSignal)
	if !ok {
		return fmt.Errorf("signal payload type = %T, want *agentic.UserSignal", baseMsg.Payload())
	}
	if signal.Type != agentic.SignalCancel || signal.LoopID != task.LoopID || signal.UserID != signalLoopOwner {
		return fmt.Errorf("cancel signal = type:%q loop:%q user:%q, want %q/%s/%s",
			signal.Type, signal.LoopID, signal.UserID, agentic.SignalCancel, task.LoopID, signalLoopOwner)
	}

	outcome, err := s.awaitTerminalOutcome(ctx, task.LoopID)
	if err != nil {
		return err
	}
	if outcome != agentic.OutcomeCancelled {
		return fmt.Errorf("signalled loop outcome = %q, want %q", outcome, agentic.OutcomeCancelled)
	}
	cancelled, err := s.awaitLoopState(ctx, task.LoopID, agentic.LoopStateCancelled)
	if err != nil {
		return err
	}
	if cancelled.CancelledBy != signalLoopOwner {
		return fmt.Errorf("cancelled_by = %q, want %q", cancelled.CancelledBy, signalLoopOwner)
	}

	result.Details["signal_outcome"] = outcome
	return nil
}

// refuseNonCanonicalSignal asserts the chat lane's classified refusals. The
// command answers 200 with a typed error response either way — the
// classification lives in the refusal message and in the reason label the gate
// moved, which is what this asserts.
func (s *Scenario) refuseNonCanonicalSignal(ctx context.Context, result *scenarios.Result) error {
	loopID, _ := result.Details["signal_loop_id"].(string)
	if loopID == "" {
		return fmt.Errorf("signal refusal proof requires the cancelled loop id")
	}
	refusals := []loopRefusalCase{
		{name: "non_canonical", loopID: nonCanonicalToken(loopID), seam: "cancel_command", reason: "form_malformed"},
		{name: "absent", loopID: uuid.NewString(), seam: "cancel_command", reason: "existence_absent"},
	}
	for _, refusal := range refusals {
		labels := map[string]string{"seam": refusal.seam, "reason": refusal.reason}
		before, err := s.metricWithLabels(ctx, loopAdmissionRefusalsMetric, labels)
		if err != nil {
			return fmt.Errorf("read %s cancel refusal baseline: %w", refusal.name, err)
		}
		response, err := s.chatCommand(ctx, signalLoopOwner, "/cancel "+refusal.loopID)
		if err != nil {
			return err
		}
		if response.Type != agentic.ResponseTypeError {
			return fmt.Errorf("/cancel %s response type = %q content = %q, want %q",
				refusal.name, response.Type, response.Content, agentic.ResponseTypeError)
		}
		if err := s.waitMetricWithLabels(ctx, loopAdmissionRefusalsMetric,
			labels, before+1, 15*time.Second); err != nil {
			return fmt.Errorf("%s cancel refusal was not counted: %w", refusal.name, err)
		}
		// The OBSERVED counter, not the label that was asked for: recording the
		// expectation would make validate-results re-read its own input and
		// assert nothing.
		observed, err := s.metricWithLabels(ctx, loopAdmissionRefusalsMetric, labels)
		if err != nil {
			return fmt.Errorf("read %s cancel refusal counter: %w", refusal.name, err)
		}
		result.Details["signal_refusal_"+refusal.name+"_count"] = observed
	}
	return nil
}

// publishTask marshals a task through the production BaseMessage envelope and
// publishes it on the agent.task.* input port subject.
func (s *Scenario) publishTask(ctx context.Context, subject string, task agentic.TaskMessage) error {
	envelope := message.NewBaseMessage(task.Schema(), &task, "e2e-test")
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal task %s: %w", task.TaskID, err)
	}
	if err := s.nats.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish task %s: %w", task.TaskID, err)
	}
	return nil
}

// awaitApprovalPending waits for the loop's ApprovalPendingEvent and decodes it
// through the production payload registry.
// The gated call is refused on the loop's FIRST tool round, so the event owes
// its arrival within the task budget — waiting the completion budget for it
// only delays a failure whose cause is already decided.
func (s *Scenario) awaitApprovalPending(ctx context.Context, loopID string) (*agentic.ApprovalPendingEvent, error) {
	baseMsg, err := s.awaitStreamPayload(ctx, "agent.approval_pending."+loopID, s.config.TaskTimeout)
	if err != nil {
		return nil, fmt.Errorf("read approval-pending event for loop %s: %w", loopID, err)
	}
	pending, ok := baseMsg.Payload().(*agentic.ApprovalPendingEvent)
	if !ok {
		return nil, fmt.Errorf("approval-pending payload type = %T, want *agentic.ApprovalPendingEvent", baseMsg.Payload())
	}
	if pending.LoopID != loopID {
		return nil, fmt.Errorf("approval-pending loop_id = %q, want %q", pending.LoopID, loopID)
	}
	return pending, nil
}

// awaitStreamPayload polls the stream for the last message on a subject and
// decodes it through the production registry. Absence is retried; any other
// read failure is returned immediately.
func (s *Scenario) awaitStreamPayload(
	ctx context.Context, subject string, timeout time.Duration,
) (*message.BaseMessage, error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return nil, fmt.Errorf("open JetStream: %w", err)
	}
	stream, err := js.Stream(ctx, agentStream)
	if err != nil {
		return nil, fmt.Errorf("open %s stream: %w", agentStream, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		stored, getErr := stream.GetLastMsgForSubject(ctx, subject)
		if getErr == nil {
			return s.decoder.Decode(stored.Data)
		}
		if !isMsgNotFound(getErr) {
			return nil, fmt.Errorf("read %s: %w", subject, getErr)
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("no message on %s within %s", subject, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func isMsgNotFound(err error) bool {
	return errors.Is(err, jetstream.ErrMsgNotFound)
}

// awaitLoopState polls the durable AGENT_LOOPS record until the loop reports
// the wanted state. The record — not the dispatch tracker's projection — is the
// observation, because the tracker holds an externally-created loop's state
// only as of its creation event.
func (s *Scenario) awaitLoopState(
	ctx context.Context, loopID string, want agentic.LoopState,
) (*agentic.LoopEntity, error) {
	deadline := time.Now().Add(s.config.CompleteTimeout)
	lastState := agentic.LoopState("")
	for {
		raw, err := s.nats.GetKV(ctx, agentLoopsBucket, loopID)
		if err == nil {
			var entity agentic.LoopEntity
			if err := json.Unmarshal(raw, &entity); err != nil {
				return nil, fmt.Errorf("decode %s/%s: %w", agentLoopsBucket, loopID, err)
			}
			if entity.State == want {
				return &entity, nil
			}
			lastState = entity.State
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("loop %s state = %q, want %q within %s",
				loopID, lastState, want, s.config.CompleteTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// awaitTerminalOutcome reads the loop's terminal event off the AGENT stream and
// returns the outcome it declares.
func (s *Scenario) awaitTerminalOutcome(ctx context.Context, loopID string) (string, error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return "", fmt.Errorf("open JetStream: %w", err)
	}
	stream, err := js.Stream(ctx, agentStream)
	if err != nil {
		return "", fmt.Errorf("open %s stream: %w", agentStream, err)
	}
	deadline := time.Now().Add(s.config.CompleteTimeout)
	for {
		stored, getErr := stream.GetLastMsgForSubject(ctx, "agent.complete."+loopID)
		if getErr == nil {
			decoded, err := s.decoder.Decode(stored.Data)
			if err != nil {
				return "", fmt.Errorf("decode terminal for loop %s through the production registry: %w", loopID, err)
			}
			outcome, err := terminalOutcome(decoded.Payload())
			if err != nil {
				return "", fmt.Errorf("loop %s: %w", loopID, err)
			}
			return outcome, nil
		}
		if !isMsgNotFound(getErr) {
			return "", fmt.Errorf("read terminal for loop %s: %w", loopID, getErr)
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("loop %s did not settle within %s", loopID, s.config.CompleteTimeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// terminalOutcome reads the verdict off whichever settlement payload ended the
// loop. The three are separate registered types, so this is a type switch and
// not a field cast: an envelope that is not a settlement event is an error
// here, where an anonymous shape would have read a zero-valued outcome out of
// it and compared that to what the caller expected.
func terminalOutcome(payload message.Payload) (string, error) {
	var outcome string
	switch settled := payload.(type) {
	case *agentic.LoopCompletedEvent:
		outcome = settled.Outcome
	case *agentic.LoopFailedEvent:
		outcome = settled.Outcome
	case *agentic.LoopCancelledEvent:
		outcome = settled.Outcome
	default:
		return "", fmt.Errorf("terminal payload type %T is not a loop settlement event", payload)
	}
	if outcome == "" {
		return "", fmt.Errorf("terminal payload %T declares no outcome", payload)
	}
	return outcome, nil
}

// submitApproval answers a pending approval over the production HTTP seam,
// naming the execution it is answering — the endpoint refuses a body that does
// not, and refuses one that names an execution other than the pending gate.
//
// Every status but 200 is the answer and fails the walk; a 409 is not retried.
// The endpoint decides from the loop's AGENT_LOOPS record, and every caller
// has already read that record awaiting this gate before it answers — the
// walk's awaitLoopState, the replacement walk's parked records — so there is
// no window in which the record could still be catching up. Nor could the
// ApprovalPendingEvent outrun it: a gate is written before its event is
// published (persistHandlerResult keeps write-then-publish for an
// awaiting_approval result), and a re-echo is built from a gate already
// written. A 409 therefore means the record is not awaiting this gate, which
// across a replacement is a lost gate, not a race. reason is the free text a
// reviewer attaches; empty sends none.
func (s *Scenario) submitApproval(ctx context.Context, loopID, executionID, decision, reason string) error {
	status, body, err := s.postJSON(ctx,
		fmt.Sprintf("%s/loops/%s/approval", dispatchRoutePrefix, loopID),
		agenticdispatch.ApprovalRequest{
			Decision:    decision,
			ExecutionID: executionID,
			Reason:      reason,
			UserID:      approvalRequester,
		})
	if err != nil {
		return fmt.Errorf("post approval for loop %s: %w", loopID, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("approval status = %d, want 200 (body %s)", status, strings.TrimSpace(string(body)))
	}
	var accepted agenticdispatch.ApprovalAcceptResponse
	if err := json.Unmarshal(body, &accepted); err != nil {
		return fmt.Errorf("decode approval acceptance: %w", err)
	}
	if !accepted.Accepted || accepted.LoopID != loopID || accepted.Decision != decision {
		return fmt.Errorf("approval acceptance = %+v, want accepted %s for loop %s", accepted, decision, loopID)
	}
	// The acceptance names the execution it answered: a caller that cannot
	// see WHICH gate it just answered is back to inferring it from the
	// request it sent.
	if accepted.ExecutionID != executionID {
		return fmt.Errorf("approval acceptance execution_id = %q, want the execution the decision named (%q)",
			accepted.ExecutionID, executionID)
	}
	return nil
}

// chatCommand submits a slash command on the dispatch HTTP message endpoint and
// returns the synchronous typed response.
func (s *Scenario) chatCommand(ctx context.Context, userID, content string) (agenticdispatch.HTTPMessageResponse, error) {
	status, body, err := s.postJSON(ctx, dispatchRoutePrefix+"/message", agenticdispatch.HTTPMessageRequest{
		Content:     content,
		UserID:      userID,
		ChannelType: "e2e",
		ChannelID:   "e2e-command",
	})
	if err != nil {
		return agenticdispatch.HTTPMessageResponse{}, fmt.Errorf("post command %q: %w", content, err)
	}
	if status != http.StatusOK {
		return agenticdispatch.HTTPMessageResponse{}, fmt.Errorf("command %q status = %d (body %s)",
			content, status, strings.TrimSpace(string(body)))
	}
	var response agenticdispatch.HTTPMessageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return agenticdispatch.HTTPMessageResponse{}, fmt.Errorf("decode command response: %w", err)
	}
	return response, nil
}

// postJSON posts a JSON body to a dispatch route and returns the status and raw
// body. A refusal is an ANSWER here, not a transport failure, so a non-2xx
// status is returned rather than turned into an error.
func (s *Scenario) postJSON(ctx context.Context, path string, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal %s body: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.HTTPURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("post %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer := new(bytes.Buffer)
	if _, err := answer.ReadFrom(resp.Body); err != nil {
		return 0, nil, fmt.Errorf("read %s response: %w", path, err)
	}
	return resp.StatusCode, answer.Bytes(), nil
}
