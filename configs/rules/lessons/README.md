# Agent-lesson gated lifecycle — reference rule pack

Reference configuration for the `agent.lesson.*` gated lifecycle (ADR-080,
change `agent-memory-lesson-substrate`). A lesson is born `proposed`; only
`active` lessons are injected into future loop briefs. Two lanes move a lesson
between states, both riding the canonical single-valued **reconcile** operation
(`graph.mutation.entity.reconcile`, ADR-091) — never an append.

## The local projection group

`lesson-lifecycle-rulepack.json` declares the lesson lifecycle **reconcile group**
in `projection_contracts`:

| Predicate | Role |
|---|---|
| `agent.lesson.status` | `proposed` → `active` → `retired` / `superseded` |
| `agent.lesson.superseded-by` | entity ID of the replacing lesson |
| `agent.lesson.retired-at` | retirement timestamp |

These three are **mutable** and single-valued, so they belong in this local
`reconcile` group. This mirrors the canonical built-in source declaration in
`internal/builtinprojection/contracts.go` (`builtinprojection.Contracts()`),
which the standard constructor consumes directly. The
**birth predicates** — `agent.lesson.created-at`,
`category`, `polarity`, `severity`, `summary`, `detail`, `injection-form`,
`evidence`, `applies-to`, `observed-role`, and `agent.action.executed-by` — are
stamped once at emit and deliberately **absent** from this group. Actions using
this unchanged local group cannot select those predicates for removal; another
locally authored contract receives no global protection from this declaration.

## Lane 1 — VALIDATED promotion (evidence-existence-resolved)

Promotion `proposed → active` is the guarded transition: before flipping a
lesson active, every cited `agent.lesson.evidence` entity must **exist** in the
graph. Use the reference writer
`processor/agentic-tools/lesson_promotion.go` (`LessonCurator.Promote`), which

1. reads the lesson's cited evidence entity IDs (via `graph.ingest.query.entity`),
2. resolves that each cited entity exists,
3. replaces `agent.lesson.status` `proposed → active` only if **all** resolve —
   otherwise it **refuses** and the lesson stays `proposed`.

Promotion is **operator/product-invoked**, not an agent tool (ADR-080 makes
operator/product review the default gate; there is no `promote_lesson` tool). A
product wraps `Promote` in a curation UI or an explicit auto-promotion policy.

Go products include `LessonProjectionContract()` in their composition-root-local
mutation client and inject that client's narrow reconcile/read capabilities through
`NewLessonCurator`. They do not reproduce the built-in contract literals. This rule
pack's local `projection_contracts` entry is a separate config-authored mechanical
rule surface and does not construct the validated curator.

`Retire` (status → `retired` + `retired-at`) and `Supersede` (status →
`superseded` + `superseded-by`) live on the same writer; retirement requires no
evidence check.

## Lane 2 — mechanical rule transition (this config's example)

For declarative, hot-reloadable transitions that assert nothing about the
world, a rule `reconcile_predicates` action writes a lifecycle predicate
directly. This is a **mechanical primitive** — products wire it to their own
genuine trigger. The shipped example, `auto-retire-suppressed-lesson-at-birth`,
is a **birth-time suppression** policy:

- **Trigger** (both reachable at birth): `agent.lesson.status == "proposed"`
  (the born state) **and** `agent.lesson.category == "deprecated"` (an
  illustrative product convention on the open `category` taxonomy).
- **Action**: `reconcile_predicates agent.lesson.status → "retired"`.

Effect: a freshly-born proposed lesson a product never wants injected (e.g. it
documents an already-fixed anti-pattern, kept only for audit) is retired
immediately — durable in the graph, never entering the promotion queue or a
brief.

Honest scope — read before adopting:

- It acts **at birth on a `proposed` lesson**; it does **not** retire an
  already-`active` lesson. To retire an active lesson, use
  `LessonCurator.Retire` (Lane 1).
- It does **not** mutate `category`. `category` is an **immutable,
  identity-bearing** birth predicate (part of the content-derived entity ID);
  no supported lane changes an existing lesson's category. The rule only
  **reads** `category` as a condition and **writes** the owned `status`
  predicate. (Attempting a reconcile of `category` HARD-FAILS the load —
  it is not in the reconcile group; see the config test.)
- It performs **no evidence-existence check**. That check is exactly what makes
  a `proposed → active` promotion honest, so **promotion must route through
  Lane 1** (`LessonCurator.Promote`), never a bare rule.

Swap the trigger for your own reachable signal — another lifecycle
predicate (`agent.lesson.superseded-by`, `agent.lesson.retired-at`), a
rule-matchable birth fact (`severity`, `polarity`, `applies-to`,
`observed-role`), or `$now` in the object to stamp a timestamp — as long as the
predicate you **write** stays inside the owned group.

## Wiring

Point a rule processor at this file (or fold `projection_contracts` +
`inline_rules` into an existing ops rule pack). At load, the rule engine
HARD-FAILS if any `reconcile_predicates` predicate is not a literal inside a
`reconcile` group in this pack's `projection_contracts` (ADR-091
Decision 3) — `retire-deprecated-lesson` passes because `agent.lesson.status`
is in the reconcile group above.
