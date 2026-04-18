package executors

import (
	"log/slog"
	"os"
)

// registerWebSearch registers the web_search executor: real Brave-backed
// one when BRAVE_SEARCH_API_KEY is set, stub otherwise. The stub keeps
// web_search advertised for researcher-role agents and e2e fixtures that
// don't want to hit an external API.
func registerWebSearch(logger *slog.Logger) {
	if apiKey := os.Getenv("BRAVE_SEARCH_API_KEY"); apiKey != "" {
		ws := NewWebSearchExecutor(apiKey)
		if err := registerGlobal("web_search", ws); err != nil {
			logger.Warn("Failed to register web_search tool", slog.Any("error", err))
			return
		}
		logger.Info("Registered web_search tool (global)", slog.String("provider", "brave"))
		return
	}
	stub := NewStubWebSearchExecutor()
	if err := registerGlobal("web_search", stub); err != nil {
		logger.Warn("Failed to register web_search stub", slog.Any("error", err))
		return
	}
	logger.Info("Registered web_search tool (global)", slog.String("provider", "stub"))
}
