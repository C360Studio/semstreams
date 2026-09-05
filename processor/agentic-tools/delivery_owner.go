package agentictools

import (
	"context"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// deliveryLaneAdmission is private to this component owner. JetStream retains
// delivery authority; this latch only prevents new local work after ownership
// control becomes unsafe.
type deliveryLaneAdmission struct {
	mu      sync.Mutex
	open    bool
	fatal   chan natsclient.DeliveryResult
	onFatal func(natsclient.DeliveryResult)
}

func newDeliveryLaneAdmission(onFatal func(natsclient.DeliveryResult)) *deliveryLaneAdmission {
	return &deliveryLaneAdmission{open: true, fatal: make(chan natsclient.DeliveryResult, 1), onFatal: onFatal}
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
			c.recordHandlerError(ctx, result.Err())
			binding.drain()
		case <-ctx.Done():
		}
	}()
}
