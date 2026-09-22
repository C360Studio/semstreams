package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/natsclient"
)

// latchedStarter is a milestoneStarter that also reports a latched delivery
// fatal, the shape the production *agentrun.MilestoneSubscriber presents.
// The subscriber's own latch is driven by a NATS delivery callback, so the
// service-side read is exercised here and the production type's conformance is
// pinned separately by TestMilestoneServiceHealthReadsTheProductionSubscriber.
type latchedStarter struct {
	fatal error
}

func (s *latchedStarter) Start(
	context.Context, *natsclient.Client, agentrun.StartConfig,
) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}

func (s *latchedStarter) DeliveryFatal() error { return s.fatal }

// milestoneSubStatus pulls the milestone entry out of an aggregated /health
// body along with the response code, or fails: an absent entry would otherwise
// read as "not unhealthy".
func milestoneSubStatus(t *testing.T, manager *Manager) (health.Status, int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	manager.handleSystemHealth(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	var payload health.Status
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	for _, sub := range payload.SubStatuses {
		if sub.Component == "milestone" {
			return sub, recorder.Code
		}
	}
	t.Fatalf("/health carried no milestone sub-status: %s", recorder.Body.String())
	return health.Status{}, recorder.Code
}

// TestMilestoneServiceHealthReportsDeliveryFatal is the § 2.7 health row: the
// latched cause reaches an operator through the real /health aggregate, not
// just through a method nobody calls.
//
// Both halves matter. A running service whose lanes still own their deliveries
// must stay healthy, or the signal is noise; a running service whose lane
// latched must report unhealthy AND carry the cause, because the process is
// otherwise indistinguishable from an idle one — consumers still bound, no
// milestones arriving.
func TestMilestoneServiceHealthReportsDeliveryFatal(t *testing.T) {
	t.Run("owned lanes stay healthy", func(t *testing.T) {
		starter := &latchedStarter{}
		svc := NewMilestoneService(starter, nil, agentrun.StartConfig{StreamName: agentrun.AgentStreamName}, nil)
		require.NoError(t, svc.Start(context.Background()))
		t.Cleanup(func() { _ = svc.Stop(context.Background()) })

		manager := NewServiceManager(NewServiceRegistry())
		require.NoError(t, manager.RegisterInstance("milestone", svc))

		status, code := milestoneSubStatus(t, manager)
		assert.True(t, status.IsHealthy(), "an owned lane must not report a delivery fatal: %s", status.Message)
		assert.Equal(t, http.StatusOK, code, "an owned lane must not fail the aggregate")
	})

	t.Run("a latched lane reports unhealthy with its cause", func(t *testing.T) {
		cause := errors.New("delivery metadata unavailable")
		starter := &latchedStarter{}
		svc := NewMilestoneService(starter, nil, agentrun.StartConfig{StreamName: agentrun.AgentStreamName}, nil)
		require.NoError(t, svc.Start(context.Background()))
		t.Cleanup(func() { _ = svc.Stop(context.Background()) })

		manager := NewServiceManager(NewServiceRegistry())
		require.NoError(t, manager.RegisterInstance("milestone", svc))
		before, _ := milestoneSubStatus(t, manager)
		require.True(t, before.IsHealthy(), "precondition: healthy before the latch")

		starter.fatal = cause

		status, code := milestoneSubStatus(t, manager)
		assert.False(t, status.IsHealthy(), "a lane that lost delivery ownership must not read as healthy")
		assert.Equal(t, http.StatusServiceUnavailable, code, "the aggregate must carry the verdict to the caller")
		assert.Contains(t, status.Message, cause.Error(),
			"the verdict must carry the cause an operator acts on")
	})
}

// TestMilestoneServiceHealthReadsTheProductionSubscriber pins the type
// assertion Health() makes. Health() falls back to the base status when the
// starter does not report, so a renamed or resigned DeliveryFatal on the
// production subscriber would silently turn the whole signal off; this fails
// loudly instead.
func TestMilestoneServiceHealthReadsTheProductionSubscriber(t *testing.T) {
	subscriber := agentrun.NewMilestoneSubscriber(nil, nil, "acme", "ops", nil)
	svc := NewMilestoneService(subscriber, nil, agentrun.StartConfig{StreamName: agentrun.AgentStreamName}, nil)

	_, ok := svc.subscriber.(deliveryFatalReporter)
	require.True(t, ok, "*agentrun.MilestoneSubscriber no longer reports DeliveryFatal to Health()")
}
