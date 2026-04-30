package rule

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// recordingExecutor is a test-only ActionExecutorInterface that records every
// dispatched action. Concurrency-safe so the scheduler's goroutine model can
// exercise it without races.
type recordingExecutor struct {
	mu      sync.Mutex
	calls   []Action
	errOnce error // returned for the first call only when set; subsequent calls succeed
	delay   time.Duration
	panicOn string // panic when action.Subject equals this token
}

func (r *recordingExecutor) Execute(_ context.Context, action Action, _ *ExecutionContext) error {
	if r.panicOn != "" && action.Subject == r.panicOn {
		panic("recordingExecutor forced panic")
	}
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, action)
	if r.errOnce != nil {
		err := r.errOnce
		r.errOnce = nil
		return err
	}
	return nil
}

func (r *recordingExecutor) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newSchedulerForTest(t *testing.T, exec ActionExecutorInterface) *CronScheduler {
	t.Helper()
	s, err := NewCronScheduler(exec, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewCronScheduler = %v, want nil", err)
	}
	return s
}

func cronRuleForTest(t *testing.T, mutate func(*Definition)) *CronRule {
	t.Helper()
	def := validCronDef()
	def.ID = "rule-" + t.Name()
	if mutate != nil {
		mutate(&def)
	}
	r, err := NewCronRule(def)
	if err != nil {
		t.Fatalf("NewCronRule = %v, want nil", err)
	}
	return r
}

func TestNewCronScheduler_RejectsNilExecutor(t *testing.T) {
	_, err := NewCronScheduler(nil, nil, slog.Default())
	if err == nil {
		t.Fatal("err = nil, want non-nil for nil executor")
	}
}

func TestCronScheduler_RegisterAndDeregister(t *testing.T) {
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, nil)

	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v, want nil", err)
	}
	if got := s.RegisteredCount(); got != 1 {
		t.Errorf("RegisteredCount = %d, want 1", got)
	}

	s.Deregister(rule.ID())
	if got := s.RegisteredCount(); got != 0 {
		t.Errorf("RegisteredCount after Deregister = %d, want 0", got)
	}

	// Deregister of unknown rule is a no-op (hot-reload calls it
	// unconditionally).
	s.Deregister("does-not-exist")
}

func TestCronScheduler_RegisterDuplicate(t *testing.T) {
	s := newSchedulerForTest(t, &recordingExecutor{})
	rule := cronRuleForTest(t, nil)

	if err := s.Register(rule); err != nil {
		t.Fatalf("first Register = %v, want nil", err)
	}
	if err := s.Register(rule); err == nil {
		t.Fatal("second Register err = nil, want non-nil")
	}
}

func TestCronScheduler_RegisterSkipsDisabled(t *testing.T) {
	s := newSchedulerForTest(t, &recordingExecutor{})
	rule := cronRuleForTest(t, func(d *Definition) { d.Enabled = false })

	if err := s.Register(rule); err != nil {
		t.Fatalf("Register disabled = %v, want nil (silent skip)", err)
	}
	if got := s.RegisteredCount(); got != 0 {
		t.Errorf("RegisteredCount = %d, want 0 (disabled rule should not be registered)", got)
	}
}

func TestCronScheduler_RegisterRejectsNil(t *testing.T) {
	s := newSchedulerForTest(t, &recordingExecutor{})
	if err := s.Register(nil); err == nil {
		t.Fatal("Register(nil) err = nil, want non-nil")
	}
}

func TestCronScheduler_StartTwiceFails(t *testing.T) {
	s := newSchedulerForTest(t, &recordingExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("first Start = %v, want nil", err)
	}
	defer func() {
		stopCtx := s.Stop()
		<-stopCtx.Done()
	}()

	if err := s.Start(ctx); err == nil {
		t.Fatal("second Start err = nil, want non-nil")
	}
}

func TestCronScheduler_StartRejectsNilContext(t *testing.T) {
	s := newSchedulerForTest(t, &recordingExecutor{})
	if err := s.Start(nil); err == nil {
		t.Fatal("Start(nil) err = nil, want non-nil")
	}
}

func TestCronScheduler_StopOnNeverStartedIsSafe(t *testing.T) {
	s := newSchedulerForTest(t, &recordingExecutor{})
	stopCtx := s.Stop()
	select {
	case <-stopCtx.Done():
		// Already-closed context; expected.
	default:
		t.Fatal("Stop() on never-started scheduler returned an open context")
	}
}

