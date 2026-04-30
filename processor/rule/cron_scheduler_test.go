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
	s, err := NewCronScheduler(exec, slog.Default())
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
	_, err := NewCronScheduler(nil, slog.Default())
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
