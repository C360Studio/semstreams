package agenticloop

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

// TrajectoryManager is a test-only view of active-loop execution mechanics.
// Production builds expose no aggregate trajectory manager or read method.
type TrajectoryManager struct {
	manager *trajectoryManager
}

func NewTrajectoryManager() *TrajectoryManager {
	return &TrajectoryManager{manager: newTrajectoryManager()}
}

func (m *TrajectoryManager) StartTrajectory(loopID string) (agentic.Trajectory, error) {
	return m.manager.startTrajectory(loopID)
}

func (m *TrajectoryManager) AddStep(loopID string, step agentic.TrajectoryStep) (agentic.Trajectory, error) {
	return m.manager.addStep(loopID, step)
}

func (m *TrajectoryManager) GetTrajectory(loopID string) (agentic.Trajectory, error) {
	return m.manager.getTrajectory(loopID)
}

// GetTrajectory exposes active-loop state only to external-package tests.
func (h *MessageHandler) GetTrajectory(loopID string) (agentic.Trajectory, error) {
	return h.trajectoryManager.getTrajectory(loopID)
}

// SetTestPublishHook wires a capturing function onto c.testPublishHook so
// unit tests can observe wire-level publishes from publishApprovalResponseToWire
// without a real NATS connection. The hook is only ever set from test code;
// production components always have a nil hook.
func (c *Component) SetTestPublishHook(fn func(subject string, data []byte)) {
	c.testPublishHook = fn
}

// GraphWriterForTest exposes graphWriter for integration testing.
// This type wraps the unexported graphWriter so that the _test package
// can exercise the full NATS round-trip without duplicating construction logic.
type GraphWriterForTest struct {
	w *graphWriter
}

// NewGraphWriterForTest creates a graphWriter for integration tests.
func NewGraphWriterForTest(client *natsclient.Client, reg model.RegistryReader, platform types.PlatformMeta) *GraphWriterForTest {
	return &GraphWriterForTest{
		w: &graphWriter{
			natsClient:    client,
			modelRegistry: reg,
			platform:      platform,
			logger:        slog.Default(),
		},
	}
}

// SetLogger replaces the graphWriter's logger for integration tests that need
// to capture log output (e.g. verifying divergent task_id warnings).
func (g *GraphWriterForTest) SetLogger(logger *slog.Logger) {
	g.w.logger = logger
}

func (g *GraphWriterForTest) WriteModelEndpoints(ctx context.Context) { g.w.WriteModelEndpoints(ctx) }
func (g *GraphWriterForTest) WriteLoopCompletion(
	ctx context.Context, e *agentic.LoopCompletedEvent, evidenceIncomplete bool,
) {
	g.w.WriteLoopCompletion(ctx, e, evidenceIncomplete)
}
func (g *GraphWriterForTest) WriteLoopFailure(
	ctx context.Context, e *agentic.LoopFailedEvent, evidenceIncomplete bool,
) {
	g.w.WriteLoopFailure(ctx, e, evidenceIncomplete)
}
func (g *GraphWriterForTest) WriteLoopCancellation(
	ctx context.Context, e *agentic.LoopCancelledEvent, evidenceIncomplete bool,
) {
	g.w.WriteLoopCancellation(ctx, e, evidenceIncomplete)
}
func (g *GraphWriterForTest) WriteLineageTriples(ctx context.Context, loopID string, related map[string]any) error {
	return g.w.WriteLineageTriples(ctx, loopID, related)
}
func (g *GraphWriterForTest) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) error {
	return g.w.WriteSpawnIdentity(ctx, loopID, task)
}
