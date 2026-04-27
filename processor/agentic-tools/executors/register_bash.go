package executors

import (
	"log/slog"
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerBash registers the bash executor globally. Always available —
// resolves local vs sandbox mode from SANDBOX_URL at construction time.
func registerBash(tools *agentictools.ExecutorRegistry, logger *slog.Logger) {
	bash := NewBashExecutorFromEnv()
	if err := tools.RegisterTool("bash", bash); err != nil {
		logger.Warn("Failed to register bash tool", slog.Any("error", err))
		return
	}
	mode := "local"
	if os.Getenv("SANDBOX_URL") != "" {
		mode = "sandbox"
	}
	logger.Info("Registered bash tool (global)", slog.String("mode", mode))
}
