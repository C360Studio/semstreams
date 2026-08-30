# Tasks — federation-identity

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat; use "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises (measured at `main@300e57fe`): inventory `docs/proposals/gh1168-federation-identity-inventory.md`
(`5967394f`, sha256 `7ec8c088…`) §2.2 A1–A5 (seven derivation sites), §2.4 P1/P6/P7 (config-manager first boot and
sync direction), §2.5 (gate provenance), §2.6 (caller table), §4.22 (STREAMKIT surface); design
`docs/proposals/gh1168-federation-identity-design.md` P1–P20.

**Design status: pending independent pre-owner design review and the owner's rulings O-1..O-8 on #1168.** No task
below starts before both.

## 1. Claim

- [x] 1.1 Worktree `../semstreams-wt/claude/gh1168-federation-identity`, branch `claude/gh1168-federation-identity`;
      draft PR #1178 `Closes #1168` (add `Closes #1171`, `Closes #1174` only when the owner's CONFIRM-CLOSE names
      them); `implemented-by: <persona>` set at implementation. The proposal was the first commit; the inventory and
      this design followed.

## 2. Baseline capture — write the named tests first

- [ ] 2.1 `pkg/types/framework_identity_families_test.go`: `TestAgentRunFamilyBindsTheAuthorityPairBudget` (168;
      `TestMaxAuthorityPairBytesDerivesFromLongestFamily` self-updates), `TestDerivedEntityIDIsFramedSHA256OverTheOrigin`
      (two origins sharing an instance → two IDs; frame order and length prefix pinned against a hand-computed
      vector), `TestDerivedEntityIDRefusesEmptyOrigin`. Does not compile at baseline.
- [ ] 2.2 `agentic/agentrun/agentrun_test.go`: `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns`,
      `TestMint_RefusesEmptyOrigin` (kept), `TestMint_StoredOriginMismatchIsRefusedWithoutNamingIt` (error text
      contains neither identity), `TestMint_IsIdempotentForOneOrigin`; `agentrun_integration_test.go` drops `RunID()`.
      The three retired names (`…AreRefusedNotAliased`, `…LegacyOriginlessStoredRunIsRefused`, `TestMint_DotInLoopIDReturnsError`) are deleted, not renamed.
- [ ] 2.3 `agentic/run_id_test.go`: `TestTaskMessageRequiresRunEntityIDWithRunID`, `TestLoopEntityRunEntityIDRoundTrip`,
      `TestUserMessageRunEntityIDRoundTrip` through the production decoder.
- [ ] 2.4 `processor/agentic-loop/handlers_test.go`: `TestRunEntityIDIsCarriedOnEveryLoopSurface` — created/completed/
      failed/cancelled events and `ToolCall.Metadata[agent.run_entity_id]` equal `task.RunEntityID` verbatim; no
      `resolveRunEntityID` remains (compile).
- [ ] 2.5 `processor/rule/actions_run_scope_integration_test.go` (integration): the three existing tests updated to
      the digest form; `TestRunScopeInheritCarriesRunEntityID` (inherit path copies `agent.run.entity-id`).
- [ ] 2.6 `internal/entityidaudit/audit_test.go`: `TestAuditFlagsDerivedFamilyComposedOutsideItsHome` (a `Sprintf`
      with `chain.agent.execution` in production Go → finding; a declaration pattern `*.*.chain.agent.execution.*`
      → no finding; the family file itself → no finding).
- [ ] 2.7 `config/manager_test.go` (integration, real NATS): `TestConfigManagerFirstBootMintsPlatformIdentity`,
      `TestConfigManagerAdoptsPersistedPlatformIdentity`, `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity`
      (two managers, one bucket, one record), `TestUniquePlatformIDIsNotSuffixed`,
      `TestFirstBootMintsDistinctSuffixesPerDeployment` (two buckets), `TestVersionArbitrationNeverOverwritesPlatformIdentity`;
      `config/config_test.go`: `TestPlatformPairHasNoEnvironmentOverride`, `TestConfigRejectsOversizedAuthorityPair` (169).
- [ ] 2.8 `processor/graph-ingest/authority_gate_integration_test.go` (integration): `TestImportCollisionRejectsSecondLane`,
      `TestImportLaneTripleIsFrameworkOwned`, `TestImportCollisionIsTerminatedNotRedelivered`.
- [ ] 2.9 `pkg/types/entity_id_semantics_test.go`: `TestPrefixLevelsAreNamed` drops `DeploymentPrefix`; `message/`
      `TestBaseMessageMetaIsCreatedAtReceivedAtSourceOnly` (no federation option compiles).
- [ ] 2.10 Baseline capture on main, verbatim, filtered to build errors and `--- FAIL` lines.

## 3. Contract — `pkg/types`, `agentrun`, wire, config

