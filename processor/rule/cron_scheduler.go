// Package rule - Cron scheduler for time-driven rule firing.
//
// CronScheduler is the third firing path of the rule processor, parallel to
// message-driven and KV-watch-driven evaluation. It wraps robfig/cron/v3.Cron
// and dispatches each registered CronRule's actions through the shared
// ActionExecutor at the times described by the rule's cron expression.
//
// Lifecycle is owned by the rule processor:
//
//  1. Processor.Start (via initializeCronScheduler) constructs the scheduler
//     and registers the cron rules loaded by Initialize.
//  2. Processor.run launches the scheduler with Start(ctx); robfig spawns
//     its own internal goroutine that walks the schedule and dispatches
//     each fire callback on its own goroutine.
//  3. Processor.run defers Stop on shutdown, draining in-flight fires up
//     to the shutdown grace period.
//  4. Hot reload (applyRuleChanges) calls Register / Deregister under the
//     processor's mu.Lock; robfig supports live add/remove without a
//     scheduler restart.
//
// The fire path is best-effort: per-rule panic-recover keeps one bad action
// from crashing the scheduler goroutine, action errors are logged and do
// not stop sibling actions in the same tick. This matches StatefulEvaluator
// behaviour so the operator's mental model is consistent across rule kinds.
package rule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

// CronScheduler dispatches CronRule actions on a cron schedule. Methods are
// safe for concurrent use; the entries map is guarded by mu.
type CronScheduler struct {
	cron     *cronlib.Cron
	executor ActionExecutorInterface
	tracker  *ScheduleTracker
	metrics  *cronMetrics
	logger   *slog.Logger
	ready    func() bool

	mu           sync.Mutex
	entries      map[string]*cronEntry
	dispatch     chan cronFireRequest
	dispatchDone chan struct{}
	started      bool
	stopping     bool
}

// cronFireRequest is the narrow adapter between robfig's context-free callback
// and the Start-owned dispatcher. It carries no lifecycle authority.
type cronFireRequest struct {
	ruleID             string
	entry              *cronEntry
	previousFiredNanos int64
	done               chan struct{}
}

// cronEntry is the per-registered-rule state held by the scheduler.
//
// tickCount, lastFiredNanos, and inflight are read/written from the fire
// callback without holding mu so that a long-running fire on one rule
// doesn't serialise scheduling decisions for sibling rules.
type cronEntry struct {
	rule    *CronRule
	entryID cronlib.EntryID

	// tickCount counts every clock-tick delivered by robfig (before the
	// FireEveryN gate). Used to compute the every-Nth-tick cadence.
	tickCount atomic.Int64

	// lastFiredNanos records the wallclock of the last successful action
	// dispatch. Read by the cooldown gate and updated only after action
	// dispatch returns. Zero means "never fired."
	lastFiredNanos atomic.Int64

	// inflight is true while a fire callback is between the inflight-CAS
	// and the deferred reset. Skipping when already inflight prevents a
	// long-running publish_agent from queuing a second fire on the next
	// tick.
	inflight atomic.Bool
}

// CronSchedulerConfig groups the collaborators NewCronScheduler needs.
// Constructed by initializeCronScheduler in production; tests build it
// inline with whichever fields they exercise. Following the team's
// "4+ args → request struct" convention so future additions (timeouts,
// custom parsers, observers) don't grow a positional arg list.
type CronSchedulerConfig struct {
	// Executor is required. A nil executor is rejected because every
	// fire would silently no-op, hiding misconfiguration.
	Executor ActionExecutorInterface

	// Tracker is optional. Pass nil to run without persistence — no
	// cross-restart missed-fire detection, no cooldown hydration on
	// startup. Tests typically pass nil; the production processor
	// wires in a tracker bound to the RULE_SCHEDULES bucket.
	Tracker *ScheduleTracker

	// Metrics is optional. Pass nil to run without Prometheus
	// observability — every recordX call short-circuits on the nil
	// receiver. Tests that don't assert on metrics pass nil; the
	// production processor wires in metrics scoped to its registry.
	Metrics *cronMetrics

	// Logger is optional. Defaults to slog.Default() when nil so the
	// scheduler always has something to log to.
	Logger *slog.Logger

	// Ready is an optional fail-closed dispatch gate. Production binds it to
	// the Processor's authoritative graph-state guard; tests may omit it.
	Ready func() bool
}

