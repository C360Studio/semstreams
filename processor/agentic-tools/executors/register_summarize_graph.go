package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerSummarizeGraph wires the summarize_graph tool. The
// underlying graph.query.summary resolver lives in processor/graph-
// query/ (PR #54); this tool is a thin agent-facing wrapper that
// formats the structured SummaryData as LLM-friendly text. Read-only,
// no graph emission, no platform identity required — just the NATS
// client.
//
// A registry-level failure (duplicate name) returns the error so
// RegisterBuiltins can surface it at boot.
func registerSummarizeGraph(reg *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger) error {
	executor := NewSummarizeGraphExecutor(natsClient)
	if err := reg.RegisterTool("summarize_graph", executor); err != nil {
		return fmt.Errorf("register summarize_graph: %w", err)
	}
	logger.Info("Registered summarize_graph tool")
	return nil
}