- [ ] 3.1 `pkg/types/framework_identity_families.go`: add `{Name: "agent-run", System: "chain", Domain: "agent",
      Type: "execution", InstanceBytes: 64}`, `AgentRunIdentityFamily()`, and
      `(f FrameworkIdentityFamily) DerivedEntityID(org, platform, digestDomain string, frames ...string) (string, error)`
      (length-framed SHA-256; refuses an empty frame). Delete `EntityID.DeploymentPrefix()` (inline in `SourcePrefix`).
      Rename `entity_domain_authority.go` → `entity_domain.go` (#1171).
- [ ] 3.2 `agentic/agentrun/agentrun.go`: `Mint(ctx, mgr, org, platform, originEntityID string)` mints through
      `AgentRunIdentityFamily().DerivedEntityID(org, platform, "semstreams.agent.run.v1", originEntityID)`; the
      stored-origin check keeps its classification and names neither identity (#1174); delete `RunID()`,
      `runIDFromChainEntityID`, `ResolveRun`, `LoopTripleReader`, `maxAncestryHops`, `nats_reader.go`; the milestone
      subscriber keeps the fast path only; `NewMilestoneSubscriber(mgr, logger)`,
      `NewMilestoneSubscriberWithRunStateReader(runs, logger)`.
- [ ] 3.3 `agentic/entity_ids.go`: delete `ChainExecutionEntityID` and `TryChainExecutionEntityID`.
      `agentic/tools.go:396-399`: delete the recompute instruction.
- [ ] 3.4 Wire: `TaskMessage.RunEntityID`, `LoopEntity.RunEntityID`, `UserMessage.RunEntityID` (`json:"run_entity_id,omitempty"`);
      `TaskMessage.Validate` requires both-or-neither; `LoopManager.SetRunEntityID/GetRunEntityID`;
      `LoopExecutionEntity.Triples` stamps `agent.run.entity-id` from `Task.RunEntityID` verbatim (deletes the
      wire-authority derivation, the run half of #1154); `handlers.go` deletes `resolveRunEntityID` and reads
      `entity.RunEntityID` at the five sites; `loop_wire.go` reads `e.RunEntityID`; dispatch fills
      `TaskMessage.RunEntityID` from `UserMessage.RunEntityID` or, when absent and `ReplyTo` names a persisted loop,
      from that record, else rejects the submission naming the field.
- [ ] 3.5 `processor/rule/actions.go`: `run_scope=new` passes the firing entity to `Mint` and sets
      `task.RunEntityID` from the returned run; `stampRunAnchors` takes the run entity ID; inherit path copies
      `agent.run.entity-id`; `graph_event_identity.go` composes through `DerivedEntityID` and deletes
      `writeRuleTriggerFrame` (digest bytes unchanged — pinned by the existing trigger identity tests).
- [ ] 3.6 `config`: `platform.Config.Unique bool` (`json:"unique,omitempty"`); delete `envPrefix`, `applyEnvOverrides`,
      `minimal_config.go`, `ExampleMinimalConfig`; `Manager.Start(ctx)` gains `establishPlatformIdentity(ctx)` before
      the identity guard — `Create` of `platform_identity` `{org, stem, id}` with `crypto/rand` 3 bytes → 6 hex, adopt
      on `ErrKVKeyExists`, re-run `validateAuthorityPair` on the effective pair, set the effective config through
      `SafeConfig.Mutate`; the guard compares the effective id; `PushToKV`/`syncFromKV` never write or apply
      `platform_identity`; every call uses the Start context (`manager.go:73` is untouched and stays recorded debt).
- [ ] 3.7 `graph-ingest`: `ingestWork.portName`; `vocabulary.EntityImportLane = "entity.import.lane"` registered;
      the CAS closure stamps it on import-lane births, strips it from every arrival, and on the existing branch
      compares it when `importLane` is true — mismatch returns `errs.WrapInvalid` with code
      `entity_id_authority_invalid`, reason `EntityIDReasonImportCollision = "import_collision"` (in `pkg/types`),
      metered `authority_collision` through `authorityMetricReason` (explicit case), WARN names both lane names.
- [ ] 3.8 `message`: delete `federation.go`, `WithFederation`, `WithFederationAndTime`, the doc examples
      (`base_message.go:36,40,116,120`, `doc.go:332,463`), the `pkg/platform` import; `graph/README.md:140`;
      `pkg/platform/platform.go:27-28` clause.

## 4. Forced omissions — one per new guard (commit GREEN first; restore by `cp` + `shasum`)

- [ ] 4.1 M1 `DerivedEntityID`: hash the instance segment instead of the full origin → `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns` MUST fail.
- [ ] 4.2 M2 `Mint`: delete the stored-origin comparison → `TestMint_StoredOriginMismatchIsRefusedWithoutNamingIt` MUST fail.
- [ ] 4.3 M3 audit: skip the family-file exemption check → `TestAuditFlagsDerivedFamilyComposedOutsideItsHome` MUST fail.
- [ ] 4.4 M4 `establishPlatformIdentity`: use `Put` instead of `Create` → `TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity` MUST fail.
- [ ] 4.5 M5 guard: compare the file id instead of the effective id → `TestConfigManagerAdoptsPersistedPlatformIdentity` MUST fail.
- [ ] 4.6 M6 gate: delete the lane comparison CALL in the CAS closure → `TestImportCollisionRejectsSecondLane` MUST fail.
- [ ] 4.7 M7 gate: skip the strip → `TestImportLaneTripleIsFrameworkOwned` MUST fail.
- [ ] 4.8 M8 `TaskMessage.Validate`: drop the both-or-neither rule → `TestTaskMessageRequiresRunEntityIDWithRunID` MUST fail.

## 5. Sweep — spec, docs, e2e, configs, sisters' notes

- [ ] 5.1 **First implementation task by direction: delete the DEFERRED paragraphs at `openspec/specs/graph-ingest/spec.md:934-948`**
      via this change's graph-ingest delta (the MODIFIED block restates every scenario); `openspec validate --strict`.
- [ ] 5.2 Docs: `docs/proposals/gh1095-entity-id-segment-semantics-design.md:275` (the collision claim corrected by
      an appended note, not rewritten); `docs/concepts/16-federation.md:55-63`; `config/README.md:43-50,149,184,358`;
      `config/doc.go:84,87`; `docs/operations/38-agent-terminal-settlement.md` (RunID unchanged — verify only);
      ADR-104 to Accepted; `docs/adr/README.md` index.
- [ ] 5.3 e2e: `test/e2e/config` reads `semstreams_config/platform_identity` (`EffectiveAuthority(ctx)`); the two
      drift tests become stem checks; `cmd/e2e/main.go:370` canary uses the observed pair; `cmd/e2e-semstreams`
      `--lifecycle-seed` takes positions 3–6 and composes under the effective pair; `docker/compose/lifecycle.yml:62`;
      `test/e2e/scenarios/lessons/scenario.go:35`, `throughput/query_load.go:95`, `tiered.go:428`.
- [ ] 5.4 e2e stages: `configs/rules/agentic/run-scope-new.json` + `validate-run-identity` in the agentic scenario;
      `validate-minted-authority` in the core scenario.
- [ ] 5.5 `task schema:generate`; `git diff --exit-code schemas/ specs/`.
- [ ] 5.6 Migration note: the federation-identity section of `docs/operations/migration-beta162-to-beta163.md`
      (landed with the design; amend to what shipped).
- [ ] 5.7 File the two issues this design records rather than fixes: ADR-076 alert/trigger entities have no path to
      the graph (`NewAlertEvent` 0 callers; `graph.events.entity.*` 0 consumers); `config/manager.go:73`
      `context.Background()` in a constructor.
- [ ] 5.8 `test/compat/semteams/agentrun_terminal_compat_test.go:44-52` uses `AgentRunIdentityFamily().DerivedEntityID`.

## 6. Gates and landing

- [ ] 6.1 `task lint`; `go test -race -count=1 ./...`; `go test -tags=integration -race -count=1 -p 2 ./...`;
      `go test ./test/contract/...`; `task entity-id:audit`; `task schema:generate && git diff --exit-code schemas/ specs/`;
      `openspec validate federation-identity --strict --no-interactive`; `go mod tidy -diff`.
- [ ] 6.2 Covering e2e tiers, one at a time on an idle host, results verbatim: `task e2e:core` (Case B stage),
      `task e2e:agentic` (Case A stage), `task e2e:lifecycle` (observed-pair seed), `task e2e:structural`
      (ingest/hierarchy regression). Excluded with reason: `statistical`, `semantic` (same ingest path as structural,
      no run or identity literal), `ops`, `lessons`, `crud-tools`, `research-graph`, `deep-research`, `slow-consumer`,
      `throughput`, `openai-responses` (no touched path beyond the e2e config helper — re-run any whose scenario
      file changes in 5.3). The O-4 two-lane e2e gap is filed unless the owner rules an e2e-only config in.
- [ ] 6.3 Implementation review by `semstreams-reviewer`; dispositions in `conformance.md`.
- [ ] 6.4 Owner-run cross-agent round where asked.
- [ ] 6.5 `openspec archive federation-identity` + spec sync as the final content commit; narrow reviewer check.
- [ ] 6.6 Undraft; PR body carries `implemented-by`, the per-sister list, the values that change on the wire
      (run entity instance, `platform.id` suffix), the e2e evidence pointers. No task asserts CI state.
