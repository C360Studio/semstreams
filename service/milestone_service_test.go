package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/natsclient"
)

// fakeStarter is a milestoneStarter test double. It records call counts and
// returns a canned (stop, err) so the MilestoneService lifecycle can be exercised
// without a live NATS connection.
type fakeStarter struct {
	startErr   error
	startCalls int
	stopCalls  int
	stop       func(context.Context) error
}

func (f *fakeStarter) Start(_ context.Context, _ *natsclient.Client, _ agentrun.StartConfig) (func(context.Context) error, error) {
	f.startCalls++
	if f.startErr != nil && f.stop == nil {
		return nil, f.startErr
	}
	stop := func(ctx context.Context) error {
		if f.stop != nil {
			return f.stop(ctx)
		}
		f.stopCalls++
		return nil
	}
	return stop, f.startErr
}

func TestMilestoneServiceStartRetainsPartialCleanupReturnedWithError(t *testing.T) {
	startErr := errors.New("second durable consumer failed")
	f := &fakeStarter{startErr: startErr}
	rollbackErr := errors.New("partial cleanup still pending")
	f.stop = func(context.Context) error {
		f.stopCalls++
		if f.stopCalls == 1 {
			return rollbackErr
		}
		return nil
	}
	svc := newTestMilestoneService(f)

	startResult := svc.Start(context.Background())
	require.ErrorIs(t, startResult, startErr)
	require.ErrorIs(t, startResult, rollbackErr)
	require.NotNil(t, svc.stop, "partial Start cleanup authority must remain reachable")
	require.Error(t, svc.Start(context.Background()), "cleanupPending rejects another Start")
	require.NoError(t, svc.Stop(context.Background()))
	require.Nil(t, svc.stop, "terminal cleanup clears the native stop function")
	require.NoError(t, svc.Stop(context.Background()))
	require.Equal(t, 2, f.stopCalls)
}

func TestMilestoneServiceStartRejectsInvalidContextWithoutConsumingAuthority(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() context.Context
	}{
		{name: "nil", ctx: func() context.Context { return nil }},
		{name: "pre-canceled", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &fakeStarter{}
			svc := newTestMilestoneService(starter)

			require.Error(t, svc.Start(tt.ctx()))
			require.False(t, svc.used, "invalid Start must preserve unused authority")
			require.Nil(t, svc.startDone)
			require.False(t, svc.running)
			require.False(t, svc.terminal)
			require.Equal(t, StatusStopped, svc.Status())
			require.Equal(t, 0, starter.startCalls)
		})
	}
}

func TestMilestoneServiceRunningStopFailureIsTerminalWithoutReplay(t *testing.T) {
	f := &fakeStarter{}
	stopErr := errors.New("native closure failed")
	f.stop = func(context.Context) error {
		f.stopCalls++
		return stopErr
	}
	svc := newTestMilestoneService(f)
	require.NoError(t, svc.Start(context.Background()))
	require.ErrorIs(t, svc.Stop(context.Background()), stopErr)
	require.NoError(t, svc.Stop(context.Background()))
	require.Equal(t, 1, f.stopCalls, "running Stop failure is terminal and never replayed")
}

type blockingMilestoneStarter struct {
	startEntered chan struct{}
	releaseStart chan struct{}
	stopCalled   chan struct{}
}

type observedMilestoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedMilestoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func (f *blockingMilestoneStarter) Start(
	context.Context, *natsclient.Client, agentrun.StartConfig,
) (func(context.Context) error, error) {
	close(f.startEntered)
	<-f.releaseStart
	return func(context.Context) error {
		close(f.stopCalled)
		return nil
	}, nil
}

