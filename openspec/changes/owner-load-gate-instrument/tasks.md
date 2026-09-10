# Tasks — owner-load-gate-instrument (#1284)

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat. Section 2 uses HOLD deliberately so the queue surfaces the owner docket; other
sections avoid those words unless they mean them.

Premises measured on `main@29187077` and pinned in `inventory.md` (66 pins, `task inventory:verify` exit 0). The
load-bearing ones: `natsclient/kv.go:39`, `:70` (the 5s bound the framework already enforces),
`processor/graph-index/component.go:927`, `:953` (production runs the same bound),
`processor/graph-index/owner_filter_load_integration_test.go:376`, `:489`, `:492` (two per-repetition gates, one of
which discards its evidence), `owner_filter_budget_contract_test.go:29`, `:32`, `:55`, `:66` (the anti-relaxation
pins that constrain the target state), `scripts/run-integration-tests.sh:304`, `:311` (uncapped parallel Docker
packages under `-race`), `docs/adr/077-...:139` (condition 4), `openspec/specs/graph-index/spec.md:184` (citation
defect A), `docs/operations/32-...:70` (the CI row the test comment mis-cites as `:49-50`).

Do not raise `operationBudget` and do not delete a per-repetition assertion. Either silently retires ADR-077
condition 4's activation evidence (#1284; PR #755 already did the first one once).

## 1. Claim and design

- [x] 1.1 Draft PR #1285 opened with `Closes #1284` on `claude/gh1284-owner-load-gate`, own worktree.
- [x] 1.2 Line-pinned inventory at `openspec/changes/owner-load-gate-instrument/inventory.md`; `task
      inventory:verify` exit 0 (66 pins, 0 drift).
- [ ] 1.3 Independent inventory review (`semstreams-reviewer` re-derivation) recorded on PR #1285. An INVENTORY PASS
      is required before the design is submitted for review.
- [ ] 1.4 Independent pre-owner design review of `design.md` recorded on PR #1285.
- [ ] 1.5 Consider one `semstreams-judge` round on the § 7 fork (M2's empirical stall-immunity against M3's
      arithmetic one) before the owner rules. Optional; the default is not to spawn.

## 2. Owner docket — every line below is a HOLD on the task it names

- [ ] 2.1 **HOLD — Q1, the ruling this change exists for.** Does "each operation below 3 seconds" mean every
      observation (M1/M4), every corroborated observation (M2), or a percentile over more repetitions (M3)?
      Architect recommends M2; `design.md` § 7 states the case for M3. Blocks sections 4 and 6.
- [ ] 2.2 **HOLD — Q2.** Does the evidence repair (section 3) land independently of Q1? It changes no budget and
      deletes no assertion, so the architect reads it as ungated, but it does change what a failing run prints.
- [ ] 2.3 **HOLD — Q3, ADR form.** In-place amendment note on ADR-077 §8 condition 4 (`docs/adr/046-...:14` is the
      precedent), or a new ADR? ADR-107 is taken by the unmerged `claude/gh1267-honor-predicate-datatype` branch
      (`bc7d79cc`), so a new record would be 108 and would race that branch. Blocks task 6.2.
- [ ] 2.4 **HOLD — Q4.** File M4 (harness in its own CI job) as a follow-on issue now, or hold it until section 3's
      recorded data attributes the stalls between in-run contention and hypervisor steal?
- [ ] 2.5 **HOLD — Q5.** The gh#750 comment at `owner_filter_load_integration_test.go:73` records a "same-run max of
      2.24s" that no forward filter in the measured window approaches (worst 389ms). Correct it to current-pin
      measurements, or preserve it as historical under `docs/operations/32-...:44`'s pre-pin warning?
- [ ] 2.6 **HOLD — Q6.** Citation defect A is repaired in the spec delta (ADR-065 -> ADR-077 §8 condition 4).
      ADR-065's own text is not wrong and needs no edit — confirm no ADR-065 change is wanted.

## 3. Record before deciding (option E) — the evidence repair

Gated only by task 2.2. Nothing here moves a budget or removes an assertion.

- [ ] 3.1 `measureOwnerLoadFilter` collects every repetition before evaluating any budget, so a firing prints reps
      0..n-1 instead of discarding them at `:489`. The `require.NoError` and `require.Len` checks at `:487`/`:488`
      stay per-repetition — an errored or wrong-length result is not a latency observation.
- [ ] 3.2 The `phase=latency` line is emitted for every filter on every run, pass or fail. Today `t.Logf` output
      reaches CI only on failure, and `scripts/run-integration-tests.sh:311` passes no `-v`: decide between passing
      `-v` for this package and writing the distribution through a channel the runner always shows, and record the
      choice here.
