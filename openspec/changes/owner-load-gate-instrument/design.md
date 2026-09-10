# Design — owner-load-gate-instrument (#1284)

**Status: architect draft, revision 2. Not accepted. Section 9 is the owner docket.**

Revision 2 answers a review that returned CHANGES REQUESTED with 2 blocking findings, and folds in a new measurement
that partly inverts the reviewer's own headline recommendation. **Two claims in revision 1 were wrong and are
withdrawn in § 11.** Premises are pinned at `main@29187077` in `inventory.md` (81 pins, `task inventory:verify`
exit 0).

## 1. The decision

ADR-077 §8 condition 4 reads: *"the 5,000-hot-member plus 20-predicate CI guard, with each operation below 3
seconds"* (`docs/adr/077-...:139`).

Today's instrument reads **"each operation"** as *every wall-clock sample, taken once, on a runner shared with the
rest of the integration suite, decided by that single observation.* Five firings are now measured, one on `main`.

The question for the owner is **what a measurement has to be before it decides a contracted gate**, and — new in
revision 2 — **whether a shared-runner CI profile is the right home for this gate at all.**

## 2. Premises, each with its measurement

| # | Premise | Measurement |
|---|---|---|
| P1 | The framework enforces a 5s ceiling on the measured operation | `natsclient/kv.go:39` `Timeout: 5 * time.Second`, guarded at `:69` on `Timeout > 0`, applied for this operation by `applyTimeout` at `:538`; harness passes no override (`:168`) |
| P2 | Production runs against the identical bound | `processor/graph-index/component.go:927`, `:953` — `NewKVStore(bucket)` with no option override |
| P3 | Every budget firing sits in the 3s-5s band where production succeeds | 3.356s, 3.515s, 4.760s, 4.805s (`inventory.md` § Observed firings) |
| P4 | The framework's ceiling is already asserted and has already fired | run 33208133273: `require.NoError` at `:487` failed with `context deadline exceeded`; the partial-set refusal is enforced at `natsclient/kv.go:589`, `:592` |
| P5 | The instrument discards its passing repetitions | `require.Less` at `:489` calls `t.FailNow()` before `:492` |
| P6 | A passing run records nothing | `t.Logf` reaches CI only for failing tests; `scripts/run-integration-tests.sh:311` passes no `-v`; run 34475316237 (`main`, green) prints only `ok ... 60.725s` |
| P7 | At `repetitions: 5` the percentiles never examine the max | `(5-1)*95/100 == (5-1)*99/100 == 3`; pinned by `TestOwnerLoadPercentiles_DoNotCoverTheMax` |
| P8 | Only 5,000-key forward filters ever fire | all 5 firings are `*-forward`; owner filters measure 2-6ms |
| P9 | The measurement shares a runner with uncapped parallel Docker packages | `scripts/run-integration-tests.sh:304`, `:311`; one `ubuntu-latest` (`ci.yml:131`) |
| P10 | There are two per-repetition budget gates | `:489` (measurement phase, forward + owner) and `:376` (concurrent load phase, owner only) |
| P11 | Raising `repetitions` breaks an existing pin test | `owner_filter_budget_contract_test.go:55` over a hardcoded five-sample fixture |
| P12 | Cost of an added repetition, all filters | ~0.6-0.8s; 5 -> 21 costs ~10-13s |
| **P13** | **The distribution the 3s budget was argued against no longer exists** | gh#750 body: `p50=99.784608ms p95=697.726516ms max=2.23697341s`. Three current runs: p95 ~157-175ms, max 166-389ms (`inventory.md` § Adjacent claims) |
| **P14** | **The budget carries ~7.7x headroom; gh#220 is already satisfied** | 3s / 389ms worst observed forward max. gh#220's discipline is >=3x (`predicate_layout_smoke_integration_test.go:89`-`:90`) |
| **P15** | **The firings are excursions far outside the distribution, not its tail** | 21.9x and 30.5x the same run's own `predicate-forward` p50; in gh#750's era 2.24s was *inside* a 22.4x-spread distribution |
| **P16** | **The same-class instance is in the same package and resolved this in 2026-07-18** | `predicate_layout_smoke_integration_test.go:89`-`:94` (gh#220 rationale), `:96`-`:98` (10s/8s/9s); also `service/service_manager_health_listener_test.go:114`, `:253` |
| **P17** | **That sibling has never fired.** 80 CI failures fetched across 2026-05-30..2026-09-10 (42 date-bounded since 2026-08-26, 0 fetch errors; 38 of an older 40-slice, 2 HTTP 410): `PredicateLayoutSmoke` appears in **zero**, `OwnerFilterLoadHarness` in **five** | `inventory.md` § Searches, § Adjacent claims |
| **P18** | **The full profile's absolute ceiling is dead code** | `:85` sets `operationBudget: 10 * time.Second`, but every measured call is bounded at 5s (P1) and an expiry fails at `:487` before `:489`. A 10s per-operation budget on this operation can never fire |
| **P19** | **`assertOwnerLoadLatency` destroys submission order before recording** | `:497` sorts the slice before any percentile or log line |
| **P20** | **The alternative evidence home is currently historical** | `docs/operations/32-...:80`+ owner-filter acceptance record was measured at revision `0a7af288` on `nats:2.12.4-alpine`; `:43` declares every pre-pin-move performance row historical |

**P1-P4 remain the core finding.** The gate asks the test to *predict* a 3-second ceiling that neither the framework
nor production enforces, inside a band where both succeed. The real ceiling is *observed*: the framework applies it,
and when it is reached the call returns `context deadline exceeded` and the harness fails at `:487` with no predicted
number involved.

**P13-P15 are new and change the weighting.** gh#750's diagnosis — "a per-rep gate asserting on the tail of a
distribution whose max is 22x its median" — was right for 2026-07-30 and does not describe today. The tail it was
arguing about is gone.

## 3. The problem shape, and the nearest instances

**Shape:** a threshold decision over a measurement that a transient can corrupt.

- **Nearest same-class instance — same package, same struct shape, already resolved:**
  `processor/graph-index/predicate_layout_smoke_integration_test.go`. It hit this identical flake on 2026-07-18 and
  resolved it by widening its CI profile to 10s/8s/9s and demoting it to an order-of-magnitude regression guard,
  citing a standing repository discipline, **gh#220**, that the owner-filter harness never cites (P16). It has never
  fired since (P17). This instance produces option **M5** below; revision 1 missed it entirely.
- **The framework's own answer to an unreliable observation:** `natsclient/kv.go:37`, `:41` — bounded re-observation
  with a hard cap, never a wider threshold.
- **In this very file:** `:408` — `require.Eventually` already decides the consumer-return-to-baseline assertion by
  re-observation. The budget gate is the only assertion in the file that decides on one sample.

An instance of the shape exists on two planes, so **no adoption sweep is owed**.

## 4. Options

All presuppose **E**, which is not an option.

### E — Record before deciding, in submission order (prerequisite, not owner-gated in code)

`measureOwnerLoadFilter` collects every repetition, logs **the per-repetition durations in submission order**, and
only then evaluates. The distribution is emitted on passing runs too.

- Revision 1 specified only p50/p95/p99/max. That was insufficient: `assertOwnerLoadLatency` sorts at `:497` (P19),
  and the discriminator between a single stall and a sustained one is whether an inflated sample is **isolated or
  adjacent** to another. Only submission order carries that. This correction is what makes "E then decide"
  decidable.
- Fixes P5, P6 and P19. Cost: none measurable.
- **Constraint on the delivery route:** `ci.yml:144` invokes `scripts/run-integration-tests.sh` with no arguments, so
  `packages=(./...)` at `:307`-`:309` and `:311` is a single `go test`. Adding `-v` makes the whole suite verbose;
  the alternatives are writing the distribution to the test binary's stdout, or invoking the script a second time
  with the package as an argument (it accepts `"$@"`, and releases its host lock on exit). Picking one is task 3.2.

### M1 — Do nothing (the status quo option)

- Keeps condition 4's text and its per-observation reading intact.
- Cost, measured: 4 of 42 CI failures since 2026-08-26 and a fifth on 2026-08-25, on a required job, with the
  standing temptation being rerun-to-green, which the repo prohibits.
- Cost to the evidence: on every green run, condition 4's numbers are recorded nowhere (P6).

### M2 — Corroborated breach in the measurement phase (#1284 option 2) — RECOMMENDED

A repetition at or above `operationBudget` **in the per-filter measurement phase** does not decide. It is recorded,
and it triggers exactly **one** corroborating measurement set of the same filter, taken after the breaching set
completes. The guard fails when the corroborating set also breaches.

- **The concurrent load phase's gate at `:376` is explicitly excluded**, and the delta says so. Revision 1 claimed
  one rule covered both homes with no asymmetry; that was wrong (§ 11, withdrawal 1). Corroborating there could only
  happen after `churnWG.Wait()` (`:381`), `samplerWG.Wait()` (`:386`), `cancelDispatch()` (`:392`) and the join
  (`:392`-`:396`) — i.e. under exactly the quiescence the phase exists to exclude, which would make `:376`
  effectively never fire. The exclusion is justified on margin, not convenience: `:376` measures single-key owner
  filters at 2-6ms against 3s, a margin of three orders of magnitude, and has never fired in 80 fetched failures.
- `operationBudget` stays `3 * time.Second`; `p95Budget`/`p99Budget` stay 3s; `repetitions` stays 5. No constant
  pinned by `owner_filter_budget_contract_test.go` moves; neither contract test needs an edit.
- #1284 frames option 2 as needing "a stall signal that is not itself wall-clock". It does not: **reproduction is the
  discriminator.**
- Cost on the common path: zero. Cost on a breach: one set of 5 repetitions of **one** filter — **~0.77s**
  (`predicate-forward` p50 153ms x 5) to **~1.28s** (`name-forward` p50 256ms x 5). Revision 1 mispriced this as
  "~0.6-0.8s" by citing P12, which is the cost of one added repetition across *all* filters (§ 11, correction).
  The wider real window is better for discrimination.
- Bounded by construction: exactly one corroborating set, never a third.

### M3 — Statistical coverage (#1284 option 3)

Raise `repetitions` so p95/p99 gain resolution, then discard a bounded number of outliers or move the 3s figure onto
the percentiles.

- Stall-immunity is arithmetic rather than empirical: `durations[3]` of 21 samples cannot be moved by one stall.
- Costs ~10-13s of CI on **every** run (P12), and breaks `TestOwnerLoadPercentiles_DoNotCoverTheMax` at `:55` (P11),
  retiring its premise at `:66`.
- Requires amending condition 4's *text*, not only its reading.
- **Revision 1's "low-complexity salvage" framing of this option is withdrawn** (§ 11, withdrawal 2). "Copy the full
  profile's three fields onto CI" would install an **unreachable** per-operation budget (P18) and leave only
  percentiles doing work — functionally Prohibited-B, reached by a different route.
- P13-P15 weaken it further: with maxima at 166-389ms and 7.7x headroom, more repetitions buy resolution on a
  distribution that is already tight and far inside budget.
- Note P7 already gives partial stall-immunity for free: at `repetitions: 5`, `durations[3]` is the second-largest of
  five, so the existing percentile gates already survive exactly one stalled repetition. Only the per-repetition gate
  does not.

### M4 — Isolate the instrument (#1284 option 1, environment arm)

Run the harness in its own CI job, on its own runner VM, so it does not compete with the rest of the integration
suite (P9).

- Preserves condition 4's text and its reading. Costs the evidence nothing.
- Another Docker image pull, another testcontainer bring-up, another required job.
- Removes in-run contention; cannot remove hypervisor steal, and the split is undetermined until E lands.
- Composes with M2 rather than competing.

### M5 — Demote the CI profile to a regression guard (#1284 option 1, contract arm) — NEW in revision 2

Adopt the sibling's 2026-07-18 resolution (P16): the CI profile becomes an order-of-magnitude regression guard with a
wide budget, and ADR-077 condition 4's activation evidence comes instead from a recorded supervised run
(`GRAPH_INDEX_OWNER_FILTER_FULL=1`) — which is what condition 5 already is for the 21k shape. The doctrine half is
already written at `docs/operations/32-...:77`: *"The CI profile is a regression guard, not a source for comparative
layout selection."*

- **It has the strongest empirical support of any option here.** The same package's sibling made exactly this move
  and has fired zero times in 80 fetched CI failures across 3.5 months, while this harness fired five (P17).
- **But its stated precedent does not transfer on the numbers.** The smoke widened because it sat at ~1.13x headroom;
  this harness sits at ~7.7x, so gh#220's >=3x discipline is **already satisfied** and does not call for widening
  here (P14). The case for M5 is therefore *architectural* — a shared-runner CI profile is the wrong home for a tight
  activation budget — not a margin case, and it must be ruled on that basis.
- **It entails raising `operationBudget`**, which #1284 prohibits and the architect's brief binds it not to
  recommend. The prohibition's stated rationale is that doing so *"silently retires ADR-077 condition 4's activation
  evidence"*; M5 retires it explicitly and rehomes it, which is a different act. That distinction is the owner's to
  draw, not the architect's, so M5 is presented in full and not recommended.
- **Measured cost the option must carry:** the evidence home it moves to is currently historical. The owner-filter
  acceptance record at `docs/operations/32-...:80`+ was measured at revision `0a7af288` on `nats:2.12.4-alpine`, and
  `:43` declares every pre-pin-move performance row historical (P20). M5 therefore owes a supervised re-run on the
  current pin before condition 4 has any evidence at all, and it moves the gate from something that runs on every PR
  to something that runs when a person remembers.

### Prohibited-A — Raise `operationBudget` with no replacement evidence

Excluded by #1284 and the brief. PR #755 did this once on the reasoning that it "matched the full profile", and it
matched the wrong profile's contract (`owner_filter_budget_contract_test.go:22`-`:25`). M5 is the non-silent form and
is docketed separately.

### Prohibited-B — Delete the per-repetition assertion

Excluded by #1284 and the brief. At `repetitions: 5` it is the only tail coverage the CI profile has (P7). Note this
was gh#750's own option 2 and was rejected then.

## 5. What each option costs ADR-077 condition 4's activation evidence

| Option | Condition 4 text | Its reading | Evidence lost | Evidence gained |
|---|---|---|---|---|
| E | unchanged | unchanged | none | the per-repetition distribution in submission order, on every run — today recorded on none |
| M1 | unchanged | unchanged | none textually; nothing is recorded on any green run | none |
| **M2** | **unchanged, verbatim** | narrows: a corroborated observation decides in the measurement phase; the load phase is unchanged | a genuine tail that fails to reproduce within one corroborating set — but it is recorded, and P13-P15 show no such tail on the current pin | breaches counted instead of discarded; one absolute ceiling named as the framework bound it already is |
| M3 | **must be amended** — "each operation" no longer holds literally | replaced by a percentile contract | the literal per-operation guarantee; `TestOwnerLoadPercentiles_DoNotCoverTheMax`'s premise | p95/p99 resolution; arithmetic stall-immunity |
| M4 | unchanged | unchanged | none | none (a lower breach rate is not evidence) |
| **M5** | **condition 4 is rehomed** — the CI guard stops being activation evidence | replaced by a supervised recorded run | per-PR enforcement; and until a supervised re-run lands, condition 4 has NO current-pin evidence (P20) | a CI guard that catches order-of-magnitude regressions without forging failures; alignment with the sibling and with `docs/operations/32-...:77` |
| Prohibited-A | silently retired | — | condition 4's whole tightening over ADR-065's 10s bound | none |
| Prohibited-B | silently retired | — | all CI-profile tail coverage | none |

## 6. Recommendation

**The recommendation stands at E + M2, and E is now the larger half of it: land the submission-order evidence repair,
and rule M2 — corroboration in the measurement phase only, with the load-phase gate at `:376` explicitly excluded —
as the reading of condition 4.** It is the only option that keeps condition 4's text verbatim, moves no pinned
constant, and answers the stall-versus-tail question every time it fires instead of discarding the evidence.

**Its margin over the alternatives narrowed, and its justification changed.** P13-P15 strengthen it — a 21.9-30.5x
excursion above a distribution whose max is 389ms is far more clearly a stall than gh#750's case was, and
corroboration reproduces a real regression trivially at that separation. P18 removes M3's "salvage at the record"
argument entirely. But P16-P17 introduce M5, which has better empirical evidence than anything the architect can
recommend, and § 7 now leads with it rather than with M3.

## 7. The strongest case against the recommendation

1. **M5 has the better evidence, and the architect cannot recommend it.** The same package's sibling made exactly
   this move and has not fired once in 80 fetched CI failures over 3.5 months; this harness has fired five times in
   the same corpus (P17). That is a direct A/B on the same runners, the same package and the same workload class, and
   it beats any argument from mechanism. M2 is recommended over it because the brief prohibits raising the budget —
   which is a constraint on the architect, not evidence about the world. **If the owner is willing to rule the
   non-silent rehoming, M5 is the better-evidenced choice and the recommendation should be read as conditional on
   the prohibition holding.**
2. **M2's stall-immunity is empirical; M3's is arithmetic.** The firings imply stalls of ~3.2-4.6s inside calls that
   normally take ~0.2s. A corroborating set takes 0.77-1.28s and begins after the breaching set's remaining
   repetitions (~0.3-0.5s). If a stall window spans all of that, M2 fires anyway. P17 bounds this indirectly — no
   observed stall pushed a same-package, same-runner operation past 10s — but that bounds the *excursion*, not the
   window, and nobody has measured the window. That is what E now produces, and it is why "E then decide" remains a
   defensible ruling.
3. **M2 can still pass over a real intermittent tail**, and its text would not disclose that. P13-P15 make this much
   less likely than revision 1 assumed — no current-pin forward distribution reaches the 3s-5s band at all — but
   "much less likely" is not "cannot".
4. **M2 adds a mechanism where M1/M4/M5 change none.** ~20 test-tier lines against a one-line CI job change (M4) or a
   constant plus a rehoming (M5). "A design pass never ratchets complexity up" points away from M2; revision 1
   pointed that argument at M3's "copy the full profile", and P18 killed that shape, so the argument now lands on M4
   and M5 instead.

## 8. Invariants, each with its spec home

- **I1 — Bounded decision.** A gate outcome never depends on more than two measurement sets per filter. Home:
  *"a corroborating set is taken exactly once"*.
- **I2 — Deterministic on a real regression, within the measurement phase.** A filter whose latency genuinely
  exceeds the budget fails, because every sample of both sets breaches. Home: *"a sustained breach fails both
  measurement sets"*. **Scoped in revision 2:** it asserts nothing about the load phase, whose gate takes one sample
  under load and is excluded by its own scenario.
- **I3 — Evidence is unconditional and ordered.** Every filter's per-repetition durations are recorded in submission
  order on every run, and a breach is recorded even when the corroborating set passes. Home: *"the measured
  distribution is recorded on a passing run"*.
- **I4 — One absolute ceiling, observed rather than predicted.** An operation that reaches the framework-enforced
  deadline fails as a typed error; no second per-operation budget restates it. Home: *"an operation that reaches the
  framework bound fails as an error"*. Already true in code (`:487`, `natsclient/kv.go:589`, `:592`).

## 9. Owner docket

Held in `tasks.md` § 2.

- **Q1 (the ruling this change exists for), now four arms.** Does "each operation below 3 seconds" mean every
  observation (M1/M4), every corroborated observation in the measurement phase (M2), a percentile over more
  repetitions (M3), or **is the CI guard no longer condition 4's evidence at all (M5)**? The architect recommends M2
  and states in § 7 item 1 that M5 has the better evidence and is barred to it, not disproven.
- **Q2.** Does E land independently of Q1? Its **code** is ungated. Its **spec text is not separable** from M2's:
  both live in one MODIFIED requirement synced as a unit by task 6.1, and E's normative sentence *"A run that records
  no distribution is not activation evidence"* retroactively voids every CI run to date as condition-4 evidence
  (`docs/operations/32-...:75` already says as much for the runbook). That, not the print format, is what needs
  ruling.
- **Q3.** ADR form: in-place amendment note on ADR-077 §8 condition 4 (`docs/adr/046-...:14` is the precedent), or a
  new ADR? ADR-107 is taken by the unmerged `claude/gh1267-honor-predicate-datatype` branch (`bc7d79cc`), so a new
  record would be 108 and would race it.
- **Q4.** File M4 (own CI job) as a follow-on now, or hold until E's recorded data attributes the stalls?
- **Q5 — RESOLVED by measurement, no longer a question.** gh#750's "same-run max of 2.24s" was a real measurement
  (`p50=99.784608ms p95=697.726516ms max=2.23697341s`, quoted in #750's body, which itself calls it "a long tail an
  order of magnitude past the median" — so the millisecond-misread hypothesis is falsified by the issue's own words).
  The distribution changed underneath it: p95 ~698ms -> ~170ms, max 2.237s -> 166-389ms. The comment at `:73` is
  **stale evidence, not a wrong measurement**, and `docs/operations/32-...:43` is the mechanism that predicted this.
  Task 5.4 records that rather than asking.
- **Q6.** Confirm ADR-065 itself needs no edit — the citation defect is in the spec text, not the ADR.
- **Q7 (new, from blocking finding 2).** The full profile's `operationBudget: 10 * time.Second` at `:85` is
  unreachable (P18) and ADR-077 condition 5 carries the same phrase. The delta stops carrying the 10s figure as a
  per-operation budget on a directly measured key listing. Confirm that, and rule whether condition 5's phrasing and
  the dead `:85` field are in scope here or a separate filing. **The architect did not touch either.**
- **Q8 (new, from high finding 2).** `docs/operations/32-...:70` has asserted "every operation <3s; p95/p99 <=3s" for
  the CI profile since 2026-07-18, while its own harness reads 10s/8s/9s (`predicate_layout_smoke...:97`-`:98`).
  ~7 weeks stale, and about a different harness than the one this change touches. File separately, or fold in?

## 10. ADR-077 amendment — DRAFT, OWNER-GATED, NOT APPLIED

Not written to `docs/adr/`. Follows the `docs/adr/046-...:14` precedent: a bolded amendment note inside the amended
ADR, mechanics in the capability spec. It rehomes condition 4's surviving premises rather than restating them loosely.

Insert immediately after `docs/adr/077-...:139` (condition 4), **under M2**:

> **Amendment (#1284, owner ruling <date>):** condition 4's workload, its 3-second figure, and its prohibition on
> activating under a failed absolute budget all stand unchanged. What changes is what counts as a measurement. In the
> per-filter measurement phase, "each operation below 3 seconds" is evaluated on a corroborated measurement: a
> repetition at or above 3 seconds is recorded and re-measured once as a complete set, and the guard fails when the
> corroborating set also breaches. The concurrent load phase's gate is excluded and continues to decide on a single
> sample. The absolute ceiling for this guard is the framework-enforced `natsclient` KV deadline, which the harness
> observes as a typed error rather than predicting as a budget. The measurement rules live in the `graph-index`
> capability spec; this record states the guarantee, not the instrument.

**Under M3** the second sentence becomes *"'Each operation below 3 seconds' is evaluated as p95 at most 3 seconds
and p99 at most 3 seconds over at least N repetitions"*, and `docs/operations/32-...` plus
`TestOwnerLoadPercentiles_DoNotCoverTheMax` both need rewriting.

**Under M5** the note is a different act and belongs in condition 4 itself:

> **Amendment (#1284, owner ruling <date>):** the 5,000-hot-member CI guard is a regression guard, not activation
> evidence. Its budgets are set for order-of-magnitude detection on a shared runner per the gh#220 wall-clock
> discipline. Condition 4's activation evidence is a recorded supervised run of the same 5,000-hot-member profile on
> the current server pin, captured in the Predicate Layout Evidence Runbook alongside condition 5's 21,000-entity
> run. Activation remains prohibited until that supervised run exists on the current pin.

## 11. Withdrawn and corrected claims from revision 1

Recorded rather than quietly edited, because a design that changes its mind silently is unreviewable.

1. **WITHDRAWN — "one rule, two homes, no asymmetry".** Revision 1's task 4.2 corroborated the load-phase gate after
   the dispatcher joins at `:392`-`:396`, which is after `churnWG.Wait()` (`:381`) and `samplerWG.Wait()` (`:386`) —
   i.e. under zero concurrent load. That would have made `:376` effectively never fire, reaching Prohibited-B
   indirectly for the only measurements taken under load, and it falsified revision 1's own I2. Revision 2 excludes
   `:376` explicitly and justifies the exclusion on its three-orders-of-magnitude margin.
2. **WITHDRAWN — "the full profile already encodes this separation at `:85`".** It does not: `:85`'s ceiling half is
   unreachable (P18), so the record revision 1 pointed at as salvage is broken, and § 7's "low-complexity salvage"
   framing of M3 was selling a shape that is functionally Prohibited-B.
3. **CORRECTED — the price of a corroborating set.** Revision 1 said "~0.6-0.8s", citing P12, which is the cost of
   one added repetition across *all* filters. A corroborating set is 5 repetitions of *one* filter: ~0.77s
   (`predicate-forward`) to ~1.28s (`name-forward`). The correction favours M2.
4. **CORRECTED — E's specification.** Revision 1 recorded p50/p95/p99/max only, which cannot answer the question E
   exists to answer, because `:497` sorts first (P19). Revision 2 records per-repetition durations in submission
   order.
5. **CORRECTED — two pin citations.** The 5s timeout is not "applied unconditionally at `:70`" (`:69` guards it;
   `:538` applies it for this operation), and the partial-set refusal is enforced at `natsclient/kv.go:589`/`:592`,
   not in the doc comment at `:533`-`:536`.
6. **CORRECTED — the citation repair target.** Revision 1's task 5.1 re-pointed the test comment at
   `docs/operations/32-...:70`-`:71`. That table describes the **smoke** harness (churn column: `:70`'s
   `2 writers x 100` is `predicate_layout_smoke...:96`; the owner harness is 4 workers x `churnPerWriter: 50`) and
   its CI row is itself stale. Revision 2 re-points at ADR-077 `:139` and the owner-filter acceptance record at
   `docs/operations/32-...:80`+.
7. **CORRECTED — pin count.** `proposal.md` said 63; it is 81 after revision 2.
8. **CORRECTED — a sweep method that produced a false zero.** A first pass over 40 failures with
   `gh run view ... 2>/dev/null` returned zero hits for both harnesses, including for runs known to contain them.
   Removing the stderr discard showed the fetches were failing. The sweeps behind P17 capture stderr to a file and
   report the denominator and every fetch failure.

## 12. Decision skills

- `/kv-or-stream` — **not triggered.** No new communication path.
- `/orchestration-check` — **not triggered.** No multi-step runtime behavior; the corroboration loop is inside one
  test helper.
- `/new-payload` — **not triggered.** No new message type.
- `/query-pattern` — **not triggered.** `KeysByFilter` is called exactly as today.
- `/entity-or-bucket` — **not triggered.** No new durable state.
