package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// ReadLoopResultToolName is the name agents use to invoke the read_loop_result tool.
const ReadLoopResultToolName = "read_loop_result"

// completeKeyPrefix is the key prefix for completion events written by the
// agentic-loop component to the AGENT_LOOPS KV bucket (see
// processor/agentic-loop/component.go storeCompletionEvent). Full key is
// completeKeyPrefix + loopID.
const completeKeyPrefix = "COMPLETE_"

// defaultReadLoopResultChunk is the max_bytes used when the caller omits the
// argument. Sized to fit small-model context windows comfortably while
// returning enough content for most summaries in one shot.
const defaultReadLoopResultChunk = 4 * 1024

// LoopsKVReader is the minimal surface this executor consumes. It matches
// natsclient.KVStore.Get exactly, so production callers pass a *KVStore
// directly and tests supply an in-memory fake. Keeping the interface tiny
// and typed on natsclient.KVEntry avoids re-adapting jetstream types here —
// natsclient already owns that translation (see natsclient/kv.go).
type LoopsKVReader interface {
	Get(ctx context.Context, key string) (*natsclient.KVEntry, error)
}

// ReadLoopResultExecutor fetches a completed loop's full Result from the
// AGENT_LOOPS KV bucket so downstream agents can read another loop's output
// without having it injected into their prompt. Supports paging (max_bytes,
// offset) so small-context-window agents can consume large outputs a slice
// at a time.
type ReadLoopResultExecutor struct {
	kv LoopsKVReader
}

// NewReadLoopResultExecutor constructs the executor against a KV reader
// scoped to the loops bucket (AGENT_LOOPS by default).
func NewReadLoopResultExecutor(kv LoopsKVReader) *ReadLoopResultExecutor {
	return &ReadLoopResultExecutor{kv: kv}
}

// ListTools describes the read_loop_result tool.
func (e *ReadLoopResultExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        ReadLoopResultToolName,
			Description: "Fetch the final Result text a previously completed agent loop produced, so this agent can react to or build on it without having the full content injected into its prompt. Returns a text slice plus role/outcome/completed_at metadata and total_bytes so the caller can page through long results by calling again with a different offset.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"loop_id": map[string]any{
						"type":        "string",
						"description": "ID of the completed loop to fetch. Obtain from the rule substitution $entity.id or from another tool (query_entity, list loops).",
					},
					"max_bytes": map[string]any{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum bytes of Result text to return in this call. Defaults to %d. Use smaller values to stay within tight context budgets.", defaultReadLoopResultChunk),
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based byte offset into the Result text. Defaults to 0. Use with max_bytes to page through long results.",
					},
				},
				"required": []string{"loop_id"},
			},
		},
	}
}

// Execute routes the tool call to the handler.
func (e *ReadLoopResultExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != ReadLoopResultToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "ReadLoopResultExecutor", "Execute", "route tool")
	}
	return e.readLoopResult(ctx, call)
}

func (e *ReadLoopResultExecutor) readLoopResult(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	loopID, ok := call.Arguments["loop_id"].(string)
	if !ok || loopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "loop_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	maxBytes := parsePositiveInt(call.Arguments["max_bytes"], defaultReadLoopResultChunk)
	offset := parsePositiveInt(call.Arguments["offset"], 0)

	entry, err := e.kv.Get(ctx, completeKeyPrefix+loopID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("no completion record for loop %s (loop may still be running or never existed)", loopID),
				ErrorKind: agentic.ToolErrorNotFound,
			}, nil
		}
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to read completion record: %v", err),
			ErrorKind: agentic.ToolErrorExternal,
		}, errs.WrapTransient(err, "ReadLoopResultExecutor", "readLoopResult", "get completion entry")
	}

	var event agentic.LoopCompletedEvent
	if err := json.Unmarshal(entry.Value, &event); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("completion record for loop %s is malformed: %v", loopID, err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "ReadLoopResultExecutor", "readLoopResult", "unmarshal completion event")
	}

	total := len(event.Result)
	start := min(offset, total)
	end := min(start+maxBytes, total)
	slice := event.Result[start:end]

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: slice,
		Metadata: map[string]any{
			"loop_id":        loopID,
			"role":           event.Role,
			"outcome":        event.Outcome,
			"completed_at":   event.CompletedAt,
			"total_bytes":    total,
			"returned_bytes": len(slice),
			"offset":         start,
			"has_more":       end < total,
			"next_offset":    end,
			"task_id":        event.TaskID,
		},
	}, nil
}

// parsePositiveInt reads an integer argument that may arrive as float64 (the
// JSON decode default for numbers), int, or int64. Returns fallback when the
// value is missing, non-numeric, or negative.
func parsePositiveInt(raw any, fallback int) int {
	switch v := raw.(type) {
	case nil:
		return fallback
	case float64:
		if v < 0 {
			return fallback
		}
		return int(v)
	case int:
		if v < 0 {
			return fallback
		}
		return v
	case int64:
		if v < 0 {
			return fallback
		}
		return int(v)
	default:
		return fallback
	}
}
