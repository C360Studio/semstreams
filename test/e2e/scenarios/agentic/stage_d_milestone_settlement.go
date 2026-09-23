package agentic

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/test/e2e/harness/milestoneprobe"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

const (
	milestoneCompleteConsumerName = "agentrun-milestone-complete"
	milestoneFailedConsumerName   = "agentrun-milestone-failed"

	// milestoneProbeDedupWindow must outlast the gap between a delivery's two
	// attempts — a crash, a compose restart and the lane's 30s AckWait — or the
	// effect's Nats-Msg-Id would stop collapsing the replay and the proof would
	// read a duplicate the handler did not actually commit twice.
	milestoneProbeDedupWindow = 5 * time.Minute

	// milestoneRedeliveryBudget covers one AckWait expiry (30s, declared in
	// agentic/agentrun/agentrun.go) plus a replacement process reaching its
	// consumers again.
	milestoneRedeliveryBudget = 90 * time.Second

	// milestoneExhaustionBudget covers five attempts separated by the lane's
	// DelayedDeliveryRetry(30s): the fifth drop fires MAX_DELIVERIES about two
	// minutes after the first delivery, and the advisory observer has to store
	// and consume it. Doubled over that to survive a contended Docker host.
	milestoneExhaustionBudget = 240 * time.Second

	// milestoneLaneMaxDeliver is the finite ceiling both lanes declare (O2).
	// The tier asserts the handler saw exactly this many attempts, which is the
	// same number read from the other side of the contract.
	milestoneLaneMaxDeliver = 5

	milestoneExhaustionIdentityKey = "milestone_exhaustion_source_message_id"
	milestoneExhaustionBaselineKey = "milestone_exhaustion_baseline"
)

// armMilestoneExhaustion publishes the transient probe terminal that will
// exhaust the complete lane's finite MaxDeliver, and creates the evidence
// stream every later milestone stage reads.
//
// It is armed HERE, near the top of the tier, and asserted in
// verify-milestone-exhaustion, because exhaustion costs four redeliveries at
// the lane's 30s retry delay — about two minutes that would otherwise be spent
// waiting rather than testing. It is an action stage: it verifies nothing.
//
// It must also finish before any stage replaces the process. The exhaustion
// counter is a process-local Prometheus counter fed by a durable advisory whose
// acknowledgement floor persists, so an advisory counted by a process that is
// later replaced is counted once and never again.
func (s *Scenario) armMilestoneExhaustion(ctx context.Context, result *scenarios.Result) error {
	if _, err := s.ensureMilestoneProbeStream(ctx); err != nil {
		return err
	}
	baseline, err := s.metricWithLabels(ctx, "semstreams_nats_max_delivery_exhaustions_total",
		map[string]string{"consumer": milestoneCompleteConsumerName})
	if err != nil {
		return fmt.Errorf("read milestone exhaustion baseline: %w", err)
	}
	fixture, err := s.newMilestoneProbeTerminal(ctx, milestoneprobe.BehaviorTransient, agentic.CategoryLoopCompleted)
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, fixture.subject, fixture.wire); err != nil {
		return fmt.Errorf("publish milestone exhaustion terminal: %w", err)
	}
	result.Details[milestoneExhaustionIdentityKey] = fixture.sourceMessageID
	result.Details[milestoneExhaustionBaselineKey] = baseline
	return nil
}

