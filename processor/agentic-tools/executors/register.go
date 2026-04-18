// Package executors hosts the concrete tool implementations and their
// wire-to-global-registry entry points.
//
// Stateless tools (bash, http_request, web_search, github_*) wire from env
// vars alone. Stateful tools (query_entity, read_loop_result, decide) need
// runtime deps (NATS KV buckets, platform identity) which only exist after
// the binary has initialised streams/buckets — so their wire functions
// take explicit arguments rather than registering at init() time.
//
// The single caller of RegisterAll is main.go, after ensureStreams and
// before component.Start. Keeping wiring out of agentic-tools.Component
// leaves that component a pure tool-execution endpoint (it reads from the
// global registry, it doesn't write to it).
package executors

import (
	"context"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// RegisterAll wires every tool this package owns into the agentic-tools
// global registry. Failures are logged; stateful-tool wiring that can't
// reach its bucket skips silently and lets the rest proceed — same
// philosophy as the pre-refactor init() pattern.
func RegisterAll(ctx context.Context, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	registerBash(logger)
	registerWebSearch(logger)
	registerHTTPRequest(logger)
	registerGitHub(logger)

	if natsClient == nil {
		logger.Warn("nats client not available; skipping stateful tool registration (read_loop_result, decide, query_entity)")
		return
	}

	registerReadLoopResult(ctx, natsClient, logger)
	registerDecide(natsClient, platform, logger)
	registerGraphQuery(ctx, natsClient, logger)
}

// registerGlobal is the shared RegisterTool wrapper with idempotent
// "already registered" handling. The global registry persists across
// component Stop/Start cycles; a re-registration on restart would return
// an error the wire functions should treat as a no-op.
func registerGlobal(name string, executor agentictools.ToolExecutor) error {
	if err := agentictools.RegisterTool(name, executor); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			return nil
		}
		return err
	}
	return nil
}
