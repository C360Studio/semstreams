package executors

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// loopMonitorBucket is the KV bucket that holds COMPLETE_* loop records.
// Matches the constant used by registerReadLoopResult — same physical bucket,
// different access pattern (scan vs point-get).
const loopMonitorBucket = loopResultBucket

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

// registerFlowMonitor opens the AGENT_LOOPS KV bucket and wires the
// monitor_flow tool globally. Nil natsClient or flowMgr → skip with warn
// (the tool requires both to be useful).
func registerFlowMonitor(natsClient *natsclient.Client, flowMgr FlowManager, logger *slog.Logger) {
	if natsClient == nil {
		logger.Warn("monitor_flow tool disabled: no NATS client provided")
		return
	}
	if flowMgr == nil {
		logger.Warn("monitor_flow tool disabled: no FlowManager provided")
		return
	}

	// We reuse the same AGENT_LOOPS bucket as read_loop_result; open a fresh
	// handle here so the two tools are lifecycle-independent. Use the shared
	// loopResultBucketConfig so whichever register fn (or the agentic-loop
	// component) lands first creates the bucket with the agreed-upon
	// History/TTL; the others get the existing handle idempotently.
	ctx := context.Background()
	bucket, err := natsClient.CreateKeyValueBucket(ctx, loopResultBucketConfig)
	if err != nil {
		logger.Warn("monitor_flow tool disabled: could not open loops bucket",
			slog.String("bucket", loopMonitorBucket),
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
		slog.String("bucket", loopMonitorBucket))
}
