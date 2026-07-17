package agenticloop

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/types"
)

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

// SetContentStore sets the ObjectStore for content storage in integration tests.
func (g *GraphWriterForTest) SetContentStore(store *objectstore.Store) {
	g.w.contentStore = store
}

// SetLogger replaces the graphWriter's logger for integration tests that need
// to capture log output (e.g. verifying divergent task_id warnings).
func (g *GraphWriterForTest) SetLogger(logger *slog.Logger) {
	g.w.logger = logger
}

func (g *GraphWriterForTest) WriteModelEndpoints(ctx context.Context) { g.w.WriteModelEndpoints(ctx) }
func (g *GraphWriterForTest) WriteLoopCompletion(ctx context.Context, e *agentic.LoopCompletedEvent) {
	g.w.WriteLoopCompletion(ctx, e)
}
func (g *GraphWriterForTest) WriteLoopFailure(ctx context.Context, e *agentic.LoopFailedEvent) {
	g.w.WriteLoopFailure(ctx, e)
}
func (g *GraphWriterForTest) WriteLoopCancellation(ctx context.Context, e *agentic.LoopCancelledEvent) {
	g.w.WriteLoopCancellation(ctx, e)
}
func (g *GraphWriterForTest) WriteTrajectorySteps(ctx context.Context, loopID string, trajectory *agentic.Trajectory) {
	g.w.WriteTrajectorySteps(ctx, loopID, trajectory)
}
func (g *GraphWriterForTest) WriteLineageTriples(ctx context.Context, loopID string, related map[string]any) error {
	return g.w.WriteLineageTriples(ctx, loopID, related)
}
func (g *GraphWriterForTest) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) error {
	return g.w.WriteSpawnIdentity(ctx, loopID, task)
}
