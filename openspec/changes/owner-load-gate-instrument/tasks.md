# Tasks — owner-load-gate-instrument (#1284)

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat. Section 2 uses HOLD deliberately so the queue surfaces the owner docket; other
sections avoid those words unless they mean them.

**Ruled 2026-09-11** (#1284 comment 5635299542): M5 with the supervised re-run; the CI `operationBudget` is deleted,
not widened; `p95Budget`/`p99Budget` kept and re-derived. This file is the implementation order for that ruling.

Premises pinned at `main@29187077` in `inventory.md` (92 pins, `task inventory:verify` exit 0). Load-bearing:
`natsclient/kv.go:39`, `:69`, `:538` (the 5s deadline that is now the ceiling), `:589`, `:592` (partial-set refusal),
`processor/graph-index/owner_filter_load_integration_test.go:376`, `:489` (the two assertions being deleted), `:487`,
`:374` (the typed-error ceiling that replaces them), `:497` (the sort that destroys submission order), `:85` (the
full profile's dead 10s budget — Q7), `owner_filter_budget_contract_test.go:29`, `:32`, `:55`, `:66` (the pins that
must be rewritten deliberately), `docs/operations/32-...:43`, `:80` (the evidence home).

Do not restate the framework deadline as a predicted budget at any value. Below it, it fires on stalls the framework
tolerates; above it, it can never fire. Both defects are in the tree today (`:76` and `:85`).

## 1. Claim and design

- [x] 1.1 Draft PR #1285 opened with `Closes #1284` on `claude/gh1284-owner-load-gate`, own worktree.
- [x] 1.2 Line-pinned inventory; `task inventory:verify` exit 0 (92 pins, 0 drift).
- [x] 1.3 Independent inventory review — CHANGES REQUESTED (2 blocking, 3 high, 4 medium); architect revision 2
      landed with 2 withdrawals and 6 corrections.
- [x] 1.4 Owner ruling 2026-09-11, #1284 comment 5635299542. Design revised to a ruled target state (revision 3),
      which voids the sibling A/B that revision 2 had presented as its strongest evidence — `design.md` § 11.1-11.3.
- [ ] 1.5 Independent review of design revision 3 recorded on PR #1285, covering the ruled state, the re-derived
      percentile numbers in § 2, and the six-item docket.

## 2. Owner docket — every line below is a HOLD on the task it names

- [x] 2.1 Q1 RULED 2026-09-11: M5 with the supervised re-run; CI `operationBudget` deleted; `p95Budget`/`p99Budget`
      kept and re-derived.
- [x] 2.2 Q2 is MOOT under the ruling. It asked whether the evidence repair could land independently, because its
      normative sentence voided every CI run as condition-4 evidence. CI runs are no longer condition-4 evidence at
      all, so the sentence scopes to the supervised run, where it is simply correct.
- [ ] 2.3 **HOLD — Q3, ADR form and what condition 4 says.** In-place amendment note on ADR-077 §8 condition 4
      (`docs/adr/046-...:14` is the precedent), or a new ADR? ADR-107 is taken by the unmerged
      `claude/gh1267-honor-predicate-datatype` branch (`bc7d79cc`), so a new record would be 108 and would race it.
      `design.md` § 10 drafts the M5 wording and also proposes dropping the literal "below 3 seconds" from the ADR in
      favour of the runbook holding the numbers. Blocks task 6.2.
- [ ] 2.4 **HOLD — Q4.** Is M4 (the harness in its own CI job) still wanted? The ruling removes its original
      justification — there is no tight budget left to protect — but the supervised run still has to happen
      somewhere. File, drop, or fold into the supervised-run question.
- [x] 2.5 Q5 RESOLVED by measurement, and revision 3 corrects revision 2's over-generalization: gh#750's 2.237s is
      genuine seconds, but the sibling's "2.65s" is milliseconds misread as seconds and is a different number
      entirely (#1286).
- [ ] 2.6 **HOLD — Q6.** Confirm ADR-065 needs no edit; the citation defect is in the spec text, not the ADR.
- [ ] 2.7 **HOLD — Q7, now load-bearing.** Two parts. (a) `ownerLoadFullProfile`'s `operationBudget: 10 * time.Second`
      at `:85` is dead code under the same 5s deadline; the proposed resolution — remove `operationBudget` from the
      `ownerLoadProfile` struct entirely and delete both per-repetition assertions for both profiles — is stated in
      `design.md` § 9 and **not applied**. It touches the supervised profile the ruling just promoted, and ADR-077
      condition 5 carries the same "10-second handler bound" phrase. (b) The full profile's `p95Budget: 3s` /
      `p99Budget: 5s` now sit at 9.6x / 15.6x over the measured 311.449 ms / 320.157 ms. Re-derive them too, or
      leave them? Blocks tasks 4.2 and 6.1.
- [ ] 2.8 **HOLD — Q8.** `docs/operations/32-...:70` has asserted "every operation <3s; p95/p99 <=3s" for the CI
      profile since 2026-07-18 while its own harness reads 10s/8s/9s. That row describes the **smoke** harness (churn
      column `2 writers x 100` is `predicate_layout_smoke...:96`), so the repair belongs with **#1286**. What this
      change refreshes is the **owner-filter acceptance record at `:80`+**. Confirm the split.
- [ ] 2.9 **HOLD — Q9.** Keep `repetitions: 5`, or raise it? With the 1s budget this is no longer neutral: more
      repetitions make the percentile a better detector and harder for a two-repetition stall to move. ~0.6-0.8 s
      each against a CI subtest that runs 8.64 s today.
- [ ] 2.10 **HOLD — Q10.** Two additions the architect did not apply: a p50 floor at 500 ms as the primary,
      stall-immune regression detector (`durations[2]` needs three of five inflated to move), and per-class budgets
      so one constant stops spanning a 108x range of healthy values. Offered as additions to the ruled
      `p95Budget`/`p99Budget`, never substitutions.

## 3. The supervised re-run — ruling 1's evidence

- [x] 3.1 Supervised run executed at revision `60c79736`, worktree clean, on the same host as the `0a7af288` record
      (Apple M3 Pro; 12 CPU; 38,654,705,664 bytes RAM), current pin `nats:2.14.4-alpine@sha256:f2123f53...`, Go SDK
      `v1.52.0`, Docker 29.7.2. Invocation matches the documented form at
      `docs/operations/evidence/graph-index-pre-tag-0a7af288.md:26`-`:27`:
      `env TESTCONTAINERS_RYUK_DISABLED=true GRAPH_INDEX_OWNER_FILTER_FULL=1 go test -race -tags=integration
      ./processor/graph-index -run '^TestIntegration_OwnerFilterLoadHarness$' -count=1 -v -timeout=25m`.
      **21k full: PASS 43.17 s, exit 0.** Worst measurement p95 311.449 ms, p99 320.157 ms, max 396.719 ms.
- [x] 3.2 The same quiet box also ran the default 5k profile (drop `GRAPH_INDEX_OWNER_FILTER_FULL`), because CI's
      percentile budgets are a 5k quantity and cannot be derived from a 21k run. **PASS 2.12 s, exit 0.** Worst
      measurement p95 77.861 ms, max 80.068 ms. Both baselines are recorded in `design.md` P25-P26 and parsed with a
      unit-safe regex — 18 of 18 and 9 of 9 rows, after a first parse silently dropped two `µs` rows (§ 11.10).
- [ ] 3.3 Publish both raw logs as the in-tree evidence appendix the acceptance record points at, alongside
      `docs/operations/evidence/graph-index-pre-tag-0a7af288.md`. Sources:
      `scratchpad/supervised_baseline.log` and `scratchpad/ci_baseline.log` in this session's scratchpad — copy them
      in rather than re-running, since re-running produces a different measurement. Carry the provenance table shape
      from `docs/operations/32-...:85`-`:95`: revision, worktree state, run timestamp and timezone, host CPU and
      memory, Docker allocation, Docker client/server, NATS server and image digest, Go SDK, evidence capture.
- [ ] 3.4 Replace the historical latency rows in the owner-filter acceptance record (`docs/operations/32-...:80`+)
      with the `60c79736` measurements, and state the unit on every table — the #1286 defect is a unit that was
      stated once, far from the number that was read. Note that the old record's CONTEXT rows have no counterpart:
      that store was retired after `0a7af288`, which is itself evidence the record is stale.

## 4. The ruled instrument change

- [ ] 4.1 Delete `operationBudget` from the CI profile and with it both per-repetition wall-clock assertions —
      `owner_filter_load_integration_test.go:489` (measurement phase) and `:376` (concurrent load phase). The
      per-operation ceiling becomes the typed error `require.NoError` already raises at `:487` and `:374`.
- [ ] 4.2 **HOLD until Q7 is ruled.** Whether `operationBudget` leaves the `ownerLoadProfile` struct entirely, taking
      the full profile's unreachable 10s at `:85` with it.
- [ ] 4.3 **OWNER RULED 2026-09-11: keep `p95Budget`/`p99Budget` at `3 * time.Second` in this change.** The
      re-derivation to `1 * time.Second` is DEFERRED until the task 4.5 recording produces within-filter adjacency
      data. Rationale, in the owner's terms: this change must be strictly flake-reducing, and a 1s percentile gate
      is exposed to a stall that inflates two consecutive repetitions — an exposure the design itself calls real and
      unquantified, whose only supporting evidence is cross-filter. Tightening on unmeasured exposure is the same
      predict-instead-of-observe move that deleting the per-operation budget just retired.
      Record the measured basis in the comment anyway, **in milliseconds**, so the follow-up starts from data and not
      from a re-derivation: quiet-box worst healthy p95 77.861 ms; worst shared-runner healthy p95 175.4 ms; worst
      shared-runner healthy sample 389.0 ms; supervised 21k worst p95 311.449 ms / p99 320.157 ms. Note that 3s is a
      weak regression guard at ~38x the quiet-box p95 and that the follow-up exists to fix that.
- [ ] 4.3a File the follow-up issue: tighten the CI percentile budgets from the recorded adjacency data. It must cite
      the measurements above, the `durations[3]`-needs-two-breaches arithmetic, and the realistic regression class
      (filtered `ListKeys` degrading to a full-bucket scan, 5-20x). Blocked until task 4.5's output has covered
      enough CI runs to show whether a stall inflates consecutive repetitions.
- [ ] 4.4 Rewrite both contract tests to pin the NEW contract rather than deleting the old pins.
      `TestOwnerLoadCIProfile_ContractedBudgets` asserts the CI profile carries no per-operation budget and that its
      percentiles match the published supervised record; `TestOwnerLoadPercentiles_DoNotCoverTheMax` is re-pointed —
      its arithmetic is still true and still worth pinning, but its conclusion becomes "which is why the ceiling is
      the framework deadline, not a percentile", not "the per-rep gate MUST remain". The anti-relaxation property
      must survive the move; deleting these tests along with the assertion they guarded is the failure mode.
- [ ] 4.5 Record the per-repetition durations in **submission order**, not only the sorted summary: `:497` sorts
      before any percentile is computed, and ordering is what separates one stall from a sustained one.
- [ ] 4.6 Decide and record the delivery route for that output. Constraint: `ci.yml:144` invokes
      `scripts/run-integration-tests.sh` with no arguments, so `packages=(./...)` (`:307`-`:309`) and `:311` is a
      single `go test`; adding `-v` makes the whole suite verbose. Alternatives: write to the test binary's stdout,
      or a second invocation of the script with the package as an argument (it accepts `"$@"` and releases its host
      lock on exit).
- [ ] 4.7 Rewrite the contract comment at `owner_filter_load_integration_test.go:60`-`:75`. Its core claim —
      "operationBudget 3s is a CONTRACTED ACTIVATION GATE, not a tunable" — is retired by the ruling. The replacement
      says what the CI profile now is (a regression guard), where condition 4's evidence now lives (the supervised
      record), and what the ceiling now is (the framework deadline, observed as a typed error).

## 5. Citation repairs

- [ ] 5.1 The comment cites `docs/operations/32-...:49-50` for the 3s/10s profile assignment; those lines are the
      `TestIntegration_PredicateLayoutSmoke` reproduction command and a blank line. Re-point at ADR-077 `:139` and
      the owner-filter acceptance record at `:80`+ — **not** at the budget table at `:70`-`:71`, which describes the
      smoke harness and is itself stale (Q8, #1286).
- [ ] 5.2 The comment cites `docs/adr/077-...:134-142` for condition 4; condition 4 is at `:139` and condition 5 at
      `:140`-`:142`. Narrow it.
- [ ] 5.3 Re-point "the graph-index spec's absolute-budget requirement" at the requirement title rather than a line
      number, so the next line shift does not re-break it.
- [ ] 5.4 Update the gh#750 note at `:73`-`:75`: the 2.24 s was a real measurement of a distribution that no longer
      exists (p95 ~698 ms -> ~170 ms; max 2.237 s -> 166-389 ms), so it is stale evidence rather than a wrong
      measurement. Do not carry the "relaxing this needs an ADR/spec change" sentence forward unchanged — under the
      ruling the budget is gone, and the sentence that replaces it protects the *evidence home*, not the constant.

## 6. Contract text

- [ ] 6.1 **HOLD until Q7 is ruled.** Sync `specs/graph-index/spec.md` of this change into
      `openspec/specs/graph-index/spec.md` at archive time, not before. It repairs citation defect A at `:184`
      (ADR-065 -> ADR-077 §8 condition 4), names one absolute ceiling observed as a typed error, separates the
      regression guard from the activation evidence, and requires the supervised record's provenance and unit.
- [ ] 6.2 **HOLD until Q3 is ruled.** Apply the ADR-077 amendment drafted in `design.md` § 10 in the form the owner
      rules — in-place note after `docs/adr/077-...:139`, or a new ADR — and decide whether condition 4 keeps its
      literal "below 3 seconds". The architect wrote the words and did not apply them.

## 7. Verification

- [ ] 7.1 `go test -race -tags=integration ./processor/graph-index` whole-package green; both contract tests carry
      the `integration` build tag and do not run under `task test`.
- [ ] 7.2 Prove the ceiling still fails closed. Mutation check: force `KeysByFilter` to exceed the deadline and
      confirm the guard fails at `:487` with `context deadline exceeded` and not silently. Commit before mutating.
- [ ] 7.3 Prove the regression guard still detects a regression. Mutation check: make a forward filter over-match or
      rescan, and confirm the match-set assertion, the convergence check at `:443`, or the 1s percentile budget
      rejects it. This is invariant I3 and it is what makes deleting the per-operation gate safe.
- [ ] 7.4 `task lint`, unit suite, `task schema:generate` with no drift, `openspec validate
      owner-load-gate-instrument --strict`, `task inventory:verify -- openspec/changes/owner-load-gate-instrument/
      inventory.md`.
- [ ] 7.5 Independent implementation review by `semstreams-reviewer` recorded on PR #1285.
- [ ] 7.6 A green CI run after this change proves the guard passes. It is explicitly **not** activation evidence —
      that is task 3.3-3.4's published record. Do not report one as the other.

## 8. Archive

- [ ] 8.1 Spec sync (task 6.1) is the last content commit and is reviewed with the code.
- [ ] 8.2 `openspec archive owner-load-gate-instrument`; `implemented-by: <persona>` in the PR body; squash merge
      closes #1284.
