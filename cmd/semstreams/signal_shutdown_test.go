package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	shutdownerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type signalTestManager struct {
	mu         sync.Mutex
	startCtx   context.Context
	stopCtx    context.Context
	started    chan struct{}
	startErr   error
	stopErr    error
	operations []string
}

func (m *signalTestManager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	m.startCtx = ctx
	m.operations = append(m.operations, "start")
	m.mu.Unlock()
	close(m.started)
	return m.startErr
}

func TestRunUntilShutdownStartFailureUsesBoundedAbortCleanup(t *testing.T) {
	wantErr := errors.New("start failed")
	manager := &signalTestManager{started: make(chan struct{}), startErr: wantErr}
	var closeCtx context.Context
	err := runUntilShutdown(
		t.Context(), make(chan struct{}), manager, time.Second, 0,
		func(ctx context.Context) error {
			closeCtx = ctx
			return nil
		},
	)
	require.ErrorIs(t, err, wantErr)
	require.NotNil(t, manager.stopCtx)
	require.Same(t, manager.stopCtx, closeCtx)
	_, hasDeadline := closeCtx.Deadline()
	require.True(t, hasDeadline)
}

func (*signalTestManager) StartHealthListener(context.Context, int) error { return nil }

func (m *signalTestManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCtx = ctx
	m.operations = append(m.operations, "stop")
	if err := m.startCtx.Err(); err != nil {
		return errors.Join(errors.New("runtime authority canceled before StopAll"), err)
	}
	return m.stopErr
}

func TestRunUntilShutdownKeepsRuntimeAuthorityAndSharesOneBudgetWithClose(t *testing.T) {
	manager := &signalTestManager{started: make(chan struct{})}
	shutdownRequested := make(chan struct{})
	var closeCtx context.Context
	result := make(chan error, 1)
	go func() {
		result <- runUntilShutdown(
			t.Context(),
			shutdownRequested,
			manager,
			time.Second,
			0,
			func(ctx context.Context) error {
				manager.mu.Lock()
				defer manager.mu.Unlock()
				closeCtx = ctx
				manager.operations = append(manager.operations, "close")
				return nil
			},
		)
	}()
	<-manager.started
	close(shutdownRequested)
	require.NoError(t, <-result)

	manager.mu.Lock()
	defer manager.mu.Unlock()
	require.NoError(t, manager.startCtx.Err())
	require.Same(t, manager.stopCtx, closeCtx)
	_, hasDeadline := closeCtx.Deadline()
	require.True(t, hasDeadline)
	require.Equal(t, []string{"start", "stop", "close"}, manager.operations)
}

func TestRunUntilShutdownClosesTransportAfterStopFailure(t *testing.T) {
	wantErr := errors.New("service stop failed")
	manager := &signalTestManager{started: make(chan struct{}), stopErr: wantErr}
	shutdownRequested := make(chan struct{})
	closed := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- runUntilShutdown(
			t.Context(), shutdownRequested, manager, time.Second, 0,
			func(context.Context) error {
				close(closed)
				return nil
			},
		)
	}()
	<-manager.started
	close(shutdownRequested)
	require.ErrorIs(t, <-result, wantErr)
	<-closed
}

func TestRunUntilShutdownAttributesTransportCloseFailure(t *testing.T) {
	wantErr := errors.New("close failed")
	manager := &signalTestManager{started: make(chan struct{})}
	shutdownRequested := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- runUntilShutdown(
			t.Context(), shutdownRequested, manager, time.Second, 0,
			func(context.Context) error { return wantErr },
		)
	}()
	<-manager.started
	close(shutdownRequested)
	err := <-result
	var shutdownErr *shutdownerrs.ShutdownError
	require.ErrorAs(t, err, &shutdownErr)
	require.Equal(t, appName, shutdownErr.Owner)
	require.Equal(t, shutdownerrs.PhaseCloseTransport, shutdownErr.Phase)
	require.ErrorIs(t, err, wantErr)
}

func TestStopWithinShutdownBudgetReportsExpiredContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var budget time.Duration
	err := stopWithinShutdownBudget(ctx, func(timeout time.Duration) error {
		budget = timeout
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Positive(t, budget)
	var shutdownErr *shutdownerrs.ShutdownError
	require.ErrorAs(t, err, &shutdownErr)
	require.Equal(t, shutdownerrs.PhaseDrainSubscriptions, shutdownErr.Phase)
}
