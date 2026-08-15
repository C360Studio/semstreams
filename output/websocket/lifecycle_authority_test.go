package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestRetainedPartialStartAuthorityRejectsRestartAndReplaysGenuineError(t *testing.T) {
	runCtx, cancel := context.WithCancel(t.Context())
	cancelObserved := make(chan struct{})
	releaseWait := make(chan struct{})
	generation := lifecyclejoin.NewGeneration(cancel, func() {
		<-runCtx.Done()
		close(cancelObserved)
		<-releaseWait
	})
	wantShutdownErr := errors.New("shutdown failed")
	serverShutdown := lifecyclejoin.NewOperation()
	require.ErrorIs(t, serverShutdown.Run(t.Context(), func(context.Context) error {
		return wantShutdownErr
	}), wantShutdownErr)

	w := &Output{
		clients:        make(map[*websocket.Conn]*clientInfo),
		generation:     generation,
		serverShutdown: serverShutdown,
	}
	firstCtx, cancelFirst := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancelFirst()
	firstErr := w.Stop(firstCtx)
	require.ErrorIs(t, firstErr, context.DeadlineExceeded)
	require.ErrorIs(t, firstErr, wantShutdownErr)
	select {
	case <-cancelObserved:
	default:
		t.Fatal("Stop must cancel the generation before waiting for completion")
	}

	restartErr := w.Start(t.Context())
	require.Error(t, restartErr)
	require.ErrorIs(t, restartErr, errs.ErrAlreadyStarted)
	require.Same(t, generation, w.generation)
	require.Same(t, serverShutdown, w.serverShutdown)

	close(releaseWait)
	rejoinedErr := w.Stop(t.Context())
	require.ErrorIs(t, rejoinedErr, wantShutdownErr)
	require.NotErrorIs(t, rejoinedErr, context.DeadlineExceeded)
	require.Same(t, generation, w.generation, "genuine terminal failure must retain replay authority")
	require.Same(t, serverShutdown, w.serverShutdown)
	require.ErrorIs(t, w.Stop(t.Context()), wantShutdownErr)
}
