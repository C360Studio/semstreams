package agenticloop

import (
	"fmt"
	"sync"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
)

// trajectoryManager holds transient detail for active-loop execution mechanics.
// It is not durable state or a trajectory query authority.
type trajectoryManager struct {
	trajectories map[string]*agentic.Trajectory
	mu           sync.RWMutex
}

func newTrajectoryManager() *trajectoryManager {
	return &trajectoryManager{
		trajectories: make(map[string]*agentic.Trajectory),
	}
}

func (m *trajectoryManager) startTrajectory(loopID string) (agentic.Trajectory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	traj := agentic.NewTrajectory(loopID)
	m.trajectories[loopID] = &traj

	return traj, nil
}

func (m *trajectoryManager) addStep(loopID string, step agentic.TrajectoryStep) (agentic.Trajectory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	traj, exists := m.trajectories[loopID]
	if !exists {
		return agentic.Trajectory{}, errs.Wrap(fmt.Errorf("trajectory for loop %s not found", loopID), "TrajectoryManager", "operation", "find trajectory")
	}

	traj.AddStep(step)

	return *traj, nil
}

func (m *trajectoryManager) completeTrajectory(loopID, outcome string) (agentic.Trajectory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	traj, exists := m.trajectories[loopID]
	if !exists {
		return agentic.Trajectory{}, errs.Wrap(fmt.Errorf("trajectory for loop %s not found", loopID), "TrajectoryManager", "operation", "find trajectory")
	}

	traj.Complete(outcome)

	return *traj, nil
}

func (m *trajectoryManager) getTrajectory(loopID string) (agentic.Trajectory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	traj, exists := m.trajectories[loopID]
	if !exists {
		return agentic.Trajectory{}, errs.Wrap(fmt.Errorf("trajectory for loop %s not found", loopID), "TrajectoryManager", "operation", "find trajectory")
	}

	return *traj, nil
}
