# Design — owner-load-gate-instrument (#1284)

**Status: architect draft. Not accepted. Section 9 is the owner docket.** The recommendation below narrows the
reading of an ADR-077 activation condition; the architect frames it and does not rule it.

Premises are pinned at `main@29187077` in `inventory.md` (66 pins, `task inventory:verify` exit 0). Every claim here
cites either a pin or a named CI run.

## 1. The decision

ADR-077 §8 condition 4 reads: *"the 5,000-hot-member plus 20-predicate CI guard, with each operation below 3
seconds"* (`docs/adr/077-...:139`).

Today's instrument reads **"each operation"** as *every wall-clock sample, taken once, on a runner shared with the
rest of the integration suite, decided by that single observation.* Under that reading the gate fires on 1.3% of all
CI runs and 10% of all CI failures, three orders of magnitude above any plausible regression rate for a layout that
measures 153-256ms at p50.

The question for the owner is not "what number" — the number does not move under any option here. It is **what a
measurement has to be before it decides a contracted gate.**

## 2. Premises, each with its measurement

| # | Premise | Measurement |
|---|---|---|
| P1 | The framework already enforces a 5s ceiling on the measured operation | `natsclient/kv.go:39` `Timeout: 5 * time.Second`, applied unconditionally at `:70`; harness passes no override (`:168`) |
| P2 | Production runs against the identical bound | `processor/graph-index/component.go:927`, `:953` — `NewKVStore(bucket)` with no option override |
| P3 | Every budget firing sits in the 3s-5s band where production succeeds | 3.356s, 3.515s, 4.760s (`inventory.md` § Observed firings, rows 1-3) |
| P4 | The framework's own ceiling is already asserted and has already fired | row 4, run 33208133273: `require.NoError` at `:487` failed with `context deadline exceeded` |
| P5 | The instrument discards its passing repetitions | `require.Less` at `:489` calls `t.FailNow()` before `:492` |
| P6 | A passing run records nothing | `t.Logf` is emitted only for failing tests without `-v`; `scripts/run-integration-tests.sh:311` passes no `-v`; run 34475316237 (`main`, green) prints only `ok ... 60.725s` |
| P7 | At `repetitions: 5` the percentiles never examine the max | `(5-1)*95/100 == (5-1)*99/100 == 3`; pinned by `TestOwnerLoadPercentiles_DoNotCoverTheMax` |
| P8 | Only 5,000-key forward filters ever fire; 1-key owner filters never do | all 4 firings are `*-forward`; owner filters measure 2-6ms |
| P9 | The measurement shares a runner with uncapped parallel Docker packages | `scripts/run-integration-tests.sh:304` "uncapped package parallelism", `:311` `-race`, one `ubuntu-latest` (`ci.yml:131`) |
| P10 | There are two per-repetition budget gates, not one | `:489` (sequential, forward + owner) and `:376` (concurrent phase, owner only) |
| P11 | Raising `repetitions` breaks an existing pin test | `owner_filter_budget_contract_test.go:55` — `require.Len(t, durations, ci.repetitions` over a hardcoded five-sample fixture |
| P12 | Cost of an added repetition | 153ms + 256ms + [`incoming-forward` unmeasured, bracketed 150-390ms] + ~11ms = ~0.6-0.8s; 5 -> 21 costs ~10-13s |

**P1-P4 together are the core finding.** The gate asks the test to *predict* a 3-second ceiling that neither the
framework nor production enforces, inside a band where both succeed. The real ceiling is *observed*: the framework
applies it, and when it is reached the call returns `context deadline exceeded` and the harness fails at `:487`
without any predicted number being involved. The CI profile is the only place in this harness where an absolute
ceiling and a latency contract are collapsed onto one field — the full profile already separates them
(`:85`: 10s ceiling, p95 3s, p99 5s).

## 3. The problem shape, and the closest existing instance

