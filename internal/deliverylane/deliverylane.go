// Package deliverylane owns the one owner-side reaction the typed settlement
// contract asks every durable-consumer owner to build: a per-lane admission
// latch that closes on the first result requiring owner stop, a drain-once
// binding around the exact jetstream.ConsumeContext that lane committed, and
// the observer that drains that handle when the latch closes.
//
// It owns no lifecycle authority: no Stop, no restart, no reconstruction, no
// registry of lanes. The owner constructs the admission before acquisition,
// retains the binding, decides Stop, awaits Closed, and joins the observer;
// JetStream keeps delivery authority throughout. It is internal because every
// consumer is a lane inside this module: the five agentic components today, and
// agentic/agentrun once #1249 adopts it; exporting it is a Tier 1 widening gated
// on a measured adopter outside this module (design § 3, option C).
package deliverylane

import (
	"context"
	"fmt"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Admission is one lane's latch. JetStream retains delivery authority; the
// latch only prevents new local work after ownership control becomes unsafe.
type Admission struct {
	mu        sync.Mutex
	open      bool
	fatal     chan natsclient.DeliveryResult
	onFatal   func(natsclient.DeliveryResult)
	onRefused func(subject string)
}

// NewAdmission returns an open latch. onFatal runs synchronously on the first
// owner-stop result — after the lane has closed, before that result is
// buffered for Observe — so the owner's record is complete by the time the
// FATAL observer drains that lane's handle. That is the only drain it orders.
// The owner's own Stop calls Binding.Drain independently of this Admission, so
// a Stop concurrent with a latching lane may drain while onFatal is still
// running, and an ordinary Stop drains a lane that never latched at all; nil
// is allowed and means the owner records nothing. onRefused, when non-nil, declares each delivery the closed
// latch refuses; nil leaves the refusal undeclared, which is what every lane
// without a refusal counter does today (#1342 tracks the gap).
func NewAdmission(
	onFatal func(natsclient.DeliveryResult),
	onRefused func(subject string),
) *Admission {
	return &Admission{
		open:      true,
		fatal:     make(chan natsclient.DeliveryResult, 1),
		onFatal:   onFatal,
		onRefused: onRefused,
	}
}

// Admit reports whether the lane still accepts new local work.
func (a *Admission) Admit() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.open
}

// Latch closes the lane on the first result requiring owner stop and buffers
// that result for Observe. Every later result, fatal or not, changes nothing:
// the first cause is the one health and the observer see.
func (a *Admission) Latch(result natsclient.DeliveryResult) {
	if !result.OwnerStopRequired() {
		return
	}
	a.mu.Lock()
	if !a.open {
		a.mu.Unlock()
		return
	}
	a.open = false
	a.mu.Unlock()
	if a.onFatal != nil {
		a.onFatal(result)
	}
	a.fatal <- result
}

// refuse declares a refused delivery through onRefused. The lane is drained
// rather than stopped, so buffered deliveries still reach this path after the
// latch; without a declarer only the first fatal is visible and every later
// refusal is a silent drop. The subject is the only metadata a closed lane
// reads, and only when a declarer exists to receive it.
func (a *Admission) refuse(msg jetstream.Msg) {
	if a.onRefused == nil {
		return
	}
	subject := ""
	if msg != nil {
		subject = msg.Subject()
	}
	a.onRefused(subject)
}

// Consume runs one heartbeat-lane delivery under admission: a closed lane
// refuses without reading, heartbeating, or settling; an open lane consumes
// through the typed heartbeat helper and latches on its result. The bool
// reports admission, so a caller can guard every branch on it. A nil msg is
// tolerated because the typed helper quarantines it.
func Consume(
	ctx context.Context,
	msg jetstream.Msg,
	policy natsclient.HeartbeatDeliveryPolicy,
	admission *Admission,
) (natsclient.DeliveryResult, bool) {
	if !admission.Admit() {
		admission.refuse(msg)
		return natsclient.DeliveryResult{}, false
	}
	result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)
	admission.Latch(result)
	return result, true
}

