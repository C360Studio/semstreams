package executors

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerScratchpad wires the scratchpad tool. The tool writes
// agent.scratch.* triples on the calling loop's entity via the same
// graph.mutation.triple.append NATS surface decide/write_todos use.
//
// semspec ask 2026-05-12, ADR-036 §Future candidates first
// implementation. Available to any role; persona authors opt specific
// roles out via tool allowlists for tiny-model deployments.
//
// A registry-level failure (duplicate name) returns the error so
// RegisterBuiltins surfaces it at boot.
func registerScratchpad(reg *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger) error {
	publisher := agentictools.NewNATSTriplePublisher(natsClient)
	executor := agentictools.NewScratchpadExecutor(publisher, platform)
	executor.SetLogger(logger)
	if err := reg.RegisterTool(agentictools.ScratchpadToolName, executor); err != nil {
		return fmt.Errorf("register scratchpad: %w", err)
	}
	logger.Info("Registered scratchpad tool",
		slog.String("org", platform.Org),
		slog.String("platform", platform.Platform))
	return nil
}
