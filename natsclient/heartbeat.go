package natsclient

import "errors"

// ErrHeartbeatFailed identifies loss of the InProgress settlement path while
// work may already have caused an external effect.
var ErrHeartbeatFailed = errors.New("delivery heartbeat failed")

// PermanentDeliveryError marks a handler failure as structurally permanent for
// this exact message: a binding maps it to DeliveryDecisionTerminate rather
// than retrying a message no redelivery can fix. Unwrap preserves the handler's
// typed error contract.
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