- [ ] 3.3 The concurrent phase (`:372`-`:400`) records its per-label distributions the same way. It already defers
      `assertOwnerLoadLatency` to `:400`; only the per-repetition assertion at `:376` short-circuits.
- [ ] 3.4 One repetition run (`-count`, default GOMAXPROCS, not `-cpu 1`) with the recorded output captured, so the
      stall-window duration behind the observed firings becomes a measurement instead of an inference. Attach it to
      PR #1285; it is the input to task 2.1 and task 2.4.

## 4. Corroborated breach (option M2) — held on task 2.1

- [ ] 4.1 **HOLD until Q1 is ruled.** A repetition at or above `operationBudget` records the breach and triggers
      exactly one corroborating measurement set of the same filter, taken after the breaching set completes. The
      guard fails when the corroborating set also breaches. Never a third set.
- [ ] 4.2 **HOLD until Q1 is ruled.** The same rule covers the concurrent phase's gate at `:376`: collect breaching
      labels during the drain, corroborate after the dispatcher joins at `:392`-`:396`. No re-measurement inside the
      dispatcher — that would perturb the concurrency the phase exists to test.
- [ ] 4.3 **HOLD until Q1 is ruled.** A breach is recorded with a stable, greppable key even when the corroborating
      set passes, so its rate is countable from CI history. This is the mitigation for `design.md` § 7 item 2 and
      the input to any later M3 decision.
- [ ] 4.4 `operationBudget` stays `3 * time.Second`, `p95Budget` and `p99Budget` stay `3 * time.Second`,
      `repetitions` stays 5. Verify by running `TestOwnerLoadCIProfile_ContractedBudgets` and
      `TestOwnerLoadPercentiles_DoNotCoverTheMax` unchanged — under M2 neither test file needs an edit, and needing
      one means the implementation drifted off the ruling.

## 5. Citation repairs

- [ ] 5.1 The contract comment at `owner_filter_load_integration_test.go:60`-`:75` cites
      `docs/operations/32-predicate-layout-smoke-harness.md:49-50` for the 3s/10s profile assignment. Those lines are
      "Run the CI profile:" and a blank line; the budget table is at `:70`-`:71`. Repair the citation.
- [ ] 5.2 The same comment cites `docs/adr/077-...:134-142` for condition 4; condition 4 is at `:139` and condition 5
      at `:140`-`:142`. Narrow the citation to what it means.
- [ ] 5.3 Re-point the comment's "the graph-index spec's absolute-budget requirement" at the requirement title rather
      than a line number, so the next line shift does not re-break it.

## 6. Contract text

- [ ] 6.1 Sync `specs/graph-index/spec.md` of this change into `openspec/specs/graph-index/spec.md` at archive time,
      not before. It repairs citation defect A at `:184` (ADR-065 -> ADR-077 §8 condition 4) and carries the
      two-bound separation and the corroboration rule.
- [ ] 6.2 **HOLD until Q1 and Q3 are ruled.** Apply the ADR-077 amendment drafted in `design.md` § 10 in the form the
      owner rules — in-place note after `docs/adr/077-...:139`, or a new ADR. The architect wrote the words but did
      not apply them.
- [ ] 6.3 **HOLD until Q1 is ruled.** Update the CI row at `docs/operations/32-...:70` only if the ruling changes
      what the row asserts. Under M2 the row's numbers are unchanged and only a sentence about corroboration is
      added; under M3 the row itself changes.

## 7. Verification

- [ ] 7.1 `go test -race -tags=integration -run '^TestIntegration_OwnerFilterLoadHarness$' ./processor/graph-index`
      green, with the recorded distribution attached to PR #1285.
- [ ] 7.2 Repetition evidence, not one green run: the rate being fixed is 1.3% of runs, so a single pass proves
      nothing. Record a `-count` run and state the denominator.
- [ ] 7.3 Mutation check the corroboration wiring, not the primitive: delete the CALL that triggers the corroborating
      set and prove the gate reverts to deciding on one sample.
- [ ] 7.4 `go test -race -tags=integration ./processor/graph-index` whole-package green (the two contract tests carry
      the `integration` build tag and do not run under `task test`).
- [ ] 7.5 `task lint`, unit suite, `task schema:generate` with no drift, `openspec validate
      owner-load-gate-instrument --strict`, `task inventory:verify -- openspec/changes/owner-load-gate-instrument/
      inventory.md`.
- [ ] 7.6 Independent implementation review by `semstreams-reviewer` recorded on PR #1285.

## 8. Archive

- [ ] 8.1 Spec sync (task 6.1) is the last content commit and is reviewed with the code.
- [ ] 8.2 `openspec archive owner-load-gate-instrument`; `implemented-by: <persona>` in the PR body; squash merge
      closes #1284.
