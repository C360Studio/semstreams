package httppost

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type httpPostCausalCoreSubscription struct {
	drainCalled chan struct{}
	release     chan struct{}
	drains      atomic.Int32
}

func (s *httpPostCausalCoreSubscription) Drain(ctx context.Context) error {
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

type httpPostCausalConsumeContext struct {
	closed      chan struct{}
	drainCalled chan struct{}
	drains      atomic.Int32
	events      chan<- string
}

func (*httpPostCausalConsumeContext) Stop() { panic("unexpected force Stop") }
func (h *httpPostCausalConsumeContext) Drain() {
	if h.drains.Add(1) == 1 && h.drainCalled != nil {
		close(h.drainCalled)
	}
	if h.events != nil {
		h.events <- "consumer-drain"
	}
}
func (h *httpPostCausalConsumeContext) Closed() <-chan struct{} {
	if h.events != nil {
		h.events <- "consumer-closed-wait"
	}
	return h.closed
}

type httpPostRecordingTransport struct {
	events chan<- string
	closes atomic.Int32
}

func (*httpPostRecordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected HTTP request")
}

func (t *httpPostRecordingTransport) CloseIdleConnections() {
	t.closes.Add(1)
	t.events <- "idle-close"
}

func httpPostLifecyclePorts(t *testing.T, count int) []component.Port {
	t.Helper()
	ports := make([]component.Port, 0, count)
	for index := range count {
		port, err := (component.PortDefinition{
			Name: fmt.Sprintf("input_%d", index),
			Config: component.JetStreamPort{
				StreamName: fmt.Sprintf("O1_HTTPPOST_%d", index),
				Subjects:   []string{fmt.Sprintf("o1.httppost.%d", index)},
			},
		}).Resolve(component.DirectionInput)
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, port)
	}
	return ports
}

