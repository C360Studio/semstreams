// Package executors hosts the concrete tool implementations and their
// wire-to-registry entry points.
//
// Stateless tools (bash, http_request, web_search) wire from env
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
	"github.com/c360studio/semstreams/pkg/projection"
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
//   - RestrictedDecideActions nil/empty → permissive default (no decide
//     action restricted)
//   - SkipBuiltins nil/empty → all builtins register (today's behaviour)
//
// Platform is a value type (not pointer) because PlatformMeta is a small
// POD; the empty value is still safe for the decide tool to use.
type ToolDependencies struct {
	NATSClient          *natsclient.Client
	MutationClient      *projection.MutationClient
	Platform            component.PlatformMeta
	Logger              *slog.Logger
	RuleManager         RuleManager         // Pattern-B step 1
	FlowManager         FlowManager         // Pattern-B step 2
	PersonaManager      PersonaManager      // Pattern-B step 3
	FlowTemplateManager FlowTemplateManager // Pattern-B step 4
	ComponentRegistry   *component.Registry // Pattern-B step 5; nil → list_components skipped
	FlowEngineManager   FlowEngineManager   // Pattern-B step 6; nil → flow_lifecycle skipped
	// LoopsBucket is the NATS KV bucket name holding agent-loop state.
	// read_loop_result + flow_monitor both read from it. Empty falls back
	// to "AGENT_LOOPS". One bucket per process — wiring is boot-time so
	// the name is frozen at RegisterBuiltins for the lifetime of the
	// process.
	LoopsBucket string
	// RestrictedDecideActions is the deployment-level decide-action
	// restriction policy (gh#239): decide action names barred for EVERY
	// coordinator task (front-door and rule-spawned), composing with and
	// taking precedence over the per-task action_allowlist. Sourced from
	// the agentic-tools component config's restricted_decide_actions at
	// boot (main.go) and frozen for the process lifetime, like LoopsBucket.
	// nil/empty = permissive default. Vocabulary-agnostic: a product shell
	// maps its run mode (e.g. autonomous) onto this list.
	RestrictedDecideActions []string
	// SkipBuiltins is the list of builtin group keys to NOT register.
	// Product shells set this when they want to register their own
	// implementation under a canonical tool name (e.g., a chain-scoped
	// `bash` wrapper for sandbox isolation).
	//
	// Valid group keys are defined in BuiltinGroupKeys. Unknown names
	// cause RegisterBuiltins to return an error before any registration
	// happens — typos surface loudly at boot, not silently as "wait,
	// why isn't my replacement tool being called?".
	//
	// For single-tool registrations (e.g., bash, decide), the group key
	// IS the tool name. For multi-tool registrations (e.g., graph_query
	// advertises five query_* tools, rules advertises five rule_* CRUD
	// tools), the group key is the register-function's domain label
	// and skips all tools registered by that function as a unit. See
	// BuiltinGroupKeys for the full mapping.
	//
	// Empty/nil = today's behaviour: every builtin registers.
	SkipBuiltins []string
}

