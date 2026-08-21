package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopSpyService is a Service whose Stop returns a configurable error and counts
// invocations, for exercising StopAll idempotency and aggregation (gh#520).
type stopSpyService struct {
	MockService
	stopErr   error
	stopCalls int
}

func (s *stopSpyService) Stop(context.Context) error {
	s.stopCalls++
	return s.stopErr
}

// TestServiceManager_StopAll_Idempotency covers the coordinated-shutdown contract:
// a service whose Stop exactly completed is clean success; StatusStopping alone is
// not completion evidence. Genuine failures surface and a clean shutdown returns nil (gh#520).
func TestServiceManager_StopAll_Idempotency(t *testing.T) {
	t.Run("already-stopped service is not aggregated as fatal", func(t *testing.T) {
		manager := createTestServiceManager(ManagerConfig{}, nil)

		already := &stopSpyService{
			MockService: MockService{name: "already", status: StatusStopped, healthy: false},
			stopErr:     ErrAlreadyStopped,
		}
		clean := &stopSpyService{
			MockService: MockService{name: "clean", status: StatusRunning, healthy: true},
		}

		manager.mu.Lock()
		manager.services["already"] = already
		manager.services["clean"] = clean
		manager.order = []string{"clean", "already"}
		manager.mu.Unlock()

		err := manager.StopAll(context.Background())
		require.NoError(t, err, "an already-stopped service must not fail StopAll")
		assert.Equal(t, 1, already.stopCalls, "already-stopped service is still visited")
		assert.Equal(t, 1, clean.stopCalls, "clean service is stopped")
	})

	t.Run("genuine stop failure is surfaced and others still stopped", func(t *testing.T) {
		manager := createTestServiceManager(ManagerConfig{}, nil)

		failing := &stopSpyService{
			MockService: MockService{name: "failing", status: StatusRunning, healthy: true},
			stopErr:     errors.New("boom"),
		}
		other := &stopSpyService{
			MockService: MockService{name: "other", status: StatusRunning, healthy: true},
		}

		manager.mu.Lock()
		manager.services["failing"] = failing
		manager.services["other"] = other
		// reverse-registration order stops "failing" then "other"
		manager.order = []string{"other", "failing"}
		manager.mu.Unlock()

		err := manager.StopAll(context.Background())
		require.Error(t, err, "a genuine stop error must be surfaced")
		assert.Contains(t, err.Error(), "failing")
		assert.Equal(t, 1, other.stopCalls, "remaining services are stopped despite one failure")
	})

	t.Run("fully clean shutdown returns nil", func(t *testing.T) {
		manager := createTestServiceManager(ManagerConfig{}, nil)

		a := &stopSpyService{MockService: MockService{name: "a", status: StatusRunning, healthy: true}}
		b := &stopSpyService{MockService: MockService{name: "b", status: StatusStopped, healthy: false}, stopErr: ErrAlreadyStopped}

		manager.mu.Lock()
		manager.services["a"] = a
		manager.services["b"] = b
		manager.order = []string{"a", "b"}
		manager.mu.Unlock()

		require.NoError(t, manager.StopAll(context.Background()))
	})
}

// TestServiceManager_StopAll_CancellationBeforeStopAll drives the real gh#549
// ordering with real services, not mocks: the parent context is cancelled (the
// SIGTERM / Docker-restart analogue), each service's contextMonitor observes it
// and self-transitions to stopped via performGracefulShutdown, and only then
// does the manager visit Stop. StopAll must report a clean shutdown — the
// heartbeat and metrics-forwarder overrides used to reject this ordering with a
// plain error (gh#549).
func TestServiceManager_StopAll_CancellationBeforeStopAll(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)

	require.NoError(t, manager.RegisterInstance("component-manager", &mockComponentHealthGetter{
		MockService: MockService{name: "component-manager", status: StatusRunning, healthy: true},
	}))
	require.NoError(t, manager.registry.Register("heartbeat", NewHeartbeatService))
	heartbeat, err := manager.CreateService("heartbeat", json.RawMessage(`{"interval":"1s"}`), &Dependencies{})
	require.NoError(t, err)
	hb := heartbeat.(*HeartbeatService)
	mf := createTestMetricsForwarder(t, "1s", &metricsForwarderMockNATS{}, metric.NewMetricsRegistry())

	manager.RegisterInstance("metrics-forwarder", mf)

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, hb.Start(ctx))
	require.NoError(t, mf.Start(ctx))

	cancel()
	require.Eventually(t, func() bool {
		return hb.Status() == StatusStopped && mf.Status() == StatusStopped
	}, 2*time.Second, 5*time.Millisecond,
		"services must self-stop when the parent context is cancelled")

	require.NoError(t, manager.StopAll(context.Background()),
		"services already stopped by parent-context cancellation are a clean shutdown (gh#520/gh#549)")
}

// TestBaseService_CompletedStopIsIdempotent locks the per-service half of the
// contract: after exact owner completion, another Stop returns nil without
// repeating teardown (gh#520).
func TestBaseService_CompletedStopIsIdempotent(t *testing.T) {
	svc := NewBaseServiceWithOptions("idempotent-stop", nil, WithHealthInterval(0))
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()), "first Stop completes")
	assert.Equal(t, StatusStopped, svc.Status())

	require.NoError(t, svc.Stop(context.Background()), "second Stop after exact completion returns nil")
	assert.Equal(t, StatusStopped, svc.Status())
}
