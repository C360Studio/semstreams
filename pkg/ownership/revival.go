package ownership

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// watchRearmBackoff is how long WatchRevival waits before re-arming the epoch
// watcher after an arm failure or a closed watcher channel. Short enough that a
// genuine supersession is caught promptly, long enough not to hot-loop against a
// flapping connection. The detection is best-effort (the write-seam lease check
// is the real guarantee), so a few seconds of re-arm latency is acceptable.
const watchRearmBackoff = 2 * time.Second

// revivalDecision classifies what WatchRevival should do for one of this
// process's owners given a live epoch observation.
type revivalDecision int

const (
	// revivalNone — the owner's live claim still carries our incarnation (or a
	// legacy/pre-fence empty incarnation): no rival, nothing to do.
	revivalNone revivalDecision = iota
	// revivalQuiesce — the owner's live claim carries a DIFFERENT incarnation: a
	// rival process owns it now, so quiesce our authoritative writes.
	revivalQuiesce
	// revivalAbsentWarn — the owner has no owning claim in the live epoch
	// (compacted with no successor): warn-once, do NOT quiesce (no rival to
	// clobber; the repair is re-registration, a later increment).
	revivalAbsentWarn
)

// decideRevival classifies an epoch observation for one of our owners (ADR-056
// PR-4). myInc is this process's incarnation; liveInc/present describe the
// owner's live OWNING claim in the epoch (from epoch.incarnationOfOwner).
//
// Decision (Coby 2026-06-18, quiesce-only-on-supersession):
//   - absent → warn-only (no rival is clobbering; self-silencing a still-live
//     owner whose presence key merely lapsed under GC pressure would be worse
//     than the benign drift).
//   - present with a DIFFERENT non-empty incarnation → quiesce (clear
//     supersession; we are the stale writer).
//   - present with OUR incarnation → none (our claim, the common case on every
//     benign epoch bump by an unrelated owner).
//   - present with an EMPTY incarnation → none (a legacy/pre-fence owner; there
//     is no rival incarnation to compare, so the lease is not enforceable —
//     mirrors the graph-ingest fail-open on empty incarnation).
func decideRevival(myInc, liveInc string, present bool) revivalDecision {
	if !present {
		return revivalAbsentWarn
	}
	if liveInc == "" || liveInc == myInc {
		return revivalNone
	}
	return revivalQuiesce
}

// WatchRevival watches the OWNER_CLAIMS epoch and quiesces any of this process's
// registered owners that the live epoch shows superseded by a different
// incarnation (ADR-056 PR-4 — the post-registration Watch the epoch.Version
// counter was added for). It is the producer-side courtesy that stops a
// revived-stale writer from clobbering between its revival and the next write;
// the write-seam lease check (PR-3 observe / PR-5 reject) is the real data-safety
// guarantee. Posture is QUIESCE-not-HALT: a superseded owner stops writing, the
// process keeps running (availability over strict; Coby).
//
// Blocks until ctx is cancelled — run it in a goroutine over a SHUTDOWN-CANCELLED
// context (never context.Background), and join it before the NATS client closes
// (see the cmd wiring). Best-effort and fail-open: a decode error skips one
// update, a closed/failed watcher re-arms after a short backoff, and nothing here
// ever blocks a write or exits the process. metrics may be nil (tests).
func (reg *Registry) WatchRevival(ctx context.Context, metrics *metric.MetricsRegistry) error {
	counter := getOwnerRevivalQuiesceMetric(metrics)
	for ctx.Err() == nil {
		watcher, err := reg.claims.Watch(ctx, registryKey)
		if err != nil {
			if ctx.Err() != nil {
				break // shutting down — the arm error is just the cancelled ctx, not a fault.
			}
			reg.logger.Warn("ownership: WatchRevival could not arm epoch watch — retrying",
				slog.Any("error", err))
			if !sleepCtx(ctx, watchRearmBackoff) {
				break
			}
			continue
		}
		// runRevivalWatch returns on ctx cancellation OR a closed watcher channel;
		// the outer loop re-arms in the latter case.
		reg.runRevivalWatch(ctx, watcher, counter)
		_ = watcher.Stop()
		if ctx.Err() == nil {
			// Channel closed without cancellation — back off before re-arming.
			if !sleepCtx(ctx, watchRearmBackoff) {
				break
			}
		}
	}
	return ctx.Err()
}

