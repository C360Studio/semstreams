# Pattern Adoption

A design can enumerate every spelling of the fact it models, pass an independent
`INVENTORY PASS`, and still reinvent a shape this repository already solved. That
is structural, not carelessness, and this page is the worked case that explains
why the surface inventory grew a fifth category.

Cited from `.agents/contracts/semstreams-architect.md` (surface inventory,
category 5) and `.agents/contracts/semstreams-reviewer.md` (diff-level check).

## The gap the first four categories cannot reach

Surface-inventory category 2 already states the right rule: enumerate *"every
current spelling of the fact being modeled"*, and treat more than one home as a
defect to consolidate toward one shared primitive.

It failed anyway, because of what it is scoped to. Categories 1–4 all ask about
**the fact**. A pattern is not a fact — it is a **problem shape**, and the same
shape attaches to many unrelated facts.

`processor/graph-ingest/authority_gate.go` is not another spelling of "loop token
shape". It is the same *shape* — admit-or-refuse at a seam — attached to a
different fact. No question in categories 1–4 reaches it. The reviewer
re-derives the same fact-scoped categories independently, so an independent
review inherits the identical blind spot.

## The shape was already solved

Three issues (#1225, #1227, #1228) reported these elements missing on the agentic
plane. Every one of them already existed on the graph plane, verified at
`0a40ddf3`:

| Element | Pin |
| --- | --- |
| One home, called at every seam, on every lane | `processor/graph-ingest/authority_gate.go:39-40` |
| Structural check first, *"so an authority reason never masks a malformed candidate"* | `processor/graph-ingest/authority_gate.go:40-42` |
| Explicit carve-out for what is deliberately not gated | `processor/graph-ingest/authority_gate.go:44-46` |
| Classified refusal carrying a `Code` | `pkg/types/entity_id_authority.go:8` |
| Typed `Detail` keys on the refusal | `pkg/types/entity_id_authority.go:17-18` |
| One home for the metric mapping, *"so the fact lane and the mutation lane cannot disagree"* | `processor/graph-ingest/authority_gate.go:55-58` |
| One named log string, so a test pins the production text | `processor/graph-ingest/authority_gate.go:33` |

The comment at `:40-42` is the punchline: **form-before-authority ordering is
exactly the relationship between #1228 and #1227**, already answered on the graph
plane under ADR-102, months before either issue was filed.

## Old debt or active failure? Both, and the split is datable

| What | Born |
| --- | --- |
| `CreateLoopWithID`, `loop_tracker.go` | 2026-01-28 / 2026-01-31 |
| `errs.ClassifiedCode` (ADR-060, #336) | 2026-06-23 |
| `pkg/types` authority primitive (#1119) | 2026-08-28 |
| `graph-ingest/authority_gate.go` (#1148) | 2026-08-29 |

The agentic plane predates every pattern it should use by five to seven months.
Nobody ignored a pattern that did not exist — the **origin** is genuine old debt.

The **perpetuation** is current, and it is the part a contract can reach: nothing
sweeps an older plane onto a pattern when that pattern lands, and until category
5 no inventory question surfaced the pattern on a later edit.

## Case study: PR #1210

The cleanest instance available, because the diagnosis and the fix are in the
same commit message. `0a40ddf3` states:

> a dispatch collision was SILENT, because `CreateLoopWithID` overwrites the
> colliding record and context manager, **merging two conversations**.

The overwrite is named, by symbol, as the mechanism. The fix shipped one layer
above it: widen the token to a full UUID so collisions become improbable
(2^122). The unconditional overwrite at `processor/agentic-loop/state.go:170-180`
is untouched — `m.loops[loopID]`, `m.pendingTools[loopID]`, and
`m.contextManagers[loopID]` are all assigned with no create-vs-exists check.

Later investigation established that the overwrite **does not need a collision to
fire**: it fires on every legitimate `reply_to` continuation, and on the
AutoContinue path it lands on live in-flight state. A probability argument was
applied to a defect that occurs deterministically.

In fairness, every individual step was defensible. #1192's owner ruling *was*
scoped to token shape, and filing the residue as #1225/#1227/#1228 was correct
process. That is what makes this a case study rather than a reprimand — the shape
was missed while someone typed the sentence describing it.

## Applying category 5

Name the shape before you search, and search on **every** plane:

- admit-or-refuse at a seam
- create-vs-exists
- read-through over a cache
- classified refusal plus observed signal
- authority delegation
- bounded dispatch

Then cite the closest existing instance at `file:line` and state either that the
design adopts it or why it does not. `"No existing instance"` is a claim closed
by the searches that came up empty, exactly like every other category.

The search is deliberately *not* keyed on the fact. Searching for "loop token"
finds nothing on the graph plane; searching for "what refuses a malformed
candidate at a boundary" finds `authority_gate.go` immediately.

## A usable proxy for where this drift sits

`errs.Classified*` adoption is countable per package and is a first proxy for
where a refusal cannot be observed. Measured at `0a40ddf3`, non-test files only:

```bash
git grep -o 'errs\.[A-Za-z]*'            -- '*.go' ':!*_test.go'
git grep -o 'errs\.Classified[A-Za-z]*'  -- '*.go' ':!*_test.go'
```

`processor/agentic-dispatch` carries **37** `errs.` references and **zero**
`errs.Classified*` — the uncoded `Wrap*` family only, which has no `Code` and no
`Detail`. **27** directories in this tree use the coded family; dispatch is not
one of them. That is the mechanical reason a dropped message there is a *silent*
drop: the package has no vocabulary in which a refusal can be observed.

Treat the raw `Wrap*` count as a proxy that **overstates badly**. Wrapping a
downstream error for context is exactly what `Wrap*` is for. The pattern applies
only to **a refusal at a seam that a caller or a metric must branch on**.
