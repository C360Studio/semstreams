//go:build integration

package rule_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/processor/rule"
)

type processorTestOwner struct {
	processor   *rule.Processor
	cancelStart context.CancelFunc
	attempted   bool
}

func newProcessorTestOwner(processor *rule.Processor) *processorTestOwner {
	return &processorTestOwner{processor: processor}
}

func (o *processorTestOwner) startContext(operationCtx context.Context) context.Context {
	ctx, cancel := context.WithCancel(operationCtx)
	o.cancelStart = cancel
	return ctx
}

func (o *processorTestOwner) stop(operationCtx context.Context) error {
	if o.attempted {
		return errors.New("external rule Processor fixture Stop already attempted")
	}
	o.attempted = true
	if o.cancelStart != nil {
		defer o.cancelStart()
	}
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
	defer cancelStop()
	stopErr := o.processor.Stop(stopCtx)
	if err := stopCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("external rule Processor terminal context ended: %w", err))
	}
	if err := operationCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("external rule Processor operation authority ended before controlled Stop: %w", err))
	}
	return stopErr
}

func (o *processorTestOwner) finish(operationCtx context.Context, t *testing.T) {
	t.Helper()
	if !o.attempted {
		if err := o.stop(operationCtx); err != nil {
			t.Errorf("external rule Processor terminal cleanup: %v", err)
		}
	}
}
