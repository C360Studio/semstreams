//go:build integration

package rule

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
)

type graphIngestTestOwner struct {
	component   *graphingest.Component
	cancelStart context.CancelFunc
	attempted   bool
	transferred bool
}

func newGraphIngestTestOwner(component *graphingest.Component) *graphIngestTestOwner {
	return &graphIngestTestOwner{component: component}
}

func (o *graphIngestTestOwner) startContext(operationCtx context.Context) context.Context {
	ctx, cancel := context.WithCancel(operationCtx)
	o.cancelStart = cancel
	return ctx
}

func (o *graphIngestTestOwner) stop(operationCtx context.Context) error {
	if o.attempted {
		return errors.New("rule graph-ingest fixture Stop already attempted")
	}
	o.attempted = true
	if o.cancelStart != nil {
		defer o.cancelStart()
	}
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
	defer cancelStop()
	stopErr := o.component.Stop(stopCtx)
	if err := stopCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("rule graph-ingest terminal context ended: %w", err))
	}
	if err := operationCtx.Err(); err != nil {
		stopErr = errors.Join(stopErr, fmt.Errorf("rule graph-ingest operation authority ended before controlled Stop: %w", err))
	}
	return stopErr
}

func (o *graphIngestTestOwner) finish(operationCtx context.Context, t *testing.T) {
	t.Helper()
	if !o.attempted {
		if err := o.stop(operationCtx); err != nil {
			t.Errorf("rule graph-ingest terminal cleanup: %v", err)
		}
	}
}

func (o *graphIngestTestOwner) provisionalFinish(operationCtx context.Context, t *testing.T) {
	t.Helper()
	if !o.transferred {
		o.finish(operationCtx, t)
	}
}

func (o *graphIngestTestOwner) transfer() { o.transferred = true }
