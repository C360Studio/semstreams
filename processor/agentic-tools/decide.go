package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// DecideToolName is the name agents use to invoke the coordinator's
// terminal decision tool.
const DecideToolName = "decide"

// decideMutationSubject is the NATS request/reply subject the graph-ingest
// component handles to add triples. Kept consistent with rule and graph
// writer callers (processor/rule/triple_mutator.go, processor/agentic-loop/
// graph_writer.go).
const decideMutationSubject = "graph.mutation.triple.add"

// decideMutationTimeout bounds each per-triple add request.
const decideMutationTimeout = 5 * time.Second

// decideToolSource is the Source field on triples this tool publishes. It
// lets operators distinguish coordinator decisions from rule-driven triple
// mutations at a glance in graph.
const decideToolSource = "coordinator-decide"

// TriplePublisher is the narrow surface DecideExecutor uses to write triples.
// Production satisfies it with a natsclient.Client adapter; tests use an
// in-memory recorder so they don't need a real NATS connection.
type TriplePublisher interface {
	AddTriple(ctx context.Context, triple message.Triple) error
}

// DecideExecutor is the coordinator's terminal tool. A coordinator agent
// calls decide() exactly once to signal its judgment; on success, the tool
// publishes a small set of metadata triples onto the coordinator's loop
// entity so downstream rules match deterministically, and returns
// StopLoop=true with the full decision payload in Content so downstream
// agents can fetch any bulky fields (subtopics list, retry hint) via
// read_loop_result without them riding in triples.
type DecideExecutor struct {
	publisher TriplePublisher
	platform  types.PlatformMeta
}

// NewDecideExecutor constructs the executor given a triple publisher and
// the platform identity used to build the coordinator's loop entity ID.
func NewDecideExecutor(publisher TriplePublisher, platform types.PlatformMeta) *DecideExecutor {
	return &DecideExecutor{publisher: publisher, platform: platform}
}

// ListTools describes the decide tool. The action string is NOT enumerated
// at the tool level — different flows want different terminal actions. The
// coordinator's system prompt enumerates valid values per flow; downstream
// rules match on specific action values via the CoordinatorNextAction
// predicate.
//
// Description must NOT pre-load example action names. Real-LLM models
// (claude-sonnet-class observed against a downstream product flow,
// 2026-05) treat tool descriptions as more authoritative than persona
// prose. When the description named a handful of example actions,
// models biased their terminal choice toward those examples,
// overriding the persona's enumerated value and wedging chains at an
// unhandled coordinator.next_action triple. The action vocabulary
// lives strictly in the role's system prompt; flows that want
// structural enforcement use the per-spawn allowlist threaded through
// TaskMessage.Metadata under MetadataKeyDecideActionAllowlist (see
// agentic/tools.go).
//
// TestDecideExecutor_DescriptionDoesNotPreloadActions guards the
// regression — see its pattern checks for the leak shapes.
func (e *DecideExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        DecideToolName,
			Description: "Terminal decision tool for coordinator agents. Call exactly once with the action your role's system prompt enumerates. Emits a coordinator.next_action triple on this loop's entity so downstream rules can route; the full args stay in the loop's Result for any agent that needs supporting data.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "The decision. Valid values are enumerated in your role's system prompt.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Short natural-language justification for the chosen action.",
					},
					"subtopics": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional. List of sub-targets when the chosen action represents a multi-target investigation; populate only when your role's contract names this field.",
					},
					"retry_hint": map[string]any{
						"type":        "string",
						"description": "Optional. Free-form guidance for a downstream pass; populate only when your role's contract names this field.",
					},
				},
				"required": []string{"action", "reason"},
			},
		},
	}
}

// decideArgs is the parsed shape of the decide tool's Arguments.
type decideArgs struct {
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	Subtopics []string `json:"subtopics,omitempty"`
	RetryHint string   `json:"retry_hint,omitempty"`
}

// Execute routes the tool call to decide; any other tool name is treated
// as a routing bug.
func (e *DecideExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != DecideToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "DecideExecutor", "Execute", "route tool")
	}
	return e.decide(ctx, call)
}

