package executors

import (
	"log/slog"
	"os"
)

// registerBash registers the bash executor globally. Always available —
// resolves local vs sandbox mode from SANDBOX_URL at construction time.
func registerBash(logger *slog.Logger) {
	bash := NewBashExecutorFromEnv()
	if err := registerGlobal("bash", bash); err != nil {
		logger.Warn("Failed to register bash tool", slog.Any("error", err))
		return
	}
	mode := "local"
	if os.Getenv("SANDBOX_URL") != "" {
		mode = "sandbox"
	}
	logger.Info("Registered bash tool (global)", slog.String("mode", mode))
}
