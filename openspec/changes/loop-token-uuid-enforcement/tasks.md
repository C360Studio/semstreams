# Tasks: loop-token UUID enforcement

Claim: draft PR #1210, branch `claude/gh1192-framed-digest-run-identity`, base `ae35f296`. The framed-digest
package this replaces survives in PR history at `b0e92253`. Deviations from the 2026-08-31 ruling are BLOCKING.

## 1. Gates before implementation

- [x] 1.1 RULED (owner, 2026-08-31, chat, transcribed on #1192): "q1 - everyone who mints a loop uses uuid" —
      A1 confirmed, graph-research IN scope. A2 conditionals removed from this file.
- [x] 1.2 RULED (owner, 2026-08-31, chat, transcribed on #1192): "q2 drop it unless we are fixing it" — this
      scope does not fix it; `Closes #1174` dropped from the PR #1210 body. #1174 stays open on its own.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `agentic/user_types_test.go`: `TestTaskMessageRefusesNonUUIDLoopID` (present + non-canonical → error
      naming `loop_id`; empty → valid; canonical → valid; uppercase/braced/urn forms → refused) and
      `TestTaskMessageRefusesNonCanonicalLoopTokenFields` (`parent_loop_id`, `in_reply_to`, `run_id` — each
      refused naming its field; round-2 B2/H1).
- [ ] 2.2 `processor/agentic-loop/state_test.go`: `TestCreateLoopWithIDRefusesNonUUIDToken` (classified invalid,
      no loop registered, no context manager created).
- [ ] 2.3 `processor/agentic-loop/component` intake test: `TestNonUUIDLoopIDIsTerminatedAtIntake` — decoded task
      with `loop_id: "workflow-7"` → intake-rejection metric increments, delivery terminated, no loop exists.
- [ ] 2.4 `processor/agentic-dispatch`: `TestNewConversationMintsCanonicalUUID` (BOTH intake paths — HTTP submit
      and channel message — minted loop_id parses canonical, 36 bytes) and
      `TestNonUUIDReplyToHTTPGetsSynchronousError` (inline error response naming `reply_to`, no task published),
      and `TestNonUUIDReplyToChannelGetsErrorResponse` (error published to the response subject — the channel
      path answers async via `sendResponse`; round-2 H3).
- [ ] 2.5 `agentic/agentrun/agentrun_test.go`: `TestMint_NonUUIDRootLoopIDIsRefused` (classified invalid, before
      any store call); existing origin tests updated to canonical-UUID instances.
- [ ] 2.6 `frameworkcapabilities/graphresearch`: `TestResearchLoopIDIsCanonicalUUID` — reads the AGENT_LOOPS
      key back; the generator option is deleted (round-2 H2), so nothing injects.
- [ ] 2.7 Baseline on main, verbatim, filtered to build errors and `--- FAIL`.

## 3. Implementation

- [ ] 3.1 `internal/looptoken`: `Valid(s string) bool` — `uuid.Parse` + canonical round-trip equality. Module-
      internal; no adopter surface. Doc comment carries the 32-bit/2^122 context pointer to ADR-105;
      `agentic/entity_ids.go` gains the reciprocal pointer (composition home ↔ form home; round-2 N2).
- [ ] 3.2 `agentic/user_types.go:357` `TaskMessage.Validate`: any present non-canonical loop-token field —
      `loop_id`, `parent_loop_id`, `in_reply_to`, `run_id` — → error naming the field and the contract (round-2
      B2/H1: the gh#256 anchors are client-set and stamped raw at `loop_execution_entity.go:136-145`;
      `parent_loop_id` reaches the panicking builder at `:130`). Note the import (`internal/looptoken`) in the package's import-discipline comments
      where they exist (`agentic/agentrun/agentrun.go:16-17` gains the same note for its own check).
- [ ] 3.3 `processor/agentic-loop/state.go:142` `CreateLoopWithID`: refuse via looptoken, `errs.WrapInvalid`,
      before any map write. `processor/agentic-dispatch/http.go:306` + `component.go:884`: mint
      `uuid.New().String()`; at both intake sites validate the RESOLVED
      continuation token after the auto-continue branch and before the mint — one check covers `reply_to` and
      auto-continue (round-2 M4); the HTTP path answers with an inline error response, the channel path via
      `sendResponse` on the response subject, each naming `reply_to`.
- [ ] 3.4 `agentic/agentrun/agentrun.go`: refuse non-canonical `rootLoopID` (classified invalid, unexported
      sentinel per the #1148 pattern at `:233-239`) before `:290`; doc comment: the token contract + the
      origin-mismatch backstop; mismatch error text UNTOUCHED (#1174 stays its own issue unless owner says else).
- [ ] 3.5 `frameworkcapabilities/graphresearch/executor.go`: default generator → `uuid.NewString()`; DELETE
      `WithResearchGraphIDGenerator` (`executor.go:102-110`, zero consumers — its only caller,
      `executor_test.go:68`, reads the KV key back instead) and `loopIDPrefix` (`:39`).
- [ ] 3.6 Fixture/harness sweep by SEAM-CALLER ENUMERATION, not string patterns (round-2 B1/M2/M3):
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

- [ ] 4.1 Revert `http.go:306` to `"loop_" + uuid[:8]` → `TestNewConversationMintsCanonicalUUID` MUST fail.
- [ ] 4.2 Revert `component.go:884` the same way → the channel-path case of the same test MUST fail.
- [ ] 4.3 Delete the `loop_id` check in `TaskMessage.Validate` → `TestNonUUIDLoopIDIsTerminatedAtIntake` MUST fail.
- [ ] 4.4 Delete the looptoken call in `CreateLoopWithID` → `TestCreateLoopWithIDRefusesNonUUIDToken` MUST fail.
- [ ] 4.5 Delete Mint's check → `TestMint_NonUUIDRootLoopIDIsRefused` MUST fail.
- [ ] 4.6 Delete the HTTP-path continuation check → `TestNonUUIDReplyToHTTPGetsSynchronousError` MUST fail.
- [ ] 4.7 Delete the channel-path continuation check → `TestNonUUIDReplyToChannelGetsErrorResponse` MUST fail.
- [ ] 4.8 Delete the sibling-field checks in `TaskMessage.Validate` →
      `TestTaskMessageRefusesNonCanonicalLoopTokenFields` MUST fail.
- [ ] 4.9 Revert the graphresearch generator → `TestResearchLoopIDIsCanonicalUUID` MUST fail.

## 5. Sweep

- [ ] 5.1 Prose: `docs/advanced/08-agentic-components.md:458,515,528` (`loop_xyz789`),
      `configs/rules/research-graph/README.md:57` + `05-continuation.json:5` descriptions (`rg_…`),
      `agentic/research/predicates.go:61`, `executor.go:34-38` comment;
      `processor/agentic-loop/trajectory_observability_test.go:215` (states the retired overwrite expectation as
      live; round-2 M5); `git grep -n '"loop_\|rg_'` over docs/,
      configs/, specs/ and fix every shape survivor.
- [ ] 5.2 `task schema:generate`; `git diff --exit-code schemas/ specs/` — no wire-shape fields change; any drift
      is a finding to explain, not commit blindly.
- [ ] 5.3 Migration-note append (`docs/operations/migration-beta162-to-beta163.md`) from this change's
      `migration-note.md`; pin the sister SHAs read in the bounded pass.

## 6. Gates and landing

- [ ] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `openspec validate loop-token-uuid-enforcement --strict`. Push only green.
- [ ] 6.2 **BREAKING gate:** `task e2e:agentic` AND `task e2e:research-graph` green on the branch before the
      breaking commit lands. Results verbatim.
- [ ] 6.3 Implementation review by `semstreams-reviewer`; dispositions recorded.
- [ ] 6.4 Archive + spec sync as the LAST content commit, reviewed with the code.
- [ ] 6.5 Undraft; PR body: `implemented-by:`, `Closes #1192` (the #1174 declaration is dropped — ruled 2026-08-31), before/after token
      shapes; if
      any round withdrew a claim a commit asserted, author the squash body via `--body-file`.
