package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/nats-io/nats.go/jetstream"
)

// This file is the approval lane's process-replacement proof (#1362 task 6.1,
// the last acceptance row of #1155): a loop parked on a human approval survives
// the death of the process that parked it, and the REPLACEMENT applies the
// human's answer.
//
// "Applies" is the whole claim, and three shapes can pass for it without being
// it:
//
//   - The answer is acknowledged and dropped. Before #1362 a replacement took
//     exactly this path (a loop it does not hold was a stale drop), and the
//     loop then sat in awaiting_approval forever. Caught by every assertion
//     below that reads an effect: the loop must reach agent.complete.
//   - A deadline decides instead of the human. The approval-timeout sweeper
//     auto-rejects, and an auto-rejected loop also completes. Caught twice: the
//     approved loop must EXECUTE the gated tool, which no rejection does, and
//     the rejected loop's next request must carry the rejection this stage
//     wrote — its approver and its reason — not the sweeper's.
//   - The answer is applied twice, or the placeholder leaks. Caught by the
//     exact counts: one successful execution across both loops, exactly two
//     requests per loop, and exactly one tool message for the gated call.
//
// Two loops are parked and ONE replacement serves both: approve and reject are
// the two ways the approval lane applies an answer (a re-dispatched call, and
// a synthesized result that mints the next request), and they exercise
// different branches of the carrier. Modify shares approve's branch
// (dispatchApprovedCall with other arguments) and is not walked separately.
//
// What the replacement holds when the answer arrives is ARRANGED, not raced:
// every loop-lane consumer, the model consumer and the tools consumer are
// quiescent before the kill, so no redelivery can seat either loop warm on the
// replacement ahead of the answer. The record's revision is re-read after the
// replacement to show nothing touched it. The answer therefore reaches the cold
// branch (settleApprovalResponseWithoutLoop): step 0 reads the record and finds
// no newer retained request, the gate matches, I4 holds, the loop is rebuilt
// from the retained request and response, and the answer is applied warm on the
// rebuilt loop. That inference holds only while no startup pass re-hydrates
// parked loops; see verifyApprovalAcrossReplacement.

const (
	// approvalRestartRejectReason is the reason the rejected loop's answer
	// carries. It is what tells this stage's rejection apart from the
	// approval-timeout sweeper's, which stamps its own approver and reason.
	approvalRestartRejectReason = "e2e approval-restart: rejected across a process replacement"

	// loopLaneOwner, modelLaneOwner and toolsLaneOwner are the durable-consumer
	// name prefixes of the components whose deliveries could reach a parked
	// loop. Each component names its consumers in its own setupConsumer:
	// fmt.Sprintf("agentic-loop-%s", ...), fmt.Sprintf("agentic-model-%s", ...)
	// and fmt.Sprintf("agentic-tools-%s", ...).
	loopLaneOwner  = "agentic-loop-"
	modelLaneOwner = "agentic-model-"
	toolsLaneOwner = "agentic-tools-"

	// approvalRestartSettleTimeout bounds each wait for consumer quiescence.
	approvalRestartSettleTimeout = 30 * time.Second

	// approvalRestartDeadlineMargin is the least time the loop deadline and the
	// approval deadline must each still have left once the replacement is up.
	// It is several times the whole stage's measured duration (6.2s in the
	// task e2e:agentic run at aad9627d), so a slow host still finishes the
	// answer path before either deadline could fire.
	approvalRestartDeadlineMargin = 30 * time.Second

	// approvalAppliedTimeout bounds the wait for each answered loop's terminal.
	approvalAppliedTimeout = 90 * time.Second
)

