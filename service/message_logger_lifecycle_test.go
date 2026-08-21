package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

type observedMessageLoggerDrain struct {
	manager         *MessageLogger
	subscriptionCtx context.Context
	drainEntered    chan struct{}
	releaseDrain    chan struct{}
	drainReturned   chan struct{}
	retryDone       <-chan struct{}
	attempts        atomic.Int32
	enteredOnce     sync.Once
	returnedOnce    sync.Once
}

type blockingMessageLoggerDrain struct {
	drainEntered chan struct{}
	releaseDrain <-chan struct{}
	drainErr     error
	attempts     atomic.Int32
	enteredOnce  sync.Once
}

func (s *blockingMessageLoggerDrain) Drain(context.Context) error {
	attempt := s.attempts.Add(1)
	s.enteredOnce.Do(func() { close(s.drainEntered) })
	if attempt == 1 && s.releaseDrain != nil {
		<-s.releaseDrain
	}
	return s.drainErr
}

func (s *observedMessageLoggerDrain) Drain(context.Context) error {
	s.attempts.Add(1)
	_, _, _ = s.manager.subjectInspectionState()
	if err := s.subscriptionCtx.Err(); err != nil {
		return errors.New("subscription authority canceled before Drain")
	}
	if s.retryDone != nil {
		select {
		case <-s.retryDone:
		default:
			return errors.New("subscription Drain began before retry joined")
		}
	}
	s.enteredOnce.Do(func() { close(s.drainEntered) })
	if s.releaseDrain != nil {
		<-s.releaseDrain
	}
	if err := s.subscriptionCtx.Err(); err != nil {
		return errors.New("subscription authority canceled during Drain")
	}
	s.returnedOnce.Do(func() { close(s.drainReturned) })
	return nil
}

func TestMessageLoggerNilAndCanceledLifecycleContextsPreserveAuthority(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	fake := newFakeLoggerSubscriber()
	ml.subscribe = fake.subscribe

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, ml.Start(nil))
	require.Error(t, ml.Stop(nil))
	require.Error(t, ml.Start(canceled))
	require.Error(t, ml.Stop(canceled))
	require.False(t, ml.lifecycleUsed)
	require.False(t, ml.lifecycleTerminal)
	require.Nil(t, ml.runtimeCancel)
	require.Empty(t, ml.subscriptions)

	require.NoError(t, ml.Start(t.Context()))
	require.Error(t, ml.Stop(canceled))
	require.True(t, ml.running)
	fake.mu.Lock()
	require.Zero(t, fake.drainAttempts["explicit.>"])
	fake.mu.Unlock()
	require.NoError(t, ml.Stop(t.Context()))
	require.Error(t, ml.Stop(nil))
	require.Error(t, ml.Stop(canceled))
	require.NoError(t, ml.Stop(t.Context()))
	require.ErrorContains(t, ml.Start(t.Context()), "already used")
}

func TestMessageLoggerStopFencesReconciliationAndDrainsBeforeCancel(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	subscription := &observedMessageLoggerDrain{
		manager:       ml,
		drainEntered:  make(chan struct{}),
		releaseDrain:  make(chan struct{}),
		drainReturned: make(chan struct{}),
	}
	ml.subscribe = func(
		ctx context.Context, _ string, _ func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		subscription.subscriptionCtx = ctx
		return subscription, nil
	}
	require.NoError(t, ml.Start(t.Context()))

	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(t.Context()) }()
	<-subscription.drainEntered
	require.NoError(t, subscription.subscriptionCtx.Err())
	reconcileErr := ml.reconcileSubjects(t.Context(), []string{"late.>"}, nil, nil)
	require.Error(t, reconcileErr)
	require.True(t, errs.IsTransient(reconcileErr))
	concurrentErr := ml.Stop(t.Context())
	require.Error(t, concurrentErr)
	require.True(t, errs.IsTransient(concurrentErr))
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before Drain completed: %v", err)
	default:
	}

	close(subscription.releaseDrain)
	<-subscription.drainReturned
	require.NoError(t, <-stopResult)
	require.ErrorIs(t, subscription.subscriptionCtx.Err(), context.Canceled)
	require.Equal(t, int32(1), subscription.attempts.Load())
	require.NoError(t, ml.Stop(t.Context()))
	require.Equal(t, int32(1), subscription.attempts.Load())
	require.ErrorContains(t, ml.Start(t.Context()), "already used")
}

