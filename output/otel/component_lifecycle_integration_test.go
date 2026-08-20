//go:build integration

package otel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
)

type observedOTELStopContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedOTELStopContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestIntegrationStopWaitsForBlockedStartOutsideLifecycleLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(natsclient.TestStreamConfig{
		Name: "AGENT", Subjects: []string{"agent.>", "tool.result.>"},
	}))
	c, _, _ := newLifecycleIntegrationComponent(t, tc.Client, "ot1-start-overlap")
	observeEntered := make(chan struct{})
	releaseObserve := make(chan struct{})
	cleaned := make(chan struct{}, len(c.inputs))
	var blockOnce sync.Once
	c.observePolicy = func(
		context.Context,
		natsclient.PortConsumerContext,
		jetstream.ConsumerConfig,
		jetstream.Consumer,
	) (func(), error) {
		blockOnce.Do(func() {
			close(observeEntered)
			<-releaseObserve
		})
		return func() { cleaned <- struct{}{} }, nil
	}

	startResult := make(chan error, 1)
	go func() { startResult <- c.Start(ctx) }()
	<-observeEntered
	stopCtx := &observedOTELStopContext{Context: ctx, observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(stopCtx) }()
	<-stopCtx.observed
	require.True(t, c.lifecycleMu.TryLock(), "Stop must wait for startDone outside lifecycleMu")
	c.lifecycleMu.Unlock()
	require.False(t, c.Health().Healthy, "blocked Start must not publish running health before commit")
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before blocked Start finalized: %v", err)
	default:
	}

	close(releaseObserve)
	require.NoError(t, <-startResult)
	require.NoError(t, <-stopResult)
	waitOTELWorkers(t, ctx, cleaned, len(c.inputs))
}

func TestIntegrationPartialObservationFailureReleasesClaimsBeforeStartReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(natsclient.TestStreamConfig{
		Name: "AGENT", Subjects: []string{"agent.>", "tool.result.>"},
	}))
	failing, _, _ := newLifecycleIntegrationComponent(t, tc.Client, "ot1-partial")
	cleanupCalled := make(chan struct{}, 1)
	observation := 0
	wantErr := errors.New("second observation failed")
	failing.observePolicy = func(
		context.Context,
		natsclient.PortConsumerContext,
		jetstream.ConsumerConfig,
		jetstream.Consumer,
	) (func(), error) {
		observation++
		if observation == 2 {
			return nil, wantErr
		}
		return func() { cleanupCalled <- struct{}{} }, nil
	}

	require.ErrorIs(t, failing.Start(ctx), wantErr)
	select {
	case <-cleanupCalled:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Error(t, failing.Start(ctx), "failed Start consumes the one-shot instance")

	reacquired, entered, exited := newLifecycleIntegrationComponent(t, tc.Client, "ot1-partial")
	require.NoError(t, reacquired.Start(ctx), "precommit failure must release every exact claim")
	waitOTELWorkers(t, ctx, entered, len(reacquired.inputs))
	require.NoError(t, reacquired.Stop(ctx))
	waitOTELWorkers(t, ctx, exited, len(reacquired.inputs))
}

func TestIntegrationDuplicateDurableRejectsAcrossClientsWithoutStoppingIncumbent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(natsclient.TestStreamConfig{
		Name: "AGENT", Subjects: []string{"agent.>", "tool.result.>"},
	}))
	replica, err := natsclient.NewClient(tc.URL, natsclient.WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, replica.Connect(ctx))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		require.NoError(t, replica.Close(cleanupCtx))
	})

	incumbent, incumbentEntered, incumbentExited := newLifecycleIntegrationComponent(
		t, tc.Client, "ot1-cross-client",
	)
	require.NoError(t, incumbent.Start(ctx))
	waitOTELWorkers(t, ctx, incumbentEntered, len(incumbent.inputs))

	duplicate, _, _ := newLifecycleIntegrationComponent(t, replica, "ot1-cross-client")
	duplicateErr := duplicate.Start(ctx)
	require.Error(t, duplicateErr)
	require.True(t, semerrs.IsInvalid(duplicateErr))
	select {
	case <-incumbentExited:
		t.Fatal("duplicate rejection stopped the incumbent pull loop")
	default:
	}

	require.NoError(t, incumbent.Stop(ctx))
	waitOTELWorkers(t, ctx, incumbentExited, len(incumbent.inputs))
	require.Error(t, incumbent.Start(ctx), "same component instance must reject restart")

	reacquired, reacquiredEntered, reacquiredExited := newLifecycleIntegrationComponent(
		t, replica, "ot1-cross-client",
	)
	require.NoError(t, reacquired.Start(ctx), "completed Stop must release the exact local claims")
	waitOTELWorkers(t, ctx, reacquiredEntered, len(reacquired.inputs))
	require.NoError(t, reacquired.Stop(ctx))
	waitOTELWorkers(t, ctx, reacquiredExited, len(reacquired.inputs))
}

func newLifecycleIntegrationComponent(
	t *testing.T, client *natsclient.Client, suffix string,
) (*Component, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ConsumerNameSuffix = suffix
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	discoverable, err := NewComponent(raw, component.Dependencies{NATSClient: client})
	require.NoError(t, err)
	c := discoverable.(*Component)
	require.NoError(t, c.Initialize())
	c.SetExporter(&MockExporter{})
	entered := make(chan struct{}, len(c.inputs))
	exited := make(chan struct{}, len(c.inputs))
	c.consumeFrom = func(fetchCtx context.Context, _ jetstream.Consumer) {
		entered <- struct{}{}
		<-fetchCtx.Done()
		exited <- struct{}{}
	}
	return c, entered, exited
}

func waitOTELWorkers(t *testing.T, ctx context.Context, events <-chan struct{}, count int) {
	t.Helper()
	for range count {
		select {
		case <-events:
		case <-ctx.Done():
			t.Fatalf("wait for %d OTEL pull workers: %v", count, ctx.Err())
		}
	}
}
