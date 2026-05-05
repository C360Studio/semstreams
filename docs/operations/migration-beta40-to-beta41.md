# Migration Guide: beta.40 → beta.41

## Summary

Beta.41 adds a per-spawn `action_allowlist` field on the
`publish_agent` rule action that flows down to the spawned loop's
`decide` tool, so coordinator personas can structurally enforce a
closed action vocabulary on the wire — not just in persona prose.

This is the structural counterpart to beta.40's description hygiene
(stripping pre-loaded action examples from the decide tool description).
beta.40 stopped the decide tool from biasing the LLM toward a hardcoded
example list; beta.41 lets the rule author *enforce* the persona's
enumerated vocabulary on every call.

| Surface | Status |
|---|---|
| New field `rule.Action.ActionAllowlist []string` on `publish_agent` actions | **Additive** |
| New constant `agentic.MetadataKeyDecideActionAllowlist` (wire key `agent.decide.action_allowlist`) | **Additive** |
| `decide` tool validates `action` against the allowlist when present | **Behavioural — opt-in** |
| Existing `publish_agent` rules without the new field | **Unchanged** |
| Existing decide tool calls without the metadata key | **Unchanged — free-form** |
| Schema (`schemas/rule-processor.v1.json`) gains `action_allowlist` on every `publish_agent` action surface | **Additive** |

**The simplest beta.40 → beta.41 upgrade is to do nothing.** Existing
deployments keep their current free-form `decide` behaviour. Rules
that opt into `action_allowlist` get structural enforcement; rules
that don't are unaffected.

## What's new

### Per-spawn `action_allowlist`

Set on a `publish_agent` action to declare the closed set of action
values the spawned loop's `decide` tool will accept:

```jsonc
{
  "type": "publish_agent",
  "role": "dev-via-spec-planner",
  "tools": ["read_loop_result", "decide"],
  "action_allowlist": ["planned", "needs_clarification"],
  "prompt": "..."
}
```

When the LLM emits `decide(action="anything-else", reason=...)`, the
executor returns `ToolErrorInvalidArgs` with the valid set quoted in
the error message:

> action `"anything-else"` is not in the allowed set for this role:
> `[planned, needs_clarification]`. The action vocabulary your role
> accepts is closed; pick one of these and re-emit.

The error feeds back into the LLM's next iteration, giving it a
concrete correction signal without any third-party hint.

### Wire path

```
rule.Action.ActionAllowlist
  → executePublishAgent stamps onto TaskMessage.Metadata
  → JSON wire (encoded as []any)
  → agentic-loop propagates Metadata onto each ToolCall
  → DecideExecutor.decide reads & validates
```

The wire shape is `[]any` (the JSON round-trip from `TaskMessage` →
`agent.task` subject → `ToolCall` produces this), and the decide
executor's `coerceAllowlist` handles both `[]any` and `[]string` so
in-process callers and JSON-decoded callers behave identically.

### Empty vs. missing semantics

Symmetric with `task.Tools`:

- **Missing field** (no `action_allowlist` key in the rule): free-form
  decide behaviour. Back-compat for every existing rule.
- **Explicit empty array** (`"action_allowlist": []`): also free-form.
  Rule authors who set an empty array haven't accidentally locked the
  spawned loop out of every action.
- **Non-empty array**: closed enforcement. Only the listed values
  validate.

## The wedge that motivated this

semteams smoke #7 (R3.7.2.l′, 2026-05-04). The
`dev-via-spec-planner` persona enumerated `"planned"` as its terminal
action. The decide tool description (pre-beta.40) pre-loaded
`(e.g. fan_out, synthesize, retry, done)`. Real claude-sonnet-4-6
emitted `decide(action="fan_out", reason=<3000-word plan>)`; no
rule's `coordinator.next_action` condition matched; the chain
wedged at 7 of 12 expected loops.

beta.40 closed the description-leak class. beta.41 closes the
wire-vocabulary class so future flows can't get bitten by a different
persona-vs-tool-surface drift.

## Migration steps

### To opt in (per rule)

Add `action_allowlist` to the relevant `publish_agent` actions in
your rule config. Names must match exactly — this is a literal
string-set membership check, not a pattern match.

### Existing rules and configs

Unchanged. The new field defaults to nil/empty (free-form), so any
rule that doesn't set it keeps the pre-beta.41 behaviour exactly.

## Backward compatibility

- Existing rules without `action_allowlist`: unchanged behaviour.
- Existing decide tool calls without the metadata key on the wire:
  unchanged behaviour.
- Existing tool-result schemas: no change.
- The error message on rejection uses the existing
  `ToolErrorInvalidArgs` kind — no new error type, no new client-side
  handling required.

## Cross-references

- `processor/rule/actions.go:Action.ActionAllowlist` — the new field
- `processor/rule/actions.go:executePublishAgent` — the producer
- `agentic/tools.go:MetadataKeyDecideActionAllowlist` — the wire-key
  constant
- `processor/agentic-tools/decide.go:validateActionAllowlist` — the
  consumer (with `coerceAllowlist` handling the JSON wire shape)
- `schemas/rule-processor.v1.json` — schema regen
- semteams smoke #7 findings (the empirical case study)
- beta.40: decide tool description hygiene (sibling, complementary fix)
