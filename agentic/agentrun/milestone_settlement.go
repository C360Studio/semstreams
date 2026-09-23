package agentrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// The two milestone lanes, as they appear in log lines and on the decisions
// counter. They are the durable consumer suffixes, so a counter series points
// straight at "agentrun-milestone-complete" or "agentrun-milestone-failed".
const (
	milestoneLaneComplete = "complete"
	milestoneLaneFailed   = "failed"
)

// The closed set of reasons a delivery did not acknowledge. Every non-Ack
// decision carries exactly one of these, on its log line and on the counter, so
// an operator reading the counter alone can tell a poison payload from an
// unbuilt composition from a handler that is merely not ready yet.
const (
	// reasonDecode — the stored bytes are not a terminal this build can read.
	reasonDecode = "decode"
	// reasonNotManaged — the run entity exists with no phase triple yet.
	reasonNotManaged = "not_managed"
	// reasonComposition — the agent-run workflow was never registered.
	reasonComposition = "composition"
	// reasonResolutionType — the lifecycle participant is not an *AgentRun.
	reasonResolutionType = "resolution_type"
	// reasonResolutionInvalid — resolution failed on a deterministic defect.
	reasonResolutionInvalid = "resolution_invalid"
	// reasonResolutionFatal — resolution failed on a Fatal-classified error.
	reasonResolutionFatal = "resolution_fatal"
	// reasonResolutionTransient — resolution failed on a repairable or
	// unclassifiable error, which is bounded by the lane's MaxDeliver.
	reasonResolutionTransient = "resolution_transient"
	// reasonHandlerInvalid — every handler that spoke rejected the input.
	reasonHandlerInvalid = "handler_invalid"
	// reasonHandlerTransient — at least one handler is not ready yet.
	reasonHandlerTransient = "handler_transient"
	// reasonHandlerFatal — a handler panicked or failed unprovably.
	reasonHandlerFatal = "handler_fatal"
)

// milestoneHeartbeatInterval renews the delivery lease while the fanout runs.
// It must stay at or below half the lane's AckWait; ValidateHeartbeatDeliveryPolicy
// refuses the policy at setup rather than letting a lease expire mid-fanout.
const milestoneHeartbeatInterval = 10 * time.Second

// milestoneRetryDelay is how long a semantic Retry waits before redelivery. A
// bare Nak would redeliver at line rate and burn the lane's finite MaxDeliver
// in milliseconds; 30s gives a forward reference or a not-yet-ready handler
// real time to resolve within the attempt budget.
const milestoneRetryDelay = 30 * time.Second

// milestoneOutcome ranks what one handler contributed to an attempt. The
// declaration order IS the precedence the aggregate applies: a higher rank wins.
type milestoneOutcome uint8

const (
	// outcomeDone — the handler committed its durable consequence, or had none.
	outcomeDone milestoneOutcome = iota
	// outcomeInvalid — the handler rejected this message permanently.
	outcomeInvalid
	// outcomeTransient — the handler could not act yet and wants the replay.
	outcomeTransient
	// outcomeFatal — the handler panicked or failed in a way nothing can place.
	outcomeFatal
)

// aggregateMilestoneOutcomes reduces one attempt's ordered outcome list to the
// attempt's own outcome: any fatal wins, else any transient, else any invalid,
// else done. It is a pure function of the list — membership decides, position
// never does — and an empty list (no registered handlers) is done.
//
// This is what makes the fanout one settlement unit: the only way to reach done
// is for EVERY handler to have returned nil on THIS attempt.
func aggregateMilestoneOutcomes(outcomes []milestoneOutcome) milestoneOutcome {
	worst := outcomeDone
	for _, outcome := range outcomes {
		if outcome > worst {
			worst = outcome
		}
	}
	return worst
}

