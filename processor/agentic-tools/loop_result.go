package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage/objectstore"
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

// LoopContentFetcher resolves an offloaded result body from the content
// ObjectStore (payload-size-chokepoints D4): completion values whose result
// exceeded the offload threshold carry a storage_ref instead of the inline
// text. *objectstore.Store satisfies this interface; tests supply a fake.
type LoopContentFetcher interface {
	FetchContent(ctx context.Context, ref *message.StorageReference) (*objectstore.StoredContent, error)
}

// ReadLoopResultExecutor fetches a completed loop's full Result from the
// AGENT_LOOPS KV bucket so downstream agents can read another loop's output
// without having it injected into their prompt. Supports paging (max_bytes,
// offset) so small-context-window agents can consume large outputs a slice
// at a time. Offloaded results (storage_ref-bearing completion values) are
// hydrated from the content store transparently — the paging contract is
// unchanged and pages over the hydrated content.
type ReadLoopResultExecutor struct {
	kv      LoopsKVReader
	content LoopContentFetcher
}

// NewReadLoopResultExecutor constructs the executor against a KV reader
// scoped to the loops bucket (AGENT_LOOPS by default) and a content fetcher
// for offloaded result bodies (AGENT_CONTENT by default). content may be nil
// when no content store is available — inline results still work and
// ref-bearing results return a typed error naming the gap instead of
// silently serving the preview as if it were the whole result.
func NewReadLoopResultExecutor(kv LoopsKVReader, content LoopContentFetcher) *ReadLoopResultExecutor {
	return &ReadLoopResultExecutor{kv: kv, content: content}
}

// ListTools describes the read_loop_result tool.
//
// Paginated: true declares this tool follows the canonical pagination
// contract (agentic.MetadataKeyHasMore + MetadataKeyNextOffset). The
// agent loop reads has_more in buildToolMessages and appends a
// continuation hint to the model's next message when more pages
// remain; the executor sets the metadata fields in readLoopResult.
func (e *ReadLoopResultExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        ReadLoopResultToolName,
			Description: "Fetch the final Result text a previously completed agent loop produced, so this agent can react to or build on it without having the full content injected into its prompt. Returns a text slice plus role/outcome/completed_at metadata and total_bytes so the caller can page through long results by calling again with a different offset.",
			Paginated:   true,
			Effect:      agentic.ToolEffectReadOnly,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"loop_id": map[string]any{
						"type":        "string",
						"description": "Loop ID. Use $entity.instance in rule templates (bare UUID); the full 6-part entity ID also works.",
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
	rawLoopID, ok := call.Arguments["loop_id"].(string)
	if !ok || rawLoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "loop_id is required and must be a non-empty string",
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	loopID := normalizeLoopID(rawLoopID)

	maxBytes := parsePositiveInt(call.Arguments["max_bytes"], defaultReadLoopResultChunk)
	offset := parsePositiveInt(call.Arguments["offset"], 0)

	entry, err := e.kv.Get(ctx, completeKeyPrefix+loopID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			// Blocker 3: a missing COMPLETE_ is NOT one answer. Consult the
			// loop entity (same bucket, key <loopID>): a result-not-durable
			// marker means the loop TERMINATED but its record could not be
			// stored — a typed error distinct from both "still running" and
			// "never existed", which stay distinct from each other.
			return e.absentCompletionResult(ctx, call, loopID), nil
		}
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("failed to read completion record: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
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

	// Offloaded results (D4): hydrate the full body from the content store,
	// then page over the HYDRATED content — max_bytes/offset semantics are
	// identical to the inline case.
	resultText := event.Result
	offloaded := event.ResultRef != nil
	if offloaded {
		full, err := e.hydrateResult(ctx, event.ResultRef)
		if err != nil {
			kind := agentic.ToolErrorNetwork
			if errs.Classify(err) != errs.ErrorTransient {
				kind = agentic.ToolErrorInternal
			}
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("result for loop %s is offloaded but could not be hydrated: %v", loopID, err),
				ErrorKind: kind,
			}, err
		}
		resultText = full
	}

	total := len(resultText)
	start := min(offset, total)
	end := min(start+maxBytes, total)
	slice := resultText[start:end]

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: slice,
		Metadata: map[string]any{
			"loop_id":                     loopID,
			"role":                        event.Role,
			"outcome":                     event.Outcome,
			"completed_at":                event.CompletedAt,
			"returned_bytes":              len(slice),
			"offset":                      start,
			"task_id":                     event.TaskID,
			"result_offloaded":            offloaded,
			agentic.MetadataKeyHasMore:    end < total,
			agentic.MetadataKeyNextOffset: end,
			agentic.MetadataKeyTotalBytes: total,
		},
	}, nil
}

