package component

import (
	"context"
	"time"
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

	// Named Context Management for Individual Component Lifecycle Control
	//
	// These fields store named child contexts to enable individual component cancellation
	// during shutdown. This follows the pattern where ComponentManager creates a child
	// context for each component and passes it to lifecycle.Start(ctx).
	//
	// The component itself NEVER stores the context - it receives it as a parameter
	// following proper Go idioms. Only the ComponentManager stores these contexts
	// to coordinate orderly shutdown and individual component cancellation.
	//
	// Pattern:
	//   1. ComponentManager creates: ctx, cancel := context.WithCancel(parentCtx)
	//   2. ComponentManager stores: mc.Context = ctx, mc.Cancel = cancel
	//   3. ComponentManager calls: lifecycle.Start(mc.Context)
	//   4. Component uses context as parameter (proper Go idiom)
	//   5. ComponentManager can cancel individual components: mc.Cancel()
	Context context.Context    // Named child context for this specific component
	Cancel  context.CancelFunc // Named cancellation for this specific component

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