**Shape:** a threshold decision over a measurement that a transient can corrupt — "admit-or-refuse where one
observation is unreliable".

**Closest existing instances, on two planes:**

- `natsclient/kv.go:37`, `:41` — `MaxRetries: 10` with `UseExponentialBackoff: true`. The framework's own answer to a
  transient making one observation unreliable is **bounded re-observation with a hard cap**, never a wider threshold.
- `processor/graph-index/owner_filter_load_integration_test.go:408` — this same harness already decides its
  consumer-return-to-baseline assertion by `require.Eventually` re-observation before the equality check at `:414`.

The budget gate is the only assertion in the file that decides on a single sample. **An instance of the shape exists,
so no adoption sweep is owed** (the establishing side of category 5 does not trigger).

## 4. Options

Mapped to #1284's numbering where they correspond. All of them presuppose **E**, which is not an option.

### E — Record before deciding (prerequisite, not owner-gated)

`measureOwnerLoadFilter` collects every repetition, logs the distribution, and only then evaluates the budget. The
distribution is also emitted on passing runs.

- Fixes P5 and P6. Makes "stall vs. real tail" answerable for the first time, and makes condition 4's evidence exist
  on the runs that authorize activation.
- Changes no budget, deletes no assertion, moves no pinned constant.
- Cost: none measurable. `-v` output on the graph-index integration package, ~12 additional lines per run.
- **Alone it does not stop the flake.** It is a prerequisite of every option below, and the only reason to sequence
  anything after it is that it is what would attribute the stall.

### M1 — Do nothing (the status quo option)

- Keeps condition 4's text and its per-observation reading intact.
- Cost, measured: 1.3% of all CI runs red, 10% of all CI failures, on a required job with no open issue behind it
  since #750 closed. Every PR that hits it rediscovers the history from a closed issue, and the standing merge-gate
  temptation is rerun-to-green, which the repo prohibits.
- Cost to the evidence: condition 4's numbers are recorded on **no** passing run at all (P6). Doing nothing keeps an
  unrecorded activation gate.

### M2 — Corroborated breach (#1284 option 2, without needing a stall signal) — RECOMMENDED

A repetition at or above `operationBudget` does not decide. It is recorded, and it triggers exactly **one**
corroborating measurement set of the same filter, taken after the breaching set completes. The gate fails when the
corroborating set also breaches.

- `operationBudget` stays `3 * time.Second`; the per-repetition assertion stays; `repetitions` stays 5; every pinned
  constant in `owner_filter_budget_contract_test.go` stays.
- #1284 frames option 2 as needing "a stall signal that is not itself wall-clock". It does not: **reproduction is the
  discriminator.** A layout regression reproduces in every sample of both sets; a stall must span both sets to forge
  a failure.
- Cost on the common path: zero. Cost on a breach: one extra measurement set, ~0.6-0.8s for a forward filter (P12).
- Applies to both gates (P10) with one rule: the concurrent phase collects breaching labels during the drain and
  corroborates after the dispatcher joins, so there is no asymmetry between `:489` and `:376` and no re-measurement
  inside the dispatcher that would perturb the concurrency the phase exists to test.
- Bounded by construction: exactly one corroborating set, never a third. A gate that retries until green is
  rerun-to-green.

### M3 — Statistical coverage (#1284 option 3)

Raise `repetitions` (e.g. 5 -> 21) so p95/p99 gain real resolution, then either discard a bounded number of outliers
from the per-operation rule or move the 3s figure onto the percentiles with the absolute ceiling left to the
framework's 5s deadline.

- The strongest *detector*: a 21-sample distribution finds a layout regression far earlier than five samples do, and
  its stall-immunity is arithmetic rather than empirical.
- Costs ~10-13s of CI on **every** run (P12), on a harness that already runs 35.06s inside a 60.7s package inside a
  job capped at `timeout-minutes: 25`.
- Requires amending condition 4's *text*, not only its reading: "each operation" becomes "p95/p99 over N
  repetitions" or "each operation except k discarded outliers".
