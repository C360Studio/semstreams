//go:build integration

package rule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// stopDeadlineUnderTest is the Stop bound this test exercises: short, so a
// Stop that honors it returns long before stopReturnTolerance.
const stopDeadlineUnderTest = 100 * time.Millisecond

// stopReturnTolerance is how long the test waits for a bounded Stop before
// declaring it unbounded. Generous against CI scheduling; an unbounded Stop
// never returns while the target is held.
const stopReturnTolerance = 5 * time.Second

// applyBlockingTarget holds its first ApplyConfigUpdate until released,
// ignoring every context, the way a wedged processor would.
type applyBlockingTarget struct {
	fakeRuleTarget
	entered chan struct{}
	release chan struct{}
}

func (a *applyBlockingTarget) ApplyConfigUpdate(map[string]any) error {
	close(a.entered)
	<-a.release
	return nil
}

// A Stop whose context ends while the manager's work is held returns the
// context's error without waiting for it, and the reconcile loop still exits
// once the work is released. Owner ruling on #1188, docket 6 Q11 (b-full).
//
// spec: component-runtime-config / Config Manager delivers a registered key family to its owner
func TestConfigManagerStopReturnsWhenItsContextEnds(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rcm, _ := startRulesFamily(t, ctx, tc)
	target := &applyBlockingTarget{entered: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(target.release)
		}
	}()

	// Start's initial reconcile reaches the target's apply and is held there.
	startErr := make(chan error, 1)
	go func() { startErr <- rcm.Start(ctx, []HotReloadTarget{target}) }()
	select {
	case <-target.entered:
	case <-time.After(stopReturnTolerance):
		t.Fatalf("Start's initial reconcile did not reach the target within %s", stopReturnTolerance)
	}
	rcm.lifecycleMu.Lock()
	done := rcm.done
	rcm.lifecycleMu.Unlock()
	require.NotNil(t, done, "Start publishes its completion fence before seeding")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), stopDeadlineUnderTest)
	defer stopCancel()
	stopErr := make(chan error, 1)
	go func() { stopErr <- rcm.Stop(stopCtx) }()
	select {
	case err := <-stopErr:
		require.True(t, errors.Is(err, context.DeadlineExceeded),
			"a Stop whose bound wins returns its context's error, got %v", err)
	case <-time.After(stopReturnTolerance):
		t.Fatalf("Stop did not return within %s of its %s deadline", stopReturnTolerance, stopDeadlineUnderTest)
	}
	select {
	case <-done:
		t.Fatal("the completion fence closed while the target was still held")
	default:
	}

	close(target.release)
	released = true
	require.NoError(t, <-startErr)
	select {
	case <-done:
	case <-time.After(stopReturnTolerance):
		t.Fatalf("the reconcile loop did not exit within %s of the target's release", stopReturnTolerance)
	}
	// The fence has closed, so a repeated Stop is a nil no-op.
	stopConfigManagerWithinBudget(t, rcm)
}
