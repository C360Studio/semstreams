package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/metric"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type controlledMetricsServer struct {
	stopEntered chan struct{}
	stopRelease chan struct{}
	stopErrs    chan error
	stopCalls   atomic.Int32
}

func (*controlledMetricsServer) Start(context.Context) error { return nil }

func (s *controlledMetricsServer) Stop(ctx context.Context) error {
	s.stopCalls.Add(1)
	if s.stopEntered != nil {
		close(s.stopEntered)
	}
	if s.stopRelease != nil {
		select {
		case <-s.stopRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.stopErrs != nil {
		return <-s.stopErrs
	}
	return nil
}

func TestMetricsStopRunsProviderOutsideLockAndRejectsConcurrentStop(t *testing.T) {
	base := NewBaseServiceWithOptions("metrics", nil)
	runCtx, cancel := context.WithCancel(t.Context())
	require.NoError(t, base.Start(runCtx))
	server := &controlledMetricsServer{
		stopEntered: make(chan struct{}),
		stopRelease: make(chan struct{}),
	}
	m := &Metrics{BaseService: base, server: server, used: true, running: true, cancel: cancel}

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.Stop(t.Context()) }()
	<-server.stopEntered
	if !m.lifecycleMu.TryLock() {
		t.Fatal("Metrics.Stop held lifecycle lock during provider shutdown")
	}
	m.lifecycleMu.Unlock()
	concurrentErr := m.Stop(t.Context())
	require.Error(t, concurrentErr)
	require.True(t, semerrs.IsTransient(concurrentErr))

	close(server.stopRelease)
	require.NoError(t, <-stopDone)
	require.NoError(t, m.Stop(t.Context()))
	require.Equal(t, int32(1), server.stopCalls.Load())
}

func TestMetricsFailedStartCleanupRetainsAuthorityUntilSuccessfulStop(t *testing.T) {
	wantErr := errors.New("provider cleanup blocked")
	stopErrs := make(chan error, 2)
	stopErrs <- wantErr
	stopErrs <- nil
	server := &controlledMetricsServer{stopErrs: stopErrs}
	base := NewBaseServiceWithOptions("metrics", nil)
	_, cancel := context.WithCancel(t.Context())
	m := &Metrics{
		BaseService:    base,
		server:         server,
		used:           true,
		cleanupPending: true,
		cancel:         cancel,
	}

	err := m.Stop(t.Context())
	require.ErrorIs(t, err, wantErr)
	require.True(t, m.cleanupPending)
	require.False(t, m.terminal)

	require.NoError(t, m.Stop(t.Context()))
	require.True(t, m.terminal)
	require.False(t, m.cleanupPending)
	require.Equal(t, int32(2), server.stopCalls.Load())
}

func TestMetricsRollsBackBoundProviderWhenBaseCommitFails(t *testing.T) {
	listener := boundMetricsListener(t)
	base := NewBaseServiceWithOptions("metrics", nil)
	require.NoError(t, base.Start(t.Context()))
	require.NoError(t, base.Stop(t.Context()))
	m := &Metrics{
		BaseService:  base,
		config:       MetricsConfig{Port: 9090, Path: "/metrics"},
		registry:     metric.NewMetricsRegistry(),
		testListener: listener,
	}

	err := m.Start(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, semerrs.ErrAlreadyStarted)
	require.True(t, m.terminal)
	require.Nil(t, m.server)

	connection, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
	require.Error(t, dialErr, "failed BaseService commit must release the bound metrics listener")
	if connection != nil {
		_ = connection.Close()
	}
}

func TestMetricsStopWaitsForStartFinalizationBeforeProviderCleanup(t *testing.T) {
	listener := boundMetricsListener(t)
	published := make(chan struct{})
	releaseStart := make(chan struct{})
	stopWaitObserved := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseStart) }) }

	m := &Metrics{
		BaseService:           NewBaseServiceWithOptions("metrics", nil),
		config:                MetricsConfig{Port: 9090, Path: "/metrics"},
		registry:              metric.NewMetricsRegistry(),
		testListener:          listener,
		testServerPublished:   published,
		testStartRelease:      releaseStart,
		testStartWaitUnlocked: stopWaitObserved,
	}

	startResult := make(chan error, 1)
	startFinished := make(chan struct{})
	t.Cleanup(func() {
		release()
		select {
		case <-startFinished:
		case <-time.After(5 * time.Second):
			t.Error("Metrics.Start did not finish after start gate released")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Stop(ctx); err != nil {
			t.Errorf("clean up Metrics after blocked Start: %v", err)
		}
	})
	go func() {
		defer close(startFinished)
		startResult <- m.Start(t.Context())
	}()
	select {
	case <-published:
	case <-startFinished:
		t.Fatal("Metrics.Start returned before publishing the bound server")
	}

	m.lifecycleMu.Lock()
	provider := m.server
	require.NotNil(t, provider)
	require.NotNil(t, m.startDone)
	require.False(t, m.running)
	m.lifecycleMu.Unlock()
	concrete, ok := provider.(*metric.Server)
	require.True(t, ok, "standalone Metrics must retain the concrete server")
	require.Equal(t, "http://"+listener.Addr().String()+"/metrics", concrete.Address())
	request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, concrete.Address(), nil)
	require.NoError(t, requestErr)
	response, requestErr := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	require.NoError(t, requestErr)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	m.BaseService.mu.RLock()
	require.Nil(t, m.BaseService.done, "BaseService commit must remain blocked until Start is released")
	require.Equal(t, StatusStopped, m.BaseService.status.Load())
	m.BaseService.mu.RUnlock()
	requireMetricsListenerOwned(t, listener)

	stopCtx, cancelStop := context.WithCancel(t.Context())
	stopResult := make(chan error, 1)
	go func() { stopResult <- m.Stop(stopCtx) }()
	<-stopWaitObserved
	require.True(t, m.lifecycleMu.TryLock(), "Metrics.Stop must wait for startDone outside lifecycleMu")
	m.lifecycleMu.Unlock()

	m.lifecycleMu.Lock()
	require.Same(t, provider, m.server)
	m.lifecycleMu.Unlock()
	requireMetricsListenerOwned(t, listener)

	cancelStop()
	stopErr := <-stopResult
	require.EqualError(t, stopErr, "wait for Metrics Start: context canceled")
	require.ErrorIs(t, stopErr, context.Canceled)

	m.lifecycleMu.Lock()
	require.Same(t, provider, m.server)
	require.False(t, m.terminal)
	m.lifecycleMu.Unlock()
	requireMetricsListenerOwned(t, listener)

	release()
	require.NoError(t, <-startResult)
	require.True(t, m.running)
	require.NoError(t, m.Stop(t.Context()))

	m.lifecycleMu.Lock()
	require.Nil(t, m.server)
	require.True(t, m.terminal)
	m.lifecycleMu.Unlock()

	connection, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
	require.Error(t, err, "completed Metrics.Stop must release the provider listener")
	if connection != nil {
		_ = connection.Close()
	}
}

func TestMetricsLifecycleContextAndStopBeforeStartAreImmutable(t *testing.T) {
	raw, err := json.Marshal(MetricsConfig{Port: 9090, Path: "/metrics"})
	require.NoError(t, err)
	svc, err := NewMetrics(raw, &Dependencies{MetricsRegistry: metric.NewMetricsRegistry()})
	require.NoError(t, err)
	m := svc.(*Metrics)

	ended, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, m.Start(nil))
	require.Error(t, m.Start(ended))
	require.False(t, m.used)
	require.Error(t, m.Stop(nil))
	require.False(t, m.used)

	require.NoError(t, m.Stop(t.Context()))
	require.ErrorIs(t, m.Start(t.Context()), semerrs.ErrAlreadyStarted)
	require.NoError(t, m.Stop(t.Context()))
}

func boundMetricsListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func requireMetricsListenerOwned(t *testing.T, listener net.Listener) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
	require.NoError(t, err, "published metrics provider must retain its listener")
	require.NoError(t, connection.Close())
}
