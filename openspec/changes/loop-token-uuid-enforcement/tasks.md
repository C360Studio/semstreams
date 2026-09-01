# Tasks: loop-token UUID enforcement

Claim: draft PR #1210, branch `claude/gh1192-framed-digest-run-identity`, base `ae35f296`. The framed-digest
package this replaces survives in PR history at `b0e92253`. Deviations from the 2026-08-31 ruling are BLOCKING.

## 1. Gates before implementation

- [x] 1.1 RULED (owner, 2026-08-31, chat, transcribed on #1192): "q1 - everyone who mints a loop uses uuid" —
      A1 confirmed, graph-research IN scope. A2 conditionals removed from this file.
- [x] 1.2 RULED (owner, 2026-08-31, chat, transcribed on #1192): "q2 drop it unless we are fixing it" — this
      scope does not fix it; `Closes #1174` dropped from the PR #1210 body. #1174 stays open on its own.

## 2. Baseline capture — write the named tests first

- [x] 2.1 `agentic/user_types_test.go`: `TestTaskMessageRefusesNonUUIDLoopID` (present + non-canonical → error
      naming `loop_id`; empty → valid; canonical → valid; uppercase/braced/urn forms → refused) and
      `TestTaskMessageRefusesNonCanonicalLoopTokenFields` (`parent_loop_id`, `in_reply_to`, `run_id` — each
      refused naming its field; round-2 B2/H1).
- [x] 2.2 `processor/agentic-loop/state_test.go`: `TestCreateLoopWithIDRefusesNonUUIDToken` (classified invalid,
      no loop registered, no context manager created).
- [x] 2.3 `processor/agentic-loop/component` intake test: `TestNonUUIDLoopIDIsTerminatedAtIntake` — decoded task
      with `loop_id: "workflow-7"` → intake-rejection metric increments, delivery terminated, no loop exists.
- [x] 2.4 `processor/agentic-dispatch`: `TestNewConversationMintsCanonicalUUID` (BOTH intake paths — HTTP submit
      and channel message — minted loop_id parses canonical, 36 bytes) and
      `TestNonUUIDReplyToHTTPGetsSynchronousError` (inline error response naming `reply_to`, no task published),
      and `TestNonUUIDReplyToChannelGetsErrorResponse` (error published to the response subject — the channel
      path answers async via `sendResponse`; round-2 H3).
- [x] 2.4a §6.3 BLOCKING finding — the resolved-token check missed the two CLIENT-AUTHORED loop tokens.
      `TestNonUUIDRunIDHTTPGetsSynchronousError` and `TestNonUUIDInReplyToChannelGetsErrorResponse` each assert
      three things: the typed `ResponseTypeError` NAMES the field, no loop is tracked, and the started-loops
      gauge does not move. `TestCanonicalResumeAnchorsAreAccepted` is the positive control (the seam rejects the
      shape, not the gh#256 resume feature). Observed failing first, verbatim: HTTP =
      `"failed to create task. please try again." does not contain "run_id"` + `Should be empty, but was [...]` +
      gauge `expected: 0 / actual: 1`; channel = `"[]" should have 1 item(s), but has 0` (a bare return, no
      response at all). The loop-token test component moved to a per-test `metric.NewMetricsRegistry()` so the
      gauge is readable.
- [x] 2.5 `agentic/agentrun/agentrun_test.go`: `TestMint_NonUUIDRootLoopIDIsRefused` (classified invalid, before
      any store call); existing origin tests updated to canonical-UUID instances.
- [x] 2.6 `frameworkcapabilities/graphresearch`: `TestResearchLoopIDIsCanonicalUUID` — reads the AGENT_LOOPS
      key back; the generator option is deleted (round-2 H2), so nothing injects.
- [x] 2.7 Baseline on main, verbatim, filtered to build errors and `--- FAIL`.

## 3. Implementation

- [x] 3.1 `internal/looptoken`: `Valid(s string) bool` — `uuid.Parse` + canonical round-trip equality. Module-
      internal; no adopter surface. Doc comment carries the 32-bit/2^122 context pointer to ADR-105;
      `agentic/entity_ids.go` gains the reciprocal pointer (composition home ↔ form home; round-2 N2).
- [x] 3.2 `agentic/user_types.go:357` `TaskMessage.Validate`: any present non-canonical loop-token field —
      `loop_id`, `parent_loop_id`, `in_reply_to`, `run_id` — → error naming the field and the contract (round-2
      B2/H1: the gh#256 anchors are client-set and stamped raw at `loop_execution_entity.go:136-145`;
      `parent_loop_id` reaches the panicking builder at `:130`). Note the import (`internal/looptoken`) in the package's import-discipline comments
      where they exist (`agentic/agentrun/agentrun.go:16-17` gains the same note for its own check).
- [x] 3.3 `processor/agentic-loop/state.go:142` `CreateLoopWithID`: refuse via looptoken, `errs.WrapInvalid`,
      before any map write. `processor/agentic-dispatch/http.go:306` + `component.go:884`: mint
      `uuid.New().String()`; at both intake sites validate the RESOLVED
      continuation token after the auto-continue branch and before the mint — one check covers `reply_to` and
      auto-continue (round-2 M4); the HTTP path answers with an inline error response, the channel path via
      `sendResponse` on the response subject, each naming `reply_to`.
- [x] 3.3a §6.3 BLOCKING fix: `refuseNonCanonicalContinuation(loopID)` → `refuseNonCanonicalLoopTokens(msg,
      loopID)` (`processor/agentic-dispatch/component.go`), called from both submission paths BEFORE
      `loopTracker.Track` and `recordLoopStarted`. The resolved continuation token was only one of the three
      loop tokens a submission carries: `RunID` and `InReplyTo` are client-authored (`http.go`
      `HTTPMessageRequest`) and copied verbatim by `buildTaskMessage`, so they never passed through the
      continuation branch. A non-canonical value cleared the gate and failed one layer lower inside
      `BaseMessage.MarshalJSON` → `TaskMessage.Validate` — after the loop was tracked and counted: an orphaned
      `LoopInfo` and an incremented gauge for a loop that never exists, which auto-continue then resolves to.
      The error names the client's own field (`reply_to`, `run_id`, `in_reply_to`) and classifies as invalid,
      not retryable — the rule-engine lane's classification (`processor/rule/actions.go` `publishAgentOnce`
      validates before it publishes). `TaskMessage.Validate` stays as defense-in-depth for every other producer.
- [x] 3.4 `agentic/agentrun/agentrun.go`: refuse non-canonical `rootLoopID` (classified invalid, unexported
      sentinel per the #1148 pattern at `:233-239`) before `:290`; doc comment: the token contract + the
      origin-mismatch backstop; mismatch error text UNTOUCHED (#1174 stays its own issue — ruled 2026-08-31).
- [x] 3.5 `frameworkcapabilities/graphresearch/executor.go`: default generator → `uuid.NewString()`; DELETE
      `WithResearchGraphIDGenerator` (`executor.go:102-110`, zero production consumers — its only caller,
      `executor_test.go:68`, reads the KV key back instead) and `loopIDPrefix` (`:39`).
- [x] 3.6 Fixture/harness sweep by SEAM-CALLER ENUMERATION, not string patterns (round-2 B1/M2/M3):
      `git grep -n 'agentic.TaskMessage{' -- '*.go'` → four non-test sites — the two production builders
      (`agentic-dispatch/component.go:830`, `rule/actions.go:1713`) and the two e2e harness mints that would turn
      the BREAKING gate red: `test/e2e/scenarios/agentic/scenario.go:482` (`e2e-loop-%d`) and
      `test/e2e/scenarios/research-graph/scenario.go:380-382` (`e2e-parent-%d`) — both move to `uuid.NewString()`.
      `gopls references` on `CreateLoopWithID` and `TaskMessage.Validate` closes the rest:
      `trajectory_eviction_internal_test.go:39,57,96,204,223` (`failed-loop`, `cancelled-loop`, …),
      `scenario_test.go:74,86,114` fixtures, and the ops harness's direct-`PutKV` AGENT_LOOPS seeds
      (`test/e2e/scenarios/ops/scenario.go:365-385`, `seed-loop-001/2/3` — a seam-bypassing path the
      payload-registry spec recognizes; seeds become UUIDs so no in-tree state contradicts the contract).
      `research-graph/scenario.go:448` `rg_` prefix detection → shape-independent discriminator (the
      `research.request.received.<loopID>` trigger key or `LoopEntity.Role`).

## 4. Forced omissions — one per refusal; commit GREEN first; restore by `cp` + `shasum`; print `[applied]`

- [x] 4.1 Revert `http.go:306` to `"loop_" + uuid[:8]` → `TestNewConversationMintsCanonicalUUID` MUST fail.
- [x] 4.2 Revert `component.go:884` the same way → the channel-path case of the same test MUST fail.
- [x] 4.3 Delete the `loop_id` check in `TaskMessage.Validate` → `TestTaskMessageRefusesNonUUIDLoopID` AND
      `TestNonUUIDLoopIDIsTerminatedAtIntake` MUST fail.
- [x] 4.4 Delete the looptoken call in `CreateLoopWithID` → `TestCreateLoopWithIDRefusesNonUUIDToken` MUST fail.
- [x] 4.5 Delete Mint's check → `TestMint_NonUUIDRootLoopIDIsRefused` MUST fail.
- [x] 4.6 Delete the HTTP-path continuation check → `TestNonUUIDReplyToHTTPGetsSynchronousError` MUST fail.
- [x] 4.7 Delete the channel-path continuation check → `TestNonUUIDReplyToChannelGetsErrorResponse` MUST fail.
- [x] 4.8 Delete the sibling-field checks in `TaskMessage.Validate` →
      `TestTaskMessageRefusesNonCanonicalLoopTokenFields` MUST fail.
- [x] 4.9 Revert the graphresearch generator → `TestResearchLoopIDIsCanonicalUUID` MUST fail.
- [x] 4.10 Delete the `{"run_id", msg.RunID}` row from `refuseNonCanonicalLoopTokens` →
      `TestNonUUIDRunIDHTTPGetsSynchronousError` MUST fail. KILLED: all three assertions fired — content
      `"failed to create task. please try again." does not contain "run_id"`, tracker non-empty, gauge
      `expected: 0 / actual: 1`. Restored by `cp` + `shasum -a 256` (sums matched,
      `c46a9cd3041c1367e1b7d6a8726299ab6c16d68dbf5ba73dcd97517b894e4bb6`).
- [x] 4.11 Delete the `{"in_reply_to", msg.InReplyTo}` row →
      `TestNonUUIDInReplyToChannelGetsErrorResponse` MUST fail. KILLED: `"[]" should have 1 item(s), but has 0`
      — the channel path returns bare with no response. Restored by `cp` + `shasum -a 256` (sums matched, same
      digest).

## 5. Sweep

- [x] 5.1 Prose sweep. SWEPT FILE SET (the §6.3 MEDIUM finding: the first pass was scoped to `docs/`,
      `configs/`, `specs/`, which structurally excluded package READMEs and `.agents/`, so the tick overclaimed
      until this round) — `docs/advanced/08-agentic-components.md:458,515,528` (`loop_xyz789`),
      `docs/basics/07-agentic-quickstart.md`, `docs/concepts/13-agentic-systems.md`,
      `docs/operations/11-llm-routing-and-model-selection.md`, `configs/rules/research-graph/README.md:57` +
      `05-continuation.json:5` (`rg_…`), `agentic/doc.go`, `processor/agentic-loop/doc.go` (PARTIAL until
      round 4 — see 5.1b), `agentic/research/predicates.go:61`,
      `frameworkcapabilities/graphresearch/executor.go:34-38`,
      `processor/agentic-loop/trajectory_observability_test.go:215` (stated the retired overwrite expectation as
      live; round-2 M5); §6.3 additions — `agentic/README.md:93,114,117`,
      `processor/agentic-loop/README.md:192,234,243,258,285,336,348` (including
      `"loop_id": "optional-custom-id"`, which the contract now REFUSES — replaced, and the block gained the
      omit-or-echo rule), `.agents/skills/entity-or-bucket/SKILL.md:83` (`COMPLETE_rg_abc123`),
      `docs/concepts/15-payload-registry.md:28,391,408,421` (`abc123`/`abc`), and
      `docs/operations/17-tool-call-governance.md:75,76,101` (`abc-123`/`parent-uuid`, contradicting its own
      `$message.loop_id` = "the loop's bare UUID" table row at `:295`).
- [x] 5.1b §6.3 re-review HIGH (NEW-1) — the blind spot moved one level in rather than closing. Round 2's method
      excluded package READMEs; round 3's method (`-- '*.md'`) CANNOT SEE GO DOC COMMENTS, so
      `processor/agentic-loop/doc.go:89` survived while its README twin at `README.md:192` — the same
      `UserSignal` worked example — was fixed. HIGH not MEDIUM because `UserSignal` is NOT in the
      `validateLoopTokens` set: an adopter copying it is not refused, the signal just never matches a loop.
      Canonicalized `doc.go:89` (same UUID as its README twin, so the pair cannot drift again). Two more found
      by the widened method, one of them NOT on the reviewer's list:
      `test/e2e/scenarios/research-graph/scenario.go:830` (`COMPLETE_<rg_loopID>` → `COMPLETE_<loopID>`) and
      `processor/research-graph-synthesize/handler.go:48` — a PRODUCTION godoc describing the live read as
      `read_loop_result(loop_id=<rg_xxx>)`, now `<loopID>`. Non-test Go is empty under the widened method
      (verified after the edits).
- [x] 5.1a Sweep method and its residue, so the tick above is checkable. METHOD (round 4, widened from
      `-- '*.md'` after NEW-1 proved that scope cannot see Go doc comments):
      `git grep -noE '(loop|rg)_[A-Za-z0-9-]+' -- '*.md' '*.go'`, then filter and resolve **per TOKEN, not per
      line**, and check the residue against the vocabulary list. Two method traps, both hit and both fixed here:
      (i) a line-based `grep -v` exclusion DROPS a line that carries vocabulary AND a survivor — that is exactly
      what hid `research-graph-synthesize/handler.go:48` (`read_loop_result` + `loop_id` on the same line as
      `<rg_xxx>`), so the filter runs on the extracted token via `-o`; (ii) a clean result is a broken filter
      until proven otherwise — the pattern is sanity-checked against a known-positive first
      (`git grep -cE … -- processor/agentic-loop/doc.go` = 4, cross-checked against `grep -cE` = 4).
      Remaining hits are DELIBERATE, not survivors: this change's own artifacts (`spec.md:42,52`
      use `loop_ab12cd34` as the REFUSED example; `proposal.md`/`inventory.md` describe the retired shapes);
      `docs/operations/migration-beta162-to-beta163.md:880` (reports the shape as it exists in a SISTER repo —
      rewriting it would falsify the inventory); `docs/operations/migration-beta41-to-beta42.md:54` and
      `docs/adr/048-*.md:337` (frozen history: the shape WAS the truth at those versions);
      `configs/rules/example-fan-out/README.md:56` (`investigator_loop_A/B/C` is a prose label in a flow
      diagram, not a token literal); `openspec/changes/archive/…/conformance.md:649` (`platform_absent`) and
      `migration-beta162-to-beta163.md:881` (`org_1`), both grep false positives; and `loop_xxxxxxxx` /
      `rg_xxxxxxxx` in `docs/adr/105-*.md:44,45` and `migration-beta162-to-beta163.md:855,896`, which NAME the
      retired shapes and must keep saying so.
- [x] 5.1c `*_test.go` residue is governed by task 3.6's ruled method — seam-caller enumeration, NOT string
      patterns — and is left deliberately. Two families: (a) REFUSED-INPUT fixtures that must keep the retired
      shape or they stop testing the refusal — `loop_ab12cd34` / `rg_ab12cd34` in `agentrun_test.go`,
      `user_types_test.go`, `state_test.go`, `loop_token_test.go`; (b) opaque KV keys and pass-through values
      that never reach a validating seam — `http_activity_test.go` (`loop_x/bad/ok/k`, watcher poison/decode
      paths), `intent_classifier_test.go` (`loop_abc`, `loop_abc12345`, `loop_xyz` — LLM output parsing),
      `http_loops_test.go` (`loop_abc123`), `graphresearch/executor_test.go` (`parent_loop_42`,
      `parent_loop_7` — descriptive names, not mint-shape mimics), `research-graph-classify` /
      `research-graph-llmwrap` (`rg_test001`, `rg_x` — trigger-key subjects), `agentic-model`,
      `processor/rule/triple_*_substitution_test.go` (`loop_a/b/c`), and `entity_ids_test.go`
      (`loop_42` — the deliberate "loop ID with underscores" grammar case for the form-AGNOSTIC composition
      function). The mechanical proof that (b) reaches no seam: every seam refuses this exact shape and the full
      suite is GREEN — a fixture that traversed one would fail.
- [x] 5.2 `task schema:generate`; `git diff --exit-code schemas/ specs/` — no wire-shape fields change; any drift
      is a finding to explain, not commit blindly.
- [x] 5.3 Migration-note append (`docs/operations/migration-beta162-to-beta163.md`) from this change's
      `migration-note.md`; pin the sister SHAs read in the bounded pass.
- [x] 5.3a RULED (owner, 2026-09-01, on the reviewer's §6.3 MEDIUM #3): the cross-deployment peer-import case is
      IN SCOPE for #1192 and lands as one migration-note line — not a follow-up issue, not an unstated
      assumption. Added to the §"Doing nothing" bullets of
      `docs/operations/migration-beta162-to-beta163.md` and to this change's `migration-note.md`: a peer that has
      not adopted ADR-105 still mints `loop_xxxxxxxx`, an imported loop is refused by `task.Validate()`, and
      `publish_agent` publishes nothing for it — loudly (`Failed to execute action` at ERROR plus
      `actionFailuresTotal{action_type="publish_agent"}`, `stateful_evaluator.go:439-449`), so upgrade peers
      before importing. No code change. NOTE: the ruling cited `actions.go:1886`; the `task.Validate()` call is
      at `actions.go:1885` (`:1886` is the `errs.WrapInvalid` return), and the note cites the measured line.

## 6. Gates and landing

- [x] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `openspec validate loop-token-uuid-enforcement --strict`. Push only green.
- [x] 6.2 **BREAKING gate:** `task e2e:agentic` AND `task e2e:research-graph` green on the branch before the
      breaking commit lands. Results verbatim. DONE 2026-08-31 on this branch rebased onto `0681492e`:
      `e2e:agentic` 1 "Scenario completed successfully" / 0 failures, duration=45.130s,
      `graph_loop_triples:10 graph_model_triples:6 verify-graph-triples_duration_ms:4`;
      `e2e:research-graph` 2 successes (direct + execute) / 0 failures, 1.047s and 1.039s.
      Gate was blocked until `0681492e` (#1221 / #1217) restored both tiers on main — that blocker was NOT this
      change; the tiers failed identically on main and on this branch. NOTE: `assertions_run` is structurally 0
      for both tiers (#1222), so it is not a measured-anything signal; the per-stage metrics above are.
- [x] 6.3 Implementation review by `semstreams-reviewer`; dispositions recorded. Round applied on this branch —
      dispositions: (a) BLOCKING "the dispatch refusal covers only the resolved continuation token" — ACCEPTED
      and FIXED, tasks 2.4a / 3.3a / 4.10 / 4.11 above; (b) MEDIUM "the doc sweep missed the package READMEs and
      5.1 overclaims" — ACCEPTED and FIXED, task 5.1 rewritten to name the file set actually swept plus 5.1a
      naming the method and the deliberate residue; (c) MEDIUM #3 cross-deployment peer import — RULED IN SCOPE
      by the owner 2026-09-01 and landed as the one migration-note line, task 5.3a; (d) re-review HIGH (NEW-1)
      `processor/agentic-loop/doc.go:89` — ACCEPTED and FIXED, task 5.1b, together with two further survivors the
      widened method found, one of them NOT on the reviewer's list
      (`research-graph-synthesize/handler.go:48`).
      SQUASH-BODY STATUS, corrected: the round-3 answer recorded here ("no correction owed, the claim is true at
      the tip") was WRONG as measured at `9d1d79fd` — `doc.go:89` is precisely a package doc carrying the shape
      commit `95205b65`'s subject claims to have retired, so the "package docs" half was FALSE and 5.1's tick
      listing that file was an overclaim. As of 5.1b it IS true: non-test Go carries zero survivors under the
      widened method, and the "opaque fixtures" half was independently measured clean (no token literals under
      `configs/`, `*.json`, `*.yaml`). The subject therefore needs no qualifying language in the squash body —
      but it became true in THIS round, not the previous one.
      RE-REVIEW ROUND COMPLETE. `semstreams-reviewer` re-reviewed `bc311c38`..`9d1d79fd`: (a) CLOSED — it
      independently derived the hole class and could not construct a client-authored non-canonical token
      reaching a published `TaskMessage`, a tracker entry, a signal subject, or any graph/KV write, and it
      supplied the step the developer's argument had skipped — `loop_tracker.go:153` `t.loops[info.LoopID] =
      info` is the ONLY map insertion, which is what makes "three `Track` call sites" a complete enumeration;
      (b) narrowed to NEW-1, since fixed; (c) CLOSED; forced-omission evidence ACCEPTED as field-discriminating
      and placement-proving; per-test `metric.NewMetricsRegistry()` under `t.Parallel()` ACCEPTED as sound.
      Round 4 (`4ef355d5`) delta verified by the owner session rather than a further reviewer round: the diff is
      comment-only in `*.go` (every changed line a comment) plus this file, and an independent PER-TOKEN sweep
      of non-test Go — vocabulary filtered by token, not by line, which is the defect that hid
      `handler.go:48` — returns zero example instance tokens. A further full reviewer round over comment-only
      changes was judged low-yield; that judgment is the session's, not a reviewer sign-off.
      Two findings recorded rather than fixed here: the non-token `Validate` silent drop is FILED as #1225
      (pre-existing; net new reachability from this change is zero), and `canUserControlLoop`
      (`component.go:1204-1210`) short-circuits true for a `cancel_any` user with no tracker hit, so the
      nil-check at `commands.go:86-97` is the load-bearing guard and no test names it — owner's call whether
      that is filed.
- [x] 6.3a Codex cross-agent round — CHANGES REQUESTED, owner ruled all of it on #1192 (2026-09-01 ruling
      comment). The archive was RE-ENTERED rather than appended to: `git revert --no-commit` of the archive
      commit, corrections applied to the CHANGE artifacts, then `openspec archive` re-run as the final content
      commit — so history reads archive → revert+corrections → archive and no post-archive content escapes
      review. Dispositions:
      (e) **B1 form vs. provenance — NARROW THE TEXT, KEEP THE CODE.** `internal/looptoken.Valid` is a FORM
      predicate; a client-authored fresh canonical UUID is accepted, as `TestCanonicalReplyToContinuesTheLoop`
      positively asserts. NOT a regression — on base `0681492e` dispatch took `msg.ReplyTo` with no validation
      at all — the text simply overclaimed. The delta now states enforcement is form, not provenance, carries the
      #1227 carve-out, and gained a scenario pinning the accepted-on-form-alone behavior so the limitation is
      tested rather than merely admitted. The `run_id`/`in_reply_to` widening from §6.3(a) STANDS untouched;
      what was withdrawn is only the implication that the seam detects authorship.
      (f) **B2 "every accepting seam" — NARROW TO THE FOUR ENFORCED.** Re-verified independently, not taken from
      the review: `git grep -n 'looptoken\.Valid' -- '*.go' ':!*_test.go'` returns exactly four production
      callers — `agentic/user_types.go:423` (`TaskMessage.Validate`),
      `processor/agentic-dispatch/component.go:894`, `processor/agentic-loop/state.go:152`
      (`CreateLoopWithID`), `agentic/agentrun/agentrun.go:302` (`Mint`) — and zero test callers. Confirmed the
      uncovered carriers by reading their validators: `UserSignal.Validate` (`agentic/user_types.go:124`) and
      `ApprovalResponse.Validate` (`agentic/approval.go:123`) check non-emptiness only. Remaining census is
      **#1228**; the spec now says so instead of asserting every seam.
      (g) **ADR-105 Proposed → Accepted** with ruling provenance (#1192 comments `5481478395`, `5481998272`,
      `5494522256`, plus the 2026-09-01 ruling), and the no-re-key rationale's "the framework mints every token"
      premise annotated as NOT enforced — the enforced backstop is `agentrun.Mint`'s origin-entity-ID mismatch
      refusal at `agentic/agentrun/agentrun.go:332` (the #1148 check), verified in source.
      (h) **#1227 carve-out, owner-ordered and prominent** — its own section directly under ADR-105's Status, a
      blockquote at the head of the migration-note section, and a paragraph in the spec requirement. Multi-user
      is a SUPPORTED pre-v1 configuration, so multi-tenant deployments MUST NOT rely on loop tokens for
      isolation until #1227 lands. The stale "Design stage (2026-08-31) … Amend to what ships" block is deleted.
      (i) **MEDIUM v4 assertion.** `looptoken.Valid` ignores version bits by design, so the mint sites are the
      only correct home. Asserted in the shared dispatch helper `requireCanonicalUUID` (all three call sites pass
      framework-MINTED tokens, checked) and in `TestResearchLoopIDIsCanonicalUUID`. Mutation-checked below.
- [x] 6.3b Forced omission for (i), since a new assertion that cannot fail is worthless: replace each mint with a
      canonical NON-v4 UUID (`uuid.NewSHA1`, a v5 in canonical form — it passes every pre-existing length, parse,
      round-trip, and prefix check) → the v4 assertions MUST be what fails. KILLED, and field-discriminating:
      the mutants really were canonical (`1826d139-bc21-567d-bb65-ecb8773794c7`,
      `dc3ea0b2-6e4a-53df-86c7-f4a8eb764549`, `b2f8672f-ff0b-5c8a-976e-4e6004278559`,
      `2359767d-8f7a-5e52-8ff3-6212e89dc645` — note the `5` at position 15), so length, parse, canonical
      round-trip and no-prefix all PASSED and only the version assertion fired:
      `TestNewConversationMintsCanonicalUUID` failed on both subtests,
      `TestCanonicalResumeAnchorsAreAccepted` on the minted token, and
      `TestResearchLoopIDIsCanonicalUUID` with `minted loop ID … is version 5, want a version 4 UUID`.
      Restored by `cp` + `shasum -a 256`; all three digests matched
      (`c46a9cd3…4bb6`, `9c740da9…f9a0`, `b89c38d0…1c8a`).
- [ ] 6.4 Archive + spec sync as the LAST content commit, reviewed with the code.
- [ ] 6.5 Undraft; PR body: `implemented-by:`, `Closes #1192` (the #1174 declaration is dropped — ruled 2026-08-31), before/after token
      shapes; if
      any round withdrew a claim a commit asserted, author the squash body via `--body-file`.
