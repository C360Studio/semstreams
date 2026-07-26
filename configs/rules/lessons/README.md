# Agent-lesson gated lifecycle — reference rule pack

Reference configuration for the `agent.lesson.*` gated lifecycle (ADR-080,
change `agent-memory-lesson-substrate`). A lesson is born `proposed`; only
`active` lessons are injected into future loop briefs. Two lanes move a lesson
between states through the contract-bound public projection mutation client —
never a rule-local transport or append.

## The owned projection group

`lesson-lifecycle-rulepack.json` declares the lesson lifecycle **owned group**
in `projection_contracts`:

| Predicate | Role |
|---|---|
| `agent.lesson.status` | `proposed` → `active` → `retired` / `superseded` |
| `agent.lesson.superseded-by` | entity ID of the replacing lesson |
| `agent.lesson.retired-at` | retirement timestamp |

These three are **mutable** and single-valued, so they belong in the named
`replace-owned` group `lesson-lifecycle`. A replacement supplies the complete
desired state for that group: omitting `superseded-by` or `retired-at` deletes
their existing values. Predicates in other groups and birth predicates are
untouched. This mirrors `lessonRecordProjectionContract()` in both
`cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` (the boot-time owner
registry). The **immutable birth predicates** — `agent.lesson.created-at`,
`category`, `polarity`, `severity`, `summary`, `detail`, `injection-form`,
`evidence`, `applies-to`, `observed-role`, and `agent.action.executed-by` — are
stamped once at emit and are deliberately **absent** from any owned group, so a
`replace_owned` action can never overwrite a lesson's identity or evidence.

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

`Retire` (status → `retired` + `retired-at`) and `Supersede` (status →
`superseded` + `superseded-by`) live on the same writer; retirement requires no
evidence check.

## Lane 2 — mechanical rule transition (this config's example)

For declarative, hot-reloadable transitions that assert nothing about the
world, a rule `replace_owned` action writes an **owned** lifecycle predicate
directly. This is a **mechanical primitive** — products wire it to their own
genuine trigger. The shipped example, `auto-retire-suppressed-lesson-at-birth`,
is a **birth-time suppression** policy:

- **Trigger** (both reachable at birth): `agent.lesson.status == "proposed"`
  (the born state) **and** `agent.lesson.category == "deprecated"` (an
  illustrative product convention on the open `category` taxonomy).
- **Action**: `replace_owned agent.lesson.status → "retired"` through contract
  `agentic.lesson-record`, group `lesson-lifecycle`.

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
  predicate. (Attempting a `replace_owned` on `category` HARD-FAILS the load —
  it is not in the owned group; see the config test.)
- It performs **no evidence-existence check**. That check is exactly what makes
  a `proposed → active` promotion honest, so **promotion must route through
  Lane 1** (`LessonCurator.Promote`), never a bare rule.

Swap the trigger for your own reachable signal — another owned lifecycle
predicate (`agent.lesson.superseded-by`, `agent.lesson.retired-at`), a
rule-matchable birth fact (`severity`, `polarity`, `applies-to`,
`observed-role`), or `$now` in the object to stamp a timestamp — as long as the
predicate you **write** stays inside the owned group.

## Wiring

Point a rule processor at this file (or fold `projection_contracts` +
`inline_rules` into an existing ops rule pack). At load, the rule engine
HARD-FAILS unless every `replace_owned` action explicitly names a contract and
named `replace-owned` group and its literal predicate belongs to that exact
group. The shipped action passes because `agent.lesson.status` belongs to
`agentic.lesson-record` / `lesson-lifecycle`.
