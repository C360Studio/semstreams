package deliverylane

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// fakeHandle is a jetstream.ConsumeContext that counts drains. Stop panics
// because this contract drains, never stops: a lane that force-stops abandons
// admitted work mid-effect.
type fakeHandle struct {
	drains atomic.Int32
	closed chan struct{}
}

func newFakeHandle() *fakeHandle { return &fakeHandle{closed: make(chan struct{})} }

func (*fakeHandle) Stop()                     { panic("deliverylane must never force Stop a lane") }
func (h *fakeHandle) Drain()                  { h.drains.Add(1) }
func (h *fakeHandle) Closed() <-chan struct{} { return h.closed }

// fakeMsg counts every affordance a closed lane must not touch: the payload,
// the subject, the heartbeat, and each terminal method.
type fakeMsg struct {
	data     []byte
	subject  string
	metadata *jetstream.MsgMetadata
	ackErr   error

	dataReads    atomic.Int32
	subjectReads atomic.Int32
	inProgress   atomic.Int32
	acks         atomic.Int32
	naks         atomic.Int32
	terms        atomic.Int32
}

func (m *fakeMsg) Data() []byte {
	m.dataReads.Add(1)
	return m.data
}

func (m *fakeMsg) Subject() string {
	m.subjectReads.Add(1)
	return m.subject
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return m.metadata, nil }
func (*fakeMsg) Headers() nats.Header                        { return nil }
func (*fakeMsg) Reply() string                               { return "" }
func (*fakeMsg) DoubleAck(context.Context) error             { return nil }

func (m *fakeMsg) Ack() error {
	m.acks.Add(1)
	return m.ackErr
}

func (m *fakeMsg) Nak() error {
	m.naks.Add(1)
	return nil
}

func (m *fakeMsg) NakWithDelay(time.Duration) error {
	m.naks.Add(1)
	return nil
}

func (m *fakeMsg) InProgress() error {
	m.inProgress.Add(1)
	return nil
}

func (m *fakeMsg) Term() error {
	m.terms.Add(1)
	return nil
}

func (m *fakeMsg) TermWithReason(string) error { return m.Term() }

func (m *fakeMsg) settlements() int32 { return m.acks.Load() + m.naks.Load() + m.terms.Load() }

// deliveredMsg is a message the server delivered once, so the typed heartbeat
// path accepts its metadata and runs work.
func deliveredMsg(subject string) *fakeMsg {
	return &fakeMsg{
		data:     []byte(`{"payload":true}`),
		subject:  subject,
		metadata: &jetstream.MsgMetadata{NumDelivered: 1},
	}
}

// fatalResult produces an owner-stop result through the production settlement
// path rather than by constructing DeliveryResult, which has no exported
// fields. Quarantine with a cause attempts no terminal method.
func fatalResult(t *testing.T, cause string) natsclient.DeliveryResult {
	t.Helper()
	result := natsclient.SettleDeliveryWithRetry(
		deliveredMsg("fatal"), natsclient.ImmediateDeliveryRetry(),
		natsclient.DeliveryDecisionQuarantine, errors.New(cause))
	require.True(t, result.OwnerStopRequired(), "the fixture must be an owner-stop result")
	require.ErrorContains(t, result.Err(), cause)
	return result
}

func heartbeatPolicy(t *testing.T, work natsclient.DeliveryWork) natsclient.HeartbeatDeliveryPolicy {
	t.Helper()
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		t.Context(),
		natsclient.StreamConsumerConfig{AckWait: 2 * time.Minute},
		15*time.Second,
		natsclient.ImmediateDeliveryRetry(),
		work,
	)
	require.NoError(t, err)
	return policy
}

func latchedAdmission(t *testing.T, onRefused func(string)) *Admission {
	t.Helper()
	admission := NewAdmission(nil, onRefused)
	admission.Latch(fatalResult(t, "lane already lost control"))
	require.False(t, admission.Admit())
	return admission
}

