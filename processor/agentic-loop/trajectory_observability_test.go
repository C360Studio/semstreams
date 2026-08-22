package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

// A Start-time failure carries no loop subject, so it cannot mark THROUGH
// THE PER-LOOP SET: there is no entity to key on, and an entry keyed on ""
// would never be released by any loop terminal — an unbounded leak.
//
// This is emphatically NOT the claim that such failures mark nothing. When
// Start ends up with no recorder at all, observeAllLoops latches the loss
// for every loop in the process; that is the whole of Finding A, and the
// path is proven by
// TestStartWithoutUsableTrajectoryBucketMarksEveryLoop_Integration. This
// case is the OTHER Start-time failure — provider_resolve with a recorder
// still present, where per-loop evidence failures do carry a loop ID and
// mark normally as they occur.
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
	var logs bytes.Buffer
	c := &Component{
		logger:    slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError})),
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

	// ...and the late report was NOT swallowed. Suppression is scoped to the
	// mark: through the real recorder path the orphan's own classification
	// still reaches Health and the ERROR log, alongside the budget branch's
	// synthetic one. Two distinct reports, two Health errors.
	if health := c.Health(); health.ErrorCount != 2 {
		t.Errorf("Health().ErrorCount = %d, want 2 (budget report + late report); "+
			"a late discovery must still degrade Health", health.ErrorCount)
	}
	line := logs.String()
	if !strings.Contains(line, string(trajectoryReasonTimeout)) {
		t.Errorf("budget branch's synthetic report missing from the log: %s", line)
	}
	if !strings.Contains(line, "late=true") {
		t.Errorf("the abandoned attempt's own late classification never reached the log: %s", line)
	}
}

// The narrowing, stated as a test so the next person cannot "simplify" it
// back into a blanket drop.
//
// A late failure is a REAL failure: a store.Put that returned a backend
// error at T+240ms is a genuine evidence_put/backend_error. The budget
// branch already reported the LOSS, but it could only classify it as the
// synthetic fact_create/timeout — so dropping the late report outright
// would leave an operator diagnosing a payload-size rejection as a latency
// problem, and would make the MODIFIED requirement's "Every trajectory
// audit failure SHALL emit ERROR ... increment ... and latch Health"
// literally false.
//
// Lateness therefore suppresses exactly one sink: the mark, which is the
// only one for which "late" makes the answer WRONG rather than tardy.
func TestLateAuditFailureReachesEverySinkExceptTheMark(t *testing.T) {
	var logs bytes.Buffer
	c := &Component{
		logger:    slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError})),
		metrics:   getMetrics(nil),
		started:   true,
		startTime: time.Now(),
	}

	counter := c.metrics.trajectoryAuditFailures.WithLabelValues(
		string(trajectoryStageEvidencePut),
		string(agentic.TrajectoryKindToolCompleted),
		string(trajectoryReasonBackend))
	before := testutil.ToFloat64(counter)

	late := newAuditFailure("loop-late", trajectoryStageEvidencePut, trajectoryReasonBackend)
	late.Late = true
	c.reportTrajectoryAuditFailure(late)

	// Sink 1 — Health still degrades. A late discovery is still a
	// discovery that this component lost audit state.
	if health := c.Health(); health.Healthy || health.ErrorCount != 1 || health.LastError == "" {
		t.Errorf("Health() = %#v, want degraded on a late failure", health)
	}

	// Sink 2 — the bounded counter still increments, under the failure's
	// OWN stage and reason, not the budget branch's synthetic pair.
	if got := testutil.ToFloat64(counter) - before; got != 1 {
		t.Errorf("evidence_put/backend_error counter delta = %v, want 1", got)
	}

	// Sink 3 — the ERROR line still carries the real classification, and
	// says the report was late so an operator can tell why no condition
	// landed on the loop.
	line := logs.String()
	for _, want := range []string{
		string(trajectoryStageEvidencePut),
		string(trajectoryReasonBackend),
		"loop-late",
		"late=true",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("ERROR line missing %q; got: %s", want, line)
		}
	}

	// Sink 4 — and ONLY this one is suppressed.
	if c.trajectoryAuditLoss.observed("loop-late") {
		t.Error("a late failure marked the loop; it can only re-mark a loop that already terminated")
	}
}

// The same failure arriving on time marks normally — the flag, not the
// stage or reason, is what suppresses the mark.
func TestOnTimeAuditFailureMarksAndReachesEverySink(t *testing.T) {
	c := &Component{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics:   getMetrics(nil),
		started:   true,
		startTime: time.Now(),
	}

	onTime := newAuditFailure("loop-on-time", trajectoryStageEvidencePut, trajectoryReasonBackend)
	c.reportTrajectoryAuditFailure(onTime)

	if !c.trajectoryAuditLoss.observed("loop-on-time") {
		t.Error("an on-time failure did not mark its loop")
	}
	if health := c.Health(); health.Healthy || health.ErrorCount != 1 {
		t.Errorf("Health() = %#v, want degraded", health)
	}
}
