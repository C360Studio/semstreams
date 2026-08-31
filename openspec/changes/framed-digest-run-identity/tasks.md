# Tasks: framed-digest run identity (Case A)

Claim: draft PR #1210, branch `claude/gh1192-framed-digest-run-identity`, worktree
`../semstreams-wt/claude/gh1192-framed-digest-run-identity`, base `ae35f296`. Every task cites its salvage source;
deviations from a binding ruling are BLOCKING at any severity.

## 1. Gates before implementation

- [ ] 1.1 Owner rulings recorded on #1192 for the four docket items (O-1 RunID meaning; the `ResolveRun` capability
      loss; `DerivedEntityID` truncation; the family-table/budget tightening). Implement as ruled or escalate —
      never adapt silently.
- [ ] 1.2 Sequencing vs Codex settled on #1192: #1146/#1155 (PRs #1159/#1156) either landed and this branch rebased
      over them, or explicitly ordered behind this change by the owner. #1154 confirmed sequenced after.
- [ ] 1.3 Owner design review of the new exported `pkg/types` surface (`AgentRunIdentityFamily`,
      `FrameworkIdentityFamily.DerivedEntityID`) — required for new exported surface on `pkg/*`.

## 2. Baseline capture — write the named tests first (salvage: pre-cut §2 Case A rows + revision §1.I)

- [ ] 2.1 `pkg/types/framework_identity_families_test.go`: `TestAgentRunFamilyBindsTheAuthorityPairBudget` (168;
      `TestMaxAuthorityPairBytesDerivesFromLongestFamily` self-updates), `TestDerivedEntityIDIsFramedSHA256OverTheOrigin`
      (two origins sharing an instance → two IDs; frame order and 8-byte big-endian length prefix pinned against a
      hand-computed vector), `TestDerivedEntityIDRefusesEmptyOrigin`,
      `TestDerivedEntityIDTruncatesToFamilyInstanceBytes` (a 16-byte family), and the 0/>64 refusal (N5). Does not
      compile at baseline.
