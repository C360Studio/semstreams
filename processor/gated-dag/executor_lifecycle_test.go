package gateddagexec

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/dispatch"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/worker"
	"github.com/stretchr/testify/require"
)

func TestExecutorStopCancelsDispatcherWorkBeforeJoin(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	workStarted := make(chan struct{})
	workCanceled := make(chan struct{})
	disp, err := dispatch.New(runCtx, dispatch.Config[dispatchJob]{
		Workers:   1,
		QueueSize: 1,
		Process: func(ctx context.Context, _ dispatchJob) error {
			close(workStarted)
			<-ctx.Done()
			close(workCanceled)
			return ctx.Err()
		},
	}, dispatch.Deps{Logger: discardLogger()})
	require.NoError(t, err)
	require.NoError(t, disp.Submit(dispatchJob{unitID: "unit"}))
	<-workStarted

	done := make(chan struct{})
	close(done)
	exec := &executor{disp: disp, cancel: cancel, done: done}

	require.NoError(t, exec.stop(context.Background()))
	<-workCanceled
	require.Nil(t, exec.cancel, "Stop consumes cancellation authority exactly once")
	require.NoError(t, exec.stop(context.Background()), "later Stop is a terminal no-op")
}

func TestComponentFailedStopIsTerminalAndUnhealthy(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	exec := &executor{cancel: cancel, done: make(chan struct{})}
	c := &Component{exec: exec, running: true}

	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	err := c.Stop(stopCtx)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, runCtx.Err(), context.Canceled)

	health := c.Health()
	require.False(t, health.Healthy)
	require.Equal(t, "unhealthy", health.Status)

	err = c.Stop(context.Background())
	require.Error(t, err, "a later Component Stop must not report false success")
	require.ErrorContains(t, err, "already claimed")
}

func TestComponentCompletedStopRepeatsWithoutTeardown(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	close(done)
	exec := &executor{cancel: cancel, done: done}
	c := &Component{exec: exec, running: true, logger: discardLogger()}

	require.NoError(t, c.Stop(context.Background()))
	require.False(t, c.running)
	require.NoError(t, c.Stop(context.Background()))
}

func TestComponentRejectsSameInstanceStartAfterCompletedStop(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	close(done)
	c := &Component{
		exec:    &executor{cancel: cancel, done: done},
		running: true,
		logger:  discardLogger(),
	}
	require.NoError(t, c.Stop(context.Background()))

	err := c.Start(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "cannot be restarted")
}

func TestExecutorStartWatchFailureStopsExactDispatcher(t *testing.T) {
	exec := &executor{
		cfg:     validCfg(),
		log:     discardLogger(),
		mgr:     lifecycle.NewManager(nil, nil),
		claimer: &fakeClaimer{},
		pub:     &fakePublisher{},
	}

	err := exec.start(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "workflow")
	require.NotNil(t, exec.disp, "failed Start retains the exact dispatcher through rollback")
	require.True(t, errors.Is(exec.disp.Submit(dispatchJob{}), worker.ErrPoolStopped))
}
