package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerSearchGraph wires the search_graph tool. The underlying
// graph.query.searchGraph resolver lives in processor/graph-query/
// (PR #54) and wraps globalSearch with a server-side semantic
// fallback when GraphRAG strategies return empty. The agent tool is
// a thin wrapper that formats the response as LLM-friendly text
// including a degraded-banner when the fallback fired.
//
// A registry-level failure (duplicate name) returns the error so
// RegisterBuiltins can surface it at boot.
func registerSearchGraph(reg *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger) error {
	executor := NewSearchGraphExecutor(natsClient)
	if err := reg.RegisterTool("search_graph", executor); err != nil {
		return fmt.Errorf("register search_graph: %w", err)
	}
	logger.Info("Registered search_graph tool")
	return nil
}
