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
)

// parkedStops counts goroutines blocked in a channel receive inside
// (*ConfigManager).Stop, which is the completion-fence wait. Reading the
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
		if strings.Contains(goroutine, "[chan receive") &&
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

// A Stop that races Start's seeding cancels it and returns only after Start has
// released the target it was seeding. Codex round 1 on #1188, finding 2.
func TestConfigManagerStopDuringSeedingJoinsStart(t *testing.T) {
	rcm, err := NewConfigManager(slog.Default())
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
		_ = rcm.Stop()
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
			_ = rcm.Stop()
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