// runRevivalWatch consumes one watcher's update stream until ctx is cancelled or
// the channel closes. A nil entry is the end-of-initial-replay sentinel.
func (reg *Registry) runRevivalWatch(ctx context.Context, watcher jetstream.KeyWatcher, counter *prometheus.CounterVec) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				return // watcher channel closed → caller re-arms
			}
			if entry == nil {
				continue // end-of-initial-replay sentinel (no value)
			}
			reg.processEpochUpdate(entry.Value(), counter)
		}
	}
}

// processEpochUpdate evaluates one epoch value against this process's registered
// owners, quiescing any that a different incarnation now owns. Fail-open on a
// decode error (skip this update). Fires the test-only revivalUpdates signal
// after fully processing the update so integration tests can await detection.
func (reg *Registry) processEpochUpdate(value []byte, counter *prometheus.CounterVec) {
	defer reg.signalRevivalUpdate()

	ep, err := decodeEpoch(value)
	if err != nil {
		reg.logger.Warn("ownership: WatchRevival could not decode epoch — skipping update (fail-open)",
			slog.Any("error", err))
		return
	}

	for owner := range reg.registeredOwners() {
		if reg.IsQuiesced(owner) {
			continue // already latched — no further action
		}
		liveInc, present := ep.incarnationOfOwner(owner)
		switch decideRevival(reg.incarnation, liveInc, present) {
		case revivalQuiesce:
			if reg.markQuiesced(owner) {
				counter.WithLabelValues(owner).Inc()
				reg.logger.Error("ownership: REVIVAL QUIESCE — owner superseded by another process; quiescing this process's authoritative writes for it",
					slog.String("owner", owner),
					slog.String("my_incarnation", reg.incarnation),
					slog.String("live_incarnation", liveInc),
					slog.Uint64("epoch_version", ep.Version))
			}
		case revivalAbsentWarn:
			reg.warnAbsentOnce(owner, ep.Version)
		case revivalNone:
			// Our claim with our incarnation (or a legacy empty one) — nothing to do.
		}
	}
}

// warnAbsentOnce logs (at WARN) that one of our registered owners is absent from
// the live epoch, exactly once per owner. Absence is benign (no rival to
// clobber), so it is not a quiesce — but it is an anomaly worth surfacing.
func (reg *Registry) warnAbsentOnce(owner string, epochVersion uint64) {
	reg.absentWarnedMu.Lock()
	_, already := reg.absentWarned[owner]
	if !already {
		reg.absentWarned[owner] = struct{}{}
	}
	reg.absentWarnedMu.Unlock()
	if already {
		return
	}
	reg.logger.Warn("ownership: registered owner absent from live epoch (compacted with no successor; not quiescing — re-registration repair is a later increment)",
		slog.String("owner", owner),
		slog.Uint64("epoch_version", epochVersion))
}

// signalRevivalUpdate fires the test-only synchronization channel, if set, after
// an epoch update has been fully processed. Non-blocking so a full buffer never
// wedges the watch loop; nil (the production default) is a no-op.
func (reg *Registry) signalRevivalUpdate() {
	if reg.revivalUpdates == nil {
		return
	}
	select {
	case reg.revivalUpdates <- struct{}{}:
	default:
	}
}

// setRevivalUpdates installs the test-only post-update signal channel. Used by
// integration tests to await detection deterministically instead of sleeping;
// never set in production.
func (reg *Registry) setRevivalUpdates(ch chan<- struct{}) {
	reg.revivalUpdates = ch
}

// sleepCtx waits for d or ctx cancellation, returning true if d elapsed and
// false if ctx was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

var (
	ownerRevivalQuiesceOnce sync.Once
	ownerRevivalQuiesceVec  *prometheus.CounterVec
)

// getOwnerRevivalQuiesceMetric returns the process-wide counter for ADR-056 PR-4
// revival quiesces — owners this process stopped writing after detecting a
// different incarnation took them over. Registered into the app's MetricsRegistry
// (the /metrics-scraped registry) when one is provided; falls back to the default
// Prometheus registerer for nil (test paths). Label: owner (bounded — one of this
// process's own registered owners). Mirrors graph-ingest's lazy metric pattern.
func getOwnerRevivalQuiesceMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	ownerRevivalQuiesceOnce.Do(func() {
		ownerRevivalQuiesceVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "ownership",
			Name:      "owner_revival_quiesce_total",
			Help:      "Count of owners this process quiesced after WatchRevival detected eviction-then-supersession by a different incarnation (ADR-056 PR-4; quiesce-not-HALT). Label: owner.",
		}, []string{"owner"})
		if registry != nil {
			_ = registry.RegisterCounterVec("ownership", "owner_revival_quiesce_total", ownerRevivalQuiesceVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(ownerRevivalQuiesceVec)
		}
	})
	return ownerRevivalQuiesceVec
}
