package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

var canonicalGraphIndexQuerySubjects = []string{
	"graph.index.query.outgoing",
	"graph.index.query.incoming",
	"graph.index.query.alias",
	"graph.index.query.predicate",
	"graph.index.query.predicateList",
	"graph.index.query.predicateStats",
	"graph.index.query.predicateCompound",
	"graph.index.query.byName",
}

type failedStartRuntimeHandles struct {
	runDone       <-chan struct{}
	poolDone      <-chan struct{}
	coalescerDone <-chan struct{}
}

type observedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestComponentFailedStartSecondQuerySubscriptionRollsBackBeforeCancel(t *testing.T) {
	index, client := newFailedStartSubscriptionComponent(t)
	sentinel := errors.New("incoming subscription failed")
	releaseCallback := make(chan struct{})
	callbackEntered := make(chan context.Context, 1)
	callbackReturned := make(chan error, 1)
	failureReturned := make(chan struct{})
	runtimeHandles := make(chan failedStartRuntimeHandles, 1)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCallback) }) })

	call := 0
	index.subscribeForRequests = func(
		ctx context.Context,
		subject string,
		handler func(context.Context, []byte) ([]byte, error),
	) (*natsclient.Subscription, error) {
		call++
		switch call {
		case 1:
			require.Equal(t, canonicalGraphIndexQuerySubjects[0], subject)
			return client.SubscribeForRequests(ctx, subject, func(callbackCtx context.Context, data []byte) ([]byte, error) {
				callbackEntered <- callbackCtx
				<-releaseCallback
				response, err := handler(callbackCtx, data)
				callbackReturned <- callbackCtx.Err()
				return response, err
			})
		case 2:
			require.Equal(t, canonicalGraphIndexQuerySubjects[1], subject)
			require.NoError(t, client.Publish(ctx, canonicalGraphIndexQuerySubjects[0], []byte(`{}`)))
			require.NoError(t, client.GetConnection().Flush())
			callbackCtx := <-callbackEntered
			require.NoError(t, callbackCtx.Err(), "failed-Start callback authority canceled before rollback drain")
			runtimeHandles <- failedStartRuntimeHandles{
				runDone:       index.runDone,
				poolDone:      index.indexPool.done,
				coalescerDone: index.entityCoalescer.done,
			}
			close(failureReturned)
			return nil, sentinel
		default:
			t.Fatalf("unexpected subscription acquisition %d for %s", call, subject)
			return nil, errors.New("unexpected subscription acquisition")
		}
	}

	startResult := make(chan error, 1)
	go func() { startResult <- index.Start(t.Context()) }()
	<-failureReturned
	select {
	case err := <-startResult:
		t.Fatalf("Start returned before admitted outgoing callback completed: %v", err)
	default:
	}

	releaseOnce.Do(func() { close(releaseCallback) })
	require.NoError(t, <-callbackReturned, "callback authority canceled before admitted callback returned")
	startErr := <-startResult
	require.ErrorIs(t, startErr, sentinel)

	handles := <-runtimeHandles
	requireChannelClosed(t, handles.runDone, "runtime children did not join before failed Start returned")
	requireChannelClosed(t, handles.poolDone, "dispatcher children did not join before failed Start returned")
	requireChannelClosed(t, handles.coalescerDone, "coalescer did not join before failed Start returned")

	index.mu.RLock()
	defer index.mu.RUnlock()
	require.Nil(t, index.runCancel)
	require.Nil(t, index.runDone)
	require.Nil(t, index.indexPool)
	require.Nil(t, index.entityCoalescer)
	require.Empty(t, index.querySubscriptions)
	require.False(t, index.cleanupPending)
	require.False(t, index.running)
}

func TestComponentFailedStartSubscriptionRollbackExpiryRetainsAuthorityForCallerStop(t *testing.T) {
	index, client := newFailedStartSubscriptionComponent(t)
	sentinel := errors.New("incoming subscription failed")
	releaseCallback := make(chan struct{})
	callbackEntered := make(chan context.Context, 1)
	var outgoingSubscription *natsclient.Subscription
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCallback) }) })

	call := 0
	index.subscribeForRequests = func(
		ctx context.Context,
		subject string,
		handler func(context.Context, []byte) ([]byte, error),
	) (*natsclient.Subscription, error) {
		call++
		if call == 1 {
			require.Equal(t, canonicalGraphIndexQuerySubjects[0], subject)
			subscription, err := client.SubscribeForRequests(ctx, subject, func(callbackCtx context.Context, data []byte) ([]byte, error) {
				callbackEntered <- callbackCtx
				<-releaseCallback
				return handler(callbackCtx, data)
			})
			outgoingSubscription = subscription
			return subscription, err
		}
		require.Equal(t, canonicalGraphIndexQuerySubjects[1], subject)
		require.NoError(t, client.Publish(ctx, canonicalGraphIndexQuerySubjects[0], []byte(`{}`)))
		require.NoError(t, client.GetConnection().Flush())
		callbackCtx := <-callbackEntered
		require.NoError(t, callbackCtx.Err(), "failed-Start callback authority canceled before rollback drain")
		return nil, sentinel
	}

	startResult := make(chan error, 1)
	go func() { startResult <- index.Start(t.Context()) }()
	select {
	case startErr := <-startResult:
		require.ErrorIs(t, startErr, sentinel)
		require.ErrorIs(t, startErr, context.DeadlineExceeded)
	case <-time.After(7 * time.Second):
		t.Fatal("failed Start did not return after the canonical rollback budget")
	}

	index.mu.RLock()
	require.True(t, index.cleanupPending)
	require.NotNil(t, index.runCancel)
	require.NotNil(t, index.runDone)
	require.NotNil(t, index.indexPool)
	require.NotNil(t, index.entityCoalescer)
	require.Len(t, index.querySubscriptions, 1)
	retainedSubscription := index.querySubscriptions[0]
	index.mu.RUnlock()
	require.Same(t, outgoingSubscription, retainedSubscription,
		"failed Start did not retain the exact acquired outgoing subscription")

	startErr := index.Start(t.Context())
	require.Error(t, startErr)
	require.Contains(t, startErr.Error(), "cleanup pending")

	releaseOnce.Do(func() { close(releaseCallback) })
	stopCtx := &observedDoneContext{Context: t.Context(), observed: make(chan struct{})}
	require.NoError(t, index.Stop(stopCtx))
	requireChannelClosed(t, stopCtx.observed, "later Stop context was not passed to retained subscription drain")

	index.mu.RLock()
	require.False(t, index.cleanupPending)
	require.Nil(t, index.runCancel)
	require.Nil(t, index.runDone)
	require.Nil(t, index.indexPool)
	require.Nil(t, index.entityCoalescer)
	require.Empty(t, index.querySubscriptions)
	index.mu.RUnlock()
	require.NoError(t, index.Stop(t.Context()), "completed retained cleanup must make repeated Stop a no-op")
}