- Breaks `TestOwnerLoadPercentiles_DoNotCoverTheMax` at `:55` (P11) and retires its premise at `:66` ("the per-rep
  gate MUST remain"). That test is the anti-relaxation guard; rewriting it is exactly the move #750 / PR #755 made
  and had reverted.
- Note what P7 already gives for free: at `repetitions: 5`, `durations[3]` is the second-largest of five, so the
  existing percentile gates are **already immune to exactly one stalled repetition**. The per-repetition gate is the
  only one that is not. M3 buys resolution, not stall-immunity that the file lacks.

### M4 — Isolate the instrument (#1284 option 1)

Run the harness in its own CI job, on its own runner VM, so it does not compete with the rest of the integration
suite (P9).

- Preserves condition 4's text **and** its per-observation reading. Costs the evidence nothing.
- Cheap in engineering, not free in CI: another Docker image pull, another testcontainer bring-up, another required
  job.
- **Efficacy unmeasured.** It removes in-run contention; it cannot remove hypervisor steal on a shared-tenancy
  runner, and the split between those two causes is undetermined until E lands. Recommending it now would be
  predicting a cause we have not observed — the exact failure this repo's seam rule names.
- Composes with M2 rather than competing: M4 lowers the breach rate, M2 decides breaches honestly.

### Prohibited-A — Raise `operationBudget`

Excluded by #1284 and by the task brief. It conflates the CI guard with the Decision profile's 10s ceiling; PR #755
already did this once on the reasoning that it "matched the full profile", and it matched the wrong profile's
contract (`owner_filter_budget_contract_test.go:22-25`).

### Prohibited-B — Delete the per-repetition assertion

Excluded by #1284 and by the task brief. At `repetitions: 5` it is the only tail coverage the CI profile has (P7),
which `TestOwnerLoadPercentiles_DoNotCoverTheMax` exists to prove.

## 5. What each option costs ADR-077 condition 4's activation evidence

| Option | Condition 4 text | Its reading | Evidence lost | Evidence gained |
|---|---|---|---|---|
| E | unchanged | unchanged | none | the distribution, on every run, pass or fail — today recorded on none (P6) |
| M1 | unchanged | unchanged | none textually; in practice the gate records nothing on any green run and trains readers to re-run | none |
| **M2** | **unchanged, verbatim** | narrows: a corroborated observation decides, not any single one | a genuine one-in-five 3s-5s tail that does not reproduce within one corroborating set now passes — **but it is recorded** | breaches are counted instead of discarded; the framework's 5s ceiling is named as the absolute bound it already is |
| M3 | **must be amended** — "each operation" no longer holds literally | replaced by a percentile contract | the literal per-operation guarantee; `TestOwnerLoadPercentiles_DoNotCoverTheMax`'s premise | genuine p95/p99 resolution; arithmetic stall-immunity |
| M4 | unchanged | unchanged | none | none (a lower breach rate is not evidence) |
| Prohibited-A | silently retired | — | the whole of condition 4's tightening over ADR-065's 10s bound | none |
| Prohibited-B | silently retired | — | all CI-profile tail coverage | none |

## 6. Recommendation

**Land E now, and rule M2 as the reading of condition 4; hold M3 and M4 as data-gated follow-ons rather than
speculative fixes.** M2 is the only option that keeps condition 4's text verbatim, moves no pinned constant, and
answers the unresolved stall-vs-tail question every time it fires instead of discarding the evidence — and unlike M3
it pays its cost only on the rare breach, and unlike M4 it does not predict a cause nobody has yet observed.

Consequently condition 4's evidence gets **stronger**, not weaker: the distribution is recorded on every run instead
of none (E), the framework-enforced 5s ceiling is named as the absolute bound it already is and already asserts (P1,
P4), and the 3-second figure is decided by reproduction instead of by one sample from a shared runner.

## 7. The strongest case against the recommendation

Four arguments, in descending force:

1. **M2's stall-immunity is empirical; M3's is arithmetic.** The observed budget firings imply stall windows of
   ~3.2s, ~3.3s and ~4.6s inside calls that normally take ~0.2s. A corroborating set of five forward measurements
   takes ~1s. If a stall window is long enough to span the breaching sample *and* the corroborating set, M2 fires
   anyway. Nobody has measured stall-window duration — that is precisely what E would produce — so **M2 is being
   recommended on a mechanism whose efficacy the same design admits is unmeasured.** M3 needs no such assumption:
   `durations[3]` of 21 samples cannot be moved by one stall, by arithmetic.
2. **M2 can pass over a real intermittent tail.** If the 3s-5s band contains a genuine tail of the 5,000-key drain
   (`inventory.md`, second UNRESOLVED), corroboration will sometimes miss it and the gate will report green over a
   real breach. That is a weakening of condition 4 that its text does not disclose, whereas M3's weakening is
   written down. The mitigation — every breach is logged with a greppable key even when the corroborating set
   passes, so the rate is measurable from CI history — is a mitigation, not an answer.
3. **M2 adds a mechanism; M3 changes a constant.** "A design pass never ratchets complexity up" and "materialize
   salvage at or below the record" both point at M3: the record is the full profile at `:85`, which solves this with
   three existing struct fields and no new logic. M2 introduces a breach-and-corroborate loop in two places. The
   honest scale is ~20 test-tier lines against a one-token change plus ~11s of CI, and a reasonable owner can
   prefer paying the CI seconds to avoid the logic.
4. **The cheapest text-preserving option is being deferred, not taken.** M4 costs the evidence exactly nothing and
   removes a contention source that is measured and self-inflicted (P9). Deferring it until E attributes the cause
   is defensible discipline, and it is also a decision to keep shipping a known flake for at least one more cycle.

If the owner weighs (1) and (2) above (3), the correct ruling is **M3, or E-then-decide** — and E-then-decide is
cheap: E lands under no ruling at all, and the stall-window data it produces settles (1) within a few CI runs.

## 8. Invariants, each with its spec home

Stated for the corroboration rule as the only admissible source for a later property harness. Spec homes are the
scenarios in `specs/graph-index/spec.md` of this change.

- **I1 — Bounded decision.** A gate outcome never depends on more than two measurement sets per filter. Home:
  *"a corroborating set is taken exactly once"*.
- **I2 — Deterministic on a real regression.** A filter whose latency genuinely exceeds the budget fails, because
  every sample of both sets breaches. Home: *"a sustained breach fails both measurement sets"*.
- **I3 — Evidence is unconditional.** The measured distribution is recorded for every filter on every run, whether
  or not the gate fires, and a breach is recorded even when the corroborating set passes. Home: *"the measured
  distribution is recorded on a passing run"*.
- **I4 — The absolute ceiling is observed, never predicted.** An operation that reaches the framework-enforced
  deadline fails as a typed error, not as a budget comparison. Home: *"an operation that reaches the framework bound
  fails as an error"*. This invariant is already true in code (`:487`, P4); the spec delta names it.

## 9. Owner docket

Every item below is held in `tasks.md` § 2 and blocks implementation of the corresponding task.

- **Q1 (the ruling this change exists for).** Does "each operation below 3 seconds" mean *every observation* (M1/M4),
  *every corroborated observation* (M2), or *a percentile over more repetitions* (M3)? The architect recommends M2
  and states the case for M3 in § 7.
- **Q2.** Does E land independently of Q1? It is owner-gated by nothing in the architect's reading — it changes no
  budget and deletes no assertion — but it does change what a failing run prints, and the reviewer may read that as
  touching the contract.
- **Q3.** ADR form. `docs/adr/046-...:14` is the precedent: a bolded in-place amendment note in the amended ADR
  pointing at the deciding record. Is an in-place note on ADR-077 §8 condition 4 sufficient (§ 10 drafts it), or does
  this need its own ADR? Note ADR-107 is already taken by the unmerged `claude/gh1267-honor-predicate-datatype`
  branch (`bc7d79cc`), so a new one would be 108 and would race that branch's number.
- **Q4.** Should M4 (own CI job) be filed as a follow-on issue now, or held until E's data attributes the stalls?
  Filing it now costs one issue; holding it risks the attribution never being done.
- **Q5.** The gh#750 comment at `:73` records a "same-run max of 2.24s" that no forward filter in the measured window
  comes near (worst 389ms). It stays in the file under this change. Should the comment be corrected to the
  current-pin measurements, or preserved as a historical note under `docs/operations/32-...:44`'s pre-pin warning?
- **Q6.** Citation defect A is repaired in the spec delta (ADR-065 -> ADR-077 §8 condition 4). ADR-065 itself says
  nothing wrong and needs no edit — confirm no ADR-065 change is wanted.

## 10. ADR-077 amendment — DRAFT, OWNER-GATED, NOT APPLIED

Not written to `docs/adr/`. It is drafted here so the owner can rule on the exact words. It follows the
`docs/adr/046-...:14` precedent: a bolded amendment note inside the amended ADR, with the mechanics living in the
capability spec. It rehomes condition 4's surviving premises (5,000 hot members, 20 predicates, 3 seconds, and the
prohibition on activating under a failed absolute budget) rather than restating them loosely.

