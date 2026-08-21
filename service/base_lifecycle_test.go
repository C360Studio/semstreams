package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBaseServiceStartRejectsCanceledContext(t *testing.T) {
	s := NewBaseServiceWithOptions("canceled-start", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Start(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, StatusStopped, s.Status())
	require.NoError(t, s.Stop(context.Background()))
}

func TestBaseServiceStopIsOneShotAfterFailedJoin(t *testing.T) {
	tests := []struct {
		name        string
		stopContext func() (context.Context, context.CancelFunc)
		wantErr     error
	}{
		{
			name: "canceled",
			stopContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline",
			stopContext: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Unix(1, 0))
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			checkReturned := make(chan struct{})
			callbackStarted := make(chan struct{}, 1)
			s := NewBaseServiceWithOptions("start-stop", nil,
				WithHealthInterval(time.Hour),
				WithHealthCheck(func() error {
					close(entered)
					<-release
					close(checkReturned)
					return nil
				}),
			)
			s.OnHealthChange(func(bool) { callbackStarted <- struct{}{} })
			require.NoError(t, s.Start(context.Background()))
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("initial health check did not start")
			}

			stopCtx, stopCancel := tt.stopContext()
			require.ErrorIs(t, s.Stop(stopCtx), tt.wantErr)
			stopCancel()

			secondStop := make(chan error, 1)
			go func() { secondStop <- s.Stop(context.Background()) }()
			select {
			case err := <-secondStop:
				require.NoError(t, err, "completed repeated Stop must not rejoin timed-out work")
			case <-time.After(time.Second):
				close(release)
				t.Fatal("completed repeated Stop attempted to rejoin timed-out work")
			}
			require.Equal(t, StatusStopping, s.Status(), "repeat Stop must not predict owner completion")
			require.Error(t, s.Start(context.Background()), "same-instance Start must reject after terminal Stop")

			close(release)
			select {
			case <-checkReturned:
			case <-time.After(time.Second):
				t.Fatal("owned health check did not return")
			}
			select {
			case <-s.done:
			case <-time.After(time.Second):
				t.Fatal("owned BaseService goroutines did not join")
			}
			select {
			case <-callbackStarted:
				t.Fatal("health callback ran after terminal shutdown began")
			default:
			}
		})
	}
}

func TestBaseServiceCompletedStopRejectsSameInstanceRestart(t *testing.T) {
	s := NewBaseServiceWithOptions("terminal", nil, WithHealthInterval(0))
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Stop(context.Background()))
	require.NoError(t, s.Stop(context.Background()))
	require.Error(t, s.Start(context.Background()))
}

func TestBaseServiceStopBeforeStartRejectsSameInstanceStart(t *testing.T) {
	s := NewBaseServiceWithOptions("stopped-before-start", nil, WithHealthInterval(0))
	require.NoError(t, s.Stop(context.Background()))
	require.NoError(t, s.Stop(context.Background()))
	require.Error(t, s.Start(context.Background()))
}

func TestBaseServiceParentCancellationConvergesAndJoins(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	s := NewBaseServiceWithOptions("parent-cancel", nil, WithHealthInterval(0))
	require.NoError(t, s.Start(parent))
	cancel()

	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not join owned BaseService goroutines")
	}
	require.Equal(t, StatusStopped, s.Status())
	require.NoError(t, s.Stop(context.Background()))
	require.Error(t, s.Start(context.Background()))
}
