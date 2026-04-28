# LLM Response Truncation Handling

## What "truncated" means

A model response is **truncated** when it ended because of an
output-budget ceiling rather than because the model finished its
thought. The OpenAI-compatible signal is
`choice.finish_reason == "length"`. Inside semstreams this maps to
`agentic.AgentResponse.Status == StatusLengthTruncated`.

Two distinct things can cause a length-truncation, and beta.21's
loop handler tells them apart by reading **context utilization at
request time**.

## The two cases

### Case A — Output-budget mismatch

The model fits the prompt + history just fine, but its allowed
output tokens (the `max_tokens` parameter, or a provider-side hard
cap) can't carry the response the model wants to emit. Visible at
**low utilization** at request time — there's plenty of room in
the context, the model just can't say what it wants to say in the
budget it has.

Examples:

- 4K model asked to call a tool with 5K of arguments → truncates
  on iteration 1 at ~17% utilization.
- 128K model asked to write a 110K essay → truncates at ~5%
  utilization.
- `max_tokens` set too low for the task — easy config bug.

**No amount of context compaction will fix this.** The output
budget itself is the limit. The framework now fails loud with a
diagnostic message naming the actual numbers so you can pick a
larger model, raise `max_tokens`, or restructure the task.

### Case B — Context full

The conversation history has accumulated to the point where the
model's allowed output budget (model_limit minus history) isn't
enough for the response. Visible at **high utilization** at
request time — the prompt+history is eating most of the window.

Examples:

- 128K model on a long agentic flow with 100K of history →
  the next response only has ~28K of budget; tool args need 30K
  → truncates at ~87% utilization.

**Compaction can fix this.** Beta.21's loop handler runs a
within-iteration retry: it calls the compactor inline, frees room,
and re-emits the same agent.request from the smaller context. If
the retry's response still truncates, that means the response
itself doesn't fit even in the freed budget — that's case A in
disguise; fail loud with the post-compaction diagnostic.

## How the framework decides

```
On finish_reason=length:
  preUtilization = context_manager.Utilization()
  retryCount    = increment_truncation_retry(loop_id)

  if preUtilization < CompactThreshold OR retryCount > 1:
    → Case A diagnostic: fail loud
  else:
    → Case B retry: compact inline, re-emit agent.request,
      do not increment iteration counter, do not fail
```

The retry counter resets to 0 on any forward-progress response
(`StatusComplete` or `StatusToolCall`). So a long-running loop
that hits a truncation, self-heals, runs more iterations, then
hits another truncation will get another retry — the budget is
"once between forward-progress events," not "once per loop
lifetime."

## Reading the diagnostic messages

The framework emits one of two formats when it gives up:

```
truncated at 17% utilization without compaction attempted
(model_limit=4096, completion_tokens=2000) — output budget too
small for task; raise max_tokens or use a model with larger
output capacity
```

That's Case A. Action: switch models or raise `max_tokens`.

```
truncated after compaction (model_limit=128000,
pre-compact utilization=92%, post-compact utilization=72%,
completion_tokens=30000) — response exceeds available budget even
after freeing context; try a larger model or raise max_tokens
```

That's Case B-collapsing-to-A. Compaction did its job (utilization
dropped from 92% to 72%) but the response itself is bigger than
the freed budget. Action: same as Case A — bigger model or raise
`max_tokens`.

A rare third shape fires only when the inline compactor itself
errors during a Case B retry attempt:

```
truncated and compaction failed (model_limit=128000,
utilization=87%, completion_tokens=30000,
compactor_error=<error from the compactor>) — try a larger model
or tune CompactThreshold/HeadroomTokens
```

This means the retry path couldn't run because compaction failed.
Today the stub compactor (no LLM summariser configured) doesn't
return errors, so this branch is defensive insurance. If you see
it firing, your summariser is mis-wired or unreachable.

## Knobs

