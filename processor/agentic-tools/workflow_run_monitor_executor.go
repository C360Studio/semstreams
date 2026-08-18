package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// WorkflowRunMonitorToolName is the tool name agents use to inspect completed
// workflow runs.
const WorkflowRunMonitorToolName = "monitor_workflow_runs"

// defaultRecentLimit is the default cap on recent loop entries returned.
const defaultRecentLimit = 20

// LoopKVScanner is the minimal KV surface monitor_workflow_runs needs: list keys
// and fetch individual entries. *natsclient.KVStore satisfies this interface
// by duck-typing. Declared here so unit tests can inject an in-memory fake
// without pulling in the NATS stack.
type LoopKVScanner interface {
	// Keys returns all current keys in the bucket (deleted keys excluded).
	Keys(ctx context.Context) ([]string, error)
	// Get fetches a single KV entry by exact key.
	Get(ctx context.Context, key string) (*natsclient.KVEntry, error)
}

// WorkflowRunMonitorExecutor aggregates completed-loop data for a workflow.
// It scans the AGENT_LOOPS KV bucket for COMPLETE_* keys, filters by
// WorkflowSlug, and returns aggregate counts plus a recency-capped list.
type WorkflowRunMonitorExecutor struct {
	kv     LoopKVScanner
	logger *slog.Logger
}

// NewWorkflowRunMonitorExecutor constructs the executor with its KV handle.
func NewWorkflowRunMonitorExecutor(kv LoopKVScanner, logger *slog.Logger) *WorkflowRunMonitorExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkflowRunMonitorExecutor{kv: kv, logger: logger}
}

// ListTools describes the monitor_workflow_runs tool.
func (e *WorkflowRunMonitorExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        WorkflowRunMonitorToolName,
			Description: "Return aggregate statistics for completed workflow runs: totals by outcome and role, token usage, and a recency-capped list of individual loop records. Scans AGENT_LOOPS entries whose workflow_slug matches the requested workflow_slug.",
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workflow_slug": map[string]any{
						"type":        "string",
						"description": "Workflow slug set when the task was dispatched.",
					},
					"recent_limit": map[string]any{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum number of recent loop records to include (default %d). Set lower to reduce context pressure.", defaultRecentLimit),
					},
				},
				"required": []string{"workflow_slug"},
			},
		},
	}
}

// Execute routes the tool call to the handler.
func (e *WorkflowRunMonitorExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != WorkflowRunMonitorToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "WorkflowRunMonitorExecutor", "Execute", "route tool")
	}
	return e.monitorWorkflowRuns(ctx, call)
}

// eventDiscriminator is used as a first-pass unmarshal to identify which
// event shape (LoopCompletedEvent, LoopFailedEvent, LoopCancelledEvent) is
// stored under a COMPLETE_* key, without having to try each type blindly.
type eventDiscriminator struct {
	Outcome string `json:"outcome"`
	LoopID  string `json:"loop_id"`
}

// terminalEvent is the normalised in-memory representation of any COMPLETE_*
// KV entry, regardless of which concrete event shape was stored. It carries
// only the fields that the aggregation and sort code needs.
type terminalEvent struct {
	loopID       string
	workflowSlug string
	role         string
	outcome      string
	terminalAt   time.Time
	iterations   int
	tokensIn     int
	tokensOut    int
}

// decodeTerminalEvent unmarshals raw JSON from a COMPLETE_* KV entry into a
// terminalEvent. It dispatches on the "outcome" discriminator field so each
// concrete event type contributes the correct timestamp and fields.
// Unknown outcomes, invalid payloads, and missing terminal timestamps fail
// closed because a COMPLETE_* key is authoritative terminal state.
func decodeTerminalEvent(data []byte) (terminalEvent, error) {
	var disc eventDiscriminator
	if err := json.Unmarshal(data, &disc); err != nil {
		return terminalEvent{}, fmt.Errorf("decode terminal discriminator: %w", err)
	}

	switch disc.Outcome {
	case agentic.OutcomeSuccess:
		var ev agentic.LoopCompletedEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return terminalEvent{}, fmt.Errorf("decode completed event: %w", err)
		}
		if err := ev.Validate(); err != nil {
			return terminalEvent{}, fmt.Errorf("validate completed event: %w", err)
		}
		if ev.CompletedAt.IsZero() {
			return terminalEvent{}, fmt.Errorf("validate completed event: completed_at required")
		}
		return terminalEvent{
			loopID:       ev.LoopID,
			workflowSlug: ev.WorkflowSlug,
			role:         ev.Role,
			outcome:      ev.Outcome,
			terminalAt:   ev.CompletedAt,
			iterations:   ev.Iterations,
			tokensIn:     ev.TokensIn,
			tokensOut:    ev.TokensOut,
		}, nil

	case agentic.OutcomeFailed:
		var ev agentic.LoopFailedEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return terminalEvent{}, fmt.Errorf("decode failed event: %w", err)
		}
		if err := ev.Validate(); err != nil {
			return terminalEvent{}, fmt.Errorf("validate failed event: %w", err)
		}
		if ev.FailedAt.IsZero() {
			return terminalEvent{}, fmt.Errorf("validate failed event: failed_at required")
		}
		return terminalEvent{
			loopID:       ev.LoopID,
			workflowSlug: ev.WorkflowSlug,
			role:         ev.Role,
			outcome:      ev.Outcome,
			terminalAt:   ev.FailedAt,
			iterations:   ev.Iterations,
			tokensIn:     ev.TokensIn,
			tokensOut:    ev.TokensOut,
		}, nil

	case agentic.OutcomeCancelled:
		var ev agentic.LoopCancelledEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return terminalEvent{}, fmt.Errorf("decode cancelled event: %w", err)
		}
		if err := ev.Validate(); err != nil {
			return terminalEvent{}, fmt.Errorf("validate cancelled event: %w", err)
		}
		if ev.CancelledAt.IsZero() {
			return terminalEvent{}, fmt.Errorf("validate cancelled event: cancelled_at required")
		}
		// LoopCancelledEvent has no Role field; role stays empty.
		return terminalEvent{
			loopID:       ev.LoopID,
			workflowSlug: ev.WorkflowSlug,
			outcome:      ev.Outcome,
			terminalAt:   ev.CancelledAt,
		}, nil

	default:
		return terminalEvent{}, fmt.Errorf("unknown terminal outcome %q", disc.Outcome)
	}
}