// Settle runs one settlement-only delivery under admission: a closed lane
// refuses; an open lane runs work under the panic guard, applies one terminal
// method under retry, and latches on the result. Pass
// natsclient.ImmediateDeliveryRetry() for a lane that settles immediately.
//
// The bool reports admission, and a caller MUST guard every branch on it,
// because a refusal returns the zero natsclient.DeliveryResult and a zero
// result's Err() is non-nil by construction: Err reports "delivery result is
// incomplete for decision 0" for anything that settled nothing
// (natsclient/delivery_settlement.go). An unguarded `result.Err() != nil`
// branch therefore reports a refused delivery — which ran no work and
// attempted no terminal method — as a settlement failure. The early-return
// form, `if !admitted { return }`, keeps the branches below it unchanged.
//
// Unlike Consume, msg must be non-nil: work needs its payload, and the NATS
// callback never delivers nil; a nil here is the caller's contract violation.
func Settle(
	ctx context.Context,
	msg jetstream.Msg,
	retry natsclient.DeliveryRetryPolicy,
	admission *Admission,
	owner string,
	work natsclient.DeliveryWork,
) (natsclient.DeliveryResult, bool) {
	if !admission.Admit() {
		admission.refuse(msg)
		return natsclient.DeliveryResult{}, false
	}
	decision, cause := runWork(ctx, msg.Data(), owner, work)
	result := natsclient.SettleDeliveryWithRetry(msg, retry, decision, cause)
	admission.Latch(result)
	return result, true
}

// runWork invokes settlement-only work and converts a panic into Quarantine
// with a cause naming the owner, so the lane latches instead of the NATS
// callback unwinding. Heartbeat lanes do not need it: the typed helper
// synthesizes Quarantine on panic itself.
func runWork(
	ctx context.Context,
	data []byte,
	owner string,
	work natsclient.DeliveryWork,
) (decision natsclient.DeliveryDecision, cause error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = natsclient.DeliveryDecisionQuarantine
			cause = fmt.Errorf("%s delivery work panicked: %v", owner, recovered)
		}
	}()
	return work(ctx, data)
}

// noObserver is what Done returns for a binding no observer was started on:
// already closed, so a Stop that joins it returns at once instead of blocking
// on a nil channel forever.
var noObserver = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

// Binding retains the exact consume handle a lane committed and drains it at
// most once, whether Observe or the owner's Stop reaches it first. It is
// constructed after acquisition, so a fatal reported before the handle
// returned stays buffered in the Admission until Observe runs.
type Binding struct {
	handle    jetstream.ConsumeContext
	drainOnce sync.Once
	done      <-chan struct{}
}

// NewBinding wraps the handle acquisition returned. The owner retains the
// pointer for Stop; the package never enumerates or stops bindings itself.
func NewBinding(handle jetstream.ConsumeContext) *Binding {
	return &Binding{handle: handle, done: noObserver}
}

// Drain stops this lane through jetstream.ConsumeContext.Drain, not Stop. It is
// deliberately unsynchronized with Admission: the owner calls it from Stop,
// where the lane usually never latched, and nothing here waits on onFatal.
// On the FATAL path the ordering does hold, because Observe drains only after
// onFatal has returned and the result has been buffered — so every delivery
// that drain flushes hits closed admission, runs no work, attempts no terminal
// method, and stays pending for the reconstructed owner; an already-admitted
// in-flight delivery can finish and settle instead of being abandoned
// mid-effect. Repeated calls rejoin the one drain.
func (b *Binding) Drain() { b.drainOnce.Do(b.handle.Drain) }

// Closed is the exact handle's Closed channel, for the owner's Stop to await
// after Drain.
func (b *Binding) Closed() <-chan struct{} { return b.handle.Closed() }

// Done is never nil: it is closed once the observer started by Observe has
// exited, or already closed when no observer was started. Stop joins it after
// cancelling the observer's context.
func (b *Binding) Done() <-chan struct{} { return b.done }

// Observe starts the lane's owner-stop observer on ctx, which must derive
// from the owner's Start. On the first buffered fatal it runs react, then
// drains the exact handle; on ctx cancellation it exits without draining.
// react is required — a lane whose fatal drains with no owner reaction is a
// silent degrade — and a nil react fails here, at wiring, not at the first
// fatal. Call Observe before publishing the binding to other goroutines; the
// owner's lifecycle mutex is what orders Done against Stop.
func Observe(
	ctx context.Context,
	binding *Binding,
	admission *Admission,
	react func(natsclient.DeliveryResult),
) {
	if react == nil {
		panic("deliverylane: Observe requires a non-nil react")
	}
	done := make(chan struct{})
	binding.done = done
	go func() {
		defer close(done)
		select {
		case result := <-admission.fatal:
			react(result)
			binding.Drain()
		case <-ctx.Done():
		}
	}()
}