// fire() is exercised directly because waiting for a real cron tick adds
// minutes of test latency. The scheduler's contract for fire() is the same
// whether triggered by robfig or test code.

func TestCronScheduler_FireDispatchesAllActions(t *testing.T) {
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Actions = []Action{
			{Type: ActionTypePublish, Subject: "a"},
			{Type: ActionTypePublish, Subject: "b"},
		}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	// Start sets parentCtx; required by fire's nil-check.
	ctx := context.Background()
	s.parentCtx = ctx

	s.fire(rule.ID())

	if got := exec.callCount(); got != 2 {
		t.Errorf("Execute call count = %d, want 2 (one per action)", got)
	}
}

func TestCronScheduler_FireFireEveryNGate(t *testing.T) {
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.FireEveryNEvents = 3
		d.Actions = []Action{{Type: ActionTypePublish, Subject: "a"}}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	// Tick 6 times. With N=3, only ticks 3 and 6 fire actions → 2 dispatches.
	for i := 0; i < 6; i++ {
		s.fire(rule.ID())
	}
	if got := exec.callCount(); got != 2 {
		t.Errorf("Execute calls = %d, want 2 (every-3 gate over 6 ticks)", got)
	}
}

func TestCronScheduler_FireCooldownGate(t *testing.T) {
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Cooldown = "10s"
		d.Actions = []Action{{Type: ActionTypePublish, Subject: "a"}}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	// First fire dispatches; second within 10s is skipped by cooldown.
	s.fire(rule.ID())
	s.fire(rule.ID())
	if got := exec.callCount(); got != 1 {
		t.Errorf("Execute calls = %d, want 1 (second fire suppressed by cooldown)", got)
	}
}

func TestCronScheduler_FireInflightGuard(t *testing.T) {
	// A slow first fire still in flight when a second fire arrives must
	// drop the second fire — not queue or stack it. Verifies the atomic
	// CAS gate.
	exec := &recordingExecutor{delay: 200 * time.Millisecond}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Actions = []Action{{Type: ActionTypePublish, Subject: "a"}}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.fire(rule.ID()) }()
	// Give the first fire time to grab the inflight CAS.
	time.Sleep(20 * time.Millisecond)
	go func() { defer wg.Done(); s.fire(rule.ID()) }()
	wg.Wait()

	if got := exec.callCount(); got != 1 {
		t.Errorf("Execute calls = %d, want 1 (overlapping fire dropped by inflight guard)", got)
	}
}

func TestCronScheduler_FirePanicRecover(t *testing.T) {
	// A panicking action must not propagate out of fire(). The deferred
	// recover keeps robfig's internal goroutine alive.
	exec := &recordingExecutor{panicOn: "boom"}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Actions = []Action{
			{Type: ActionTypePublish, Subject: "boom"},
			{Type: ActionTypePublish, Subject: "after-panic"},
		}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	// If recover were missing this would crash the test goroutine.
	s.fire(rule.ID())
	// Note: we don't assert on call count after the panic — the second
	// action is intentionally not dispatched (panic unwinds the for-loop).
	// The only invariant is that the test process is still alive here.
}

func TestCronScheduler_FireUnknownRuleIsNoop(t *testing.T) {
	// Race window: a tick is delivered between Deregister and the fire
	// closure's map lookup. Dropping silently is correct.
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	s.parentCtx = context.Background()

	s.fire("never-registered")

	if got := exec.callCount(); got != 0 {
		t.Errorf("Execute calls = %d, want 0", got)
	}
}

func TestCronScheduler_FireSurvivesActionError(t *testing.T) {
	// One failing action does not stop sibling actions in the same tick —
	// matches StatefulEvaluator best-effort semantics.
	exec := &recordingExecutor{errOnce: errors.New("boom")}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Actions = []Action{
			{Type: ActionTypePublish, Subject: "first-fails"},
			{Type: ActionTypePublish, Subject: "second-runs"},
		}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	s.fire(rule.ID())

	if got := exec.callCount(); got != 2 {
		t.Errorf("Execute calls = %d, want 2 (best-effort: sibling action runs after error)", got)
	}
}

func TestCronScheduler_StartIntegratesWithRegister(t *testing.T) {
	// Smoke test: Start, Register, Deregister, Stop sequence — confirms
	// the lifecycle wiring without waiting for a cron tick.
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start = %v", err)
	}

	stopCtx := s.Stop()
	select {
	case <-stopCtx.Done():
		// Drained cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop drain timed out")
	}
}

