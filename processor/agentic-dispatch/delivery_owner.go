package agenticdispatch

import (
	"context"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

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
