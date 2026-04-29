# Migration Guide: beta.23 → beta.24

## Summary

Beta.24 closes the inline `<think>...</think>` reasoning gap
semspec reported in the post-beta.22 audit. qwen3 / qwen3-coder /
deepseek-r1 served via OpenAI-compatible endpoints (Ollama compat
path in particular) emit reasoning as inline tags in the regular
content field rather than via the channel-based
`reasoning_content` field. Pre-beta.24, those tags reached the
loop intact — bloating context, polluting the user-visible answer,
and feeding the model's prior thinking back as plaintext on
subsequent turns.

Additive surface; no API breakage. Channel-based reasoning
(OpenAI o1, DeepSeek-R1 API direct) is unchanged. Models that
don't emit `<think>` tags pay nothing on the no-think hot path.

## What changes

### Inline `<think>` blocks now route to `ReasoningContent`

Before beta.24:

```json
{
  "role": "assistant",
  "content": "<think>Let me reason about this.</think>The answer is 42.",
  "reasoning_content": ""
}
```

After beta.24 — same wire response from the model; framework
splits it before the loop sees it:

```json
{
  "role": "assistant",
  "content": "The answer is 42.",
  "reasoning_content": "Let me reason about this."
}
```

This matches the shape produced by channel-based reasoning
providers. Downstream consumers (the loop, trajectory recorder,
audit log, compaction) do not need to know which provider
generated the reasoning — `Content` is the user-visible answer,
`ReasoningContent` is the model's thinking, regardless of
wire format.

### Streaming path: chunk-handler split

Live `ChunkHandler` callbacks now receive the routed
`(ContentDelta, ReasoningDelta)` split. Pre-beta.24 a stream of
`<think>...</think>final` deltas would surface raw bytes including
the tags in `ContentDelta`. Post-beta.24 the same stream surfaces
clean content in `ContentDelta` and reasoning in `ReasoningDelta`.

Tags spanning chunk boundaries are handled by a small state
machine in the streaming accumulator — partial tag bytes are
buffered until the next delta resolves them.

## What is NOT changing

- **`agentic.ChatMessage.ReasoningContent` field** — already
  existed; the fix is upstream of it.
- **`agentic.AgentResponse.Message` shape** — unchanged.
- **OpenAI o1 / DeepSeek-R1 channel-based path** — already
  handled by `stream.go` and `agentic/types.go:143-144` (which
  accepts both `reasoning_content` and `reasoning` aliases). The
  channel-based and inline-tag paths both feed the same field
  now.
- **`graph/query/classifier_llm_adapter.go`** — keeps its
  existing `stripThinkTags`. Different domain (T3 query
  classifier wants raw JSON only, not reasoning audit), so
  strip-and-discard is correct there. The shared piece is the
  regex pattern.
- **Provider adapters** — `GenericAdapter`, `GeminiAdapter`,
  `OpenAIAdapter` are unchanged. Inline-think is a model-family
  quirk, not a provider quirk; the extraction lives universally
  in `convertResponse` after the adapter step rather than in any
  one adapter.

### Ollama `think: false` request-side knob — still deferred

Ollama 0.6+ exposes `think: false` on the native `/api/chat`
endpoint to disable thinking server-side. semstreams uses
Ollama's OpenAI-compat `/v1/chat/completions` layer, which Ollama
upstream has explicitly declined to extend (see
`project_ollama_num_ctx_gap.md`). To send `think: false` we'd
need either a custom Ollama path on `/api/chat` (deviates from
the framework's "OpenAI-compat-only" invariant in the
agentic-model client) or `extra_body` injection that the
`sashabaranov/go-openai` library does not expose.

Both are larger lifts than the response-side extract, and the
extract on its own removes the operator-visible damage. Defer
the request-side knob until a stronger forcing function (e.g.,
token-cost analysis showing the wasted thinking tokens are
expensive enough to warrant the architectural step).

## Behavior reference

### Edge cases handled by the streaming state machine

| Case | Behavior |
|---|---|
| Tag entirely within one delta | Standard split; content + reasoning surfaced atomically. |
| Open tag split across boundary (`<thi` + `nk>...`) | Partial buffered; transitions when complete. |
| Close tag split across boundary (`...</thi` + `nk>`) | Same; partial buffered, transitions when complete. |
| Byte-by-byte deltas | Each character buffered; full tag forms incrementally; state transitions atomically. |
| Empty think block (`<think></think>`) | Block removed from content; reasoning is empty (no separator emitted between this block and any following block since neither side has content). |
| Multiple think blocks | Each extracted in order; reasoning blocks concatenated. |
| Stream ends with unmatched open tag | Buffered partial reasoning is flushed to `ReasoningContent`; warning logged (`"streaming response ended mid-<think>"`); content is empty for that turn. |
| Stream ends with unmatched partial open tag (e.g., model emitted literal `<thi` and stopped) | Buffered bytes flushed to `Content`; no warning (no think block was ever opened). |
| Hybrid: delta carries both inline tag AND channel-based `reasoning_content` | Both surface in `ReasoningDelta`; content cleaned. |

### TTFT metric on think-emitting models

Time-to-first-token is now gated on the first user-observable
byte rather than the first wire byte. When a model opens its
response with `<think>` (likely on qwen3-family models), the
opening tag itself routes to nothing visible — TTFT now marks
the first content or reasoning byte after the state transition,
which can shift TTFT later by a few tokens. This is more
semantically correct (operators tracking TTFT against UX want
the first visible byte, not the first wire byte) but operators
comparing against pre-beta.24 baselines on think-emitting models
will see a small apparent regression that's actually a
metric-quality improvement.

### Hot path

For models that do not emit `<think>` tags:

- Non-streaming: a single `strings.Contains(content, "<think>")`
  check, returns immediately if absent. No allocation.
- Streaming: state machine runs but stays in "outside think,
  no pending tag" state; partial-tag peeling iterates at most
  6 bytes (max prefix of `<think>` minus the full tag).

## Verification

```bash
# Unit + state-machine tests
go test -race ./processor/agentic-model/...

# End-to-end via mock LLM (requires Docker)
go test -race -tags=integration -run TestIntegration_InlineThinkExtraction \
  ./processor/agentic-model/...

# Manual smoke test with a real qwen3 model
# (Ollama-hosted, replace with your endpoint)
ollama pull qwen3:8b
# Update your config to point at qwen3:8b, then:
task e2e:agentic
# Confirm the trajectory shows reasoning in `reasoning_content`
# and clean content in `content`.
```

## Related

- Memory: `project_inline_think_extraction.md` — captures the
  gap, the extract-not-strip rationale, and the scope decisions.
- Plan: `~/.claude/plans/inline-think-extraction.md` (local;
  drafted during the beta.23-to-beta.24 cycle).
- Channel-based reasoning: `processor/agentic-model/stream.go`
  + `agentic/types.go:138` + tests
  `TestChatCompletion_ReasoningContent` / `TestBuildChatRequest_ReasoningContentStripped`.
- Classifier strip-and-discard (different domain):
  `graph/query/classifier_llm_adapter.go:44`.
