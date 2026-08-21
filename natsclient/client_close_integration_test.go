//go:build integration

package natsclient

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestIntegration_ClientCloseWaitsForNativeDrain(t *testing.T) {
	testClient := NewTestClient(t, WithMinimalFeatures())
	client := testClient.Client
	conn := client.conn

	handlerStarted := make(chan struct{})
	runClientOperation := make(chan struct{})
	clientOperationDone := make(chan error, 1)
	releaseHandler := make(chan struct{})
	handlerDone := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseHandler) })
	})

	_, err := conn.Subscribe("client.close.drain", func(_ *nats.Msg) {
		close(handlerStarted)
		<-runClientOperation
		_, operationErr := client.MaxPayload()
		clientOperationDone <- operationErr
		<-releaseHandler
		close(handlerDone)
	})
	require.NoError(t, err)
	require.NoError(t, conn.Flush())
	require.NoError(t, conn.Publish("client.close.drain", []byte("work")))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	select {
	case <-handlerStarted:
	case <-ctx.Done():
		t.Fatalf("wait for subscription handler to start: %v", ctx.Err())
	}

	draining := conn.StatusChanged(nats.DRAINING_SUBS)
	nativeClosed := conn.StatusChanged(nats.CLOSED)
	defer conn.RemoveStatusListener(draining)
	defer conn.RemoveStatusListener(nativeClosed)

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- client.Close(ctx)
	}()

	select {
	case <-draining:
	case <-ctx.Done():
		t.Fatalf("wait for native drain to start: %v", ctx.Err())
	}
	close(runClientOperation)
	select {
	case operationErr := <-clientOperationDone:
		require.NoError(t, operationErr)
	case <-ctx.Done():
		t.Fatalf("wait for draining handler client operation: %v", ctx.Err())
	}

	// DRAINING_SUBS proves Close initiated the native drain. Hold the handler
	// briefly so an immediate Close has a bounded chance to expose the defect;
	// successful progress below is synchronized only through explicit channels.
	blockedWindow := time.NewTimer(250 * time.Millisecond)
	select {
	case err := <-closeDone:
		blockedWindow.Stop()
		t.Fatalf("Client.Close completed while an in-flight handler was blocked: %v", err)
	case status := <-nativeClosed:
		blockedWindow.Stop()
		t.Fatalf("native connection reached %s while an in-flight handler was blocked", status)
	case <-blockedWindow.C:
	}

	releaseOnce.Do(func() { close(releaseHandler) })

	select {
	case <-handlerDone:
	case <-ctx.Done():
		t.Fatalf("wait for subscription handler to finish: %v", ctx.Err())
	}
	select {
	case status := <-nativeClosed:
		require.Equal(t, nats.CLOSED, status)
	case <-ctx.Done():
		t.Fatalf("wait for native connection to close: %v", ctx.Err())
	}
	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatalf("wait for Client.Close to complete: %v", ctx.Err())
	}
}
