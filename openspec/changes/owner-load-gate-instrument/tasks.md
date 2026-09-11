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
      `design.md` § 9. It touches the supervised profile the ruling just promoted, and ADR-077 condition 5 carries
      the same "10-second handler bound" phrase. **(a) has now been APPLIED under a session-level call, not an owner
      ruling** (task 4.2): after task 4.1 the field was read by nothing but the contract test, and the full profile's
      value could never be compared, so it is the `class:phantom-config` shape. The owner still owns confirming or
      reversing it, and ADR-077 condition 5 is NOT edited either way. (b) The full profile's `p95Budget: 3s` /
      `p99Budget: 5s` now sit at 9.6x / 15.6x over the measured 311.449 ms / 320.157 ms — re-derive them too, or
      leave them? — is untouched and still open. Blocks task 6.1.
- [ ] 2.8 **HOLD — Q8.** `docs/operations/32-...:70` has asserted "every operation <3s; p95/p99 <=3s" for the CI
      profile since 2026-07-18 while its own harness reads 10s/8s/9s. That row describes the **smoke** harness (churn
      column `2 writers x 100` is `predicate_layout_smoke...:96`), so the repair belongs with **#1286**. What this
      change refreshes is the **owner-filter acceptance record at `:80`+**. Confirm the split.
- [ ] 2.9 **HOLD — Q9.** Keep `repetitions: 5`, or raise it? More repetitions make the percentile a better detector
      and harder for a two-repetition stall to move. ~0.6-0.8 s each against a CI subtest that runs 8.64 s today.
      *(Premise corrected at implementation time: this line was written against a 1s percentile budget, which task
      4.3's ruling declined — the budget stays at 3s, so the trade-off is now #1287's to weigh, not this change's.
      The mirror sentence in `design.md` § 9 Q9 carries the same superseded premise.)*
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

- [x] 4.1 Both per-repetition wall-clock assertions are deleted — the measurement phase's old `:489` and the
      concurrent load phase's old `:376`. The `require.NoError`/`require.Len` pair in front of each stays (old `:487`
      and `:374`, now `:522`/`:523` and `:405`/`:406`), so the per-operation ceiling is the typed error those already
      raise. Mutation-checked in task 7.2.
- [x] 4.2 `operationBudget` is removed from the `ownerLoadProfile` struct entirely, taking the full profile's
      unreachable `10 * time.Second` at the old `:85` with it. **This is a session-level call layered on the owner's
      ruling, not an owner ruling** — Q7(a) was on the docket and was implemented under the repo's phantom-config
      doctrine, because after task 4.1 the field was read by nothing but the contract test and the full profile's
      value could never be compared (the same 5s deadline fires first). Flagged for review on PR #1285; Q7(b) — the
      full profile's `p95Budget: 3s` / `p99Budget: 5s` — is untouched and still on the docket at task 2.7.
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
      DONE: both budgets are unchanged at `3 * time.Second`; all four measurements, the ~38x/~17x statement and the
      gh#1287 pointer are in the profile comment, and `TestOwnerLoadCIProfile_ContractedBudgets` pins both values.
- [x] 4.3a Filed as **#1287** (milestone v1.0.0-beta.165, `area:graph-index`, `type:test`): tighten the CI percentile
      budgets from the recorded submission-order distributions. It carries all four measurements in milliseconds, the
      `durations[3]`-needs-two-breaches arithmetic, the 5-20x full-bucket-scan regression class, the unquantified
      two-adjacent-repetition exposure, and design Q10's p50 floor and per-class budgets as additions. Cited from the
      profile comment and from the rewritten contract test.
- [x] 4.4 Rewrite both contract tests to pin the NEW contract rather than deleting the old pins.
      `TestOwnerLoadCIProfile_ContractedBudgets` asserts the CI profile carries no per-operation budget and that its
      percentiles match the published supervised record; `TestOwnerLoadPercentiles_DoNotCoverTheMax` is re-pointed —
      its arithmetic is still true and still worth pinning, but its conclusion becomes "which is why the ceiling is
      the framework deadline, not a percentile", not "the per-rep gate MUST remain". The anti-relaxation property
      must survive the move; deleting these tests along with the assertion they guarded is the failure mode.
      DONE: `TestOwnerLoadCIProfile_ContractedBudgets` rejects, by reflection over `ownerLoadProfile`, ANY
      `time.Duration` field outside `p95Budget`/`p99Budget` — so a renamed per-operation budget trips it too — pins
      both percentiles at 3s, and pins both below `natsclient.DefaultKVOptions().Timeout`, because a budget at or
      above the framework deadline can never fire. `TestOwnerLoadPercentiles_DoNotCoverTheMax` keeps its arithmetic
      as the general property `(n-1)*99/100 < n-1`, builds its fixture from `ci.repetitions` and the real framework
      deadline rather than a hard-coded five-sample literal, and concludes that the ceiling is the deadline.
