package agentictools

import (
	"log/slog"
	"os"

	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
)

// registerWebSearchTool registers the web_search executor: real Brave-backed
// one when BRAVE_SEARCH_API_KEY is set, stub otherwise. The stub keeps
// web_search advertised for researcher-role agents and e2e fixtures that
// don't want to hit an external API.
func (c *Component) registerWebSearchTool() {
	if apiKey := os.Getenv("BRAVE_SEARCH_API_KEY"); apiKey != "" {
		ws := executors.NewWebSearchExecutor(apiKey)
		if err := registerGlobalTool("web_search", ws); err != nil {
			c.logger.Warn("Failed to register web_search tool", slog.Any("error", err))
			return
		}
		c.logger.Info("Registered web_search tool (global)", slog.String("provider", "brave"))
		return
	}
	stub := executors.NewStubWebSearchExecutor()
	if err := registerGlobalTool("web_search", stub); err != nil {
		c.logger.Warn("Failed to register web_search stub", slog.Any("error", err))
		return
	}
	c.logger.Info("Registered web_search tool (global)", slog.String("provider", "stub"))
}
