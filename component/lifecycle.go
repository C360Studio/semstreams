package component

import (
	"context"
	"time"

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

// LifecycleComponent defines components that support full lifecycle management
// following the unified Pattern A:
//   - Initialize() error                     // Setup/create only, NO context
//   - Start(ctx context.Context) error      // Start with context passed through
//   - Stop(timeout time.Duration) error     // Stop with timeout for graceful shutdown
type LifecycleComponent interface {
	Discoverable
	Initialize() error
	Start(ctx context.Context) error
	Stop(timeout time.Duration) error
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

	// Cancel lets ComponentManager signal this specific component to stop. The
	// child context itself is passed directly to Start and is never retained on
	// this mutable lifecycle record.
	Cancel context.CancelFunc

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
