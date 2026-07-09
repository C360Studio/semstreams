package service

import (
	"errors"
	"testing"
	"time"

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

func (s *stopSpyService) Stop(_ time.Duration) error {
	s.stopCalls++
	return s.stopErr
}

// TestServiceManager_StopAll_Idempotency covers the coordinated-shutdown contract:
// an already-stopped/stopping service is clean success, a genuine stop failure is
// still surfaced, and a fully clean shutdown returns nil (gh#520).
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

		err := manager.StopAll(time.Second)
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

		err := manager.StopAll(time.Second)
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

		require.NoError(t, manager.StopAll(time.Second))
	})
}

// TestBaseService_StopIdempotent locks the per-service half of the contract:
// invoking Stop on an already-stopped/stopping service returns nil and does not
// re-run teardown (gh#520). BaseService is the framework contract holder.
func TestBaseService_StopIdempotent(t *testing.T) {
	svc := NewBaseServiceWithOptions("idempotent-stop", nil)

	// Simulate a running service that is then stopped, then stopped again.
	svc.status.Store(StatusRunning)

	require.NoError(t, svc.Stop(time.Second), "first Stop succeeds")
	assert.Equal(t, StatusStopped, svc.Status())

	require.NoError(t, svc.Stop(time.Second), "second Stop on an already-stopped service returns nil")
	assert.Equal(t, StatusStopped, svc.Status())

	// A service that observed self-transition to stopping also stops cleanly.
	svc.status.Store(StatusStopping)
	require.NoError(t, svc.Stop(time.Second), "Stop while stopping returns nil")
}
