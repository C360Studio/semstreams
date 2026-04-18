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

// ToolDependencies carries the runtime inputs the tool-registration
// functions need. Using a struct rather than a growing positional arg list
// follows the project convention (memory: feedback_go_signatures — "4+
// args → request struct"). Adding a new Pattern-B manager in the future
// means adding a field here, not shifting every call site.
//
// Zero values are legal on optional fields:
//   - Logger nil → slog.Default()
//   - NATSClient nil → stateful tools (read_loop_result, decide,
//     query_entity) are skipped
//   - RuleManager nil → rule CRUD tools are skipped
//
// Platform is a value type (not pointer) because PlatformMeta is a small
// POD; the empty value is still safe for the decide tool to use.
type ToolDependencies struct {
	NATSClient          *natsclient.Client
	Platform            component.PlatformMeta
	Logger              *slog.Logger
	RuleManager         RuleManager         // Pattern-B step 1
	FlowManager         FlowManager         // Pattern-B step 2
	PersonaManager      PersonaManager      // Pattern-B step 3
	FlowTemplateManager FlowTemplateManager // Pattern-B step 4
}

// RegisterAll wires every tool this package owns into the agentic-tools
// global registry. Failures are logged; stateful-tool wiring that can't
// reach its bucket skips silently and lets the rest proceed — same
// philosophy as the pre-refactor init() pattern.
func RegisterAll(ctx context.Context, deps ToolDependencies) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	registerBash(logger)
	registerWebSearch(logger)
	registerHTTPRequest(logger)
	registerGitHub(logger)

	if deps.NATSClient == nil {
		logger.Warn("nats client not available; skipping stateful tool registration (read_loop_result, decide, query_entity)")
	} else {
		registerReadLoopResult(ctx, deps.NATSClient, logger)
		registerDecide(deps.NATSClient, deps.Platform, logger)
		registerGraphQuery(ctx, deps.NATSClient, logger)
	}

	// Pattern-B registry-backed tools. Each wire function handles a nil
	// manager as a skip so callers can ship partial configs without
	// exploding.
	registerRules(deps.RuleManager, logger)
	registerFlows(deps.FlowManager, logger)
	registerPersonas(deps.PersonaManager, logger)
	registerFlowTemplates(deps.FlowTemplateManager, logger)
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
