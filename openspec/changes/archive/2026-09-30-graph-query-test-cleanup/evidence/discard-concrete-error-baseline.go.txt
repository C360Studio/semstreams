package graphquery

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// graphQueryTestOwner owns one concrete fixture on the lexical test goroutine.
// It keeps cancellation and terminal observations, never operation authority.
type graphQueryTestOwner struct {
	component       *Component
	cancelStart     context.CancelFunc
	attempted       bool
	concreteStopErr error
	stopBoundErr    error
}

func newGraphQueryTestOwner(component *Component) *graphQueryTestOwner {
	return &graphQueryTestOwner{component: component}
}

func (o *graphQueryTestOwner) startContext(parent context.Context) context.Context {
	if o.cancelStart != nil {
		panic("graph-query fixture Start authority already derived")
	}
	ctx, cancel := context.WithCancel(parent)
	o.cancelStart = cancel
	return ctx
}

func (o *graphQueryTestOwner) stop(operationCtx, startCtx context.Context, abort bool) error {
	if o.attempted {
		return errors.New("graph-query fixture Stop already attempted")
	}
	o.attempted = true // A concrete error or panic never permits implicit finalizer retry.
	if o.cancelStart != nil {
		defer o.cancelStart() // Preserve accepted Start authority through concrete Stop.
	}
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
	defer cancelStop()
	o.concreteStopErr = o.component.Stop(stopCtx)
	o.stopBoundErr = stopCtx.Err()
	result := o.concreteStopErr
	if o.stopBoundErr != nil {
		result = errors.Join(result, fmt.Errorf("graph-query terminal context ended: %w", o.stopBoundErr))
	}
	if !abort && operationCtx.Err() != nil {
		result = errors.Join(result, fmt.Errorf("graph-query operation authority ended before controlled Stop: %w", operationCtx.Err()))
	}
	if !abort && startCtx != nil && startCtx.Err() != nil {
		result = errors.Join(result, fmt.Errorf("graph-query Start authority ended before controlled Stop: %w", startCtx.Err()))
	}
	return result
}

func (o *graphQueryTestOwner) finish(operationCtx, startCtx context.Context, abort bool, t *testing.T) {
	t.Helper()
	if o.attempted {
		return
	}
	if err := o.stop(operationCtx, startCtx, abort); err != nil {
		t.Errorf("graph-query terminal cleanup: %v", err)
	}
}
