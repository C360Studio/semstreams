## Why

Two semdev blockers on the agentic-loop budget contract (filed by its issue→PR agent, verified still present
at beta.147):

- **gh#528**: a spawned loop's iteration budget cannot be set per-spawn — only the component-global
  `Config.MaxIterations` applies, so a human-approved task budget (e.g. semdev's `task.spec.<i>.budget`,
  clamped [1,5]) cannot bound its loop. Downstream projects would have to re-derive budget enforcement with
  counting rules or product-Go gates.
- **gh#529**: iteration exhaustion publishes two different failure reasons depending on the detecting path —
  `"max_iterations"` from the tool-completion path, generic `"handler_error"` from the model-response guard
  (the path that fires in the natural flow). Reactive rules routing exhausted loops to escalation must
  string-match error text.

## What Changes

- `agentic.TaskMessage` gains optional `max_iterations` (`*int`, omitempty): nil → component default;
  non-nil → used for loop creation, validated ≥ 1 in `TaskMessage.Validate`, and clamped by
  `Config.MaxIterations` as the operator ceiling — a spawn may narrow its budget, never widen it.
- The `publish_agent` rule action gains `loop_max_iterations` (name deliberately distinct from the rule
  action's existing firing-cap `max_iterations`), with variable substitution so the value can come from an
  entity triple; substituted values that are not a positive integer fail the action loudly.
- Iteration exhaustion publishes exactly one reason, `"max_iterations"`, on every detection path: the
  model-response guard returns a typed sentinel error and the component failure handler maps it, instead of
  wrapping generically as `"handler_error"`.

Additive payload field (no version bump); wire-visible reason change for the guard path is called out in the
changelog — it is the defect being fixed.

## Non-goals

- Rule-layer firing caps (`Action.MaxIterations`) — unchanged, distinct concern.
- Any change to cap-exhaust *routing* (rules own that downstream).
- Time budgets (gh#384) and per-spawn tool budgets.

## Capabilities

### Modified Capabilities

- `agentic-loop` (spec seeded by this change): per-spawn iteration budget and uniform exhaustion reason.

## Impact

- `agentic/user_types.go` (TaskMessage + Validate), `processor/agentic-loop` (handlers, component failure
  mapping), `processor/rule/actions.go` (publish_agent), schema regeneration, operator JSON round-trip tests.
- semdev unblocked: budgeted developer loops + deterministic escalation routing. Closes gh#528, gh#529.
