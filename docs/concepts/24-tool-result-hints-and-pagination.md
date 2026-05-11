# Tool-Result Hints and Pagination

Structured signaling on tool results — how SemStreams tells the agent
*not just what happened* but *what to do next* when a successful call
returns data the agent should treat as a refinement signal.

Shipped in beta.63 to close the gap semspec hit on 2026-05-09:
mid-tier models (qwen3.6-27b, llama-3.3-70b) retried the same
102KB-overflowing graph query 3+ times because the "use more specific
queries" advice was buried in a free-form error string the small model
ignored. Lifting the signal to a typed field — and a documented
pagination continuation token — gives the loop a structured cue to
inject at the top of the model's next message, where it's harder to
miss.

## Two complementary contracts

| Contract | Wire shape | Use when |
|---|---|---|
| **ResultHint** | `agentic.ToolResultHint` field on `ToolResult` | The call succeeded but the agent should refine its approach (too-large, empty, syntax-error). |
| **Pagination** | `MetadataKey{HasMore,NextOffset,NextCursor}` in `ToolResult.Metadata` + `Paginated:true` on `ToolDefinition` | The call returned the first page of a larger result set and the agent can continue. |

The two compose: a paginated tool whose first page exhausted the
caller's byte budget can return BOTH `ResultHint=HintTooLarge` AND
`Metadata[MetadataKeyHasMore]=true`. The framework renders both into
the model's next message — "narrow your query OR continue with
cursor=…" — and the model picks.

## ResultHint — structured refinement signal

The agent loop's `buildToolMessages` reads `ResultHint` and prepends a
canonical advice line to the model's next tool-result message:

```
[hint: too_large] The result was truncated because the response
exceeded the executor's size cap. Narrow your query — add a more
specific filter, entity_id, or a smaller limit — before retrying.

<original tool content>
```

The framework controls the advice text so the phrasing is consistent
across every executor that signals the same hint. Executors pick the
enum value, the framework renders the language.

### Enum values

- `HintTooLarge` — the call returned more data than the executor or
  framework permitted; the content was truncated or rejected. **Action:**
  narrow the query.
- `HintEmpty` — the call succeeded with an empty result set (no
  entities matched, search returned zero hits). **Action:** broaden
  the filter or try a different tool. Distinct from
  `ToolErrorNotFound` — empty results from a well-formed query is not
  an error.
