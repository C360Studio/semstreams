package executors

import (
	"fmt"
	"log/slog"
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerGitHub registers github_read + github_write when GITHUB_TOKEN is
// present. Returns nil and skips silently when the token is absent — that's
// a deployment choice, not a misconfiguration. Registry-level failures
// (duplicate names) propagate so RegisterBuiltins can surface them at boot.
func registerGitHub(tools *agentictools.ExecutorRegistry, logger *slog.Logger) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil
	}

	client := NewGitHubHTTPClient(token)

	if err := tools.RegisterTool("github_read", NewGitHubReadExecutor(client)); err != nil {
		return fmt.Errorf("register github_read: %w", err)
	}
	if err := tools.RegisterTool("github_write", NewGitHubWriteExecutor(client)); err != nil {
		return fmt.Errorf("register github_write: %w", err)
	}
	logger.Info("Registered github_read + github_write tools")
	return nil
}
