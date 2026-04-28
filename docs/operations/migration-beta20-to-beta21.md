# Migration Guide: beta.20 → beta.21

## Summary

Beta.21 is a **stability/correctness tag**. No API breakage. It
fixes a silent regression in the LLM streaming chunker (beta.2's
`finish_reason=length` truncation contract was bypassed when
tool_calls were present, since beta.2) AND turns the loop's
truncation handling from a dead-end into a self-heal-or-actionable-
fail flow.

semspec reported a bug in the streaming code chunker; the audit
turned up the chunker bug plus a deeper UX issue around what
happens after a truncation. Beta.21 closes both.

## What changes

### 1. Chunker preserves length-truncation when tool_calls accumulated

`processor/agentic-model/stream.go` (streaming path) and
`processor/agentic-model/client.go` (non-streaming path) used to
unconditionally overwrite `Status` to `"tool_call"` when any
tool_calls were accumulated, even if `finish_reason=length` had
been set. The malformed-arguments fallback then silently
substituted `args = make(map[string]any)` for unparseable
arguments, turning a "truncation" signal into a "tool dispatch with
empty input" silent dispatch.

After beta.21: when `finish_reason=length`, `Status` stays
`StatusLengthTruncated` (this was already the case for the
non-streaming path through beta.2; the bug there was the
subsequent overwrite to `"tool_call"` when tool_calls were
present, not the Status mapping itself) and `Message.ToolCalls`
stays nil. A truncated response cannot be trusted to have
complete arguments, so callers cannot mistakenly dispatch one.
The raw partial `tc.Function.Arguments` lives only in logs from
there on.

### 2. Loop branches truncation handling on context utilization

`processor/agentic-loop/handlers.go:handleLengthTruncation` (new)
replaces the unconditional `failLoop` on `Status=length_truncated`:

- **Utilization < `CompactThreshold` OR already retried this loop:**
  fail with `OutcomeTruncated` (unchanged code) and a diagnostic
  message naming model_limit, completion_tokens, utilization, and a
  `compaction_attempted` boolean. Operator reads the message and
  decides: switch models, raise `max_tokens`, or tune compaction.
- **Utilization >= `CompactThreshold` AND first retry:** call the
  compactor inline, emit a `compaction_retry` ContextEvent and
  `context_compaction_retry` trajectory step, build a fresh
  `agent.request` from the now-compacted context, and emit it.
  Within-iteration self-heal — does NOT increment the loop's
  iteration counter. Single retry budget per loop until forward
  progress (a `StatusComplete` or `StatusToolCall` response clears
  the counter).

### Behaviour change

A previously fatal `Status=length_truncated` may now self-heal once
via compaction. Worst case: a chronic-truncation loop with a too-
small model adds one extra LLM round-trip plus compaction
wallclock before the loop fails — typically a few hundred
milliseconds to a couple of seconds depending on model + summariser.

## What you should do

For most deployments: nothing. Pull beta.21 and the silent
truncation-as-empty-args dispatch goes away; visible
truncation failures self-heal more often.

If you have **parents grepping the previous truncation error
message** (`"output hit max_tokens limit"`), they need to update.
The outcome code is unchanged (`OutcomeTruncated`), so parents
matching on outcome are unaffected. If you parse
`LoopFailedEvent.Error` for substring matches other than the old
string, expect one of three new diagnostic shapes:

- `"truncated at N% utilization without compaction attempted (model_limit=X, completion_tokens=Y) — output budget too small for task; raise max_tokens or use a model with larger output capacity"` — case A (output-budget mismatch).
- `"truncated after compaction (model_limit=X, pre-compact utilization=N%, post-compact utilization=M%, completion_tokens=Y) — response exceeds available budget even after freeing context; try a larger model or raise max_tokens"` — case B (context-full, retry didn't rescue).
- `"truncated and compaction failed (model_limit=X, utilization=N%, completion_tokens=Y, compactor_error=...) — try a larger model or tune CompactThreshold/HeadroomTokens"` — rare; only fires when the inline compactor itself errors.

All three messages are actionable. The `OutcomeTruncated` code is
the machine-readable signal; the message is the operator-facing
diagnosis. Parents that need structured signals should parse the
WARN log fields (`pre_compact_utilization`, `compaction_attempted`,
`retry_count`, `completion_tokens`) emitted alongside the failure
rather than substring-matching the message.

If you have **custom tool implementations that previously received
unexpectedly empty arguments from a truncated response and worked
around it silently**, those workarounds can be removed in beta.21
— the truncated tool_call won't reach you anymore.

## What didn't change

- Outcome codes: `OutcomeTruncated` is still the single code for
  all length-truncation failures. The diagnostic message
  distinguishes the two cases.
- The compaction threshold (`CompactThreshold`, default 0.6) and
  headroom config (`HeadroomTokens`/`HeadroomRatio`) are unchanged.
- Approval flow (beta.19), payload registry (beta.18),
  request/retry decision framework (beta.20) — all unchanged.
- API surface — no removals, no renames.

## Verification

After upgrading:

- `go build ./...` succeeds.
- `go test -race ./...` passes including the four new
  `TestHandleLengthTruncation_*` regression tests.
- `task lint` reports 0 revive warnings.
- Existing flows that don't hit truncation continue to work
  unchanged.
- For flows that DO hit truncation: the loop now either self-heals
  (case B) or fails with an actionable message (case A) instead of
  failing generically.

## Related

- [`docs/operations/08-llm-truncation-handling.md`](08-llm-truncation-handling.md)
  — the operator-facing guide explaining the two truncation cases
  and the knobs to tune.
- [`migration-beta19-to-beta20.md`](migration-beta19-to-beta20.md)
  — the previous migration (NATS request/retry audit).
