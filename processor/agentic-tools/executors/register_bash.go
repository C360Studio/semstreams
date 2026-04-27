package executors

import (
	"fmt"
	"log/slog"
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerBash registers the bash executor on the supplied registry.
// Always available — resolves local vs sandbox mode from SANDBOX_URL at
// construction time. A registry-level failure (duplicate name) returns
// the error so RegisterBuiltins can surface it at boot.
func registerBash(tools *agentictools.ExecutorRegistry, logger *slog.Logger) error {
	bash := NewBashExecutorFromEnv()
	if err := tools.RegisterTool("bash", bash); err != nil {
		return fmt.Errorf("register bash: %w", err)
	}
	mode := "local"
	if os.Getenv("SANDBOX_URL") != "" {
		mode = "sandbox"
	}
	logger.Info("Registered bash tool", slog.String("mode", mode))
	return nil
}