// I1 and I2: the first owner-stop result closes the lane forever, and every
// later result — fatal or not — changes neither the health writer's view nor
// the one buffered cause.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestLatchClosesOnFirstOwnerStopAndIgnoresEveryLaterResult(t *testing.T) {
	var recorded []natsclient.DeliveryResult
	admission := NewAdmission(func(result natsclient.DeliveryResult) {
		recorded = append(recorded, result)
	}, nil)

	admission.Latch(natsclient.SettleDelivery(deliveredMsg("ordinary"), natsclient.DeliveryDecisionAck, nil))
	require.True(t, admission.Admit(), "a clean Ack must not close the lane")
	require.Empty(t, recorded, "a clean Ack is not an owner fatal")

	first := fatalResult(t, "first cause")
	admission.Latch(first)
	require.False(t, admission.Admit())
	require.Len(t, recorded, 1)
	require.ErrorContains(t, recorded[0].Err(), "first cause")

	admission.Latch(fatalResult(t, "second cause"))
	admission.Latch(natsclient.SettleDelivery(deliveredMsg("ordinary"), natsclient.DeliveryDecisionAck, nil))
	require.False(t, admission.Admit(), "a latched lane never reopens")
	require.Len(t, recorded, 1, "a later fatal must neither overwrite nor recount the first")

	handle := newFakeHandle()
	binding := NewBinding(handle)
	observed := make(chan natsclient.DeliveryResult, 2)
	Observe(t.Context(), binding, admission, func(result natsclient.DeliveryResult) { observed <- result })

	select {
	case result := <-observed:
		require.ErrorContains(t, result.Err(), "first cause",
			"the observer must see the first cause, not the last")
	case <-time.After(5 * time.Second):
		t.Fatal("the buffered fatal never reached the observer")
	}
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, 5*time.Second, time.Millisecond)
	require.Empty(t, observed, "exactly one result reaches the observer per lane")
}

// I3: the owner's health writer completes inside the callback, before the
// result is buffered — so on the FATAL path health cannot read "healthy" after
// the handle has drained, because the observer is what drains it and the
// observer runs after the buffer. This orders nothing against the owner's own
// Stop, which calls Binding.Drain independently of the Admission; a Stop
// concurrent with a latching lane may drain while this writer is still running.
// Nothing here is a synchronization contract for that case, and the assertions
// below only hold because no Stop competes with them.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestHealthWriterCompletesBeforeTheResultIsBuffered(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	admission := NewAdmission(func(natsclient.DeliveryResult) {
		close(entered)
		<-release
	}, nil)

	handle := newFakeHandle()
	binding := NewBinding(handle)
	reacted := make(chan struct{}, 1)
	Observe(t.Context(), binding, admission, func(natsclient.DeliveryResult) { reacted <- struct{}{} })

	latched := make(chan struct{})
	go func() {
		defer close(latched)
		admission.Latch(fatalResult(t, "control lost"))
	}()

	<-entered
	require.False(t, admission.Admit(), "the lane closes before the health writer runs")
	require.Zero(t, handle.drains.Load(),
		"the OBSERVER drained while the health writer was still running; no owner Stop competes in this test")
	select {
	case <-reacted:
		t.Fatal("the observer reacted before the health writer returned")
	default:
	}

	close(release)
	<-latched
	require.Eventually(t, func() bool { return handle.drains.Load() == 1 }, 5*time.Second, time.Millisecond)
}

// I4: the exact handle drains at most once however many drainers race — the
// observer's fatal arm and every Stop-side call.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestDrainHappensAtMostOnceAcrossObserverAndStop(t *testing.T) {
	admission := NewAdmission(nil, nil)
	handle := newFakeHandle()
	binding := NewBinding(handle)
	drained := make(chan struct{})
	Observe(t.Context(), binding, admission, func(natsclient.DeliveryResult) { close(drained) })

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for range 8 {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			binding.Drain()
		}()
	}
	go admission.Latch(fatalResult(t, "control lost"))
	start.Done()
	done.Wait()
	<-drained
	<-binding.Done()

	require.Equal(t, int32(1), handle.drains.Load(), "the exact handle drained more than once")
	binding.Drain()
	require.Equal(t, int32(1), handle.drains.Load(), "a later Stop must rejoin the one drain")
}

// I5: a closed lane runs no work, sends no heartbeat, attempts no terminal
// method, and reads nothing from the delivery — not even its subject — when no
// declarer exists to receive it.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestClosedLaneTouchesNothingOnARefusedDelivery(t *testing.T) {
	admission := latchedAdmission(t, nil)
	var workRuns atomic.Int32
	policy := heartbeatPolicy(t, func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
		workRuns.Add(1)
		return natsclient.DeliveryDecisionAck, nil
	})

	consumed := deliveredMsg("tool.execute")
	result, admitted := Consume(t.Context(), consumed, policy, admission)
	require.False(t, admitted)
	require.False(t, result.OwnerStopRequired())
	require.Equal(t, natsclient.DeliveryDecisionInvalid, result.Decision())

	settled := deliveredMsg("user.message")
	settleResult, settleAdmitted := Settle(
		t.Context(), settled, natsclient.ImmediateDeliveryRetry(), admission, "dispatch",
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			workRuns.Add(1)
			return natsclient.DeliveryDecisionAck, nil
		})
	require.False(t, settleAdmitted)
	require.False(t, settleResult.OwnerStopRequired())

	require.Zero(t, workRuns.Load(), "a closed lane ran work")
	for name, msg := range map[string]*fakeMsg{"heartbeat lane": consumed, "settlement lane": settled} {
		require.Zero(t, msg.settlements(), "%s attempted a terminal method on a refused delivery", name)
		require.Zero(t, msg.inProgress.Load(), "%s heartbeated a refused delivery", name)
		require.Zero(t, msg.dataReads.Load(), "%s read the payload of a refused delivery", name)
		require.Zero(t, msg.subjectReads.Load(),
			"%s read the subject of a refused delivery with no declarer to receive it", name)
	}
}

