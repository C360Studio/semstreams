package rule

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

func TestRuleRuntimeDrainIsSafeAfterHotReloadInitializationFailure(t *testing.T) {
	scheduler := newSchedulerForTest(t, &recordingExecutor{})
	if err := scheduler.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{
		logger:        slog.Default(),
		cronScheduler: scheduler,
	}

	hotReloadMgr := processor.startHotReloadManager(t.Context())
	if hotReloadMgr != nil {
		t.Fatal("hot-reload manager returned after initialization failed")
	}
	processor.drainRuleRuntime(hotReloadMgr)

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.started {
		t.Fatal("cron scheduler remained started after partial rule-runtime drain")
	}
	if scheduler.dispatch != nil || scheduler.dispatchDone != nil {
		t.Fatal("cron scheduler retained dispatcher handles after partial rule-runtime drain")
	}
}

func TestDrainRuleRuntimeJoinsHotReloadBeforeStoppingCron(t *testing.T) {
	cfg := mustTestConfig(t, t.Name())
	processor, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatal(err)
	}

	scheduler := newSchedulerForTest(t, &recordingExecutor{})
	oldDefinition := validCronDef()
	oldDefinition.ID = "replace-me"
	oldRule, err := NewCronRule(oldDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Register(oldRule); err != nil {
		t.Fatal(err)
	}
	runCtx, runCancel := context.WithCancel(t.Context())
	t.Cleanup(runCancel)
	if err := scheduler.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	processor.cronScheduler = scheduler
	processor.cronRules[oldDefinition.ID] = oldRule
	processor.ruleDefinitions[oldDefinition.ID] = oldDefinition
	processor.ruleConfigs[oldDefinition.ID] = definitionMap(t, oldDefinition)

	newDefinition := oldDefinition
	newDefinition.Schedule = "*/5 * * * *"
	bucket := newMockKVBucket()
	encoded, err := json.Marshal(newDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Put(t.Context(), "rules."+newDefinition.ID, encoded); err != nil {
		t.Fatal(err)
	}
	rcm := NewConfigManager(processor, nil, nil)
	rcm.kvStore = new(natsclient.Client).NewKVStore(bucket)
	hotReloadCtx, hotReloadCancel := context.WithCancel(t.Context())
	rcm.mu.Lock()
	rcm.cancel = hotReloadCancel
	rcm.mu.Unlock()

	var reconcileErr atomic.Pointer[error]
	rcm.wg.Add(1)
	go func() {
		defer rcm.wg.Done()
		if err := rcm.reconcileFromKV(hotReloadCtx); err != nil {
			reconcileErr.Store(&err)
		}
	}()

	scheduler.mu.Lock()
	schedulerLocked := true
	defer func() {
		if schedulerLocked {
			scheduler.mu.Unlock()
		}
		scheduler.Stop()
	}()
	waitForLockContention(t, func() bool {
		if !processor.mu.TryRLock() {
			return true
		}
		processor.mu.RUnlock()
		return false
	})

	drained := make(chan struct{})
	go func() {
		processor.drainRuleRuntime(rcm)
		close(drained)
	}()
	waitForLockContention(t, func() bool {
		rcm.mu.RLock()
		defer rcm.mu.RUnlock()
		return rcm.cancel == nil
	})
	select {
	case <-drained:
		t.Fatal("runtime drain returned before admitted hot-reload reconcile")
	default:
	}
	if scheduler.stopping {
		t.Fatal("cron scheduler fenced before hot-reload reconcile joined")
	}

	scheduler.mu.Unlock()
	schedulerLocked = false
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime drain did not complete")
	}
	if stored := reconcileErr.Load(); stored != nil {
		t.Fatalf("reconcile failed: %v", *stored)
	}
	if got := processor.ruleDefinitions[newDefinition.ID].Schedule; got != newDefinition.Schedule {
		t.Fatalf("processor schedule = %q, want %q", got, newDefinition.Schedule)
	}
	if got := scheduler.entries[newDefinition.ID].rule.ScheduleString(); got != newDefinition.Schedule {
		t.Fatalf("scheduler schedule = %q, want %q", got, newDefinition.Schedule)
	}
}

func definitionMap(t *testing.T, definition Definition) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func waitForLockContention(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("synchronization condition was not reached")
		}
		runtime.Gosched()
	}
}
