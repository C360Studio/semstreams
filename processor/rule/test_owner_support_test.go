package rule

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// processorTestOwner delays its private Start cancellation until concrete Stop
// returns, including on an assertion escape.
type processorTestOwner struct {
	processor   *Processor
	cancelStart context.CancelFunc
	attempted   bool
	transferred bool
}

func newProcessorTestOwner(processor *Processor) *processorTestOwner {
	return &processorTestOwner{processor: processor}
}

func (o *processorTestOwner) startContext(operationCtx context.Context) context.Context {
	ctx, cancel := context.WithCancel(operationCtx)
	o.cancelStart = cancel
	return ctx
}

func (o *processorTestOwner) stop(operationCtx context.Context) error {
	if o.attempted {
		return errors.New("rule Processor fixture Stop already attempted")
	}
	o.attempted = true
	if o.cancelStart != nil {
		defer o.cancelStart()
	}
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
	defer cancelStop()
	stopErr := o.processor.Stop(stopCtx)
	if err := stopCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("rule Processor terminal context ended: %w", err))
	}
	if err := operationCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("rule Processor operation authority ended before controlled Stop: %w", err))
	}
	return stopErr
}

func (o *processorTestOwner) finish(operationCtx context.Context, t *testing.T) {
	t.Helper()
	if !o.attempted {
		if err := o.stop(operationCtx); err != nil {
			t.Errorf("rule Processor terminal cleanup: %v", err)
		}
	}
}

func (o *processorTestOwner) provisionalFinish(operationCtx context.Context, t *testing.T) {
	t.Helper()
	if !o.transferred {
		o.finish(operationCtx, t)
	}
}

func (o *processorTestOwner) transfer() { o.transferred = true }

// cronSchedulerTestOwner is separate because CronScheduler has its own native
// Stop contract, including a documented retry after an expired Stop context.
type cronSchedulerTestOwner struct {
	scheduler   *CronScheduler
	cancelStart context.CancelFunc
	attempted   bool
}

func newCronSchedulerTestOwner(scheduler *CronScheduler) *cronSchedulerTestOwner {
	return &cronSchedulerTestOwner{scheduler: scheduler}
}

func (o *cronSchedulerTestOwner) startContext(operationCtx context.Context) context.Context {
	ctx, cancel := context.WithCancel(operationCtx)
	o.cancelStart = cancel
	return ctx
}

func (o *cronSchedulerTestOwner) stop(operationCtx context.Context) error {
	if o.attempted {
		return errors.New("rule CronScheduler fixture Stop already attempted")
	}
	o.attempted = true
	if o.cancelStart != nil {
		defer o.cancelStart()
	}
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
	defer cancelStop()
	stopErr := o.scheduler.Stop(stopCtx)
	if err := stopCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("rule CronScheduler terminal context ended: %w", err))
	}
	if err := operationCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("rule CronScheduler operation authority ended before controlled Stop: %w", err))
	}
	return stopErr
}

func (o *cronSchedulerTestOwner) finish(operationCtx context.Context, t *testing.T) {
	t.Helper()
	if !o.attempted {
		if err := o.stop(operationCtx); err != nil {
			t.Errorf("rule CronScheduler terminal cleanup: %v", err)
		}
	}
}
