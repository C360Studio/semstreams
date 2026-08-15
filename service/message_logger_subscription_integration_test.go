//go:build integration

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestMessageLoggerExplicitSubscriptionContextLivesUntilStop(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithMinimalFeatures())
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"logger.explicit"},
		MaxEntries:      10,
		SampleRate:      1,
	}, testClient.Client)
	require.NoError(t, err)

	callbackCompletions := make(chan messageLoggerCallbackCompletion, 2)
	ml.subscribe = func(
		ctx context.Context, subject string, handler func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		subscription, subscribeErr := testClient.Client.Subscribe(
			ctx,
			subject,
			func(msgCtx context.Context, msg *nats.Msg) {
				contextErr := msgCtx.Err()
				handler(msgCtx, msg)
				callbackCompletions <- messageLoggerCallbackCompletion{contextErr: contextErr}
			},
		)
		if subscribeErr != nil {
			return nil, subscribeErr
		}
		return &failFirstMessageLoggerUnsubscribe{subscription: subscription}, nil
	}

	require.NoError(t, ml.Start(t.Context()))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	require.NoError(t, testClient.Client.Publish(t.Context(), "logger.explicit", []byte(`{"phase":"running"}`)))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	require.NoError(t, receiveCallbackContextError(t, callbackCompletions),
		"a subscription installed by Start must receive a live handler context")
	require.Eventually(t, func() bool {
		return len(ml.GetMessages()) == 1
	}, 5*time.Second, 10*time.Millisecond)

	firstStopErr := ml.Stop(context.Background())
	require.ErrorContains(t, firstStopErr, "injected unsubscribe failure")
	require.NoError(t, testClient.Client.Publish(t.Context(), "logger.explicit", []byte(`{"phase":"stopped"}`)))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	require.ErrorIs(t, receiveCallbackContextError(t, callbackCompletions), context.Canceled,
		"Stop must cancel handler context even when unsubscribe must be retried")
	require.Len(t, ml.GetMessages(), 1, "a stopped logger must not capture later publishes")
	require.EqualError(t, ml.Stop(context.Background()), firstStopErr.Error(),
		"a genuine teardown failure is retained and replayed without repeating the side effect")
}

type failFirstMessageLoggerUnsubscribe struct {
	mu           sync.Mutex
	failed       bool
	subscription messageLoggerSubscription
}

func (s *failFirstMessageLoggerUnsubscribe) Unsubscribe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.failed {
		s.failed = true
		return errors.New("injected unsubscribe failure")
	}
	return s.subscription.Unsubscribe()
}

type messageLoggerCallbackCompletion struct {
	contextErr error
}

func receiveCallbackContextError(
	t *testing.T, callbackCompletions <-chan messageLoggerCallbackCompletion,
) error {
	t.Helper()
	select {
	case completion := <-callbackCompletions:
		return completion.contextErr
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for production subscription callback")
		return nil
	}
}