// verifyMilestoneExhaustion is the assertion half of the armed exhaustion, and
// the verify-milestone-exhaustion stage.
//
// Its position is load-bearing in one direction: it must run before any stage
// replaces the process. Both counters it reads are process-local Prometheus
// counters fed by durable advisories and deliveries, so a replacement between
// the event and this read would lose the only occurrence there will be.
func (s *Scenario) verifyMilestoneExhaustion(ctx context.Context, result *scenarios.Result) error {
	sourceMessageID, ok := result.Details[milestoneExhaustionIdentityKey].(string)
	if !ok || sourceMessageID == "" {
		return fmt.Errorf("milestone exhaustion proof was never armed")
	}
	baseline, _ := result.Details[milestoneExhaustionBaselineKey].(float64)
	if err := s.waitMetricWithLabels(ctx, "semstreams_nats_max_delivery_exhaustions_total",
		map[string]string{"consumer": milestoneCompleteConsumerName},
		baseline+1, milestoneExhaustionBudget); err != nil {
		return fmt.Errorf("milestone lane exhaustion was not counted: %w", err)
	}
	// The same five attempts read through the operator's own signal. The
	// decisions counter increments only for a delivery that did NOT acknowledge
	// (observeDecision returns early on an Ack —
	// agentic/agentrun/milestone_settlement.go:218-219), so an ordinary
	// milestone contributes nothing to this series and the armed probe's five
	// transient returns are what it counts. The advisory above cannot fire
	// before the fifth attempt, so this wait costs no wall clock; it is the
	// first e2e observation of the lane/decision/reason signal at all.
	if err := s.waitMetricWithLabels(ctx, "semstreams_agentrun_milestone_decisions_total",
		map[string]string{"lane": "complete", "decision": "retry", "reason": "handler_transient"},
		milestoneLaneMaxDeliver, milestoneExhaustionBudget); err != nil {
		return fmt.Errorf("the milestone decisions counter did not record the lane's transient retries: %w", err)
	}
	stream, err := s.ensureMilestoneProbeStream(ctx)
	if err != nil {
		return err
	}
	attempts, err := streamSubjectCount(ctx, stream, milestoneprobe.AttemptSubject(sourceMessageID))
	if err != nil {
		return fmt.Errorf("count exhausted milestone attempts: %w", err)
	}
	// The same ceiling read from the other side: the server stopped at
	// MaxDeliver, so the handler must have been invoked exactly that many
	// times. A lane that had drifted to an unlimited MaxDeliver would keep
	// going and never produce the advisory the assertion above waits for.
	if attempts != milestoneLaneMaxDeliver {
		return fmt.Errorf("exhausted milestone handler attempts = %d, want exactly %d",
			attempts, milestoneLaneMaxDeliver)
	}
	effects, err := streamSubjectCount(ctx, stream, milestoneprobe.EffectSubject(sourceMessageID))
	if err != nil {
		return fmt.Errorf("count exhausted milestone effects: %w", err)
	}
	// A handler that never returned nil never committed a consequence. This is
	// the negative half of the identity contract: the delivery was dropped, and
	// nothing downstream was told it succeeded.
	if effects != 0 {
		return fmt.Errorf("exhausted milestone effects = %d, want 0 for a never-acknowledged delivery", effects)
	}
	result.Metrics["milestone_exhaustion_attempts"] = attempts
	result.Details["milestone_exhaustion_consumer"] = milestoneCompleteConsumerName
	return nil
}

// verifyMilestoneSettlement is #1155 stage D, on the acceptance amended by
// owner ruling O5: handler re-invocation plus an idempotent effect count, never
// a lifecycle transition (ADR-053 D5).
//
// It runs after verify-stage-a-process-replacement so no later replacement can
// reset what it measures, and before the approval and signal walks, whose loops
// publish terminals of their own onto the same two lanes.
func (s *Scenario) verifyMilestoneSettlement(ctx context.Context, result *scenarios.Result) (runErr error) {
	evidence, err := s.ensureMilestoneProbeStream(ctx)
	if err != nil {
		return err
	}
	defer func() {
		joinHarnessFinalizationError(ctx, &runErr, "delete milestone probe evidence stream",
			func(finalCtx context.Context) error {
				js, jsErr := s.nats.Client().JetStream()
				if jsErr != nil {
					return jsErr
				}
				return js.DeleteStream(finalCtx, milestoneprobe.EvidenceStream)
			})
	}()
	controller := newComposeProcessController(s.config.ComposeFile)

	if err := s.verifyMilestoneReplacementAcrossCrash(
		ctx, result, controller, evidence, agentic.CategoryLoopCompleted, milestoneCompleteConsumerName,
	); err != nil {
		return fmt.Errorf("complete lane replacement: %w", err)
	}
	if err := s.verifyMilestoneReplacementAcrossCrash(
		ctx, result, controller, evidence, agentic.CategoryLoopFailed, milestoneFailedConsumerName,
	); err != nil {
		return fmt.Errorf("failed lane replacement: %w", err)
	}
	if err := s.verifyMilestoneQuarantineAcrossReplacement(ctx, result, controller, evidence); err != nil {
		return fmt.Errorf("milestone quarantine: %w", err)
	}
	return nil
}

