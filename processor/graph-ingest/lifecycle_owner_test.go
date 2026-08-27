package graphingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/pkg/dispatch"
	"github.com/c360studio/semstreams/pkg/errs"
)

type graphIngestLifecycleConsumeContext struct {
	closed    chan struct{}
	drainSeen chan struct{}
	drainOnce sync.Once
	drains    atomic.Int32
}

func (*graphIngestLifecycleConsumeContext) Stop() { panic("unexpected force Stop") }

func (c *graphIngestLifecycleConsumeContext) Drain() {
	c.drainOnce.Do(func() {
		c.drains.Add(1)
		close(c.drainSeen)
	})
}

func (c *graphIngestLifecycleConsumeContext) Closed() <-chan struct{} { return c.closed }

type graphIngestLifecycleCoreSubscription struct {
	drainSeen chan struct{}
	drainOnce sync.Once
}

func (s *graphIngestLifecycleCoreSubscription) Drain(context.Context) error {
	s.drainOnce.Do(func() { close(s.drainSeen) })
	return nil
}

func TestLifecycleOwnerRunningStopPreservesEffectSettlementOrder(t *testing.T) {
	consumer := &graphIngestLifecycleConsumeContext{
		closed: make(chan struct{}), drainSeen: make(chan struct{}),
	}
	coreSub := &graphIngestLifecycleCoreSubscription{drainSeen: make(chan struct{})}
	processEntered := make(chan struct{})
	releaseProcess := make(chan struct{})
	processDone := make(chan struct{})
	poolCtx, poolCancel := context.WithCancel(t.Context())
	pool, err := dispatch.NewKeyedPool(poolCtx, dispatch.KeyedConfig[ingestWork]{
		Lanes:      1,
		QueueDepth: 1,
		KeyOf:      func(ingestWork) string { return "entity" },
		Process: func(ctx context.Context, _ int, _ ingestWork) error {
			close(processEntered)
			select {
			case <-releaseProcess:
				close(processDone)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, dispatch.KeyedDeps{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("construct keyed pool: %v", err)
	}
	if err := pool.Submit(ingestWork{}); err != nil {
		t.Fatalf("submit work: %v", err)
	}
	<-processEntered

	runCtx, runCancel := context.WithCancel(t.Context())
	submitCtx, submitCancel := context.WithCancel(runCtx)
	owner := withTestRegistry(t, &Component{
		logger:             slog.Default(),
		lifecycleUsed:      true,
		running:            true,
		cancel:             runCancel,
		ingestPool:         pool,
		ingestPoolCancel:   poolCancel,
		ingestSubmitCancel: submitCancel,
		consumers: []graphIngestConsumerBinding{{
			handle: consumer,
		}},
		subscriptions:  []graphIngestCoreSubscription{coreSub},
		boundConsumers: []boundConsumer{{stream: "ENTITY", name: "graph-ingest-entity"}},
	})

	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(t.Context()) }()
	<-consumer.drainSeen
	if err := submitCtx.Err(); err != nil {
		t.Fatalf("submission authority canceled before native Closed: %v", err)
	}
	if err := poolCtx.Err(); err != nil {
		t.Fatalf("pool authority canceled before native Closed: %v", err)
	}
	if got := owner.boundConsumerSnapshot(); len(got) != 1 {
		t.Fatalf("readiness observation removed before native Closed: %v", got)
	}
	select {
	case <-coreSub.drainSeen:
		t.Fatal("core subscription drained before JetStream Closed and keyed settlement")
	default:
	}

	close(consumer.closed)
	<-submitCtx.Done()
	select {
	case <-coreSub.drainSeen:
		t.Fatal("core subscription drained before keyed pool completed admitted work")
	default:
	}
	close(releaseProcess)
	<-processDone
	<-coreSub.drainSeen
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if consumer.drains.Load() != 1 {
		t.Fatalf("native Drain calls = %d, want 1", consumer.drains.Load())
	}
	if !errors.Is(runCtx.Err(), context.Canceled) {
		t.Fatalf("runtime context after Stop = %v, want canceled", runCtx.Err())
	}
	if got := owner.boundConsumerSnapshot(); len(got) != 0 {
		t.Fatalf("terminal readiness observation retained: %v", got)
	}
}

func TestLifecycleOwnerFailedCleanupRetainsExactHandlesForLaterStop(t *testing.T) {
	consumer := &graphIngestLifecycleConsumeContext{
		closed: make(chan struct{}), drainSeen: make(chan struct{}),
	}
	runCtx, runCancel := context.WithCancel(t.Context())
	owner := withTestRegistry(t, &Component{
		logger:         slog.Default(),
		lifecycleUsed:  true,
		cleanupPending: true,
		cancel:         runCancel,
		consumers: []graphIngestConsumerBinding{{
			handle: consumer,
		}},
	})

	expired, expire := context.WithCancel(t.Context())
	expire()
	if err := owner.cleanup(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired cleanup error = %v, want context.Canceled", err)
	}
	<-consumer.drainSeen
	if consumer.drains.Load() != 1 || len(owner.consumers) != 1 || !owner.consumers[0].drainIssued {
		t.Fatalf("failed cleanup lost exact handle: drains=%d consumers=%d", consumer.drains.Load(), len(owner.consumers))
	}
	if owner.cancel == nil {
		t.Fatal("failed cleanup discarded runtime cancellation authority")
	}
	if !errors.Is(runCtx.Err(), context.Canceled) {
		t.Fatalf("failed cleanup runtime context = %v, want canceled", runCtx.Err())
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start while cleanup pending error = %v, want ErrAlreadyStarted", err)
	}

	close(consumer.closed)
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("later Stop: %v", err)
	}
	if consumer.drains.Load() != 1 {
		t.Fatalf("native Drain replayed: calls=%d", consumer.drains.Load())
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated terminal Stop: %v", err)
	}
}

func TestLifecycleOwnerRunningDeadlineIsTerminalWithoutReplay(t *testing.T) {
	consumer := &graphIngestLifecycleConsumeContext{
		closed: make(chan struct{}), drainSeen: make(chan struct{}),
	}
	owner := withTestRegistry(t, &Component{
		logger:        slog.Default(),
		lifecycleUsed: true,
		running:       true,
		cancel:        func() {},
		consumers: []graphIngestConsumerBinding{{
			handle: consumer,
		}},
	})
	stopCtx, expire := context.WithCancel(t.Context())
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(stopCtx) }()
	<-consumer.drainSeen
	expire()
	if err := <-stopResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop error = %v, want context.Canceled", err)
	}
	if !owner.terminal || len(owner.consumers) != 0 {
		t.Fatalf("deadline Stop did not terminalize: terminal=%v consumers=%d", owner.terminal, len(owner.consumers))
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
	if consumer.drains.Load() != 1 {
		t.Fatalf("native Drain replayed: calls=%d", consumer.drains.Load())
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("restart after terminal deadline error = %v, want ErrAlreadyStarted", err)
	}
	close(consumer.closed)
}

func TestLifecycleOwnerStopBeforeStartIsTerminal(t *testing.T) {
	owner := withTestRegistry(t, &Component{})
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.Start(canceled); err == nil {
		t.Fatal("pre-canceled Start succeeded")
	}
	if owner.lifecycleUsed {
		t.Fatal("pre-canceled Start consumed lifecycle authority")
	}
	if err := owner.Stop(nil); err == nil {
		t.Fatal("Stop(nil) succeeded")
	}
	if owner.lifecycleUsed {
		t.Fatal("Stop(nil) consumed lifecycle authority")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop error = %v, want ErrAlreadyStarted", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated completed Stop: %v", err)
	}
}
