package agenticgovernance

import (
	"context"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// deliveryLaneAdmission is private to this component owner, and is the same
// shape agentic-loop, agentic-model, agentic-dispatch and agentic-tools each
// carry in their own delivery_owner.go. JetStream retains delivery authority;
// this latch only prevents new local work after ownership control becomes
// unsafe.
//
// Governance previously spelled this inline in setupConsumer as three locals
// and an ad-hoc goroutine. Same shape, fifth spelling — and a spelling nobody
// could grep for beside the other four.
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

func newStreamConsumerBinding(handle jetstream.ConsumeContext) streamConsumerBinding {
	return streamConsumerBinding{handle: handle, drainOnce: &sync.Once{}}
}

func (c *Component) observeDeliveryLane(
	ctx context.Context,
	binding *streamConsumerBinding,
	admission *deliveryLaneAdmission,
	portName string,
) {
	done := make(chan struct{})
	binding.observerDone = done
	go func() {
		defer close(done)
		select {
		case result := <-admission.fatal:
			c.logger.Error("Governance delivery ownership lost", "port", portName, "error", result.Err())
			binding.drain()
		case <-ctx.Done():
		}
	}()
}
