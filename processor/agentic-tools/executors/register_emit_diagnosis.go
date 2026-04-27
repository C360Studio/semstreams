package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerEmitDiagnosis wires the ops agent's emit_diagnosis terminal tool.
// The tool mints a new ops.diagnosis entity per call and publishes findings
// via the graph.mutation.triple.add NATS surface (same path rule actions
// and the decide tool use). A nil natsClient is a deployment choice (skip
// silently); a registry-level failure (duplicate name) propagates so
// RegisterBuiltins can surface it at boot.
func registerEmitDiagnosis(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	if natsClient == nil {
		logger.Warn("nats client not available; skipping emit_diagnosis registration")
		return nil
	}
	publisher := agentictools.NewNATSTriplePublisher(natsClient)
	executor := agentictools.NewEmitDiagnosisExecutor(publisher, platform, logger)
	if err := tools.RegisterTool(agentictools.EmitDiagnosisToolName, executor); err != nil {
		return fmt.Errorf("register emit_diagnosis: %w", err)
	}
	logger.Info("Registered emit_diagnosis tool",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
	return nil
}
