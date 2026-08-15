package service

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestMessageLoggerWildcardReconcilesRegistryCompleteSetsAndStops(t *testing.T) {
	natsClient := &natsclient.Client{}
	fake := newFakeLoggerSubscriber()
	registry := component.NewRegistry()
	serviceInstance, err := NewMessageLoggerService(
		json.RawMessage(`{"monitor_subjects":["*","operator.>"]}`),
		&Dependencies{NATSClient: natsClient, ComponentRegistry: registry},
	)
	require.NoError(t, err)
	ml := serviceInstance.(*MessageLogger)
	ml.subscribe = fake.subscribe
	require.Nil(t, ml.observerCancel, "constructor must not attach the Registry observer")
	require.Empty(t, ml.subscriptions, "constructor must not subscribe")

	require.NoError(t, ml.Start(context.Background()))
	waitForLoggerSubjects(t, ml, []string{"operator.>"})

	first := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "worker"},
		outputs: []component.Port{{
			Name: "events", Direction: component.DirectionOutput,
			Config: component.NATSPort{Subject: "events.first"},
		}},
	}
	admitTestRegistryComponent(t, registry, "worker", first)
	waitForLoggerSubjects(t, ml, []string{"events.first", "operator.>"})

	second := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "worker"},
		outputs: []component.Port{{
			Name: "events", Direction: component.DirectionOutput,
			Config: component.JetStreamPort{Subjects: []string{"events.second"}},
		}},
	}
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "worker-v2", Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return second, nil
		},
	}))
	_, err = registry.ReplaceComponent(
		componentadmission.Access{}, "worker", types.ComponentConfig{
			Name: "worker-v2", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{}`),
		}, component.Dependencies{NATSClient: natsClient}, nil)
	require.NoError(t, err)
	waitForLoggerSubjects(t, ml, []string{"events.second", "operator.>"})

	registry.UnregisterInstance("worker")
	waitForLoggerSubjects(t, ml, []string{"operator.>"})
	require.NoError(t, ml.Stop(context.Background()))
	require.Nil(t, ml.observerCancel)
	require.Empty(t, ml.subscriptions)
	require.Empty(t, ml.resolvedSubjects)
}

func TestMessageLoggerExplicitModeNeverAttachesRegistryObserver(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 1000, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	ml.subscribe = fake.subscribe
	require.NoError(t, ml.Start(context.Background()))
	require.Nil(t, ml.observerCancel)
	require.Nil(t, ml.observerDone)
	waitForLoggerSubjects(t, ml, []string{"explicit.>"})
	require.NoError(t, ml.Stop(context.Background()))
}

func TestMessageLoggerAcceptedContainmentCapturesOnceAndReportsResolution(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	registry := component.NewRegistry()
	componentWithOverlap := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "governance"},
		outputs: []component.Port{
			{Name: "broad", Direction: component.DirectionOutput, Config: component.NATSPort{Subject: "agent.toolcall.proposed.>"}},
			{Name: "covered", Direction: component.DirectionOutput, Config: component.NATSPort{Subject: "agent.toolcall.proposed.*"}},
		},
	}
	admitTestRegistryComponent(t, registry, "governance", componentWithOverlap)
	serviceInstance, err := NewMessageLoggerService(
		json.RawMessage(`{"monitor_subjects":["*"]}`),
		&Dependencies{NATSClient: &natsclient.Client{}, ComponentRegistry: registry},
	)
	require.NoError(t, err)
	ml := serviceInstance.(*MessageLogger)
	ml.subscribe = fake.subscribe
	require.NoError(t, ml.Start(context.Background()))
	defer ml.Stop(context.Background())
	waitForLoggerSubjects(t, ml, []string{"agent.toolcall.proposed.>"})

	fake.deliver(t, "agent.toolcall.proposed.>", "agent.toolcall.proposed.loop", []byte(`{}`))
	require.Equal(t, int64(1), ml.stats.totalMessages.Load())
	_, overlaps := ml.subjectInspection()
	require.Equal(t, []subjectOverlap{{
		Broader: "agent.toolcall.proposed.>", Covered: "agent.toolcall.proposed.*",
		Resolution: "covered subscription omitted",
	}}, overlaps)
}

func TestMessageLoggerRetriesSubscribeFailureAndClearsDegradedState(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	fake.subscribeFailures["explicit.>"] = 1
	retryTicks := make(chan time.Time, 1)
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 1000, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	ml.subscribe = fake.subscribe
	ml.retryAfter = func(time.Duration) <-chan time.Time { return retryTicks }
	require.NoError(t, ml.Start(context.Background()))
	waitForLoggerReconciliation(t, ml, nil, true)

	retryTicks <- time.Now()
	waitForLoggerReconciliation(t, ml, []string{"explicit.>"}, false)
	require.NoError(t, ml.Stop(context.Background()))
}

func TestMessageLoggerRetainsFailedOverlapUnsubscribeWithoutDuplicateThenRetries(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	retryTicks := make(chan time.Time, 1)
	registry := component.NewRegistry()
	covered := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "governance"},
		outputs: []component.Port{{
			Name: "covered", Direction: component.DirectionOutput,
			Config: component.NATSPort{Subject: "agent.toolcall.proposed.*"},
		}},
	}
	admitTestRegistryComponent(t, registry, "governance", covered)
	serviceInstance, err := NewMessageLoggerService(
		json.RawMessage(`{"monitor_subjects":["*"]}`),
		&Dependencies{NATSClient: &natsclient.Client{}, ComponentRegistry: registry},
	)
	require.NoError(t, err)
	ml := serviceInstance.(*MessageLogger)
	ml.subscribe = fake.subscribe
	ml.retryAfter = func(time.Duration) <-chan time.Time { return retryTicks }
	require.NoError(t, ml.Start(context.Background()))
	waitForLoggerReconciliation(t, ml, []string{"agent.toolcall.proposed.*"}, false)

	broadAndCovered := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "governance"},
		outputs: []component.Port{
			{Name: "broad", Direction: component.DirectionOutput,
				Config: component.NATSPort{Subject: "agent.toolcall.proposed.>"}},
			{Name: "covered", Direction: component.DirectionOutput,
				Config: component.NATSPort{Subject: "agent.toolcall.proposed.*"}},
		},
	}
	require.NoError(t, registry.RegisterWithConfig(component.RegistrationConfig{
		Name: "governance-v2", Type: "processor",
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return broadAndCovered, nil
		},
	}))
	fake.mu.Lock()
	fake.unsubscribeFailures["agent.toolcall.proposed.*"] = 1
	fake.mu.Unlock()
	_, err = registry.ReplaceComponent(
		componentadmission.Access{}, "governance", types.ComponentConfig{
			Name: "governance-v2", Type: types.ComponentTypeProcessor, Enabled: true, Config: json.RawMessage(`{}`),
		}, component.Dependencies{NATSClient: &natsclient.Client{}}, nil)
	require.NoError(t, err)
	waitForLoggerReconciliation(t, ml, []string{"agent.toolcall.proposed.*"}, true)
	fake.mu.Lock()
	require.Zero(t, fake.subscribeAttempts["agent.toolcall.proposed.>"],
		"broader subscription must wait while the covered subscription remains active")
	fake.mu.Unlock()

	retryTicks <- time.Now()
	waitForLoggerReconciliation(t, ml, []string{"agent.toolcall.proposed.>"}, false)
	require.NoError(t, ml.Stop(context.Background()))
}

func TestMessageLoggerStopRetainsAndReplaysFirstTeardownFailure(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 1000, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	ml.subscribe = fake.subscribe
	require.NoError(t, ml.Start(context.Background()))
	waitForLoggerSubjects(t, ml, []string{"explicit.>"})
	fake.mu.Lock()
	fake.unsubscribeFailures["explicit.>"] = 1
	fake.mu.Unlock()

	firstErr := ml.Stop(context.Background())
	require.ErrorContains(t, firstErr, "unsubscribe explicit.>")
	waitForLoggerReconciliation(t, ml, []string{"explicit.>"}, true)
	secondErr := ml.Stop(context.Background())
	require.EqualError(t, secondErr, firstErr.Error())
	waitForLoggerReconciliation(t, ml, []string{"explicit.>"}, true)
	fake.mu.Lock()
	require.Equal(t, 1, fake.unsubscribeAttempts["explicit.>"], "genuine teardown failure must not be retried")
	fake.mu.Unlock()
}

func TestMessageLoggerStopDeadlineCanResumeSameGeneration(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 1000, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	ml.subscribe = fake.subscribe
	require.NoError(t, ml.Start(context.Background()))
	waitForLoggerSubjects(t, ml, []string{"explicit.>"})

	retryDone := make(chan struct{})
	ml.lifecycleMu.Lock()
	ml.retryDone = retryDone
	ml.lifecycleMu.Unlock()
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	cancelFirst()
	require.ErrorIs(t, ml.Stop(firstCtx), context.Canceled)
	fake.mu.Lock()
	require.Zero(t, fake.unsubscribeAttempts["explicit.>"], "expired Stop must not begin terminal teardown")
	fake.mu.Unlock()

	close(retryDone)
	require.NoError(t, ml.Stop(context.Background()))
	fake.mu.Lock()
	require.Equal(t, 1, fake.unsubscribeAttempts["explicit.>"])
	fake.mu.Unlock()
}

func TestMessageLoggerStartStopTransitionDoesNotMissInstalledObserverOrSubscription(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	fake.subscribeEntered = make(chan struct{})
	fake.releaseSubscribe = make(chan struct{})
	serviceInstance, err := NewMessageLoggerService(
		json.RawMessage(`{"monitor_subjects":["*","operator.>"]}`),
		&Dependencies{NATSClient: &natsclient.Client{}, ComponentRegistry: component.NewRegistry()},
	)
	require.NoError(t, err)
	ml := serviceInstance.(*MessageLogger)
	ml.subscribe = fake.subscribe
	startResult := make(chan error, 1)
	go func() { startResult <- ml.Start(context.Background()) }()
	<-fake.subscribeEntered
	require.False(t, ml.transitionMu.TryLock(), "Start must retain the transition lock through handle installation")

	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(context.Background()) }()
	close(fake.releaseSubscribe)
	require.NoError(t, <-startResult)
	require.NoError(t, <-stopResult)
	require.Nil(t, ml.observerCancel)
	require.Nil(t, ml.observerDone)
	require.Nil(t, ml.retryCancel)
	require.Nil(t, ml.retryDone)
	require.Empty(t, ml.subscriptions)
	fake.mu.Lock()
	require.Empty(t, fake.handlers)
	fake.mu.Unlock()
}

type fakeLoggerSubscriber struct {
	mu                  sync.Mutex
	handlers            map[string]func(context.Context, *nats.Msg)
	subscribeFailures   map[string]int
	unsubscribeFailures map[string]int
	subscribeAttempts   map[string]int
	unsubscribeAttempts map[string]int
	subscribeEntered    chan struct{}
	releaseSubscribe    chan struct{}
	enteredOnce         sync.Once
}

func newFakeLoggerSubscriber() *fakeLoggerSubscriber {
	return &fakeLoggerSubscriber{
		handlers:            make(map[string]func(context.Context, *nats.Msg)),
		subscribeFailures:   make(map[string]int),
		unsubscribeFailures: make(map[string]int),
		subscribeAttempts:   make(map[string]int),
		unsubscribeAttempts: make(map[string]int),
	}
}

func (f *fakeLoggerSubscriber) subscribe(
	_ context.Context, subject string, handler func(context.Context, *nats.Msg),
) (messageLoggerSubscription, error) {
	if f.subscribeEntered != nil {
		f.enteredOnce.Do(func() { close(f.subscribeEntered) })
	}
	if f.releaseSubscribe != nil {
		<-f.releaseSubscribe
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribeAttempts[subject]++
	if f.subscribeFailures[subject] > 0 {
		f.subscribeFailures[subject]--
		return nil, errors.New("injected subscribe failure")
	}
	f.handlers[subject] = handler
	return &fakeLoggerSubscription{owner: f, subject: subject}, nil
}

type fakeLoggerSubscription struct {
	owner   *fakeLoggerSubscriber
	subject string
}

func (s *fakeLoggerSubscription) Unsubscribe() error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	s.owner.unsubscribeAttempts[s.subject]++
	if s.owner.unsubscribeFailures[s.subject] > 0 {
		s.owner.unsubscribeFailures[s.subject]--
		return errors.New("injected unsubscribe failure")
	}
	delete(s.owner.handlers, s.subject)
	return nil
}

func (f *fakeLoggerSubscriber) deliver(t *testing.T, pattern, subject string, data []byte) {
	t.Helper()
	f.mu.Lock()
	handler := f.handlers[pattern]
	f.mu.Unlock()
	require.NotNil(t, handler, "subscription handler for %q", pattern)
	handler(context.Background(), &nats.Msg{Subject: subject, Data: data})
}

func waitForLoggerSubjects(t *testing.T, ml *MessageLogger, want []string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := ml.subjectInspection()
		if slices.Equal(got, want) {
			return
		}
		runtime.Gosched()
	}
	got, _ := ml.subjectInspection()
	t.Fatalf("resolved logger subjects = %v, want %v", got, want)
}

func waitForLoggerReconciliation(t *testing.T, ml *MessageLogger, want []string, degraded bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _, reconcileError := ml.subjectInspectionState()
		if slices.Equal(got, want) && (reconcileError != "") == degraded {
			return
		}
		runtime.Gosched()
	}
	got, _, reconcileError := ml.subjectInspectionState()
	t.Fatalf("logger reconciliation subjects=%v error=%q, want subjects=%v degraded=%t",
		got, reconcileError, want, degraded)
}
