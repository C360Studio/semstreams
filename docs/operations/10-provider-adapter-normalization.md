# Provider Adapter Normalization

## What the adapter layer does

The agentic-model component talks to LLM providers over an OpenAI-compatible HTTP surface, but
"OpenAI-compatible" hides a long tail of provider-specific quirks that produce 400 errors or silent
data corruption. The `ProviderAdapter` interface in `processor/agentic-model/adapter.go` is the
narrow seam where those quirks are normalized away from the rest of the codebase.

```
chat request build → adapter.NormalizeMessages → adapter.NormalizeRequest → HTTP send
                            ↑
                request-shape fixes happen here
```

`AdapterFor(provider)` returns the right adapter for a registered endpoint. Today: `gemini`,
`openai`, and a `generic` fallback for everything else (Anthropic-via-compat, OpenRouter, Ollama,
LiteLLM, etc.).

## Per-adapter behavior

| Adapter | Tool message `Name` fallback | Assistant `Content` fallback | Consecutive same-role collapse | Stream delta index inference |
|---------|------------------------------|------------------------------|--------------------------------|------------------------------|
| `generic` | `"unknown_tool"` if empty | `" "` if empty + `tool_calls` | **Yes** (beta.28) | Yes |
| `gemini`  | `"unknown_tool"` if empty | `" "` if empty + `tool_calls` | No (see below) | Yes |
| `openai`  | None | None | No | Trusts explicit index |

### Why `generic` collapses and the others don't

Several providers (Anthropic, some Gemini compatibility layers, OpenRouter routes that bridge to
either) require **strict role alternation** and reject consecutive same-role messages with a 400.
The `generic` adapter is the fallback for every provider that isn't `gemini` or `openai`, which
means it routes traffic to providers most likely to enforce strict alternation. Beta.28 added a
collapse pass that joins the `Content` of adjacent same-role messages with `\n\n`. The trigger was
a semspec report — multiple consecutive system messages produced 400s on their OpenRouter route.

`gemini` is **not** collapsed today. Gemini's compat layer has been tolerating consecutive
same-role messages in practice, and changing its message shape risks regressing the existing
tool-pair fixes. The collapse can be lifted into Gemini if a concrete report surfaces.

`openai` is **not** collapsed. OpenAI accepts consecutive same-role messages natively; collapsing
would change the wire shape (and any request-snapshot tests) without solving anything.

## What the collapse pass excludes

Two message classes are deliberately preserved as separate messages even when they share a role:

1. **Tool messages** (`role == "tool"`). Each carries a distinct `ToolCallID` that pairs it with a
   specific assistant tool call. Merging tool messages would either lose the IDs or pair multiple
   results with one ID — both break the contract.
2. **Assistant messages with `tool_calls`**. Each carries a distinct `ToolCalls` payload. Merging
   them would discard the second message's tool-call slice.

Both invariants are enforced by the `canMerge` predicate in
`processor/agentic-model/adapter_generic.go::collapseConsecutiveSameRole`.

## Known footguns

These are both currently latent (nothing in the repo exercises them today), but they will activate
the moment multimodal support or the `Name` participant field comes into use.

### Footgun 1 — `MultiContent` silent drop

`openai.ChatCompletionMessage` has two content carriers:

- `Content string` — the plain-text path used everywhere in semstreams today.
- `MultiContent []ChatMessagePart` — the vision/audio path (each part is text, image URL, or
  audio data). Used by GPT-4o, Gemini multimodal, Claude vision, etc.

The collapse pass joins **only** `Content`. If two adjacent same-role messages each carry
`MultiContent`, the second message's parts are silently discarded when the first message survives
the merge. There is no warning, no fallback — the parts just disappear from the wire payload.

This has not bitten anyone yet because the codebase does not construct `MultiContent` anywhere. A
`grep -r MultiContent` across the repo returns zero hits as of beta.28. **It will become a real
data-loss bug the first day a component starts populating `MultiContent`.**

**Mitigation when multimodal lands:** harden `collapseConsecutiveSameRole` to refuse to merge when
either message has a non-empty `MultiContent`. The simplest contract is "if either side is
multimodal, treat them as non-mergeable." A more sophisticated contract (concatenate the parts
slice when both sides are multimodal) is also viable but adds surface area.

### Footgun 2 — `Name` field loss

`ChatCompletionMessage.Name` is the OpenAI "participant hint" field — rarely used, but legal on
user, assistant, and system roles. The collapse keeps `out[last]`'s `Name` and ignores the
incoming message's. If only the second message sets `Name`, that value is lost.

This is much less concerning than the multimodal case (the field is rarely used and the collapse
doesn't *corrupt* anything — it just drops a hint), but it is a real divergence from the
"merge two messages losslessly" mental model. If a use case for `Name` emerges, the collapse
predicate should also bail when `Name` differs between the two candidates.

## Action items when multimodal support is added

Before any component starts producing `MultiContent` parts:

1. Update `collapseConsecutiveSameRole` to skip merging when either message has a non-empty
   `MultiContent`. Add a regression test asserting that two adjacent user messages with image
   parts remain two distinct messages on the wire.
2. Decide whether the existing `Content == ""` checks need to also consider `MultiContent != nil`
   when deciding "does this message carry payload?" The empty-content fallback for assistants
   with `tool_calls` (the `" "` insertion) becomes ambiguous if the assistant message already
   carries `MultiContent` parts.
3. Audit the `Name` field's usage in any new multimodal flows; if it gets used, tighten the
   collapse predicate to bail on `Name` mismatch.

## See also

- `processor/agentic-model/adapter.go` — the adapter interface.
- `processor/agentic-model/adapter_generic.go` — the generic adapter, including the collapse pass.
- `processor/agentic-model/adapter_gemini.go` — the Gemini-specific adapter.
