// Package lifecyclejoin owns the private, generation-scoped mechanics shared
// by SemStreams lifecycle implementations. It is internal framework plumbing,
// not an adopter-facing lifecycle API.
package lifecyclejoin

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Generation coordinates one Start-owned runtime generation. It stores only
// cancellation, completion, and terminal result authority; it never retains a
// context.
type Generation struct {
	cancel     context.CancelFunc
	cancelOnce sync.Once
	done       chan struct{}

	signalOnce sync.Once
	signalErr  error
	stop       *Operation
}

// NewGeneration publishes Start-owned completion for a fixed join set. Add all
// fixed goroutines before calling it. Protocol callback completion belongs to
// the protocol's native drain or shutdown authority, not this join.
func NewGeneration(cancel context.CancelFunc, wait func()) *Generation {
	g := &Generation{
		cancel: cancel,
		done:   make(chan struct{}),
		stop:   NewOperation(),
	}
	if wait == nil {
		close(g.done)
		return g
	}
	go func() {
		wait()
		close(g.done)
	}()
	return g
}

// Stop signals generation cancellation and component-specific shutdown once,
// joins Start-owned work under ctx, and runs terminal cleanup once. A context
// expiry leaves shared completion open so a later authorized Stop can finish.
func (g *Generation) Stop(
	ctx context.Context,
	signal func() error,
	cleanup func(context.Context) error,
) error {
	if ctx == nil {
		return errors.New("lifecycle join: nil Stop context")
	}
	if g == nil {
		return nil
	}
	g.Signal(signal)
	stopErr := g.stop.Run(ctx, func(ctx context.Context) error {
		select {
		case <-g.done:
		case <-ctx.Done():
			return fmt.Errorf("wait for Start-owned runtime: %w", ctx.Err())
		}
		if cleanup != nil {
			return cleanup(ctx)
		}
		return nil
	})
	return errors.Join(g.signalErr, stopErr)
}

// Signal cancels the generation and runs an immediate, context-free shutdown
// signal exactly once. It is useful for protocol shutdown signals that must
// precede a context-bound join.
func (g *Generation) Signal(signal func() error) error {
	if g == nil {
		return nil
	}
	g.Cancel()
	g.signalOnce.Do(func() {
		if signal != nil {
			g.signalErr = signal()
		}
	})
	return g.signalErr
}

// Cancel signals the generation lifetime exactly once. Components whose
// servers require a context-bound pre-join Shutdown call use this before that
// call, then enter Stop to join the same generation.
func (g *Generation) Cancel() {
	if g == nil {
		return
	}
	g.cancelOnce.Do(func() {
		if g.cancel != nil {
			g.cancel()
		}
	})
}
