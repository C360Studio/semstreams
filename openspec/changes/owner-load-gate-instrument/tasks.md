# Tasks — owner-load-gate-instrument (#1284)

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat. Section 2 uses HOLD deliberately so the queue surfaces the owner docket; other
sections avoid those words unless they mean them.

Premises measured on `main@29187077` and pinned in `inventory.md` (81 pins, `task inventory:verify` exit 0). The
load-bearing ones: `natsclient/kv.go:39`, `:69`, `:538` (the 5s bound the framework enforces), `:589`, `:592` (the
partial-set refusal), `processor/graph-index/component.go:927`, `:953` (production runs the same bound),
`processor/graph-index/owner_filter_load_integration_test.go:376`, `:489`, `:492`, `:497` (two per-repetition gates,
one discarding its evidence, and the sort that destroys submission order),
`owner_filter_budget_contract_test.go:29`, `:32`, `:55`, `:66` (the anti-relaxation pins that constrain the target
state), `predicate_layout_smoke_integration_test.go:89`-`:98` (the same-class instance that resolved this in
2026-07-18 under gh#220), `scripts/run-integration-tests.sh:304`, `:307`-`:311` (uncapped parallel Docker packages
under `-race`, one `go test` over `./...`), `docs/adr/077-...:139` (condition 4),
`openspec/specs/graph-index/spec.md:184` (citation defect A), `docs/operations/32-...:77`, `:80`, `:43`.

Do not raise `operationBudget` and do not delete a per-repetition assertion. #1284 prohibits both; M5 in `design.md`
is the non-silent rehoming and is docketed at task 2.1, not assumed.

## 1. Claim and design

- [x] 1.1 Draft PR #1285 opened with `Closes #1284` on `claude/gh1284-owner-load-gate`, own worktree.
- [x] 1.2 Line-pinned inventory; `task inventory:verify` exit 0 (81 pins, 0 drift).
- [x] 1.3 Independent inventory review recorded on PR #1285 — INVENTORY CHANGES REQUESTED: 2 blocking, 3 high, 4
      medium. Architect revision 2 landed; `design.md` § 11 records 2 withdrawals and 6 corrections. Re-review owed.
- [ ] 1.4 Re-review of the revised inventory and design. An INVENTORY PASS is required before implementation begins.
- [ ] 1.5 Independent pre-owner design review of `design.md` revision 2 recorded on PR #1285.
- [ ] 1.6 Consider one `semstreams-judge` round on the § 7 item 1 fork: M5 has the better empirical evidence (a
      same-package sibling with zero firings in 80 fetched CI failures) and the architect is barred from
      recommending it. That is the shape a judge exists for. Optional; the default is not to spawn.

## 2. Owner docket — every line below is a HOLD on the task it names

- [ ] 2.1 **HOLD — Q1, the ruling this change exists for, now four arms.** Does "each operation below 3 seconds" mean
      every observation (M1/M4), every corroborated observation in the measurement phase (M2), a percentile over more
      repetitions (M3), or is the CI guard no longer condition 4's evidence at all (M5)? Architect recommends M2 and
      records in `design.md` § 7 item 1 that M5 is better-evidenced and barred to it, not disproven. Blocks sections
      4 and 6.
- [ ] 2.2 **HOLD — Q2.** Does the evidence repair land independently of Q1? Its code is ungated; its spec text is not
      separable from M2's (one MODIFIED requirement, synced as a unit by task 6.1), and its normative sentence "A run
      that records no distribution is not activation evidence" retroactively voids every CI run to date as
      condition-4 evidence — `docs/operations/32-...:75` already says as much for the runbook.
- [ ] 2.3 **HOLD — Q3, ADR form.** In-place amendment note on ADR-077 §8 condition 4 (`docs/adr/046-...:14` is the
      precedent), or a new ADR? ADR-107 is taken by the unmerged `claude/gh1267-honor-predicate-datatype` branch
      (`bc7d79cc`), so a new record would be 108 and would race that branch. Blocks task 6.2.
- [ ] 2.4 **HOLD — Q4.** File M4 (harness in its own CI job) as a follow-on issue now, or hold it until section 3's
      recorded data attributes the stalls between in-run contention and hypervisor steal?
- [x] 2.5 Q5 is RESOLVED by measurement and is no longer a question. gh#750's "same-run max of 2.24s" was real
      (`p50=99.784608ms p95=697.726516ms max=2.23697341s`, quoted in #750's body, which reads it as "a long tail an
      order of magnitude past the median" — the millisecond-misread hypothesis is falsified by the issue's own
      words). The distribution collapsed: p95 ~698ms -> ~170ms, max 2.237s -> 166-389ms. Task 5.4 records that.