func TestMilestoneServiceStopWaitsForStartFinalizationOutsideLocks(t *testing.T) {
	f := &blockingMilestoneStarter{
		startEntered: make(chan struct{}),
		releaseStart: make(chan struct{}),
		stopCalled:   make(chan struct{}),
	}
	svc := NewMilestoneService(f, nil, agentrun.StartConfig{StreamName: agentrun.AgentStreamName}, nil)
	startResult := make(chan error, 1)
	go func() { startResult <- svc.Start(t.Context()) }()
	<-f.startEntered
	stopCtx := &observedMilestoneContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- svc.Stop(stopCtx) }()
	<-stopCtx.observed

	require.True(t, svc.mu.TryLock(), "Stop must not hold the service lock while waiting for startDone")
	svc.mu.Unlock()
	select {
	case <-f.stopCalled:
		t.Fatal("subscriber cleanup ran before Start finalized")
	default:
	}
	close(f.releaseStart)
	require.NoError(t, <-startResult)
	require.NoError(t, <-stopResult)
	<-f.stopCalled
}

func newTestMilestoneService(f *fakeStarter) *MilestoneService {
	return NewMilestoneService(f, nil, agentrun.StartConfig{StreamName: agentrun.AgentStreamName}, nil)
}

// TestMilestoneService_Start_RunsAndStops covers the happy path AND the gh#246
// resourceless-boot path: the subscriber returns (stop, nil) — whether a real
// consumer or the stream-absent no-op — and the wrapper reports Running and calls
// the captured stop on Stop. A nil-error Start is exactly what keeps StartAll from
// aborting boot when there are no agentic components.
func TestMilestoneService_Start_RunsAndStops(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{}
	svc := newTestMilestoneService(f)

	require.NoError(t, svc.Start(context.Background()))
	assert.Equal(t, StatusRunning, svc.Status())
	assert.Equal(t, 1, f.startCalls)

	require.NoError(t, svc.Stop(context.Background()))
	assert.Equal(t, 1, f.stopCalls, "Stop must call the captured stop func exactly once")
}

// TestMilestoneService_Start_ErrorForwardedAndRollsBack proves a genuine
// consumer-start failure is forwarded (so StartAll aborts boot) AND BaseService status is rolled back so a failed
// Start does not leave the service phantom-Running.
func TestMilestoneService_Start_ErrorForwardedAndRollsBack(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{startErr: errors.New("consumer boom")}
	svc := newTestMilestoneService(f)

	err := svc.Start(context.Background())
	require.Error(t, err, "a genuine consumer-start failure must be forwarded")
	assert.Contains(t, err.Error(), "milestone subscriber start")
	assert.NotEqual(t, StatusRunning, svc.Status(), "a failed Start must roll back status — not phantom-Running")
}

// TestMilestoneService_ReentrancyGuard verifies a double-Start returns an error
// and does NOT re-invoke the subscriber.
func TestMilestoneService_ReentrancyGuard(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{}
	svc := newTestMilestoneService(f)

	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop(context.Background()) //nolint:errcheck

	require.Error(t, svc.Start(context.Background()), "double-Start must return an error")
	assert.Equal(t, 1, f.startCalls, "double-Start must NOT re-invoke subscriber.Start")
}

// TestMilestoneService_Stop_Idempotent verifies the captured stop func is called
// at most once across repeated Stop calls.
func TestMilestoneService_Stop_Idempotent(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{}
	svc := newTestMilestoneService(f)

	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))
	require.NoError(t, svc.Stop(context.Background()), "second Stop must be a clean no-op")
	assert.Equal(t, 1, f.stopCalls, "stop func must be called at most once")
}

// TestMilestoneService_StopBeforeStart_NoPanic verifies Stop on a never-started
// service is a clean no-op (nil stop func guarded).
func TestMilestoneService_StopBeforeStart_NoPanic(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{}
	svc := newTestMilestoneService(f)

	require.NoError(t, svc.Stop(context.Background()))
	assert.Equal(t, 0, f.stopCalls)
}

// TestMilestoneService_StopAfterFailedStart_NoPanic verifies Stop is clean after
// a forwarded Start error: no stop func was captured (subscriber.Start failed
// before returning one), so Stop is a no-op and does not panic.
func TestMilestoneService_StopAfterFailedStart_NoPanic(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{startErr: errors.New("boom")}
	svc := newTestMilestoneService(f)

	require.Error(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()), "Stop after a failed Start must be a clean no-op")
	assert.Equal(t, 0, f.stopCalls)
}
