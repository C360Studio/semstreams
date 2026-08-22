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

// loopAuditLoss records, per loop, that the component OBSERVED at least one
// trajectory audit failure while recording that loop's evidence. It is the
// fourth sink of reportTrajectoryAuditFailure's fan-out, a sibling of the
// Health latch, the metric, and the ERROR log — fed by the same
// trajectoryAuditFailure value, never re-derived from the counter or by
// re-evaluating any predicate.
//
// The set is deliberately unqualified: it holds loop IDs, not stages,
// kinds, reasons, or attempts. A loop may lose evidence at several stages
// and electing one would manufacture a claim about which mattered.
//
// Lifetime is bounded to the loop. Entries are released by
// releaseLoopTransientState alongside the loop's trajectory aggregate, so
// the set cannot grow without limit in a long-running process. Failures
// with no loop subject (bucket acquisition and provider resolution at
// Start) carry an empty LoopID and are recorded by the other three sinks
// only — there is no entity to stamp and no terminal that would ever
// release the entry.
//
// The zero value is usable, matching trajectoryAuditHealth: the component
// holds it by value so no wiring step can leave the observation unrecorded.
type loopAuditLoss struct {
	mu    sync.Mutex
	loops map[string]struct{}
}

func (l *loopAuditLoss) observe(loopID string) {
	if loopID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loops == nil {
		l.loops = make(map[string]struct{})
	}
	l.loops[loopID] = struct{}{}
}

// observed reports whether audit loss was seen for loopID. It is a pure
// read: the terminal graph write asks the question and the loop's terminal
// cleanup answers for the release.
func (l *loopAuditLoss) observed(loopID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.loops[loopID]
	return ok
}

// release drops the loop's marker. Idempotent, so competing terminal
// cleanup paths cannot turn release into another failure.
func (l *loopAuditLoss) release(loopID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.loops, loopID)
}

func (c *Component) reportTrajectoryAuditFailure(failure trajectoryAuditFailure) {
	diagnostic := fmt.Sprintf("trajectory audit %s/%s/%s failed: %v",
		failure.Stage, failure.Kind, failure.Reason, failure.Err)
	c.trajectoryAuditHealth.latch(diagnostic)
	c.trajectoryAuditLoss.observe(failure.LoopID)
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
