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
	"fmt"
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
	// the name is frozen at RegisterBuiltins for the lifetime of the
	// process.
	LoopsBucket string
}

// RegisterBuiltins wires every tool this package owns into the supplied
// registry. Errors from individual register_* functions are aggregated via
// errors.Join so a misconfigured deployment sees every collision on a
// single boot, not just the first. The aggregate error is returned to the
// caller (main.go) which surfaces it via its normal error-return path —
// no panics, just a non-zero exit.
//
// Two distinct failure shapes:
//
//   - Pre-condition skips (nil manager, missing env var, KV bucket
//     unreachable) are intentional disable paths. They log and proceed —
//     not an error from this function's perspective.
//   - Registry-level failures (duplicate tool names, invalid args) are
//     misconfigurations that should block boot. Each register_* returns
//     them; we join them and return the aggregate.
func RegisterBuiltins(ctx context.Context, reg *agentictools.ExecutorRegistry, deps ToolDependencies) error {
	if reg == nil {
		return errors.New("RegisterBuiltins: nil registry")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	loopsBucket := deps.LoopsBucket
	if loopsBucket == "" {
		loopsBucket = "AGENT_LOOPS"
	}

	var errs []error
	track := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	track(registerBash(reg, logger))
	track(registerWebSearch(reg, deps.NATSClient, deps.Platform, logger))
	track(registerHTTPRequest(reg, deps.NATSClient, deps.Platform, logger))
	track(registerGitHub(reg, logger))

	if deps.NATSClient == nil {
		logger.Warn("nats client not available; skipping stateful tool registration (read_loop_result, decide, emit_diagnosis, query_entity); web_search and http_request fall back to text-only return without graph emission")
	} else {
		track(registerReadLoopResult(ctx, reg, deps.NATSClient, logger, loopsBucket))
		track(registerDecide(reg, deps.NATSClient, deps.Platform, logger))
		track(registerEmitDiagnosis(reg, deps.NATSClient, deps.Platform, logger))
		track(registerGraphQuery(ctx, reg, deps.NATSClient, logger))
		track(registerWriteTodos(reg, deps.NATSClient, deps.Platform, logger))
		track(registerScratchpad(reg, deps.NATSClient, deps.Platform, logger))
		// Gateway-first discovery tools (PR #54 step 2): thin wrappers
		// over the new graph.query.summary + graph.query.searchGraph
		// server-side resolvers. Read-only, no platform identity
		// required. See project_graph_tools_gateway_first_plan memory.
		track(registerSummarizeGraph(reg, deps.NATSClient, logger))
		track(registerSearchGraph(reg, deps.NATSClient, logger))
	}

	// Pattern-B registry-backed tools. A nil manager is a legal skip;
	// duplicate-name failures propagate.
	track(registerRules(reg, deps.RuleManager, logger))
	track(registerFlows(reg, deps.FlowManager, logger))
	track(registerPersonas(reg, deps.PersonaManager, logger))
	track(registerFlowTemplates(reg, deps.FlowTemplateManager, logger))
	track(registerComponentCatalog(reg, deps.ComponentRegistry, logger))
	track(registerFlowMonitor(reg, deps.NATSClient, deps.FlowManager, logger, loopsBucket))

	if len(errs) > 0 {
		return fmt.Errorf("RegisterBuiltins: %w", errors.Join(errs...))
	}
	return nil
}
