package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerEmitLesson wires the ops agent's emit_lesson tool (ADR-080), the
// procedural-memory write path on the ADR-027 ops observation seam. Each call
// mints a content-derived {org}.{platform}.agent.lesson.record.{uuid5} entity
// (born status="proposed") via the graph.mutation.entity.create_with_triples
// NATS surface — the same birth path emit_diagnosis uses. A nil natsClient is a
// deployment choice (skip silently); a registry-level failure (duplicate name)
// propagates so RegisterBuiltins can surface it at boot.
func registerEmitLesson(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	if natsClient == nil {
		logger.Warn("nats client not available; skipping emit_lesson registration")
		return nil
	}
	store := agentictools.NewNATSLessonStore(natsClient)
	executor := agentictools.NewEmitLessonExecutor(store, platform, logger)
	if err := tools.RegisterTool(agentictools.EmitLessonToolName, executor); err != nil {
		return fmt.Errorf("register emit_lesson: %w", err)
	}
	logger.Info("Registered emit_lesson tool",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
	return nil
}
