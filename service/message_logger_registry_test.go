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
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestMessageLoggerExplicitModeUsesOnlyConfiguredSubjects(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 1000, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	ml.subscribe = fake.subscribe
	require.NoError(t, ml.Start(context.Background()))
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

func TestMessageLoggerCompletedStopDoesNotReplayTeardownFailure(t *testing.T) {
	fake := newFakeLoggerSubscriber()
	ml, err := NewMessageLogger(&MessageLoggerConfig{
		MonitorSubjects: []string{"explicit.>"}, MaxEntries: 1000, SampleRate: 1,
	}, &natsclient.Client{})
	require.NoError(t, err)
	ml.subscribe = fake.subscribe
	require.NoError(t, ml.Start(context.Background()))
	waitForLoggerSubjects(t, ml, []string{"explicit.>"})
	fake.mu.Lock()
	fake.drainFailures["explicit.>"] = 1
	fake.mu.Unlock()

	firstErr := ml.Stop(context.Background())
	require.ErrorContains(t, firstErr, "drain explicit.>")
	secondErr := ml.Stop(context.Background())
	require.NoError(t, secondErr)
	fake.mu.Lock()
	require.Equal(t, 1, fake.drainAttempts["explicit.>"], "genuine teardown failure must not be retried")
	fake.mu.Unlock()
}

func TestMessageLoggerStopDeadlineIsTerminalWithoutReplay(t *testing.T) {
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
	baseCtx, cancelFirst := context.WithCancel(t.Context())
	firstCtx := &messageLoggerObservedContext{Context: baseCtx, observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(firstCtx) }()
	<-firstCtx.observed
	cancelFirst()
	require.ErrorIs(t, <-stopResult, context.Canceled)
	fake.mu.Lock()
	require.Equal(t, 1, fake.drainAttempts["explicit.>"], "terminal deadline still attempts exact drain once")
	fake.mu.Unlock()

	close(retryDone)
	require.NoError(t, ml.Stop(context.Background()))
	fake.mu.Lock()
	require.Equal(t, 1, fake.drainAttempts["explicit.>"], "completed Stop must not replay teardown")
	fake.mu.Unlock()
}

func TestMessageLoggerStartStopTransitionDoesNotMissInstalledSubscription(t *testing.T) {
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

	stopCtx := &messageLoggerObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- ml.Stop(stopCtx) }()
	<-stopCtx.observed
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before the admitted acquisition published its handle: %v", err)
	default:
	}
	close(fake.releaseSubscribe)
	require.NoError(t, <-startResult)
	require.NoError(t, <-stopResult)
	require.Nil(t, ml.retryCancel)
	require.Nil(t, ml.retryDone)
	require.Empty(t, ml.subscriptions)
	fake.mu.Lock()
	require.Empty(t, fake.handlers)
	require.Equal(t, 1, fake.drainAttempts["operator.>"])
	fake.mu.Unlock()
}

type messageLoggerObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *messageLoggerObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type fakeLoggerSubscriber struct {
	mu                sync.Mutex
	handlers          map[string]func(context.Context, *nats.Msg)
	subscribeFailures map[string]int
	drainFailures     map[string]int
	subscribeAttempts map[string]int
	drainAttempts     map[string]int
	subscribeEntered  chan struct{}
	releaseSubscribe  chan struct{}
	enteredOnce       sync.Once
}

func newFakeLoggerSubscriber() *fakeLoggerSubscriber {
	return &fakeLoggerSubscriber{
		handlers:          make(map[string]func(context.Context, *nats.Msg)),
		subscribeFailures: make(map[string]int),
		drainFailures:     make(map[string]int),
		subscribeAttempts: make(map[string]int),
		drainAttempts:     make(map[string]int),
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

func (s *fakeLoggerSubscription) Drain(context.Context) error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	s.owner.drainAttempts[s.subject]++
	if s.owner.drainFailures[s.subject] > 0 {
		s.owner.drainFailures[s.subject]--
		return errors.New("injected drain failure")
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
