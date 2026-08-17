package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// ErrHeartbeatFailed identifies loss of the InProgress settlement path while
// work may already have caused an external effect.
var ErrHeartbeatFailed = errors.New("delivery heartbeat failed")

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

func nonCancellationWorkError(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var retained []error
		for _, child := range joined.Unwrap() {
			if child = nonCancellationWorkError(child); child != nil {
				retained = append(retained, child)
			}
		}
		return errors.Join(retained...)
	}
	if unwrapped := errors.Unwrap(err); unwrapped != nil &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return nonCancellationWorkError(unwrapped)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
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
// Cancellation and heartbeat failure wait for work to exit before returning
// delivery control.
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
				workCancel()
				workErr := <-done
				heartbeatErr := errors.Join(ErrHeartbeatFailed, fmt.Errorf("failed to send InProgress: %w", err))
				if cleanupErr := nonCancellationWorkError(workErr); cleanupErr != nil {
					return errors.Join(heartbeatErr, fmt.Errorf("work cleanup after heartbeat failure: %w", cleanupErr))
				}
				return heartbeatErr
			}

		case err := <-done:
			// Cancellation owns the delivery outcome even when work observes the
			// same cancellation and reports at nearly the same instant. Without
			// this check select can choose done first and spend the normal 30s
			// work-error NAK path instead of the one shutdown/restart NAK below.
			if ctx.Err() != nil {
				if nakErr := msg.NakWithDelay(5 * time.Second); nakErr != nil {
					return errors.Join(ctx.Err(), fmt.Errorf("NAK cancelled delivery: %w", nakErr))
				}
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
				if nakErr := msg.NakWithDelay(30 * time.Second); nakErr != nil {
					return errors.Join(err, fmt.Errorf("NAK transient delivery: %w", nakErr))
				}
				return err
			}
			return msg.Ack()

		case <-ctx.Done():
			workCancel()
			<-done
			if nakErr := msg.NakWithDelay(5 * time.Second); nakErr != nil {
				return errors.Join(ctx.Err(), fmt.Errorf("NAK cancelled delivery: %w", nakErr))
			}
			return ctx.Err()
		}
	}
}
