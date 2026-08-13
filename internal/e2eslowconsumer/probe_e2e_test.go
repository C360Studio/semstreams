//go:build e2e_slow_consumer

package e2eslowconsumer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

func TestGatedErrorHandlerDelegatesUnrelatedCallbacksImmediately(t *testing.T) {
	delegated := make(chan error, 1)
	installed := func(_ *nats.Conn, _ *nats.Subscription, err error) { delegated <- err }
	callbackEntered := make(chan callbackObservation, 1)
	releaseCallback := make(chan struct{})
	callbackHandled := make(chan struct{}, 1)
	var matching atomic.Bool
	handler := gatedErrorHandler(
		t.Context(), installed, callbackEntered, releaseCallback, callbackHandled, &matching,
	)

	want := errors.New("unrelated")
	handler(nil, nil, want)
	select {
	case got := <-delegated:
		require.ErrorIs(t, got, want)
	case <-t.Context().Done():
		t.Fatalf("unrelated callback did not delegate immediately: %v", t.Context().Err())
	}
	select {
	case <-callbackEntered:
		t.Fatal("unrelated callback entered the fixture gate")
	default:
	}
}

func TestRunUsesAndRestoresInstalledErrorHandler(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	go server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, err := natsclient.NewClient(server.ClientURL(),
		natsclient.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	connection := client.GetConnection()
	installed := connection.ErrorHandler()
	require.NotNil(t, installed)
	require.NoError(t, Run(ctx, client))
	require.NotNil(t, connection.ErrorHandler())
	require.Equal(t,
		reflect.ValueOf(installed).Pointer(),
		reflect.ValueOf(connection.ErrorHandler()).Pointer(),
		"probe must restore the exact installed callback",
	)
}
