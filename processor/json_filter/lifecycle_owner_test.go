package jsonfilter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type lifecycleTestConsumeContext struct {
	closed       chan struct{}
	closedCalls  chan struct{}
	drainOnce    sync.Once
	closeOnce    sync.Once
	closeOnDrain bool
	drains       atomic.Int32
}

func (f *lifecycleTestConsumeContext) Stop() { panic("unexpected force Stop") }
func (f *lifecycleTestConsumeContext) Drain() {
	f.drainOnce.Do(func() {
		f.drains.Add(1)
		if f.closeOnDrain {
			f.closeOnce.Do(func() { close(f.closed) })
		}
	})
}
func (f *lifecycleTestConsumeContext) Closed() <-chan struct{} {
	f.closedCalls <- struct{}{}
	return f.closed
}

type lifecycleObservedContext struct {
	context.Context
	doneOnce sync.Once
	doneSeen chan struct{}
}

func (c *lifecycleObservedContext) Done() <-chan struct{} {
	c.doneOnce.Do(func() { close(c.doneSeen) })
	return c.Context.Done()
}

func lifecycleJetStreamPorts(t *testing.T, count int) []component.Port {
	t.Helper()
	ports := make([]component.Port, 0, count)
	for index := range count {
		definition := component.PortDefinition{
			Name: fmt.Sprintf("input_%d", index),
			Config: component.JetStreamPort{
				StreamName: fmt.Sprintf("S1_FILTER_%d", index),
				Subjects:   []string{fmt.Sprintf("s1.filter.%d", index)},
			},
		}
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			t.Fatalf("Resolve input %d: %v", index, err)
		}
		ports = append(ports, port)
	}
	return ports
}

func TestLifecycleOwnerProductionStartStopOverlapRollsBackExactHandle(t *testing.T) {
	first := &lifecycleTestConsumeContext{
		closed: make(chan struct{}), closedCalls: make(chan struct{}, 1), closeOnDrain: true,
	}
	secondEntered := make(chan struct{})
	releaseSecond := make(chan struct{})
	sentinel := errors.New("second acquisition failed")
	var calls atomic.Int32
	owner := &Processor{
		name: "json-filter-processor", logger: slog.Default(), natsClient: &natsclient.Client{},
		inputPorts:         lifecycleJetStreamPorts(t, 2),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(
			_ context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig,
			_ func(context.Context, jetstream.Msg),
		) (jetstream.ConsumeContext, error) {
			if calls.Add(1) == 1 {
				return first, nil
			}
			close(secondEntered)
			<-releaseSecond
			return nil, sentinel
		},
	}
	startResult := make(chan error, 1)
	go func() { startResult <- owner.Start(t.Context()) }()
	<-secondEntered
	owner.lifecycleMu.Lock()
	if len(owner.consumers) != 1 || owner.consumers[0].handle != first {
		owner.lifecycleMu.Unlock()
		t.Fatal("first exact handle was not published before second acquisition")
	}
	owner.lifecycleMu.Unlock()

	stopCtx := &lifecycleObservedContext{Context: t.Context(), doneSeen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(stopCtx) }()
	<-stopCtx.doneSeen
	owner.lifecycleMu.Lock()
	owner.lifecycleMu.Unlock()
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before Start completed rollback: %v", err)
	default:
	}
	close(releaseSecond)
	if err := <-startResult; !errors.Is(err, sentinel) {
		t.Fatalf("Start error = %v, want sentinel", err)
	}
	if err := <-stopResult; err != nil {
		t.Fatalf("overlapping Stop: %v", err)
	}
	if first.drains.Load() != 1 {
		t.Fatalf("first native Drain calls = %d, want 1", first.drains.Load())
	}
}