// BuiltinGroupKeys is the closed set of valid SkipBuiltins entries.
// For single-tool register functions the key matches the tool name;
// for multi-tool register functions the key is a domain label that
// skips the whole group as a unit (the registry can't partially
// register one executor's ListTools() output).
//
// The slice is exported so external callers (and tests) can iterate
// for validation, documentation, or "skip everything except X"
// derivation. Order is stable for golden-test reproducibility.
var BuiltinGroupKeys = []string{
	// Single-tool registrations: key == tool name
	"bash",
	"web_search",
	"http_request",
	"read_loop_result",
	"decide",
	"emit_diagnosis",
	"emit_lesson",
	"write_todos",
	"scratchpad",
	"summarize_graph",
	"search_graph",
	"flow_monitor",
	// Multi-tool registrations: key is the register-function's domain
	"graph_query",       // registerGraphQuery — query_entity + 4 others
	"rules",             // registerRules — rule CRUD tools
	"flows",             // registerFlows — flow CRUD tools
	"personas",          // registerPersonas — persona CRUD tools
	"flow_templates",    // registerFlowTemplates — flow_template CRUD tools
	"component_catalog", // registerComponentCatalog — list_components
	"flow_lifecycle",    // registerFlowLifecycle — deploy/start/stop/undeploy_flow
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
//   - Pre-condition skips (nil manager, missing env var) are intentional
//     disable paths. They log and proceed — not an error from this function's
//     perspective. Owned KV buckets bind lazily during execution instead of
//     disabling tools at boot.
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

	skip, err := resolveSkipSet(deps.SkipBuiltins)
	if err != nil {
		return fmt.Errorf("RegisterBuiltins: %w", err)
	}

	var errs []error
	track := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	// gate runs fn() only when key is not in the skip set. Each
	// register_* function is wrapped through this so the gating policy
	// (skip vs register, log on skip) lives in one place and stays
	// uniform across single-tool and multi-tool registrations.
	gate := func(key string, fn func() error) {
		if skip[key] {
			logger.Debug("Skipping builtin tool group (SkipBuiltins)",
				"group", key,
				"hint", "product shell will register its own implementation under this name")
			return
		}
		track(fn())
	}

	gate("bash", func() error { return registerBash(reg, logger) })
	gate("web_search", func() error { return registerWebSearch(reg, deps.NATSClient, deps.Platform, logger) })
	gate("http_request", func() error { return registerHTTPRequest(reg, deps.NATSClient, deps.Platform, logger) })

	if deps.NATSClient == nil {
		logger.Warn("nats client not available; skipping stateful tool registration (read_loop_result, decide, emit_diagnosis, emit_lesson, query_entity); web_search and http_request fall back to text-only return without graph emission")
	} else {
		gate("read_loop_result", func() error { return registerReadLoopResult(reg, deps.NATSClient, logger, loopsBucket) })
		gate("decide", func() error {
			return registerDecide(reg, deps.NATSClient, deps.Platform, deps.RestrictedDecideActions, logger)
		})
		gate("emit_diagnosis", func() error { return registerEmitDiagnosis(reg, deps.NATSClient, deps.Platform, logger) })
		gate("emit_lesson", func() error { return registerEmitLesson(reg, deps.NATSClient, deps.Platform, logger) })
		gate("graph_query", func() error { return registerGraphQuery(ctx, reg, deps.NATSClient, logger) })
		gate("write_todos", func() error {
			if deps.MutationClient == nil {
				return errWriteTodosMutationClientRequired
			}
			return registerWriteTodos(reg, deps.MutationClient, deps.Platform, logger)
		})
		gate("scratchpad", func() error { return registerScratchpad(reg, deps.NATSClient, deps.Platform, logger) })
		// Gateway-first discovery tools (PR #54 step 2): thin wrappers
		// over the new graph.query.summary + graph.query.searchGraph
		// server-side resolvers. Read-only, no platform identity
		// required. See project_graph_tools_gateway_first_plan memory.
		gate("summarize_graph", func() error { return registerSummarizeGraph(reg, deps.NATSClient, logger) })
		gate("search_graph", func() error { return registerSearchGraph(reg, deps.NATSClient, logger) })
	}

	// Pattern-B registry-backed tools. A nil manager is a legal skip;
	// duplicate-name failures propagate.
	gate("rules", func() error { return registerRules(reg, deps.RuleManager, logger) })
	gate("flows", func() error { return registerFlows(reg, deps.FlowManager, logger) })
	gate("personas", func() error { return registerPersonas(reg, deps.PersonaManager, logger) })
	gate("flow_templates", func() error { return registerFlowTemplates(reg, deps.FlowTemplateManager, logger) })
	gate("component_catalog", func() error { return registerComponentCatalog(reg, deps.ComponentRegistry, logger) })
	gate("flow_monitor", func() error { return registerFlowMonitor(reg, deps.NATSClient, deps.FlowManager, logger, loopsBucket) })
	gate("flow_lifecycle", func() error { return registerFlowLifecycle(reg, deps.FlowEngineManager, logger) })

	if len(errs) > 0 {
		return fmt.Errorf("RegisterBuiltins: %w", errors.Join(errs...))
	}
	return nil
}

// resolveSkipSet validates the skip list against BuiltinGroupKeys and
// returns a lookup map. An unknown name returns an error referencing
// the full valid set so operators can self-correct without grepping
// the source.
//
// Empty/nil input is the common case (today's behaviour); returns
// (nil, nil) — the gate() helper treats nil maps as "skip nothing".
func resolveSkipSet(skipBuiltins []string) (map[string]bool, error) {
	if len(skipBuiltins) == 0 {
		return nil, nil
	}
	valid := make(map[string]bool, len(BuiltinGroupKeys))
	for _, k := range BuiltinGroupKeys {
		valid[k] = true
	}
	skip := make(map[string]bool, len(skipBuiltins))
	for _, name := range skipBuiltins {
		if !valid[name] {
			return nil, fmt.Errorf("unknown builtin group key %q in SkipBuiltins; valid keys: %v", name, BuiltinGroupKeys)
		}
		skip[name] = true
	}
	return skip, nil
}
