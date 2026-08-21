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

	mu      sync.Mutex
	entries map[string]*cronEntry

	lifecycleMu   sync.Mutex
	lifecycleUsed bool
	startDone     chan struct{}
	stopDone      chan struct{}
	cancel        context.CancelFunc
	registerFence bool
	dispatchMu    sync.Mutex
	dispatchQueue []cronDispatch
	dispatchWake  chan struct{}
	dispatchDone  chan struct{}
	dispatchFence bool
}

type cronDispatch struct {
	run    func(context.Context) error
	result chan error
}

type cronStopContext struct{ done <-chan struct{} }

func (c cronStopContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c cronStopContext) Done() <-chan struct{}       { return c.done }
func (c cronStopContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}
func (cronStopContext) Value(any) any { return nil }

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
	s.lifecycleMu.Lock()
	if s.registerFence || s.stopDone != nil {
		s.lifecycleMu.Unlock()
		return errors.New("cron scheduler: registration admission is closed")
	}
	if !rule.Enabled() {
		s.lifecycleMu.Unlock()
		s.logger.Debug("Skipping disabled cron rule", "rule_id", rule.ID())
		return nil
	}

	s.mu.Lock()
	if _, exists := s.entries[rule.ID()]; exists {
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		return fmt.Errorf("cron rule %s already registered", rule.ID())
	}

	entry := &cronEntry{rule: rule}
	ruleID := rule.ID() // captured by closure; rule is stable for the entry's lifetime

	job := cronlib.FuncJob(func() {
		s.fire(ruleID)
	})

	entry.entryID = s.cron.Schedule(rule.Schedule(), job)
	s.entries[ruleID] = entry
	s.mu.Unlock()
	s.lifecycleMu.Unlock()

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

// Start kicks off the scheduler ticker. The supplied context is captured
// and passed to every subsequent fire callback so action dispatches
// inherit the processor's cancellation. Calling Start twice is an error;
// the scheduler is single-shot per Start/Stop cycle.
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
		return fmt.Errorf("cron scheduler: Start context already canceled: %w", err)
	}
	s.lifecycleMu.Lock()
	if s.lifecycleUsed {
		s.lifecycleMu.Unlock()
		return errors.New("cron scheduler: already started")
	}
	s.lifecycleUsed = true
	s.startDone = make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.dispatchWake = make(chan struct{}, 1)
	s.dispatchDone = make(chan struct{})
	s.dispatchFence = false
	s.lifecycleMu.Unlock()

	go s.runDispatcher(runCtx)

	s.restoreFromTracker(ctx)

	s.cron.Start()
	s.metrics.recordSchedulerRunning(true)
	s.logger.Info("Cron scheduler started", "registered_rules", s.RegisteredCount())
	s.lifecycleMu.Lock()
	close(s.startDone)
	s.startDone = nil
	s.lifecycleMu.Unlock()
	return nil
}

func (s *CronScheduler) runDispatcher(ctx context.Context) {
	defer close(s.dispatchDone)
	for {
		select {
		case <-ctx.Done():
			s.failDispatchQueue(ctx.Err())
			return
		case <-s.dispatchWake:
			for {
				s.dispatchMu.Lock()
				if len(s.dispatchQueue) == 0 {
					s.dispatchMu.Unlock()
					break
				}
				dispatch := s.dispatchQueue[0]
				s.dispatchQueue = s.dispatchQueue[1:]
				s.dispatchMu.Unlock()
				dispatch.result <- dispatch.run(ctx)
				close(dispatch.result)
			}
		}
	}
}

func (s *CronScheduler) failDispatchQueue(err error) {
	s.dispatchMu.Lock()
	queue := s.dispatchQueue
	s.dispatchQueue = nil
	s.dispatchMu.Unlock()
	for _, dispatch := range queue {
		dispatch.result <- err
		close(dispatch.result)
	}
}

func (s *CronScheduler) submitDispatch(run func(context.Context) error) error {
	dispatch := cronDispatch{run: run, result: make(chan error, 1)}
	s.dispatchMu.Lock()
	if s.dispatchFence || s.dispatchWake == nil {
		s.dispatchMu.Unlock()
		return errors.New("cron scheduler: dispatch admission is closed")
	}
	select {
	case <-s.dispatchDone:
		s.dispatchMu.Unlock()
		return errors.New("cron scheduler: dispatcher stopped")
	default:
	}
	s.dispatchQueue = append(s.dispatchQueue, dispatch)
	wake := s.dispatchWake
	s.dispatchMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return <-dispatch.result
}

func (s *CronScheduler) fenceDispatch() <-chan error {
	barrier := cronDispatch{run: func(context.Context) error { return nil }, result: make(chan error, 1)}
	s.dispatchMu.Lock()
	s.dispatchFence = true
	if s.dispatchDone == nil {
		s.dispatchMu.Unlock()
		barrier.result <- nil
		close(barrier.result)
		return barrier.result
	}
	select {
	case <-s.dispatchDone:
		s.dispatchMu.Unlock()
		barrier.result <- nil
		close(barrier.result)
		return barrier.result
	default:
	}
	s.dispatchQueue = append(s.dispatchQueue, barrier)
	wake := s.dispatchWake
	s.dispatchMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return barrier.result
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

// Stop signals the scheduler to halt. Its returned settlement context closes
// after robfig callbacks and every admitted dispatcher action have completed.
// Callers should select on it with their shutdown deadline. Calling Stop on a
// never-started scheduler is safe.
func (s *CronScheduler) Stop() context.Context {
	var settlement <-chan struct{}
	for {
		s.lifecycleMu.Lock()
		if s.startDone != nil {
			startDone := s.startDone
			s.lifecycleMu.Unlock()
			<-startDone
			continue
		}
		if s.stopDone != nil {
			stopDone := s.stopDone
			s.lifecycleMu.Unlock()
			return cronStopContext{done: stopDone}
		}
		s.lifecycleUsed = true
		s.stopDone = make(chan struct{})
		s.registerFence = true
		stopDone := s.stopDone
		settlement = stopDone
		cancel := s.cancel
		dispatchDone := s.dispatchDone
		s.lifecycleMu.Unlock()

		barrier := s.fenceDispatch()
		nativeStop := s.cron.Stop()
		go func() {
			<-nativeStop.Done()
			<-barrier
			if cancel != nil {
				cancel()
			}
			if dispatchDone != nil {
				<-dispatchDone
			}
			close(stopDone)
		}()
		break
	}
	s.metrics.recordSchedulerRunning(false)
	s.logger.Info("Cron scheduler stopping")
	return cronStopContext{done: settlement}
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
	if !entry.inflight.CompareAndSwap(false, true) {
		s.metrics.recordFire(ruleID, cronFireStatusInflightSkipped, 0)
		s.logger.Warn("Cron rule fire skipped (previous fire still running)",
			"rule_id", ruleID)
		return
	}
	defer entry.inflight.Store(false)

	if err := s.submitDispatch(func(ctx context.Context) error {
		s.dispatchAndRecord(ctx, ruleID, entry, previousFiredNanos)
		return nil
	}); err != nil {
		s.logger.Debug("Cron rule fire rejected by runtime coordinator", "rule_id", ruleID, "error", err)
	}
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
