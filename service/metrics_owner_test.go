package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
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
	port := freeMetricsPort(t)
	base := NewBaseServiceWithOptions("metrics", nil)
	require.NoError(t, base.Start(t.Context()))
	require.NoError(t, base.Stop(t.Context()))
	m := &Metrics{
		BaseService: base,
		config:      MetricsConfig{Port: port, Path: "/metrics"},
		registry:    metric.NewMetricsRegistry(),
	}

	err := m.Start(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, semerrs.ErrAlreadyStarted)
	require.True(t, m.terminal)
	require.Nil(t, m.server)

	connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
	require.Error(t, dialErr, "failed BaseService commit must release the bound metrics listener")
	if connection != nil {
		_ = connection.Close()
	}
}

func TestMetricsStopWaitsForStartFinalizationBeforeProviderCleanup(t *testing.T) {
	port := freeMetricsPort(t)
	published := make(chan struct{})
	releaseStart := make(chan struct{})
	stopWaitObserved := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseStart) }) }
	t.Cleanup(release)

	m := &Metrics{
		BaseService:           NewBaseServiceWithOptions("metrics", nil),
		config:                MetricsConfig{Port: port, Path: "/metrics"},
		registry:              metric.NewMetricsRegistry(),
		testServerPublished:   published,
		testStartRelease:      releaseStart,
		testStartWaitUnlocked: stopWaitObserved,
	}

	startResult := make(chan error, 1)
	go func() { startResult <- m.Start(t.Context()) }()
	<-published

	m.lifecycleMu.Lock()
	provider := m.server
	require.NotNil(t, provider)
	require.NotNil(t, m.startDone)
	require.False(t, m.running)
	m.lifecycleMu.Unlock()
	m.BaseService.mu.RLock()
	require.Nil(t, m.BaseService.done, "BaseService commit must remain blocked until Start is released")
	require.Equal(t, StatusStopped, m.BaseService.status.Load())
	m.BaseService.mu.RUnlock()
	requireMetricsPortOwned(t, port)

	stopCtx, cancelStop := context.WithCancel(t.Context())
	stopResult := make(chan error, 1)
	go func() { stopResult <- m.Stop(stopCtx) }()
	<-stopWaitObserved
	require.True(t, m.lifecycleMu.TryLock(), "Metrics.Stop must wait for startDone outside lifecycleMu")
	m.lifecycleMu.Unlock()

	m.lifecycleMu.Lock()
	require.Same(t, provider, m.server)
	m.lifecycleMu.Unlock()
	requireMetricsPortOwned(t, port)

	cancelStop()
	stopErr := <-stopResult
	require.EqualError(t, stopErr, "wait for Metrics Start: context canceled")
	require.ErrorIs(t, stopErr, context.Canceled)

	m.lifecycleMu.Lock()
	require.Same(t, provider, m.server)
	require.False(t, m.terminal)
	m.lifecycleMu.Unlock()
	requireMetricsPortOwned(t, port)

	release()
	require.NoError(t, <-startResult)
	require.True(t, m.running)
	require.NoError(t, m.Stop(t.Context()))

	m.lifecycleMu.Lock()
	require.Nil(t, m.server)
	require.True(t, m.terminal)
	m.lifecycleMu.Unlock()

	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	require.NoError(t, err, "completed Metrics.Stop must release the provider listener")
	require.NoError(t, listener.Close())
}

func TestMetricsLifecycleContextAndStopBeforeStartAreImmutable(t *testing.T) {
	raw, err := json.Marshal(MetricsConfig{Port: freeMetricsPort(t), Path: "/metrics"})
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

func freeMetricsPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func requireMetricsPortOwned(t *testing.T, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	require.Error(t, err, "published metrics provider must retain its exact listener")
	if listener != nil {
		require.NoError(t, listener.Close())
	}
}
