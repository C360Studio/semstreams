package agenticloop

import (
	"context"
	"errors"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// consumeTypedLongRunningInput exercises older intake behavior tests through
// the same typed settlement contract used by the production heartbeat owner.
func consumeTypedLongRunningInput(
	ctx context.Context,
	msg jetstream.Msg,
	heartbeatInterval time.Duration,
	handler inputHandler,
) error {
	retry, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
	if err != nil {
		return err
	}
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		ctx,
		natsclient.StreamConsumerConfig{AckWait: 2 * heartbeatInterval},
		heartbeatInterval,
		retry,
		func(workCtx context.Context, _ natsclient.DeliveryAttempt, data []byte) (natsclient.DeliveryDecision, error) {
			handlerErr := handler(workCtx, data)
			if handlerErr == nil {
				return natsclient.DeliveryDecisionAck, nil
			}
			var permanent *natsclient.PermanentDeliveryError
			if errors.As(handlerErr, &permanent) {
				return natsclient.DeliveryDecisionTerminate, handlerErr
			}
			return natsclient.DeliveryDecisionRetry, handlerErr
		},
	)
	if err != nil {
		return err
	}
	return natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy).Err()
}