func (e *DecideExecutor) decide(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	args, err := parseDecideArgs(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "decide invoked without a loop_id on the tool call; cannot resolve the coordinator's loop entity",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "DecideExecutor", "decide", "resolve loop entity")
	}

	loopEntityID := agentic.LoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)

	now := time.Now()
	triples := []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorNextAction,
			Object:     args.Action,
			Source:     decideToolSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorDecisionReason,
			Object:     args.Reason,
			Source:     decideToolSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
	}

	for _, triple := range triples {
		if err := e.publisher.AddTriple(ctx, triple); err != nil {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("publish %s triple: %v", triple.Predicate, err),
				ErrorKind: agentic.ToolErrorNetwork,
			}, errs.WrapTransient(err, "DecideExecutor", "decide", "publish triple")
		}
	}

	// The tool's Content is the canonical decision payload. Downstream
	// agents needing the full args (subtopics list, retry_hint) pull it
	// via read_loop_result on this loop's completion.
	payload, err := json.Marshal(args)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal decision payload: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "DecideExecutor", "decide", "marshal payload")
	}

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(payload),
		StopLoop: true,
		Metadata: map[string]any{
			"action":         args.Action,
			"reason":         args.Reason,
			"loop_entity_id": loopEntityID,
			"subtopic_count": len(args.Subtopics),
			"has_retry_hint": args.RetryHint != "",
		},
	}, nil
}

// parseDecideArgs reads the untyped tool arguments into decideArgs and
// enforces the required fields. Missing or wrong-typed action/reason
// surface as invalid-args errors so the framework retry policy can step
// in — small models that miss the schema on the first try get a second
// chance before the loop fails.
func parseDecideArgs(raw map[string]any) (decideArgs, error) {
	action, ok := raw["action"].(string)
	if !ok || action == "" {
		return decideArgs{}, fmt.Errorf("action is required and must be a non-empty string")
	}
	reason, ok := raw["reason"].(string)
	if !ok || reason == "" {
		return decideArgs{}, fmt.Errorf("reason is required and must be a non-empty string")
	}
	args := decideArgs{Action: action, Reason: reason}

	if rawSubtopics, present := raw["subtopics"]; present && rawSubtopics != nil {
		slice, ok := rawSubtopics.([]any)
		if !ok {
			return decideArgs{}, fmt.Errorf("subtopics must be an array of strings")
		}
		args.Subtopics = make([]string, 0, len(slice))
		for i, v := range slice {
			s, ok := v.(string)
			if !ok {
				return decideArgs{}, fmt.Errorf("subtopics[%d] must be a string", i)
			}
			args.Subtopics = append(args.Subtopics, s)
		}
	}

	if hint, present := raw["retry_hint"]; present && hint != nil {
		s, ok := hint.(string)
		if !ok {
			return decideArgs{}, fmt.Errorf("retry_hint must be a string")
		}
		args.RetryHint = s
	}

	return args, nil
}

// natsTriplePublisher adapts natsclient.Client to TriplePublisher by
// issuing a graph.mutation.triple.add request/reply per call. Kept local to
// this file rather than shared because the only current caller is the
// decide executor; other places that publish triples (rule actions, graph
// writer) have their own adapters shaped to their needs.
type natsTriplePublisher struct {
	client *natsclient.Client
}

// NewNATSTriplePublisher builds a TriplePublisher backed by the shared
// graph.mutation.triple.add NATS surface.
func NewNATSTriplePublisher(client *natsclient.Client) TriplePublisher {
	return &natsTriplePublisher{client: client}
}

func (p *natsTriplePublisher) AddTriple(ctx context.Context, triple message.Triple) error {
	req := graph.AddTripleRequest{Triple: triple}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal add-triple request: %w", err)
	}
	// RequestWithRetry handles transient "no responders" errors when
	// graph-gateway is restarting or its subscription hasn't yet
	// propagated. The decide tool's terminal action is the triple
	// downstream rules trigger on — silent failure here breaks the
	// coordinator pattern's workflow. Idempotent (graph is a set of
	// triples), so retry is safe.
	respData, err := p.client.RequestWithRetry(ctx, decideMutationSubject, reqData, decideMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", decideMutationSubject, err)
	}
	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("graph-ingest rejected triple: %s", resp.Error)
	}
	return nil
}
