package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	loopFastDeliveryAckWait    = 30 * time.Second
	loopFastDeliveryWorkBudget = 25 * time.Second
)

type loopFastDeliveryWork func(context.Context, []byte) (natsclient.DeliveryDecision, error)

func consumeAdmittedLoopFastDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	work loopFastDeliveryWork,
	admission *deliveryLaneAdmission,
) (natsclient.DeliveryDecision, bool, error) {
	if !admission.admit() {
		return natsclient.DeliveryDecisionInvalid, false, nil
	}
	decision, err := consumeLoopFastDelivery(ctx, msg, work)
	if decision == natsclient.DeliveryDecisionQuarantine {
		admission.latchFatal(err)
	}
	return decision, true, err
}

func consumeLoopFastDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	work loopFastDeliveryWork,
) (natsclient.DeliveryDecision, error) {
	workCtx, cancel := context.WithTimeout(ctx, loopFastDeliveryWorkBudget)
	decision, workErr := runLoopFastDeliveryWork(workCtx, msg.Data(), work)
	deadlineErr := workCtx.Err()
	cancel()
	if errors.Is(deadlineErr, context.DeadlineExceeded) {
		decision = natsclient.DeliveryDecisionRetry
		workErr = errors.Join(
			workErr,
			fmt.Errorf("loop fast delivery work budget %s exceeded: %w", loopFastDeliveryWorkBudget, deadlineErr),
		)
	}
	decision, workErr = validateLoopFastDeliveryResult(decision, workErr)
	return decision, settleLoopFastDelivery(msg, decision, workErr)
}

func runLoopFastDeliveryWork(
	ctx context.Context,
	data []byte,
	work loopFastDeliveryWork,
) (decision natsclient.DeliveryDecision, workErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = natsclient.DeliveryDecisionQuarantine
			workErr = fmt.Errorf("loop fast delivery work panicked: %v", recovered)
		}
	}()
	if work == nil {
		return natsclient.DeliveryDecisionQuarantine, errors.New("loop fast delivery work is nil")
	}
	return work(ctx, data)
}

func validateLoopFastDeliveryResult(
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
		errors.Join(fmt.Errorf("invalid loop fast delivery decision %d and error tuple", decision), workErr)
}

func settleLoopFastDelivery(
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
