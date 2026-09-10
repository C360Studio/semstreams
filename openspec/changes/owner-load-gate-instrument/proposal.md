# Change: The owner-load activation gate is decided by a corroborated measurement, and records what it measured

Closes #1284. Claim: draft PR #1285 on `claude/gh1284-owner-load-gate`, own worktree. Premises pinned at
`main@29187077` in `inventory.md` (63 pins, `task inventory:verify` exit 0). Milestone: `v1.0.0-beta.165` (already on
#1284).

**Design status: NOT ACCEPTED. This is an architect draft awaiting independent design review and an owner ruling.**
The central question — what "each operation below 3 seconds" means when the instrument is shared — changes the
reading of an ADR-077 activation condition, so it is the owner's call, not the architect's. `design.md` frames it;
`tasks.md` section 2 holds every gated item.

## Why

`TestIntegration_OwnerFilterLoadHarness` fails the required `Test` job on a wall-clock gate that a runner stall can
forge. Measured over 2026-08-26T18:30Z..2026-09-09T22:14Z: 314 CI runs, 40 failures, **4 of them this harness** —
10% of all CI failures and 1.3% of all runs, including one on `main`. #750 closed as COMPLETED having shipped only a
comment describing the flake, so a known-unfixed flake in a required job has had no open issue behind it since
2026-07-30.

Four measured facts decide the shape of the fix:

1. **The gate predicts a ceiling the framework does not enforce, inside a band where production succeeds.**
   `KeysByFilter` applies `DefaultKVOptions().Timeout` = 5s (`natsclient/kv.go:39`, `:70`) and the harness passes no
   override (`:168`), so every measured call is already bounded at 5s. Production graph-index builds its stores
   through the same call with no override (`processor/graph-index/component.go:927`, `:953`). All three budget
   firings — 3.356s, 3.515s, 4.760s — land in the 3s-5s band where the framework and production both succeed and only
   the test fails. The fourth firing reached the framework bound outright and failed at `:487` with
   `context deadline exceeded`: that is the framework's own ceiling working, observed rather than predicted.
2. **The instrument destroys its own evidence.** `require.Less` at `:489` calls `t.FailNow()` before
   `assertOwnerLoadLatency` at `:492`, so the repetitions already collected are discarded. No firing has ever
   recorded its own reps 0..n-1. `incoming-forward` accounts for 2 of the 4 firings and its `phase=latency` line has
   never been printed. "Stall or real tail" is therefore currently unanswerable from the artifact the gate produces.
3. **A green run records nothing at all.** `t.Logf` is emitted only for failing tests without `-v`, and
   `scripts/run-integration-tests.sh:311` does not pass it. Measured on run 34475316237 (`main`, green): the only
   graph-index line is `ok ... 60.725s`. ADR-077's Status requires evidence "recorded against the exact
   implementation revision" (`:8`), and `docs/operations/32-...:75` calls silence a failed evidence run. The
   activation gate that authorizes production is, on every passing run, an unrecorded gate.
4. **The contention is partly self-inflicted and unattributed.** `scripts/run-integration-tests.sh:304`/`:311` run
   the whole integration suite with `-race` and **uncapped package parallelism** on one shared `ubuntu-latest`
   runner, so up to `GOMAXPROCS` Docker-backed packages execute while this one measures latency. Whether the stalls
   come from that or from hypervisor steal is not determined — and cannot be, until fact 2 is repaired.

The issue also asks whether `incoming-forward` is exercised by the CI profile. It is: all three fixtures define a
non-empty `forwardFilter` and the label is composed at `:274`/`:278` rather than written literally
(`inventory.md` § 4). No work follows from that question.

## What changes

Behavior of the CI activation guard, in the test tier only. No production code changes.

1. **Record before deciding.** `measureOwnerLoadFilter` collects every repetition, logs the full distribution, and
   only then evaluates the budget. A firing prints reps 0..n-1. The distribution is emitted on passing runs too, so
   condition 4's evidence exists on the runs that authorize activation. **Not owner-gated** — it changes no budget
   and deletes no assertion.
2. **A breach is decided by corroboration, not by one shared-runner sample.** A repetition at or above
   `operationBudget` records the breach and triggers exactly one corroborating measurement set of the same filter;
   the gate fails when the corroborating set also breaches. `operationBudget` stays `3 * time.Second` and the
   per-repetition assertion stays. **Owner-gated** — it narrows the reading of ADR-077 §8 condition 4.
3. **Name the two bounds the CI profile conflates.** The framework-enforced 5s deadline is the absolute ceiling and
   is already asserted at `:487`; 3 seconds is the latency contract. The full profile already encodes exactly this
   separation at `:85`. No number moves; the spec stops pretending one field is both.
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
  M3); its measured cost is ~0.6-0.8s per added repetition, ~10-13s for 5 -> 21.
- `TestOwnerLoadCIProfile_ContractedBudgets` is untouched and stays green. Under the recommendation no pinned
  constant moves.
- No production code, no `natsclient` surface, no new exported symbol, no new communication path, no new payload.

## Non-goals

- Deciding whether the 3s-5s band contains a real tail of the 5,000-key drain. That is `inventory.md`'s second
  UNRESOLVED and this change makes it measurable rather than answering it.
- Reconciling the gh#750 comment's "same-run max of 2.24s" with the window's 389ms worst forward max
  (`inventory.md`, first UNRESOLVED). Different era, different server pin, or different filter — undetermined, and
  `docs/operations/32-...:44` warns pre-pin performance rows are historical.
- Changing the integration suite's parallelism or splitting the harness into its own CI job (`design.md` option M4).
  Cheap and text-preserving, but its efficacy is unmeasured until deliverable 1 attributes the stalls.
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
   expiry (`natsclient/kv.go:533-536`). That seam is already observation-shaped and correct.
3. **Where do they find out?** ADR-077 §8 and the graph-index spec (doc tier) for the guarantee; a typed runtime
   error for the 5s bound (the good tier). The evidence-policy fact belongs at doc tier; the correctness fact is
   already above it.
4. **What SHOULD they have to know?** Ideally nothing about the instrument. The ADR states the guarantee; the spec
   and `docs/operations/32-...` own the mechanics. The gap between 1 and 4 is closed by keeping the instrument's
   rules out of the ADR.

**Prefer observation to prediction, applied to this change.** The harness is itself an adopter of `KVStore`, and it
predicts a 3-second ceiling the framework does not enforce. The framework already observes and enforces the real
ceiling at 5s and reports the breach as a typed error — firing #4 is that path working correctly. So the
absolute-ceiling half of condition 4 needs no predicted number at all, and the 3-second figure is a latency contract
that the CI profile is the only place to conflate with a ceiling. Naming that is the whole of deliverable 3, and it
deletes a conflation rather than adding a knob.

## Consumers at birth

No new exported symbol, port, subject, bucket, or config field. The changed surfaces have present consumers:
`openspec/specs/graph-index/spec.md:174` (the requirement), `docs/adr/077-...:139` (condition 4),
`docs/operations/32-...:70` (the CI row), and the two test files pinned in `inventory.md` § 7.