- [ ] 2.6 **HOLD — Q6.** Citation defect A is repaired in the spec delta (ADR-065 -> ADR-077 §8 condition 4).
      ADR-065's own text is not wrong and needs no edit — confirm no ADR-065 change is wanted.
- [ ] 2.7 **HOLD — Q7, the unreachable ceiling.** `:85` sets `operationBudget: 10 * time.Second` on the full profile,
      but every measured call is a `KeysByFilter` bounded at 5s, so it can never fire. The delta stops carrying the
      10-second figure as a per-operation budget on a directly measured key listing. ADR-077 condition 5 carries the
      same phrase. Confirm the delta's wording, and rule whether condition 5 and the dead `:85` field are in scope
      here or a separate filing. The architect touched neither. Blocks task 6.1.
- [ ] 2.8 **HOLD — Q8.** `docs/operations/32-...:70` has asserted "every operation <3s; p95/p99 <=3s" for the CI
      profile since 2026-07-18 while its own harness reads 10s/8s/9s (`predicate_layout_smoke...:97`-`:98`). ~7 weeks
      stale, and about a different harness than this change touches. File separately, or fold in?

## 3. Record before deciding, in submission order (option E)

Gated only by task 2.2. Nothing here moves a budget or removes an assertion.

- [ ] 3.1 `measureOwnerLoadFilter` collects every repetition before evaluating any budget, so a firing prints reps
      0..n-1 instead of discarding them at `:489`. `require.NoError` and `require.Len` at `:487`/`:488` stay
      per-repetition — an errored or wrong-length result is not a latency observation.
- [ ] 3.2 Record **per-repetition durations in submission order**, not only the sorted summary. `:497` sorts before
      any percentile is computed, and the discriminator between one stall and a sustained one is whether an inflated
      sample is isolated or adjacent to another. A p50/p95/p99/max line cannot answer it.
- [ ] 3.3 Pick the delivery route and record the choice here. Constraint: `ci.yml:144` invokes
      `scripts/run-integration-tests.sh` with no arguments, so `packages=(./...)` (`:307`-`:309`) and `:311` is a
      single `go test`; adding `-v` makes the whole suite verbose. The alternatives are writing the distribution to
      the test binary's stdout, or a second invocation of the script with the package as an argument (it accepts
      `"$@"` and releases its host lock on exit).
- [ ] 3.4 The concurrent phase (`:372`-`:400`) records its per-label distributions the same way. It already defers
      `assertOwnerLoadLatency` to `:400`; only the per-repetition assertion at `:376` short-circuits.
- [ ] 3.5 Collect the recorded distributions from CI runs, not from a developer laptop. A local `-count` run samples
      a quiet machine; the 1.3% rate is over the `ubuntu-latest` + uncapped-parallel-Docker population, which a local
      run does not sample at all. Push the branch and read the recorded output from the `Test` job across several
      runs. This is the input to tasks 2.1 and 2.4, and its value is the stall-window duration behind the five
      observed firings.

## 4. Corroborated breach in the measurement phase (option M2) — held on task 2.1

- [ ] 4.1 **HOLD until Q1 is ruled.** In `measureOwnerLoadFilter`, a repetition at or above `operationBudget` records
      the breach and triggers exactly one corroborating measurement set of the same filter, taken after the breaching
      set completes. The guard fails when the corroborating set also breaches. Never a third set.
- [ ] 4.2 **HOLD until Q1 is ruled.** The concurrent load phase's gate at `:376` is **excluded** from the rule and
      keeps single-sample semantics. Do not corroborate it: the only place to do so is after `churnWG.Wait()`
      (`:381`), `samplerWG.Wait()` (`:386`), `cancelDispatch()` (`:392`) and the join (`:392`-`:396`) — under the
      quiescence the phase exists to exclude, which would make `:376` effectively never fire. Its margin is what
      makes a single sample safe there: single-key owner filters at 2-6ms against a 3-second budget.
- [ ] 4.3 **HOLD until Q1 is ruled.** A breach is recorded with a stable, greppable key even when the corroborating
      set passes, so its rate is countable from CI history. This is the mitigation for `design.md` § 7 item 3 and the
      input to any later M3 or M5 decision.
