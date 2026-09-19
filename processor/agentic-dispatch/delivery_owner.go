package agenticdispatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

func runDispatchDeliveryWork(
	ctx context.Context,
	data []byte,
	work func(context.Context, []byte) (natsclient.DeliveryDecision, error),
) (decision natsclient.DeliveryDecision, cause error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = natsclient.DeliveryDecisionQuarantine
			cause = fmt.Errorf("dispatch delivery work panicked: %v", recovered)
		}
	}()
	return work(ctx, data)
}

type deliveryLaneAdmission struct {
	mu        sync.Mutex
	open      bool
	fatal     chan natsclient.DeliveryResult
	onFatal   func(natsclient.DeliveryResult)
	onRefused func(subject string)
}

func newDeliveryLaneAdmission(
	onFatal func(natsclient.DeliveryResult),
	onRefused func(subject string),
) *deliveryLaneAdmission {
	return &deliveryLaneAdmission{
		open:      true,
		fatal:     make(chan natsclient.DeliveryResult, 1),
		onFatal:   onFatal,
		onRefused: onRefused,
	}
}

// refuse declares the refusal. The lane is Drain()ed rather than stopped, so
// buffered deliveries still reach this path after the latch; without this
// declaration only the first fatal is visible and every later refusal is a
// silent drop.
func (a *deliveryLaneAdmission) refuse(subject string) {
	if a.onRefused != nil {
		a.onRefused(subject)
	}
}

func (a *deliveryLaneAdmission) admit() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.open
}

func (a *deliveryLaneAdmission) latch(result natsclient.DeliveryResult) {
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

func consumeAdmittedDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	policy natsclient.HeartbeatDeliveryPolicy,
	admission *deliveryLaneAdmission,
) (natsclient.DeliveryResult, bool) {
	if !admission.admit() {
		subject := ""
		if msg != nil {
			subject = msg.Subject()
		}
		admission.refuse(subject)
		return natsclient.DeliveryResult{}, false
	}
	result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)
	admission.latch(result)
	return result, true
}

func newStreamConsumerBinding(handle jetstream.ConsumeContext) streamConsumerBinding {
	return streamConsumerBinding{handle: handle, drainOnce: &sync.Once{}}
}

// drain stops this lane through `jetstream.ConsumeContext.Drain`, not `Stop`.
// #759's acceptance wording says the exact handle is "stopped" with "no later
// delivery before explicit reconstruction"; tasks.md 4.7 records Drain as the
// implementation, and the two agree because admission latches BEFORE the
// handle is drained: every buffered delivery Drain flushes hits closed
// admission, runs no work, attempts no terminal method, is declared through
// recordDeliveryRefused, and stays pending for the reconstructed owner. Drain
// is preferred over Stop so an already-admitted in-flight delivery can finish
// and settle instead of being abandoned mid-effect.
func (b *streamConsumerBinding) drain() {
	if b.drainOnce == nil {
		b.drainOnce = &sync.Once{}
	}
	b.drainOnce.Do(b.handle.Drain)
}

func (c *Component) observeDeliveryLane(
	ctx context.Context,
	binding *streamConsumerBinding,
	admission *deliveryLaneAdmission,
) {
	done := make(chan struct{})
	binding.observerDone = done
	go func() {
		defer close(done)
		select {
		case result := <-admission.fatal:
			c.observeTerminalDelivery(result.Err())
			c.logger.Error("Terminal delivery ownership lost", slog.Any("error", result.Err()))
			binding.drain()
		case <-ctx.Done():
		}
	}()
}
