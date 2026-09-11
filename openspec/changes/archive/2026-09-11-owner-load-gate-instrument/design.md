# Design — owner-load-gate-instrument (#1284)

**Status: RULED TARGET STATE.** The owner ruled on 2026-09-11 (#1284 comment 5635299542). This document records the
decided shape, not a set of options; § 5 keeps the option comparison as rationale, marked decided. Implementation
follows separately — no production or test file is edited by this change's design phase.

Revision 3. Premises pinned at `main@29187077` in `inventory.md` (92 pins, `task inventory:verify` exit 0). § 11
records everything withdrawn or corrected across three revisions, including **one claim this revision voids that
revision 2 presented as its strongest evidence**.

**The supervised re-run ruling 1 requires has been executed** at revision `60c79736` on the current pin; both
profiles passed. Its numbers are in § 3 (P25-P27) and they set the re-derived percentile budgets in § 2, so no
budget in this design is a placeholder.

## 1. The ruling

Two rulings, quoted from #1284 comment 5635299542:

1. **"M5 with rerun seems best."** The CI profile is demoted to a regression guard, and ADR-077 §8 condition 4's
   activation evidence is rehomed onto the supervised run — which owes a fresh measurement, because the existing
   acceptance record was taken at `0a7af288` on `nats:2.12.4-alpine` and `docs/operations/32-...:43` declares those
   rows historical.
2. **The demoted CI per-operation budget is deleted, not widened.** The framework's already-enforced 5s KV deadline
   (`natsclient/kv.go:39`) becomes the guard. `p95Budget`/`p99Budget` are kept and re-derived from the supervised
   run. "Prohibited-B" existed to protect condition 4's evidence; under ruling 1 that evidence has moved, so the
   prohibition no longer binds the CI profile.

The owner's rationale, in the house's terms: the framework already **observes** the real bound and fails on it as a
typed error, so a second **predicted** budget sitting below an enforced bound is exactly the knob the adopter-seam
rule says to delete.

## 2. The ruled target state

**In the CI profile** (`ownerLoadCIProfile`, `owner_filter_load_integration_test.go:56`):

- `operationBudget` is **deleted**, and with it both per-repetition wall-clock assertions — `:489` in the
  measurement phase and `:376` in the concurrent load phase. The per-operation ceiling becomes the typed error that
  `require.NoError` at `:487` and `:374` already raise when `KeysByFilter` reaches its deadline.
- `p95Budget` and `p99Budget` are **kept at `3 * time.Second`** in this change. **Owner ruling 2026-09-11: the
  re-derivation to `1 * time.Second` is DEFERRED** until the per-repetition recording produces within-filter
  adjacency data, so that this change is strictly flake-reducing and nothing is tightened on unmeasured exposure.
  The analysis below is retained as the follow-up's starting point, not as this change's content. The basis, stated
  in the unit the measurements are in — the discipline #1286 exists for:

  | Quantity | Measured | 1s is |
  |---|---:|---:|
  | Worst healthy CI p95, quiet box (`name-forward`) | 77.861 ms | 12.8x |
  | Worst healthy CI p95, shared runner (`predicate-forward`, run 33208133273) | 175.4 ms | 5.7x |
  | Worst healthy CI single sample, shared runner (run 33208133273) | 389.0 ms | 2.6x |
  | Absolute headroom above that worst healthy sample | — | 611 ms |

  **Why not keep 3s.** At 3s the gate is ~38x the quiet-box p95 and ~17x the shared-runner p95: it would not notice a
  10x regression. The realistic regression class here — a filtered `ListKeys` degrading to a full-bucket scan with
  client-side filtering — is a 5-20x class, which 1s mostly catches and 3s catches none of.

  **Why 1s is not a return to the deleted gate.** At `repetitions: 5` the percentiles select `durations[3]`, the
  second-largest of five, so **two** repetitions must breach for the gate to trip. It is immune to one stalled
  repetition at any threshold. The deleted `operationBudget` tripped on **one**, which is the shape that fired five
  times. 1s is therefore strictly safer than the status quo ante while being a much better detector than 3s.

  **The exposure that buys, stated plainly.** A sustained slowdown that inflates two consecutive repetitions past 1s
  would trip it. That shape has not been observed: in run 34367949188, `incoming-forward` hit 3.356 s while the same
  run's `predicate-forward` stayed at p50 153 ms / max 190 ms, so the stall was absorbed by the repetition in flight
  rather than spread across the phase. That is cross-filter evidence, not within-filter — the firing filter's own
  repetitions are discarded (P5) — so the exposure is real and unquantified until the recording change lands. If it
  fires, the recorded distribution will say so and the number moves **with a recorded basis**, which is precisely
  what the previous regime could not do.
- Everything else the guard asserts stays exactly as it is: exact match sets (`:488`, `:375`), post-churn convergence
  (`:443`), bounded dispatcher queue (`:402`), temporary consumers returning to every per-store baseline (`:408`,
  `:414`), released subscriptions (`:448`), zero slow consumers (`:450`), and the NATS RSS bound.
- The distribution is recorded on every run in **submission order**, not only as a sorted summary —
  `assertOwnerLoadLatency` sorts at `:497`, and `t.Logf` reaches CI only for failing tests (`:311` passes no `-v`),
  so today a passing run records nothing at all.

**In the evidence home:**

- A supervised run at the current pin (`GRAPH_INDEX_OWNER_FILTER_FULL=1`, and the default 5k profile on the same
  quiet box) is recorded in `docs/operations/32-...` with revision, host, runtime, pin and timestamp, replacing the
  `0a7af288` rows. That record, not CI, is condition 4's evidence.

**In the contract text:**

- `openspec/specs/graph-index/spec.md:184`'s ADR-065 attribution is repaired to ADR-077 §8 condition 4.
- The delta states what the regression guard asserts and that the ceiling is observed, never restated as a budget.
- The ADR-077 amendment is drafted in § 10 and stays **unapplied** pending Q3.

**Two contract tests are consequences, not collateral.** `TestOwnerLoadCIProfile_ContractedBudgets:29` pins
`operationBudget == 3s` and `:32`/`:33` pin the percentiles at 3s; `TestOwnerLoadPercentiles_DoNotCoverTheMax:66`
asserts in prose that "the per-rep gate MUST remain". Both were written to stop exactly this change from happening
by accident. Under the ruling they are rewritten deliberately, and `tasks.md` § 4.4 requires the replacement to pin
the *new* contract — that the CI profile carries no per-operation budget and that its percentiles match the
supervised record — so the anti-relaxation property survives the move rather than being dropped with the assertion
it guarded.

## 3. Premises, each with its measurement

| # | Premise | Measurement |
|---|---|---|
| P1 | The framework enforces a 5s ceiling on the measured operation | `natsclient/kv.go:39`, guarded at `:69`, applied for this operation by `applyTimeout` at `:538`; harness passes no override (`:168`) |
| P2 | Production runs against the identical bound | `processor/graph-index/component.go:927`, `:953` |
| P3 | Four budget firings sit in the 3s-5s band where production succeeds | 3.356s, 3.515s, 4.760s, 4.805s |
| P4 | The framework's ceiling is already asserted and has already fired | run 33208133273 failed at `:487` with `context deadline exceeded`; partial-set refusal enforced at `natsclient/kv.go:589`, `:592` |
| P5 | The instrument discards its passing repetitions | `require.Less` at `:489` calls `t.FailNow()` before `:492` |
| P6 | A passing run records nothing | `t.Logf` reaches CI only for failing tests; `scripts/run-integration-tests.sh:311` passes no `-v`; run 34475316237 (`main`, green) prints only `ok ... 60.725s` |
| P7 | At `repetitions: 5` the percentiles never examine the max | `(5-1)*95/100 == (5-1)*99/100 == 3` |
| P8 | Only 5,000-key forward filters ever fire | all 5 events are `*-forward`; owner filters measure 2-6ms |
| P9 | The measurement shares a runner with uncapped parallel Docker packages | `scripts/run-integration-tests.sh:304`, `:311`; one `ubuntu-latest` |
| P10 | There are two per-repetition budget gates | `:489` and `:376` |
| P13 | The distribution the 3s budget was argued against no longer exists | gh#750: `p50=99.784608ms p95=697.726516ms max=2.23697341s`; three current runs: p95 ~157-175ms, max 166-389ms |
| P18 | The full profile's absolute ceiling is dead code | `:85` sets 10s; every measured call is bounded at 5s and an expiry fails at `:487` first |
| P19 | `assertOwnerLoadLatency` destroys submission order | `:497` sorts before any percentile or log line |
| P20 | The evidence home is currently historical | `docs/operations/32-...:80`+ measured at `0a7af288` on `nats:2.12.4-alpine`; `:43` declares those rows historical |
| **P21** | **No budget choice reduces the residual below one event** | healthy forward max 389ms; the five events imply stalls of ~3.2s, ~3.3s, ~4.6s, ~4.6s and >=4.7s. Absorbing the largest needs ~5s — which is where `KeysByFilter` fails as a typed error instead, and event 4 already did |
| **P22** | **gh#220's >=3x rule was satisfied and was never the problem** | 3s / 389ms = 7.7x. A stall *adds* seconds rather than multiplying them, so the load-bearing quantity is absolute headroom (2.61s), not the ratio |
| **P23** | **The sibling A/B is void** | `predicate_layout_smoke_integration_test.go:208` builds its store with no timeout override, and `:479` (`require.NoError`) precedes `:480` (`require.Less` against a 10s budget), so a stall fails as a deadline error and never reaches the comparison |
| **P24** | **The sibling's widening rests on a unit error** | `:90`-`:92` cites "healthy p95 already 2.65s"; the smoke's table is headed `p95 ms`/`p99 ms` (`docs/operations/32-...:160`) with worst row 333.641500 ms (`:168`), and `2.664542` is the *owner-filter* harness's 5k CI PREDICATE p95 in ms (`:120`, under `:133`). Filed as **#1286**; not repaired here |
| **P25** | **Supervised 21k baseline, current pin** | rev `60c79736`, PASS 43.17 s, exit 0. Worst measurement-phase p95 **311.449 ms** (`incoming-forward`, 16 workers), worst p99 **320.157 ms** and worst max **396.719 ms** (`predicate-forward`, 16 workers). Worst concurrent-phase p95 55.978 ms (`incoming`, 16 workers). Against the current 3s/5s full-profile budgets that is 9.6x / 15.6x headroom |
| **P26** | **Supervised 5k CI baseline, same quiet box** | PASS 2.12 s, exit 0. Worst measurement-phase p95 **77.861 ms** and worst max **80.068 ms** (`name-forward`); worst concurrent-phase p95 4.397 ms (`name`). Fastest filter's p95 is 771.792 µs (`predicate-owner`) — a **108x spread** under one shared budget |
| **P27** | **The contention tax, measured** | same profile, same workload: subtest **2.12 s** quiet against **8.64 s** on the shared runner (run 34367949188, `workers-4`; whole test 11.02 s) = **4.1x**; forward-filter p95 **78 ms** quiet against **157-175 ms** shared = **2.0-2.2x**. This is steady-state contention, a different quantity from the 3.2-4.8 s stalls, and it is the measured part of P9 |

**P22 generalizes past this harness.** A ratio-based headroom rule buys a slow assertion many absolute seconds and a
fast one almost none. Two assertions can both satisfy ">=3x" while one tolerates a 6-second stall and the other
tolerates 0.3 seconds. Where the threat is a stall, the rule needs to be stated in absolute seconds.

## 4. Why this shape — the architectural leg, and only that

**M5 survives on one argument: a shared-runner CI profile is the wrong home for a tight activation budget.** The
measurement is taken under `-race` with uncapped package parallelism on a shared `ubuntu-latest` (P9); the quantity
being asserted is a latency contract that authorizes production; and the two cannot both be served by one number.
Rehoming the contract onto a supervised run puts the assertion where the conditions are controlled, and leaves CI
asserting what CI can actually assert — correctness, convergence, resource bounds, and an order-of-magnitude
latency check.

**The empirical argument revision 2 led with is retired.** It claimed a same-package sibling had made this move and
fired zero times in 80 CI failures. P23 shows that sibling's budget cannot fire at all, so its silence is not
evidence about budgets; P24 shows the widening that produced it read another harness's milliseconds as seconds. The
sweep's only surviving output is the count of five owner-harness events. **Do not cite the A/B anywhere.**

**Deleting rather than widening follows from the same rule that produced this repo's adopter-seam discipline.** The
framework owns the deadline, enforces it, and reports the breach as a typed error the harness already asserts on
(P1, P4). Any number the test predicts is either below that bound — in which case it fails on stalls the framework
tolerates, which is the measured defect — or above it, in which case it can never fire, which is the `:85` and P23
defect. There is no correct third value. The knob gets deleted.

## 5. Recorded rationale — the options, DECIDED

Kept for the record. The ruling chose M5 + deletion; nothing below is open.

| Option | Shape | Outcome |
|---|---|---|
| E | Record the distribution in submission order before deciding | **Folded into the ruled state**, § 2; it is how the regression guard and the supervised record both show their numbers |
| M1 | Do nothing | Rejected. 4 of 42 CI failures since 2026-08-26, one on `main`, with a fifth on 2026-08-25 |
| M2 | Corroborate a breach with one re-measured set | Rejected. It keeps a predicted number under an enforced bound, and P21 means it still cannot absorb the largest observed stall |
| M3 | Raise repetitions, gate on percentiles | Partly absorbed: the percentile gates survive as the CI latency check. The repetition count is left open — § 7 residual 2 |
| M4 | Give the harness its own CI job | Not taken. It preserves a budget the ruling deletes; may still be wanted for other reasons — Q4 |
| **M5** | **Demote CI to a regression guard; rehome condition 4 onto a supervised run** | **RULED** |
| Prohibited-A | Raise `operationBudget` silently | Never in scope; M5 is the explicit rehoming, not this |
| Prohibited-B | Delete the assertion | **Released by ruling 2** for the CI profile only, because the evidence it protected has moved |

## 6. What the ruling costs condition 4, and what re-establishes it

**Lost:** per-PR enforcement of the latency contract. Between this change landing and the supervised re-run being
recorded, condition 4 has **no** current-pin evidence at all — P20 means it does not have any today either, so this
is a debt made visible rather than a debt created. Activation was already blocked; it stays blocked, now for a
stated reason.

**Re-established by:** the supervised run at revision `60c79736` on the current pin (`nats:2.14.4-alpine`, SDK
`v1.52.0`, Docker 29.7.2, Apple M3 Pro / 12 CPU / 38,654,705,664 bytes — the same host as the `0a7af288` record, so
only the pin moved). Both profiles passed: 21k full in 43.17 s, 5k CI in 2.12 s, exit 0 on both. The numbers are
P25-P26. What remains is publishing them as the in-tree evidence appendix and replacing the historical rows in the
acceptance record — `tasks.md` § 3.3-3.4. The spec delta makes "a superseded pin does not carry forward as evidence"
a scenario, so the next pin move re-arms the same obligation instead of silently inheriting.

**Retained in CI:** everything that detects a real layout regression. An over-matching filter, a rescan, or a
resurrected catalog fails the exact match-set assertions (`:488`, `:375`), the post-churn convergence check (`:443`),
or the percentile budgets — none of which a runner stall can forge in the way a per-repetition maximum can.

## 7. Residuals the ruling accepts, and what it leaves open

1. **One residual event class remains red, by design.** P21: a stall large enough to reach the 5s deadline fails as
   `context deadline exceeded`, and one of the five observed events already did. The ruling accepts this — "the
   residual is the genuine 5s deadline breach, which should be red."
2. **UNDERDETERMINED — the CI repetition count.** With `repetitions: 5`, p95 and p99 both index `durations[3]` and
   neither examines the max (P7). After `operationBudget` is deleted, the CI profile's only coverage above
   `durations[3]` is the 5s deadline, so a single genuine 4.9s operation passes CI. That is defensible for a
   regression guard — a layout regression moves p50 and p95 together — but it is a real change in coverage the
   ruling did not address. It also cuts both ways for the 1s budget: more repetitions make the percentile a better
   detector *and* harder for a two-repetition stall to move. Cost ~0.6-0.8 s per added repetition (~10-13 s for
   5 → 21) against a CI subtest that runs 8.64 s today. Recorded as **Q9**.
5. **UNDERDETERMINED — one constant covering a 108x spread.** P26: the CI profile's healthy p95 ranges from
   771.792 µs (`predicate-owner`) to 77.861 ms (`name-forward`) under a single `p95Budget`. At 1s the slowest filter
   has 12.8x headroom and the fastest has ~1,300x — which is P22's failure mode reproduced *inside* this profile: one
   constant buys wildly different absolute protection per assertion. Splitting owner-class and forward-class budgets
   would fix it. Recorded as **Q10**, with a second instrument below.
6. **UNDERDETERMINED — the full profile's own budgets, now that it is the evidence.** P25: measured worst p95
   311.449 ms and p99 320.157 ms against budgets of 3s and 5s, i.e. 9.6x and 15.6x headroom on a *quiet* box with 30
   repetitions. That was acceptable when CI carried the contract; it is loose for the run that now *is* the
   activation evidence. Folded into **Q7**, which already owns the full profile.
3. **UNDERDETERMINED — whether condition 4 keeps the number "3 seconds" at all.** Under the ruling the figure is no
   longer a CI assertion; it becomes a statement about the supervised run, whose fresh numbers may justify something
   very different. § 10's amendment draft proposes that the ADR state the guarantee and let the runbook hold the
   numbers, which is the ADR-versus-spec split this repo already uses — but that is a proposal, not a ruling.
   Recorded inside **Q3**.
4. **The supervised run must record both profiles.** The `0a7af288` record carries `5k CI` and `21k full` rows. CI's
   percentile budgets are a 5k quantity and cannot be derived from a 21k run, so the re-run records the default
   profile on the quiet box as well as `GRAPH_INDEX_OWNER_FILTER_FULL=1`. This is stated as task 3.2 rather than
   left implied.

## 8. Invariants, each with its spec home

- **I1 — One ceiling, observed.** An operation that reaches the framework-enforced deadline fails as a typed error;
  no predicted per-operation budget restates it at any value. Home: *"an operation that reaches the framework bound
  fails as an error"*.
- **I2 — A sub-deadline stall cannot forge a failure.** Home: *"a runner stall below the framework deadline does not
  fail the regression guard"*.
- **I3 — A layout regression still fails.** Home: *"a layout regression still fails the regression guard"*. This is
  the invariant that makes I2 safe, and the one a later property harness must be written against.
- **I4 — Evidence is recorded, ordered, and pin-scoped.** Every run records its per-filter durations in submission
  order; a supervised record carries its provenance; a superseded pin does not carry forward. Homes: *"the measured
  distribution is recorded on a passing run"* and *"a superseded pin does not carry forward as evidence"*.

## 9. Owner docket

- **Q1 — RULED 2026-09-11** (#1284 comment 5635299542): M5 with the supervised re-run; CI `operationBudget` deleted;
  `p95Budget`/`p99Budget` kept and re-derived.
- **Q2 — MOOT.** It asked whether the evidence repair could land independently of Q1, because its normative sentence
  retroactively voided every CI run as condition-4 evidence. Under the ruling CI runs are no longer condition-4
  evidence at all, so the sentence now scopes to the supervised run, where it is simply correct. Nothing separable
  remains to gate, and the change lands as one unit because the CI percentile budgets depend on the supervised
  record.
- **Q3 — OPEN. ADR form, and what condition 4 says.** In-place amendment note on ADR-077 §8 condition 4
  (`docs/adr/046-...:14` is the precedent), or a new ADR? ADR-107 is taken by the unmerged
  `claude/gh1267-honor-predicate-datatype` branch (`bc7d79cc`), so a new record would be 108 and would race it.
  § 10 drafts the M5 wording; it also proposes that the ADR stop carrying the numeric budget and let the runbook
  hold it (§ 7 residual 3).
- **Q4 — OPEN.** Is M4 (the harness in its own CI job) still wanted? The ruling removes its original justification —
  there is no longer a tight budget to protect — but the supervised run still has to happen somewhere, and a
  dedicated job is one answer to "where". File, drop, or fold into the supervised-run question.
- **Q6 — OPEN.** Confirm ADR-065 needs no edit; the citation defect is in the spec text, not the ADR.
- **Q7 — OPEN, now load-bearing.** The supervised run is the new evidence home, and `ownerLoadFullProfile`'s
  `operationBudget: 10 * time.Second` at `:85` is dead code under the same 5s deadline (P18). **Proposed resolution,
  following the ruling's own logic and NOT applied:** remove `operationBudget` from the `ownerLoadProfile` struct
  entirely and delete both per-repetition assertions for both profiles. The two values fail differently — CI's 3s
  sits below the enforced deadline and fires spuriously, the full profile's 10s sits above it and can never fire —
  but the conclusion is identical: neither is the ceiling, the deadline is. Leaving `:85` in place would keep a dead
  predicted budget in the very run that is now the activation evidence. **Owner confirmation required**, because it
  touches the supervised profile the ruling just promoted, and because ADR-077 condition 5 carries the same
  "10-second handler bound" phrase. **Second part, from § 7 residual 6:** the full profile's `p95Budget: 3s` /
  `p99Budget: 5s` now sit at 9.6x / 15.6x over the measured 311.449 ms / 320.157 ms (P25) on a quiet box. Should the
  run that is now the activation evidence also have its percentiles re-derived, or does its looseness stop mattering
  once the numbers are published in the record?
- **Q8 — OPEN, and now in scope because M5 rehomes evidence onto this runbook.** `docs/operations/32-...:70` has
  asserted "every operation <3s; p95/p99 <=3s" for the CI profile since 2026-07-18 while its own harness reads
  10s/8s/9s. That row describes the **smoke** harness (churn column `2 writers x 100` is
  `predicate_layout_smoke...:96`), so the repair belongs with **#1286**. What this change must refresh is the
  **owner-filter acceptance record at `:80`+**. Confirm the split.
- **Q9 — OPEN (§ 7 residual 2).** Does the CI profile keep `repetitions: 5` after the per-operation budget is
  deleted, accepting that a single sub-deadline outlier passes, or does it rise so the percentiles gain resolution?
  With the 1s budget this is no longer neutral: more repetitions make the percentile both a better detector and
  harder for a two-repetition stall to move. Cost ~0.6-0.8 s each against a CI subtest that runs 8.64 s today.
- **Q10 — OPEN (new, § 7 residual 5).** Two instrument questions the ruling did not reach, neither of which the
  architect applied:
  1. **A p50 floor as the primary regression detector.** At `repetitions: 5`, p50 is `durations[2]` — it needs
     **three** of five repetitions inflated to move, so it is strictly more stall-immune than the p95 gate, and it
     moves on exactly the thing a layout regression does: the typical case. A p50 budget of **500 ms** would be 6.5x
     the quiet-box worst healthy p50 (77.363 ms) and 3.2x the shared-runner worst healthy p50 (156.1 ms), and would
     catch a ~3.2x regression that the 1s p95 gate misses. The ruling named `p95Budget`/`p99Budget`, so this is
     offered as an **addition**, never a substitution — the ruled state in § 2 is complete without it.
  2. **Per-class budgets.** One constant currently spans a 108x range of healthy values (P26). Owner-class and
     forward-class budgets would give each assertion comparable absolute headroom, which is P22 applied inside the
     profile rather than across harnesses.

## 10. ADR-077 amendment — DRAFT for M5, OWNER-GATED, NOT APPLIED

Not written to `docs/adr/`. Follows the `docs/adr/046-...:14` precedent: a bolded amendment note inside the amended
ADR, mechanics in the capability spec and the runbook.

Insert immediately after `docs/adr/077-...:139` (condition 4):

> **Amendment (#1284, owner ruling 2026-09-11):** condition 4's workload — 5,000 hot members plus 20 spread
> predicates — and its prohibition on activating under a failed absolute budget both stand. What changes is where the
> evidence comes from and what bounds it. The continuously-running CI guard is a regression guard, not activation
> evidence: it proves exact match sets, post-churn convergence, bounded queue and consumer behaviour, and an
> order-of-magnitude latency check, and it carries no per-operation wall-clock budget. Condition 4 is satisfied by a
> **supervised run recorded against the current server and SDK pin**, captured in the Predicate Layout Evidence
> Runbook alongside condition 5's 21,000-entity run, with its revision, host, runtime, pin and complete per-filter
> distribution. The absolute ceiling on a directly measured key listing is the framework-enforced `natsclient` KV
> deadline, observed as the operation's own typed error and never restated as a predicted budget. Activation remains
> prohibited until that supervised record exists on the current pin.

**Open inside this draft (Q3, § 7 residual 3):** whether the amendment should also drop condition 4's literal "below
3 seconds". The draft above does — it states the guarantee and leaves the numbers to the runbook, which is the
ADR-versus-spec split this repo uses. Keeping the figure is equally available and would read *"…with each operation
in the supervised record below 3 seconds"*. The fresh measurement may make that number look arbitrary in either
direction, which is an argument for deciding it after the run rather than before.

## 11. Withdrawn and corrected claims

Recorded across all three revisions rather than quietly edited.

1. **VOID (revision 3) — the sibling A/B, which revision 2 presented as M5's strongest evidence.** The sweep was
   sound; the inference was not. `predicate_layout_smoke_integration_test.go:208` builds its measured store with no
   timeout override, so it carries the same 5s deadline, and `:479` (`require.NoError`) precedes `:480`
   (`require.Less` against a 10s budget) — a stalled call fails as a deadline error and never reaches the
   comparison. **Zero firings in 80 failures is what an unreachable assertion looks like.** The ruling stands on its
   architectural leg (§ 4), not on this.
2. **CORRECTED (revision 3) — the millisecond-misread hypothesis.** Revision 2 falsified it using #750's body, and
   #750's 2.237s *is* genuine seconds (verified from its run log). But the hypothesis was aimed at a second number,
   and there it is right: the sibling's *"healthy p95 already 2.65s"* (`predicate_layout_smoke...:90`-`:92`) is
   milliseconds read as seconds, and the value is the **owner-filter** harness's 5k CI PREDICATE p95
   (`docs/operations/32-...:120`, under `:133` *"All latency values above are milliseconds"*) — a single-key owner
   lookup, not a forward filter. Two unrelated numbers were treated as one. Filed as **#1286**.
3. **CORRECTED (revision 3) — the gh#220 framing.** Revision 2 said gh#220's >=3x rule "does not call for widening
   this harness". True, but it understated the point: the rule was *satisfied* at 7.7x and was never the problem,
   because a stall adds absolute seconds rather than multiplying them (P22). The smoke's own "1.13x" figure is void
   for the reason in item 2.
4. **WITHDRAWN (revision 2) — "one rule, two homes, no asymmetry"** for the corroboration design. Corroborating the
   load-phase gate could only happen after the writers and dispatcher join (`:381`, `:386`, `:392`-`:396`), i.e.
   under the quiescence the phase exists to exclude. Moot under the ruling, which deletes both gates.
5. **WITHDRAWN (revision 2) — "the full profile already encodes this separation at `:85`".** It does not: `:85`'s
   ceiling half is unreachable (P18). That defect is now Q7.
6. **CORRECTED (revision 2) — E's specification**, which recorded p50/p95/p99/max only and could not answer the
   question E existed to answer, because `:497` sorts first (P19). Submission order is now in the delta.
7. **CORRECTED (revision 2) — two pin citations.** The 5s timeout is not "applied unconditionally at `:70`" (`:69`
   guards it, `:538` applies it), and the partial-set refusal is enforced at `natsclient/kv.go:589`/`:592`, not in
   the doc comment.
8. **CORRECTED (revision 2) — the citation repair target.** `docs/operations/32-...:70`-`:71` describes the smoke
   harness, not this one.
9. **CORRECTED (revision 2) — a sweep method that produced a false zero.** A first pass using
   `gh run view ... 2>/dev/null` returned zero hits for runs known to contain them; the fetches were failing
   silently. Every sweep since captures stderr to a file and reports its denominator and failures.
10. **METHOD (revision 3) — a duration regex that silently dropped two rows.** Parsing the baseline logs with
    `p50=[0-9.]+m?s` returned **16 of 18** `phase=latency` lines and dropped both `name-owner` rows, whose p50 is
    `876.833µs`: `m?s` cannot match `µs`. The missing rows read as "that filter did not run" — an invented
    structural finding, from a parser. Go's duration formatting emits `ns`, `µs`, `ms` and `s`, so the unit must be
    matched as a set, and the parsed count checked against `grep -c` before anything is concluded. Both baselines in
    P25-P26 were re-parsed that way: 18 of 18 and 9 of 9.

## 12. Break classification and gates

- **Not BREAKING.** No exported surface, wire contract, config key, or payload. No package in
  `release/tier1-packages.txt` changes.
- **No e2e tier is owed.** The change is confined to `processor/graph-index/*_test.go` (integration tag) plus
  documentation and spec text.
- **The gate that matters is the supervised run**, not a green CI run: `tasks.md` § 3. A green CI run after this
  change proves the guard still passes; it is explicitly no longer activation evidence.

## 13. Decision skills

- `/kv-or-stream`, `/orchestration-check`, `/new-payload`, `/query-pattern`, `/entity-or-bucket` — **none
  triggered.** No new communication path, multi-step runtime behavior, message type, query access, or durable state.
