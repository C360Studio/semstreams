# ADR-106: Beta → RC → v1 Exit Criteria, and the Two-Tier Surface Freeze

## Status

**Accepted (2026-09-02)** — owner rulings in session, recorded on #1247. Five rulings, taken after an after-action
review of the repository record and a `semstreams-judge` pass on the draft criteria (the judge's amendments were
adopted; its recommendation is not itself a ruling). The RC-4 instrument is #1246. This ADR records the decision;
the mechanics of each gate live with the gate.

## Context

`v1.0.0-alpha` was tagged 2026-03-03 with no stated exit criteria, and 84 further tags shipped that month. 258 tags
later the project is still in beta, and nothing anywhere said what beta must prove to become an RC, or what an RC
must prove to become 1.0. The `v1.0.0-rc.1` milestone existed with two issues in it and no definition behind it.

The measured record says the dominant defect shape is **a surface declared, wired partially, and never walked
end-to-end** — and that it is uniform across the project's life rather than concentrated in any early period. The
surfaces behind `class:advertised-absent` were introduced on 2025-11-17, 2025-11-19, 2025-12-02, 2026-01-28,
2026-03-28 and 2026-04-21: after CI, after `CLAUDE.md`, after agents, after ADRs, and after issue tracking. **Every
governance mechanism added so far has been process, and none of them stopped production of the class.** That is the
fact this ADR is written against.

Two further measurements bound the decision:

- The framework exposes **10,886 declarations across 158 non-internal packages**; sister repositories import **67**
  of them. 58% of the public surface has no external consumer.
- **27 of 62 comparable Tier 1 packages (44%) carried incompatible API changes across beta.160..HEAD**, a ~3-week
  window. `component` alone changed ~29 exported declarations, including the whole `Registry` surface.

That last number is why the paused sisters are paused. They are not dormant from disinterest; they are waiting out
back-to-back breaking changes rather than paying the migration repeatedly.

## Decision

### 1. Stress-dimension coverage — every project in the stage table stays in scope

Current adopter status is **not** evidence that a dimension is unwanted. The paused sisters are paused *because* the
framework is in back-to-back breaking changes, so reading dormancy as disinterest is circular: surfaces would be
deleted because adopters stopped tracking, and adopters stopped tracking because the surfaces kept moving. This
reinforces the standing product-boundary rule — a `grep` for callers answers "is this wired", never "is this
wanted".

V1-1 is therefore the plain formulation: **every sister in the docs-site stage table migrated to the RC.**

### 2. The surface freeze is two-tier

Freezing all 158 packages is neither achievable nor useful: it would promise compatibility on a majority of packages
nobody imports, and would block the pre-RC surface-removal pass now in flight.

- **Tier 1 — frozen, semver-binding at 1.0.** The Go packages any sister imports (`release/tier1-packages.txt`,
  62 entries), the 33 component schemas in `schemas/`, the payload envelope, the entity-ID grammar (ADR-102), and
  the subject namespace (ADR-093, ADR-098).
- **Tier 2 — explicitly not frozen.** The remaining ~91 non-internal packages, to be moved under `internal/` or
  published as unstable. **This is the removal lane**: deletion continues here through RC without touching the
  freeze.

Adding a line to the Tier 1 list is a deliberate widening of the 1.0 compatibility promise.

### 3. Auth lands pre-freeze; authorization policy does not

