package executors

import (
	"fmt"
	"log/slog"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerHTTPRequest registers the http_request executor. Always
// available; no external deps. A registry-level failure (duplicate name)
// returns the error so RegisterBuiltins can surface it at boot.
func registerHTTPRequest(tools *agentictools.ExecutorRegistry, logger *slog.Logger) error {
	httpExec := NewHTTPRequestExecutor()
	if err := tools.RegisterTool("http_request", httpExec); err != nil {
		return fmt.Errorf("register http_request: %w", err)
	}
	logger.Info("Registered http_request tool")
	return nil
}
