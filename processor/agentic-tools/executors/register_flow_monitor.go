package executors

import (
	"context"
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

// registerFlowMonitor opens the loops KV bucket and wires the monitor_flow
// tool globally. Nil natsClient or flowMgr → skip with warn (the tool
// requires both to be useful). The bucketName must match the one
// registerReadLoopResult used — they're the same physical bucket, different
// access pattern (scan vs point-get). ToolDependencies.LoopsBucket is the
// single source of truth for both.
func registerFlowMonitor(natsClient *natsclient.Client, flowMgr FlowManager, logger *slog.Logger, bucketName string) {
	if natsClient == nil {
		logger.Warn("monitor_flow tool disabled: no NATS client provided")
		return
	}
	if flowMgr == nil {
		logger.Warn("monitor_flow tool disabled: no FlowManager provided")
		return
	}

	// Open a fresh handle to the same KV bucket read_loop_result opened.
	// Shared KV config means whichever register fn (or the agentic-loop
	// component) lands first creates the bucket with the agreed-upon
	// History/TTL; the others get the existing handle idempotently.
	ctx := context.Background()
	bucket, err := natsClient.CreateKeyValueBucket(ctx, newLoopResultBucketConfig(bucketName))
	if err != nil {
		logger.Warn("monitor_flow tool disabled: could not open loops bucket",
			slog.String("bucket", bucketName),
			slog.Any("error", err))
		return
	}

	store := natsClient.NewKVStore(bucket)
	adapter := &flowStateAdapter{mgr: flowMgr}
	executor := agentictools.NewFlowMonitorExecutor(store, adapter, logger)

	if err := registerGlobal(agentictools.FlowMonitorToolName, executor); err != nil {
		logger.Warn("Failed to register monitor_flow tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered monitor_flow tool (global)",
		slog.String("bucket", bucketName))
}
