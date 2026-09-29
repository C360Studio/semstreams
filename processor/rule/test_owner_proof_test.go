package rule

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"testing/synctest"
	"time"
)

// The action is admitted by the real scheduler, and its native Stop must wait
// for the action while the owner's private Start child is still live.
func TestCronSchedulerTestOwnerJoinsAdmittedFireBeforeStartCancel(t *testing.T) {
	operationCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	exec := &blockingContextExecutor{started: make(chan context.Context, 1), release: make(chan struct{})}
	scheduler := newUnstartedSchedulerForTest(t, exec)
	owner := newCronSchedulerTestOwner(scheduler)
	defer owner.finish(operationCtx, t)
	rule := cronRuleForTest(t, nil)
	if err := scheduler.Register(rule); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(owner.startContext(operationCtx)); err != nil {
		t.Fatal(err)
	}

	fireDone := make(chan struct{})
	go func() { defer close(fireDone); scheduler.fire(rule.ID()) }()
	released := false
	var stopDone chan error
	stopObserved := false
	defer func() {
		if !released {
			close(exec.release)
		}
		joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
		defer cancelJoin()
		select {
		case <-fireDone:
		case <-joinCtx.Done():
			t.Error("test-owned scheduler fire did not join after release")
		}
		if !stopObserved {
			if stopDone != nil {
				select {
				case <-stopDone:
				case <-joinCtx.Done():
					t.Error("concrete scheduler Stop did not return after release")
				}
			}
		}
	}()
	var fireCtx context.Context
	select {
	case fireCtx = <-exec.started:
	case <-operationCtx.Done():
		t.Fatal("scheduler fire was not admitted before operation deadline")
	}
	stopDone = make(chan error, 1)
	go func() { stopDone <- owner.stop(operationCtx) }()
	for {
		scheduler.lifecycleMu.Lock()
		stopping := scheduler.stopping
		scheduler.lifecycleMu.Unlock()
		if stopping {
			break
		}
		select {
		case err := <-stopDone:
			stopObserved = true
			t.Fatalf("native Stop returned before admitting terminal phase: %v", err)
		case <-operationCtx.Done():
			t.Fatal("native Stop did not enter terminal phase")
		default:
			runtime.Gosched()
		}
	}
	select {
	case err := <-stopDone:
		stopObserved = true
		t.Fatalf("native Stop returned before admitted fire completed: %v", err)
	default:
	}
	if err := fireCtx.Err(); err != nil {
		t.Fatalf("Start child canceled before native Stop settled fire: %v", err)
	}
	close(exec.release)
	released = true
	select {
	case <-fireDone:
	case <-operationCtx.Done():
		t.Fatal("scheduler fire did not finish after release")
	}
	var stopErr error
	select {
	case stopErr = <-stopDone:
		stopObserved = true
	case <-operationCtx.Done():
		t.Fatal("concrete scheduler Stop did not return after released fire")
	}
	if stopErr != nil {
		stopObserved = true
		t.Fatalf("native Stop after released fire: %v", stopErr)
	}
	if err := fireCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("private Start child after Stop = %v, want cancellation", err)
	}
}

// A native Stop deadline is an attempted terminal call even when the scheduler
// retains its own retry capability. The test owns the deliberately blocked fire.
func TestCronSchedulerTestOwnerExpiredStopIsFinalAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		operationCtx, cancelOperation := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancelOperation()
		exec := &blockingContextExecutor{started: make(chan context.Context, 1), release: make(chan struct{})}
		scheduler := newUnstartedSchedulerForTest(t, exec)
		owner := newCronSchedulerTestOwner(scheduler)
		rule := cronRuleForTest(t, nil)
		if err := scheduler.Register(rule); err != nil {
			t.Fatal(err)
		}
		if err := scheduler.Start(owner.startContext(operationCtx)); err != nil {
			t.Fatal(err)
		}
		fireDone := make(chan struct{})
		go func() { defer close(fireDone); scheduler.fire(rule.ID()) }()
		released := false
		var stopDone chan error
		stopObserved := false
		defer func() {
			if !released {
				close(exec.release)
			}
			joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
			defer cancelJoin()
			select {
			case <-fireDone:
			case <-joinCtx.Done():
				t.Error("test-owned expired scheduler fire did not join after release")
			}
			if stopDone != nil && !stopObserved {
				select {
				case <-stopDone:
				case <-joinCtx.Done():
					t.Error("expired scheduler Stop did not return after release")
				}
			}
		}()
		select {
		case <-exec.started:
		case <-operationCtx.Done():
			t.Fatal("scheduler fire was not admitted before expiry fixture deadline")
		}
		stopDone = make(chan error, 1)
		go func() { stopDone <- owner.stop(operationCtx) }()
		waitCtx, cancelWait := context.WithTimeout(operationCtx, 6*time.Second)
		defer cancelWait()
		var stopErr error
		select {
		case stopErr = <-stopDone:
			stopObserved = true
		case <-waitCtx.Done():
			t.Fatal("native Stop did not return by its bounded virtual terminal observation")
		}
		if !errors.Is(stopErr, context.DeadlineExceeded) {
			t.Errorf("native Stop = %v, want terminal deadline", stopErr)
		}
		owner.finish(operationCtx, t) // fallback must not retry an attempted native Stop.
		if err := owner.stop(operationCtx); err == nil || err.Error() != "rule CronScheduler fixture Stop already attempted" {
			t.Errorf("second explicit owner Stop = %v, want once-only refusal", err)
		}
		close(exec.release)
		released = true
		joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
		defer cancelJoin()
		select {
		case <-fireDone:
		case <-joinCtx.Done():
			t.Fatal("test-owned scheduler fire did not finish after release")
		}
		synctest.Wait()
	})
}
