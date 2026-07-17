package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// PermanentDeliveryError marks a handler failure as structurally permanent for
// this exact message. ConsumeWithHeartbeat terminates the JetStream delivery
// instead of retrying it. Unwrap preserves the handler's typed error contract.
type PermanentDeliveryError struct {
	err error
}

func (e *PermanentDeliveryError) Error() string { return e.err.Error() }
func (e *PermanentDeliveryError) Unwrap() error { return e.err }

// TerminateDelivery marks err for JetStream Term handling. Transient and
// cancellation errors must be returned unchanged so their existing NAK paths
// remain intact.
func TerminateDelivery(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentDeliveryError{err: err}
}

// ConsumeWithHeartbeat runs work in a goroutine while periodically calling
// msg.InProgress() to reset the AckWait clock. This allows short AckWait
// values for failure detection while supporting arbitrarily long processing.
//
// Ack/Nak ownership: this function calls Ack, NakWithDelay, or Nak on the
// message. The caller must NOT call these methods when using this helper.
//
// On work success: msg.Ack()
// On permanent work error: msg.Term() so structurally invalid data is not retried
// On other work error: msg.NakWithDelay(30s) to allow breathing room before retry
// On context cancellation: msg.NakWithDelay(5s) for graceful shutdown
// On InProgress failure: returns error (message will be redelivered by server)
func ConsumeWithHeartbeat(
	ctx context.Context,
	msg jetstream.Msg,
	heartbeatInterval time.Duration,
	work func(context.Context) error,
) error {
	// Derive a cancellable context so we can stop the work goroutine
	// if the heartbeat fails. Without this, an InProgress() failure
	// leaves the work goroutine running while JetStream redelivers
	// the message — causing duplicate processing.
	workCtx, workCancel := context.WithCancel(ctx)
	defer workCancel()

	done := make(chan error, 1)
	go func() {
		done <- work(workCtx)
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := msg.InProgress(); err != nil {
				slog.Warn("Failed to send InProgress heartbeat",
					"error", err,
					"subject", msg.Subject())
				workCancel() // stop the orphaned work goroutine
				return fmt.Errorf("failed to send InProgress: %w", err)
			}

		case err := <-done:
			// Cancellation owns the delivery outcome even when work observes the
			// same cancellation and reports at nearly the same instant. Without
			// this check select can choose done first and spend the normal 30s
			// work-error NAK path instead of the one shutdown/restart NAK below.
			if ctx.Err() != nil {
				_ = msg.NakWithDelay(5 * time.Second)
				return ctx.Err()
			}
			if err != nil {
				var permanent *PermanentDeliveryError
				if errors.As(err, &permanent) {
					if termErr := msg.Term(); termErr != nil {
						return errors.Join(err, fmt.Errorf("terminate permanent delivery: %w", termErr))
					}
					return err
				}
				_ = msg.NakWithDelay(30 * time.Second)
				return err
			}
			return msg.Ack()

		case <-ctx.Done():
			_ = msg.NakWithDelay(5 * time.Second)
			return ctx.Err()
		}
	}
}
