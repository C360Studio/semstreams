//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestIntegration_PublicSubscriptionFactoriesReturnDrainableAuthority(t *testing.T) {
	natsContainer, natsURL := startNATSContainer(t.Context(), t)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, natsContainer.Terminate(cleanupCtx))
	})

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(t.Context()))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Close(cleanupCtx))
	})

	tests := []struct {
		name      string
		subscribe func(string) (*Subscription, error)
	}{
		{
			name: "raw",
			subscribe: func(subject string) (*Subscription, error) {
				return client.Subscribe(t.Context(), subject, func(context.Context, *nats.Msg) {})
			},
		},
		{
			name: "request",
			subscribe: func(subject string) (*Subscription, error) {
				return client.SubscribeForRequests(t.Context(), subject, func(context.Context, []byte) ([]byte, error) {
					return nil, nil
				})
			},
		},
		{
			name: "typed",
			subscribe: func(subject string) (*Subscription, error) {
				return NewSubject[string](subject).Subscribe(t.Context(), client, func(context.Context, string) error {
					return nil
				})
			},
		},
		{
			name: "typed_with_message",
			subscribe: func(subject string) (*Subscription, error) {
				return NewSubject[string](subject).SubscribeWithMsg(t.Context(), client, func(context.Context, *nats.Msg, string) error {
					return nil
				})
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sub, err := test.subscribe(fmt.Sprintf("subscription.factory.%d", i))
			require.NoError(t, err)
			drainCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			require.NoError(t, sub.Drain(drainCtx))
		})
	}
}

// A connection that closes while a callback runs must still let Drain return
// nil once that callback returns (#1372). This also guards a nats.go upgrade
// that changes when the closed handler fires.
func TestIntegration_SubscriptionDrainAfterConnectionCloseJoinsCallback(t *testing.T) {
	testClient := NewTestClient(t, WithMinimalFeatures())
	client := testClient.Client

	entered := make(chan struct{})
	release := make(chan struct{})
	var handlerReturned atomic.Bool
	sub, err := client.Subscribe(t.Context(), "subscription.drain.closed", func(context.Context, *nats.Msg) {
		close(entered)
		<-release
		handlerReturned.Store(true)
	})
	require.NoError(t, err)

	require.NoError(t, client.Publish(t.Context(), "subscription.drain.closed", []byte("x")))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("handler never received the message")
	}

	// Close the native connection directly: Client.Close would drain first.
	testClient.GetNativeConnection().Close()

	drainCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- sub.Drain(drainCtx) }()

	// A Drain that does not wait for the callback returns within
	// microseconds; 200ms is ample to see it, and a slow host can only make
	// this check miss a defect, never fail a correct Drain.
	select {
	case err := <-result:
		t.Fatalf("Drain returned while the callback was still running: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-result)
	require.True(t, handlerReturned.Load(), "Drain must return only after the callback has returned")
}
