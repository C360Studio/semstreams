package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// FlowMonitorToolName is the tool name agents use to invoke monitor_flow.
const FlowMonitorToolName = "monitor_flow"

// defaultRecentLimit is the default cap on recent loop entries returned.
const defaultRecentLimit = 20

// LoopKVScanner is the minimal KV surface monitor_flow needs: list keys
// and fetch individual entries. *natsclient.KVStore satisfies this interface
// by duck-typing. Declared here so unit tests can inject an in-memory fake
// without pulling in the NATS stack.
type LoopKVScanner interface {
	// Keys returns all current keys in the bucket (deleted keys excluded).
	Keys(ctx context.Context) ([]string, error)
	// Get fetches a single KV entry by exact key.
	Get(ctx context.Context, key string) (*natsclient.KVEntry, error)
}

// FlowStateReader is the minimal flowstore surface needed to read
// runtime_state. FlowManager (declared in executors package) already
// satisfies this — we use a separate interface here to keep
// agentic-tools free of a direct dependency on the executors package.
type FlowStateReader interface {
	Get(ctx context.Context, id string) (FlowState, error)
}

// FlowState is the minimal projection of flowstore.Flow that monitor_flow
// needs. Returned by FlowStateReader.
type FlowState struct {
	RuntimeState string
}

// FlowMonitorExecutor aggregates completed-loop data for a given flow.
// It scans the AGENT_LOOPS KV bucket for COMPLETE_* keys, filters by
// WorkflowSlug, and returns aggregate counts plus a recency-capped list.
type FlowMonitorExecutor struct {
	kv     LoopKVScanner
	flows  FlowStateReader
	logger *slog.Logger
}

// NewFlowMonitorExecutor constructs the executor with its KV handle and
// flow state reader. Both must be non-nil; callers that can't satisfy
// either should skip registration (see executors.registerFlowMonitor).
func NewFlowMonitorExecutor(kv LoopKVScanner, flows FlowStateReader, logger *slog.Logger) *FlowMonitorExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &FlowMonitorExecutor{kv: kv, flows: flows, logger: logger}
}

// ListTools describes the monitor_flow tool.
func (e *FlowMonitorExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        FlowMonitorToolName,
			Description: "Return aggregated runtime statistics for a flow: total completed loops, breakdown by outcome and role, total token usage, and a recency-capped list of individual loop records. Scans the AGENT_LOOPS KV bucket for completions whose workflow_slug matches flow_id. Note: flow_id must match the workflow_slug used at dispatch time.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flow_id": map[string]any{
						"type":        "string",
						"description": "ID of the flow to monitor. Must match the workflow_slug set at task dispatch time.",
					},
					"recent_limit": map[string]any{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum number of recent loop records to include (default %d). Set lower to reduce context pressure.", defaultRecentLimit),
					},
				},
				"required": []string{"flow_id"},
			},
		},
	}
}

// Execute routes the tool call to the handler.
func (e *FlowMonitorExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != FlowMonitorToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "FlowMonitorExecutor", "Execute", "route tool")
	}
	return e.monitorFlow(ctx, call)
}

// loopRecentEntry is one entry in the recent list.
type loopRecentEntry struct {
	LoopID      string `json:"loop_id"`
	Role        string `json:"role"`
	Outcome     string `json:"outcome"`
	CompletedAt string `json:"completed_at"`
	Iterations  int    `json:"iterations"`
	TokensIn    int    `json:"tokens_in"`
	TokensOut   int    `json:"tokens_out"`
}

// flowMonitorResult is the JSON shape returned to the LLM.
type flowMonitorResult struct {
	FlowID         string            `json:"flow_id"`
	RuntimeState   string            `json:"runtime_state"`
	TotalLoops     int               `json:"total_loops"`
	ByOutcome      map[string]int    `json:"by_outcome"`
	ByRole         map[string]int    `json:"by_role"`
	TotalTokensIn  int               `json:"total_tokens_in"`
	TotalTokensOut int               `json:"total_tokens_out"`
	Recent         []loopRecentEntry `json:"recent"`
}

func (e *FlowMonitorExecutor) monitorFlow(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	flowID, _ := call.Arguments["flow_id"].(string)
	if flowID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "flow_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	recentLimit := parsePositiveInt(call.Arguments["recent_limit"], defaultRecentLimit)

	// 1. Fetch flow runtime state.
	runtimeState := "unknown"
	if e.flows != nil {
		fs, err := e.flows.Get(ctx, flowID)
		if err != nil {
			// Not found is not fatal — return aggregate with unknown state.
			e.logger.Warn("monitor_flow: could not fetch flow state",
				slog.String("flow_id", flowID), slog.Any("error", err))
		} else {
			runtimeState = fs.RuntimeState
		}
	}

	// 2. Scan all COMPLETE_* keys in the loops bucket.
	keys, err := e.kv.Keys(ctx)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to list loops bucket: %v", err),
			ErrorKind: agentic.ToolErrorExternal,
		}, errs.WrapTransient(err, "FlowMonitorExecutor", "monitorFlow", "list keys")
	}

	result := &flowMonitorResult{
		FlowID:       flowID,
		RuntimeState: runtimeState,
		ByOutcome:    map[string]int{},
		ByRole:       map[string]int{},
		Recent:       []loopRecentEntry{},
	}

	for _, key := range keys {
		if !strings.HasPrefix(key, completeKeyPrefix) {
			continue
		}

		entry, err := e.kv.Get(ctx, key)
		if err != nil {
			e.logger.Warn("monitor_flow: skipping unreadable key",
				slog.String("key", key), slog.Any("error", err))
			continue
		}

		var event agentic.LoopCompletedEvent
		if err := json.Unmarshal(entry.Value, &event); err != nil {
			e.logger.Warn("monitor_flow: skipping malformed completion record",
				slog.String("key", key), slog.Any("error", err))
			continue
		}

		if event.WorkflowSlug != flowID {
			continue
		}

		// 3. Accumulate aggregates.
		result.TotalLoops++
		result.ByOutcome[event.Outcome]++
		result.ByRole[event.Role]++
		result.TotalTokensIn += event.TokensIn
		result.TotalTokensOut += event.TokensOut

		result.Recent = append(result.Recent, loopRecentEntry{
			LoopID:      event.LoopID,
			Role:        event.Role,
			Outcome:     event.Outcome,
			CompletedAt: event.CompletedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Iterations:  event.Iterations,
			TokensIn:    event.TokensIn,
			TokensOut:   event.TokensOut,
		})
	}

	// 4. Sort recent descending by completed_at, then cap.
	sort.Slice(result.Recent, func(i, j int) bool {
		return result.Recent[i].CompletedAt > result.Recent[j].CompletedAt
	})
	if len(result.Recent) > recentLimit {
		result.Recent = result.Recent[:recentLimit]
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal result: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "FlowMonitorExecutor", "monitorFlow", "marshal")
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(data),
	}, nil
}
