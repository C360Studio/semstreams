package agenticloop

import (
	"fmt"
	"log/slog"
	"sync"
)

const trajectoryHealthDiagnosticMaxBytes = 1024

type trajectoryAuditHealth struct {
	mu         sync.RWMutex
	errorCount int
	lastError  string
}

func (h *trajectoryAuditHealth) latch(diagnostic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.errorCount++
	h.lastError = boundedTrajectoryDiagnostic(diagnostic)
}

func (h *trajectoryAuditHealth) snapshot() (int, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.errorCount, h.lastError
}

func (c *Component) reportTrajectoryAuditFailure(failure trajectoryAuditFailure) {
	diagnostic := fmt.Sprintf("trajectory audit %s/%s/%s failed: %v",
		failure.Stage, failure.Kind, failure.Reason, failure.Err)
	c.trajectoryAuditHealth.latch(diagnostic)
	if c.metrics != nil {
		c.metrics.recordTrajectoryAuditFailure(failure.Stage, failure.Kind, failure.Reason)
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("trajectory audit recording failed",
		slog.String("loop_id", failure.LoopID),
		slog.String("attempt_id", failure.AttemptID),
		slog.String("kind", string(failure.Kind)),
		slog.String("stage", string(failure.Stage)),
		slog.String("reason", string(failure.Reason)),
		slog.Any("error", failure.Err))
}

func (c *Component) trajectoryProviderAvailable() bool {
	if c.trajectoryRecorder == nil {
		return true
	}
	if c.deps.StoreRegistry == nil {
		return false
	}
	_, ok := c.deps.StoreRegistry.Store(c.config.TrajectoryEvidenceStorageInstance)
	return ok
}

func boundedTrajectoryDiagnostic(value string) string {
	return agenticBoundedDiagnostic(value, trajectoryHealthDiagnosticMaxBytes)
}

func agenticBoundedDiagnostic(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes]
}
