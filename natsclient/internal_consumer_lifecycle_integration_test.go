//go:build integration

package natsclient

import (
	"context"
	"runtime"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestInternalConsumerReturnsNativeHandleAndRejectsDuplicateIncumbent(t *testing.T) {
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "I1_INTERNAL", Subjects: []string{"i1.internal.>"}},
	))
	defer testClient.Terminate()
	cfg := StreamConsumerConfig{
		StreamName: "I1_INTERNAL", ConsumerName: "i1-fixed",
		FilterSubject: "i1.internal.>", AckPolicy: "explicit", DeliverPolicy: "all",
	}
	delivered := make(chan struct{}, 1)
	handle, err := testClient.Client.ConsumeInternalStreamWithConfig(
		t.Context(), cfg, func(_ context.Context, msg jetstream.Msg) {
			delivered <- struct{}{}
			_ = msg.Ack()
		},
	)
	require.NoError(t, err)
	require.NotNil(t, handle)

	key := cfg.StreamName + ":" + cfg.ConsumerName
	testClient.Client.consumersMu.RLock()
	_, cataloged := testClient.Client.consumers[key]
	testClient.Client.consumersMu.RUnlock()
	require.False(t, cataloged, "internal native ownership must not enter the Client lifecycle catalog")

	duplicate, duplicateErr := testClient.Client.ConsumeInternalStreamWithConfig(
		t.Context(), cfg, func(context.Context, jetstream.Msg) {},
	)
	require.Nil(t, duplicate)
	require.Error(t, duplicateErr)
	require.True(t, errs.IsInvalid(duplicateErr))

	require.NoError(t, testClient.Client.PublishToStream(t.Context(), "i1.internal.work", []byte("work")))
	<-delivered

	handle.Drain()
	<-handle.Closed()
	waitForInternalClaimRelease(t, testClient.Client, internalConsumerIdentity{
		stream: cfg.StreamName, durable: cfg.ConsumerName,
	})
	reacquired, err := testClient.Client.ConsumeInternalStreamWithConfig(
		t.Context(), cfg, func(context.Context, jetstream.Msg) {},
	)
	require.NoError(t, err)
	reacquired.Drain()
	<-reacquired.Closed()
	waitForInternalClaimRelease(t, testClient.Client, internalConsumerIdentity{
		stream: cfg.StreamName, durable: cfg.ConsumerName,
	})
}

func TestInternalConsumerSetupFailureReleasesReservationWithoutDelivery(t *testing.T) {
	testClient := NewTestClient(t, WithJetStream(), WithStreams(
		TestStreamConfig{Name: "I1_SETUP", Subjects: []string{"i1.setup.>"}},
	))
	defer testClient.Terminate()
	cfg := StreamConsumerConfig{
		StreamName: "I1_SETUP", ConsumerName: "i1-setup-fixed",
		FilterSubject: "i1.setup.>.invalid", AckPolicy: "explicit", DeliverPolicy: "all",
	}
	delivered := make(chan struct{}, 1)
	handle, err := testClient.Client.ConsumeInternalStreamWithConfig(
		t.Context(), cfg, func(context.Context, jetstream.Msg) { delivered <- struct{}{} },
	)
	require.Nil(t, handle)
	require.Error(t, err)
	select {
	case <-delivered:
		t.Fatal("setup failure began delivery")
	default:
	}

	cfg.FilterSubject = "i1.setup.>"
	handle, err = testClient.Client.ConsumeInternalStreamWithConfig(
		t.Context(), cfg, func(context.Context, jetstream.Msg) {},
	)
	require.NoError(t, err, "failed setup must release the exact local reservation")
	handle.Drain()
	<-handle.Closed()
	waitForInternalClaimRelease(t, testClient.Client, internalConsumerIdentity{
		stream: cfg.StreamName, durable: cfg.ConsumerName,
	})
}

func waitForInternalClaimRelease(t *testing.T, client *Client, identity internalConsumerIdentity) {
	t.Helper()
	released := make(chan struct{})
	go func() {
		defer close(released)
		for {
			client.internalClaimsMu.Lock()
			_, active := client.internalClaims[identity]
			client.internalClaimsMu.Unlock()
			if !active {
				return
			}
			runtime.Gosched()
		}
	}()
	<-released
}

func waitForConsumerObservationRemoval(t *testing.T, metrics *jetstreamMetrics, key string) {
	t.Helper()
	removed := make(chan struct{})
	go func() {
		defer close(removed)
		for {
			metrics.mu.Lock()
			_, active := metrics.consumers[key]
			metrics.mu.Unlock()
			if !active {
				return
			}
			runtime.Gosched()
		}
	}()
	<-removed
}