func TestLifecycleOwnerProductionRunningStopKeepsCallbackAuthorityUntilClosed(t *testing.T) {
	handle := &lifecycleTestConsumeContext{
		closed: make(chan struct{}), closedCalls: make(chan struct{}, 1),
	}
	var callbackCtx context.Context
	owner := &Processor{
		name: "json-filter-processor", logger: slog.Default(), natsClient: &natsclient.Client{},
		inputPorts:         lifecycleJetStreamPorts(t, 1),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(
			ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig,
			_ func(context.Context, jetstream.Msg),
		) (jetstream.ConsumeContext, error) {
			callbackCtx = ctx
			return handle, nil
		},
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(t.Context()) }()
	<-handle.closedCalls
	if err := callbackCtx.Err(); err != nil {
		t.Fatalf("callback authority canceled before native Closed: %v", err)
	}
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before native Closed: %v", err)
	default:
	}
	close(handle.closed)
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("callback authority after Closed = %v, want context.Canceled", callbackCtx.Err())
	}
}

func TestLifecycleOwnerStopBeforeStartIsTerminal(t *testing.T) {
	owner := &Processor{}
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
	if err := owner.Stop(nil); err == nil {
		t.Fatal("terminal Stop(nil) succeeded")
	}
}

func TestLifecycleOwnerFailedCleanupRetainsExactHandleForStopRetry(t *testing.T) {
	handle := &lifecycleTestConsumeContext{
		closed: make(chan struct{}), closedCalls: make(chan struct{}, 2),
	}
	runtimeCanceled := make(chan struct{})
	var cancelOnce sync.Once
	owner := &Processor{
		lifecycleUsed:  true,
		cleanupPending: true,
		cancel:         func() { cancelOnce.Do(func() { close(runtimeCanceled) }) },
		consumers:      []streamConsumerBinding{{handle: handle}},
	}
	expired, expire := context.WithCancel(t.Context())
	expire()
	if err := owner.cleanupFailedStart(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired failed-Start cleanup error = %v, want context.Canceled", err)
	}
	<-handle.closedCalls
	<-runtimeCanceled
	if handle.drains.Load() != 1 || len(owner.consumers) != 1 || !owner.consumers[0].drainIssued {
		t.Fatalf("failed cleanup lost exact handle: drains=%d consumers=%d", handle.drains.Load(), len(owner.consumers))
	}
	if owner.cancel == nil {
		t.Fatal("failed cleanup discarded runtime cancellation authority")
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start while cleanup pending error = %v, want ErrAlreadyStarted", err)
	}

	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(t.Context()) }()
	<-handle.closedCalls
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before exact handle closed: %v", err)
	default:
	}
	close(handle.closed)
	if err := <-stopResult; err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
	if handle.drains.Load() != 1 {
		t.Fatalf("native Drain calls = %d, want 1", handle.drains.Load())
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated terminal Stop: %v", err)
	}
}

func TestLifecycleOwnerRunningDeadlineIsTerminalWithoutReplay(t *testing.T) {
	handle := &lifecycleTestConsumeContext{
		closed: make(chan struct{}), closedCalls: make(chan struct{}, 1),
	}
	runtimeCanceled := make(chan struct{})
	var cancelOnce sync.Once
	owner := &Processor{
		lifecycleUsed: true,
		running:       true,
		cancel:        func() { cancelOnce.Do(func() { close(runtimeCanceled) }) },
		consumers:     []streamConsumerBinding{{handle: handle}},
	}
	stopCtx, expire := context.WithCancel(t.Context())
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(stopCtx) }()
	<-handle.closedCalls
	expire()
	if err := <-stopResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop error = %v, want context.Canceled", err)
	}
	<-runtimeCanceled
	if !owner.terminal || len(owner.consumers) != 0 {
		t.Fatalf("deadline Stop did not terminalize: terminal=%v consumers=%d", owner.terminal, len(owner.consumers))
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
	if handle.drains.Load() != 1 {
		t.Fatalf("native Drain replayed: calls=%d", handle.drains.Load())
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("restart after terminal deadline error = %v, want ErrAlreadyStarted", err)
	}
	close(handle.closed)
}