// I9: with a declarer, every refused delivery is declared once with its own
// subject — the buffered deliveries a drained handle flushes included, which is
// why the first fatal alone is not the signal.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestRefusalDeclarerSeesEverySubjectExactlyOnce(t *testing.T) {
	var declared []string
	admission := latchedAdmission(t, func(subject string) { declared = append(declared, subject) })
	policy := heartbeatPolicy(t, func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
		return natsclient.DeliveryDecisionAck, nil
	})

	first := deliveredMsg("agent.complete")
	second := deliveredMsg("agent.failed")
	_, firstAdmitted := Consume(t.Context(), first, policy, admission)
	_, secondAdmitted := Consume(t.Context(), second, policy, admission)
	require.False(t, firstAdmitted)
	require.False(t, secondAdmitted)

	settlementLane := deliveredMsg("user.message")
	_, settleAdmitted := Settle(t.Context(), settlementLane, natsclient.ImmediateDeliveryRetry(), admission,
		"dispatch", func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, nil
		})
	require.False(t, settleAdmitted)

	require.Equal(t, []string{"agent.complete", "agent.failed", "user.message"}, declared)
	require.Equal(t, int32(1), first.subjectReads.Load(), "the subject is read once, only to declare the refusal")
	require.Equal(t, int32(1), second.subjectReads.Load())
	require.Equal(t, int32(1), settlementLane.subjectReads.Load())
	require.Zero(t, first.dataReads.Load()+second.dataReads.Load()+settlementLane.dataReads.Load(),
		"the subject is the only metadata a closed lane reads")
}

// I6, first half: a binding no observer was started on is joinable at once, so
// a Stop that joins every binding cannot block on a lane that never observed.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestDoneIsClosedWhenNoObserverRan(t *testing.T) {
	binding := NewBinding(newFakeHandle())
	select {
	case <-binding.Done():
	default:
		t.Fatal("Done() must be already closed when no observer was started")
	}
}

// I6, second half: the observer exits on the owner's context cancellation
// without draining, and Done closes either way.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestObserverExitsOnCancellationWithoutDraining(t *testing.T) {
	admission := NewAdmission(nil, nil)
	handle := newFakeHandle()
	binding := NewBinding(handle)
	ctx, cancel := context.WithCancel(t.Context())
	var reactions atomic.Int32
	Observe(ctx, binding, admission, func(natsclient.DeliveryResult) { reactions.Add(1) })

	cancel()
	select {
	case <-binding.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the observer did not exit on context cancellation")
	}
	require.Zero(t, handle.drains.Load(), "a cancelled observer must not drain the handle")
	require.Zero(t, reactions.Load())
	require.True(t, admission.Admit(), "cancellation is not an owner fatal")
}

// I7: a panic in settlement-only work becomes Quarantine with a cause naming
// the owner, never a nil error and never an unwound NATS callback.
//
// spec: jetstream-consumer-policy / settlement-only delivery decisions use one shared interpreter
func TestSettleQuarantinesPanickingWorkWithTheOwnerNamedCause(t *testing.T) {
	admission := NewAdmission(nil, nil)
	msg := deliveredMsg("governance.request")

	result, admitted := Settle(t.Context(), msg, natsclient.ImmediateDeliveryRetry(), admission, "governance",
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			panic("approval handler exploded")
		})

	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
	require.True(t, result.Quarantined())
	require.True(t, result.OwnerStopRequired())
	require.ErrorContains(t, result.Err(), "governance delivery work panicked: approval handler exploded")
	require.Zero(t, msg.settlements(), "a quarantined delivery stays pending for the reconstructed owner")
	require.False(t, admission.Admit())
}

