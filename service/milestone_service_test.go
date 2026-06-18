package service

import (
	"context"
	"errors"
	"testing"
	"time"

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
}

func (f *fakeStarter) Start(_ context.Context, _ *natsclient.Client, _ agentrun.StartConfig) (func(), error) {
	f.startCalls++
	if f.startErr != nil {
		return nil, f.startErr
	}
	return func() { f.stopCalls++ }, nil
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

	require.NoError(t, svc.Stop(time.Second))
	assert.Equal(t, 1, f.stopCalls, "Stop must call the captured stop func exactly once")
}

// TestMilestoneService_Start_ErrorForwardedAndRollsBack proves the deliberate
// divergence from OwnershipService: a genuine consumer-start failure is forwarded
// (so StartAll aborts boot) AND BaseService status is rolled back so a failed
// Start does not leave the service phantom-Running.
func TestMilestoneService_Start_ErrorForwardedAndRollsBack(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{startErr: errors.New("consumer boom")}
	svc := newTestMilestoneService(f)

	err := svc.Start(context.Background())
	require.Error(t, err, "a genuine consumer-start failure must be forwarded (hard dependency, not R1-degraded)")
	assert.Contains(t, err.Error(), "milestone subscriber start")
	assert.NotEqual(t, StatusRunning, svc.Status(), "a failed Start must roll back status — not phantom-Running")
}

// TestMilestoneService_ReentrancyGuard verifies a double-Start returns an error
// and does NOT re-invoke the subscriber (BUG-CLASS guard, mirrors OwnershipService).
func TestMilestoneService_ReentrancyGuard(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{}
	svc := newTestMilestoneService(f)

	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop(time.Second) //nolint:errcheck

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
	require.NoError(t, svc.Stop(time.Second))
	require.NoError(t, svc.Stop(time.Second), "second Stop must be a clean no-op")
	assert.Equal(t, 1, f.stopCalls, "stop func must be called at most once")
}

// TestMilestoneService_StopBeforeStart_NoPanic verifies Stop on a never-started
// service is a clean no-op (nil stop func guarded).
func TestMilestoneService_StopBeforeStart_NoPanic(t *testing.T) {
	t.Parallel()
	f := &fakeStarter{}
	svc := newTestMilestoneService(f)

	require.NoError(t, svc.Stop(time.Second))
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
	require.NoError(t, svc.Stop(time.Second), "Stop after a failed Start must be a clean no-op")
	assert.Equal(t, 0, f.stopCalls)
}
