package rule

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/pkg/errs"
)

// parkedStops counts goroutines blocked in the select inside
// (*ConfigManager).Stop, which is the completion-fence wait bounded by ctx. Reading the
// goroutine dump is the synchronization: it observes that a Stop has reached
// the fence instead of guessing with a sleep.
func parkedStops() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for _, goroutine := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(goroutine, "[select") &&
			strings.Contains(goroutine, "rule.(*ConfigManager).Stop(") {
			count++
		}
	}
	return count
}

// waitForParkedStops returns once want Stops are parked on the fence. A Stop
// that returns first has skipped the fence, which fails the test.
func waitForParkedStops(t *testing.T, want int, stopped <-chan bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for parkedStops() < want {
		select {
		case <-stopped:
			t.Fatal("Stop returned while the work it must join was still running")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d Stops parked on the completion fence within 10s", want)
		}
		runtime.Gosched()
	}
}

// seedBlockingTarget holds Start inside seeding until released.
type seedBlockingTarget struct {
	fakeRuleTarget
	entered  chan struct{}
	release  chan struct{}
	returned atomic.Bool
}

func (s *seedBlockingTarget) LoadedRuleDefinitions() map[string]Definition {
	close(s.entered)
	<-s.release
	s.returned.Store(true)
	return nil
}

// recordingLogHandler records every log message it handles.
type recordingLogHandler struct {
	mu       sync.Mutex
	messages []string
}

func (h *recordingLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingLogHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordingLogHandler) WithGroup(string) slog.Handler            { return h }
func (h *recordingLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	h.messages = append(h.messages, record.Message)
	h.mu.Unlock()
	return nil
}

func (h *recordingLogHandler) logged(prefix string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, message := range h.messages {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

// A Stop that races Start's seeding cancels it and returns only after Start has
// released the target it was seeding. Codex round 1 on #1188, finding 2.
func TestConfigManagerStopDuringSeedingJoinsStart(t *testing.T) {
	logs := &recordingLogHandler{}
	rcm, err := NewConfigManager(slog.New(logs))
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	target := &seedBlockingTarget{
		fakeRuleTarget: fakeRuleTarget{applied: make(chan map[string]any, 1)},
		entered:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	startErr := make(chan error, 1)
	go func() { startErr <- rcm.Start(context.Background(), []HotReloadTarget{target}) }()
	<-target.entered

	stopped := make(chan bool, 1)
	go func() {
		stopConfigManagerWithinBudget(t, rcm)
		stopped <- target.returned.Load()
	}()
	waitForParkedStops(t, 1, stopped)
	close(target.release)

	select {
	case released := <-stopped:
		if !released {
			t.Fatal("Stop returned before Start released the target")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return within 10s of the target's release")
	}
	if err := <-startErr; err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case changes := <-target.applied:
		t.Fatalf("a cancelled Start must not reconcile, got %v", changes)
	default:
	}
	// The family is never bound, so any reconcile Start attempted would fail
	// at ListRules and log before reaching an apply: the absence of that log
	// line, not of an apply, is what shows Start skipped the reconcile.
	if !logs.logged("Rule configuration hot reload started") {
		t.Fatal("the recording handler saw no Start log; the reconcile observation below would be vacuous")
	}
	if logs.logged("Initial rule reconcile failed") {
		t.Fatal("a cancelled Start attempted its initial reconcile")
	}
}

// blockingLogHandler blocks the goroutine that logs message until released.
type blockingLogHandler struct {
	message string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *blockingLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *blockingLogHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *blockingLogHandler) WithGroup(string) slog.Handler            { return h }
func (h *blockingLogHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message == h.message {
		h.once.Do(func() { close(h.entered) })
		<-h.release
	}
	return nil
}

// Two concurrent Stops both wait for the reconcile loop to exit: the second
// joins the same completion fence rather than returning while the first is
// still joining. Codex round 1 on #1188, finding 2.
func TestConfigManagerConcurrentStopsBothJoinTheLoop(t *testing.T) {
	// The family is unbound, so a reconcile fails at ListRules and the loop
	// logs the failure; the handler holds the loop there.
	handler := &blockingLogHandler{
		message: "Rule hot-reload reconcile failed",
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	rcm, err := NewConfigManager(slog.New(handler))
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	target := &fakeRuleTarget{applied: make(chan map[string]any, 1)}
	if err := rcm.Start(context.Background(), []HotReloadTarget{target}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rcm.lifecycleMu.Lock()
	done := rcm.done
	rcm.lifecycleMu.Unlock()
	if done == nil {
		t.Fatal("Start with a target must run the reconcile loop")
	}
	rcm.onRuleEntry(config.KeyFamilyEntry{})
	<-handler.entered

	stopped := make(chan bool, 2)
	for range 2 {
		go func() {
			stopConfigManagerWithinBudget(t, rcm)
			select {
			case <-done:
				stopped <- true
			default:
				stopped <- false
			}
		}()
	}
	waitForParkedStops(t, 2, stopped)
	close(handler.release)

	for range 2 {
		select {
		case loopExited := <-stopped:
			if !loopExited {
				t.Fatal("a Stop returned before the reconcile loop exited")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a Stop did not return within 10s of the loop's release")
		}
	}
}

// Stop refuses a nil context before any action: the manager is not cancelled.
func TestConfigManagerStopRefusesANilContext(t *testing.T) {
	rcm, err := NewConfigManager(slog.Default())
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	//nolint:staticcheck // SA1012: a nil context is the input under test.
	if err := rcm.Stop(nil); !errs.IsInvalid(err) {
		t.Fatalf("Stop(nil) = %v, want an invalid error", err)
	}
	rcm.lifecycleMu.Lock()
	terminal := rcm.terminal
	rcm.lifecycleMu.Unlock()
	if terminal {
		t.Fatal("a refused Stop marked the manager terminal")
	}
}
