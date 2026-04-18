package agentictools

import (
	"log/slog"
	"os"

	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
)

// registerGitHubTools registers github_read + github_write when a
// GITHUB_TOKEN is present in the environment. No-op otherwise, so the
// binary starts cleanly in environments without GitHub integration.
// Moved here from executors/github_init.go to break the
// agentic-tools↔executors import cycle; same env-gated behaviour.
func (c *Component) registerGitHubTools() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return
	}

	client := executors.NewGitHubHTTPClient(token)

	if err := registerGlobalTool("github_read", executors.NewGitHubReadExecutor(client)); err != nil {
		c.logger.Warn("Failed to register github_read tool",
			slog.Any("error", err))
		return
	}
	if err := registerGlobalTool("github_write", executors.NewGitHubWriteExecutor(client)); err != nil {
		c.logger.Warn("Failed to register github_write tool",
			slog.Any("error", err))
		return
	}
	c.logger.Info("Registered github_read + github_write tools (global)")
}
