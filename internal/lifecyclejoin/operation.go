package lifecyclejoin

import (
	"context"
	"errors"
	"sync"

	shutdownerrs "github.com/c360studio/semstreams/pkg/errs"
)

// Operation coordinates one context-bound protocol shutdown operation. The
// executing caller runs the operation synchronously; concurrent callers join
// its result. Context expiry leaves the operation available for a later caller
// with a fresh Stop budget.
type Operation struct {
	executor chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	err      error
}

// NewOperation creates a protocol shutdown operation join.
func NewOperation() *Operation {
	return &Operation{
		executor: make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
}

// Run executes or joins op under ctx. A context-bound result remains resumable;
// every other result is retained and replayed.
func (o *Operation) Run(ctx context.Context, op func(context.Context) error) error {
	if ctx == nil {
		return errors.New("lifecycle join: nil Operation context")
	}
	if o == nil {
		return nil
	}
	select {
	case o.executor <- struct{}{}:
		defer func() { <-o.executor }()
	default:
		select {
		case o.executor <- struct{}{}:
			defer func() { <-o.executor }()
		case <-o.done:
			return errors.Join(o.result(), ctx.Err())
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case <-o.done:
		return errors.Join(o.result(), ctx.Err())
	default:
	}

	var err error
	if op != nil {
		err = op(ctx)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		o.accumulate(nonContextError(err, ctxErr))
		if err != nil {
			return errors.Join(o.result(), err)
		}
		return errors.Join(o.result(), ctxErr)
	}
	o.mu.Lock()
	o.err = errors.Join(o.err, err)
	close(o.done)
	err = o.err
	o.mu.Unlock()
	return err
}

func (o *Operation) accumulate(err error) {
	if err == nil {
		return
	}
	o.mu.Lock()
	o.err = errors.Join(o.err, err)
	o.mu.Unlock()
}

func (o *Operation) result() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

// nonContextError removes the caller's expired-context branch while retaining
// genuine errors returned alongside it. Joined errors are the standard shape
// for reporting both. Structured shutdown attribution is rebuilt around its
// filtered genuine cause so owner and phase survive later authorized rejoin.
func nonContextError(err, ctxErr error) error {
	if err == nil || ctxErr == nil {
		return err
	}
	if shutdownErr, ok := err.(*shutdownerrs.ShutdownError); ok {
		filtered := nonContextError(shutdownErr.Err, ctxErr)
		if filtered == nil {
			return nil
		}
		return &shutdownerrs.ShutdownError{
			Owner: shutdownErr.Owner,
			Phase: shutdownErr.Phase,
			Err:   filtered,
		}
	}
	if children, ok := err.(interface{ Unwrap() []error }); ok {
		filtered := make([]error, 0, len(children.Unwrap()))
		for _, child := range children.Unwrap() {
			if childErr := nonContextError(child, ctxErr); childErr != nil {
				filtered = append(filtered, childErr)
			}
		}
		return errors.Join(filtered...)
	}
	if child, ok := err.(interface{ Unwrap() error }); ok && errors.Is(err, ctxErr) {
		return nonContextError(child.Unwrap(), ctxErr)
	}
	if errors.Is(err, ctxErr) {
		return nil
	}
	return err
}
