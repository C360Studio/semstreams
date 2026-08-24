package graphindexspatial

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/require"
)

type lifecycleObservedContext struct {
	context.Context
	observed chan struct{}
	proceed  <-chan struct{}
	once     sync.Once
}

func (c *lifecycleObservedContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.observed)
		if c.proceed != nil {
			<-c.proceed
		}
	})
	return c.Context.Done()
}

func newLifecycleNATSClient(t *testing.T) *natsclient.Client {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	require.NoError(t, err)
	go server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)
	client, err := natsclient.NewClient(server.ClientURL())
	require.NoError(t, err)
	require.NoError(t, client.Connect(t.Context()))
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func lifecycleLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLifecycleOwnerNoActionStopIsTerminal(t *testing.T) {
	client, err := natsclient.NewClient("nats://127.0.0.1:1")
	require.NoError(t, err)
	c := &Component{config: DefaultConfig(), initialized: true, logger: lifecycleLogger(), natsClient: client}
	require.NoError(t, c.Stop(t.Context()))
	require.ErrorContains(t, c.Start(t.Context()), "already used")
	require.NoError(t, c.Stop(t.Context()))
}

func TestLifecycleOwnerNilContextsPreserveAuthority(t *testing.T) {
	runCtx, cancel := context.WithCancel(t.Context())
	startDone := make(chan struct{})
	runtimeDone := make(chan struct{})
	c := &Component{logger: lifecycleLogger(), initialized: true, lifecycleUsed: true, cleanupPending: true, cancel: cancel, startDone: startDone, runtimeDone: runtimeDone}
	require.Error(t, c.Start(nil))
	require.Error(t, c.Stop(nil))
	require.Equal(t, startDone, c.startDone)
	require.Equal(t, runtimeDone, c.runtimeDone)
	require.True(t, c.lifecycleUsed)
	require.True(t, c.cleanupPending)
	require.False(t, c.lifecycleTerminal)
	require.NoError(t, runCtx.Err())
	cancel()

	terminal := &Component{logger: lifecycleLogger(), initialized: true}
	require.NoError(t, terminal.Stop(t.Context()))
	require.Error(t, terminal.Stop(nil))
	require.True(t, terminal.lifecycleUsed)
	require.True(t, terminal.lifecycleTerminal)
	require.False(t, terminal.cleanupPending)
}

func runLifecycleStartRollback(t *testing.T, blockCallback bool) {
	t.Helper()
	client := newLifecycleNATSClient(t)
	config := DefaultConfig()
	config.StartupAttempts = 1
	config.StartupInterval = 1
	c := &Component{config: config, logger: lifecycleLogger(), initialized: true, natsClient: client}
	sentinel := errors.New("later subscription failed")
	healthDone := make(chan struct{})
	release := make(chan struct{})
	failureReady := make(chan struct{})
	allowFailure := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	calls := 0
	var firstSubject string
	c.subscribeForRequests = func(ctx context.Context, subject string, handler func(context.Context, []byte) ([]byte, error)) (*natsclient.Subscription, error) {
		calls++
		if calls == 1 {
			firstSubject = subject
			return client.SubscribeForRequests(ctx, subject, func(callbackCtx context.Context, data []byte) ([]byte, error) {
				_ = c.Health()
				close(healthDone)
				if blockCallback {
					<-release
				}
				return handler(callbackCtx, data)
			})
		}
		require.NoError(t, client.Publish(ctx, firstSubject, nil))
		require.NoError(t, client.GetConnection().Flush())
		<-healthDone
		if !blockCallback {
			close(failureReady)
			<-allowFailure
		}
		return nil, sentinel
	}
	if !blockCallback {
		startResult := make(chan error, 1)
		go func() { startResult <- c.Start(t.Context()) }()
		<-failureReady
		stopProceed := make(chan struct{})
		stopCtx := &lifecycleObservedContext{Context: t.Context(), observed: make(chan struct{}), proceed: stopProceed}
		stopResult := make(chan error, 1)
		go func() { stopResult <- c.Stop(stopCtx) }()
		<-stopCtx.observed
		close(allowFailure)
		startErr := <-startResult
		require.ErrorIs(t, startErr, sentinel)
		require.True(t, c.lifecycleTerminal)
		require.False(t, c.cleanupPending)
		require.Empty(t, c.querySubscriptions)
		close(stopProceed)
		require.NoError(t, <-stopResult)
		return
	}
	startErr := c.Start(t.Context())
	require.ErrorIs(t, startErr, sentinel)
	require.ErrorIs(t, startErr, context.DeadlineExceeded)
	require.True(t, c.cleanupPending)
	require.False(t, c.lifecycleTerminal)
	require.Len(t, c.querySubscriptions, 1)
	require.ErrorContains(t, c.Start(t.Context()), "already used")
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, c.Stop(t.Context()))
	require.True(t, c.lifecycleTerminal)
}

