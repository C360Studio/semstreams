package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
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
	seedCtx, cancelSeed := context.WithCancel(t.Context())
	require.ErrorIs(t, generation.StopWithQuiesce(seedCtx, nil, func(ctx context.Context) error {
		cancelSeed()
		return errors.Join(wantShutdownErr, ctx.Err())
	}, nil), wantShutdownErr)

	w := &Output{
		clients:    make(map[*websocket.Conn]*clientInfo),
		generation: generation,
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

	close(releaseWait)
	rejoinedErr := w.Stop(t.Context())
	require.ErrorIs(t, rejoinedErr, wantShutdownErr)
	require.NotErrorIs(t, rejoinedErr, context.DeadlineExceeded)
	require.Same(t, generation, w.generation, "genuine terminal failure must retain replay authority")
	require.ErrorIs(t, w.Stop(t.Context()), wantShutdownErr)
}

func TestWebSocketOutputServerShutdownKeepsHandlerAuthorityLiveUntilHandlerReturns(t *testing.T) {
	runtimeCtx, cancel := context.WithCancel(t.Context())
	handlerCtx := make(chan context.Context, 1)
	releaseHandler := make(chan struct{})
	shutdownStarted := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		handlerCtx <- r.Context()
		<-releaseHandler
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{
		Handler:     mux,
		BaseContext: func(net.Listener) context.Context { return runtimeCtx },
	}
	server.RegisterOnShutdown(func() { close(shutdownStarted) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()
	requestDone := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/block")
		if response != nil {
			_ = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	requestCtx := <-handlerCtx

	output := &Output{
		clients:    make(map[*websocket.Conn]*clientInfo),
		server:     server,
		generation: lifecyclejoin.NewGeneration(cancel, func() { <-serveDone }),
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- output.Stop(t.Context()) }()
	<-shutdownStarted
	select {
	case <-requestCtx.Done():
		t.Fatal("WebSocket output handler authority canceled before listener Shutdown drained it")
	default:
	}
	close(releaseHandler)
	require.NoError(t, <-stopDone)
	require.NoError(t, <-requestDone)
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)
}
