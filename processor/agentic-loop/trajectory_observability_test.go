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
