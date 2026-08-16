package errs

import (
	"fmt"
)

// ShutdownPhase identifies the closed set of graceful-shutdown boundaries.
type ShutdownPhase string

const (
	// PhaseDrainConsumers identifies accepted consumer work being quiesced.
	PhaseDrainConsumers ShutdownPhase = "drain_consumers"
	// PhaseDrainSubscriptions identifies subscription or watcher drainage.
	PhaseDrainSubscriptions ShutdownPhase = "drain_subscriptions"
	// PhaseShutdownListener identifies graceful network-listener shutdown.
	PhaseShutdownListener ShutdownPhase = "shutdown_listener"
	// PhaseJoinRuntime identifies waiting for Start-owned runtime completion.
	PhaseJoinRuntime ShutdownPhase = "join_runtime"
	// PhaseCloseTransport identifies final transport closure.
	PhaseCloseTransport ShutdownPhase = "close_transport"
)

// ShutdownError attributes a shutdown failure to its stable owner and phase.
type ShutdownError struct {
	Owner string
	Phase ShutdownPhase
	Err   error
}

func (e *ShutdownError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("shutdown %s at %s: %v", e.Owner, e.Phase, e.Err)
}

// Unwrap preserves the underlying cancellation, deadline, or genuine failure.
func (e *ShutdownError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewShutdownError constructs a validated shutdown failure. Nil causes remain
// nil so optional phases do not manufacture failures.
func NewShutdownError(owner string, phase ShutdownPhase, err error) error {
	if owner == "" {
		return fmt.Errorf("shutdown error: owner must not be empty")
	}
	if !phase.valid() {
		return fmt.Errorf("shutdown error: unknown phase %q", phase)
	}
	if err == nil {
		return nil
	}
	return &ShutdownError{Owner: owner, Phase: phase, Err: err}
}

func (p ShutdownPhase) valid() bool {
	switch p {
	case PhaseDrainConsumers,
		PhaseDrainSubscriptions,
		PhaseShutdownListener,
		PhaseJoinRuntime,
		PhaseCloseTransport:
		return true
	default:
		return false
	}
}
