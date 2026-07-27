package ownership

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Liveness tuning for owning leases (ADR-056 Decision 2 staleness lifecycle).
// These are the values graph-ingest stamps as the OWNER_PRESENCE bucket TTL
// and that an embedder ticks Heartbeat on.
const (
	// PresenceTTL is the bucket-level TTL on OWNER_PRESENCE, set at bucket
	// creation. A presence key not re-bumped within this window ages out, and
	// its owning registration becomes compactable out of the epoch by the next
	// registrant. Non-owning registrations have no presence key and are exempt.
	// It is the staleness floor the ADR pins at ttl_hint ≥ 3×max(boot_time,
	// gc_pause_budget): 120s comfortably exceeds a slow service boot or a long
	// GC pause, so a live owning entry is never falsely evicted, while a
	// genuinely dead owning lease frees within ~one TTL of the next boot.
	PresenceTTL = 120 * time.Second

	// HeartbeatInterval is how often a live owning registration re-bumps its
	// presence key — well under PresenceTTL (4 beats per window), so losing up to 3
	// consecutive beats does not cross the staleness floor.
	HeartbeatInterval = 30 * time.Second
)

// Heartbeater periodically refreshes the OWNER_PRESENCE keys of a set of owning
// registrations so a live process's replace/CAS lease is never compacted out of
// the epoch by a later registrant (ADR-056 Decision 2). Registry.Heartbeat's
// contract is "the caller runs this on a ticker"; Heartbeater is that ticker — a
// small substrate helper the embedder (lifecycle.Manager today; future owning
// producers next) drives over its own lifetime context, so each embedder does
// not reinvent the loop.
//
// Owning owners are added incrementally via Add, which is safe to call before
// or during Run. Append/foreign-edge-only registrations are not enrolled. Run
// blocks until its context is cancelled.
type Heartbeater struct {
	reg      *Registry
	interval time.Duration
	logger   *slog.Logger

	mu     sync.Mutex
	owners map[string]struct{}
}

// NewHeartbeater builds a Heartbeater over the registry. A non-positive
// interval falls back to HeartbeatInterval.
func (reg *Registry) NewHeartbeater(interval time.Duration) *Heartbeater {
	if interval <= 0 {
		interval = HeartbeatInterval
	}
	return &Heartbeater{
		reg:      reg,
		interval: interval,
		logger:   reg.logger,
		owners:   make(map[string]struct{}),
	}
}

// Add enrolls an owning registration's owner id for heartbeating on every
// subsequent tick. Idempotent.
func (h *Heartbeater) Add(owner string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.owners[owner] = struct{}{}
}

// IsEnrolled reports whether owner is currently enrolled for heartbeating.
// Enrollment is the property that keeps a live owner's OWNER_PRESENCE key fresh
// (and therefore its atomic owning entry compaction-safe); used by observability
// and tests.
func (h *Heartbeater) IsEnrolled(owner string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.owners[owner]
	return ok
}

// snapshot copies the enrolled owner set under lock for lock-free tick iteration.
func (h *Heartbeater) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.owners))
	for o := range h.owners {
		out = append(out, o)
	}
	return out
}

// Run ticks until ctx is cancelled, re-bumping every enrolled owning
// registration's presence key each tick. Blocks — run it in a goroutine. A
// failed bump is LOGGED, never returned: a single missed tick is absorbed by
// the TTL margin, and a loop that exited on the first transient blip would
// defeat the liveness it exists to maintain.
func (h *Heartbeater) Run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, owner := range h.snapshot() {
				if err := h.reg.Heartbeat(ctx, owner); err != nil {
					h.logger.Warn("ownership: heartbeat tick failed",
						slog.String("owner", owner), slog.Any("error", err))
				}
			}
		}
	}
}