func TestComponentRetryAfterSuccessfulFailedStartHasOneResponderPerSubject(t *testing.T) {
	index, client := newFailedStartSubscriptionComponent(t)
	sentinel := errors.New("incoming subscription failed")
	callbackDone := make(chan struct{})
	failureSubjects := make([]string, 0, 2)

	index.subscribeForRequests = func(
		ctx context.Context,
		subject string,
		handler func(context.Context, []byte) ([]byte, error),
	) (*natsclient.Subscription, error) {
		failureSubjects = append(failureSubjects, subject)
		if len(failureSubjects) == 1 {
			return client.SubscribeForRequests(ctx, subject, func(callbackCtx context.Context, data []byte) ([]byte, error) {
				response, err := handler(callbackCtx, data)
				close(callbackDone)
				return response, err
			})
		}
		require.NoError(t, client.Publish(ctx, canonicalGraphIndexQuerySubjects[0], []byte(`{}`)))
		require.NoError(t, client.GetConnection().Flush())
		<-callbackDone
		return nil, sentinel
	}

	require.ErrorIs(t, index.Start(t.Context()), sentinel)
	require.Equal(t, canonicalGraphIndexQuerySubjects[:2], failureSubjects)

	committedSubjects := make([]string, 0, len(canonicalGraphIndexQuerySubjects))
	index.subscribeForRequests = func(
		ctx context.Context,
		subject string,
		handler func(context.Context, []byte) ([]byte, error),
	) (*natsclient.Subscription, error) {
		committedSubjects = append(committedSubjects, subject)
		return client.SubscribeForRequests(ctx, subject, handler)
	}
	require.NoError(t, index.Start(t.Context()))
	require.Equal(t, canonicalGraphIndexQuerySubjects, committedSubjects)

	conn := client.GetConnection()
	replyInbox := nats.NewInbox()
	replies, err := conn.SubscribeSync(replyInbox)
	require.NoError(t, err)
	t.Cleanup(func() { _ = replies.Unsubscribe() })
	require.NoError(t, conn.Flush())
	require.NoError(t, conn.PublishRequest(canonicalGraphIndexQuerySubjects[0], replyInbox, []byte(`{}`)))
	require.NoError(t, conn.Flush())
	_, err = replies.NextMsg(2 * time.Second)
	require.NoError(t, err, "committed outgoing responder did not answer")
	_, err = replies.NextMsg(200 * time.Millisecond)
	require.ErrorIs(t, err, nats.ErrTimeout, "failed-attempt responder survived clean rollback")

	require.NoError(t, index.Stop(t.Context()))
	require.NoError(t, conn.PublishRequest(canonicalGraphIndexQuerySubjects[0], replyInbox, []byte(`{}`)))
	require.NoError(t, conn.Flush())
	_, err = replies.NextMsg(200 * time.Millisecond)
	require.True(t, errors.Is(err, nats.ErrTimeout) || errors.Is(err, nats.ErrNoResponders),
		"responder remained after committed Stop: %v", err)
}

func newFailedStartSubscriptionComponent(t *testing.T) (*Component, *natsclient.Client) {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	})
	require.NoError(t, err)
	go server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))

	client, err := natsclient.NewClient(server.ClientURL())
	require.NoError(t, err)
	require.NoError(t, client.Connect(t.Context()))
	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateKeyValue(t.Context(), jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Failed-Start query subscription lifecycle proof",
	})
	require.NoError(t, err)

	config := DefaultConfig()
	config.StartupAttempts = 1
	config.StartupInterval = 1
	config.CoalesceMs = 1
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	created, err := CreateGraphIndex(rawConfig, component.Dependencies{NATSClient: client})
	require.NoError(t, err)
	index := created.(*Component)
	require.NoError(t, index.Initialize())

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = index.Stop(cleanupCtx)
		_ = client.Close(cleanupCtx)
		server.Shutdown()
	})
	return index, client
}

func requireChannelClosed(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	require.NotNil(t, ch)
	select {
	case <-ch:
	default:
		t.Fatal(message)
	}
}