// approvalRestartLanes are the consumers whose deliveries could seat a parked
// loop on the replacement ahead of its answer. The set is enumerated by hand,
// from the loop's input-port routing (the port switch in agentic-loop's
// consumer setup, processor/agentic-loop/component.go) and from the upstream
// components whose in-flight work becomes one of those inputs:
//
//   - agent.task, agent.response and tool.result: a delivery for a loop the
//     process does not hold seats it.
//   - agent.approval_response: the answer lane itself. It must be quiet before
//     the kill so the only answer the replacement sees is the one this stage
//     sends.
//   - agentic-model over agent.request: an in-flight request becomes an
//     agent.response.
//   - agentic-tools over tool.execute: an in-flight call becomes a tool.result.
//
// agent.signal and agent.toolcall.approved / agent.toolcall.rejected are left
// out. For a loop the process does not hold they settle against the record
// (settleUncancellableLoop, settleVerdictWithoutWaiter) and never seat one.
//
// Each lane is found by owner and filter, never by a guessed name, and each
// must resolve to exactly one consumer. A lane that matched nothing would make
// the quiescence wait pass vacuously.
var approvalRestartLanes = []consumerLane{
	{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.task"},
	{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.response"},
	{stream: agentStream, owner: loopLaneOwner, subjectRoot: "agent.approval_response"},
	{stream: toolStream, owner: loopLaneOwner, subjectRoot: "tool.result"},
	{stream: agentStream, owner: modelLaneOwner, subjectRoot: "agent.request"},
	{stream: toolStream, owner: toolsLaneOwner, subjectRoot: "tool.execute"},
}

// consumerLane names one durable consumer by the stream it reads, the
// component that owns it, and the subject root its filter covers.
type consumerLane struct {
	stream      string
	owner       string
	subjectRoot string
}

// approvalAnswer is what the human decided. The three values travel together.
type approvalAnswer struct {
	decision string
	approver string
	reason   string
}

// parkedApprovalLoop is one loop gated on a human approval, as observed before
// the process that gated it is killed.
type parkedApprovalLoop struct {
	label  string
	task   agentic.TaskMessage
	gate   agentic.PendingApprovalState
	parked loopRecordObservation
	answer approvalAnswer
	// submitted is the stage host's reading, monotonic, taken just before the
	// task was published. It anchors deadlineClock.
	submitted time.Time
}

// deadlineClock estimates the current reading of the clock the loop's
// deadlines are on.
//
// The loop stamps TimeoutAt and the gate's RequestedAt with the SemStreams
// container's clock. The stage host's wall clock can be skewed from it (Docker
// Desktop runs the containers in a VM), so it is never compared against them
// directly. Instead the estimate starts from the parked revision's commit
// time, which the NATS server stamped on the same container clock, and adds
// the host's monotonic time elapsed since the task was submitted. Submission
// came before that commit, so the estimate runs ahead of the true reading by
// the time the loop took to park, and every budget check errs toward refusing.
// The one premise is that the NATS and SemStreams containers read the same
// clock, which containers on one Docker host or VM do.
func (p parkedApprovalLoop) deadlineClock() time.Time {
	return p.parked.committed.Add(time.Since(p.submitted))
}

// firstRequestID and nextRequestID are the loop's first request and the one
// its answered tool round mints (the same derivation the mid-flight check in
// stage A reads).
func (p parkedApprovalLoop) firstRequestID() string { return p.task.LoopID + ":req:1:0" }
func (p parkedApprovalLoop) nextRequestID() string  { return p.task.LoopID + ":req:2:0" }

// loopRevision is one committed revision of a loop record, with the server
// time it was committed at. The time is what orders it against a stream
// publication: both are stamped by the same NATS server.
type loopRevision struct {
	entity    agentic.LoopEntity
	revision  uint64
	committed time.Time
}

// verifyApprovalAcrossReplacement parks two loops on approval, replaces the
// SemStreams process, answers both over the production HTTP seam, and asserts
// the replacement applied each answer.
//
// Assumption (owner ruling, #1362 issuecomment-5812283590): this stage proves
// the cold branch (settleApprovalResponseWithoutLoop) only while no startup
// re-hydration of parked loops from AGENT_LOOPS exists. It does not observe
// the branch run; it infers it from quiesced lanes and an unchanged record
// revision. That re-hydration is deferred as OQ2 (the doc comment on
// runApprovalTimeoutSweeper, processor/agentic-loop/approval_sweeper.go). If
// OQ2 lands, the answer takes the warm path, this stage stays green without
// proving #1155, and it must be revisited.
func (s *Scenario) verifyApprovalAcrossReplacement(
	ctx context.Context, result *scenarios.Result,
) (runErr error) {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream: %w", err)
	}
	agent, err := js.Stream(ctx, agentStream)
	if err != nil {
		return fmt.Errorf("open %s stream: %w", agentStream, err)
	}
	tools, err := js.Stream(ctx, toolStream)
	if err != nil {
		return fmt.Errorf("open %s stream: %w", toolStream, err)
	}
	loops, err := js.KeyValue(ctx, agentLoopsBucket)
	if err != nil {
		return fmt.Errorf("open %s bucket: %w", agentLoopsBucket, err)
	}

	approve, err := s.parkApprovalLoop(ctx, loops, "approve", approvalAnswer{
		decision: agentic.ApprovalDecisionApprove, approver: approvalRequester,
	})
	if err != nil {
		return err
	}
	reject, err := s.parkApprovalLoop(ctx, loops, "reject", approvalAnswer{
		decision: agentic.ApprovalDecisionReject, approver: approvalRequester, reason: approvalRestartRejectReason,
	})
	if err != nil {
		return err
	}
	parked := []*parkedApprovalLoop{approve, reject}

	// Nothing may be in flight for either loop when the process dies: a
	// redelivered task, response, or gated tool result would reach the
	// replacement first and the answer would not take the cold branch.
	if err := waitForLanesQuiescent(ctx, js, approvalRestartLanes, approvalRestartSettleTimeout); err != nil {
		return fmt.Errorf("the parked loops' deliveries never settled, so the replacement could rebuild them "+
			"before the answer arrives: %w", err)
	}

	if err := s.replaceSemStreams(ctx, newComposeProcessController(s.config.ComposeFile)); err != nil {
		return err
	}
	if err := s.assertParkedAcrossReplacement(ctx, loops, parked); err != nil {
		return err
	}

	gatedSuccess := map[string]string{"tool_name": approvalGatedTool, "status": toolExecutionSucceeded}
	executed, err := s.metricWithLabels(ctx, toolExecutionsMetric, gatedSuccess)
	if err != nil {
		return fmt.Errorf("read gated tool execution baseline on the replacement: %w", err)
	}
	if executed != 0 {
		return fmt.Errorf("%s%v = %v on the replacement before any answer; the gated tool ran without one",
			toolExecutionsMetric, gatedSuccess, executed)
	}

	// Watch each record from before its answer, so the first revision the
	// answer commits is observed even though later revisions overwrite it.
	watchers := make(map[string]jetstream.KeyWatcher, len(parked))
	defer func() {
		for _, watcher := range watchers {
			if err := watcher.Stop(); err != nil && runErr == nil {
				runErr = fmt.Errorf("stop loop record watcher: %w", err)
			}
		}
	}()
	for _, loop := range parked {
		watcher, err := loops.Watch(ctx, loop.task.LoopID)
		if err != nil {
			return fmt.Errorf("watch %s loop record: %w", loop.label, err)
		}
		watchers[loop.task.LoopID] = watcher
	}

	for _, loop := range parked {
		if err := s.submitApproval(ctx, loop.task.LoopID, loop.gate.ExecutionID,
			loop.answer.decision, loop.answer.reason); err != nil {
			return fmt.Errorf("answer the %s loop on the replacement: %w", loop.label, err)
		}
	}

	for _, loop := range parked {
		if err := s.awaitAnswerApplied(ctx, agent, loop); err != nil {
			return err
		}
	}
	// Quiescence again, now as the settlement proof: an answer retried toward
	// MaxDeliver or quarantined leaves a delivery pending on its lane.
	if err := waitForLanesQuiescent(ctx, js, approvalRestartLanes, approvalRestartSettleTimeout); err != nil {
		return fmt.Errorf("the replacement did not settle the answered loops' deliveries: %w", err)
	}

	for _, loop := range parked {
		revisions, err := s.verifyAppliedRecord(ctx, loops, watchers[loop.task.LoopID], loop)
		if err != nil {
			return err
		}
		if err := s.verifyAppliedRequests(ctx, agent, loop, revisions); err != nil {
			return err
		}
		if loop.answer.decision == agentic.ApprovalDecisionApprove {
			if err := s.verifyApprovedDispatch(ctx, tools, loop, revisions); err != nil {
				return err
			}
		}
	}

	if err := s.verifyReplacementCounters(ctx, gatedSuccess, approve); err != nil {
		return err
	}

	for _, loop := range parked {
		outcome, err := s.awaitTerminalOutcome(ctx, loop.task.LoopID)
		if err != nil {
			return err
		}
		if outcome != agentic.OutcomeSuccess {
			return fmt.Errorf("%s loop outcome = %q, want %q", loop.label, outcome, agentic.OutcomeSuccess)
		}
		result.Details["approval_restart_"+loop.label+"_loop_id"] = loop.task.LoopID
		result.Details["approval_restart_"+loop.label+"_outcome"] = outcome
	}
	return nil
}

// verifyReplacementCounters reads the replacement's own counters once both
// answers have settled. They started at zero with the replacement process, so
// every count here is the replacement's work.
func (s *Scenario) verifyReplacementCounters(
	ctx context.Context, gatedSuccess map[string]string, approved *parkedApprovalLoop,
) error {
	// Exactly one: the approved call ran once, and the rejected call not at all.
	if err := s.waitMetricWithLabels(ctx, toolExecutionsMetric, gatedSuccess, 1, 15*time.Second); err != nil {
		return fmt.Errorf("the approved call did not execute successfully on the replacement: %w", err)
	}
	executed, err := s.metricWithLabels(ctx, toolExecutionsMetric, gatedSuccess)
	if err != nil {
		return fmt.Errorf("read gated tool executions: %w", err)
	}
	if executed != 1 {
		return fmt.Errorf("%s%v = %v on the replacement, want exactly 1 (the approved call once, the rejected "+
			"call never)", toolExecutionsMetric, gatedSuccess, executed)
	}
	inapplicable := map[string]string{"reason": "approval_inapplicable"}
	dropped, err := s.metricWithLabels(ctx, toolResultsDroppedMetric, inapplicable)
	if err != nil {
		return fmt.Errorf("read inapplicable approval answers: %w", err)
	}
	if dropped != 0 {
		return fmt.Errorf("%s%v = %v: the replacement acknowledged an answer without applying it",
			toolResultsDroppedMetric, inapplicable, dropped)
	}
	if err := s.proveInapplicableSeriesLive(ctx, approved, inapplicable); err != nil {
		return err
	}
	if err := s.waitForComponentHealth(ctx, "agentic-loop", true, 10*time.Second); err != nil {
		return fmt.Errorf("agentic-loop did not stay healthy across the approval recovery: %w", err)
	}
	return nil
}

// toolResultsDroppedMetric is the loop-side series an approval answer moves
// when it is acknowledged without effect (reason="approval_inapplicable").
const toolResultsDroppedMetric = "semstreams_agentic_loop_tool_results_dropped_total"

// proveInapplicableSeriesLive makes the zero read before it evidence.
//
// A series nothing has moved and a series under a name nobody exports read the
// same: absent, which metricWithLabels reports as zero. Checking that the
// metric family exists does not tell them apart either, because a counter
// vector exports no family at all until one of its label sets is first
// incremented. So the series is made to move. One more answer is published for
// the approved loop, which is complete by now. The replacement must
// acknowledge it as inapplicable, and the series must read exactly 1.
//
// It is published straight onto the answer lane, not through the dispatch
// endpoint, because the endpoint refuses a loop whose record is not awaiting
// approval and never publishes.
func (s *Scenario) proveInapplicableSeriesLive(
	ctx context.Context, approved *parkedApprovalLoop, inapplicable map[string]string,
) error {
	late := agentic.ApprovalResponse{
		LoopID:      approved.task.LoopID,
		CallID:      approved.gate.CallID,
		ExecutionID: approved.gate.ExecutionID,
		RequestID:   approved.gate.RequestID,
		Decision:    agentic.ApprovalDecisionApprove,
		ApprovedBy:  approvalRequester,
		Reason:      "e2e approval-restart: a late answer for a completed loop",
		DecidedAt:   time.Now().UTC(),
	}
	envelope := message.NewBaseMessage(late.Schema(), &late, "e2e-test")
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal the late answer: %w", err)
	}
	if err := s.nats.Publish(ctx, "agent.approval_response."+late.LoopID, data); err != nil {
		return fmt.Errorf("publish the late answer: %w", err)
	}
	if err := s.waitMetricWithLabels(ctx, toolResultsDroppedMetric, inapplicable, 1, 15*time.Second); err != nil {
		return fmt.Errorf("a late answer for the completed %s loop never moved %s%v, so its zero reading "+
			"before the late answer is no evidence: %w", approved.label, toolResultsDroppedMetric, inapplicable, err)
	}
	dropped, err := s.metricWithLabels(ctx, toolResultsDroppedMetric, inapplicable)
	if err != nil {
		return fmt.Errorf("read inapplicable approval answers after the late answer: %w", err)
	}
	if dropped != 1 {
		return fmt.Errorf("%s%v = %v after one late answer, want exactly 1", toolResultsDroppedMetric,
			inapplicable, dropped)
	}
	return nil
}