- [x] 4.5 Per-repetition durations are recorded in **submission order**. `assertOwnerLoadLatency` sorts a COPY, so
      the caller's submission sequence survives, and it logs/records BEFORE asserting, so a run that breaches a
      percentile still publishes the distribution that explains it (proven by the task 7.3 slowdown mutation). The
      concurrent phase carries the submission serial on its result and fills a per-serial slot, with a completeness
      check that turns a lost or duplicated result into a failure instead of a zero-duration sample.
- [x] 4.6 **Route decided: a file named by `GRAPH_INDEX_LATENCY_LOG`, written by the harness and printed by
      `scripts/run-integration-tests.sh` after the suite, on pass and on failure alike.** The first alternative was
      measured and does not work: `go test` discards a PASSING package's output entirely without `-v` — a probe test
      writing to `t.Log`, `os.Stdout` and `os.Stderr` and passing produced only `ok <pkg> <time>`, for `./...` and for
      a single named package. The second alternative (a second script invocation with the package as an argument)
      would re-run the whole graph-index integration package — ~40-60 s and a second lock acquisition — and would
      publish a DIFFERENT distribution from the one the guard actually asserted on. The chosen route needs no
      `ci.yml` change, keeps the suite un-verbose, runs the harness once, and publishes the distribution the
      assertions used. Proven end-to-end through the CI path: `scripts/run-integration-tests.sh ./processor/graph-index`
      exits 0, `go test` prints only `ok ... 39.407s`, and all nine distribution lines follow it.
- [x] 4.7 Rewrite the contract comment at `owner_filter_load_integration_test.go:60`-`:75`. Its core claim —
      "operationBudget 3s is a CONTRACTED ACTIVATION GATE, not a tunable" — is retired by the ruling. The replacement
      says what the CI profile now is (a regression guard), where condition 4's evidence now lives (the supervised
      record), and what the ceiling now is (the framework deadline, observed as a typed error). DONE; the replacement
      states all three, and the "CONTRACTED ACTIVATION GATE" sentence does not survive anywhere in the CODE (inventory.md:57 still quotes it as a pre-change pin, which is its correct home).

## 5. Citation repairs

- [x] 5.1 Repaired. The `docs/operations/32-...:49-50` citation is gone; the comment now points at ADR-077 `:139`
      and at the owner-filter acceptance record at `docs/operations/32-...:80`+. The `:70`-`:71` budget table is not
      cited anywhere — it describes the SMOKE harness and belongs to #1286.
- [x] 5.2 Narrowed to `docs/adr/077-...:139`. Verified against the file: `:134` is the "all of the following" stem,
      condition 4 is the single line `:139`, condition 5 spans `:140`-`:142`. Condition 5 is not cited and ADR-077 is
      not edited by this change.
- [x] 5.3 The comment now names the requirement by title — "Fixed-position owner filtering is proven before
      production reconciliation activates" — with no line number, so a line shift cannot re-break it.
- [x] 5.4 The gh#750 note now reads as stale evidence rather than a wrong measurement: it quotes that run's own
      figures (p50 99.784608 ms, p95 697.726516 ms, max 2.23697341 s) against today's (p95 78-175 ms, max
      166-389 ms), and the closing sentence says what an ADR/spec change now protects is the EVIDENCE HOME, not the
      constant.

## 6. Contract text

- [ ] 6.1 **HOLD until Q7 is ruled.** Sync `specs/graph-index/spec.md` of this change into
      `openspec/specs/graph-index/spec.md` at archive time, not before. It repairs citation defect A at `:184`
      (ADR-065 -> ADR-077 §8 condition 4), names one absolute ceiling observed as a typed error, separates the
      regression guard from the activation evidence, and requires the supervised record's provenance and unit.
- [ ] 6.2 **HOLD until Q3 is ruled.** Apply the ADR-077 amendment drafted in `design.md` § 10 in the form the owner
      rules — in-place note after `docs/adr/077-...:139`, or a new ADR — and decide whether condition 4 keeps its
      literal "below 3 seconds". The architect wrote the words and did not apply them.

## 7. Verification

