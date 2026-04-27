package executors

import (
	"log/slog"
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerGitHub registers github_read + github_write when a GITHUB_TOKEN
// is present in the environment. No-op otherwise, so the binary starts
// cleanly in environments without GitHub integration.
//
// Pre-consolidation this was an init() in github_init.go that ran on
// package import. Now it's explicit from RegisterAll so main owns the
// registration schedule.
func registerGitHub(tools *agentictools.ExecutorRegistry, logger *slog.Logger) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return
	}

	client := NewGitHubHTTPClient(token)

	if err := tools.RegisterTool("github_read", NewGitHubReadExecutor(client)); err != nil {
		logger.Warn("Failed to register github_read tool", slog.Any("error", err))
		return
	}
	if err := tools.RegisterTool("github_write", NewGitHubWriteExecutor(client)); err != nil {
		logger.Warn("Failed to register github_write tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered github_read + github_write tools (global)")
}
