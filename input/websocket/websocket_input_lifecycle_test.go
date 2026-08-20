package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/stretchr/testify/require"
)

// createTestComponent creates a test instance for lifecycle testing.
func createTestComponent() component.LifecycleComponent {
	mockClient := &natsclient.Client{}
	config := DefaultConfig()
	config.Mode = ModeServer

	input, err := NewInput(
		"websocket-input-test",
		mockClient,
		config,
		nil, // metrics registry
		security.Config{},
	)
	if err != nil {
		panic("failed to create test component: " + err.Error())
	}

	return input
}

func TestWebSocketInputServerShutdownKeepsHandlerAuthorityLiveUntilHandlerReturns(t *testing.T) {
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
	serveDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
		close(serveDone)
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

	input := createTestComponent().(*Input)
	input.httpServer = server
	input.serveDone = serveDone
	input.cancel = cancel
	input.lifecycleUsed = true
	input.started.Store(true)
	stopDone := make(chan error, 1)
	go func() { stopDone <- input.Stop(t.Context()) }()
	<-shutdownStarted
	select {
	case <-requestCtx.Done():
		t.Fatal("WebSocket input handler authority canceled before listener Shutdown drained it")
	default:
	}
	close(releaseHandler)
	require.NoError(t, <-stopDone)
	require.NoError(t, <-requestDone)
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)
}