| Knob | Where | Default | Effect |
|---|---|---|---|
| `max_tokens` | per-request via `agentic.AgentRequest.MaxTokens` | none | Output token ceiling. Raise if Case A diagnostic fires repeatedly. |
| Model selection | model registry | per-deployment | A model with a larger context limit + larger output cap fixes both cases at once. |
| `CompactThreshold` | agentic-loop `ContextConfig` | 0.60 | Lower = compact earlier. Lowering helps if you keep hitting Case B; raising delays compaction (more aggressive use of the window). |
| `HeadroomTokens` | agentic-loop `ContextConfig` | 4000 | Floor on the response budget reserved during context budgeting. Raise if responses are large. |
| `HeadroomRatio` | agentic-loop `ContextConfig` | 0.05 | Same as HeadroomTokens but as a fraction of model_limit. The effective headroom is `max(ratio*limit, floor)`. |

## What you'll see in the trajectory

A self-heal retry adds a `context_compaction_retry` step to the
trajectory (distinct from the routine pre-iteration
`context_compaction` step). The corresponding ContextEvent has
type `compaction_retry` and carries pre-compaction Utilization and
TokensSaved. Operators reading a loop's history can tell at a
glance: "iteration N had two model_request entries because we
hit truncation, compacted, and retried."

A failed truncation adds the standard `agent.failed` envelope with
`reason="length_truncated"` and the diagnostic message in the
`error` field.

## Tool-call truncation specifically

When `finish_reason=length` arrives mid-tool-call (the model started
emitting tool_call arguments but ran out of budget before the JSON
closed), the framework drops the tool_calls from the response
entirely. The truncated arguments cannot be trusted — the JSON may
be cut mid-string, mid-escape, or mid-multi-byte UTF-8 — and a
silent dispatch with empty args is more dangerous than a clean
failure. The same Case A / Case B branching applies; the model
just doesn't get to dispatch a half-formed tool call.

## Common questions

**Q: My loop keeps failing with Case A even though the model has a
huge context window.**
A: The model's context limit and its `max_tokens` ceiling are
different. A 128K model can still have a 4K `max_tokens` budget
configured. Raise `max_tokens` on the request or in the model
registry's per-endpoint config.

**Q: Compaction fired but I still got Case B-collapsing-to-A.**
A: The response itself is bigger than the freed budget. Pick a
model with a larger output budget, or restructure the task to emit
multiple smaller responses.

**Q: Can I disable the within-iteration retry?**
A: Not directly. If you want strict fail-fast on every truncation,
your parent rule can match on `OutcomeTruncated` and treat it as
terminal — the loop's self-heal attempt is at most one extra LLM
round-trip, and the retry only fires when compaction would have
helped.

**Q: How do I tell which case fired without parsing the prose
message?**
A: The framework emits a structured WARN log at the failure point
with these fields:

- `pre_compact_utilization` — utilization at request time (the
  case-A vs case-B split)
- `compaction_attempted` — `false` for case A, `true` for case
  B-collapsing-to-A
- `retry_count` — how many self-heal attempts ran in the current
  forward-progress window
- `completion_tokens` — what the model managed to emit before
  hitting the budget
- `model_limit` — the resolved model context limit at this loop

Match on the structured fields, not the message string. The string
is for humans on a 3am pager; the fields are for log aggregation.

**Q: What happens to approval-gated tool calls when truncation
hits?**
A: A truncation retry re-emits `agent.request` straight to the
model — it does NOT re-invoke any approval gate that may have
already fired in this loop. The approval flow (beta.19) and the
truncation flow are independent: approval gates work on tool
dispatch (`tool.execute.*`), while truncation handling works on
model responses (`agent.response.*`). Most flows won't notice the
distinction; if your product was relying on approval-gating to
serialise model traffic, double-check that an unexpected silent
re-request is acceptable in your model.

## Related

- [migration-beta20-to-beta21.md](migration-beta20-to-beta21.md)
  — what changed in beta.21.
- [`processor/agentic-loop/handlers.go:handleLengthTruncation`](../../processor/agentic-loop/handlers.go)
  — the branching logic.
- [`processor/agentic-loop/context_compaction.go`](../../processor/agentic-loop/context_compaction.go)
  — the compactor invoked inline on retry.