// NewCronScheduler builds a scheduler that will dispatch actions through
// cfg.Executor. Returns an error only when cfg.Executor is nil — the
// other fields are all optional with documented degraded-mode
// behaviour.
func NewCronScheduler(cfg CronSchedulerConfig) (*CronScheduler, error) {
	if cfg.Executor == nil {
		return nil, errors.New("cron scheduler: executor is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &CronScheduler{
		// cronlib.New uses the default parser (POSIX 5-field + descriptors)
		// internally for AddFunc/AddJob, but we register via Schedule(),
		// passing the rule's pre-parsed Schedule so spec strings only need
		// to satisfy our own cronParser (cron_rule.go). The Cron's own
		// parser is therefore unused.
		cron:     cronlib.New(),
		executor: cfg.Executor,
		ready:    cfg.Ready,
		tracker:  cfg.Tracker,
		metrics:  cfg.Metrics,
		logger:   logger,
		entries:  make(map[string]*cronEntry),
	}, nil
}

// Register schedules a CronRule for periodic firing. Disabled rules are
// skipped silently (logged at Debug). Returns an error if the rule is
// already registered — callers performing hot-reload should call
// Deregister first.
func (s *CronScheduler) Register(rule *CronRule) error {
	if rule == nil {
		return errors.New("cron scheduler: nil rule")
	}
	if !rule.Enabled() {
		s.logger.Debug("Skipping disabled cron rule", "rule_id", rule.ID())
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return errors.New("cron scheduler: stopping")
	}

	if _, exists := s.entries[rule.ID()]; exists {
		return fmt.Errorf("cron rule %s already registered", rule.ID())
	}

	entry := &cronEntry{rule: rule}
	ruleID := rule.ID() // captured by closure; rule is stable for the entry's lifetime

	job := cronlib.FuncJob(func() { s.fire(ruleID) })

	entry.entryID = s.cron.Schedule(rule.Schedule(), job)
	s.entries[ruleID] = entry

	s.metrics.recordRuleRegistered()
	s.metrics.recordNextFire(ruleID, float64(rule.Schedule().Next(time.Now()).Unix()))

	s.logger.Info("Cron rule registered",
		"rule_id", ruleID,
		"schedule", rule.ScheduleString(),
		"action_count", len(rule.Actions()))
	return nil
}

// Deregister removes a cron rule from the scheduler. It is a no-op when
// the rule isn't registered, so hot-reload can call it unconditionally.
func (s *CronScheduler) Deregister(ruleID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[ruleID]
	if !ok {
		return
	}
	s.cron.Remove(entry.entryID)
	delete(s.entries, ruleID)
	s.metrics.recordRuleDeregistered()
	s.metrics.clearNextFire(ruleID)
	s.logger.Info("Cron rule deregistered", "rule_id", ruleID)
}

// Start kicks off the scheduler ticker. The context is passed explicitly to
// the dispatcher and every admitted fire; it is never retained by the
// scheduler or by robfig's callback closure. Calling Start twice is an error.
//
// Before the ticker starts, Start runs a one-shot restoreFromTracker
// pass against the persisted last-fired records (when a tracker is
// configured). It seeds each entry's in-memory lastFiredNanos cache so
// the cooldown gate and `$schedule.last_fired_at` substitution behave
// correctly across restarts, and it logs a Warn for any rule whose
// schedule expected at least one fire between the persisted timestamp
// and now (log-only per ADR-031 product direction). Tracker failures
// are logged but do not block startup; a clean cron tick is more
// important than a perfect audit log.
func (s *CronScheduler) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("cron scheduler: Start requires a non-nil context")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cron scheduler: Start context is canceled: %w", err)
	}
	s.mu.Lock()
	if s.started || s.stopping {
		s.mu.Unlock()
		return errors.New("cron scheduler: already started")
	}
	dispatch := make(chan cronFireRequest)
	dispatchDone := make(chan struct{})
	s.dispatch = dispatch
	s.dispatchDone = dispatchDone
	s.started = true
	s.mu.Unlock()

	s.restoreFromTracker(ctx)

	go s.runDispatcher(ctx, dispatch, dispatchDone)
	s.cron.Start()
	s.metrics.recordSchedulerRunning(true)
	s.logger.Info("Cron scheduler started", "registered_rules", s.RegisteredCount())
	return nil
}

// runDispatcher bridges robfig callbacks into the Start lifetime. It remains
// available after ctx cancellation until Stop closes dispatch, allowing every
// callback admitted before the Stop fence to receive the exact canceled ctx,
// finish, and release robfig's own Stop join.
func (s *CronScheduler) runDispatcher(
	ctx context.Context,
	dispatch <-chan cronFireRequest,
	done chan<- struct{},
) {
	var fires sync.WaitGroup
	for request := range dispatch {
		fires.Add(1)
		go func() {
			defer fires.Done()
			s.runFire(ctx, request)
		}()
	}
	fires.Wait()
	close(done)
}

