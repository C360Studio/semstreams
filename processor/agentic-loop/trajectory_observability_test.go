package agenticloop

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
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