func TestCronScheduler_StartActuallyFiresFromRobfig(t *testing.T) {
	// Closes the gap between TestCronScheduler_FireDispatchesAllActions
	// (calls fire() directly) and Chunk 6's testcontainer integration
	// test. Proves robfig's internal goroutine actually invokes the
	// closure we registered. Uses `@every 100ms` (robfig descriptor)
	// so a single 1.5s wait is enough to catch at least one fire even
	// on a busy CI host.
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)

	def := validCronDef()
	def.ID = "rule-" + t.Name()
	def.Schedule = "@every 100ms"
	rule, err := NewCronRule(def)
	if err != nil {
		t.Fatalf("NewCronRule = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start = %v", err)
	}
	defer func() {
		stopCtx := s.Stop()
		<-stopCtx.Done()
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if exec.callCount() >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("expected at least 1 fire from robfig within 1.5s, got %d", exec.callCount())
}

// Sanity check: atomic counters in cronEntry behave under contention. Not
// strictly needed (atomic.Int64 is std-lib), but documents the expectation
// that tickCount and lastFiredNanos are read/written without a per-entry
// lock.
func TestCronEntry_AtomicCountersAreSafe(t *testing.T) {
	var entry cronEntry
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry.tickCount.Add(1)
			entry.lastFiredNanos.Store(time.Now().UnixNano())
		}()
	}
	wg.Wait()
	if got := entry.tickCount.Load(); got != 100 {
		t.Errorf("tickCount = %d, want 100", got)
	}
	if entry.lastFiredNanos.Load() == 0 {
		t.Error("lastFiredNanos = 0, want non-zero after 100 stores")
	}
}

// newSchedulerWithTrackerForTest builds a scheduler wired to a real
// ScheduleTracker backed by an in-memory bucket, so persistence behaviour
// can be asserted without testcontainers.
func newSchedulerWithTrackerForTest(t *testing.T, exec ActionExecutorInterface) (*CronScheduler, *ScheduleTracker) {
	t.Helper()
	bucket := newMockKVBucket()
	tracker := NewScheduleTracker(bucket, slog.Default())
	s, err := NewCronScheduler(exec, tracker, slog.Default())
	if err != nil {
		t.Fatalf("NewCronScheduler = %v", err)
	}
	return s, tracker
}

// fire() must persist a last-fired record when a tracker is wired in.
// Skipped fires (cooldown/inflight/everyN) must not persist.
func TestCronScheduler_FirePersistsLastFiredRecord(t *testing.T) {
	exec := &recordingExecutor{}
	s, tracker := newSchedulerWithTrackerForTest(t, exec)
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	before := time.Now()
	s.fire(rule.ID())

	rec, err := tracker.LastFiredAt(context.Background(), rule.ID())
	if err != nil {
		t.Fatalf("LastFiredAt = %v, want nil", err)
	}
	if rec.RuleID != rule.ID() {
		t.Errorf("RuleID = %q, want %q", rec.RuleID, rule.ID())
	}
	if rec.ScheduleSpec != rule.ScheduleString() {
		t.Errorf("ScheduleSpec = %q, want %q", rec.ScheduleSpec, rule.ScheduleString())
	}
	if rec.LastFiredAt.Before(before.Add(-time.Second)) {
		t.Errorf("LastFiredAt = %v, want >= %v", rec.LastFiredAt, before)
	}
}

// fire() with no tracker must remain a clean no-op rather than panicking
// — the scheduler degrades gracefully when the bucket couldn't be
// created at startup.
func TestCronScheduler_FireWithNilTrackerNoops(t *testing.T) {
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec) // tracker is nil
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()
	s.fire(rule.ID()) // must not panic
	if exec.callCount() != 1 {
		t.Errorf("Execute calls = %d, want 1", exec.callCount())
	}
}

// fire() must persist EVERY successful dispatch, overwriting prior
// records. Verifies the timestamp advances across consecutive fires.
func TestCronScheduler_FireOverwritesPriorRecord(t *testing.T) {
	exec := &recordingExecutor{}
	s, tracker := newSchedulerWithTrackerForTest(t, exec)
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	s.fire(rule.ID())
	first, err := tracker.LastFiredAt(context.Background(), rule.ID())
	if err != nil {
		t.Fatalf("LastFiredAt after first = %v", err)
	}

	// Sleep past clock resolution so the second timestamp is definitively
	// newer. 2ms is comfortably > nanosecond clock granularity on every
	// supported OS without slowing CI.
	time.Sleep(2 * time.Millisecond)
	s.fire(rule.ID())
	second, err := tracker.LastFiredAt(context.Background(), rule.ID())
	if err != nil {
		t.Fatalf("LastFiredAt after second = %v", err)
	}
	if !second.LastFiredAt.After(first.LastFiredAt) {
		t.Errorf("second.LastFiredAt = %v, want after %v", second.LastFiredAt, first.LastFiredAt)
	}
}

