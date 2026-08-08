package executors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// flowStateAdapter bridges FlowManager (executors package) to the
// agentictools.FlowStateReader interface so the monitor executor stays free
// of a direct flowstore import.
type flowStateAdapter struct {
	mgr FlowManager
}

func (a *flowStateAdapter) Get(ctx context.Context, id string) (agentictools.FlowState, error) {
	flow, err := a.mgr.Get(ctx, id)
	if err != nil {
		return agentictools.FlowState{}, err
	}
	return agentictools.FlowState{RuntimeState: string(flow.RuntimeState)}, nil
}

// registerFlowMonitor wires monitor_flow without opening AGENT_LOOPS. Nil
// natsClient or flowMgr remains a legal dependency skip. The executor binds
// the bucket must-exist when each call runs.
//
// The bucketName must match the one registerReadLoopResult used —
// they're the same physical bucket, different access pattern (scan vs
// point-get). ToolDependencies.LoopsBucket is the single source of truth
// for both.
func registerFlowMonitor(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, flowMgr FlowManager, logger *slog.Logger, bucketName string) error {
	if natsClient == nil {
		logger.Warn("monitor_flow tool disabled: no NATS client provided")
		return nil
	}
	if flowMgr == nil {
		logger.Warn("monitor_flow tool disabled: no FlowManager provided")
		return nil
	}

	adapter := &flowStateAdapter{mgr: flowMgr}
	executor := agentictools.NewFlowMonitorExecutor(lazyLoopsKV{client: natsClient, bucket: bucketName}, adapter, logger)

	if err := tools.RegisterTool(agentictools.FlowMonitorToolName, executor); err != nil {
		return fmt.Errorf("register monitor_flow: %w", err)
	}
	logger.Info("Registered monitor_flow tool",
		slog.String("bucket", bucketName))
	return nil
}
