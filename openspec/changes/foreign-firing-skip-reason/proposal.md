# Change: The foreign-firing skip counter stops reporting an absent firing entity as a foreign import

Closes #1169. Claim: branch `claude/gh1169-cron-skip-reason`, own worktree. Premises pinned at `main@5b7c3db3`.

## Why

`rule_foreign_firing_writes_skipped_total{reason="foreign_authority"}` and its Info line report `foreign_authority`
for a `publish_agent` dispatch that has **no firing entity at all**. The cron path builds an `ExecutionContext` with
`Schedule` set and no `EntityID` (`processor/rule/cron_scheduler.go:650-656`); `foreignFiringEntity`
(`processor/rule/actions.go:596-598`) asks `ValidateEntityIDAuthority`, which returns the structural
`ParseEntityID` error before any authority comparison (`pkg/types/entity_id_authority.go:36-39`); the caller treats
any non-nil error as foreign; and `foreignFiringSkipRecorder` (`actions.go:634-651`) hardcodes
`EntityIDReasonForeignAuthority` for both the counter label and the log's `reason` field.

So in any deployment with graph integration, **every cron `publish_agent` dispatch** increments the counter under
`foreign_authority` and logs *"firing entity carries a foreign authority — framework writes to it skipped"* about an
entity that does not exist. `docs/operations/migration-beta162-to-beta163.md:565-575` presents that exact line as
the operator's only signal for import-boundary activity; cron noise under the same label makes the signal unreadable.

Skipping the write is right — a `rule.task.spawned` back-reference with no subject cannot be issued, and declining
when the subject cannot be established is the fail-closed answer the requirement already demands. The label is the
defect: `pkg/types` deliberately codes authority rejections (`entity_id_authority_invalid`) distinctly from
structural ones (`entity_id_invalid`) "so a caller can tell 'malformed' from 'not yours'"
(`pkg/types/entity_id_authority.go:4-8`), and the rule engine discards that distinction.

## What changes

- **One new fixed reason token, rule-local**: `unresolvable_firing_entity` — "no firing entity was established":
  the dispatch carries none (cron) or carries one that fails structural validation. It is an unexported constant in
  `processor/rule`, not an `EntityIDReason*` export: it is not an entity-ID validation outcome, it is the rule
  engine's own skip reason, and `processor/rule` is a Tier 1 package that gains no exported symbol from this.
- `foreignFiringSkipReason` classifies by the classified error's code: authority code → `foreign_authority`;
  anything else → `unresolvable_firing_entity`; nil → local. The bool `foreignFiringEntity` stays as a one-line
  wrapper for the two platform-wiring integration tests that call it; production reads the reason directly.
  **Pattern check:** `pkg/errs` exports constructors and a class accessor but no code reader; reading a
  `*errs.ClassifiedError` through `errors.As` and comparing `Code` is the repo idiom at ten production sites
  (graph-query, graph-clustering, agentic-tools, agentic-loop, graph-ingest's `mutation_runtime.go:61`), so
  there was nothing to reuse. graph-ingest's `entityIDContractReason` (`mutation_runtime.go:120-135`) reads the
  `reason` detail instead because it answers a different question — WHICH structural reason — and its
  default-to-unknown shape is the one this classifier mirrors with default-to-unresolvable.
- The skip line's `rule_id` falls back to `Schedule.ID` when `RuleID()` is empty, so the cron line — the only
  attribution the operator gets, since the counter carries no rule label by design — names its rule.
- The recorder takes the reason and uses it for the label and the log field; the unresolvable case logs a message
  that does not claim a foreign authority. Still ONE Info line per dispatch, still one increment per dispatch.
- The fail-closed empty-authority answer is unchanged: a canonical firing entity under an empty pair still reads as
  `foreign_authority`, exactly as the requirement states and
  `TestPublishAgentThroughExportedFullConstructorSkipsForeignSpawnedTask` pins.
- The `graph-ingest` requirement that pins the counter widens its `reason` vocabulary to the two tokens and gains a
  scenario for the cron/no-entity case; the migration note's operator passage says which label means what.
- The slog capture helper used by the tagged run-scope tests moves to an untagged file so the new unit test asserts
  the log line the same way — the precedent is `foreignFiringSkipTestMetrics` (`actions_test.go:2476-2487`).

## Adopter seam inventory

The surface is a metric label and a log field, read by an operator, not a Go API.

- **What must they know?** `foreign_authority` now means only an imported entity; a cron or malformed-entity
  dispatch reports `unresolvable_firing_entity`. A dashboard or alert keyed on the bare counter (no label filter)
  sees no change in total; one filtered on `reason="foreign_authority"` sees cron noise disappear.
- **What happens if they do nothing?** Nothing breaks. A cron-heavy deployment sees `foreign_authority` drop to its
  true rate and the new label carry the remainder.
- **Where do they find out?** The migration note's foreign-firing passage and the `graph-ingest` spec.
- **What should they have to know?** Nothing — the label was wrong, and a truthful label is not a new obligation.
  Prefer observation to prediction: the executor observes which check failed rather than predicting "foreign".

## Non-goals

- Not silencing the cron case. It is a counted, logged skip by design (`class:unobserved-skip` is the class this
  repo fights); only its name changes. Whether a by-construction skip should log at Info is a separate question
  and is not decided here.
- Not propagating the six structural `EntityIDReason*` tokens into the rule metric. One token covers "could not be
  established"; six would widen an operator vocabulary for a distinction the operator cannot act on differently.
- Not changing `ValidateEntityIDAuthority`, its reason vocabulary, or the `entity-id-contract` spec.
- Not touching the graph-ingest mutation rejection reasons (`mutation_rejections{reason="authority_foreign"}`),
  which are a different surface with a correct label.
- No exported symbol added or changed. No config knob.
- Residual, recorded not fixed: `ExecutionContext.RuleID()` reads `State.RuleID` and returns `""` for a cron
  context, which carries the rule's ID in `Schedule.ID` instead. The skip line this change owns falls back to
  `Schedule.ID` on its own line; every OTHER cron-path log line that reads `RuleID()` still logs an empty
  `rule_id`. Widening the accessor touches every caller that keys on it and is not this issue.
- Not adopted by `graph/inference/hierarchy.go:217`, the one other production reader of
  `ValidateEntityIDAuthority` that collapses any error to "foreign": it emits no reason label, so there is
  nothing to mislabel and no adoption owed.

## Consumers

No sister repo reads the counter through code. Operators of any deployment that runs cron `publish_agent` rules
with graph integration (semsource, semops when it returns) read it on a dashboard.
