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

// loopAuditLoss records that the component OBSERVED trajectory audit loss,
// at two scopes that answer the same question for the terminal write.
//
// Per loop: the fourth sink of reportTrajectoryAuditFailure's fan-out, a
// sibling of the Health latch, the metric, and the ERROR log — fed by the
// same trajectoryAuditFailure value, never re-derived from the counter or
// by re-evaluating any predicate.
//
// Whole component: when Start determines it cannot record trajectory
// evidence AT ALL (see observeAllLoops), no loop in the process will ever
// produce a per-loop failure to observe, because nothing is ever attempted.
// Without that scope the most total evidence loss possible would emit a
// graph byte-identical to a perfectly healthy one — precisely the state
// this predicate exists to make readable.
//
// observed() answers for both scopes, so the terminal write consults ONE
// reader and a future terminal path cannot accidentally honour half the
// fact.
//
// The set is deliberately unqualified: it holds loop IDs, not stages,
// kinds, reasons, or attempts. A loop may lose evidence at several stages
// and electing one would manufacture a claim about which mattered.
//
// Growth is bounded. Per-loop entries are released by
// releaseLoopTransientState alongside the loop's trajectory aggregate; the
// component-wide latch is one bool that is set at most once and never
// released, because the condition it records is never repaired in-process
// (the operator wipes the bucket and restarts). Failures with no loop
// subject carry an empty LoopID and mark nothing here — there is no entity
// to stamp and no terminal that would ever release such an entry. The
// component-wide latch, not the empty-LoopID report, is what makes total
// loss readable.
//
// The zero value is usable, matching trajectoryAuditHealth: the component
// holds it by value so no wiring step can leave the observation unrecorded.
type loopAuditLoss struct {
	mu       sync.Mutex
	loops    map[string]struct{}
	allLoops bool
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

// observeAllLoops latches audit loss for EVERY loop this component will
// ever terminate. Its one caller is the Start path that discovers the
// trajectory fact bucket is unusable and leaves the component with no
// recorder: from that point nothing is attempted, so no per-loop failure
// can be observed, yet every loop's evidence is missing. Stamping them all
// is the honest reading — it reports what the component observed about
// itself, and still never claims any loop IS complete.
//
// Deliberately one-way. The Start path's own policy is that an unusable
// bucket is never reconciled in-process; a latch that could clear would
// let loops run unmarked after a repair that does not happen.
func (l *loopAuditLoss) observeAllLoops() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.allLoops = true
}

// observed reports whether audit loss was seen for loopID, at either
// scope. It is a pure read: the terminal graph write asks the question and
// the loop's terminal cleanup answers for the release.
func (l *loopAuditLoss) observed(loopID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.allLoops {
		return true
	}
	_, ok := l.loops[loopID]
	return ok
}

// release drops the loop's per-loop marker. Idempotent, so competing
// terminal cleanup paths cannot turn release into another failure. It does
// not clear the component-wide latch, which is not a per-loop fact.
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