// absentCompletionResult answers a missing COMPLETE_ record by consulting the
// loop entity (Blocker 3). Answers, in order of specificity:
//   - entity carries the result-not-durable marker → typed result-not-durable
//     error naming the classified cause (the loop IS terminal; retrying the
//     read cannot help; the parent must branch on the failure).
//   - entity exists and is non-terminal → "still running".
//   - entity exists, terminal, no marker → record absent (likely a
//     persist/read race); named as such.
//   - no entity → "never existed".
func (e *ReadLoopResultExecutor) absentCompletionResult(ctx context.Context, call agentic.ToolCall, loopID string) agentic.ToolResult {
	entry, err := e.kv.Get(ctx, loopID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("no completion record and no loop entity for %s (loop never existed)", loopID),
				ErrorKind: agentic.ToolErrorNotFound,
			}
		}
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("no completion record for loop %s and the loop entity could not be read: %v", loopID, err),
			ErrorKind: agentic.ToolErrorNetwork,
		}
	}
	var entity agentic.LoopEntity
	if uerr := json.Unmarshal(entry.Value, &entity); uerr != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("no completion record for loop %s and its loop entity is malformed: %v", loopID, uerr),
			ErrorKind: agentic.ToolErrorInternal,
		}
	}
	if entity.ResultNotDurable {
		return agentic.ToolResult{
			CallID: call.ID,
			Error: fmt.Sprintf("loop %s terminated but its result was not durably stored: %s",
				loopID, entity.ResultNotDurableReason),
			ErrorKind: agentic.ToolErrorInternal,
			Metadata: map[string]any{
				"loop_id":            loopID,
				"result_not_durable": true,
				"outcome":            entity.Outcome,
			},
		}
	}
	if !entity.State.IsTerminal() {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("loop %s is still running (state=%s); no completion record yet", loopID, entity.State),
			ErrorKind: agentic.ToolErrorNotFound,
		}
	}
	return agentic.ToolResult{
		CallID:    call.ID,
		Error:     fmt.Sprintf("loop %s is terminal (state=%s) but its completion record is absent", loopID, entity.State),
		ErrorKind: agentic.ToolErrorNotFound,
	}
}

// hydrateResult fetches an offloaded result body and resolves it through the
// stored ContentFields map (body role → field name) — the same contract the
// writer's ContentStorable declared. Fails CLOSED on every absent path: a
// missing fetcher, role, or field is a typed error, never a silent empty
// result or a preview masquerading as the whole.
func (e *ReadLoopResultExecutor) hydrateResult(ctx context.Context, ref *message.StorageReference) (string, error) {
	if e.content == nil {
		return "", errs.WrapInvalid(
			errors.New("result is offloaded but no content store is configured for read_loop_result"),
			"ReadLoopResultExecutor", "hydrateResult", "check content fetcher")
	}
	stored, err := e.content.FetchContent(ctx, ref)
	if err != nil {
		return "", err
	}
	fieldName, ok := stored.ContentFields[message.ContentRoleBody]
	if !ok || fieldName == "" {
		return "", errs.WrapInvalid(
			fmt.Errorf("stored content %s has no %q role mapping", ref.Key, message.ContentRoleBody),
			"ReadLoopResultExecutor", "hydrateResult", "resolve body role")
	}
	body, ok := stored.Fields[fieldName]
	if !ok {
		return "", errs.WrapInvalid(
			fmt.Errorf("stored content %s is missing field %q", ref.Key, fieldName),
			"ReadLoopResultExecutor", "hydrateResult", "read body field")
	}
	return body, nil
}

// normalizeLoopID accepts a loop_id argument in either of the two shapes
// rule prompts produce in practice and returns the bare-UUID form the
// AGENT_LOOPS KV bucket keys on (`COMPLETE_<bare-uuid>`).
//
// Rule templates that substitute `$entity.id` for an agent-loop entity
// hand the LLM the full 6-part federated string
// `org.platform.agent.agentic-loop.execution.<uuid>`. The LLM faithfully
// copies that into the tool call. The bucket only knows about
// `<uuid>` — the trailing segment after the last dot. Templates that
// switch to `$entity.instance` (preferred — see entity_substitution.go)
// hand the LLM `<uuid>` directly and this function is a no-op.
//
// We strip everything before the last `.` regardless of segment count.
// The strip is safe for the bare-UUID case (no dot present, returns the
// input unchanged), and tolerant of any partial-prefix shape an LLM
// might hallucinate. UUIDs themselves never contain dots, so the strip
// can never lose part of a real loop ID.
func normalizeLoopID(loopID string) string {
	if idx := strings.LastIndex(loopID, "."); idx >= 0 {
		return loopID[idx+1:]
	}
	return loopID
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