// verifyMilestoneReplacementAcrossCrash proves the W1 crash window on one lane:
// the handler commits its durable effect, the process ends BEFORE the Ack, the
// replacement is redelivered the same stored message, the handler observes the
// SAME SourceMessageID, and the effect is still one.
func (s *Scenario) verifyMilestoneReplacementAcrossCrash(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
	evidence jetstream.Stream,
	category string,
	consumerName string,
) error {
	fixture, err := s.newMilestoneProbeTerminal(ctx, milestoneprobe.BehaviorExitBeforeAck, category)
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, fixture.subject, fixture.wire); err != nil {
		return fmt.Errorf("publish crash-window terminal: %w", err)
	}
	first, err := waitForProbeAttempts(ctx, evidence, fixture.sourceMessageID, 1, 30*time.Second)
	if err != nil {
		return fmt.Errorf("wait for the first handler invocation: %w", err)
	}
	if err := s.waitForProcessGone(ctx, 30*time.Second); err != nil {
		return err
	}
	// Read the effect while the process is DOWN. This is the whole point of the
	// crash window: the consequence is durable although nothing acknowledged
	// the delivery that produced it.
	effect, err := readProbeEffect(ctx, evidence, fixture.sourceMessageID)
	if err != nil {
		return fmt.Errorf("the durable effect did not survive the crash: %w", err)
	}
	if effect.ProcessInstance != first.ProcessInstance {
		return fmt.Errorf("effect process instance = %q, want the crashed process %q",
			effect.ProcessInstance, first.ProcessInstance)
	}

	if err := s.startReplacement(ctx, controller); err != nil {
		return err
	}
	second, err := waitForProbeAttempts(ctx, evidence, fixture.sourceMessageID, 2, milestoneRedeliveryBudget)
	if err != nil {
		return fmt.Errorf("the replacement was not redelivered the unacknowledged milestone: %w", err)
	}
	if second.ProcessInstance == first.ProcessInstance {
		return fmt.Errorf("the redelivered milestone ran in the crashed process instance %q", first.ProcessInstance)
	}
	return s.assertIdempotentMilestoneSettlement(ctx, result, evidence, fixture, consumerName)
}

// assertIdempotentMilestoneSettlement is the O5 acceptance itself: one effect
// for the identity however many attempts it took, and nothing left pending.
func (s *Scenario) assertIdempotentMilestoneSettlement(
	ctx context.Context,
	result *scenarios.Result,
	evidence jetstream.Stream,
	fixture milestoneProbeFixture,
	consumerName string,
) error {
	consumer, err := s.milestoneConsumer(ctx, consumerName)
	if err != nil {
		return err
	}
	if err := waitForConsumerSettled(ctx, consumer, 0, milestoneRedeliveryBudget); err != nil {
		return fmt.Errorf("the replaced %s lane did not settle: %w", consumerName, err)
	}
	effects, err := streamSubjectCount(ctx, evidence, milestoneprobe.EffectSubject(fixture.sourceMessageID))
	if err != nil {
		return fmt.Errorf("count milestone effects: %w", err)
	}
	if effects != 1 {
		return fmt.Errorf("durable effects for %s = %d, want exactly 1 across every attempt",
			fixture.sourceMessageID, effects)
	}
	attempts, err := streamSubjectCount(ctx, evidence, milestoneprobe.AttemptSubject(fixture.sourceMessageID))
	if err != nil {
		return fmt.Errorf("count milestone handler attempts: %w", err)
	}
	if attempts < 2 {
		return fmt.Errorf("handler invocations for %s = %d, want at least 2 across the replacement",
			fixture.sourceMessageID, attempts)
	}
	// Keyed by behavior AND lane: the failed lane carries two separate proofs
	// (a crash and a quarantine), and a lane-only key would report one of them
	// twice and the other not at all.
	proof := "milestone_" + fixture.behavior + "_" + consumerName
	result.Details[proof+"_source_message_id"] = fixture.sourceMessageID
	result.Metrics[proof+"_handler_attempts"] = attempts
	result.Metrics[proof+"_durable_effects"] = effects
	return nil
}
