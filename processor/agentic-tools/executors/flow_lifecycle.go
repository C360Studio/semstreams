package executors

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
)

// FlowEngineManager is the subset of the flow engine's lifecycle surface
// that FlowLifecycleExecutor needs. Declared as an interface so tests can
// substitute an in-memory fake without depending on the full
// *flowengine.Engine type — which itself transitively pulls in the
// component registry, NATS, and the metrics registry. *flowengine.Engine
// satisfies it by duck typing; signatures match engine/engine.go:91,
// 135, 173, 211 verbatim.
//
// Deploy / Start / Stop / Undeploy mirror the engine's runtime-state
// transitions: not_deployed → deployed → running → stopped → undeployed.
// Errors from the engine surface verbatim — including transition
// pre-condition violations — so the agent gets the engine's wrapped
// diagnostic rather than a tool-side rephrasing.
type FlowEngineManager interface {
	Deploy(ctx context.Context, flowID string) error
	Start(ctx context.Context, flowID string) error
	Stop(ctx context.Context, flowID string) error
	Undeploy(ctx context.Context, flowID string) error
}

// FlowLifecycleExecutor implements the runtime lifecycle tools that
// complement FlowExecutor's CRUD surface. CRUD writes the flow
// definition; lifecycle moves the deployed instance through its state
// machine. The two are separate executors so an operator can allow
// authoring (create_flow / update_flow) without enabling deployment
// (deploy_flow / start_flow), or vice-versa, via SkipBuiltins or
// approval_required gating.
//
// Companion to ADR-042 (semteams): coordinator persona issues
// create_flow → deploy_flow → start_flow at runtime; ComponentManager
// picks up the deployed flow from semstreams_config KV and spins
// components dynamically.
type FlowLifecycleExecutor struct {
	manager FlowEngineManager
}

// NewFlowLifecycleExecutor creates a flow lifecycle executor.
func NewFlowLifecycleExecutor(manager FlowEngineManager) *FlowLifecycleExecutor {
	return &FlowLifecycleExecutor{manager: manager}
}

// ListTools returns the four lifecycle tool definitions. All four take
// the same single required parameter (flow_id) — the lifecycle is
// stateless from the tool's perspective; the engine owns the
// transition pre-condition checks and surfaces violations as errors.
func (e *FlowLifecycleExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "deploy_flow",
			Description: "Deploy a flow definition into the runtime. Transitions the flow from not_deployed → deployed. The flow must already exist (use create_flow first). After deploy, call start_flow to begin processing.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to deploy.",
					},
				},
				"required": []string{"flow_id"},
			},
		},
		{
			Name:        "start_flow",
			Description: "Start a deployed flow. Transitions the flow from deployed → running. The flow must already be deployed (use deploy_flow first).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to start.",
					},
				},
				"required": []string{"flow_id"},
			},
		},
		{
			Name:        "stop_flow",
			Description: "Stop a running flow. Transitions the flow from running → stopped. Components remain deployed; call start_flow to resume or undeploy_flow to tear down.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to stop.",
					},
				},
				"required": []string{"flow_id"},
			},
		},
		{
			Name:        "undeploy_flow",
			Description: "Undeploy a flow from the runtime. Transitions the flow from stopped (or deployed) → not_deployed. Tears down the runtime components; the flow definition remains and can be re-deployed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to undeploy.",
					},
				},
				"required": []string{"flow_id"},
			},
		},
	}
}

// Execute dispatches flow lifecycle tool calls by name.
func (e *FlowLifecycleExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "deploy_flow":
		return e.runLifecycle(ctx, call, "deploy", e.manager.Deploy)
	case "start_flow":
		return e.runLifecycle(ctx, call, "start", e.manager.Start)
	case "stop_flow":
		return e.runLifecycle(ctx, call, "stop", e.manager.Stop)
	case "undeploy_flow":
		return e.runLifecycle(ctx, call, "undeploy", e.manager.Undeploy)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// runLifecycle factors the four lifecycle tools' shared shape — extract
// flow_id, call the engine method, translate transition errors to
// ToolResult.Error. Kept inline rather than dispatched via a map of
// name → method because the four methods are method-values on
// FlowEngineManager and Go can't form method-value maps without an
// extra adapter layer that obscures the call site.
func (e *FlowLifecycleExecutor) runLifecycle(
	ctx context.Context,
	call agentic.ToolCall,
	verb string,
	op func(ctx context.Context, flowID string) error,
) (agentic.ToolResult, error) {
	flowID, _ := call.Arguments["flow_id"].(string)
	if flowID == "" {
		return agentic.ToolResult{CallID: call.ID, Error: "flow_id is required"}, nil
	}
	if err := op(ctx, flowID); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("%s failed: %v", verb, err)}, nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Flow %q %s succeeded.", flowID, verbPastTense(verb)),
	}, nil
}

// verbPastTense maps the lifecycle verbs to their past-tense form for
// the success message. Tiny helper kept local — only used here.
func verbPastTense(verb string) string {
	switch verb {
	case "deploy":
		return "deployed"
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "undeploy":
		return "undeployed"
	default:
		return verb + "ed"
	}
}
