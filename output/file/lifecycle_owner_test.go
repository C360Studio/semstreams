package file

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type fileCausalCoreSubscription struct {
	drainCalled chan struct{}
	release     chan struct{}
	drains      atomic.Int32
}

func (s *fileCausalCoreSubscription) Drain(ctx context.Context) error {
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

type fileCausalConsumeContext struct {
	closed      chan struct{}
	drainCalled chan struct{}
	drains      atomic.Int32
}

func (*fileCausalConsumeContext) Stop() { panic("unexpected force Stop") }
func (h *fileCausalConsumeContext) Drain() {
	if h.drains.Add(1) == 1 && h.drainCalled != nil {
		close(h.drainCalled)
	}
}
func (h *fileCausalConsumeContext) Closed() <-chan struct{} { return h.closed }

func fileLifecyclePorts(t *testing.T, count int) []component.Port {
	t.Helper()
	ports := make([]component.Port, 0, count)
	for index := range count {
		port, err := (component.PortDefinition{
			Name: fmt.Sprintf("input_%d", index),
			Config: component.JetStreamPort{
				StreamName: fmt.Sprintf("O1_FILE_%d", index),
				Subjects:   []string{fmt.Sprintf("o1.file.%d", index)},
			},
		}).Resolve(component.DirectionInput)
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, port)
	}
	return ports
}

func fileCorePort(t *testing.T) component.Port {
	t.Helper()
	port, err := (component.PortDefinition{
		Name: "core", Config: component.NATSPort{Subject: "o1.file.core"},
	}).Resolve(component.DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestLifecycleOwnerCoreDrainKeepsCallbackAuthorityLive(t *testing.T) {
	directory := t.TempDir()
	core := &fileCausalCoreSubscription{drainCalled: make(chan struct{}), release: make(chan struct{})}
	var callbackCtx context.Context
	owner := &Output{
		name: "file-output", directory: directory, filePath: directory + "/events.jsonl",
		format: "jsonl", append: true, bufferSize: 100, buffer: make([][]byte, 0, 100),
		logger: slog.Default(), natsClient: &natsclient.Client{}, inputPorts: []component.Port{fileCorePort(t)},
		subscribeCore: func(ctx context.Context, _ string, _ func(context.Context, *nats.Msg)) (coreSubscription, error) {
			callbackCtx = ctx
			return core, nil
		},
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
	close(core.release)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("core callback context = %v, want canceled", callbackCtx.Err())
	}
}

func TestLifecycleOwnerRunningStopDrainsCallbacksBeforeCancelFlushAndClose(t *testing.T) {
	directory := t.TempDir()
	handle := &fileCausalConsumeContext{closed: make(chan struct{}), drainCalled: make(chan struct{})}
	var callbackCtx context.Context
	owner := &Output{
		name: "file-output", directory: directory, filePath: directory + "/events.jsonl",
		format: "jsonl", append: true, bufferSize: 100, buffer: make([][]byte, 0, 100),
		logger: slog.Default(), natsClient: &natsclient.Client{}, inputPorts: fileLifecyclePorts(t, 1),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			callbackCtx = ctx
			return handle, nil
		},
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner.handleMessage(callbackCtx, []byte(`{"sequence":1}`))
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	<-handle.drainCalled
	if callbackCtx.Err() != nil {
		t.Fatal("callback authority canceled before native Closed")
	}
	if _, err := owner.file.Write([]byte{}); err != nil {
		t.Fatalf("file closed before native Closed: %v", err)
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("callback context = %v, want canceled", callbackCtx.Err())
	}
	contents, err := os.ReadFile(owner.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `{"sequence":1}`) {
		t.Fatalf("final flush missing message: %q", contents)
	}
	if owner.file != nil {
		t.Fatal("file handle retained after terminal close")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop replayed teardown: %v", err)
	}
}

func TestLifecycleOwnerFailedStartRetainsExactHandleAndFileForStopRetry(t *testing.T) {
	directory := t.TempDir()
	handle := &fileCausalConsumeContext{closed: make(chan struct{})}
	acquireErr := errors.New("second acquisition failed")
	cleanupErr := errors.New("bounded cleanup failed")
	secondEntered := make(chan struct{})
	releaseSecond := make(chan struct{})
	retryWait := make(chan struct{})
	var consumeCalls, waitCalls atomic.Int32
	owner := &Output{
		name: "file-output", directory: directory, filePath: directory + "/events.jsonl",
		format: "jsonl", append: true, bufferSize: 100, buffer: make([][]byte, 0, 100),
		logger: slog.Default(), natsClient: &natsclient.Client{}, inputPorts: fileLifecyclePorts(t, 2),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			if consumeCalls.Add(1) == 1 {
				return handle, nil
			}
			close(secondEntered)
			<-releaseSecond
			return nil, acquireErr
		},
		waitConsumerClosed: func(ctx context.Context, closed <-chan struct{}) error {
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
		},
	}
	startDone := make(chan error, 1)
	go func() { startDone <- owner.Start(t.Context()) }()
	<-secondEntered
	close(releaseSecond)
	startErr := <-startDone
	if !errors.Is(startErr, acquireErr) || !errors.Is(startErr, cleanupErr) {
		t.Fatalf("Start = %v, want acquisition and cleanup errors", startErr)
	}
	if !owner.cleanupPending || len(owner.consumers) != 1 || owner.file == nil {
		t.Fatal("failed Start discarded exact cleanup authority")
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start while cleanup pending = %v", err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	<-retryWait
	if handle.drains.Load() != 1 {
		t.Fatalf("native Drain replayed: %d", handle.drains.Load())
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if owner.file != nil || !owner.terminal {
		t.Fatal("retry Stop did not finish terminal file cleanup")
	}
}

func TestLifecycleOwnerRunningDeadlineIsTerminalWithoutReplay(t *testing.T) {
	directory := t.TempDir()
	handle := &fileCausalConsumeContext{closed: make(chan struct{}), drainCalled: make(chan struct{})}
	owner := &Output{
		name: "file-output", directory: directory, filePath: directory + "/events.jsonl",
		format: "jsonl", append: true, bufferSize: 100, buffer: make([][]byte, 0, 100),
		logger: slog.Default(), natsClient: &natsclient.Client{}, inputPorts: fileLifecyclePorts(t, 1),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			return handle, nil
		},
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(handle.closed)
		if owner.file != nil {
			_ = owner.file.Close()
		}
	})
	stopCtx, cancel := context.WithCancel(t.Context())
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(stopCtx) }()
	<-handle.drainCalled
	cancel()
	if err := <-stopDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop = %v", err)
	}
	if !owner.terminal || len(owner.consumers) != 0 {
		t.Fatal("running deadline did not terminalize and discard replay authority")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if handle.drains.Load() != 1 {
		t.Fatalf("native Drain replayed: %d", handle.drains.Load())
	}
}

func TestLifecycleOwnerStopBeforeStartIsTerminalAndNilIsImmutable(t *testing.T) {
	owner := &Output{logger: slog.Default()}
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
