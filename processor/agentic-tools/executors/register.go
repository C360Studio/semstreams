package executors

import (
	"log/slog"
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// RegisterAll registers all available tool executors based on environment
// configuration. Tools that require external services (sandbox, Brave API)
// are only registered when the relevant environment variables are set.
//
// Call this during application startup to populate the global tool registry.
func RegisterAll(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	// Bash — always available (local or sandbox mode)
	bash := NewBashExecutorFromEnv()
	if err := agentictools.RegisterTool("bash", bash); err != nil {
		logger.Warn("Failed to register bash tool", slog.Any("error", err))
	} else {
		mode := "local"
		if os.Getenv("SANDBOX_URL") != "" {
			mode = "sandbox"
		}
		logger.Info("Registered bash tool", slog.String("mode", mode))
	}

	// Web search — real executor with BRAVE_SEARCH_API_KEY, stub otherwise.
	// The stub keeps web_search in the tool list so researcher-role agents
	// and E2E fixtures work without external API dependencies.
	if apiKey := os.Getenv("BRAVE_SEARCH_API_KEY"); apiKey != "" {
		ws := NewWebSearchExecutor(apiKey)
		if err := agentictools.RegisterTool("web_search", ws); err != nil {
			logger.Warn("Failed to register web_search tool", slog.Any("error", err))
		} else {
			logger.Info("Registered web_search tool", slog.String("provider", "brave"))
		}
	} else {
		stub := NewStubWebSearchExecutor()
		if err := agentictools.RegisterTool("web_search", stub); err != nil {
			logger.Warn("Failed to register web_search stub", slog.Any("error", err))
		} else {
			logger.Info("Registered web_search tool", slog.String("provider", "stub"))
		}
	}

	// HTTP request — always available
	http := NewHTTPRequestExecutor()
	if err := agentictools.RegisterTool("http_request", http); err != nil {
		logger.Warn("Failed to register http_request tool", slog.Any("error", err))
	} else {
		logger.Info("Registered http_request tool")
	}
}