- [x] 7.1 `go test -race -failfast -tags=integration -timeout=20m -count=1 ./processor/graph-index` exit 0,
      `ok ... 41.500s`, whole package. Both contract tests keep the `integration` build tag and do not run under
      `task test`. The two supervised profiles were also re-run at the implementation revision `d9582508` on a clean
      worktree (Apple M3 Pro; 12 CPU; 38,654,705,664 bytes; Docker 29.7.2; harness pin
      `nats:2.14.4-alpine@sha256:f2123f53...`; nats.go `v1.52.0`): 5k CI **PASS 2.15 s**, worst p95 81.186 ms
      (`name-forward`); 21k full **PASS 43.06 s**, worst p95 314.052 ms / p99 314.433 ms / max 327.332 ms
      (`incoming-forward`). Both parsed unit-safely, 9 of 9 and 18 of 18 rows. These corroborate the `60c79736`
      baselines in `design.md` P25-P26 within ~1%; they do not replace them as the published record, which is
      tasks 3.3-3.4.
- [x] 7.2 KILLED. Mutation applied to `natsclient/kv.go` after committing the change: `KeysByFilter` sleeps 6 s
      when the pattern names the measured PREDICATE forward filter, so the call reaches its own 5 s deadline. The
      harness failed at `owner_filter_load_integration_test.go:522` (the post-change home of the old `:487`) with
      `kv keys by filter "robotics.status.ready.*.*.*.*.*.*": context deadline exceeded`, message `predicate-forward`,
      exit 1 — not silently, and not as a budget comparison. Restored by `cp` backup, md5 verified identical.
- [x] 7.3 KILLED, twice, on the two arms the invariant names. (a) **Over-match:** production
      `predicateIndexForwardFilter` mutated to return the domain-only wildcard `robotics.*.*.*.*.*.*.*.*` instead of
      the exact three-token filter; the guard failed at `owner_filter_load_integration_test.go:523`, the match-set
      assertion, message `predicate-forward`, exit 1, 0.07 s into the subtest — correctness rejected it, no latency
      budget needed. (b) **Sustained slowdown:** `KeysByFilter` mutated to sleep 3.5 s on the same filter — a legal
      success under the 5 s deadline; the guard failed at `:545`, the p95 assertion, with
      `"3.586137875s" is not less than or equal to "3s"`, and the recorded line
      `submitted=3.586137875s,3.584074542s,3.584356917s,3.585977917s,3.590300459s` was published BEFORE the failure,
      which is invariant I4 proven on a failing run. Both restored by `cp` backup with md5 verified identical.
- [x] 7.4 `task lint` exit 0; `go test -race ./...` exit 0 (153 ok, 0 FAIL, 20 no-test-files);
      `go run ./cmd/entity-id-audit .` exit 0 (1,323 candidates — CI's Lint job runs it and `task lint` does not);
      `task schema:generate` exit 0 with no schema or spec drift (`git status --porcelain` named only the three
      edited files); `openspec validate owner-load-gate-instrument --strict` exit 0; `go test -race ./test/testinfra/`
      exit 0, which is the contract test over the runner script this change edits.
      `task inventory:verify` returns **exit 1: pins=92 ok=47 moved=30 ambiguous=1 drift=14**, and that is expected
      rather than a defect: `inventory.md` pins the PRE-change premises at `main@29187077`, and every one of the 14
      drifted pins is a line this change deliberately deleted or rewrote (the two `operationBudget` assertions, the
      CI profile comment and literal, the full profile's `:85`, the four `assertOwnerLoadLatency` lines, the three
      contract-test pins), while the 30 moved pins and the one now-ambiguous pin are pure line shifts. A deleted line
      cannot be re-pinned, and renumbering would break the `file:line` citations `design.md` carries as the ruled
      record, so the inventory is left as the design-time artifact it is. Precedent: the inventories of the two most
      recently archived changes (`2026-09-09-graph-read-tools-signal-absence`, `2026-09-01-loop-token-uuid-enforcement`)
      are both exit 1 on `main` today.
- [ ] 7.5 Independent implementation review by `semstreams-reviewer` recorded on PR #1285.
- [ ] 7.6 A green CI run after this change proves the guard passes. It is explicitly **not** activation evidence —
      that is task 3.3-3.4's published record. Do not report one as the other.

## 8. Archive

- [ ] 8.1 Spec sync (task 6.1) is the last content commit and is reviewed with the code.
- [ ] 8.2 `openspec archive owner-load-gate-instrument`; `implemented-by: <persona>` in the PR body; squash merge
      closes #1284.
