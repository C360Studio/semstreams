package executors

import (
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerEmitDiagnosis wires the ops agent's emit_diagnosis terminal tool.
// The tool mints a new ops.diagnosis entity per call and publishes findings
// via the graph.mutation.triple.add NATS surface (same path rule actions
// and the decide tool use). Registered globally so the ops-agent flow
// advertises it to the LLM the same way all other global tools are
// advertised.
func registerEmitDiagnosis(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) {
	if natsClient == nil {
		logger.Warn("nats client not available; skipping emit_diagnosis registration")
		return
	}
	publisher := agentictools.NewNATSTriplePublisher(natsClient)
	executor := agentictools.NewEmitDiagnosisExecutor(publisher, platform, logger)
	if err := tools.RegisterTool(agentictools.EmitDiagnosisToolName, executor); err != nil {
		logger.Warn("Failed to register emit_diagnosis tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered emit_diagnosis tool (global)",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
}