func httpPostCorePort(t *testing.T) component.Port {
	t.Helper()
	port, err := (component.PortDefinition{
		Name: "core", Config: component.NATSPort{Subject: "o1.httppost.core"},
	}).Resolve(component.DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestLifecycleOwnerCoreDrainKeepsCallbackAuthorityLive(t *testing.T) {
	core := &httpPostCausalCoreSubscription{drainCalled: make(chan struct{}), release: make(chan struct{})}
	var callbackCtx context.Context
	owner := &Output{
		name: "httppost-output", url: "http://example.invalid", contentType: "application/json",
		logger: slog.Default(), httpClient: &http.Client{}, natsClient: &natsclient.Client{},
		inputPorts: []component.Port{httpPostCorePort(t)},
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

func TestLifecycleOwnerACMEAuthorityBeginsAtStartAndJoinsAfterCancel(t *testing.T) {
	cfg := Config{
		Ports: &component.PortConfig{Inputs: []component.PortDefinition{{
			Name: "input", Config: component.NATSPort{Subject: "o1.httppost"}, Required: true,
		}}},
		URL: "https://example.invalid", Timeout: 1, ContentType: "application/json",
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	securityCfg := security.Config{}
	securityCfg.TLS.Client.Mode = "acme"
	securityCfg.TLS.Client.ACME.Enabled = true
	discoverable, err := NewOutput(raw, component.Dependencies{Security: securityCfg})
	if err != nil {
		t.Fatalf("constructor acquired ACME authority: %v", err)
	}
	owner := discoverable.(*Output)
	owner.natsClient = &natsclient.Client{}
	owner.inputPorts = nil
	loaded := make(chan context.Context, 1)
	joined := make(chan struct{})
	owner.loadClientTLSConfigWithACME = func(ctx context.Context, _ security.ClientTLSConfig) (*tls.Config, func(), error) {
		loaded <- ctx
		return &tls.Config{MinVersion: tls.VersionTLS12}, func() {
			if ctx.Err() == nil {
				t.Error("ACME cleanup ran before Start-derived context cancellation")
			}
			close(joined)
		}, nil
	}
	parent := t.Context()
	if err := owner.Start(parent); err != nil {
		t.Fatal(err)
	}
	runCtx := <-loaded
	if runCtx == parent {
		t.Fatal("ACME continuing work received the caller context without owner cancellation")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-joined
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop replayed ACME cleanup: %v", err)
	}
}

func TestLifecycleOwnerClosesHTTPIdleConnectionsAfterCallbacksAndACMEJoinOnce(t *testing.T) {
	events := make(chan string, 8)
	handle := &httpPostCausalConsumeContext{closed: make(chan struct{}), events: events}
	transport := &httpPostRecordingTransport{events: events}
	var runCtx context.Context
	owner := &Output{
		name: "httppost-output", url: "https://example.invalid", contentType: "application/json",
		logger: slog.Default(), httpClient: &http.Client{}, natsClient: &natsclient.Client{},
		inputPorts: httpPostLifecyclePorts(t, 1),
		security: security.Config{TLS: security.TLSConfig{Client: security.ClientTLSConfig{
			Mode: "acme", ACME: security.ACMEConfig{Enabled: true},
		}}},
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			runCtx = ctx
			return handle, nil
		},
		loadClientTLSConfigWithACME: func(context.Context, security.ClientTLSConfig) (*tls.Config, func(), error) {
			return &tls.Config{MinVersion: tls.VersionTLS12}, func() {
				if runCtx.Err() == nil {
					t.Error("ACME join ran before runtime cancellation")
				}
				events <- "acme-join"
			}, nil
		},
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner.httpClient.Transport = transport
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	if got := <-events; got != "consumer-drain" {
		t.Fatalf("first event = %q", got)
	}
	if got := <-events; got != "consumer-closed-wait" {
		t.Fatalf("second event = %q", got)
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	var terminalEvents []string
	for len(events) > 0 {
		terminalEvents = append(terminalEvents, <-events)
	}
	if got := strings.Join(terminalEvents, ","); got != "acme-join,idle-close" {
		t.Fatalf("terminal cleanup order = %q, want acme-join,idle-close", got)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("idle connection closes after repeated Stop = %d, want 1", got)
	}
}

func TestLifecycleOwnerRunningStopKeepsCallbackAuthorityUntilClosed(t *testing.T) {
	handle := &httpPostCausalConsumeContext{closed: make(chan struct{}), drainCalled: make(chan struct{})}
	var callbackCtx context.Context
	owner := &Output{
		name: "httppost-output", url: "http://example.invalid", contentType: "application/json",
		logger: slog.Default(), httpClient: &http.Client{}, natsClient: &natsclient.Client{},
		inputPorts:         httpPostLifecyclePorts(t, 1),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			callbackCtx = ctx
			return handle, nil
		},
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- owner.Stop(t.Context()) }()
	<-handle.drainCalled
	if callbackCtx.Err() != nil {
		t.Fatal("callback authority canceled before native Closed")
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("callback context = %v, want canceled", callbackCtx.Err())
	}
}

func TestLifecycleOwnerFailedStartCleanupPendingRetriesWithoutDrainReplay(t *testing.T) {
	handle := &httpPostCausalConsumeContext{closed: make(chan struct{})}
	transport := &httpPostRecordingTransport{events: make(chan string, 1)}
	acquireErr := errors.New("second acquisition failed")
	cleanupErr := errors.New("bounded cleanup failed")
	var consumeCalls, waitCalls atomic.Int32
	retryWait := make(chan struct{})
	owner := &Output{
		name: "httppost-output", url: "http://example.invalid", contentType: "application/json",
		logger: slog.Default(), httpClient: &http.Client{Transport: transport}, natsClient: &natsclient.Client{},
		inputPorts:         httpPostLifecyclePorts(t, 2),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			if consumeCalls.Add(1) == 1 {
				return handle, nil
			}
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
	startErr := owner.Start(t.Context())
	if !errors.Is(startErr, acquireErr) || !errors.Is(startErr, cleanupErr) {
		t.Fatalf("Start = %v, want acquisition and cleanup errors", startErr)
	}
	if !owner.cleanupPending || len(owner.consumers) != 1 {
		t.Fatal("failed Start discarded exact cleanup authority")
	}
	if got := transport.closes.Load(); got != 0 {
		t.Fatalf("failed rollback closed idle connections before callbacks closed: %d", got)
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
	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("successful failed-Start retry idle closes = %d, want 1", got)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("repeated terminal Stop replayed idle close: %d", got)
	}
}

func TestLifecycleOwnerRunningDeadlineIsTerminalWithoutReplay(t *testing.T) {
	handle := &httpPostCausalConsumeContext{closed: make(chan struct{}), drainCalled: make(chan struct{})}
	owner := &Output{
		name: "httppost-output", url: "http://example.invalid", contentType: "application/json",
		logger: slog.Default(), httpClient: &http.Client{}, natsClient: &natsclient.Client{},
		inputPorts:         httpPostLifecyclePorts(t, 1),
		waitForStreamInput: func(context.Context, string) error { return nil },
		consumeStream: func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
			return handle, nil
		},
	}
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer close(handle.closed)
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
	owner := &Output{}
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
