package agenticgovernance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	governanceFastDeliveryAckWait    = 30 * time.Second
	governanceFastDeliveryWorkBudget = 25 * time.Second
)

type governanceFastDeliveryWork func(context.Context, []byte) (natsclient.DeliveryDecision, error)

type governanceDeliveryLaneAdmission struct {
	mu      sync.Mutex
	open    bool
	fatal   chan error
	onFatal func(error)
}

func newGovernanceDeliveryLaneAdmission(onFatal func(error)) *governanceDeliveryLaneAdmission {
	return &governanceDeliveryLaneAdmission{open: true, fatal: make(chan error, 1), onFatal: onFatal}
}

func (a *governanceDeliveryLaneAdmission) admit() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.open
}

func (a *governanceDeliveryLaneAdmission) latchFatal(err error) {
	a.mu.Lock()
	if !a.open {
		a.mu.Unlock()
		return
	}
	a.open = false
	a.mu.Unlock()
	if a.onFatal != nil {
		a.onFatal(err)
	}
	a.fatal <- err
}

func consumeAdmittedGovernanceFastDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	work governanceFastDeliveryWork,
	admission *governanceDeliveryLaneAdmission,
) (natsclient.DeliveryDecision, bool, error) {
	if !admission.admit() {
		return natsclient.DeliveryDecisionInvalid, false, nil
	}
	decision, err := consumeGovernanceFastDelivery(ctx, msg, work)
	if decision == natsclient.DeliveryDecisionQuarantine {
		admission.latchFatal(err)
	}
	return decision, true, err
}

func consumeGovernanceFastDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	work governanceFastDeliveryWork,
) (natsclient.DeliveryDecision, error) {
	workCtx, cancel := context.WithTimeout(ctx, governanceFastDeliveryWorkBudget)
	decision, workErr := runGovernanceFastDeliveryWork(workCtx, msg.Data(), work)
	deadlineErr := workCtx.Err()
	cancel()
	if errors.Is(deadlineErr, context.DeadlineExceeded) {
		decision = natsclient.DeliveryDecisionRetry
		workErr = errors.Join(
			workErr,
			fmt.Errorf("governance fast delivery work budget %s exceeded: %w", governanceFastDeliveryWorkBudget, deadlineErr),
		)
	}
	decision, workErr = validateGovernanceFastDeliveryResult(decision, workErr)
	return decision, settleGovernanceFastDelivery(msg, decision, workErr)
}

func runGovernanceFastDeliveryWork(
	ctx context.Context,
	data []byte,
	work governanceFastDeliveryWork,
) (decision natsclient.DeliveryDecision, workErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = natsclient.DeliveryDecisionQuarantine
			workErr = fmt.Errorf("governance fast delivery work panicked: %v", recovered)
		}
	}()
	if work == nil {
		return natsclient.DeliveryDecisionQuarantine, errors.New("governance fast delivery work is nil")
	}
	return work(ctx, data)
}

func validateGovernanceFastDeliveryResult(
	decision natsclient.DeliveryDecision,
	workErr error,
) (natsclient.DeliveryDecision, error) {
	valid := decision == natsclient.DeliveryDecisionAck && workErr == nil ||
		(decision == natsclient.DeliveryDecisionRetry ||
			decision == natsclient.DeliveryDecisionTerminate ||
			decision == natsclient.DeliveryDecisionQuarantine) && workErr != nil
	if valid {
		return decision, workErr
	}
	return natsclient.DeliveryDecisionQuarantine,
		errors.Join(fmt.Errorf("invalid governance fast delivery decision %d and error tuple", decision), workErr)
}

func settleGovernanceFastDelivery(
	msg jetstream.Msg,
	decision natsclient.DeliveryDecision,
	workErr error,
) error {
	var settlementErr error
	switch decision {
	case natsclient.DeliveryDecisionAck:
		settlementErr = msg.Ack()
	case natsclient.DeliveryDecisionRetry:
		settlementErr = msg.Nak()
	case natsclient.DeliveryDecisionTerminate:
		settlementErr = msg.Term()
	case natsclient.DeliveryDecisionQuarantine:
		return workErr
	default:
		return fmt.Errorf("invalid delivery decision %d", decision)
	}
	return errors.Join(workErr, settlementErr)
}

func (c *Component) observeGovernanceDeliveryLane(
	ctx context.Context,
	binding *streamConsumerBinding,
	admission *governanceDeliveryLaneAdmission,
	portName string,
) {
	done := make(chan struct{})
	binding.observerDone = done
	go func() {
		defer close(done)
		select {
		case err := <-admission.fatal:
			c.logger.Error("Governance delivery ownership lost", slog.String("port", portName), slog.Any("error", err))
			binding.drain()
		case <-ctx.Done():
		}
	}()
}

func newGovernanceStreamConsumerBinding(handle jetstream.ConsumeContext) streamConsumerBinding {
	return streamConsumerBinding{handle: handle, drainOnce: &sync.Once{}}
}

func (b *streamConsumerBinding) drain() {
	if b.drainOnce == nil {
		b.drainOnce = &sync.Once{}
	}
	b.drainOnce.Do(b.handle.Drain)
}
