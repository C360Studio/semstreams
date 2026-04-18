package agentictools

import (
	"log/slog"
	"os"

	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
)

// registerBashTool registers the bash executor globally. Always available —
// resolves local vs sandbox mode from SANDBOX_URL at construction time.
// Moved here from executors/register.go to break the
// agentic-tools↔executors cycle once we added stateful executors (e.g.
// GraphQueryExecutor) that need agentic-tools to wire them.
func (c *Component) registerBashTool() {
	bash := executors.NewBashExecutorFromEnv()
	if err := registerGlobalTool("bash", bash); err != nil {
		c.logger.Warn("Failed to register bash tool", slog.Any("error", err))
		return
	}
	mode := "local"
	if os.Getenv("SANDBOX_URL") != "" {
		mode = "sandbox"
	}
	c.logger.Info("Registered bash tool (global)", slog.String("mode", mode))
}
