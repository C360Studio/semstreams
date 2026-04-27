// Package executors hosts the concrete tool implementations and their
// wire-to-registry entry points.
//
// Stateless tools (bash, http_request, web_search, github_*) wire from env
// vars alone. Stateful tools (query_entity, read_loop_result, decide) need
// runtime deps (NATS KV buckets, platform identity) which only exist after
// the binary has initialised streams/buckets — so their wire functions
// take explicit arguments rather than registering at init() time.
//
// The single caller of RegisterBuiltins is main.go, after ensureStreams
// and before component.Start. The registry is constructed by main and
// passed in explicitly — there is no package-level singleton.
package executors

import (
	"context"
	"errors"
	"log/slog"

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
//   - ComponentRegistry nil → list_components skipped (Pattern-B step 5)
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
	ComponentRegistry   *component.Registry // Pattern-B step 5; nil → list_components skipped
	// LoopsBucket is the NATS KV bucket name holding agent-loop state.
	// read_loop_result + flow_monitor both read from it. Empty falls back
	// to "AGENT_LOOPS". One bucket per process — wiring is boot-time so
	// the name is frozen at RegisterAll for the lifetime of the process.
	LoopsBucket string
}

// RegisterBuiltins wires every tool this package owns into the
// supplied registry. Errors propagate so callers see misconfigurations
// at boot rather than silently dropped registrations — the previous
// "already registered" swallow that lived here was a workaround for
// the global singleton's lifecycle and is no longer needed now that
// each process owns its registry explicitly.
//
// Stateful tools (NATS-bound) skip silently when their dependency is
// nil; that's intentional and lets callers ship partial-feature
// deployments (e.g., graph-only flows without an LLM loop).
func RegisterBuiltins(ctx context.Context, reg *agentictools.ExecutorRegistry, deps ToolDependencies) error {
	if reg == nil {
		return errors.New("RegisterBuiltins: nil registry")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	registerBash(reg, logger)
	registerWebSearch(reg, logger)
	registerHTTPRequest(reg, logger)
	registerGitHub(reg, logger)

	loopsBucket := deps.LoopsBucket
	if loopsBucket == "" {
		loopsBucket = "AGENT_LOOPS"
	}

	if deps.NATSClient == nil {
		logger.Warn("nats client not available; skipping stateful tool registration (read_loop_result, decide, emit_diagnosis, query_entity)")
	} else {
		registerReadLoopResult(ctx, reg, deps.NATSClient, logger, loopsBucket)
		registerDecide(reg, deps.NATSClient, deps.Platform, logger)
		registerEmitDiagnosis(reg, deps.NATSClient, deps.Platform, logger)
		registerGraphQuery(ctx, reg, deps.NATSClient, logger)
	}

	// Pattern-B registry-backed tools. Each wire function handles a nil
	// manager as a skip so callers can ship partial configs without
	// exploding.
	registerRules(reg, deps.RuleManager, logger)
	registerFlows(reg, deps.FlowManager, logger)
	registerPersonas(reg, deps.PersonaManager, logger)
	registerFlowTemplates(reg, deps.FlowTemplateManager, logger)
	registerComponentCatalog(reg, deps.ComponentRegistry, logger)
	registerFlowMonitor(reg, deps.NATSClient, deps.FlowManager, logger, loopsBucket)

	return nil
}
