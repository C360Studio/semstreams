package executors

import (
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerHTTPRequest registers the http_request executor globally.
// Always available; no external deps.
func registerHTTPRequest(tools *agentictools.ExecutorRegistry, logger *slog.Logger) {
	http := NewHTTPRequestExecutor()
	if err := tools.RegisterTool("http_request", http); err != nil {
		logger.Warn("Failed to register http_request tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered http_request tool (global)")
}
