package agenticloop

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
)

func TestTrajectoryAuditFailureLatchesDegradedHealth(t *testing.T) {
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), metrics: getMetrics(nil), started: true, startTime: time.Now()}
	c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
		Stage:     trajectoryStageFactCreate,
		Kind:      agentic.TrajectoryKindToolCompleted,
		Reason:    trajectoryReasonBackend,
		LoopID:    "loop-health",
		AttemptID: "attempt",
		Err:       errors.New("backend unavailable"),
	})
	health := c.Health()
	if health.Healthy || health.Status != "degraded" || health.ErrorCount != 1 || health.LastError == "" {
		t.Fatalf("Health() = %#v, want sticky degraded audit loss", health)
	}
}

// newAuditFailure builds a trajectoryAuditFailure for loopID at stage.
func newAuditFailure(loopID string, stage trajectoryAuditStage, reason trajectoryAuditReason) trajectoryAuditFailure {
	return trajectoryAuditFailure{
		Stage:     stage,
		Kind:      agentic.TrajectoryKindToolCompleted,
		Reason:    reason,
		LoopID:    loopID,
		AttemptID: "attempt",
		Err:       errors.New("backend unavailable"),
	}
}

// The per-loop marker is the FOURTH sink of the same fan-out, derived from
// the same observed failure value as the Health latch, the metric, and the
// log — never from the counter and never by re-evaluating a predicate.
func TestTrajectoryAuditFailureMarksObservingLoop(t *testing.T) {
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), metrics: getMetrics(nil), started: true, startTime: time.Now()}

	c.reportTrajectoryAuditFailure(newAuditFailure("loop-marked", trajectoryStageEvidencePut, trajectoryReasonBackend))

	if !c.trajectoryAuditLoss.observed("loop-marked") {
		t.Error("observing loop is not marked; the terminal write cannot classify it")
	}
	if c.trajectoryAuditLoss.observed("loop-other") {
		t.Error("an unrelated loop was marked; the marker must not leak across loops")
	}
	// The pre-existing sinks still fire — the fourth sibling is an addition,
	// not a replacement.
	if health := c.Health(); health.Healthy || health.ErrorCount != 1 {
		t.Errorf("Health() = %#v, want sticky degraded audit loss", health)
	}
}

// Spec: "repeated failures at several stages yield one unqualified
// condition". The marker is a set, so several observations collapse to the
// single terminal triple asserted in the builder tests.
func TestTrajectoryAuditFailureMultipleStagesMarkOnce(t *testing.T) {
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), metrics: getMetrics(nil), started: true, startTime: time.Now()}

	c.reportTrajectoryAuditFailure(newAuditFailure("loop-multi", trajectoryStageEvidencePut, trajectoryReasonBackend))
	c.reportTrajectoryAuditFailure(newAuditFailure("loop-multi", trajectoryStageFactCreate, trajectoryReasonTimeout))
	c.reportTrajectoryAuditFailure(newAuditFailure("loop-multi", trajectoryStageEvidenceVerify, trajectoryReasonIntegrity))

	if !c.trajectoryAuditLoss.observed("loop-multi") {
		t.Fatal("loop with three observed failures is not marked")
	}

	triples := buildLoopCompletionTriples(
		"acme.ops.agent.agentic-loop.execution.loop-multi",
		&agentic.LoopCompletedEvent{LoopID: "loop-multi", Outcome: "success", CompletedAt: time.Now()},
		"", 0, c.trajectoryAuditLoss.observed("loop-multi"))

	var conditions int
	for _, tr := range triples {
		if tr.Predicate == "agent.loop.evidence-integrity" {
			conditions++
		}
	}
	if conditions != 1 {
		t.Errorf("three observed failures produced %d condition triples, want exactly 1", conditions)
	}
}

// Bucket acquisition and provider resolution fail at Start with no loop
// subject. Those failures belong to the other three sinks: there is no
// entity to stamp, and an entry keyed on "" would never be released by any
// loop terminal — an unbounded leak in a long-running process.
func TestTrajectoryAuditFailureWithoutLoopIDMarksNothing(t *testing.T) {
	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), metrics: getMetrics(nil), started: true, startTime: time.Now()}

	c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
		Stage:  trajectoryStageProviderResolve,
		Kind:   agentic.TrajectoryKindLoopStarted,
		Reason: trajectoryReasonProviderUnavailable,
		Err:    errors.New("storage instance unavailable"),
	})

	if c.trajectoryAuditLoss.observed("") {
		t.Error("subject-less audit failure retained an unreleasable marker")
	}
	if health := c.Health(); health.Healthy {
		t.Error("subject-less audit failure did not reach the Health latch")
	}
}

// release is idempotent so competing terminal cleanup paths cannot turn
// cleanup into another failure.
func TestLoopAuditLossReleaseIsIdempotent(t *testing.T) {
	var loss loopAuditLoss

	loss.observe("loop-release")
	loss.release("loop-release")
	loss.release("loop-release")
	loss.release("never-observed")

	if loss.observed("loop-release") {
		t.Error("marker survived release")
	}
}

