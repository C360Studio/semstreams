package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerHTTPRequest registers the http_request executor. Always
// available; no external deps.
//
// When natsClient is non-nil, each successful 2xx/3xx fetch additionally
// emits agent.web.observation triples so downstream rules / queries /
// ops diagnosis can see what URLs the loop pulled. Per-triple failures
// log + counter + continue.
//
// A registry-level failure (duplicate name) returns the error so
// RegisterBuiltins can surface it at boot.
func registerHTTPRequest(tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	opts := []HTTPRequestOption{
		WithHTTPLogger(logger),
		WithHTTPPlatform(platform),
	}
	emitting := "false"
	if natsClient != nil {
		opts = append(opts, WithHTTPTriplePublisher(agentictools.NewNATSTriplePublisher(natsClient)))
		emitting = "true"
	}
	httpExec := NewHTTPRequestExecutor(opts...)
	if err := tools.RegisterTool("http_request", httpExec); err != nil {
		return fmt.Errorf("register http_request: %w", err)
	}
	logger.Info("Registered http_request tool",
		slog.String("graph_emission", emitting))
	return nil
}
