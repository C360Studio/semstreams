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

	// toolExecutionsMetric counts executor invocations by tool name. For
	// approvalGatedTool it can only be nonzero via an approved re-dispatch:
	// the approval filter refuses every un-approved call to it.
	toolExecutionsMetric = "semstreams_agentic_tools_executions_total"
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
		Prompt:      "List the temperature sensors on record. Use the query_by_type tool.",
		ChannelType: "e2e",
		ChannelID:   taskID,
		UserID:      userID,
		Tools: []agentic.ToolDefinition{{
			Name:        approvalGatedTool,
			Description: "Query all entities of a specific type with optional limit.",
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
	executionsBefore, err := s.metricWithLabels(ctx, toolExecutionsMetric,
		map[string]string{"tool_name": approvalGatedTool})
	if err != nil {
		return fmt.Errorf("read gated tool execution baseline: %w", err)
	}
	if executionsBefore != 0 {
		return fmt.Errorf("%s{tool_name=%q} = %v before any approval; the gated tool ran without one",
			toolExecutionsMetric, approvalGatedTool, executionsBefore)
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

	if err := s.submitApproval(ctx, task.LoopID, agentic.ApprovalDecisionApprove); err != nil {
		return err
	}
	if err := s.verifyApprovalResponsePublished(ctx, task.LoopID, pending.CallID); err != nil {
		return err
	}

	// The approved re-dispatch carries ApprovedBy, which is the only way a call
	// to this tool reaches an executor at all.
	if err := s.waitMetricWithLabels(ctx, toolExecutionsMetric,
		map[string]string{"tool_name": approvalGatedTool}, executionsBefore+1, 30*time.Second); err != nil {
		return fmt.Errorf("approved tool call did not execute: %w", err)
	}
	outcome, err := s.awaitTerminalOutcome(ctx, task.LoopID)
	if err != nil {
		return err
	}
	if outcome != agentic.OutcomeSuccess {
		return fmt.Errorf("approved loop outcome = %q, want %q", outcome, agentic.OutcomeSuccess)
	}

	result.Details["approval_call_id"] = pending.CallID
	result.Details["approval_outcome"] = outcome
	return nil
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
				UserID:   approvalRequester,
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
		before, err := s.metricWithLabels(ctx, loopAdmissionRefusalsMetric,
			map[string]string{"seam": refusal.seam, "reason": refusal.reason})
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
			map[string]string{"seam": refusal.seam, "reason": refusal.reason},
			before+1, 15*time.Second); err != nil {
			return fmt.Errorf("%s cancel refusal was not counted: %w", refusal.name, err)
		}
		result.Details["signal_refusal_"+refusal.name+"_reason"] = refusal.reason
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
func (s *Scenario) awaitApprovalPending(ctx context.Context, loopID string) (*agentic.ApprovalPendingEvent, error) {
	baseMsg, err := s.awaitStreamPayload(ctx, "agent.approval_pending."+loopID, s.config.CompleteTimeout)
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
			var terminal struct {
				Payload struct {
					Outcome string `json:"outcome"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(stored.Data, &terminal); err != nil {
				return "", fmt.Errorf("decode terminal for loop %s: %w", loopID, err)
			}
			if terminal.Payload.Outcome == "" {
				return "", fmt.Errorf("terminal for loop %s declares no outcome", loopID)
			}
			return terminal.Payload.Outcome, nil
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

// submitApproval answers a pending approval over the production HTTP seam.
func (s *Scenario) submitApproval(ctx context.Context, loopID, decision string) error {
	status, body, err := s.postJSON(ctx,
		fmt.Sprintf("%s/loops/%s/approval", dispatchRoutePrefix, loopID),
		agenticdispatch.ApprovalRequest{Decision: decision, UserID: approvalRequester})
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
