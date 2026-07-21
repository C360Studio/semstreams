# Pre-v1 core hardening — program entry point

**This file is the baton.** Read it first, do the Next Action, update it last.
It is the only file that carries program state across sessions.

Opened 2026-07-21 · baseline `v1.0.0-beta.157`

---

## Next action

> **Land Track 0 as one PR.** Eight mechanical fixes, all with verified failure
> modes and no open design questions. Start with #610 — it is a live
> nondeterministic outage.

---

## How to read this file

Status words lie. That is the central finding of the audit that created this
program, and it applies to project tracking too. **Every row's state is a command
you run, not an adjective someone typed.** If the command disagrees with the
table, the command is right — fix the table.

```bash
gh issue list --state open --limit 100        # what is actually open
openspec list                                 # what is actually in flight
gh run list --workflow=sister-validation.yml  # whether the gate is actually running
git status --porcelain                        # whether the tree is actually clean
```

## Session protocol

1. **Start** — read this file, then run the four commands above and reconcile.
2. **Work** — the Next Action. One thing.
3. **End** — update the table from checkable state, then rewrite Next Action.

**WIP = 1 at the epic level.** Eight OpenSpec changes are already stalled in the
80–95% band; the last increment of each is disproportionately the observability
and guardrail work this program is about. Adding parallel epics is how this
becomes change number twelve through sixteen. Finish, then start.

---

## Evidence (stable — do not re-derive)

| Doc | Holds |
|---|---|
| `prev1-audit-synthesis.md` | reconciliation of two independent audits; convergent findings, disagreements resolved, the merged plan |
| `prev1-graph-core-audit.md` | Claude-side detail: the 13 phantom signals, per-subsystem findings with file:line |

Findings in those docs are **settled evidence**. If a session re-derives them, that
is wasted budget. Cite and move on.

---

## Track 0 — fix now, no design needed

One PR. No OpenSpec change; ceremony on mechanical fixes is how they reach 90% and
stop.

| # | Fix | Issue | Done when |
|---|---|---|---|
| 1 | rule processor: delete the ENTITY_STATES create path | #610 | issue closed |
| 2 | message-logger: `GetKeyValueBucket`, return 404 | #611 | issue closed |
| 3 | dedup key: fold in embedder type + model + cap | #612 | issue closed |
| 4 | two phantom e2e metrics | #615 | issue closed |
| 5 | `WithWorkers(max(1, …))` or explicit `workers` field | #620 | issue closed |
| 6 | tombstone branch → `DeleteEmbedding` | #614 | issue closed |
| 7 | sort before truncating at `graphrag.go:1176` | #621 | issue closed |
| 8 | `GenerateQuery` read-only (stop corpus pollution) | #619 | issue closed |

State: `gh issue view 610 611 612 614 615 619 620 621 --json number,state`

---

## Epics

Sequential, not parallel. Each is a candidate OpenSpec change **only if it carries
genuine spec deltas** — A, B, and C qualify; D and E do not.

| Epic | Scope | Issues | State |
|---|---|---|---|
| **A** — evidence cannot silently expire | body TTL, hydration signal, dedup identity, vector reconciliation, BM25 contract | #600 #601 #602 #612 #613 #614 #616 #619 #599 | not started |
| **B** — one community truth | level-0-only; disable LLM enhancement until ownership split; readiness gate | #606 #607 #608 #609 #617 #618 | not started |
| **C** — derived-state ownership | accept one retention ADR; owner ledger; extend the boot guard | #622 #527 | not started |
| **D** — consumer-path release gates | see prerequisite below | #615 + CI | **in progress** |
| **E** — semsource clean cut | GRAPH stream posture, dead bucket wiring | semsource#110 | not started |

### Epic D has a prerequisite that was not obvious

`ci.yml` runs **no e2e tier at all** — jobs are lint, test, build,
schema-validation, status-check. Building a verification matrix on top of a suite
no machine runs buys nothing. Order: automate a tier first, then strengthen
assertions, then extend coverage.

Shipped 2026-07-21: `.github/workflows/sister-validation.yml` — semsource e2e per
PR, semsource + semboids + `e2e:core` nightly, all against **this checkout** via
`go mod edit -replace`.

**Two things still open on D:**

- The workflow is **unproven in CI**. It was validated locally (the replace-and-build
  mechanic works and immediately caught real drift — semsource at beta.156 does not
  compile against main: `fusion.RetrievalClient.Resolve` now returns `[]fusion.Seed`,
  `Entities` returns `fusion.Hydration`). It has not yet run on a GitHub runner.
  `gh run list --workflow=sister-validation.yml` — empty means it has never fired.
- **Main has no required checks**, so this gate is advisory until made required.
  A green check that cannot block a merge is a notification, not a gate.

Also note `semspec-validation.yml` has **zero runs, ever** — itself an instance of
the phantom class. Fold into `sister-validation.yml` or delete; do not leave a
workflow that looks like coverage and has never executed.

### Decisions deliberately not made by the audit

These need an owner, not an implementer:

- **Community scope at v1.** Evidence favors shrinking: non-deterministic
  membership, uniformly unweighted edges, three redundant runs, 0–1 of 3 ground
  truth, level 2 has no consumer and level 1 has one e2e probe — and ADR-061
  already established community is post-hoc decoration on the primary search path.
  Recommended: level-0-only now, **disable LLM enhancement** until the ownership
  split, defer the split itself (ADR-scale: ~500 production LOC, storage-key
  contract change, pre-v1 state wipe).
- **Embedding readiness semantics.** "Terminal includes failed" is a decision, not
  a patch. Fits the ADR-084 health-gates frame.
- **BM25 tier contract.** Lexical index over an immutable snapshot, or stateless
  hashed TF. Do not add locks to the current hybrid and call it fixed.
- **ADR-061 is shipped but still marked `Proposed`.** Promote it, and note it never
  claimed the broader "the semantic tier does not affect the partition" contract —
  if that is what we want, say so explicitly.

---

## Timing — DECIDED

**Owner-approved 2026-07-21: do the breaking half now, in one wave.**

Not speculative. Sister lockstep against .157 is already due and verified —
semsource at beta.156 does not compile against main (`fusion.RetrievalClient.Resolve`
now returns `[]fusion.Seed` not `[]string`; `Entities` returns `fusion.Hydration`).
That cost is being paid regardless, so folding the dedup key change, the community
collapse, the config deletions, and the TTL fixes into the same lockstep is close
to free. Post-v1 every one of them becomes a compat shim maintained forever.

Practical consequence for whoever picks this up: **do not defer a breaking fix to
"after v1" on compatibility grounds.** That trade is already resolved. Breaking
changes still owe the house rule — a relevant e2e tier green before the tag
(CLAUDE.md), which is now partly automatable via `sister-validation.yml`.

## Explicitly not doing

Coverage targets (the mechanism that produced this) · more tests (ratios are
1.2–2.1 and the unit tests are good — the rot was in e2e) · more reference designs
(diminishing returns; they are structurally blind to the phantom class) · a twelfth
parallel OpenSpec change.

---

## Log

Append one line per session. Newest last.

- **2026-07-21** — Program opened. Two audits (Codex + Claude) reconciled into
  `prev1-audit-synthesis.md`. 13 issues filed (#610–#622) + semsource#110.
  `sister-validation.yml` written and locally validated; not yet run in CI.
  **Timing decided by owner: breaking wave now, one shot.** Next: Track 0.