Insert immediately after `docs/adr/077-...:139` (condition 4), under M2:

> **Amendment (#1284, owner ruling <date>):** condition 4's workload, its 3-second figure, and its prohibition on
> activating under a failed absolute budget all stand unchanged. What changes is what counts as a measurement.
> "Each operation below 3 seconds" is a latency contract evaluated on a corroborated measurement: a repetition at or
> above 3 seconds is recorded and re-measured once as a complete set, and the guard fails when the corroborating set
> also breaches. The absolute ceiling for this guard is the framework-enforced `natsclient` KV deadline, which the
> harness already observes as a typed error rather than predicting as a budget. The measurement rules live in the
> `graph-index` capability spec; this record states the guarantee, not the instrument.

If the owner rules M3 instead, the note's second sentence becomes: *"'Each operation below 3 seconds' is evaluated
as p95 at most 3 seconds and p99 at most 3 seconds over at least N repetitions, with no operation reaching the
framework-enforced deadline"* — and `docs/operations/32-...:70`'s CI row and
`TestOwnerLoadPercentiles_DoNotCoverTheMax` both need rewriting, which M2 does not require.

## 11. Break classification and gates

- **Not BREAKING.** No exported surface, no wire contract, no config key, no payload. Tier 1 API compatibility is
  unaffected: no package in `release/tier1-packages.txt` changes.
- **No e2e tier is owed.** The change is confined to `processor/graph-index/*_test.go` (integration tag) plus
  documentation and spec text. The gate that must be green is the one being changed:
  `go test -race -tags=integration -run '^TestIntegration_OwnerFilterLoadHarness$' ./processor/graph-index`, plus
  `openspec validate --strict` and `task inventory:verify`.
- **Repeat-run evidence is owed at review time**, not a single green: the flake rate being fixed is 1.3%, so one
  green run proves nothing. `tasks.md` § 4 requires a `-count` repetition run and the recorded distributions.

## 12. Decision skills

- `/kv-or-stream` — **not triggered.** No new communication path; the change adds no subject, bucket, or watcher.
- `/orchestration-check` — **not triggered.** No multi-step runtime behavior; the corroboration loop is inside one
  test helper, not a rule, component, or lifecycle participant.
- `/new-payload` — **not triggered.** No new message type.
- `/query-pattern` — **not triggered.** No new query access; `KeysByFilter` is called exactly as it is today.
- `/entity-or-bucket` — **not triggered.** No new durable state.