// parkApprovalLoop submits one approval-gated task and waits until its record
// is durably awaiting approval with a gate that matches the published event.
func (s *Scenario) parkApprovalLoop(
	ctx context.Context, loops jetstream.KeyValue, label string, answer approvalAnswer,
) (*parkedApprovalLoop, error) {
	submitted := time.Now()
	task := newApprovalGatedTask(submitted, "approval-restart-"+label, approvalLoopOwner)
	if err := s.publishTask(ctx, "agent.task.e2e-approval-restart", task); err != nil {
		return nil, err
	}
	pending, err := s.awaitApprovalPending(ctx, task.LoopID)
	if err != nil {
		return nil, fmt.Errorf("park the %s loop: %w", label, err)
	}
	observed, err := waitForLoopRecord(ctx, loops, task.LoopID, s.config.TaskTimeout, func(entity agentic.LoopEntity) bool {
		return entity.State == agentic.LoopStateAwaitingApproval && entity.PendingApproval != nil
	})
	if err != nil {
		return nil, fmt.Errorf("the %s loop's record never reached awaiting_approval: %w", label, err)
	}
	gate := *observed.entity.PendingApproval
	if gate.ExecutionID == "" || gate.CallID != pending.CallID || gate.ToolName != approvalGatedTool {
		return nil, fmt.Errorf("the %s loop's gate = call:%q execution:%q tool:%q, want the published call %q "+
			"on %s with an execution identity", label, gate.CallID, gate.ExecutionID, gate.ToolName,
			pending.CallID, approvalGatedTool)
	}
	loop := &parkedApprovalLoop{
		label: label, task: task, gate: gate, parked: observed, answer: answer, submitted: submitted,
	}
	if gate.RequestID != loop.firstRequestID() {
		return nil, fmt.Errorf("the %s loop's gate names request %q, want its first request %q",
			label, gate.RequestID, loop.firstRequestID())
	}
	return loop, nil
}

