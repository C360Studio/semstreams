//go:build integration

package weatherstation

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

type observingStopContext struct {
	context.Context
	onDone sync.Once
	seen   chan struct{}
}

func (c *observingStopContext) Done() <-chan struct{} {
	c.onDone.Do(func() { close(c.seen) })
	return c.Context.Done()
}

func TestWeatherStopDrainsBlockedCoreCallbackBeforeCancel(t *testing.T) {
	testClient := natsclient.NewTestClient(t)
	defer testClient.Terminate()
	runCtx, cancelRun := context.WithCancel(t.Context())
	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	sub, err := testClient.Client.Subscribe(runCtx, "s1.weather.input", func(context.Context, *nats.Msg) {
		close(callbackEntered)
		<-releaseCallback
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	owner := &Component{
		lifecycleUsed: true,
		running:       true,
		cancel: func() {
			cancelOnce.Do(func() {
				cancelRun()
				close(canceled)
			})
		},
		subscriptions: []*natsclient.Subscription{sub},
	}
	if err := testClient.Client.Publish(t.Context(), "s1.weather.input", []byte("weather")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-callbackEntered

	stopCtx := &observingStopContext{Context: t.Context(), seen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(stopCtx) }()
	<-stopCtx.seen
	select {
	case <-canceled:
		t.Fatal("runtime canceled before blocked core callback completed")
	default:
	}
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before blocked callback completed: %v", err)
	default:
	}
	close(releaseCallback)
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-canceled
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
}

func TestWeatherRunningDeadlineIsTerminalWithoutDrainReplay(t *testing.T) {
	testClient := natsclient.NewTestClient(t)
	defer testClient.Terminate()
	runCtx, cancelRun := context.WithCancel(t.Context())
	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	sub, err := testClient.Client.Subscribe(runCtx, "s1.weather.deadline", func(context.Context, *nats.Msg) {
		close(callbackEntered)
		<-releaseCallback
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	owner := &Component{
		lifecycleUsed: true,
		running:       true,
		cancel: func() {
			cancelOnce.Do(func() {
				cancelRun()
				close(canceled)
			})
		},
		subscriptions: []*natsclient.Subscription{sub},
	}
	if err := testClient.Client.Publish(t.Context(), "s1.weather.deadline", []byte("weather")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-callbackEntered

	baseStopCtx, expire := context.WithCancel(t.Context())
	stopCtx := &observingStopContext{Context: baseStopCtx, seen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(stopCtx) }()
	<-stopCtx.seen
	expire()
	if err := <-stopResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop error = %v, want context.Canceled", err)
	}
	<-canceled
	if !owner.terminal || len(owner.subscriptions) != 0 {
		t.Fatalf("deadline Stop did not terminalize: terminal=%v subscriptions=%d", owner.terminal, len(owner.subscriptions))
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
	close(releaseCallback)
}

func TestWeatherFailedCleanupRetainsCoreHandleForStopRetry(t *testing.T) {
	testClient := natsclient.NewTestClient(t)
	defer testClient.Terminate()
	runCtx, cancelRun := context.WithCancel(t.Context())
	sub, err := testClient.Client.Subscribe(runCtx, "s1.weather.retry", func(context.Context, *nats.Msg) {})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	owner := &Component{
		lifecycleUsed:  true,
		cleanupPending: true,
		cancel:         cancelRun,
		subscriptions:  []*natsclient.Subscription{sub},
	}
	expired, expire := context.WithCancel(t.Context())
	expire()
	if err := owner.cleanupFailedStart(expired); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired failed-Start cleanup error = %v, want context.Canceled", err)
	}
	if len(owner.subscriptions) != 1 || owner.subscriptions[0] != sub || owner.cancel == nil {
		t.Fatal("failed cleanup discarded exact core or cancellation authority")
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start while cleanup pending error = %v, want ErrAlreadyStarted", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
}

func TestWeatherProductionStartStopOverlapRollsBackExactCoreHandle(t *testing.T) {
	testClient := natsclient.NewTestClient(t)
	defer testClient.Terminate()
	secondEntered := make(chan struct{})
	releaseSecond := make(chan struct{})
	sentinel := errors.New("second core acquisition failed")
	var first *natsclient.Subscription
	var firstCtx context.Context
	var calls atomic.Int32
	owner := &Component{
		name: "weather-station-processor", subjects: []string{"s1.weather.first", "s1.weather.second"},
		natsClient: testClient.Client, logger: slog.Default(),
		subscribeInput: func(
			ctx context.Context, subject string, handler func(context.Context, *nats.Msg),
		) (*natsclient.Subscription, error) {
			if calls.Add(1) == 1 {
				firstCtx = ctx
				var err error
				first, err = testClient.Client.Subscribe(ctx, subject, handler)
				return first, err
			}
			close(secondEntered)
			<-releaseSecond
			return nil, sentinel
		},
	}
	startResult := make(chan error, 1)
	go func() { startResult <- owner.Start(t.Context()) }()
	<-secondEntered
	owner.lifecycleMu.Lock()
	if len(owner.subscriptions) != 1 || owner.subscriptions[0] != first {
		owner.lifecycleMu.Unlock()
		t.Fatal("first exact core handle was not published before second acquisition")
	}
	owner.lifecycleMu.Unlock()

	stopCtx := &observingStopContext{Context: t.Context(), seen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- owner.Stop(stopCtx) }()
	<-stopCtx.seen
	owner.lifecycleMu.Lock()
	owner.lifecycleMu.Unlock()
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before Start completed rollback: %v", err)
	default:
	}
	close(releaseSecond)
	if err := <-startResult; !errors.Is(err, sentinel) {
		t.Fatalf("Start error = %v, want sentinel", err)
	}
	if err := <-stopResult; err != nil {
		t.Fatalf("overlapping Stop: %v", err)
	}
	if !errors.Is(firstCtx.Err(), context.Canceled) {
		t.Fatalf("first callback authority after rollback = %v, want context.Canceled", firstCtx.Err())
	}
}
