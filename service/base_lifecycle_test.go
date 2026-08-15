package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBaseServiceStartRejectsWhileExactStopGenerationIsActive(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	s := NewBaseServiceWithOptions("start-stop", nil,
		WithHealthInterval(time.Hour),
		WithHealthCheck(func() error {
			close(entered)
			<-release
			return nil
		}),
	)
	require.NoError(t, s.Start(context.Background()))
	<-entered

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.Stop(canceled), context.Canceled)
	require.Error(t, s.Start(context.Background()), "Start must not replace a stopping generation")

	close(release)
	require.NoError(t, s.Stop(context.Background()))
	s.SetHealthCheck(nil)
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Stop(context.Background()))
}