// assertParkedAcrossReplacement reads each record after the replacement is
// healthy and requires it UNTOUCHED — same revision, still awaiting — and every
// deadline that could decide instead of the human still a margin ahead. A
// replacement that rewrote the record, or a stage that arrives at a deadline,
// would be proving something other than the answer being applied.
func (s *Scenario) assertParkedAcrossReplacement(
	ctx context.Context, loops jetstream.KeyValue, parked []*parkedApprovalLoop,
) error {
	for _, loop := range parked {
		observed, err := waitForLoopRecord(ctx, loops, loop.task.LoopID, 10*time.Second,
			func(agentic.LoopEntity) bool { return true })
		if err != nil {
			return fmt.Errorf("read the %s loop's record after the replacement: %w", loop.label, err)
		}
		if observed.revision != loop.parked.revision || observed.entity.State != agentic.LoopStateAwaitingApproval {
			return fmt.Errorf("the %s loop's record moved across the replacement before any answer: revision %d -> %d, "+
				"state %s", loop.label, loop.parked.revision, observed.revision, observed.entity.State)
		}
		if err := deadlinesAhead(observed.entity, loop.deadlineClock()); err != nil {
			return fmt.Errorf("the %s loop: %w", loop.label, err)
		}
	}
	return nil
}

// loopDeadline is one deadline that settles a parked loop without the human,
// with the configuration key that sets it. The loop deadline fails the loop on
// its first delivery after a rebuild, and the approval deadline auto-rejects it.
type loopDeadline struct {
	name      string
	configKey string
	at        time.Time
}

