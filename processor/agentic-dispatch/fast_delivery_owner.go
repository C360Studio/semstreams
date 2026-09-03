package agenticdispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	dispatchFastDeliveryAckWait    = 30 * time.Second
	dispatchFastDeliveryWorkBudget = 25 * time.Second
)

type dispatchFastDeliveryWork func(context.Context, []byte) (natsclient.DeliveryDecision, error)

func consumeAdmittedDispatchFastDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	work dispatchFastDeliveryWork,
	admission *deliveryLaneAdmission,
) (natsclient.DeliveryDecision, bool, error) {
	if !admission.admit() {
		return natsclient.DeliveryDecisionInvalid, false, nil
	}
	decision, err := consumeDispatchFastDelivery(ctx, msg, work)
	if decision == natsclient.DeliveryDecisionQuarantine {
		admission.latchFatal(err)
	}
	return decision, true, err
}

func consumeDispatchFastDelivery(
	ctx context.Context,
	msg jetstream.Msg,
	work dispatchFastDeliveryWork,
) (natsclient.DeliveryDecision, error) {
	workCtx, cancel := context.WithTimeout(ctx, dispatchFastDeliveryWorkBudget)
	decision, workErr := runDispatchFastDeliveryWork(workCtx, msg.Data(), work)
	deadlineErr := workCtx.Err()
	cancel()
	if errors.Is(deadlineErr, context.DeadlineExceeded) {
		decision = natsclient.DeliveryDecisionRetry
		workErr = errors.Join(
			workErr,
			fmt.Errorf("dispatch fast delivery work budget %s exceeded: %w", dispatchFastDeliveryWorkBudget, deadlineErr),
		)
	}
	decision, workErr = validateDispatchFastDeliveryResult(decision, workErr)
	return decision, settleDispatchFastDelivery(msg, decision, workErr)
}

func runDispatchFastDeliveryWork(
	ctx context.Context,
	data []byte,
	work dispatchFastDeliveryWork,
) (decision natsclient.DeliveryDecision, workErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			decision = natsclient.DeliveryDecisionQuarantine
			workErr = fmt.Errorf("dispatch fast delivery work panicked: %v", recovered)
		}
	}()
	if work == nil {
		return natsclient.DeliveryDecisionQuarantine, errors.New("dispatch fast delivery work is nil")
	}
	return work(ctx, data)
}

func validateDispatchFastDeliveryResult(
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
		errors.Join(fmt.Errorf("invalid dispatch fast delivery decision %d and error tuple", decision), workErr)
}

func settleDispatchFastDelivery(
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