// A component that determines at Start it cannot record trajectory
// evidence at all will never observe a per-loop failure, because nothing is
// ever attempted. The component-wide latch is what keeps that case from
// emitting a graph byte-identical to a healthy one — it answers for loops
// the marker has never seen, including ones that do not exist yet.
func TestLoopAuditLossAllLoopsCoversUnseenLoops(t *testing.T) {
	var loss loopAuditLoss

	if loss.observed("loop-a") {
		t.Fatal("clean loss set reported an observation")
	}

	loss.observeAllLoops()

	for _, loopID := range []string{"loop-a", "loop-never-seen", "loop-not-yet-created"} {
		if !loss.observed(loopID) {
			t.Errorf("component-wide loss did not cover %q", loopID)
		}
	}
}

// Releasing a loop clears that loop's marker and nothing else. The
// component-wide latch is not a per-loop fact, and the condition it records
// is never repaired in-process, so a terminal must not be able to clear it
// for the loops that follow.
func TestLoopAuditLossReleaseDoesNotClearAllLoops(t *testing.T) {
	var loss loopAuditLoss

	loss.observeAllLoops()
	loss.observe("loop-a")
	loss.release("loop-a")

	if !loss.observed("loop-a") {
		t.Error("release cleared the component-wide latch for the released loop")
	}
	if !loss.observed("loop-b") {
		t.Error("release cleared the component-wide latch for a later loop")
	}
}

// orphanUnwindBucket blocks the recorder's restart scan until the batch
// budget expires, then holds the ABANDONED goroutine inside the scan until
// the test releases it. That makes the orphan's own emit attempt land
// strictly after the loop's terminal release instead of racing it, which is
// what turns this from a flaky repro into a proof.
type orphanUnwindBucket struct {
	entered chan struct{}
	unwind  chan struct{}
}

func (b *orphanUnwindBucket) Create(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error) {
	return 0, errors.New("orphanUnwindBucket: Create not reached")
}

func (b *orphanUnwindBucket) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, errors.New("orphanUnwindBucket: Get not reached")
}

func (b *orphanUnwindBucket) ListKeysFiltered(ctx context.Context, _ ...string) (jetstream.KeyLister, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	<-b.unwind
	return nil, ctx.Err()
}

// recordTrajectoryBatchWithin abandons its goroutine when the budget
// expires. If that goroutine can still reach the fan-out afterwards, it
// re-inserts the loop's marker AFTER the loop's only release point has run:
// the marker then leaks for the process lifetime, and a later loop reusing
// the same loop ID (deterministic product-supplied IDs — CreateLoopWithID
// overwrites rather than rejects) inherits an `incomplete` that is not its
// own, breaking "absent on every other loop".
//
// The loss itself is not lost by suppressing the orphan: the budget branch
// already reported it synchronously, in time to reach the terminal write,
// which the late report is not.
func TestOrphanedAuditAttemptDoesNotRemarkReleasedLoop(t *testing.T) {
	const loopID = "loop-orphan"
	bucket := &orphanUnwindBucket{entered: make(chan struct{}, 1), unwind: make(chan struct{})}
	c := &Component{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		handler:   NewMessageHandler(DefaultConfig()),
		metrics:   getMetrics(nil),
		started:   true,
		startTime: time.Now(),
	}
	c.trajectoryRecorder = newTrajectoryRecorder(bucket, nil, "objectstore", c.reportTrajectoryAuditFailure)

	c.recordTrajectoryBatchWithin(context.Background(), []trajectoryObservation{{
		LoopID: loopID, Kind: agentic.TrajectoryKindLoopTerminal, CausalPhase: agentic.TrajectoryPhaseTerminal,
	}}, 25*time.Millisecond)

	// The budget branch observed the loss on the caller's goroutine, before
	// the terminal write. This is the observation the condition is built from.
	if !c.trajectoryAuditLoss.observed(loopID) {
		t.Fatal("budget-expiry report did not mark the loop")
	}
	select {
	case <-bucket.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("restart scan never entered; the goroutine was never abandoned")
	}

	// Terminal: the loop is stamped, then its transient state is released.
	c.releaseLoopTransientState(loopID)
	if c.trajectoryAuditLoss.observed(loopID) {
		t.Fatal("release did not clear the marker")
	}

	// Only now let the abandoned goroutine unwind and attempt its own report.
	close(bucket.unwind)

	// Fence: the goroutine returns the loop's batch token from a defer that
	// runs strictly AFTER its emit attempt, so acquiring that token is a
	// happens-after for the whole orphan — no sleep, no polling.
	fenceCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, acquired := c.trajectoryRecorder.acquireLoopBatch(fenceCtx, loopID)
	if !acquired {
		t.Fatal("abandoned goroutine never released the loop batch token")
	}
	release()

	if c.trajectoryAuditLoss.observed(loopID) {
		t.Fatal("an abandoned audit attempt re-marked a released loop: the marker now leaks for the " +
			"process lifetime and a later loop reusing this ID inherits a false incomplete")
	}
}