func TestMessageLoggerStopJoinsRetryBeforeSubscriptionSnapshot(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"a.>", "b.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	retryTicks := make(chan time.Time)
	ml.retryAfter = func(time.Duration) <-chan time.Time { return retryTicks }
	subscription := &observedMessageLoggerDrain{
		manager:       ml,
		drainEntered:  make(chan struct{}),
		drainReturned: make(chan struct{}),
	}
	ml.subscribe = func(
		ctx context.Context, subject string, _ func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		if subject == "b.>" {
			return nil, errors.New("injected subscribe failure")
		}
		subscription.subscriptionCtx = ctx
		return subscription, nil
	}
	require.NoError(t, ml.Start(t.Context()))
	ml.lifecycleMu.Lock()
	subscription.retryDone = ml.retryDone
	ml.lifecycleMu.Unlock()
	require.NotNil(t, subscription.retryDone)

	require.NoError(t, ml.Stop(t.Context()))
	<-subscription.drainEntered
	<-subscription.drainReturned
	require.Equal(t, int32(1), subscription.attempts.Load())
}

func TestMessageLoggerDynamicRemovalDrainsExactlyOnceOutsideLocks(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"old.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	subscriptions := make(map[string]*observedMessageLoggerDrain)
	ml.subscribe = func(
		ctx context.Context, subject string, _ func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		subscription := &observedMessageLoggerDrain{
			manager:         ml,
			subscriptionCtx: ctx,
			drainEntered:    make(chan struct{}),
			drainReturned:   make(chan struct{}),
		}
		subscriptions[subject] = subscription
		return subscription, nil
	}
	require.NoError(t, ml.Start(t.Context()))
	require.NoError(t, ml.reconcileSubjects(t.Context(), []string{"new.>"}, nil, nil))
	require.Equal(t, int32(1), subscriptions["old.>"].attempts.Load())
	require.NoError(t, ml.reconcileSubjects(t.Context(), []string{"new.>"}, nil, nil))
	require.Equal(t, int32(1), subscriptions["old.>"].attempts.Load())
	require.NoError(t, ml.Stop(t.Context()))
	require.Equal(t, int32(1), subscriptions["new.>"].attempts.Load())
}

func TestMessageLoggerExpiredStopDoesNotDrainWhileReconciliationIsAdmitted(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"old.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	releaseDrain := make(chan struct{})
	subscription := &blockingMessageLoggerDrain{
		drainEntered: make(chan struct{}),
		releaseDrain: releaseDrain,
	}
	ml.subscribe = func(
		context.Context, string, func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		return subscription, nil
	}
	require.NoError(t, ml.Start(t.Context()))

	reconcileResult := make(chan error, 1)
	go func() { reconcileResult <- ml.reconcileSubjects(t.Context(), nil, nil, nil) }()
	<-subscription.drainEntered

	baseStopCtx, cancelStop := context.WithCancel(t.Context())
	stopCtx := &messageLoggerObservedContext{Context: baseStopCtx, observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(stopCtx) }()
	<-stopCtx.observed
	cancelStop()
	require.ErrorIs(t, <-stopResult, context.Canceled)

	close(releaseDrain)
	<-reconcileResult
	require.Equal(t, int32(1), subscription.attempts.Load(),
		"expired Stop must not race an admitted reconciliation for the same Drain")
	require.NoError(t, ml.Stop(t.Context()))
	require.Equal(t, int32(1), subscription.attempts.Load(), "terminal Stop must not replay Drain")
}