// parkedDeadlines lists the deadlines a parked record carries. A zero
// timeout_at, and a gate with no timeout, set none.
func parkedDeadlines(entity agentic.LoopEntity) []loopDeadline {
	var deadlines []loopDeadline
	if !entity.TimeoutAt.IsZero() {
		deadlines = append(deadlines, loopDeadline{
			name: "loop deadline", configKey: "agentic-loop.timeout", at: entity.TimeoutAt,
		})
	}
	if gate := entity.PendingApproval; gate != nil && gate.Timeout > 0 {
		deadlines = append(deadlines, loopDeadline{
			name: "approval deadline", configKey: "agentic-loop.approval_timeout", at: gate.RequestedAt.Add(gate.Timeout),
		})
	}
	return deadlines
}

// deadlinesAhead refuses a parked loop whose loop deadline or approval
// deadline has less than approvalRestartDeadlineMargin left at now, on the
// deadlines' own clock (deadlineClock). A deadline that is merely still ahead
// is not enough: one that fires while the answer is being applied settles the
// loop without the human, and the waits after it would blame the recovery. The
// error names the budget to raise.
func deadlinesAhead(entity agentic.LoopEntity, now time.Time) error {
	for _, deadline := range parkedDeadlines(entity) {
		if left := deadline.at.Sub(now); left < approvalRestartDeadlineMargin {
			return fmt.Errorf("the %s has %s left once the replacement is up (at %s, now %s), under the %s margin "+
				"the answer needs; raise %s in configs/agentic.json above the replacement window plus that margin",
				deadline.name, left.Round(time.Millisecond), deadline.at.UTC().Format(time.RFC3339),
				now.UTC().Format(time.RFC3339), approvalRestartDeadlineMargin, deadline.configKey)
		}
	}
	return nil
}

// budgetLeft names what each deadline has left at now, and reports whether
// any has run out. Every wait error after the answer carries it, so that a
// deadline exhausted during the wait reads as the stage's budget and never as
// an answer the replacement dropped.
func budgetLeft(entity agentic.LoopEntity, now time.Time) (string, bool) {
	var parts []string
	spent := false
	for _, deadline := range parkedDeadlines(entity) {
		left := deadline.at.Sub(now).Round(time.Millisecond)
		if left <= 0 {
			spent = true
			parts = append(parts, fmt.Sprintf("%s (%s) passed %s ago", deadline.name, deadline.configKey, -left))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s) %s left", deadline.name, deadline.configKey, left))
	}
	if len(parts) == 0 {
		return "no deadline set", false
	}
	return strings.Join(parts, ", "), spent
}

// awaitAnswerApplied waits for the loop's agent.complete. Every failure names
// the loop's deadline budget. A budget that ran out during the wait is reported
// as that. A loop that settles on agent.failed with budget left was rebuilt
// and then failed, for example with continuation_unavailable, which is not the
// answer applied.
func (s *Scenario) awaitAnswerApplied(ctx context.Context, agent jetstream.Stream, loop *parkedApprovalLoop) error {
	completeSubject := "agent.complete." + loop.task.LoopID
	if err := waitForStreamSubject(ctx, agent, completeSubject, approvalAppliedTimeout); err != nil {
		budget, spent := budgetLeft(loop.parked.entity, loop.deadlineClock())
		if spent {
			return fmt.Errorf("the %s loop's deadline budget ran out before agent.complete (%s): that is the "+
				"stage's budget, not evidence about the answer: %w", loop.label, budget, err)
		}
		failed, failedErr := streamSubjectCount(ctx, agent, "agent.failed."+loop.task.LoopID)
		if failedErr == nil && failed > 0 {
			return fmt.Errorf("the replacement settled the %s loop on agent.failed, not agent.complete, with "+
				"deadline budget left (%s): the answer was not applied: %w", loop.label, budget, err)
		}
		return fmt.Errorf("the replacement did not carry the %s loop to a terminal within %v of its answer "+
			"(deadline budget: %s): %w", loop.label, approvalAppliedTimeout, budget, err)
	}
	return nil
}

