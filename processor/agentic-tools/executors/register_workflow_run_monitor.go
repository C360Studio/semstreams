package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerWorkflowRunMonitor wires monitor_workflow_runs without opening
// AGENT_LOOPS. The executor binds the bucket when each call runs.
func registerWorkflowRunMonitor(
	tools *agentictools.ExecutorRegistry,
	natsClient *natsclient.Client,
	logger *slog.Logger,
	bucketName string,
) error {
	if natsClient == nil {
		logger.Warn("monitor_workflow_runs tool disabled: no NATS client provided")
		return nil
	}

	executor := agentictools.NewWorkflowRunMonitorExecutor(
		lazyLoopsKV{client: natsClient, bucket: bucketName},
		logger,
	)
	if err := tools.RegisterTool(agentictools.WorkflowRunMonitorToolName, executor); err != nil {
		return fmt.Errorf("register monitor_workflow_runs: %w", err)
	}
	logger.Info("Registered monitor_workflow_runs tool", slog.String("bucket", bucketName))
	return nil
}
