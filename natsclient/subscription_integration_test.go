//go:build integration

package natsclient

import (
	"context"
	"fmt"
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
