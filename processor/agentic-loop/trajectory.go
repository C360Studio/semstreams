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

	return snapshotTrajectory(traj), nil
}

// discardTrajectory releases the full-text aggregate once a loop has no more
// active execution consumers. It is intentionally idempotent so competing
// terminal/error cleanup paths cannot turn cleanup into another failure.
func (m *trajectoryManager) discardTrajectory(loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.trajectories, loopID)
}

func (m *trajectoryManager) getTrajectory(loopID string) (agentic.Trajectory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	traj, exists := m.trajectories[loopID]
	if !exists {
		return agentic.Trajectory{}, errs.Wrap(fmt.Errorf("trajectory for loop %s not found", loopID), "TrajectoryManager", "operation", "find trajectory")
	}

	// Detach the slice before releasing the lock. Active writers may append a
	// later step until the terminal owner discards the entry; readers must not
	// retain the manager's mutable backing array after the synchronized read.
	return snapshotTrajectory(traj), nil
}

func snapshotTrajectory(traj *agentic.Trajectory) agentic.Trajectory {
	snapshot := *traj
	snapshot.Steps = append([]agentic.TrajectoryStep(nil), traj.Steps...)
	return snapshot
}