func TestLifecycleOwnerStartRollbackReleasesLocks(t *testing.T) {
	runLifecycleStartRollback(t, false)
}

func TestLifecycleOwnerStartRollbackExpiryRetainsAndRetries(t *testing.T) {
	runLifecycleStartRollback(t, true)
}

func TestLifecycleOwnerDrainOrderAndCompletedRepeat(t *testing.T) {
	client := newLifecycleNATSClient(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseCallback := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseCallback)
	callbackCtx := make(chan context.Context, 1)
	runCtx, cancel := context.WithCancel(t.Context())
	sub, err := client.SubscribeForRequests(runCtx, "test.graph.spatial.lifecycle.order", func(ctx context.Context, _ []byte) ([]byte, error) {
		callbackCtx <- ctx
		close(entered)
		<-release
		return nil, nil
	})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { <-runCtx.Done(); close(done) }()
	c := &Component{logger: lifecycleLogger(), initialized: true, lifecycleUsed: true, running: true, cancel: cancel, runtimeDone: done, querySubscriptions: []*natsclient.Subscription{sub}}
	require.NoError(t, client.Publish(t.Context(), "test.graph.spatial.lifecycle.order", nil))
	require.NoError(t, client.GetConnection().Flush())
	<-entered
	handlerCtx := <-callbackCtx
	stopCtx := &lifecycleObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(stopCtx) }()
	<-stopCtx.observed
	require.NoError(t, handlerCtx.Err())
	releaseCallback()
	require.NoError(t, <-stopResult)
	require.Error(t, handlerCtx.Err())
	require.NoError(t, c.Stop(t.Context()))
}

func TestLifecycleOwnerTerminalDeadlineIsNotReplayed(t *testing.T) {
	client := newLifecycleNATSClient(t)
	runCtx, cancel := context.WithCancel(t.Context())
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseCallback := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseCallback)
	sub, err := client.SubscribeForRequests(runCtx, "test.graph.spatial.lifecycle.terminal", func(context.Context, []byte) ([]byte, error) {
		close(entered)
		<-release
		return nil, nil
	})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { <-runCtx.Done(); close(done) }()
	c := &Component{logger: lifecycleLogger(), initialized: true, lifecycleUsed: true, running: true, cancel: cancel, runtimeDone: done, querySubscriptions: []*natsclient.Subscription{sub}}
	require.NoError(t, client.Publish(t.Context(), "test.graph.spatial.lifecycle.terminal", nil))
	require.NoError(t, client.GetConnection().Flush())
	<-entered
	expired, expire := context.WithCancel(t.Context())
	expire()
	require.ErrorIs(t, c.Stop(expired), context.Canceled)
	require.True(t, c.lifecycleTerminal)
	require.Empty(t, c.querySubscriptions)
	require.NoError(t, c.Stop(t.Context()))
	releaseCallback()
}
