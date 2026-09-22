package agentic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/test/e2e/harness/milestoneprobe"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// milestoneQuarantineCause is the substring the latched /health verdict must
// carry. MilestoneService.Health composes it as "milestone delivery ownership
// lost; milestones are unacknowledged: <err>" (service/milestone_service.go:166),
// and it is the phrase every other delivery owner in the tree latches with, so
// an operator greps one string across all of them.
const milestoneQuarantineCause = "delivery ownership lost"

// verifyMilestoneQuarantineAcrossReplacement proves the fail-closed half: a
// panicking handler quarantines its delivery — no Ack, Nak or Term — the
// service reports unhealthy at /health with the cause, that lane admits no
// further work, the OTHER lane keeps consuming, and a replacement resolves it.
//
// It quarantines the FAILED lane on purpose. Nothing else in this tier publishes
// agent.failed.*, so latching it cannot perturb the approval and signal walks
// that follow, while agent.complete.* stays available as the live evidence that
// one lane's fatal leaves the other consuming.
func (s *Scenario) verifyMilestoneQuarantineAcrossReplacement(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
	evidence jetstream.Stream,
) error {
	failedConsumer, err := s.milestoneConsumer(ctx, milestoneFailedConsumerName)
	if err != nil {
		return err
	}
	baseline, err := failedConsumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read failed milestone consumer baseline: %w", err)
	}
	fixture, err := s.newMilestoneProbeTerminal(ctx, milestoneprobe.BehaviorPanicOnce, agentic.CategoryLoopFailed)
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, fixture.subject, fixture.wire); err != nil {
		return fmt.Errorf("publish quarantining terminal: %w", err)
	}
	first, err := waitForProbeAttempts(ctx, evidence, fixture.sourceMessageID, 1, 30*time.Second)
	if err != nil {
		return fmt.Errorf("wait for the panicking handler invocation: %w", err)
	}
	status, err := s.waitForMilestoneHealth(ctx, false, 30*time.Second)
	if err != nil {
		return err
	}
	result.Details["milestone_quarantine_health_message"] = status.Message

	quarantined, err := s.assertQuarantinedLaneKeptAuthority(ctx, failedConsumer, baseline, status)
	if err != nil {
		return err
	}
	if err := s.assertOtherMilestoneLaneStillConsumes(ctx, result); err != nil {
		return err
	}
	if err := s.assertQuarantinedLaneAdmitsNothing(ctx, failedConsumer, quarantined); err != nil {
		return err
	}
	return s.verifyMilestoneQuarantineRecovery(ctx, result, controller, evidence, fixture, first)
}

// assertQuarantinedLaneKeptAuthority checks the #759 fail-closed shape: the
// delivery is neither acknowledged nor terminated, so JetStream still owns it,
// and the operator-visible verdict names why.
//
// The returned ConsumerInfo is the baseline the admits-nothing check reads its
// NumPending against: it is taken here, before anything else publishes, so a
// later message that the latched lane never hands over still shows up as a
// pending message that arrived.
func (s *Scenario) assertQuarantinedLaneKeptAuthority(
	ctx context.Context,
	consumer jetstream.Consumer,
	baseline *jetstream.ConsumerInfo,
	status milestoneHealthStatus,
) (*jetstream.ConsumerInfo, error) {
	quarantined, err := consumer.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read quarantined milestone consumer: %w", err)
	}
	if quarantined.AckFloor.Consumer != baseline.AckFloor.Consumer || quarantined.NumAckPending == 0 {
		return nil, fmt.Errorf(
			"quarantined milestone settled or lost authority: ack floor=%d (baseline %d) pending=%d",
			quarantined.AckFloor.Consumer, baseline.AckFloor.Consumer, quarantined.NumAckPending)
	}
	// waitForMilestoneHealth only requires healthy=false, which any unhealthy
	// cause would satisfy. The cause is what an operator acts on, so it is
	// asserted rather than merely recorded.
	if !strings.Contains(status.Message, milestoneQuarantineCause) {
		return nil, fmt.Errorf("latched milestone health message = %q, want it to name %q",
			status.Message, milestoneQuarantineCause)
	}
	// The code a probe or an operator's curl sees. milestoneHealth deliberately
	// accepts 200 OR 503 so it can be polled across the transition; the settled
	// code is pinned here, once the verdict has been observed.
	_, code, err := s.getHealthBody(ctx)
	if err != nil {
		return nil, fmt.Errorf("read /health under a latched milestone lane: %w", err)
	}
	if code != http.StatusServiceUnavailable {
		return nil, fmt.Errorf("/health = %d under a latched milestone lane, want %d",
			code, http.StatusServiceUnavailable)
	}
	// /readyz is deliberately untouched by a latched lane: the agentic compose
	// overrides the container healthcheck to /readyz, so a 503 there would stop
	// the container and replace the proof with a restart loop.
	code, err = s.probeReadiness(ctx)
	if err != nil {
		return nil, fmt.Errorf("read /readyz under a latched milestone lane: %w", err)
	}
	if code != 200 {
		return nil, fmt.Errorf("/readyz = %d under a latched milestone lane, want 200", code)
	}
	return quarantined, nil
}

