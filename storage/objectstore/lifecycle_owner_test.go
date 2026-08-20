package objectstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type objectStoreCausalCoreSubscription struct {
	drainCalled chan struct{}
	release     chan struct{}
	drains      atomic.Int32
}

func (s *objectStoreCausalCoreSubscription) Drain(ctx context.Context) error {
	if s.drains.Add(1) == 1 {
		close(s.drainCalled)
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type objectStoreCausalConsumeContext struct {
	closed      chan struct{}
	drainCalled chan struct{}
	drains      atomic.Int32
}

func (*objectStoreCausalConsumeContext) Stop() { panic("unexpected force Stop") }
func (h *objectStoreCausalConsumeContext) Drain() {
	if h.drains.Add(1) == 1 && h.drainCalled != nil {
		close(h.drainCalled)
	}
}
func (h *objectStoreCausalConsumeContext) Closed() <-chan struct{} { return h.closed }

type objectStoreObservedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *objectStoreObservedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func objectStoreLifecycleInput(t *testing.T, index int, kind component.PortKind) (component.Port, objectStoreInputBinding) {
	t.Helper()
	name := fmt.Sprintf("input_%d", index)
	subject := fmt.Sprintf("os1.write.%d", index)
	var config component.Portable
	binding := objectStoreInputBinding{portName: name, kind: kind, subject: subject}
	switch kind {
	case component.PortKindNATS:
		config = component.NATSPort{Subject: subject}
	case component.PortKindJetStream:
		binding.streamName = fmt.Sprintf("OS1_WRITE_%d", index)
		binding.consumerName = fmt.Sprintf("objectstore-unit-%d", index)
		config = component.JetStreamPort{StreamName: binding.streamName, Subjects: []string{subject}}
	default:
		t.Fatalf("unsupported test port kind %q", kind)
	}
	port, err := (component.PortDefinition{Name: name, Config: config}).Resolve(component.DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	return port, binding
}

func newObjectStoreLifecycleOwner() *Component {
	return &Component{
		instanceName: "unit",
		config:       DefaultConfig(),
		decoder:      message.NewDecoder(nil),
		logger:       slog.Default(),
		natsClient:   &natsclient.Client{},
		portsByName:  make(map[string]component.Port),
	}
}

func TestLifecycleOwnerStopWaitsForStartFinalization(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	owner := newObjectStoreLifecycleOwner()
	owner.newStore = func(context.Context) (*Store, error) {
		close(entered)
		<-release
		return &Store{instanceName: "unit"}, nil
	}
	owner.closeStore = func(*Store) error {
		close(closed)
		return nil
	}

	startDone := make(chan error, 1)
	go func() { startDone <- owner.Start(t.Context()) }()
	<-entered
	stopCtx := &objectStoreObservedDoneContext{Context: t.Context(), observed: make(chan struct{})}
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(stopCtx) }()
	<-stopCtx.observed
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before Start finalized: %v", err)
	default:
	}
	close(release)
	if err := <-startDone; err != nil {
		t.Fatal(err)
	}
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	<-closed
}

func TestLifecycleOwnerCoreDrainKeepsStoreAndCallbackAuthorityLive(t *testing.T) {
	port, binding := objectStoreLifecycleInput(t, 0, component.PortKindNATS)
	core := &objectStoreCausalCoreSubscription{drainCalled: make(chan struct{}), release: make(chan struct{})}
	store := &Store{instanceName: "unit"}
	var callbackCtx context.Context
	owner := newObjectStoreLifecycleOwner()
	owner.inputBindings = []objectStoreInputBinding{binding}
	owner.portsByName[port.Name] = port
	owner.newStore = func(context.Context) (*Store, error) { return store, nil }
	owner.subscribeCore = func(ctx context.Context, _ string, _ func(context.Context, *nats.Msg)) (objectStoreCoreSubscription, error) {
		callbackCtx = ctx
		return core, nil
	}
	owner.closeStore = func(got *Store) error {
		if got != store {
			t.Errorf("closed store %p, want %p", got, store)
		}
		if callbackCtx.Err() == nil {
			t.Error("Store closed before callback authority was canceled")
		}
		return nil
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	<-core.drainCalled
	if callbackCtx.Err() != nil {
		t.Fatal("core callback authority canceled before Drain returned")
	}
	if owner.ProvidedStores()["unit"] != store {
		t.Fatal("StoreProvider stopped exposing the live Store before callback drain")
	}
	close(core.release)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("callback context = %v, want canceled", callbackCtx.Err())
	}
	if owner.ProvidedStores() != nil {
		t.Fatal("terminal Stop retained StoreProvider authority")
	}
}

func TestLifecycleOwnerJetStreamDrainKeepsStoreAndCallbackAuthorityLive(t *testing.T) {
	port, binding := objectStoreLifecycleInput(t, 0, component.PortKindJetStream)
	handle := &objectStoreCausalConsumeContext{closed: make(chan struct{}), drainCalled: make(chan struct{})}
	store := &Store{instanceName: "unit"}
	var callbackCtx context.Context
	owner := newObjectStoreLifecycleOwner()
	owner.inputBindings = []objectStoreInputBinding{binding}
	owner.portsByName[port.Name] = port
	owner.newStore = func(context.Context) (*Store, error) { return store, nil }
	owner.waitForStreamInput = func(context.Context, string) error { return nil }
	owner.consumeStream = func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		callbackCtx = ctx
		return handle, nil
	}
	owner.closeStore = func(*Store) error {
		if callbackCtx.Err() == nil {
			t.Error("Store closed before callback authority was canceled")
		}
		return nil
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	<-handle.drainCalled
	if callbackCtx.Err() != nil {
		t.Fatal("JetStream callback authority canceled before native Closed")
	}
	if owner.ProvidedStores()["unit"] != store {
		t.Fatal("Store closed before native Closed")
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("callback context = %v, want canceled", callbackCtx.Err())
	}
}

func TestLifecycleOwnerFailedStartRetainsExactHandlesAndStoreForStopRetry(t *testing.T) {
	firstPort, firstBinding := objectStoreLifecycleInput(t, 0, component.PortKindJetStream)
	secondPort, secondBinding := objectStoreLifecycleInput(t, 1, component.PortKindJetStream)
	handle := &objectStoreCausalConsumeContext{closed: make(chan struct{})}
	store := &Store{instanceName: "unit"}
	acquireErr := errors.New("second acquisition failed")
	cleanupErr := errors.New("bounded cleanup failed")
	var consumeCalls, waitCalls, closeCalls atomic.Int32
	retryWait := make(chan struct{})
	owner := newObjectStoreLifecycleOwner()
	owner.inputBindings = []objectStoreInputBinding{firstBinding, secondBinding}
	owner.portsByName[firstPort.Name] = firstPort
	owner.portsByName[secondPort.Name] = secondPort
	owner.newStore = func(context.Context) (*Store, error) { return store, nil }
	owner.waitForStreamInput = func(context.Context, string) error { return nil }
	owner.consumeStream = func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		if consumeCalls.Add(1) == 1 {
			return handle, nil
		}
		return nil, acquireErr
	}
	owner.waitConsumerClosed = func(ctx context.Context, closed <-chan struct{}) error {
		if waitCalls.Add(1) == 1 {
			return cleanupErr
		}
		close(retryWait)
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	owner.closeStore = func(*Store) error {
		closeCalls.Add(1)
		return nil
	}

	startErr := owner.Start(t.Context())
	if !errors.Is(startErr, acquireErr) || !errors.Is(startErr, cleanupErr) {
		t.Fatalf("Start = %v, want acquisition and cleanup errors", startErr)
	}
	if !owner.cleanupPending || len(owner.writeConsumers) != 1 || owner.store != store {
		t.Fatal("failed Start discarded exact cleanup authority")
	}
	if got := closeCalls.Load(); got != 0 {
		t.Fatalf("failed rollback closed Store before exact native Closed: %d", got)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start while cleanup pending = %v", err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	<-retryWait
	if got := handle.drains.Load(); got != 1 {
		t.Fatalf("native Drain replayed: %d", got)
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("successful cleanup retry Store closes = %d, want 1", got)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("repeated Stop replayed Store close: %d", got)
	}
}

func TestLifecycleOwnerRunningDeadlineIsTerminalWithoutReplay(t *testing.T) {
	port, binding := objectStoreLifecycleInput(t, 0, component.PortKindJetStream)
	handle := &objectStoreCausalConsumeContext{closed: make(chan struct{}), drainCalled: make(chan struct{})}
	var closeCalls atomic.Int32
	owner := newObjectStoreLifecycleOwner()
	owner.inputBindings = []objectStoreInputBinding{binding}
	owner.portsByName[port.Name] = port
	owner.newStore = func(context.Context) (*Store, error) { return &Store{instanceName: "unit"}, nil }
	owner.waitForStreamInput = func(context.Context, string) error { return nil }
	owner.consumeStream = func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		return handle, nil
	}
	owner.closeStore = func(*Store) error { closeCalls.Add(1); return nil }
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	stopCtx, cancel := context.WithCancel(t.Context())
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(stopCtx) }()
	<-handle.drainCalled
	cancel()
	if err := <-stopDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop = %v", err)
	}
	if !owner.terminal || len(owner.writeConsumers) != 0 || owner.store != nil {
		t.Fatal("running deadline did not terminalize and discard replay authority")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := handle.drains.Load(); got != 1 {
		t.Fatalf("native Drain replayed: %d", got)
	}
	if got := closeCalls.Load(); got != 0 {
		t.Fatalf("Store closed without exact native callback completion: %d", got)
	}
}

func TestLifecycleOwnerStopBeforeStartIsTerminalAndNilIsImmutable(t *testing.T) {
	owner := newObjectStoreLifecycleOwner()
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.Start(nil); err == nil || owner.lifecycleUsed {
		t.Fatal("Start(nil) changed lifecycle authority")
	}
	if err := owner.Start(ended); err == nil || owner.lifecycleUsed {
		t.Fatal("pre-canceled Start changed lifecycle authority")
	}
	if err := owner.Stop(nil); err == nil || owner.lifecycleUsed {
		t.Fatal("Stop(nil) changed lifecycle authority")
	}
	if err := owner.Stop(ended); !errors.Is(err, context.Canceled) || owner.lifecycleUsed {
		t.Fatal("pre-canceled Stop changed lifecycle authority")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop = %v", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}
