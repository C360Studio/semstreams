# Change: The owner-load activation gate is decided by a corroborated measurement, and records what it measured

Closes #1284. Claim: draft PR #1285 on `claude/gh1284-owner-load-gate`, own worktree. Premises pinned at
`main@29187077` in `inventory.md` (81 pins, `task inventory:verify` exit 0). Milestone: `v1.0.0-beta.165` (already on
#1284).

**Revision 2** answers an inventory review that returned CHANGES REQUESTED (2 blocking, 3 high, 4 medium) and folds
in new measurements that partly invert its headline recommendation. `design.md` § 11 records 2 withdrawn claims and
6 corrections rather than editing them away.

**Design status: NOT ACCEPTED. This is an architect draft awaiting independent design review and an owner ruling.**
The central question — what "each operation below 3 seconds" means when the instrument is shared — changes the
reading of an ADR-077 activation condition, so it is the owner's call, not the architect's. `design.md` frames it;
`tasks.md` section 2 holds every gated item.

## Why

`TestIntegration_OwnerFilterLoadHarness` fails the required `Test` job on a wall-clock gate that a runner stall can
forge. Measured over 2026-08-26T18:30Z..2026-09-10T14:13Z: 42 CI failures, every log fetched, **4 of them this harness**
(the run denominator was 314 at the 2026-09-09 measurement and was not re-measured, so the 1.3%-of-runs figure is as
of that date). A **fifth** firing sits just outside that window — run 32872635700, 2026-08-25, `name-forward rep 2`
at 4.805s, the largest observed. One of the five is on `main`. #750 closed as COMPLETED having shipped only a comment
describing the flake, so a known-unfixed flake in a required job has had no open issue behind it since 2026-07-30.

Six measured facts decide the shape of the fix:

1. **The gate predicts a ceiling the framework does not enforce, inside a band where production succeeds.**
   `KeysByFilter` applies `DefaultKVOptions().Timeout` = 5s (`natsclient/kv.go:39`, guarded at `:69`, applied for
   this operation by `applyTimeout` at `:538`) and the harness passes no override (`:168`), so every measured call is
   already bounded at 5s. Production graph-index builds its stores through the same call with no override
   (`processor/graph-index/component.go:927`, `:953`). All four budget firings — 3.356s, 3.515s, 4.760s, 4.805s —
   land in the 3s-5s band where the framework and production both succeed and only the test fails. A fifth event
   reached the framework bound outright and failed at `:487` with `context deadline exceeded`: that is the
   framework's own ceiling working, observed rather than predicted.
2. **The instrument destroys its own evidence.** `require.Less` at `:489` calls `t.FailNow()` before
   `assertOwnerLoadLatency` at `:492`, so the repetitions already collected are discarded. No firing has ever
   recorded its own reps 0..n-1. `incoming-forward` accounts for 2 of the 5 observed events and its `phase=latency`
   line has never been printed. Worse, `assertOwnerLoadLatency` sorts at `:497`, so even a recorded set would lose
   the submission order that separates one stall from a sustained one.
3. **A green run records nothing at all.** `t.Logf` is emitted only for failing tests without `-v`, and
   `scripts/run-integration-tests.sh:311` does not pass it. Measured on run 34475316237 (`main`, green): the only
   graph-index line is `ok ... 60.725s`. ADR-077's Status requires evidence "recorded against the exact
   implementation revision" (`:8`), and `docs/operations/32-...:75` calls silence a failed evidence run. The
   activation gate that authorizes production is, on every passing run, an unrecorded gate.
4. **The distribution the 3-second budget was argued against no longer exists.** gh#750's body records
   `p50=99.784608ms p95=697.726516ms max=2.23697341s` and reads it as "a long tail an order of magnitude past the
   median" — genuine seconds, not a millisecond misread. Three fully-logged current-pin runs show p95 ~157-175ms and
   max 166-389ms. So the budget now carries ~7.7x headroom, gh#220's >=3x wall-clock discipline is **already
   satisfied** and does not call for widening this harness, and the firings are 21.9-30.5x excursions *outside* a
   tight distribution rather than its tail. #750's diagnosis was right for 2026-07-30 and does not describe today.
5. **The same-class instance is in the same package and already resolved this.**
   `processor/graph-index/predicate_layout_smoke_integration_test.go:89`-`:98` hit the identical flake on 2026-07-18
   and widened its CI profile to 10s/8s/9s, demoting it to an order-of-magnitude regression guard under gh#220. It
   has fired **zero** times in 80 fetched CI failures spanning 2026-05-30..2026-09-10, where this harness fired five.
   That produces option M5 in `design.md`, which the architect cannot recommend because it entails raising the
   budget — and which has better empirical support than anything it can.
6. **The contention is partly self-inflicted and unattributed.** `scripts/run-integration-tests.sh:304`/`:311` run
   the whole integration suite with `-race` and **uncapped package parallelism** on one shared `ubuntu-latest`
   runner, so up to `GOMAXPROCS` Docker-backed packages execute while this one measures latency. Whether the stalls
   come from that or from hypervisor steal is not determined — and cannot be, until fact 2 is repaired.

The issue also asks whether `incoming-forward` is exercised by the CI profile. It is: all three fixtures define a
non-empty `forwardFilter` and the label is composed at `:274`/`:278` rather than written literally
(`inventory.md` § 4). No work follows from that question.

## What changes

Behavior of the CI activation guard, in the test tier only. No production code changes.

1. **Record before deciding, in submission order.** `measureOwnerLoadFilter` collects every repetition, logs the
   per-repetition durations **in submission order** — not only the sorted summary, because `assertOwnerLoadLatency`
   sorts at `:497` and the discriminator between one stall and a sustained one is whether an inflated sample is
   isolated or adjacent — and only then evaluates the budget. The distribution is emitted on passing runs too, so
   condition 4's evidence exists on the runs that authorize activation. Its **code** is ungated; its **spec text is
   not separable** from deliverable 2 (one MODIFIED requirement) and adds a normative sentence that retroactively
   voids every CI run to date as condition-4 evidence, which `docs/operations/32-...:75` already says for the
   runbook. That is what Q2 rules.
2. **A breach in the per-filter measurement phase is decided by corroboration, not by one shared-runner sample.** A
   repetition at or above `operationBudget` records the breach and triggers exactly one corroborating measurement set
   of the same filter; the gate fails when the corroborating set also breaches. The concurrent load phase's gate at
   `:376` is **explicitly excluded** and keeps single-sample semantics: the only place to corroborate it is after the
   churn writers and dispatcher have joined (`:381`, `:386`, `:392`-`:396`), which is the quiescence that phase exists
   to exclude, and its margin — single-key owner filters at 2-6ms against 3s — is what makes one sample safe there.
   `operationBudget` stays `3 * time.Second` and both per-repetition assertions stay. **Owner-gated** — it narrows
   the reading of ADR-077 §8 condition 4.
3. **Name one absolute ceiling.** The framework-enforced 5s deadline is it, and it is already asserted at `:487`;
   3 seconds is the latency contract. The full profile at `:85` *attempts* this separation and fails: its
   `operationBudget: 10 * time.Second` is **unreachable**, because every measured call is bounded at 5s and an expiry
   fails at `:487` before `:489`. The 10-second figure is the query handler's bound imported onto an operation the KV
   client bounds first. The delta stops carrying it as a per-operation budget on a directly measured key listing; it
   does not touch ADR-077 condition 5, which carries the same phrase (Q7).
4. **Two citation defects repaired.** `openspec/specs/graph-index/spec.md:184` attributes the 3s CI guard to ADR-065,
   which contracts no such budget (its stated bound is the 10s handler timeout, `docs/adr/065-...:49`); the source is
   ADR-077 §8 condition 4. The test's contract comment cites `docs/operations/32-...:49-50` for the 3s/10s profile
   assignment; that table is at `:70-71`.

## What does not change

- `operationBudget` stays `3 * time.Second`. Raising it is prohibited by #1284 and silently retires condition 4;
  #750 / PR #755 already did it once by matching the wrong profile's contract.
- The per-repetition assertion is not deleted. At `repetitions: 5` both percentiles index `durations[3]` and never
  examine the max, so it is the only tail coverage the CI profile has —
  `TestOwnerLoadPercentiles_DoNotCoverTheMax` exists to prove that.
- `repetitions` stays 5 under the recommendation. Raising it is a separate, data-gated decision (`design.md` option
  M3); its measured cost is ~0.6-0.8s per added repetition across all filters, ~10-13s for 5 -> 21. A corroborating
  set is a different quantity — 5 repetitions of ONE filter, ~0.77s to ~1.28s, paid only on a breach.
- `TestOwnerLoadCIProfile_ContractedBudgets` is untouched and stays green. Under the recommendation no pinned
  constant moves.
- No production code, no `natsclient` surface, no new exported symbol, no new communication path, no new payload.

## Non-goals

- Deciding whether a firing's own neighbouring repetitions were inflated. No current-pin forward distribution
  reaches the 3s-5s band at all (maxima 166-389ms), but the discard at `:489` plus the sort at `:497` mean no firing
  has ever recorded the ordering that would settle it. Deliverable 1 makes it measurable.
- Changing the integration suite's parallelism or splitting the harness into its own CI job (`design.md` option M4).
  Cheap and text-preserving, but its efficacy is unmeasured until deliverable 1 attributes the stalls.
- Adopting M5 — demoting the CI profile to a regression guard and rehoming condition 4's evidence onto a supervised
  run. It entails raising `operationBudget`, which #1284 prohibits and the architect's brief binds it not to
  recommend; it is drafted in full in `design.md` § 4 and docketed at Q1 because it is the owner's call, and because
  its evidence is better than the recommendation's.
- Repairing `docs/operations/32-...:70`, whose CI row has asserted "every operation <3s" since 2026-07-18 while its
  own harness reads 10s/8s/9s. That row is about the smoke harness, not this one (Q8).
- Any sister-repository change. The read-only sweep found no sister citing condition 4 or the 3-second figure.

## Adopter seam inventory

The outward surface is ADR-077 §8 condition 4 — the text a downstream reads to judge whether owner discovery is
activation-ready — and, one level down, `natsclient.KVStore.KeysByFilter`'s silent 5s bound. The specific person:
a sister-repo developer deciding whether to provision fresh storage against an activated graph-index.

1. **What must they know?** That "each operation below 3 seconds" is a latency contract decided by a corroborated
   measurement, and that the absolute ceiling their calls actually run against is the framework's 5s deadline. Two
   facts, both about guarantees, neither about our test's mechanics.
2. **What happens if they do nothing?** Today they over-trust condition 4 as a per-observation guarantee that the
   instrument cannot deliver on a shared runner, and they read a green CI run as recorded evidence when it records
   nothing. For `KeysByFilter` itself nothing bad happens: the framework enforces its own bound and returns
   `context deadline exceeded`, and `collectFilteredKeys` explicitly refuses to return a partial key set on context
   expiry — enforced at `natsclient/kv.go:589` (drain-time cancellation) and `:592` (post-closure re-check), which is
   the code, not the doc comment at `:533`-`:536`. That seam is already observation-shaped and correct.
3. **Where do they find out?** ADR-077 §8 and the graph-index spec (doc tier) for the guarantee; a typed runtime
   error for the 5s bound (the good tier). The evidence-policy fact belongs at doc tier; the correctness fact is
   already above it.
4. **What SHOULD they have to know?** Ideally nothing about the instrument. The ADR states the guarantee; the spec
   and `docs/operations/32-...` own the mechanics. The gap between 1 and 4 is closed by keeping the instrument's
   rules out of the ADR.

**Prefer observation to prediction, applied to this change.** The harness is itself an adopter of `KVStore`, and it
predicts a 3-second ceiling the framework does not enforce. The framework already observes and enforces the real
ceiling at 5s and reports the breach as a typed error — firing #4 is that path working correctly. So the
absolute-ceiling half of condition 4 needs no predicted number at all, and the 3-second figure is a latency
contract. Both profiles conflate it with a ceiling — the CI profile by collapsing three fields onto one value, the
full profile by carrying a 10-second field that can never fire. Naming that is the whole of deliverable 3, and it
deletes a conflation rather than adding a knob.

## Consumers at birth

No new exported symbol, port, subject, bucket, or config field. The changed surfaces have present consumers:
`openspec/specs/graph-index/spec.md:174` (the requirement), `docs/adr/077-...:139` (condition 4),
`docs/operations/32-...:70` (the CI row), and the two test files pinned in `inventory.md` § 7.