// restoreFromTracker must skip rules with no persisted record — first
// deploy is "fresh start", not "infinitely many missed fires".
func TestCronScheduler_RestoreFromTracker_NoRecordSkips(t *testing.T) {
	exec := &recordingExecutor{}
	s, _ := newSchedulerWithTrackerForTest(t, exec)
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	// restoreFromTracker must not error or panic; it simply has nothing
	// to report.
	s.restoreFromTracker(context.Background())
}

// restoreFromTracker with a tracker but no rules registered must be a
// clean no-op; mirrors the bare-startup path before any cron rules
// are loaded.
func TestCronScheduler_RestoreFromTracker_NoRulesNoop(t *testing.T) {
	exec := &recordingExecutor{}
	s, _ := newSchedulerWithTrackerForTest(t, exec)
	s.restoreFromTracker(context.Background())
}

// restoreFromTracker with a nil tracker is a clean no-op (degraded mode).
func TestCronScheduler_RestoreFromTracker_NilTrackerNoop(t *testing.T) {
	exec := &recordingExecutor{}
	s := newSchedulerForTest(t, exec)
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.restoreFromTracker(context.Background())
}

// restoreFromTracker walks the recorded last-fired backwards and counts
// how many fires the rule's schedule expected since. Uses a high-cadence
// schedule (@every 1s) plus a record from 5 seconds ago to deterministic-
// ally land in the missed-fires regime without sleeping.
func TestCronScheduler_RestoreFromTracker_CountsExpectedFires(t *testing.T) {
	exec := &recordingExecutor{}
	s, tracker := newSchedulerWithTrackerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Schedule = "@every 1s"
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}

	// Record a fire 5 seconds ago. With @every 1s, detection should see
	// "several" missed fires; we don't assert an exact count because
	// schedule.Next semantics interact with wallclock granularity.
	five := time.Now().Add(-5 * time.Second)
	if err := tracker.RecordFire(context.Background(), rule.ID(), rule.ScheduleString(), five); err != nil {
		t.Fatalf("RecordFire = %v", err)
	}

	// Detection must complete without panicking and without dispatching
	// (log-only policy: no replay).
	s.restoreFromTracker(context.Background())
	if exec.callCount() != 0 {
		t.Errorf("Execute calls = %d, want 0 (missed-fire detection must NOT dispatch)", exec.callCount())
	}
}

// restoreFromTracker must seed entry.lastFiredNanos from the persisted
// record. This is the cross-restart cooldown / `$schedule.last_fired_at`
// fix: without hydration the first post-restart fire would behave as
// "never fired", losing both the cooldown gate's enforcement and the
// substitution context.
func TestCronScheduler_RestoreFromTracker_HydratesLastFiredNanos(t *testing.T) {
	exec := &recordingExecutor{}
	s, tracker := newSchedulerWithTrackerForTest(t, exec)
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}

	persisted := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	if err := tracker.RecordFire(context.Background(), rule.ID(), rule.ScheduleString(), persisted); err != nil {
		t.Fatalf("RecordFire = %v", err)
	}

	s.restoreFromTracker(context.Background())

	s.mu.Lock()
	entry, ok := s.entries[rule.ID()]
	s.mu.Unlock()
	if !ok {
		t.Fatalf("entry missing after restore")
	}
	got := entry.lastFiredNanos.Load()
	if got != persisted.UnixNano() {
		t.Errorf("lastFiredNanos = %d, want %d (hydration from persisted record)", got, persisted.UnixNano())
	}
}