func (s *CronScheduler) runFire(ctx context.Context, request cronFireRequest) {
	defer close(request.done)
	defer request.entry.inflight.Store(false)
	s.dispatchAndRecord(ctx, request.ruleID, request.entry, request.previousFiredNanos)
}

// restoreFromTracker walks the registered rules, looks up each rule's
// last-fired record, and does two things:
//
//  1. Seeds entry.lastFiredNanos so the cooldown gate and the
//     $schedule.last_fired_at substitution see the persisted timestamp
//     immediately after restart, instead of treating the first
//     post-restart fire as "never fired".
//  2. Logs a Warn for any rule whose schedule expected at least one
//     fire between the persisted timestamp and now. Per ADR-031 the
//     policy is log-only: the next regular tick still happens on its
//     normal cadence.
//
// Rules with no persisted record are skipped — there's nothing to compare
// against, and treating "first deploy" as "infinitely many missed fires"
// would generate misleading noise.
func (s *CronScheduler) restoreFromTracker(ctx context.Context) {
	if s.tracker == nil {
		return
	}

	// Snapshot entries (not just rules) under lock so the hydration
	// step can write entry.lastFiredNanos without holding mu — the
	// atomic.Int64 is its own synchronisation. A concurrent Register/
	// Deregister between snapshot and iteration is benign: hydrating a
	// removed entry's atomic is a no-op from the cron ticker's
	// perspective (no callbacks fire for it), and Warn lines for a
	// removed rule are at worst a single confusing log entry per
	// restart. The benign-write argument is what matters; the call
	// chain is not actually serialised because cronScheduler.Start
	// runs on the processor's run() goroutine after rp.Start releases
	// rp.mu, so an ApplyConfigUpdate could race the snapshot.
	s.mu.Lock()
	entries := make([]*cronEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	s.mu.Unlock()

	// Capture now once: the audit horizon is a single instant, not
	// per-rule. Calling time.Now() per iteration would let the horizon
	// drift across the loop and slightly under-count missed fires for
	// rules processed late in a slow startup.
	now := time.Now()
	for _, entry := range entries {
		rule := entry.rule
		rec, err := s.tracker.LastFiredAt(ctx, rule.ID())
		if err != nil {
			if errors.Is(err, ErrScheduleRecordNotFound) {
				continue
			}
			s.logger.Warn("Failed to read last-fired record on startup",
				"rule_id", rule.ID(),
				"error", err)
			continue
		}

		// Hydrate the in-memory cache. Done unconditionally on a
		// successful tracker read so the cooldown gate immediately
		// reflects pre-restart state and the $schedule.last_fired_at
		// substitution renders the persisted timestamp on the first
		// post-restart fire.
		entry.lastFiredNanos.Store(rec.LastFiredAt.UnixNano())

		// Count expected fires between rec.LastFiredAt and now by walking
		// the rule's schedule. A bounded loop prevents pathological log
		// floods if a rule has been disabled across years of downtime —
		// after the cap we report "many" rather than the true count.
		const missedCap = 100
		missed := 0
		next := rule.Schedule().Next(rec.LastFiredAt)
		var lastExpected time.Time
		for !next.After(now) {
			missed++
			lastExpected = next
			if missed >= missedCap {
				break
			}
			next = rule.Schedule().Next(next)
		}

		if missed == 0 {
			continue
		}

		capped := missed >= missedCap
		s.metrics.recordMissedFire(rule.ID(), missed)
		if capped {
			s.metrics.recordMissedFireCapped(rule.ID())
		}

		s.logger.Warn("Cron rule missed fires while scheduler was offline",
			"rule_id", rule.ID(),
			"schedule", rule.ScheduleString(),
			"last_fired_at", rec.LastFiredAt,
			"missed_fires", missed,
			"last_expected_fire", lastExpected,
			"capped", capped)
	}
}

// Stop fences new fires, waits for robfig callbacks and their admitted action
// work, then closes and joins the Start-owned dispatcher. Calling Stop on a
// never-started scheduler is safe.
func (s *CronScheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	if s.stopping {
		done := s.dispatchDone
		s.mu.Unlock()
		<-done
		return
	}
	s.stopping = true
	dispatch := s.dispatch
	dispatchDone := s.dispatchDone
	s.mu.Unlock()

	robfigDone := s.cron.Stop()
	<-robfigDone.Done()
	close(dispatch)
	<-dispatchDone

	s.mu.Lock()
	s.started = false
	s.dispatch = nil
	s.dispatchDone = nil
	s.mu.Unlock()
	s.metrics.recordSchedulerRunning(false)
	s.logger.Info("Cron scheduler stopped")
}

// RegisteredCount returns the number of rules currently registered. Used
// by the processor for logging and (Chunk 5) the registered-rules gauge.
func (s *CronScheduler) RegisteredCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// fire is the per-tick callback installed for every rule. The four gates
// (existence, FireEveryN, cooldown, inflight) keep dispatch correct under
// hot-reload, slow actions, and overlapping ticks; the deferred recover
// keeps a panicking action from killing the scheduler goroutine.
func (s *CronScheduler) fire(ruleID string) {
	if s.ready != nil && !s.ready() {
		return
	}
	s.mu.Lock()
	entry, ok := s.entries[ruleID]
	s.mu.Unlock()
	if !ok {
		// Rule was deregistered between the tick scheduling and now;
		// dropping silently is correct — robfig will not deliver future
		// ticks for the removed entry.
		return
	}
	tick := entry.tickCount.Add(1)

	// FireEveryN gate — every Nth tick fires actions; intermediate ticks
	// are noop'd. N=0 and N=1 both mean "fire every tick" (matches
	// FireEveryNEvents semantics on expression rules).
	if n := entry.rule.FireEveryN(); n > 1 {
		if tick%int64(n) != 0 {
			s.logger.Debug("Cron rule tick skipped (every-N gate)",
				"rule_id", ruleID,
				"tick", tick,
				"every", n)
			return
		}
	}

	// Capture the previous fire timestamp once: the cooldown gate
	// reads it for its check, and the ScheduleContext built for
	// substitution below uses it as `$schedule.last_fired_at`. Reading
	// once keeps both views consistent with each other (no mid-fire
	// race where cooldown sees the prior timestamp but substitution
	// sees `time.Now()` because another fire snuck in).
	previousFiredNanos := entry.lastFiredNanos.Load()

	// Cooldown gate — skip if the previous fire's wallclock is too
	// recent. Defends against the operator-error case of a cron expression
	// that ticks faster than actions complete on average.
	if cooldown := entry.rule.Cooldown(); cooldown > 0 {
		if previousFiredNanos > 0 && time.Since(time.Unix(0, previousFiredNanos)) < cooldown {
			s.metrics.recordFire(ruleID, cronFireStatusCooldownSkipped, 0)
			s.logger.Debug("Cron rule fire skipped (cooldown)",
				"rule_id", ruleID,
				"cooldown", cooldown)
			return
		}
	}

	// Inflight guard — skip if a previous fire is still running. The CAS
	// is the source of truth for "currently dispatching"; cooldown above
	// is wallclock-based and orthogonal. Both are needed: cooldown handles
	// fast-cycling slow rules, inflight handles a fire that exceeded its
	// own period. Distinct status from cooldown_skipped because the
	// operator action is different: cooldown_skipped means "configured
	// throttling worked", inflight_skipped means "actions are slower
	// than the schedule, configure a cooldown or speed them up".
	s.mu.Lock()
	current, stillRegistered := s.entries[ruleID]
	dispatch := s.dispatch
	if !stillRegistered || current != entry || !s.started || s.stopping || dispatch == nil {
		s.mu.Unlock()
		return
	}
	if !entry.inflight.CompareAndSwap(false, true) {
		s.mu.Unlock()
		s.metrics.recordFire(ruleID, cronFireStatusInflightSkipped, 0)
		s.logger.Warn("Cron rule fire skipped (previous fire still running)",
			"rule_id", ruleID)
		return
	}

	request := cronFireRequest{
		ruleID:             ruleID,
		entry:              entry,
		previousFiredNanos: previousFiredNanos,
		done:               make(chan struct{}),
	}
	// Admission and delivery are one critical section. Stop uses the same
	// mutex to fence admission, so it cannot close dispatch behind a callback
	// that has already won the inflight gate but has not delivered its request.
	dispatch <- request
	s.mu.Unlock()
	<-request.done
}

// dispatchAndRecord is the post-gates portion of fire(). Extracted from
// fire() so the gate-heavy entry path stays under the function-length
// lint cap; behaviour is unchanged. Holds the deferred panic-recover
// (backstop only — cron-side code does not panic deliberately) and the
// metric / persistence side-effects that a fire produces regardless of
// whether the dispatch loop succeeded, errored, or panicked.
//
// status=panic on fires_total is a programming-bug signal distinct
// from status=error (expected downstream failures). An operator alert
// on rate(cron_rule_fires_total{status="panic"}[5m]) should page
// someone — if you see panics, fix the code, don't tune the alert.
//
// A panic mid-loop intentionally drops the remaining sibling actions
// in this tick — the next scheduled tick will retry from the top. The
// caller's deferred inflight reset runs after this recover (LIFO) so
// the next tick is not gated by the panicker.
func (s *CronScheduler) dispatchAndRecord(ctx context.Context, ruleID string, entry *cronEntry, previousFiredNanos int64) {
	dispatchStart := time.Now()
	status := cronFireStatusSuccess

	defer func() {
		if r := recover(); r != nil {
			status = cronFireStatusPanic
			s.logger.Error("Cron rule fire panicked",
				"rule_id", ruleID,
				"panic", r)
		}
		// Single source of truth for fires_total + fire_duration: the
		// deferred block runs whether we exited normally or via panic,
		// so the counter and histogram always reflect the actual
		// outcome.
		s.metrics.recordFire(ruleID, status, time.Since(dispatchStart).Seconds())
	}()

	rule := entry.rule
	actions := rule.Actions() // already a clone — safe to iterate

	// ExecutionContext is intentionally sparse for cron fires: no entity,
	// no related entity, no MatchState. The Schedule shim supplies the
	// cron-specific `$schedule.*` namespace; SubstituteVariables in
	// execution_context.go also handles `$now` natively. The future
	// per-entity fan-out extension (Definition.ForEach) populates
	// EntityID / Entity / State per iteration alongside Schedule —
	// neither shim leaks into the other.
	var lastFired time.Time
	if previousFiredNanos > 0 {
		lastFired = time.Unix(0, previousFiredNanos).UTC()
	}
	ec := &ExecutionContext{
		Schedule: &ScheduleContext{
			ID:          ruleID,
			Spec:        rule.ScheduleString(),
			LastFiredAt: lastFired,
		},
	}

	dispatchedOK := true
	for i, action := range actions {
		if err := s.executor.Execute(ctx, action, ec); err != nil {
			if errors.Is(err, ErrDenyVerdict) {
				// Deny is terminal for the fire cycle: subsequent actions do not run.
				// "denied" is operator-distinct from "error" — "a rule said no" vs
				// "things are broken". Alerting on status="error" will not surface
				// denials; operators who want a unified "rule did not complete normally"
				// view must also include status="denied" in their queries.
				status = cronFireStatusDenied
				s.logger.Info("Cron rule action denied; short-circuiting remaining actions",
					"rule_id", ruleID,
					"action_index", i,
					"action_type", action.Type,
					"error", err)
				break
			}
			dispatchedOK = false
			s.logger.Warn("Cron rule action failed",
				"rule_id", ruleID,
				"action_index", i,
				"action_type", action.Type,
				"error", err)
			// Best-effort: continue to subsequent actions so one bad
			// publish doesn't block sibling triple writes etc.
		}
	}
	if !dispatchedOK && status != cronFireStatusDenied {
		status = cronFireStatusError
	}

	// Record last-fired timestamp only after dispatch — both in-memory
	// (cooldown gate) and persisted to RULE_SCHEDULES KV (cross-restart
	// missed-fire detection). Persistence failures are logged but do
	// not affect dispatch correctness; the next successful fire will
	// overwrite the record.
	firedAt := time.Now()
	entry.lastFiredNanos.Store(firedAt.UnixNano())
	s.metrics.recordNextFire(ruleID, float64(rule.Schedule().Next(firedAt).Unix()))

	if s.tracker != nil {
		// Best-effort persistence: the cooldown gate uses entry.lastFiredNanos
		// (in-memory), so KV unavailability is purely an observability
		// concern. Worst case across a restart: missed-fire detection thinks
		// the rule fired one tick earlier than it did → at most one stray
		// Warn log on next startup, no dispatch impact.
		if err := s.tracker.RecordFire(ctx, ruleID, rule.ScheduleString(), firedAt); err != nil {
			s.logger.Warn("Failed to persist cron rule fire timestamp",
				"rule_id", ruleID,
				"error", err)
		}
	}

	if dispatchedOK {
		s.logger.Debug("Cron rule fired",
			"rule_id", ruleID,
			"actions", len(actions))
	}
}
