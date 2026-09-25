package agentrun

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// laneHandle is a jetstream.ConsumeContext that counts the drains it receives
// and reports Closed once drained.
//
// Stop panics on purpose. The owner drains, never stops: the forced
// handle.Stop() fallback was removed by the owner ruling of 2026-09-22, and a
// future edit that brings it back fails here rather than silently abandoning an
// admitted delivery mid-effect.
type laneHandle struct {
	drains atomic.Int32
	once   sync.Once
	closed chan struct{}
}

func newLaneHandle() *laneHandle { return &laneHandle{closed: make(chan struct{})} }

func (*laneHandle) Stop() { panic("the milestone owner must never force Stop a lane") }

func (h *laneHandle) Drain() {
	h.drains.Add(1)
	h.once.Do(func() { close(h.closed) })
}

func (h *laneHandle) Closed() <-chan struct{} { return h.closed }

// ownerUnderTest assembles both lanes through the production seams Start uses:
// the same admissions, the same observeLane, the same owner struct.
type ownerUnderTest struct {
	owner            *milestoneConsumerOwner
	completeHandle   *laneHandle
	failedHandle     *laneHandle
	completeAdmit    *deliverylane.Admission
	failedAdmit      *deliverylane.Admission
	completeLaneFixt milestoneLaneFixture
	failedLaneFixt   milestoneLaneFixture
}

func newOwnerUnderTest(t *testing.T, s *MilestoneSubscriber) *ownerUnderTest {
	t.Helper()
	runCtx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	owner := &milestoneConsumerOwner{cancel: cancel}
	under := &ownerUnderTest{
		owner:          owner,
		completeHandle: newLaneHandle(),
		failedHandle:   newLaneHandle(),
	}
	// One admission per lane, each feeding the subscriber's own onFatal and
	// refusal declarer — the arguments Start passes.
	under.completeAdmit = s.newLaneAdmission(milestoneLaneComplete)
	under.failedAdmit = s.newLaneAdmission(milestoneLaneFailed)
	under.completeLaneFixt = milestoneLaneFixture{
		policy:    milestonePolicyFor(t, s, milestoneLaneComplete),
		admission: under.completeAdmit,
	}
	under.failedLaneFixt = milestoneLaneFixture{
		policy:    milestonePolicyFor(t, s, milestoneLaneFailed),
		admission: under.failedAdmit,
	}
	owner.mu.Lock()
	owner.complete = s.observeLane(runCtx, milestoneLaneComplete, under.completeHandle, under.completeAdmit)
	owner.failed = s.observeLane(runCtx, milestoneLaneFailed, under.failedHandle, under.failedAdmit)
	owner.running = true
	owner.mu.Unlock()
	return under
}

// TestMilestoneFatalDrainsOnlyTheFailedLane pins the blast radius of a fatal:
// the lane that lost ownership stops consuming, and the other one keeps its
// milestones flowing. A shared drain would take both down for one bad handler.
func TestMilestoneFatalDrainsOnlyTheFailedLane(t *testing.T) {
	t.Parallel()
	sub := quietSubscriber(&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		panic("product handler exploded")
	}))
	under := newOwnerUnderTest(t, sub)

	under.completeLaneFixt.deliver(t, terminalBytes(t, "fatal-loop", ""))

	// The observer drains asynchronously; the handle's Closed is the signal,
	// not a sleep.
	<-under.completeHandle.Closed()
	assert.Equal(t, int32(1), under.completeHandle.drains.Load(), "the failed lane drains its exact handle")
	assert.Equal(t, int32(0), under.failedHandle.drains.Load(), "the other lane is untouched")
	assert.False(t, under.completeAdmit.Admit(), "the failed lane admits no further local work")
	assert.True(t, under.failedAdmit.Admit(), "the other lane keeps consuming")
	require.Error(t, sub.DeliveryFatal())
}

// TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain is the other half of
// design § 2.6: Stop is unconditional, so it reaches a lane the observer
// already drained. The binding's drain-once is what makes that safe, and Stop
// still joins both observers before it returns.
func TestMilestoneStopAfterFatalWaitsClosedWithoutSecondDrain(t *testing.T) {
	t.Parallel()
	sub := quietSubscriber(&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		panic("product handler exploded")
	}))
	under := newOwnerUnderTest(t, sub)

	under.completeLaneFixt.deliver(t, terminalBytes(t, "fatal-loop", ""))
	<-under.completeHandle.Closed()

	// Stop clears its binding references once it completes, so the Done
	// channels are captured before it runs.
	under.owner.mu.Lock()
	observers := map[string]<-chan struct{}{
		"complete": under.owner.complete.Done(),
		"failed":   under.owner.failed.Done(),
	}
	under.owner.mu.Unlock()

	require.NoError(t, under.owner.stop(t.Context()))
	assert.Equal(t, int32(1), under.completeHandle.drains.Load(),
		"Stop must not order a second drain of a lane the observer already drained")
	assert.Equal(t, int32(1), under.failedHandle.drains.Load(), "Stop drains the healthy lane exactly once")

	// Both observers are joined by the time Stop returns: their Done channels
	// are already closed, so reading them cannot block.
	for name, done := range observers {
		select {
		case <-done:
		default:
			t.Fatalf("Stop returned before joining the %s lane observer", name)
		}
	}

	// A second Stop is a no-op, not a second drain.
	require.NoError(t, under.owner.stop(t.Context()))
	assert.Equal(t, int32(1), under.completeHandle.drains.Load())
	assert.Equal(t, int32(1), under.failedHandle.drains.Load())
}

// TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner is the
// control-plane row: the failure happens before any handler is involved, so
// nothing about the milestone is known. Nothing is settled, the exact lane
// stops, and the cause reaches health.
//
// An InProgress failure mid-fanout reaches the owner through the identical
// seam — DeliveryResult.OwnerStopRequired, which the admission latches on —
// and natsclient's own tests own the InProgress-to-owner-stop mapping.
func TestMilestoneUnavailableDeliveryMetadataQuarantinesAndStopsExactOwner(t *testing.T) {
	t.Parallel()
	var handled atomic.Int32
	sub := quietSubscriber(&stubRunReader{err: lifecycle.ErrEntityNotFound}, stubTripleReader{}, "acme")
	sub.AddHandler(handlerFunc(func(context.Context, LoopTerminalEvent, *AgentRun) error {
		handled.Add(1)
		return nil
	}))
	under := newOwnerUnderTest(t, sub)

	msg := &settlementMsg{
		data:    terminalBytes(t, "metadata-loop", ""),
		subject: "agent.failed.loop_failed",
		metaErr: errors.New("nats: server delivery metadata is not available"),
	}
	result, admitted := deliverylane.Consume(
		t.Context(), msg, under.failedLaneFixt.policy, under.failedAdmit)
	require.True(t, admitted)
	require.True(t, result.OwnerStopRequired(), "unavailable metadata is an owner-stop result")

	<-under.failedHandle.Closed()
	assert.Zero(t, msg.settlements(), "no Ack, Nak or Term on a delivery whose metadata is unreadable")
	assert.Zero(t, handled.Load(), "no handler runs for a delivery the server cannot describe")
	assert.Equal(t, int32(1), under.failedHandle.drains.Load(), "the exact lane drains")
	assert.Equal(t, int32(0), under.completeHandle.drains.Load(), "only the exact lane drains")
	assert.False(t, under.failedAdmit.Admit())
	require.Error(t, sub.DeliveryFatal(), "the cause reaches health through DeliveryFatal")
	assert.ErrorContains(t, sub.DeliveryFatal(), "delivery_metadata_unavailable")
}
