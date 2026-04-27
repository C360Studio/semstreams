package executors

import (
	"fmt"
	"log/slog"
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerWebSearch registers the web_search executor: real Brave-backed
// one when BRAVE_SEARCH_API_KEY is set, stub otherwise. The stub keeps
// web_search advertised for researcher-role agents and e2e fixtures that
// don't want to hit an external API. A registry-level failure (duplicate
// name) returns the error so RegisterBuiltins can surface it at boot.
func registerWebSearch(tools *agentictools.ExecutorRegistry, logger *slog.Logger) error {
	if apiKey := os.Getenv("BRAVE_SEARCH_API_KEY"); apiKey != "" {
		ws := NewWebSearchExecutor(apiKey)
		if err := tools.RegisterTool("web_search", ws); err != nil {
			return fmt.Errorf("register web_search (brave): %w", err)
		}
		logger.Info("Registered web_search tool", slog.String("provider", "brave"))
		return nil
	}
	stub := NewStubWebSearchExecutor()
	if err := tools.RegisterTool("web_search", stub); err != nil {
		return fmt.Errorf("register web_search (stub): %w", err)
	}
	logger.Info("Registered web_search tool", slog.String("provider", "stub"))
	return nil
}