// verifyAppliedRecord reads the loop's terminal record and every revision the
// watcher saw up to it. The terminal record must name the request the answered
// round minted and no longer carry the gate.
func (s *Scenario) verifyAppliedRecord(
	ctx context.Context, loops jetstream.KeyValue, watcher jetstream.KeyWatcher, loop *parkedApprovalLoop,
) ([]loopRevision, error) {
	final, err := waitForLoopRecord(ctx, loops, loop.task.LoopID, 30*time.Second, func(entity agentic.LoopEntity) bool {
		return entity.State == agentic.LoopStateComplete
	})
	if err != nil {
		return nil, fmt.Errorf("the %s loop's record did not reach %s: %w", loop.label, agentic.LoopStateComplete, err)
	}
	if final.entity.PublishedRequestID != loop.nextRequestID() {
		return nil, fmt.Errorf("the %s loop's terminal record names request %q, want %q — the answered round did "+
			"not advance it", loop.label, final.entity.PublishedRequestID, loop.nextRequestID())
	}
	if final.entity.PendingApproval != nil {
		return nil, fmt.Errorf("the %s loop's terminal record still carries its approval gate (execution %q)",
			loop.label, final.entity.PendingApproval.ExecutionID)
	}
	if final.revision <= loop.parked.revision {
		return nil, fmt.Errorf("the %s loop's record revision did not move: %d -> %d",
			loop.label, loop.parked.revision, final.revision)
	}
	revisions, err := collectLoopRevisions(ctx, watcher, final.revision, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("the %s loop's record history: %w", loop.label, err)
	}
	retained, err := loops.History(ctx, loop.task.LoopID)
	if err != nil {
		return nil, fmt.Errorf("read the %s loop's retained record history: %w", loop.label, err)
	}
	retainedRevisions := make([]uint64, 0, len(retained))
	for _, entry := range retained {
		retainedRevisions = append(retainedRevisions, entry.Revision())
	}
	if err := watchSkippedNothing(revisions, retainedRevisions, loop.parked.revision, final.revision); err != nil {
		return nil, fmt.Errorf("the %s loop's watched record history: %w", loop.label, err)
	}
	return revisions, nil
}

// watchSkippedNothing proves the watcher delivered every revision of the key
// from the parked one through the terminal one.
//
// Revisions are sequence numbers of the whole bucket's stream, so one key's
// revisions are not consecutive integers and cannot be checked for gaps on
// their own. The key's retained history is the list to match: the loops bucket
// keeps ten revisions per key (loopbucket.AcquireOwner), which covers an
// answered round. The watch must begin at the parked revision, the retained
// history must still reach back to it, and the watched revisions must equal
// the retained ones in [parked, through]. A delete or purge in that window is
// retained as a revision the watch refuses (collectLoopRevisions), so it
// cannot pass either.
func watchSkippedNothing(watched []loopRevision, retained []uint64, parked, through uint64) error {
	if len(watched) == 0 || watched[0].revision != parked {
		first := "nothing"
		if len(watched) > 0 {
			first = fmt.Sprintf("revision %d", watched[0].revision)
		}
		return fmt.Errorf("the watch began at %s, not the parked revision %d", first, parked)
	}
	if len(retained) == 0 || retained[0] > parked {
		return fmt.Errorf("the retained history %v no longer reaches the parked revision %d, so a skip cannot be "+
			"ruled out", retained, parked)
	}
	var window []uint64
	for _, revision := range retained {
		if revision >= parked && revision <= through {
			window = append(window, revision)
		}
	}
	seen := make([]uint64, 0, len(watched))
	for _, rev := range watched {
		seen = append(seen, rev.revision)
	}
	if !slices.Equal(seen, window) {
		return fmt.Errorf("the watch delivered revisions %v, but the key retains %v between the parked revision %d "+
			"and the terminal %d", seen, window, parked, through)
	}
	return nil
}

// verifyAppliedRequests reads what the answered round asked the model. The
// loop must have published exactly one request after the retained first one,
// published it BEFORE the record named it, and that request must carry the
// conversation rebuilt from retained evidence plus the answer's effect.
func (s *Scenario) verifyAppliedRequests(
	ctx context.Context, agent jetstream.Stream, loop *parkedApprovalLoop, revisions []loopRevision,
) error {
	subject := "agent.request." + loop.task.LoopID
	requests, err := streamSubjectCount(ctx, agent, subject)
	if err != nil {
		return fmt.Errorf("count the %s loop's requests: %w", loop.label, err)
	}
	if requests != 2 {
		return fmt.Errorf("requests on %s = %d, want exactly 2 (the retained first and the one the answer "+
			"led to)", subject, requests)
	}
	stored, err := agent.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		return fmt.Errorf("read the %s loop's next request: %w", loop.label, err)
	}
	decoded, err := s.decoder.Decode(stored.Data)
	if err != nil {
		return fmt.Errorf("decode the %s loop's next request through the production registry: %w", loop.label, err)
	}
	request, ok := decoded.Payload().(*agentic.AgentRequest)
	if !ok {
		return fmt.Errorf("the %s loop's request payload type = %T, want *agentic.AgentRequest", loop.label, decoded.Payload())
	}
	if err := checkAppliedAnswer(request, loop.nextRequestID(), loop.task.Prompt, loop.gate, loop.answer); err != nil {
		return fmt.Errorf("the %s loop: %w", loop.label, err)
	}
	// The next request goes out before the record names it: publish, then
	// write. For the rejected loop this is the approval lane's own order
	// (#1362 task 1.4); for the approved loop it is the tool-result lane's.
	if err := publishedBeforeWritten(stored.Time, revisions, func(entity agentic.LoopEntity) bool {
		return entity.PublishedRequestID == loop.nextRequestID()
	}); err != nil {
		return fmt.Errorf("the %s loop's request %s: %w", loop.label, loop.nextRequestID(), err)
	}
	return nil
}