- [ ] 4.4 `operationBudget` stays `3 * time.Second`, `p95Budget` and `p99Budget` stay `3 * time.Second`,
      `repetitions` stays 5. Verify by running `TestOwnerLoadCIProfile_ContractedBudgets` and
      `TestOwnerLoadPercentiles_DoNotCoverTheMax` unchanged — under M2 neither test file needs an edit, and needing
      one means the implementation drifted off the ruling.

## 5. Citation repairs

- [ ] 5.1 The contract comment at `owner_filter_load_integration_test.go:60`-`:75` cites
      `docs/operations/32-predicate-layout-smoke-harness.md:49-50` for the 3s/10s profile assignment. Those lines are
      the `TestIntegration_PredicateLayoutSmoke` reproduction command and a blank line. Re-point at ADR-077 `:139`
      and the owner-filter acceptance record at `docs/operations/32-...:80`+ — **not** at the budget table at
      `:70`-`:71`, which describes the smoke harness (churn column: `2 writers x 100` is
      `predicate_layout_smoke...:96`; this harness is 4 workers x `churnPerWriter: 50` at `:59`) and is itself stale.
- [ ] 5.2 The same comment cites `docs/adr/077-...:134-142` for condition 4; condition 4 is at `:139` and condition 5
      at `:140`-`:142`. Narrow the citation to what it means.
- [ ] 5.3 Re-point the comment's "the graph-index spec's absolute-budget requirement" at the requirement title rather
      than a line number, so the next line shift does not re-break it.
- [ ] 5.4 Update the gh#750 note at `:73`-`:75` to say what task 2.5 established: the 2.24s was a real measurement of
      a distribution that no longer exists (p95 ~698ms -> ~170ms; max 2.237s -> 166-389ms across three fully-logged
      current-pin runs), so the note is stale evidence rather than a wrong measurement, and
      `docs/operations/32-...:43` is the mechanism. Keep the "relaxing this needs an ADR/spec change" sentence.

## 6. Contract text

- [ ] 6.1 **HOLD until Q7 is ruled.** Sync `specs/graph-index/spec.md` of this change into
      `openspec/specs/graph-index/spec.md` at archive time, not before. It repairs citation defect A at `:184`
      (ADR-065 -> ADR-077 §8 condition 4), names one absolute ceiling, and carries the corroboration and
      submission-order recording rules.
- [ ] 6.2 **HOLD until Q1 and Q3 are ruled.** Apply the ADR-077 amendment drafted in `design.md` § 10 in the form the
      owner rules — in-place note after `docs/adr/077-...:139`, or a new ADR, and the M2 wording or the M5 wording.
      The architect wrote the words and did not apply them.
- [ ] 6.3 **HOLD until Q1 is ruled.** Under M2 the runbook's owner-filter section gains one sentence about
      corroboration and no number moves. Under M5 the CI profile's budgets and the acceptance record both change, and
      a supervised re-run on the current pin becomes a precondition of condition 4 having any evidence at all
      (`docs/operations/32-...:43` marks the existing rows historical).

## 7. Verification

- [ ] 7.1 `go test -race -tags=integration -run '^TestIntegration_OwnerFilterLoadHarness$' ./processor/graph-index`
      green locally, with the recorded submission-order distribution attached to PR #1285. This proves the
      instrument works; it does not sample the population the flake rate is over.
- [ ] 7.2 The rate evidence comes from CI, not from a laptop: push and read the `Test` job's recorded distributions
      across several runs. State the denominator (runs observed) with any rate claim, and do not report a local
      `-count` pass as evidence about the CI population.
- [ ] 7.3 Mutation check the corroboration wiring, not the primitive: delete the CALL that triggers the corroborating
      set and prove the gate reverts to deciding on one sample. Commit before mutating.
- [ ] 7.4 `go test -race -tags=integration ./processor/graph-index` whole-package green (both contract tests carry
      the `integration` build tag and do not run under `task test`).
- [ ] 7.5 `task lint`, unit suite, `task schema:generate` with no drift, `openspec validate
      owner-load-gate-instrument --strict`, `task inventory:verify -- openspec/changes/owner-load-gate-instrument/
      inventory.md`.
- [ ] 7.6 Independent implementation review by `semstreams-reviewer` recorded on PR #1285.

## 8. Archive

- [ ] 8.1 Spec sync (task 6.1) is the last content commit and is reviewed with the code.
- [ ] 8.2 `openspec archive owner-load-gate-instrument`; `implemented-by: <persona>` in the PR body; squash merge
      closes #1284.