// loopRecentEntry is one entry in the recent list.
type loopRecentEntry struct {
	LoopID  string `json:"loop_id"`
	Role    string `json:"role"`
	Outcome string `json:"outcome"`
	// completed_at carries completed_at, failed_at, or cancelled_at depending
	// on the outcome — it is always the terminal timestamp for this loop.
	CompletedAt string `json:"completed_at"`
	Iterations  int    `json:"iterations"`
	TokensIn    int    `json:"tokens_in"`
	TokensOut   int    `json:"tokens_out"`
	terminalAt  time.Time
}

// workflowRunMonitorResult is the JSON shape returned to the LLM.
type workflowRunMonitorResult struct {
	WorkflowSlug   string            `json:"workflow_slug"`
	TotalLoops     int               `json:"total_loops"`
	ByOutcome      map[string]int    `json:"by_outcome"`
	ByRole         map[string]int    `json:"by_role"`
	TotalTokensIn  int               `json:"total_tokens_in"`
	TotalTokensOut int               `json:"total_tokens_out"`
	Recent         []loopRecentEntry `json:"recent"`
}

func (e *WorkflowRunMonitorExecutor) monitorWorkflowRuns(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	workflowSlug, _ := call.Arguments["workflow_slug"].(string)
	if workflowSlug == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "workflow_slug is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	recentLimit := parsePositiveInt(call.Arguments["recent_limit"], defaultRecentLimit)

	// Scan all COMPLETE_* keys in the loops bucket.
	keys, err := e.kv.Keys(ctx)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to list loops bucket: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "WorkflowRunMonitorExecutor", "monitorWorkflowRuns", "list keys")
	}

	result := &workflowRunMonitorResult{
		WorkflowSlug: workflowSlug,
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
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("failed to read completed loop %q: %v", key, err),
				ErrorKind: agentic.ToolErrorNetwork,
			}, errs.WrapTransient(err, "WorkflowRunMonitorExecutor", "monitorWorkflowRuns", "read completed loop")
		}

		ev, err := decodeTerminalEvent(entry.Value)
		if err != nil {
			return corruptWorkflowRunResult(call.ID, key, err)
		}
		// A valid production decode is the only evidence that a COMPLETE_*
		// record belongs to another workflow and may be ignored.
		if ev.workflowSlug != workflowSlug {
			continue
		}

		// 3. Accumulate aggregates.
		result.TotalLoops++
		result.ByOutcome[ev.outcome]++
		// Only credit by_role when a role is present. Failed/cancelled events
		// without a role field must not pollute by_role with an empty key.
		if ev.role != "" {
			result.ByRole[ev.role]++
		}
		result.TotalTokensIn += ev.tokensIn
		result.TotalTokensOut += ev.tokensOut

		result.Recent = append(result.Recent, loopRecentEntry{
			LoopID:      ev.loopID,
			Role:        ev.role,
			Outcome:     ev.outcome,
			CompletedAt: ev.terminalAt.UTC().Format(time.RFC3339Nano),
			Iterations:  ev.iterations,
			TokensIn:    ev.tokensIn,
			TokensOut:   ev.tokensOut,
			terminalAt:  ev.terminalAt,
		})
	}

	// 4. Sort recent descending by terminal timestamp (completed_at field carries
	// completed_at/failed_at/cancelled_at depending on outcome), then cap.
	sort.Slice(result.Recent, func(i, j int) bool {
		if result.Recent[i].terminalAt.Equal(result.Recent[j].terminalAt) {
			return result.Recent[i].LoopID < result.Recent[j].LoopID
		}
		return result.Recent[i].terminalAt.After(result.Recent[j].terminalAt)
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
		}, errs.WrapInvalid(err, "WorkflowRunMonitorExecutor", "monitorWorkflowRuns", "marshal")
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(data),
	}, nil
}

func corruptWorkflowRunResult(callID, key string, cause error) (agentic.ToolResult, error) {
	return agentic.ToolResult{
		CallID:    callID,
		Error:     fmt.Sprintf("invalid completed loop %q: %v", key, cause),
		ErrorKind: agentic.ToolErrorInternal,
	}, errs.WrapInvalid(cause, "WorkflowRunMonitorExecutor", "monitorWorkflowRuns", "decode completed loop")
}