// Cross-restart cooldown: a rule with a 1h cooldown that fired 1 minute
// before "restart" (simulated by hydrating the in-memory cache from a
// freshly-recorded persisted timestamp) must skip the next fire on the
// cooldown gate. Without hydration this skip would not happen and the
// post-restart fire would dispatch immediately, doubling actions.
func TestCronScheduler_RestoreFromTracker_EnforcesCooldownAcrossRestart(t *testing.T) {
	exec := &recordingExecutor{}
	s, tracker := newSchedulerWithTrackerForTest(t, exec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Cooldown = "1h"
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	// Pretend the previous process fired one minute ago and persisted.
	prev := time.Now().Add(-1 * time.Minute)
	if err := tracker.RecordFire(context.Background(), rule.ID(), rule.ScheduleString(), prev); err != nil {
		t.Fatalf("RecordFire = %v", err)
	}

	s.restoreFromTracker(context.Background())

	// First fire after restart must hit the cooldown gate (1m elapsed,
	// 1h cooldown).
	s.fire(rule.ID())
	if got := exec.callCount(); got != 0 {
		t.Errorf("Execute calls = %d, want 0 (cooldown should suppress post-restart fire)", got)
	}
}

// fire() must build an ExecutionContext with Schedule populated so cron
// action templates can use `$schedule.id`, `$schedule.spec`, and
// `$schedule.last_fired_at`. Verifies that the executor receives an ec
// whose substitution renders the cron-namespace tokens correctly.
func TestCronScheduler_FireBuildsScheduleContext(t *testing.T) {
	rec := &ecCapturingExecutor{}
	s, _ := newSchedulerWithTrackerForTest(t, rec)
	rule := cronRuleForTest(t, func(d *Definition) {
		d.Schedule = "0 9 * * MON"
		d.Actions = []Action{{Type: ActionTypePublish, Subject: "$schedule.id"}}
	})
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	s.fire(rule.ID())

	ec := rec.ec
	if ec == nil {
		t.Fatal("executor did not receive ExecutionContext")
	}
	if ec.Schedule == nil {
		t.Fatal("ec.Schedule = nil, want populated")
	}
	if ec.Schedule.ID != rule.ID() {
		t.Errorf("Schedule.ID = %q, want %q", ec.Schedule.ID, rule.ID())
	}
	if ec.Schedule.Spec != "0 9 * * MON" {
		t.Errorf("Schedule.Spec = %q, want %q", ec.Schedule.Spec, "0 9 * * MON")
	}
	// First fire: LastFiredAt is zero.
	if !ec.Schedule.LastFiredAt.IsZero() {
		t.Errorf("first fire LastFiredAt = %v, want zero", ec.Schedule.LastFiredAt)
	}
}

// Second fire's ScheduleContext.LastFiredAt must reflect the first
// fire's wallclock — the cooldown read and the substitution view of
// "previous fire" must be the same value.
func TestCronScheduler_FireScheduleContextSeesPriorFire(t *testing.T) {
	rec := &ecCapturingExecutor{}
	s, _ := newSchedulerWithTrackerForTest(t, rec)
	rule := cronRuleForTest(t, nil)
	if err := s.Register(rule); err != nil {
		t.Fatalf("Register = %v", err)
	}
	s.parentCtx = context.Background()

	s.fire(rule.ID())
	firstFireNanos := time.Now().UnixNano()
	rec.ec = nil // clear so the second fire's ec is captured fresh

	// Sleep past clock resolution so the second fire sees a strictly
	// earlier LastFiredAt.
	time.Sleep(2 * time.Millisecond)
	s.fire(rule.ID())

	if rec.ec == nil || rec.ec.Schedule == nil {
		t.Fatal("second fire did not deliver Schedule context")
	}
	if rec.ec.Schedule.LastFiredAt.IsZero() {
		t.Error("second fire LastFiredAt is zero, want the first fire's wallclock")
	}
	if rec.ec.Schedule.LastFiredAt.UnixNano() > firstFireNanos {
		t.Errorf("LastFiredAt = %d, want <= %d (must reflect first fire, not the in-progress second)",
			rec.ec.Schedule.LastFiredAt.UnixNano(), firstFireNanos)
	}
}

// ecCapturingExecutor is a recordingExecutor sibling that snapshots the
// last ExecutionContext it received. Lets tests assert on the ec the
// scheduler actually passed through.
//
// No mutex: callers invoke s.fire() synchronously from the test
// goroutine, so the write+read ordering is single-threaded by
// construction. Tests that fire concurrently (none today) should add
// their own synchronization or migrate to a lock-protected variant.
type ecCapturingExecutor struct {
	ec *ExecutionContext
}

func (e *ecCapturingExecutor) Execute(_ context.Context, _ Action, ec *ExecutionContext) error {
	e.ec = ec
	return nil
}