- `HintSyntaxError` — the tool's query-language parser rejected the
  request, distinct from `ToolErrorInvalidArgs` (which is the agent's
  JSON arguments failing the framework's schema validation).
  **Action:** introspect the tool's syntax before retrying.

### Executor example

```go
func (e *MyExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    rows, total, err := e.runQuery(ctx, call.Arguments)
    if err != nil {
        return agentic.ToolResult{
            CallID:    call.ID,
            Error:     err.Error(),
            ErrorKind: agentic.ToolErrorExternal,
        }, nil
    }
    if len(rows) == 0 {
        return agentic.ToolResult{
            CallID:     call.ID,
            Content:    "[]",
            ResultHint: agentic.HintEmpty,
        }, nil
    }
    if serialized := serialize(rows); len(serialized) > capLimit {
        return agentic.ToolResult{
            CallID:     call.ID,
            Content:    serialized[:capLimit],
            ResultHint: agentic.HintTooLarge,
        }, nil
    }
    // ... normal success path
}
```

### Composition with `Error`

`ResultHint` and `Error` are NOT mutually exclusive. A tool that fails
*partway* through and returns partial data can set both — `Error`
documents the failure, `ResultHint` advises the recovery. The
framework renders both in order: hint preamble first, then content,
then (if Content was empty) the error fallback.

### Migration from `ApprovalRequiredPrefix`

The legacy in-band signaling pattern (magic-string prefix on
`ToolResult.Error` sniffed by `agentic.IsApprovalRequired`) is the
shape `ResultHint` replaces. New executors should prefer the typed
field. A future PR will migrate the approval flow itself onto
`ResultHint=approval_required`, retiring the prefix sniffer.

## Pagination — continuation tokens

The canonical contract is **metadata-only**: a tool that supports
pagination declares `Paginated:true` on its `ToolDefinition` and
emits the continuation fields in every successful result's
`Metadata` map. The agent loop reads `MetadataKeyHasMore` in
`buildToolMessages` and appends a continuation hint to the model's
next message when more pages remain:

```
<tool content>

[pagination: more results available; pass cursor="abc123" to continue]
```

### Metadata keys

```go
const (
    MetadataKeyHasMore       = "has_more"        // bool
    MetadataKeyNextOffset    = "next_offset"     // int64 — byte/index paging
    MetadataKeyNextCursor    = "next_cursor"     // string — opaque keyset paging
    MetadataKeyTotalBytes    = "total_bytes"     // int64 — optional total
)
```

`MetadataKeyHasMore` is always set on a paginated tool's result
(`false` on the last page, `true` otherwise). Exactly one of
`NextOffset` or `NextCursor` is set on intermediate pages — the choice
depends on the tool's result-source shape:

- **`NextOffset`** when the source is offset-stable: byte-paging
  through a single string (`read_loop_result`), index-paging through a
  list-of-known-length, etc. The agent reads the integer and passes it
  back as an argument.
- **`NextCursor`** when the source is a result SET with no natural
  offset: a keyset-paginated search, a stream of entities by ID order.
  The cursor is **opaque** — the server controls the format, the agent
  must never inspect or modify it. Opacity is what allows the backend
  to change encoding (or evict expired cursors) without breaking
  in-flight pagination.

### Choosing offset vs cursor

| Property | Offset | Cursor |
|---|---|---|
| Source shape | Byte-stable / index-stable | Result set, possibly re-ranked |
| Agent semantics | Numeric continuation | Opaque token |
| Backend flexibility | Locked to byte/index ordering | Backend can change encoding |
| Resumability across rebalances | Stable if source doesn't change | Server can invalidate |

Reach for `NextOffset` when paginating a single piece of bytes;
`NextCursor` when paginating a result set whose order or membership
could shift between pages (BM25 ranking changes, new entities
ingested, etc.).

### Executor example

```go
func (e *MyExecutor) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{
        {
            Name:        "my_search",
            Description: "Search for entities by predicate.",
            Paginated:   true,
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "predicate": map[string]any{"type": "string"},
                    "cursor":    map[string]any{"type": "string", "description": "Pagination cursor from a previous call's metadata. Omit on first call."},
                },
                "required": []string{"predicate"},
            },
        },
    }
}

func (e *MyExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    cursor, _ := call.Arguments["cursor"].(string)
    page, nextCursor, err := e.fetchPage(ctx, call.Arguments["predicate"].(string), cursor)
    // ... handle err
    meta := map[string]any{
        agentic.MetadataKeyHasMore: nextCursor != "",
    }
    if nextCursor != "" {
        meta[agentic.MetadataKeyNextCursor] = nextCursor
    }
    return agentic.ToolResult{
        CallID:   call.ID,
        Content:  serialize(page),
        Metadata: meta,
    }, nil
}
```

### `Paginated:true` advertisement

The flag is informational at the wire level today: the agent loop
branches on the actual `has_more` value in result metadata, not on
the flag. Future uses:

1. **Operator introspection** — "which tools support pagination?"
   answered without scraping per-executor source.
2. **Contract-violation warnings** — the loop can log a Warn when
   `has_more` arrives from a tool whose definition doesn't declare
   `Paginated:true`, surfacing executor authors who set the metadata
   without opting into the contract.

Set the flag when your executor honors the contract; don't set it
otherwise.

## Reference

- **Field definitions:** `agentic/tools.go` — `ToolResultHint`,
  `ToolResult.ResultHint`, `ToolDefinition.Paginated`,
  `MetadataKey{HasMore,NextOffset,NextCursor,TotalBytes}`.
- **Loop rendering:** `processor/agentic-loop/result_hint.go` —
  `decorateContentWithHint`, `decorateContentWithPagination`,
  `hintMessages` text registry.
- **First in-tree consumer:** `processor/agentic-tools/loop_result.go`
  — `read_loop_result` declares `Paginated:true` and emits the metadata
  keys.
- **Migration target:** `agentic/approval.go` —
  `ApprovalRequiredPrefix` will move to `ResultHint=approval_required`
  in a follow-up.

## Why this beats prompt-stanza fixes

Every new failure mode that's handled by prompt engineering grows the
system prompt by another sentence. Small models drown — prompts
already crowd out task-specific reasoning, and a single new stanza
displaces working examples on context-limited deployments. Structural
signals compose: one hint preamble line works for every flow that
generates the same `ResultHint`, the framework owns the phrasing, and
small models pick it up from the top of the message instead of having
to parse English advice buried in a free-form error string.

The cost is a typed enum value per executor that signals — much
cheaper than a per-model persona override.