The `Principal` type and middleware seam from #1205 land **before** the freeze: a principal that touches data shapes
lands in Tier 1, and getting its shape wrong after 1.0 is an incompatible change across every gateway. Per-surface
authorization policy (#882's authz half, #854, #211) stays post-v1.

This confirms the existing 2026-08-31 ruling on #1205 (Phases 1+2, Phase 3 out); it does not amend it.

### 4. Machine guards over label counts

A `class:X open = 0` gate is satisfiable by not labeling. **Zero-because-clean and zero-because-stopped-looking are
indistinguishable**, which is the same defect as an absence claim from a search whose errors were discarded.

Therefore every class-count gate pairs with a named mechanism that would catch a *new* instance of the class — or
states plainly that it cannot, and records an owner acceptance of the residual risk. Silence is not an option.
Determinism is the priority: a gate a machine evaluates beats a gate a person attests.

### 5. The reset boundary is the frozen set

- An **incompatible change inside Tier 1** forced by a sister migration **resets to a new RC**. That is the honest
  falsification of the freeze.
- A change **outside Tier 1** lands on the RC and is recorded on the #753 scoreboard.
- A **compatible addition to Tier 1** does not reset, but must pass the walked-path guard (RC-6) before the tag that
  ships it — it is a brand-new surface with one adopter walking it under time pressure, which is the dominant defect
  shape being minted live.

Rationale for the carve-out rather than the strict form: with 21 breaking commits in three weeks, the strict form
near-guarantees a reset on the first migration and another on the next. **A gate that predictably requires a waiver
teaches everyone to grant waivers**, which is strictly worse than a narrower gate that always holds.

### 6. Migrations are staged, not simultaneous

Two or three canaries with the widest surface migrate first — semteams (30 packages), semsource (30), semmachina
(30). The remaining six follow only once the canaries land clean, and their near-zero cost is the evidence that 1.0
is real.

Without staging, one reset under ruling 5 strands eight completed migrations, which is the exact churn the freeze
exists to end. semdragon imports the most packages (32) but sits 141 betas back, making it the most expensive
canary rather than the best one.

## The criteria

### Beta → RC

| | Criterion | Instrument |
|---|---|---|
| RC-1 | Surface subtraction complete — `class:dead-surface`, `class:advertised-absent`, `class:phantom-config` cleared | each with a mechanism against recurrence (ruling 4); #1203 is the ruling vehicle |
| RC-2 | Nothing fails silently — `class:silent-noop-surface`, `class:swallowed-degrade`, `class:unobserved-skip` cleared | likewise; #1204 is the boot-honesty batch |
| RC-3 | Every stressed capability walked on a booted binary | `class:e2e-gap` cleared, **plus** a guard failing any tier that reports `assertions_run=0` (#1195, #1238) |
| RC-4 | **Zero incompatible Tier 1 changes for 30 consecutive days**, with at least one active sister tracking a tag inside the window | `task api:compat` (#1246) |
| RC-5 | Feature freeze legible in the tracker | the 27 open pre-v1 `enhancement` issues relabelled honestly |
| RC-6 | No exported surface exists at RC without a walked path — a spec scenario citing it and an assertion exercising it on a booted binary — with an explicit **recorded exemption list** | mechanism is separate work |

RC-4 is deliberately a **calendar** window under adopter load, not a tag count: tag cadence is a free variable the
release process controls, and 84 tags in 2026-03 is the proof that "two consecutive clean tags" is satisfiable in an
afternoon. It is also measured by diffing artifacts, not by reading `!` markers — that marker is an author
convention, and beta.162 read as "purely additive" while its tag range carried 8 of them.

RC-6 carries the load here. The other criteria drain the current backlog of the dominant defect class; RC-6 is the
only one that changes what happens after 1.0 ships. Its exemption list is how the product-boundary rule stays
honest: "unwired here may still be wanted" becomes a *recorded exemption*, never an unlabeled default.

### RC → v1

| | Criterion |
|---|---|
| V1-1 | Every sister in the stage table migrated to the RC, staged canaries first (ruling 6) |
| V1-2 | Migration cost is the proof; reset boundary per ruling 5 |
| V1-3 | #753 is the scoreboard, one row per stress dimension |

## Consequences

- The Tier 2 boundary must actually be drawn — ~91 packages moved under `internal/` or marked unstable. Until then
  "Tier 2" is a claim, not a fact, and adopters cannot tell which half of the surface they are standing on.
- Known migration debt is now named rather than implied: `pkg/ownership` (deleted 2026-08-05 by `dbdc9bd8`; semops
  has 51 import sites, semdragon 3) and `engine`/`flowstore`/`flowtemplate` (retired 2026-08-27 by #1116; semteams
  imports all three, with `docs/operations/migration-beta162-to-beta163.md` naming the exact broken lines).
- RC-4 gives the paused sisters a public signal to watch, so they can time their return instead of guessing. That is
  the point of publishing the number rather than merely gating on it.
- The criteria are falsifiable and may fail. If the dated census of when class-labeled surfaces were *introduced*
  shows production stopped after ~2026-05, RC-6 is prudent rather than required, and the sequencing can relax. That
  census has not been run; it is the load-bearing unproven fact under ruling 4.

## What this ADR does not decide

- The RC-6 mechanism. `task spec:properties` runs spec→code today; RC-6 needs the reverse direction, code→spec.
- Whether `apidiff` covers every Tier 1 member. It covers the Go packages. Schemas have `task schema:check-changes`;
  the payload envelope, entity-ID grammar and subject namespace need contract tests that do not yet exist.
- Any RC or 1.0 date. These are conditions, and the failure this ADR exists to prevent is exiting a stage on a date
  instead of on a demonstrated condition.