// classifyHandlerOutcome ranks one handler return.
//
// Classification here is EXPLICIT: an errs class the handler itself set, or
// one of the two admitted cancellation sentinels. Nothing else places an
// error, and the error's WORDING never does. The framework defaults are the
// opposite — errs.Classify sends unknown errors to Transient, and both
// errs.IsTransient and errs.IsFatal reach a substring pass over the message
// text ("timeout", "connection", "network", ...) before giving up — which is
// right for infrastructure the framework owns and wrong for a product
// handler's durable effect. An error nothing can read is not proven safe to
// repeat and not proven safe to drop, so it is FATAL: the lane latches and an
// operator looks, instead of the milestone retrying to MaxDeliver and
// vanishing because its message happened to contain the word "timeout".
//
// context.Canceled and context.DeadlineExceeded are transient because they are
// the FANOUT's own shutdown or deadline surfacing through the handler, not a
// handler failure: the attempt never ran to completion, and quarantining on
// them would latch both lanes on every clean Stop. They are admitted by
// sentinel identity (errors.Is), never by text, and an explicit class outranks
// them — a handler that wraps its cancellation errs Fatal means Fatal.
//
// The uncoded errs sentinels (ErrRateLimited, ErrCircuitOpen,
// ErrConnectionTimeout, ...) are deliberately NOT admitted: they carry no
// class, no handler in this tree or above it returns one, and
// errs.WrapTransient is the one-line explicit spelling for a handler that
// wants the replay.
func classifyHandlerOutcome(err error) milestoneOutcome {
	if err == nil {
		return outcomeDone
	}
	var classified *semerrs.ClassifiedError
	if errors.As(err, &classified) {
		switch classified.Class {
		case semerrs.ErrorTransient:
			return outcomeTransient
		case semerrs.ErrorInvalid:
			return outcomeInvalid
		case semerrs.ErrorFatal:
			return outcomeFatal
		default:
			// A class this build cannot name is as unplaceable as no class.
			return outcomeFatal
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return outcomeTransient
	}
	return outcomeFatal
}

// errUnexpectedRunType marks a lifecycle participant that is not an *AgentRun.
// Every site wraps it Invalid, so the delivery terminates rather than retrying
// a registration defect forever, and it stays matchable so the decision can
// carry resolution_type instead of the generic invalid reason.
var errUnexpectedRunType = errors.New("lifecycle participant is not an *AgentRun")

// asAgentRun projects a resolved participant, or says why it could not.
func asAgentRun(participant lifecycle.Participant) (*AgentRun, error) {
	run, ok := participant.(*AgentRun)
	if !ok {
		return nil, semerrs.WrapInvalid(fmt.Errorf("%w: got %T", errUnexpectedRunType, participant),
			"agentrun", "ResolveRun", "project run participant")
	}
	return run, nil
}

// classifyResolutionFailure maps one run-resolution failure to its settlement
// decision and reason. Classification is done HERE, on the AgentRun side:
// pkg/lifecycle is Tier 1 and states these conditions as sentinels, and what a
// milestone delivery should do about each of them is this package's judgment.
//
// Sentinels are matched before errs.Classify because they carry no errs class
// at all — classifying them would send both to the unknown default.
func classifyResolutionFailure(err error) (natsclient.DeliveryDecision, string) {
	switch {
	case errors.Is(err, lifecycle.ErrWorkflowNotRegistered):
		// Process-wide, never per-message: WorkflowName is a package constant,
		// so this fires for every milestone on both lanes or for none. Terminate
		// would be a silent drop of every milestone the process ever sees;
		// Quarantine leaves them unacked and redeliverable once the composition
		// root is fixed, and reaches an operator through health.
		return natsclient.DeliveryDecisionQuarantine, reasonComposition
	case errors.Is(err, lifecycle.ErrEntityNotLifecycleManaged):
		// The ADR-049 question-5 forward reference: the entity exists but its
		// phase triple has not been written yet, and a later Manager.Create
		// resolves it. Bounded Retry self-heals; Terminate would drop a
		// documented-transient condition.
		return natsclient.DeliveryDecisionRetry, reasonNotManaged
	case errors.Is(err, errUnexpectedRunType):
		return natsclient.DeliveryDecisionTerminate, reasonResolutionType
	}
	switch semerrs.Classify(err) {
	case semerrs.ErrorInvalid:
		return natsclient.DeliveryDecisionTerminate, reasonResolutionInvalid
	case semerrs.ErrorFatal:
		return natsclient.DeliveryDecisionQuarantine, reasonResolutionFatal
	default:
		// Unknown resolves to Transient (errs.Classify's default): a bounded
		// Retry, so an unreadable graph costs at most the lane's MaxDeliver
		// attempts and then a counted exhaustion, never a silent drop.
		return natsclient.DeliveryDecisionRetry, reasonResolutionTransient
	}
}

// decisionLabel is the counter's bounded decision label.
func decisionLabel(decision natsclient.DeliveryDecision) string {
	switch decision {
	case natsclient.DeliveryDecisionAck:
		return "ack"
	case natsclient.DeliveryDecisionRetry:
		return "retry"
	case natsclient.DeliveryDecisionTerminate:
		return "terminate"
	case natsclient.DeliveryDecisionQuarantine:
		return "quarantine"
	default:
		// DeliveryDecisionInvalid and anything a later revision adds. Named
		// rather than dropped: an unlabelled increment is an invisible one.
		return "invalid"
	}
}

// deliveryWork returns the DeliveryWork one lane's HeartbeatDeliveryPolicy runs
// for every delivery. The observation lives here, inside the work, keyed on the
// decision the work returns — never on the DeliveryResult that comes back from
// Consume, because a refused delivery produces a zero result that ran no work
// at all and would read as a settlement failure.
func (s *MilestoneSubscriber) deliveryWork(lane string) natsclient.DeliveryWork {
	return func(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
		decided := s.decide(ctx, data)
		s.observeDecision(lane, decided)
		return decided.decision, decided.cause
	}
}

// observeDecision emits the one log line and the one counter increment a
// non-Ack decision owes. An acknowledged delivery is the ordinary case and says
// nothing: the milestone reached its handlers.
func (s *MilestoneSubscriber) observeDecision(lane string, decided milestoneDecision) {
	if decided.decision == natsclient.DeliveryDecisionAck {
		return
	}
	s.logger.Warn("agentrun: milestone delivery was not acknowledged",
		slog.String("source_message_id", decided.event.SourceMessageID),
		slog.String("loop_id", decided.event.LoopID),
		slog.String("category", decided.event.Category),
		slog.String("lane", lane),
		slog.String("reason", decided.reason),
		slog.Any("error", decided.cause))
	s.decisions.WithLabelValues(lane, decisionLabel(decided.decision), decided.reason).Inc()
}

// consumeLane is the NATS callback for one milestone lane: exactly one admitted
// delivery through the typed heartbeat path, with the lane's admission latch
// deciding whether any local work runs at all.
func (s *MilestoneSubscriber) consumeLane(
	lane string,
	policy natsclient.HeartbeatDeliveryPolicy,
	admission *deliverylane.Admission,
) func(context.Context, jetstream.Msg) {
	return func(msgCtx context.Context, msg jetstream.Msg) {
		result, admitted := deliverylane.Consume(msgCtx, msg, policy, admission)
		// Early return, not a conjunct: a refused delivery returns the zero
		// result, whose Err() is non-nil by construction, so every branch below
		// must be unreachable on refusal — including ones added later.
		if !admitted {
			return
		}
		if result.Err() != nil && !result.OwnerStopRequired() {
			s.logger.Warn("agentrun: milestone delivery did not settle cleanly",
				slog.String("lane", lane),
				slog.Any("error", result.Err()))
		}
	}
}

// newLaneAdmission builds one lane's admission latch. Both lanes and every test
// that assembles a lane come through here, so the recorder health reads cannot
// be wired on one path and missing on another.
//
// onRefused is nil: the milestone lanes declare no refusal today, and the
// per-lane declarer sweep has one home in #1342 (owner ruling OQ1, 2026-09-22).
func (s *MilestoneSubscriber) newLaneAdmission() *deliverylane.Admission {
	return deliverylane.NewAdmission(s.recordDeliveryOwnerFatal, nil)
}

// observeLane wraps one acquired handle in its binding and starts that lane's
// owner-stop observer. It is the one place a raw jetstream.ConsumeContext
// becomes a Binding, so the owner never holds a second path to the handle.
//
// react only logs: recording already happened, synchronously, in the
// admission's onFatal before the result reached the observer, and the drain is
// Observe's own act once react returns.
func (s *MilestoneSubscriber) observeLane(
	runCtx context.Context,
	lane string,
	handle jetstream.ConsumeContext,
	admission *deliverylane.Admission,
) *deliverylane.Binding {
	binding := deliverylane.NewBinding(handle)
	deliverylane.Observe(runCtx, binding, admission, func(result natsclient.DeliveryResult) {
		s.logger.Error("agentrun: milestone delivery ownership lost",
			slog.String("lane", lane),
			slog.Any("error", result.Err()))
	})
	return binding
}

// recordDeliveryOwnerFatal latches the FIRST loss of delivery ownership on
// either lane. It runs synchronously inside the delivery callback as the lane
// admission's onFatal, before the result is buffered for the observer, so
// health can never read healthy after the observer has drained the exact handle.
func (s *MilestoneSubscriber) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deliveryFatalErr != nil {
		return
	}
	s.deliveryFatalErr = result.Err()
}

// DeliveryFatal reports the first loss of delivery ownership observed by either
// milestone lane, or nil while both lanes still own their deliveries. A non-nil
// answer is permanent for this subscriber: the lane that lost ownership drained
// its exact handle and admits no further local work, so the milestones it would
// have processed stay unacknowledged in JetStream for a replacement process.
func (s *MilestoneSubscriber) DeliveryFatal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveryFatalErr
}
