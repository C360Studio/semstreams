package rule

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAwaitEntityBorrowSettlement_CallerCancellationCancelsStartAuthority(t *testing.T) {
	borrowDone := make(chan struct{})
	var releaseBorrow sync.Once
	release := func() {
		releaseBorrow.Do(func() { close(borrowDone) })
	}
	stopCtx, endStop := context.WithCancel(context.Background())
	var cancelCalls atomic.Int32
	result := make(chan error, 1)
	helperDone := make(chan struct{})

	go func() {
		defer close(helperDone)
		result <- awaitEntityBorrowSettlement(stopCtx, borrowDone, func() {
			cancelCalls.Add(1)
		})
	}()

	t.Cleanup(func() {
		endStop()
		release()
		containment := time.NewTimer(2 * time.Second)
		defer containment.Stop()
		select {
		case <-helperDone:
		case <-containment.C:
			t.Errorf("awaitEntityBorrowSettlement test goroutine did not join after releasing held borrow")
		}
	})

	endStop()
	containment := time.NewTimer(2 * time.Second)
	defer containment.Stop()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled,
			"awaitEntityBorrowSettlement must return the exact caller cancellation")
	case <-containment.C:
		t.Fatal("awaitEntityBorrowSettlement did not return after caller cancellation within 2s containment")
	}
	require.EqualValues(t, 1, cancelCalls.Load(),
		"caller cancellation must cancel Start authority exactly once")
}

func TestEntityEvaluationFenceIdleCapacityEvictsLeastRecentWatermark(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000, 0)
	fence := entityEvaluationFence{
		now:          func() time.Time { return now },
		idleTTL:      time.Hour,
		idleCapacity: 2,
	}
	recordFenceRevision(&fence, "entity-a", 10)
	now = now.Add(time.Second)
	recordFenceRevision(&fence, "entity-b", 10)
	// Touch A so B becomes the least-recently-idle entry.
	entryA := fence.retain("entity-a")
	fence.release("entity-a", entryA)
	now = now.Add(time.Second)
	recordFenceRevision(&fence, "entity-c", 10)

	require.True(t, fenceContains(&fence, "entity-a"))
	require.False(t, fenceContains(&fence, "entity-b"))
	require.True(t, fenceContains(&fence, "entity-c"))
	requireFenceRawCounts(t, &fence, 0, 2)
}

func TestEntityEvaluationFenceIdleTTLUsesInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Unix(2_000, 0)
	fence := entityEvaluationFence{
		now:          func() time.Time { return now },
		idleTTL:      5 * time.Minute,
		idleCapacity: 10,
	}
	recordFenceRevision(&fence, "expired", 10)
	now = now.Add(5*time.Minute + time.Nanosecond)
	recordFenceRevision(&fence, "current", 10)

	require.False(t, fenceContains(&fence, "expired"))
	require.True(t, fenceContains(&fence, "current"))
	requireFenceRawCounts(t, &fence, 0, 1)
}

func TestEntityEvaluationFenceNeverEvictsActiveEntries(t *testing.T) {
	t.Parallel()

	now := time.Unix(3_000, 0)
	fence := entityEvaluationFence{
		now:          func() time.Time { return now },
		idleTTL:      time.Hour,
		idleCapacity: 1,
	}
	active := fence.retain("active")
	recordFenceRevision(&fence, "idle-a", 10)
	recordFenceRevision(&fence, "idle-b", 10)

	require.True(t, fenceContains(&fence, "active"))
	require.False(t, fenceContains(&fence, "idle-a"))
	require.True(t, fenceContains(&fence, "idle-b"))
	requireFenceRawCounts(t, &fence, 1, 1)
	fence.release("active", active)
}

func recordFenceRevision(fence *entityEvaluationFence, entityID string, revision uint64) {
	entry := fence.retain(entityID)
	entry.mu.Lock()
	entry.record(entitySnapshot{Action: "UPDATED", Revision: revision})
	entry.mu.Unlock()
	fence.release(entityID, entry)
}

func fenceContains(fence *entityEvaluationFence, entityID string) bool {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	_, exists := fence.entries[entityID]
	return exists
}

func requireFenceRawCounts(t *testing.T, fence *entityEvaluationFence, active, idle int) {
	t.Helper()
	gotActive, gotIdle := fence.counts()
	require.Equal(t, active, gotActive)
	require.Equal(t, idle, gotIdle)
}