// verifyApprovedDispatch reads the call the replacement re-dispatched for the
// approved loop: it must carry the human's approval and the gated execution
// identity, and it must be on the stream before the record released the gate —
// the approval lane publishes, then writes (#1362 task 1.4).
func (s *Scenario) verifyApprovedDispatch(
	ctx context.Context, tools jetstream.Stream, loop *parkedApprovalLoop, revisions []loopRevision,
) error {
	// Nothing else dispatches the gated tool once both answers are in: the
	// rejected loop dispatches nothing, and the approved loop's second round is
	// a completion. So the last call on the subject is the approved one, and
	// the identity check below refuses it if it is not.
	subject := "tool.execute." + approvalGatedTool
	stored, err := tools.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		return fmt.Errorf("read the approved call on %s: %w", subject, err)
	}
	decoded, err := s.decoder.Decode(stored.Data)
	if err != nil {
		return fmt.Errorf("decode the approved call through the production registry: %w", err)
	}
	call, ok := decoded.Payload().(*agentic.ToolCall)
	if !ok {
		return fmt.Errorf("approved call payload type = %T, want *agentic.ToolCall", decoded.Payload())
	}
	if call.ExecutionID != loop.gate.ExecutionID || call.ID != loop.gate.CallID {
		return fmt.Errorf("the last call on %s = call:%q execution:%q, want the gated call %q execution %q",
			subject, call.ID, call.ExecutionID, loop.gate.CallID, loop.gate.ExecutionID)
	}
	if call.ApprovedBy != loop.answer.approver {
		return fmt.Errorf("the approved call carries approved_by %q, want %q", call.ApprovedBy, loop.answer.approver)
	}
	if err := publishedBeforeWritten(stored.Time, revisions, func(entity agentic.LoopEntity) bool {
		return entity.State != agentic.LoopStateAwaitingApproval
	}); err != nil {
		return fmt.Errorf("the approved call: %w", err)
	}
	return nil
}

// checkAppliedAnswer asserts that the request the answered round published
// carries the answer's effect on a conversation the replacement rebuilt.
//
// The rebuilt conversation is the task prompt and the assistant turn that made
// the gated call — the replacement never held either in memory, so both came
// from the retained request and response. Each appears exactly once: a second
// copy of either is a rebuild that seated the retained turns twice. Exactly
// one tool message answers the gated call: the executed result for an
// approval, the human's rejection for a reject. A second one would be the
// gate's placeholder leaking beside the answer.
func checkAppliedAnswer(
	request *agentic.AgentRequest,
	wantRequestID, prompt string,
	gate agentic.PendingApprovalState,
	answer approvalAnswer,
) error {
	if request.RequestID != wantRequestID {
		return fmt.Errorf("next request id = %q, want %q", request.RequestID, wantRequestID)
	}
	var prompts, calls int
	var answers []agentic.ChatMessage
	for _, msg := range request.Messages {
		switch msg.Role {
		case "user":
			if strings.Contains(msg.Content, prompt) {
				prompts++
			}
		case "assistant":
			if slices.ContainsFunc(msg.ToolCalls, func(call agentic.ToolCall) bool { return call.ID == gate.CallID }) {
				calls++
			}
		case "tool":
			if msg.ToolCallID == gate.CallID {
				answers = append(answers, msg)
			}
		}
	}
	if prompts != 1 {
		return fmt.Errorf("request %s carries the task prompt in %d user messages, want exactly 1 (%s)",
			wantRequestID, prompts, rebuildFault(prompts))
	}
	if calls != 1 {
		return fmt.Errorf("request %s carries %d assistant turns calling %q, want exactly 1 (%s)",
			wantRequestID, calls, gate.CallID, rebuildFault(calls))
	}
	if len(answers) != 1 {
		return fmt.Errorf("request %s carries %d tool messages for call %q, want exactly 1",
			wantRequestID, len(answers), gate.CallID)
	}
	got := answers[0]
	switch answer.decision {
	case agentic.ApprovalDecisionApprove:
		if got.IsError {
			return fmt.Errorf("the approved call's tool message is an error (%q): the call was not executed "+
				"— a rejection or the gate's placeholder reached the model instead", got.Content)
		}
	case agentic.ApprovalDecisionReject:
		want := agentic.ApprovalRejectedPrefix + "rejected by " + answer.approver + ": " + answer.reason
		if !got.IsError || !strings.Contains(got.Content, want) {
			return fmt.Errorf("the rejected call's tool message = error:%v %q, want an error carrying %q — "+
				"this answer's approver and reason, not another rejection's", got.IsError, got.Content, want)
		}
	default:
		return fmt.Errorf("unknown approval decision %q", answer.decision)
	}
	return nil
}

// rebuildFault names what a wrong count of a rebuilt turn means.
func rebuildFault(count int) string {
	if count == 0 {
		return "the conversation was not rebuilt"
	}
	return "the rebuilt turns were seated more than once"
}