func TestMessageLoggerExpiredStopMakesLateAcquisitionSelfDrain(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"existing.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	existing := &blockingMessageLoggerDrain{drainEntered: make(chan struct{})}
	late := &blockingMessageLoggerDrain{drainEntered: make(chan struct{})}
	acquireEntered := make(chan struct{})
	releaseAcquire := make(chan struct{})
	ml.subscribe = func(
		_ context.Context, subject string, _ func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		if subject == "existing.>" {
			return existing, nil
		}
		close(acquireEntered)
		<-releaseAcquire
		return late, nil
	}
	require.NoError(t, ml.Start(t.Context()))

	reconcileResult := make(chan error, 1)
	go func() {
		reconcileResult <- ml.reconcileSubjects(
			t.Context(), []string{"existing.>", "late.>"}, nil, nil,
		)
	}()
	<-acquireEntered

	baseStopCtx, cancelStop := context.WithCancel(t.Context())
	stopCtx := &messageLoggerObservedContext{Context: baseStopCtx, observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(stopCtx) }()
	<-stopCtx.observed
	cancelStop()
	require.ErrorIs(t, <-stopResult, context.Canceled)
	require.Zero(t, existing.attempts.Load(), "expired admission wait must not snapshot existing handles")

	close(releaseAcquire)
	<-late.drainEntered
	<-reconcileResult
	require.Equal(t, int32(1), late.attempts.Load(), "late uncommitted handle must self-drain exactly once")
	ml.lifecycleMu.Lock()
	require.Empty(t, ml.subscriptions, "late handle must never publish after terminal Stop")
	ml.lifecycleMu.Unlock()
	require.NoError(t, ml.Stop(t.Context()))
	require.Zero(t, existing.attempts.Load())
	require.Equal(t, int32(1), late.attempts.Load())
}

func TestMessageLoggerConcurrentReconciliationsClaimObsoleteDrainOnce(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"old.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	releaseDrain := make(chan struct{})
	subscription := &blockingMessageLoggerDrain{
		drainEntered: make(chan struct{}),
		releaseDrain: releaseDrain,
	}
	ml.subscribe = func(
		context.Context, string, func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		return subscription, nil
	}
	require.NoError(t, ml.Start(t.Context()))

	firstResult := make(chan error, 1)
	go func() { firstResult <- ml.reconcileSubjects(t.Context(), nil, nil, nil) }()
	<-subscription.drainEntered
	secondResult := make(chan error, 1)
	go func() { secondResult <- ml.reconcileSubjects(t.Context(), nil, nil, nil) }()
	secondErr := <-secondResult
	close(releaseDrain)
	<-firstResult

	require.Error(t, secondErr, "the second reconciliation observes the claimed degraded record")
	require.Equal(t, int32(1), subscription.attempts.Load(),
		"concurrent reconciliations must atomically claim one native Drain")
	require.NoError(t, ml.Stop(t.Context()))
}

func TestMessageLoggerStartCannotCommitAfterExpiredStopWins(t *testing.T) {
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"late.>"}, MaxEntries: 10, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	acquireEntered := make(chan struct{})
	releaseAcquire := make(chan struct{})
	subscription := &blockingMessageLoggerDrain{
		drainEntered: make(chan struct{}),
		drainErr:     errors.New("injected late self-drain failure"),
	}
	var acquireAttempts atomic.Int32
	var acquireEnteredOnce sync.Once
	ml.subscribe = func(
		_ context.Context, _ string, _ func(context.Context, *nats.Msg),
	) (messageLoggerSubscription, error) {
		acquireAttempts.Add(1)
		acquireEnteredOnce.Do(func() { close(acquireEntered) })
		<-releaseAcquire
		return subscription, nil
	}

	startResult := make(chan error, 1)
	go func() { startResult <- ml.Start(t.Context()) }()
	<-acquireEntered

	baseStopCtx, cancelStop := context.WithCancel(t.Context())
	stopCtx := &messageLoggerObservedContext{Context: baseStopCtx, observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(stopCtx) }()
	<-stopCtx.observed
	cancelStop()
	require.ErrorIs(t, <-stopResult, context.Canceled)

	close(releaseAcquire)
	<-subscription.drainEntered
	startErr := <-startResult
	require.Error(t, startErr)
	require.True(t, errs.IsTransient(startErr), "Start losing terminal linearization must return a typed error")
	require.Equal(t, int32(1), acquireAttempts.Load(), "terminal Start must not launch reconciliation retry")
	require.Equal(t, int32(1), subscription.attempts.Load(), "late handle self-drains exactly once")

	ml.lifecycleMu.Lock()
	require.False(t, ml.running)
	require.True(t, ml.lifecycleTerminal)
	require.Nil(t, ml.retryCancel)
	require.Nil(t, ml.retryDone)
	require.Empty(t, ml.subscriptions)
	ml.lifecycleMu.Unlock()
	require.NoError(t, ml.Stop(t.Context()))
	require.Equal(t, int32(1), subscription.attempts.Load(), "repeat Stop must not replay Drain")
	require.ErrorContains(t, ml.Start(t.Context()), "already used")
}
