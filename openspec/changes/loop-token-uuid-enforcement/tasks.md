# Tasks: loop-token UUID enforcement

Claim: draft PR #1210, branch `claude/gh1192-framed-digest-run-identity`, base `ae35f296`. The framed-digest
package this replaces survives in PR history at `b0e92253`. Deviations from the 2026-08-31 ruling are BLOCKING.

## 1. Gates before implementation

- [ ] 1.1 Owner confirmation at design review of the corrected mint census: graph-research's `rg_` mint is IN
      scope (recommendation A1) or carved out (A2). Implement as confirmed; do not adapt silently.
- [ ] 1.2 Owner word on the #1174 recommendation (drop `Closes #1174`); edit the PR body accordingly.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `agentic/user_types_test.go`: `TestTaskMessageRefusesNonUUIDLoopID` (present + non-canonical → error
      naming `loop_id`; empty → valid; canonical → valid; uppercase/braced/urn forms → refused).
- [ ] 2.2 `processor/agentic-loop/state_test.go`: `TestCreateLoopWithIDRefusesNonUUIDToken` (classified invalid,
      no loop registered, no context manager created).
- [ ] 2.3 `processor/agentic-loop/component` intake test: `TestNonUUIDLoopIDIsTerminatedAtIntake` — decoded task
      with `loop_id: "workflow-7"` → intake-rejection metric increments, delivery terminated, no loop exists.
- [ ] 2.4 `processor/agentic-dispatch`: `TestNewConversationMintsCanonicalUUID` (BOTH intake paths — HTTP submit
      and channel message — minted loop_id parses canonical, 36 bytes) and
      `TestNonUUIDReplyToGetsSynchronousError` (response type error, content names `reply_to`, no task published).
- [ ] 2.5 `agentic/agentrun/agentrun_test.go`: `TestMint_NonUUIDRootLoopIDIsRefused` (classified invalid, before
      any store call); existing origin tests updated to canonical-UUID instances.
- [ ] 2.6 `frameworkcapabilities/graphresearch`: `TestResearchLoopIDIsCanonicalUUID` (default generator) and
      `TestInjectedGeneratorOutputIsValidated` (a `WithResearchGraphIDGenerator` returning `rg_x` → ToolResult
      error, nothing written to KV). [Cut if 1.1 resolves to A2.]
- [ ] 2.7 Baseline on main, verbatim, filtered to build errors and `--- FAIL`.

## 3. Implementation

- [ ] 3.1 `internal/looptoken`: `Valid(s string) bool` — `uuid.Parse` + canonical round-trip equality. Module-
      internal; no adopter surface. Doc comment carries the 32-bit/2^122 context pointer to ADR-105.
- [ ] 3.2 `agentic/user_types.go:357` `TaskMessage.Validate`: present non-canonical `loop_id` → error naming the
      field and the contract. Note the import (`internal/looptoken`) in the package's import-discipline comments
      where they exist (`agentic/agentrun/agentrun.go:16-17` gains the same note for its own check).
- [ ] 3.3 `processor/agentic-loop/state.go:142` `CreateLoopWithID`: refuse via looptoken, `errs.WrapInvalid`,
      before any map write. `processor/agentic-dispatch/http.go:306` + `component.go:884`: mint
      `uuid.New().String()`; `http.go:298` + `component.go:876`: non-canonical `reply_to` → synchronous
      `ResponseTypeError` naming `reply_to` (before loopTracker/publish).
- [ ] 3.4 `agentic/agentrun/agentrun.go`: refuse non-canonical `rootLoopID` (classified invalid, unexported
      sentinel per the #1148 pattern at `:233-239`) before `:290`; doc comment: the token contract + the
      origin-mismatch backstop; mismatch error text UNTOUCHED (#1174 stays its own issue unless owner says else).
- [ ] 3.5 `frameworkcapabilities/graphresearch/executor.go`: default generator → `uuid.NewString()`; delete
      `loopIDPrefix` (`:39`); validate `e.newLoopID()` output at `:251` → ToolErrorInternal on violation.
      [Cut if A2; then the spec delta gains the carve-out sentence instead — owner text required.]
- [ ] 3.6 In-tree fixture sweep: `test/e2e/scenarios/agentic/scenario_test.go:74,86,114` `loop-1` → UUIDs;
      `test/e2e/scenarios/research-graph/scenario.go:448` `rg_` prefix detection → shape-independent
      discriminator (the `research.request.received.<loopID>` trigger key or `LoopEntity.Role`); unit/integration
      tests using non-UUID loop tokens (`git grep -nE '"(test-)?loop[-_]' -- '*_test.go'` and fix every hit that
      feeds a mint seam).

## 4. Forced omissions — one per refusal; commit GREEN first; restore by `cp` + `shasum`; print `[applied]`

- [ ] 4.1 Revert `http.go:306` to `"loop_" + uuid[:8]` → `TestNewConversationMintsCanonicalUUID` MUST fail.
- [ ] 4.2 Revert `component.go:884` the same way → the channel-path case of the same test MUST fail.
- [ ] 4.3 Delete the `loop_id` check in `TaskMessage.Validate` → `TestNonUUIDLoopIDIsTerminatedAtIntake` MUST fail.
- [ ] 4.4 Delete the looptoken call in `CreateLoopWithID` → `TestCreateLoopWithIDRefusesNonUUIDToken` MUST fail.
- [ ] 4.5 Delete Mint's check → `TestMint_NonUUIDRootLoopIDIsRefused` MUST fail.
- [ ] 4.6 Delete the `reply_to` check (both sites) → `TestNonUUIDReplyToGetsSynchronousError` MUST fail.
- [ ] 4.7 Revert the graphresearch generator AND its use-site validation → both §2.6 tests MUST fail. [A1 only]

## 5. Sweep

- [ ] 5.1 Prose: `docs/advanced/08-agentic-components.md:458,515,528` (`loop_xyz789`),
      `configs/rules/research-graph/README.md:57` + `05-continuation.json:5` descriptions (`rg_…`),
      `agentic/research/predicates.go:61`, `executor.go:34-38` comment; `git grep -n '"loop_\|rg_'` over docs/,
      configs/, specs/ and fix every shape survivor.
- [ ] 5.2 `task schema:generate`; `git diff --exit-code schemas/ specs/` — no wire-shape fields change; any drift
      is a finding to explain, not commit blindly.
- [ ] 5.3 Migration-note append (`docs/operations/migration-beta162-to-beta163.md`) from this change's
      `migration-note.md`; pin the sister SHAs read in the bounded pass.

## 6. Gates and landing

- [ ] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `openspec validate loop-token-uuid-enforcement --strict`. Push only green.
- [ ] 6.2 **BREAKING gate:** `task e2e:agentic` AND `task e2e:research-graph` green on the branch before the
      breaking commit lands (research tier drops only under A2). Results verbatim.
- [ ] 6.3 Implementation review by `semstreams-reviewer`; dispositions recorded.
- [ ] 6.4 Archive + spec sync as the LAST content commit, reviewed with the code.
- [ ] 6.5 Undraft; PR body: `implemented-by:`, `Closes #1192` (+/- #1174 per 1.2), before/after token shapes; if
      any round withdrew a claim a commit asserted, author the squash body via `--body-file`.
