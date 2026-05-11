package executors

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerWebSearch registers the web_search executor: real Brave-backed
// one when BRAVE_SEARCH_API_KEY is set, stub otherwise. The stub keeps
// web_search advertised for researcher-role agents and e2e fixtures that
// don't want to hit an external API.
//
// When natsClient is non-nil, the Brave-backed executor additionally
// emits agent.web.observation triples per result so downstream rules /
// queries / ops diagnosis can see what URLs the loop has seen. The stub
// intentionally does NOT emit — canned data shouldn't pollute the graph.
//
// A registry-level failure (duplicate name) returns the error so
// RegisterBuiltins can surface it at boot.
func registerWebSearch(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if apiKey == "" {
		stub := NewStubWebSearchExecutor()
		if err := tools.RegisterTool("web_search", stub); err != nil {
			return fmt.Errorf("register web_search (stub): %w", err)
		}
		logger.Info("Registered web_search tool", slog.String("provider", "stub"))
		return nil
	}

	opts := []WebSearchOption{
		WithWebSearchLogger(logger),
		WithWebSearchPlatform(platform),
	}
	emitting := "false"
	if natsClient != nil {
		opts = append(opts, WithWebSearchTriplePublisher(agentictools.NewNATSTriplePublisher(natsClient)))
		emitting = "true"
	}
	ws := NewWebSearchExecutor(apiKey, opts...)
	if err := tools.RegisterTool("web_search", ws); err != nil {
		return fmt.Errorf("register web_search (brave): %w", err)
	}
	logger.Info("Registered web_search tool",
		slog.String("provider", "brave"),
		slog.String("graph_emission", emitting))
	return nil
}
