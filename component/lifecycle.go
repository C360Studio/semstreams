package component

import (
	"context"

	"github.com/c360studio/semstreams/types"
)

// State represents the current lifecycle state of a component
type State int

const (
	// StateCreated indicates component was created but not initialized
	StateCreated State = iota
	// StateInitialized indicates component was initialized but not started
	StateInitialized
	// StateStarted indicates component is running
	StateStarted
	// StateStopped indicates component was stopped
	StateStopped
	// StateFailed indicates component failed during lifecycle operation
	StateFailed
)

// String returns a string representation of the component state
func (cs State) String() string {
	switch cs {
	case StateCreated:
		return "created"
	case StateInitialized:
		return "initialized"
	case StateStarted:
		return "started"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// LifecycleComponent defines the portable lifecycle contract for components.
// Initialize performs setup without runtime authority. Start accepts the context
// that owns continuing work. Stop uses its exact caller context to bound the
// component's terminal admission fence, cancellation, join, and cleanup; the
// resource owner determines that sequence's protocol-specific ordering. Exact
// resource ordering preserves admitted callback authority where native drain
// requires it; no universal cancel-before-drain order is implied.
//
// During controlled shutdown, callers keep the accepted Start context live until
// a separately bounded Stop drains, joins, and finalizes the owner and returns nil.
// Ending the Start context first is abort cancellation. Stop then makes bounded
// best-effort progress under its exact caller authority and may return accurate
// native cleanup or deadline errors. Abort cleanup does not invent replacement
// authority, detach cleanup, or promise a second rejoin; if the bound wins, the
// portable contract makes no complete-join or leak-freedom claim.
//
// Nil Start and Stop contexts are rejected before action. A completed repeated
// Stop is a no-op. Concurrent lifecycle calls, result replay, later rejoin after
// a Stop bound wins, reinitialization, and same-instance restart are not portable
// guarantees.
type LifecycleComponent interface {
	Discoverable
	Initialize() error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// ManagedComponent tracks a component and its lifecycle state
// This is used by ComponentManager to properly manage component lifecycle
type ManagedComponent struct {
	// Component is the actual component instance
	Component Discoverable

	// State tracks the current lifecycle state
	State State

	// Config is the effective component configuration this instance was
	// created from. The ComponentManager retains it so a per-component runtime
	// config update can restart the component only when the effective config
	// actually changed — a no-op update is skipped instead of stop/start-cycling
	// a healthy running component (gh#520).
	Config types.ComponentConfig

	// StartOrder tracks the order components were started for reverse shutdown
	StartOrder int

	// LastError tracks the last error that occurred during lifecycle operations
	LastError error
}

// IsLifecycleComponent checks if a component supports lifecycle management
func IsLifecycleComponent(comp Discoverable) bool {
	_, ok := comp.(LifecycleComponent)
	return ok
}

// AsLifecycleComponent safely casts a component to LifecycleComponent
func AsLifecycleComponent(comp Discoverable) (LifecycleComponent, bool) {
	lc, ok := comp.(LifecycleComponent)
	return lc, ok
}