// I8: a terminal-method error is local, unconfirmed evidence — it never closes
// admission by itself.
//
// spec: jetstream-consumer-policy / settlement-only delivery decisions use one shared interpreter
func TestTerminalMethodErrorAloneKeepsTheLaneOpen(t *testing.T) {
	var recorded atomic.Int32
	admission := NewAdmission(func(natsclient.DeliveryResult) { recorded.Add(1) }, nil)
	msg := deliveredMsg("user.message")
	msg.ackErr = errors.New("connection closed")

	result, admitted := Settle(t.Context(), msg, natsclient.ImmediateDeliveryRetry(), admission, "dispatch",
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, nil
		})

	require.True(t, admitted)
	require.True(t, result.SettlementMethodFailed())
	require.False(t, result.OwnerStopRequired())
	require.True(t, admission.Admit(), "a failed Ack is not loss of delivery ownership")
	require.Zero(t, recorded.Load(), "a terminal-method error must not reach the health writer as a fatal")
}

// M8's primitive detector: the admission guard inside Settle is what stops a
// latched settlement lane from running work again.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestSettleRefusesWithoutInvokingWork(t *testing.T) {
	admission := latchedAdmission(t, nil)
	msg := deliveredMsg("governance.request")
	var invoked atomic.Bool

	result, admitted := Settle(t.Context(), msg, natsclient.ImmediateDeliveryRetry(), admission, "governance",
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			invoked.Store(true)
			return natsclient.DeliveryDecisionAck, nil
		})

	require.False(t, admitted)
	require.False(t, invoked.Load(), "a latched settlement lane invoked its work")
	require.Zero(t, msg.settlements())
	require.Equal(t, natsclient.DeliveryDecisionInvalid, result.Decision())
}

// The refusal trap, gated rather than documented: a refused Settle returns the
// ZERO result, and a zero result's Err() is NON-NIL, so a caller that branches
// on Err() without the admission bool reports a delivery that ran no work and
// attempted no terminal method as a settlement failure.
//
// spec: jetstream-consumer-policy / settlement-only delivery decisions use one shared interpreter
func TestSettleRefusalReturnsAZeroResultWhoseErrIsNonNil(t *testing.T) {
	admission := latchedAdmission(t, nil)
	msg := deliveredMsg("user.message")

	result, admitted := Settle(t.Context(), msg, natsclient.ImmediateDeliveryRetry(), admission, "dispatch",
		func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
			return natsclient.DeliveryDecisionAck, nil
		})

	require.False(t, admitted)
	require.Equal(t, natsclient.DeliveryResult{}, result)
	require.Error(t, result.Err(),
		"the zero result's Err() is non-nil, which is exactly why a caller must guard on the bool")
	require.False(t, result.OwnerStopRequired(), "a refusal is not itself a new owner-stop event")
	require.Zero(t, msg.settlements())
}

// A lane whose fatal drains with no owner reaction is a silent degrade, so a
// nil reaction fails at wiring rather than at the first fatal.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestObserveRefusesNilReact(t *testing.T) {
	binding := NewBinding(newFakeHandle())
	require.PanicsWithValue(t, "deliverylane: Observe requires a non-nil react", func() {
		Observe(t.Context(), binding, NewAdmission(nil, nil), nil)
	})
	select {
	case <-binding.Done():
	default:
		t.Fatal("a refused Observe must leave the binding joinable")
	}
}

// Consume runs the production typed heartbeat path: an admitted delivery is
// consumed and settled through it, and its owner-stop result latches the lane.
//
// spec: jetstream-consumer-policy / shared settlement remains stateless and heartbeat-specific
func TestConsumeRunsTheTypedHeartbeatPathAndLatchesItsResult(t *testing.T) {
	var workRuns atomic.Int32
	policy := heartbeatPolicy(t, func(context.Context, []byte) (natsclient.DeliveryDecision, error) {
		workRuns.Add(1)
		return natsclient.DeliveryDecisionAck, nil
	})
	admission := NewAdmission(nil, nil)

	healthy := deliveredMsg("tool.execute")
	result, admitted := Consume(t.Context(), healthy, policy, admission)
	require.True(t, admitted)
	require.NoError(t, result.Err())
	require.Equal(t, int32(1), healthy.acks.Load())
	require.Equal(t, int32(1), workRuns.Load())
	require.True(t, admission.Admit(), "a clean delivery keeps the lane open")

	// Absent server delivery metadata is the typed path's owner-stop case: the
	// lane cannot prove the delivery, so nothing is settled and the lane closes.
	unavailable := &fakeMsg{data: []byte(`{}`), subject: "tool.execute"}
	fatal, fatalAdmitted := Consume(t.Context(), unavailable, policy, admission)
	require.True(t, fatalAdmitted)
	require.True(t, fatal.OwnerStopRequired())
	require.Zero(t, unavailable.settlements())
	require.Equal(t, int32(1), workRuns.Load(), "work must not run for an unprovable delivery")
	require.False(t, admission.Admit())
}