- [ ] 2.2 `agentic/agentrun/agentrun_test.go`: `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns`,
      `TestMint_RefusesEmptyOrigin` (kept), `TestMint_StoredOriginMismatchIsRefusedWithoutNamingIt` (error text
      contains neither identity — #1174), `TestMint_IsIdempotentForOneOrigin`; integration test drops `RunID()`.
      The three retired names (`…AreRefusedNotAliased`, `…LegacyOriginlessStoredRunIsRefused`,
      `TestMint_DotInLoopIDReturnsError`) are deleted, not renamed.
- [ ] 2.3 `agentic/run_id_test.go`: `TestTaskMessageRequiresRunEntityIDWithRunID`,
      `TestLoopEntityRunEntityIDRoundTrip`, `TestUserMessageRunEntityIDRoundTrip` through the production decoder.
- [ ] 2.4 `processor/agentic-loop/handlers_test.go`: `TestRunEntityIDIsCarriedOnEveryLoopSurface` — created/
      completed/failed/cancelled events and `ToolCall.Metadata["agent.run_entity_id"]` equal `task.RunEntityID`
      verbatim; no `resolveRunEntityID` remains (compile).
- [ ] 2.5 `processor/rule/actions_run_scope_integration_test.go` (integration): the three existing run-scope tests
      updated to the digest form; `TestRunScopeInheritCarriesRunEntityID` (inherit path copies
      `agent.run.entity-id`).
- [ ] 2.6 `internal/entityidaudit/audit_test.go`: `TestAuditFlagsDerivedFamilyComposedOutsideItsHome` (a `Sprintf`
      with `chain.agent.execution` in production Go → finding; the declaration pattern
      `*.*.chain.agent.execution.*` → no finding; the family file itself → no finding).
- [ ] 2.7 `config/config_test.go` + entity-id-contract scenario tests: budget numbers move —
      `TestConfigRejectsOversizedAuthorityPair` (162), `TestConfigRejectsPairThatOnlyFitsUnsuffixed` (163),
      `TestMaximumDeclarablePairMintsAndStarts` / `TestEffectivePairIsBoundedWithoutTheDeclarationReserve`
      (161 declared / 168 effective).
- [ ] 2.8 Baseline capture on main, verbatim, filtered to build errors and `--- FAIL` lines.

## 3. Contract — `pkg/types`, `agentrun`, wire, rule engine (salvage: pre-cut §3.1–3.5 + §1.I amendments)

- [ ] 3.1 `pkg/types/framework_identity_families.go`: add `{Name: "agent-run", System: "chain", Domain: "agent",
      Type: "execution", InstanceBytes: 64}`, `AgentRunIdentityFamily()`, and
      `(f FrameworkIdentityFamily) DerivedEntityID(org, platform, digestDomain string, frames ...string) (string, error)`
      — length-framed SHA-256 (frames byte-identical to `writeFramedString`), refuses an empty frame, truncates the
      hex digest to `f.InstanceBytes`, refuses `InstanceBytes` 0 or >64 (N5). Update the `MaxAuthorityPairBytes`
      doc comment (168; agent-run binds). Update the stale family comments at
      `framework_identity_families.go:22-24` (agent-run becomes a fixed-suffix chain-execution family) and `:36`
      (rule-trigger stops being the longest fixed suffix) (round-1 N1). `DerivedEntityID` produces its final
      identifier through `f.EntityID(org, platform, instance)` — the established `ruleTriggerEntityID` shape
      (`processor/rule/graph_event_identity.go:33`) — so the family's fail-closed segment validation applies to
      every derived identity (round-1 N2). Do NOT touch `DeploymentPrefix` (#1187); the
      `entity_domain_authority.go` rename stays #1171's own work (round-1 B1).
- [ ] 3.2 `agentic/agentrun/agentrun.go`: `Mint(ctx, mgr, org, platform, originEntityID string)` mints through
      `AgentRunIdentityFamily().DerivedEntityID(org, platform, "semstreams.agent.run.v1", originEntityID)`; the
      stored-origin check keeps its classification and names neither identity; rewrite the Mint doc comment (the
      "is #1168" pointer and the instance-token description are stale). Delete `RunID()`, `runIDFromChainEntityID`,
      `ResolveRun`, `LoopTripleReader`, `maxAncestryHops`, `nats_reader.go`. Milestone subscriber keeps the wire
      fast path only: `NewMilestoneSubscriber(mgr, logger)`, `NewMilestoneSubscriberWithRunStateReader(runs, logger)`;
      `resolveRunForEvent` treats an event without `RunEntityID` as "not in a run" (a defined answer under the
      both-or-neither rule, not a degrade) and makes its KV Get-error path a declared degrade — logged, never
      conflated with "not in a run" (#1197; round-1 D2). The retained stored-origin comparison is documented as
      defense in depth: with the digest a mismatch is structurally unreachable absent a hash collision (round-1 N3).
- [ ] 3.3 `agentic/entity_ids.go`: delete `ChainExecutionEntityID` and `TryChainExecutionEntityID`.
      `agentic/tools.go:396-400`: delete the recompute instruction from `MetadataKeyRunID`'s doc.
- [ ] 3.4 Wire: add `TaskMessage.RunEntityID`, `LoopEntity.RunEntityID`, `UserMessage.RunEntityID`
      (`json:"run_entity_id,omitempty"`); `TaskMessage.Validate` requires both-or-neither;
      `LoopManager.SetRunEntityID/GetRunEntityID`; `LoopExecutionEntity.Triples` stamps `agent.run.entity-id` from
      `Task.RunEntityID` verbatim (deletes the wire-authority derivation — the run half of #1154);
      `processor/agentic-loop/handlers.go` deletes `resolveRunEntityID` and reads `entity.RunEntityID` at its five
      sites; `processor/agentic-dispatch/loop_wire.go` reads `e.RunEntityID`; `http.go`'s submit request gains
      `run_entity_id`. Dispatch rejects ONLY a submission carrying `RunID` without `RunEntityID`, naming the field
      (§1.I rewrite — no loop-record fallback resolution). Rewrite the gh#256 contract comment
      (`agentic/user_types.go:42-51`): `run_entity_id` is the echo token.
- [ ] 3.5 `processor/rule/actions.go`: `run_scope=new` sets `task.RunEntityID` from the run `Mint` returned;
      `stampRunAnchors` takes the run entity ID instead of recomputing (`agent.loop.run` still carries the bare
      loop id); inherit path copies `agent.run.entity-id`; `graph_event_identity.go` composes through
      `DerivedEntityID` and deletes `writeRuleTriggerFrame` — digest bytes unchanged, pinned by the existing
      trigger identity tests.
- [ ] 3.6 `internal/entityidaudit`: the `derived_family_composed` rule per the entity-id-contract delta.

## 4. Forced omissions — one per new guard (commit GREEN first; restore by `cp` + `shasum`; print `[applied]`)

- [ ] 4.1 M1 `DerivedEntityID`: hash the instance segment instead of the full origin →
      `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns` MUST fail.
- [ ] 4.2 M2 `Mint`: delete the stored-origin comparison → `TestMint_StoredOriginMismatchIsRefusedWithoutNamingIt`
      MUST fail.
- [ ] 4.3 M3 audit: skip the family-file exemption check → `TestAuditFlagsDerivedFamilyComposedOutsideItsHome`
      MUST fail.
- [ ] 4.4 M8 `TaskMessage.Validate`: drop the both-or-neither rule → `TestTaskMessageRequiresRunEntityIDWithRunID`
      MUST fail.
- [ ] 4.5 M11 (wiring, not primitive): delete the `task.RunEntityID` assignment in `actions.go` →
      `TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin` MUST fail.
- [ ] 4.6 E2E falsifiability (round-1 D7): before its first green, run the `validate-run-identity` stage against
      main (the behavior absent) and record the RED verbatim; the stage reports the count of assertions it
      executed, and a zero-assertion green is a failure.

## 5. Sweep — spec, docs, e2e, sisters' note

- [ ] 5.1 Doc-comment sweep: `vocabulary/agentic/predicates.go` `LoopRunEntityID` example
      (`…execution.<runID>` → `…execution.<64-hex digest>`); `git grep -n "ChainExecutionEntityID"` over docs/
      comments and fix every survivor. Numeric budget sweep (round-1 M1): `git grep -nE '\b(170|163|156)\b'` over
      `docs/adr/`, `docs/operations/`, `config/`, `pkg/types/` and fix every budget survivor — ADR-104 decision 3
      and ADR-102:106 are amended by ADR-105 and must say so; `docs/operations/migration-beta162-to-beta163.md`
      (`:429`, `:754-756`, `:787`, `:836`) reconciles to 168/161; `config/config.go:816`, `config/manager.go:1200`
      and stale test prose update. Tool-surface correction (round-1 M2): `processor/agentic-tools/loop_result.go:72`
      parameter description and `:164-180` doc comment — "the full 6-part entity ID also works" becomes false for
      run entities (the strip yields the 64-hex digest); correct both to name the loop family only.
- [ ] 5.2 e2e: `run_scope=new` rule + `validate-run-identity` stage in the agentic scenario (the run entity is a
      64-hex-instance chain entity carrying `agent.run.origin-entity-id`; the loop's `agent.run.entity-id` equals
      it verbatim). The stage also asserts the suffix index (round-1 D6): `graph.query.bySuffix` resolves the run
      under its 64-hex instance and the loop under its UUID — the loop/run suffix collision this change removes.
- [ ] 5.3 `task schema:generate`; `git diff --exit-code schemas/ specs/` (the OpenAPI `run_entity_id` fields
      regenerate; vendored-contract holders' re-sync obligation goes in the migration note).
- [ ] 5.4 `test/compat/semteams/agentrun_terminal_compat_test.go` composes through
      `AgentRunIdentityFamily().DerivedEntityID`.
- [ ] 5.5 Append the migration-note section (skeleton in this change's design record) to
      `docs/operations/migration-beta162-to-beta163.md`; pin the sister SHAs read during the one bounded pass;
      record that semteams `01b` has NO substitute until #1193 lands.
- [ ] 5.6 The alert-path framing defect flagged on #1168 (`alertInstance`'s 12-byte raw timestamp frame sits
      outside the string-frame grammar) has NO filed issue and no stable O-number in the surviving record
      (measured 2026-08-31; round-1 D4): file it as its own issue and link it from #1192 — do not fix here.

## 6. Gates and landing

- [ ] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `openspec validate framed-digest-run-identity --strict`. Push only green
      states.
- [ ] 6.2 **BREAKING gate:** `task e2e:agentic` green on the branch before the breaking commit lands; `task
      e2e:core` re-run (budget fixtures). Results verbatim.
- [ ] 6.3 Implementation review by `semstreams-reviewer`; dispositions recorded in `conformance.md`.
- [ ] 6.4 Archive + spec sync as the LAST content commit, reviewed with the code (this is what lands the
      `:1063-1065` deferral replacement in `openspec/specs/graph-ingest/spec.md`).
- [ ] 6.5 Undraft; PR body carries `implemented-by:`, `Closes #1192` and `Closes #1174` (the mismatch-error rewrite in 3.2 is #1174's fix), the wire-value
      before/after list, and — if
      any round withdraws a claim a commit asserted — an authored squash body via `--body-file`.