// assertOtherMilestoneLaneStillConsumes publishes an ordinary terminal on the
// complete lane and requires it to be acknowledged while the failed lane is
// latched. A shared latch would leave it pending here.
func (s *Scenario) assertOtherMilestoneLaneStillConsumes(ctx context.Context, result *scenarios.Result) error {
	completeConsumer, err := s.milestoneConsumer(ctx, milestoneCompleteConsumerName)
	if err != nil {
		return err
	}
	before, err := completeConsumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read complete milestone consumer before the live check: %w", err)
	}
	live, err := s.newMilestoneProbeTerminal(ctx, "", agentic.CategoryLoopCompleted)
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, live.subject, live.wire); err != nil {
		return fmt.Errorf("publish live complete-lane terminal: %w", err)
	}
	if err := waitForConsumerSettled(ctx, completeConsumer, before.AckFloor.Consumer+1, 30*time.Second); err != nil {
		return fmt.Errorf("the complete lane stopped consuming while the failed lane was latched: %w", err)
	}
	result.Details["milestone_live_lane_loop_id"] = live.loopID
	return nil
}

// assertQuarantinedLaneAdmitsNothing publishes a second failed-lane terminal
// and requires the latched lane not to deliver it. The consumer's own counters
// are the evidence: the handler never runs, so no probe record would exist
// either way, and only the server can say whether anything was handed over. It
// takes two of them — a pending count that GREW and a delivery count that did
// NOT — so the assertion reads "a message arrived and was not handed over"
// rather than "nothing happened", which a failed publish would also satisfy.
func (s *Scenario) assertQuarantinedLaneAdmitsNothing(
	ctx context.Context, consumer jetstream.Consumer, quarantined *jetstream.ConsumerInfo,
) error {
	blocked, err := s.newMilestoneProbeTerminal(ctx, "", agentic.CategoryLoopFailed)
	if err != nil {
		return err
	}
	if err := s.nats.Publish(ctx, blocked.subject, blocked.wire); err != nil {
		return fmt.Errorf("publish post-latch failed-lane terminal: %w", err)
	}
	if err := waitDuration(ctx, 2*time.Second); err != nil {
		return err
	}
	postLatch, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read post-latch milestone consumer: %w", err)
	}
	// The positive control on the wait: an unchanged Delivered count also
	// describes a message that never arrived, which would make a broken publish
	// read as a working latch. NumPending is the server saying the message IS
	// there for this consumer and has not been handed over.
	if postLatch.NumPending <= quarantined.NumPending {
		return fmt.Errorf(
			"the post-latch failed-lane terminal never reached the consumer: pending %d -> %d",
			quarantined.NumPending, postLatch.NumPending)
	}
	if postLatch.Delivered.Consumer != quarantined.Delivered.Consumer {
		return fmt.Errorf("the latched failed lane kept delivering: %d -> %d",
			quarantined.Delivered.Consumer, postLatch.Delivered.Consumer)
	}
	return nil
}

// verifyMilestoneQuarantineRecovery replaces the process and requires the
// quarantined delivery to come back, be handled by a different process, commit
// exactly one effect, and settle along with the work the latch had blocked.
func (s *Scenario) verifyMilestoneQuarantineRecovery(
	ctx context.Context,
	result *scenarios.Result,
	controller composeProcessController,
	evidence jetstream.Stream,
	fixture milestoneProbeFixture,
	first milestoneprobe.Attempt,
) error {
	if err := s.replaceSemStreams(ctx, controller); err != nil {
		return err
	}
	second, err := waitForProbeAttempts(ctx, evidence, fixture.sourceMessageID, 2, milestoneRedeliveryBudget)
	if err != nil {
		return fmt.Errorf("the replacement was not redelivered the quarantined milestone: %w", err)
	}
	if second.ProcessInstance == first.ProcessInstance {
		return fmt.Errorf("the quarantined milestone re-ran in the panicking process instance %q",
			first.ProcessInstance)
	}
	if err := s.assertIdempotentMilestoneSettlement(
		ctx, result, evidence, fixture, milestoneFailedConsumerName,
	); err != nil {
		return err
	}
	if _, err := s.waitForMilestoneHealth(ctx, true, 30*time.Second); err != nil {
		return fmt.Errorf("the replacement did not clear the milestone health verdict: %w", err)
	}
	result.Details["milestone_quarantine_source_message_id"] = fixture.sourceMessageID
	return nil
}
