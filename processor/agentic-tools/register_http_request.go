package agentictools

import (
	"log/slog"

	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
)

// registerHTTPRequestTool registers the http_request executor globally.
// Always available; no external deps.
func (c *Component) registerHTTPRequestTool() {
	http := executors.NewHTTPRequestExecutor()
	if err := registerGlobalTool("http_request", http); err != nil {
		c.logger.Warn("Failed to register http_request tool", slog.Any("error", err))
		return
	}
	c.logger.Info("Registered http_request tool (global)")
}