// publishedBeforeWritten finds the first revision matching applied and
// requires it to have been committed no earlier than the publication it
// implies was stored. A record that names an effect before the effect is on
// the stream is the write-before-publish order.
func publishedBeforeWritten(published time.Time, revisions []loopRevision, applied func(agentic.LoopEntity) bool) error {
	for _, rev := range revisions {
		if !applied(rev.entity) {
			continue
		}
		if rev.committed.Before(published) {
			return fmt.Errorf("the record committed it at revision %d (%s) before the publication was stored (%s)",
				rev.revision, rev.committed.UTC().Format(time.RFC3339Nano), published.UTC().Format(time.RFC3339Nano))
		}
		return nil
	}
	return fmt.Errorf("no watched record revision reflects it (%d revisions seen)", len(revisions))
}

// collectLoopRevisions drains a record watcher until it has delivered the
// revision the caller already read, and returns every revision it saw. The
// watcher was opened before the answer, so this is the record's history from
// the parked revision on, including revisions later ones overwrote;
// watchSkippedNothing is what proves it complete. A delete or purge before the
// terminal revision is refused: nothing on the answer path removes the record.
func collectLoopRevisions(
	ctx context.Context, watcher jetstream.KeyWatcher, through uint64, timeout time.Duration,
) ([]loopRevision, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var revisions []loopRevision
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("watcher did not deliver revision %d within %v (%d revisions seen)",
				through, timeout, len(revisions))
		case entry, open := <-watcher.Updates():
			if !open {
				return nil, errors.New("record watcher closed before the terminal revision")
			}
			if entry == nil {
				continue // end of the initial values
			}
			if entry.Operation() != jetstream.KeyValuePut {
				return nil, fmt.Errorf("revision %d is a %s, not a write, before the terminal revision",
					entry.Revision(), entry.Operation())
			}
			var entity agentic.LoopEntity
			if err := json.Unmarshal(entry.Value(), &entity); err != nil {
				return nil, fmt.Errorf("decode revision %d: %w", entry.Revision(), err)
			}
			revisions = append(revisions, loopRevision{
				entity: entity, revision: entry.Revision(), committed: entry.Created(),
			})
			if entry.Revision() >= through {
				return revisions, nil
			}
		}
	}
}

// waitForLanesQuiescent waits until every named lane's consumer has nothing
// delivered-but-unsettled and nothing undelivered.
func waitForLanesQuiescent(
	ctx context.Context, js jetstream.JetStream, lanes []consumerLane, timeout time.Duration,
) error {
	for _, lane := range lanes {
		consumer, err := laneConsumer(ctx, js, lane)
		if err != nil {
			return err
		}
		if err := waitForConsumerQuiescent(ctx, consumer, timeout); err != nil {
			return fmt.Errorf("%s consumer over %s: %w", lane.owner, lane.subjectRoot, err)
		}
	}
	return nil
}

// laneConsumer opens the consumer laneConsumerName finds for the lane.
func laneConsumer(ctx context.Context, js jetstream.JetStream, lane consumerLane) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, lane.stream)
	if err != nil {
		return nil, fmt.Errorf("open %s stream: %w", lane.stream, err)
	}
	name, err := laneConsumerName(ctx, stream, lane)
	if err != nil {
		return nil, err
	}
	return stream.Consumer(ctx, name)
}

// laneConsumerName asks the server which consumer the lane's owner runs over
// the lane's subject root, on the lane's stream (the stream handle must be
// the one lane.stream names). Exactly one match is required: a lane that
// matched nothing would make a wait on it pass vacuously, and a second match
// would be picked at random.
func laneConsumerName(ctx context.Context, stream jetstream.Stream, lane consumerLane) (string, error) {
	lister := stream.ListConsumers(ctx)
	var matched []string
	for info := range lister.Info() {
		if strings.HasPrefix(info.Name, lane.owner) && filtersUnder(info.Config, lane.subjectRoot) {
			matched = append(matched, info.Name)
		}
	}
	if err := lister.Err(); err != nil {
		return "", fmt.Errorf("list %s consumers: %w", lane.stream, err)
	}
	if len(matched) != 1 {
		return "", fmt.Errorf("want exactly one %s* consumer on %s filtering %s, found %d: %v",
			lane.owner, lane.stream, lane.subjectRoot, len(matched), matched)
	}
	return matched[0], nil
}

// filtersUnder reports whether any of the consumer's filters sits at or below
// the subject root.
func filtersUnder(config jetstream.ConsumerConfig, root string) bool {
	filters := config.FilterSubjects
	if len(filters) == 0 && config.FilterSubject != "" {
		filters = []string{config.FilterSubject}
	}
	for _, filter := range filters {
		if filter == root || strings.HasPrefix(filter, root+".") {
			return true
		}
	}
	return false
}

// waitForConsumerQuiescent polls server state until the consumer holds no
// unacknowledged delivery and no undelivered message.
func waitForConsumerQuiescent(ctx context.Context, consumer jetstream.Consumer, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var ackPending int
	var pending uint64
	for time.Now().Before(deadline) {
		info, err := consumer.Info(ctx)
		if err == nil {
			ackPending, pending = info.NumAckPending, info.NumPending
			if ackPending == 0 && pending == 0 {
				return nil
			}
		}
		if err := waitDuration(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("%d delivered and unsettled, %d undelivered after %v", ackPending, pending, timeout)
}
