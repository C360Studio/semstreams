package natsclient

import (
	"context"
	"log/slog"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/metric"
	"github.com/nats-io/nats.go/jetstream"
)

type controlledNativeConsumeContext struct {
	closed  chan struct{}
	stopped atomic.Bool
}

func (c *controlledNativeConsumeContext) Stop()                   { c.stopped.Store(true) }
func (c *controlledNativeConsumeContext) Drain()                  {}
func (c *controlledNativeConsumeContext) Closed() <-chan struct{} { return c.closed }

type controlledNativeConsumer struct {
	jetstream.Consumer
	consumeEntered chan struct{}
	consumeRelease chan struct{}
	handle         jetstream.ConsumeContext
	info           *jetstream.ConsumerInfo
}

func (c *controlledNativeConsumer) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	return c.info, nil
}

func (c *controlledNativeConsumer) Consume(
	jetstream.MessageHandler, ...jetstream.PullConsumeOpt,
) (jetstream.ConsumeContext, error) {
	close(c.consumeEntered)
	<-c.consumeRelease
	return c.handle, nil
}

func TestPortConsumerHandleCommitIgnoresCancellationAfterNativeConsumeBegins(t *testing.T) {
	metrics, err := newJetStreamMetrics(metric.NewMetricsRegistry())
	if err != nil {
		t.Fatalf("newJetStreamMetrics: %v", err)
	}
	client := &Client{jsMetrics: metrics, logger: slog.Default()}
	identity := internalConsumerIdentity{stream: "S1_CONTROLLED", durable: "s1-controlled"}
	claim, err := client.reserveInternalConsumer(identity, "ConsumeStreamWithConfigHandle")
	if err != nil {
		t.Fatalf("reserve claim: %v", err)
	}
	handle := &controlledNativeConsumeContext{closed: make(chan struct{})}
	native := &controlledNativeConsumer{
		consumeEntered: make(chan struct{}),
		consumeRelease: make(chan struct{}),
		handle:         handle,
		info: &jetstream.ConsumerInfo{
			Stream: identity.stream,
			Name:   identity.durable,
			Config: jetstream.ConsumerConfig{Durable: identity.durable, MaxAckPending: 17},
		},
	}
	guarded := &guardedConsumer{Consumer: native}
	cfg := StreamConsumerConfig{
		StreamName: identity.stream, ConsumerName: identity.durable, MaxAckPending: 17,
	}
	owner := PortConsumerContext{Component: "s1-controlled", Port: "input"}
	ctx, cancel := context.WithCancel(t.Context())
	type result struct {
		handle jetstream.ConsumeContext
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		returned, startErr := client.startPortConsumerHandle(
			ctx, ctx, "ConsumeStreamWithConfigHandle", owner, cfg, guarded, identity, claim,
			func(context.Context, jetstream.Msg) {},
		)
		resultCh <- result{handle: returned, err: startErr}
	}()
	<-native.consumeEntered
	cancel()
	close(native.consumeRelease)
	got := <-resultCh
	if got.err != nil || got.handle != handle {
		t.Fatalf("post-Consume cancellation result = (%v, %v), want exact handle and nil", got.handle, got.err)
	}
	if handle.stopped.Load() {
		t.Fatal("bridge force-stopped a committed native handle")
	}

	client.internalClaimsMu.Lock()
	activeClaim := client.internalClaims[identity] == claim
	client.internalClaimsMu.Unlock()
	metrics.mu.Lock()
	_, activeConsumer := metrics.consumers[identity.stream+":"+identity.durable]
	activePolicies := len(metrics.policies)
	metrics.mu.Unlock()
	if !activeClaim || !activeConsumer || activePolicies != 1 {
		t.Fatalf("authority released before exact Closed: claim=%v consumer=%v policies=%d",
			activeClaim, activeConsumer, activePolicies)
	}

	close(handle.closed)
	released := make(chan struct{})
	go func() {
		defer close(released)
		for {
			client.internalClaimsMu.Lock()
			_, claimActive := client.internalClaims[identity]
			client.internalClaimsMu.Unlock()
			metrics.mu.Lock()
			consumerCount, policyCount := len(metrics.consumers), len(metrics.policies)
			metrics.mu.Unlock()
			if !claimActive && consumerCount == 0 && policyCount == 0 {
				return
			}
			runtime.Gosched()
		}
	}()
	<-released
}

func TestConsumeStreamWithConfigContextsHandleRejectsInvalidContextsBeforeSetup(t *testing.T) {
	client := &Client{}
	cfg := StreamConsumerConfig{StreamName: "A1", ConsumerName: "a1"}
	owner := PortConsumerContext{Component: "agentic-loop", Port: "agent.task"}
	handler := func(context.Context, jetstream.Msg) {}
	if handle, err := client.ConsumeStreamWithConfigContextsHandle(nil, t.Context(), owner, cfg, handler); err == nil || handle != nil {
		t.Fatalf("nil setup context = (%v, %v), want nil handle/error", handle, err)
	}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if handle, err := client.ConsumeStreamWithConfigContextsHandle(t.Context(), ended, owner, cfg, handler); err == nil || handle != nil {
		t.Fatalf("ended handler context = (%v, %v), want nil handle/error", handle, err)
	}
}
