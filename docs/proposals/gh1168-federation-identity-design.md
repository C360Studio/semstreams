# gh#1168 — federation identity: design (options, decision records, impacts)

## Checkpoint

- **Accepted inventory:** `docs/proposals/gh1168-federation-identity-inventory.md` at `5967394f`, sha256
  `7ec8c0888afa485845e4206a7904d1f740d0ea2c437245f05e9877a0d70af9fb` (`INVENTORY PASS WITH DIVERGENCES`, residuals
  N1–N3 folded in). Referenced below by section (`inv §x.y`), not re-pasted.
- **Code baseline:** `main@300e57fe`; worktree `claude/gh1168-federation-identity` (only `docs/proposals/`,
  `docs/adr/104-*`, the migration doc section, and `openspec/changes/federation-identity/` differ from main).
- **Status: pending independent pre-owner design review.** Nothing here is approved. The owner rules O-1..O-8 and
  the §7 questions on #1168.
- **Binding inputs applied:** owner ruling 2026-08-30 (greenfield; #1168 comment 5469209066), owner steer (one
  derivation home on the `FrameworkIdentityFamily` seam; delete the recompute instruction; audit rule not package;
  web/lesson digests untouched; Case-B validation consolidated at the mint), owner-ruled shape 2026-08-29, the three
  unruled questions (O-1..O-3), the `STREAMKIT` note (2026-08-30), ADR-102 d1/d7, ADR-091, the CLAUDE.md context
  rule, inv §2.4 arithmetic, the e2e-before-breaking rule, the adopter-seam rule.
- **Artifacts this design produced:** `openspec/changes/federation-identity/{proposal,tasks}.md` and its three spec
  deltas (`entity-id-contract`, `graph-ingest`, `component-runtime-config`); `docs/adr/104-derived-identity-and-unique-authority.md`
  (Proposed); the federation-identity section of `docs/operations/migration-beta162-to-beta163.md`.

## 1. Premises (each measurable, each measured)

| # | Premise the design rests on | Measurement |
|---|---|---|
| P1 | The chain derivation `f(org, platform, loopInstance)` has 7 in-repo homes and 6 semteams homes; the carried `RunEntityID` already exists on every terminal event, tool metadata, and the local loop triple | inv §2.2 A1–A5, §2.6, §4.6; `events.go:26-28,83-85,169-171,219-221`, `tools.go:400`, `loop_execution_entity.go:139` |
| P2 | `FrameworkIdentityFamily.EntityID` is the only exported fixed-width compose seam; a 64-hex chain family has 88 fixed bytes and becomes the binding family (budget 170→168) | inv §2.2 "Digest-primitive homes", §2.4 arithmetic; `framework_identity_families.go:25-29, 65-90` |
| P3 | `RunID` keys `AGENT_LOOPS/<RunID>` in the terminal-settlement spec and code; nothing in `gateway/` or `test/e2e` reads it | inv §2.2 Fact A′, `agentic-terminal-events/spec.md:348-355`, `terminal_settlement.go:362-365` |
| P4 | `TaskMessage`, `LoopEntity`, `UserMessage` carry `RunID` but not `RunEntityID` | `rg -n RunEntityID agentic/user_types.go agentic/state.go` → 0 |
| P5 | No shipped config or e2e scenario exercises `run_scope` | `rg -n run_scope configs test/e2e` → 0 |
| P6 | First-boot persistence exists on the NATS medium (`semstreams_config` `platform` key, identity guard) and the effective config flows to `extractPlatformMeta` after `Manager.Start` | inv §2.4 P1; `cmd/semstreams/main.go:203-244`; `config/manager.go:172-260, 756-761, 866-894` |
| P7 | The KV `platform` push is `Put`; sync direction after the guard is version-driven (file newer → `PushToKV`; KV newer → `syncFromKV`) | `config/manager.go:225-260, 703-761` |
| P8 | `NewConfigManager` creates `context.Background()` in a constructor (`:73`); `Manager.Start(ctx)` receives a context | inv §7 Q9; `config/manager.go:73, 172` |
| P9 | The env override surface is `STREAMKIT_*` (id, type, region, four NATS fields; no org); no shipped artifact sets any; the binaries read `SEMSTREAMS_*` directly for NATS URLs, config path, log level; `STREAMKIT_NATS_*` is instructed only in `config/README.md:184,358` and `config/doc.go:87` | inv §4.22; `git grep -n STREAMKIT` → `config.go:388`, `config_test.go:131-137`, `doc.go:84,87`, `example_test.go:40-41`, `README.md:149,184,358` |
| P10 | `MinimalConfig` has 0 production callers | `rg MinimalConfig|LoadMinimalConfig --type go` → `service/doc.go:249` comment, `config/example_test.go:240-257` example only |
| P11 | At the fact-lane gate the port name is in scope but not carried; the envelope `source` is dropped; `Triple.Source` is persisted; the mutation lane has no import concept (`authorizeSubject(…, false)` at all four handlers); `FederationMeta` is unreadable on every lane | inv §2.5; `canonical_mutations.go:244,307,383,474` |
| P12 | An O-4 rejection must be classified `ErrorInvalid` to be `Term`'d rather than `Nak`'d by `processIngest` | `keyed_ingest.go:172-200` |
| P13 | `entity.indexing.profile` is the in-tree precedent for a framework-injected, create-time-immutable triple dropped from re-arrivals before merge | `component.go:2114, 2140-2150, 1849-1870`; `vocabulary/predicates.go:299-302` |
| P14 | `NewAlertEvent` has 0 production callers; the trigger path is live behind `enable_graph_integration` (`expression_factory.go:349-356` → `NewEntityUpdateEvent` → `publisher.go:151` → `graph.events.entity.update`); **no consumer of `graph.events.entity.*` exists** (only `graph.events.relationship.create`, `gateway/graph-gateway/component.go:1073`); `alert_cooldown_period` is operator-facing config (`processor/rule/config.go:33`) | inv §2.6; this pass |
| P15 | `FederationMeta` family: 0 callers, 0 tests, no ADR/spec/doc, introduced in the initial commit, phantom `BuildGlobalID` | inv §2.2 Fact B, §4.18 |
| P16 | `DeploymentPrefix()` is exported with 0 callers outside `pkg/types`; the spec's MUST-export list names it | inv §4.21; `entity-id-contract/spec.md:490` |
| P17 | `ResolveRun` has 0 callers outside its package; its only internal caller is the milestone slow path; `LoopTripleReader`/`NATSLoopTripleReader` exist only to feed it and are constructor parameters of `NewMilestoneSubscriber` (semteams `main.go:939`, our two `cmd` roots) | this pass: `rg LoopTripleReader|NATSLoopTripleReader` |
| P18 | `AgentRun.RunID()` has 0 production callers (5 test lines) | this pass: `rg '\.RunID\(\)'` |
| P19 | e2e tiers derive the authority pair by prediction from config files (`test/e2e/config/tier_authority.go`, `CoreAuthority`), the e2e client can read any KV bucket, and `docker/compose/lifecycle.yml:62` hardcodes a seed under the predicted pair | this pass |
| P20 | `entity.import.lane`, `entity.admission.*`, `graph.admission.*` collide with nothing | `git grep` → 0 |

## 2. Options considered, with costs

### 2.1 Case A — collision-free run identity

| Option | Shape | Cost | Where it asks someone to predict |
|---|---|---|---|
| A0 do nothing | keep `Mint`'s loud refusal (#1148) | zero; two foreign origins at one instance can never both have a run; 13 re-derivation sites stay | every re-derivation site predicts the run ID from a fragment (inv §6 S3) |
| A1 digest over the full origin, **one home on the family seam**, readers read the carried value | `pkg/types` gains the `agent-run` family and `DerivedEntityID`; `Mint` mints from `originEntityID` alone; `RunEntityID` is carried on `TaskMessage`/`LoopEntity`/`UserMessage`; A2–A5 read it; `TryChainExecutionEntityID`/`ChainExecutionEntityID`/`ResolveRun`/`RunID()` deleted; audit rule pins the home | BREAKING: `Mint` arity, `NewMilestoneSubscriber` arity, run entity IDs change shape, `ChainExecutionEntityID` gone (semteams 6 sites), budget 170→168; three additive wire fields | none: the framework observes the origin and carries the result |
| A2 digest over the full origin, derivation exported as a builder (`agentic.RunEntityIDFromOrigin`) so sisters keep deriving | same digest; re-derivation sites re-target the new builder | smaller sister diff (6 sites re-pointed); keeps 13 homes of a prediction; contradicts the owner steer | every site still predicts |
| A3 make `RunID` itself the digest | one identifier | breaks `AGENT_LOOPS/<RunID>` keying and the terminal-settlement spec (P3); dispatch would need graph access to find the root loop | none, but a plane-separation break |

**Recommendation: A1.** A2 is the steer's rejected shape; A3 costs a spec-level plane rule for no identity gain.

### 2.2 Case B — a unique authority pair by default

| Option | Where the suffix is minted / persisted | Cost | Prediction asked |
|---|---|---|---|
| B0 do nothing | — | the cloned-template footgun stays; `local_authority_claimed` is the only detector | operator predicts global uniqueness |
| B1 mint at `Manager.Start` on first boot, persist in `semstreams_config` (`Create`), adopt on later boots and in every co-process | uses P6/P7; context flows from `Start(ctx)`; the identity guard learns "stem" | e2e must observe the pair instead of predicting it (P19); `platform.unique` opt-out knob; budget arithmetic +7 bytes | none for the 99% case; the opt-out is the owner-ruled "operator owns uniqueness" statement |
| B2 mint at config load, persist by rewriting the config file (`SaveToFile`) | file medium (inv P6 precedent) | read-only mounts fail; two processes sharing a config on different hosts diverge; `SaveToFile` has 0 callers and writes the whole config | operator predicts writable config |
| B3 derive deterministically from a host fact (hostname) | no persistence | clones with equal hostnames collide; not entropy | — |
| B4 refuse an entropy-less id at load, no mint | — | "entropy-less" is undecidable by grammar; shifts the mint to the operator's hands | operator predicts |

**Recommendation: B1.** It is the only shape where the framework observes sameness (the KV record) rather than asking the operator to predict uniqueness, and it reuses P1's bucket, guard, and boot ordering.

### 2.3 O-4 — the retained "first admitted under" fact

| Option | Storage (entity-or-bucket) | Key (O-3) | Cost |
|---|---|---|---|
| C0 do nothing | — | — | first-write-wins merge on collision, unobservable (inv §6 S6) |
| C1 birth triple `entity.import.lane` on the mirror, immutable at merge (P13 precedent), compared inside the CAS closure | graph triple — default of the rubric; no ground 1–6 for a bucket | arrival **port name** (carried into `ingestWork`) | one predicate, one reason, one metric label, one `ingestWork` field; renaming an import port after admission rejects that lane's re-arrivals (fresh-state posture) |
| C2 same triple, keyed on envelope `source` | triple | producer-chosen string, dropped today (must be carried) | a peer chooses the key; a peer can claim another peer's key; ADR-102 d5 says `source` authenticates nothing |
| C3 same triple, keyed on per-triple `Triple.Source` | triple | already persisted, but per-triple and producer-chosen; the design.md §F wording | same weakness as C2 plus multi-valued ambiguity on one entity |
| C4 sidecar KV beside `GRAPH_INGEST_APPLIED_SEQ` keyed `<entityID>/<lane>` | bucket — needs a ground; none of 1–6 holds (the check already runs inside the CAS closure with the resident state in hand) | any | an extra KV read per import arrival; not rule-readable; catalog work |

**Recommendation: C1.** Skill outcomes: `entity-or-bucket` → triple (no bucket ground; rules, GraphQL and the gate read one fact); `kv-or-stream` → not triggered (no new communication path; the fact rides the existing ENTITY_STATES write); `orchestration-check` → not triggered (single write, no multi-step behaviour); `new-payload` → not triggered (no new message type; three structs gain a field).

### 2.4 The zero-caller exported surfaces (owner ruling: delete, never deprecate; CLAUDE.md: wired ≠ wanted)

| Surface | History (`git log -S`) and governing decision | Dead or broken path? | Disposition |
|---|---|---|---|
| `message.FederationMeta` family + `WithFederation{,AndTime}` + phantom `BuildGlobalID` doc | initial commit `3361a8dc` (2025-11-17), docs `7ff26cb4`, type-alias `9d70b346`; **no ADR, spec, or concept doc**; semdragon measured it never called it | dead: never serialized (inv §2.5), no product asks, no test | **delete** `message/federation.go`, `base_message.go:79-97` options and doc examples `:36,40,116,120`, `message/doc.go:332,463`, `graph/README.md:140` mention, `pkg/platform/platform.go:27-28` "embedded message metadata" clause; `message` drops its `pkg/platform` import |
| `pkg/types.EntityID.DeploymentPrefix()` | added by ADR-102 slice A `3f3133a6` with the level vocabulary; the sibling `PrefixLevel*` set was deleted 2026-08-28 for zero consumers; spec `:490` names it | dead: 0 callers outside; the "deployment" level remains expressible as a 2-position query prefix | **delete** the method (inline in `SourcePrefix`); MODIFIED entity-id-contract prefix requirement drops it from the MUST-export list |
| `agentic.ChainExecutionEntityID` / `TryChainExecutionEntityID` | `abc1be19` (2026-05-07, semteams ADR-038 chain anchor) | the fragment derivation this change replaces | **delete both**; the one home is `AgentRunIdentityFamily().DerivedEntityID` called from `agentrun.Mint` |
| `agentrun.ResolveRun`, `LoopTripleReader`, `NATSLoopTripleReader` | `ac55fcf9` (ADR-053 #231) D6 "ancestry-walk fallback for pre-migration / un-threaded loops" | dead under fresh state: every loop in a run carries `RunEntityID` at spawn (A1) | **delete**; `NewMilestoneSubscriber(mgr, logger)` — BREAKING for semteams `main.go:939` and our two roots |
| `AgentRun.RunID()` / `runIDFromChainEntityID` | ADR-053 D1 | derivation from the instance; 0 production callers | **delete**; the root loop is `LoopIDFromExecutionEntityID(run.OriginEntityID)` |
| `graph.NewAlertEvent` (+ `alertInstance`, `writeFramedString`) | `b8ec8bf5` "add rules engine" (2025-11-19), `4c4b720f` consolidation; **ADR-076 d1/d2 decided the alert family**; `alert_cooldown_period` is shipped operator config | **broken path, not dead**: the constructor is never called AND the `graph.events.entity.*` lane it would publish on has no consumer (P14), so ADR-076's alert *and trigger* entities never reach `ENTITY_STATES` | **not deleted here — filed** as a defect ("ADR-076 rule alert/trigger entities have no producer/consumer path to the graph"); its private frame writer stays with it until that defect is ruled; the trigger's byte-identical duplicate `writeRuleTriggerFrame` consolidates onto the new `pkg/types` helper |
| `agentic/tools.go:396-399` recompute instruction | ADR-053 D8 era | the prediction-shaped doc | **delete** the sentence; `MetadataKeyRunEntityID` is stamped from the carried value |
| `config.MinimalConfig` (+ `LoadMinimalConfig`) | — | dead (P10) | **delete** `config/minimal_config.go`, `ExampleMinimalConfig`, `service/doc.go:249` line |
| `STREAMKIT` env prefix and `applyEnvOverrides` | dead pivot (owner note 2026-08-30) | dead | **delete** (§3 O-6) |

## 3. Decision records for the owner

**O-1 — Does `RunID` stop meaning the dispatch-root loop UUID?** Options: (a) yes, `RunID` becomes the run instance (option A3 above — breaks `AGENT_LOOPS/<RunID>`); (b) no on the loop plane, yes for the run *entity*: `RunID` keeps naming the root loop's UUID and its `AGENT_LOOPS` record; the run entity's instance is the framed digest of the origin's full ID; the run entity is **carried** (`RunEntityID` on `TaskMessage`, `LoopEntity`, `UserMessage`, the four events, tool metadata, `agent.run.entity-id`) and never derived from `RunID`; `agent.run.origin-entity-id` (written by `Mint`) becomes the run→loop pointer that replaces `AgentRun.RunID()`; (c) A0. **Recommendation: (b).** Consequence: the answer to the issue's phrasing is "the run entity stops being derivable from `RunID`; `RunID` keeps its loop-plane meaning", which leaves `agentic-terminal-events` untouched and gives `agent.run.origin-entity-id` its first reader (the milestone subscriber and any product resolving the root loop).

**O-2 — Refuse an entropy-less `platform.id`, or default one?** Options: (A) suffix on first boot whenever `platform.unique` is not `true`, whether the id is hand-written or a copied template; the KV record — never grammar — is what says "already minted"; (B) suffix only when `platform.id` is absent (rejected: the footgun is a present, cloned id; and `platform.id` is required today — inv G6); (C) refuse at load unless `unique: true` (rejected: undecidable by grammar; moves the mint to the operator). **Recommendation: (A).** Evidence: the three validator homes collapse to two calls of one function (`validateAuthorityPair` at load for shape; at the mint for the suffixed pair); `MinimalConfig` deleted; the STREAMKIT override deleted (O-6) so "absent" has exactly one source, the file.

**O-3 — O-4's comparison key.** Options: arrival **port name** (framework-observed, operator-declared, unique within the component's ports; carried one field further into `ingestWork`); envelope `source` (producer-claimed, dropped today); `Triple.Source` (producer-claimed, per triple); filter subject (two ports may share a subject shape). **Recommendation: port name**, recorded on the mirror as `entity.import.lane`. Cost stated: renaming an import port after admission rejects that lane's re-arrivals until fresh state (pre-v1 posture).

**O-4 — Derivation home and the alert path.** Options: (i) `pkg/types` exports `(FrameworkIdentityFamily).DerivedEntityID(org, platform, digestDomain string, frames ...string)`; run and trigger consolidate onto it (trigger bytes identical: all its frames are length-prefixed strings); alert left with its private writer pending the filed defect; (ii) run-only helper in `agentrun`. **Recommendation: (i)** — one home, and the audit rule `derived_family_composed` (production Go composing positions 3–5 of a derived family anywhere but the family file) keeps it one.

**O-5 — Zero-caller dispositions** as in §2.4. The owner is asked to confirm the one non-deletion: `NewAlertEvent` filed rather than deleted, because ADR-076 and `alert_cooldown_period` expect the path.

**O-6 — Environment override of the platform pair.** The STREAMKIT surface is deleted whole: `config/config.go:380,388,733-758` (`envPrefix`, `applyEnvOverrides`), `config_test.go:131-137` (the env-override test), `example_test.go:40-41`, `doc.go:84,87`, `README.md:149,184,358`. Should any env override of the pair exist afterwards? **Recommendation: no.** The pair is identity that is now minted and persisted per NATS server; a per-process override would fork identity between co-processes sharing one config (the `e2e.yml` two-binary shape) and bypass the KV record. NATS credential overrides: `SEMSTREAMS_NATS_URLS` (`cmd/semstreams/main.go:482`) is the only live one and is untouched; username/password/token had only the dead `STREAMKIT_` path and no shipped artifact or operator doc outside `config/README.md` — their loss is recorded in the migration note; if wanted they are a fresh `SEMSTREAMS_`-prefixed design, not this change.

**O-7 — Seam issues.** #1154: the *run* derivation from wire `Org`/`Platform` at `loop_execution_entity.go:138` is absorbed (it now stamps `Task.RunEntityID` verbatim); the five types' own `EntityID()` recompute stays #1154's scope — excluded, sequenced after. #1172: excluded — hierarchy observability is a different seam and this change adds no second interpreter. #1174: absorbed — `Mint`'s remaining stored-origin check reports the conflict without either identity (the run's own local ID is in the caller's hands). #1171: absorbed — `pkg/types/entity_domain_authority.go` → `entity_domain.go` in the sweep (this change edits `pkg/types` anyway).

**O-8 — e2e tiers and coverage.** Case A: `task e2e:agentic` gains stage `validate-run-identity` (a rule pack with `run_scope=new`, P5 says none exists) asserting the run entity `*.*.chain.agent.execution.<64hex>` carries `agent.run.origin-entity-id` = the firing loop and the child loop and its completion event carry the same `run_entity_id`. Case B: `task e2e:core` gains stage `validate-minted-authority` (the e2e client reads `semstreams_config/platform_identity`; asserts `id == stem + "-" + 6hex`, the canary is under that pair) and `task e2e:lifecycle`'s seed composes under the observed pair. O-4: not a shipped-config break (no lane is shipped); landing coverage is the integration test family `authority_gate_integration_test.go`; e2e coverage of two import lanes is **filed as a gap** (the core-federation scenario was removed by #1129) unless the owner wants an e2e-only two-lane config.

### Break wave

| Order | Item | BREAKING | Tier before landing |
|---|---|---|---|
| 1 | this change, one PR, one archive | yes | `e2e:agentic` (Case A stage), `e2e:core` (Case B stage), `e2e:lifecycle` (seed under observed pair), `e2e:structural` (regression on ingest/hierarchy) |
| 2 | sisters re-slot on the tag | — | each sister's own gates on fresh storage |

## 4. Second- and third-order impact rows

| Surface | Today | After | Evidence |
|---|---|---|---|
| Run entity ID | `org.platform.chain.agent.execution.<loopUUID>` | `…chain.agent.execution.<64 hex>` | inv A1 |
| Family table / budget | 3 families, longest 86, budget 170 | 4 families, longest 88 (`agent-run`), budget **168**; `TestConfigRejectsOversizedAuthorityPair` 171→169; `TestMaxAuthorityPairBytesDerivesFromLongestFamily` self-updates | inv §2.4 |
| NATS subjects | `agent.complete.<loopID>` etc. | unchanged (loop IDs unchanged) | P3 |
| KV keys | `AGENT_LOOPS/<loopID>`, `semstreams_config/{version,platform,…}` | unchanged + `semstreams_config/platform_identity` (Create-once) | P6 |
| Wire fields | `TaskMessage.RunID`, `LoopEntity.RunID`, `UserMessage.RunID` | + `RunEntityID` on each (additive JSON; `TaskMessage.Validate` requires both-or-neither) | P4 |
| Prefix queries | `org.platform.chain.agent.execution` | unchanged; `DeploymentPrefix()` method gone, 2-position prefixes still valid | P16 |
| Predicates | — | + `entity.import.lane` (framework-owned, stamped on import-lane births, stripped from arrivals) | P13, P20 |
| Coded errors / metrics | `foreign_authority`, `local_authority_claimed` | + `import_collision` → `mutation_rejections{reason="authority_collision"}`; `Mint` mismatch error no longer names identities | inv §2.5 |
| Audit rules | 3 segment rules | + `derived_family_composed` | `segment_rules.go:29-38` |
| Config | `platform.{org,id,type,region,capabilities,environment}` | + `platform.unique` (bool); `MinimalConfig` gone; `STREAMKIT_*` gone; `config/README.md:43-50,149,184,358`, `config/doc.go:84,87` updated | P9, P10 |
| e2e literals | `TierAuthority`, `CoreAuthority`, `lifecycle.yml:62` seed | observed from `platform_identity`; `TestTierAuthorityMatchesShippedConfigs`/`TestCoreAuthorityMatchesShippedConfig` become stem checks; `--lifecycle-seed` takes positions 3–6 and composes under the observed pair | P19 |
| Docs | `docs/concepts/16-federation.md:55-63` ("nothing coordinates that pair"), `gh1095-…-design.md:275`, ADR-053 D1/D8 mechanics, `agentic/tools.go:396-399`, `graph/README.md:140`, `message/doc.go:332,463` | corrected / deleted; ADR-104 amends ADR-102 and ADR-053 by reference | inv G12 |
| Context rule | `config/manager.go:73` root in a constructor | untouched and recorded as removal debt (separate issue); the mint runs under `Start(ctx)` | P8 |
| semsource arithmetic | `MaxOrgLen=64` assumes `platform (9)` | the suffix adds 7 bytes; migration note | inv Fact B′ |

## 5. Sister impact (communicate only; read-only census inv §4.6)

| Adopter | Impact |
|---|---|
| semteams | 6 derivation sites re-point to carried values (`RunEntityID` on the event/metadata/triple; migration text); `isChainExecutionEntityID` parse `implementspec/command.go:305-322` keeps working on the shape; `main.go:939` `NewMilestoneSubscriber` arity; `implementspec/command.go:212-217` accepts a run entity ID or reads `RunEntityID` from the message; 4 configs gain a suffix on first boot unless `unique: true`; `instance_id` already refused |
| semdev | `agentrun.Register` unchanged; testdata `bad.go:33` 5-arg `Mint` becomes 4-arg; `ledger.RunID` unchanged (loop UUID); 2 configs suffixed |
| semspec, semspec-ui-* | their `RunID` is their own `workflow/` concept — no framework change; 13/11/11 configs suffixed unless `unique` |
| semmem | `instance_id` in 3 configs already fails load on main; suffix afterwards |
| semsource | `MaxOrgLen` comment/arithmetic assumes a 9-byte platform; with `-xxxxxx` the org budget it assumes shrinks by 7 |
| semmachina | composes `platform.id` per world in Go; its `ssconfig.Config` gains `Unique` if it wants pure-readable world ids, else each world's id is suffixed on its own first boot |
| semboids, semsage, semconnect, semdragon, semlink, semops | config suffix only; no code surface |

## 6. Decision skills applied

`entity-or-bucket` → O-4's fact is a graph triple (`entity.import.lane`), not a bucket: no ground 1–6 holds, and rules, GraphQL and the gate read one fact. `kv-or-stream` → not triggered: no new communication path; the fact rides the existing ENTITY_STATES write and the identity record rides the existing `semstreams_config` bucket. `orchestration-check` → not triggered: single writes, no multi-step behaviour. `new-payload` → not triggered: no new message type; three structs gain an additive field.

## 7. Open questions for the owner (rule on #1168)

1. **O-1..O-8** as recorded in §3, each with its recommendation.
2. **`NewAlertEvent` and the `graph.events.entity.*` lane** (P14): file as a defect and leave the constructor, as recommended, or delete the alert family now and amend ADR-076 by reference?
3. **The O-4 e2e gap:** accept integration-test coverage for landing and file the two-lane e2e gap, or rule an e2e-only two-import-lane config into `e2e:core`?
4. **`semstreams_config` is one bucket per NATS server, shared by every sem* app on it** (inv §2.4). The suffix mint inherits that scope: two *different* sem* apps on one NATS with entropy-less ids `dep` would be told apart by the existing environment/tuple guard, but two clones of one app on one NATS share one record by design. Is "first boot" = "first boot per NATS server" the intended scope, or should the record key include the org (`platform_identity.<org>`)?
5. **`platform.unique` naming** — the one operator knob this design adds; alternatives `id_unique`, `suffix: false`. It is the owner-ruled override expressed as one boolean; the design deletes no other knob to make room for it.
6. **Q5 from the inventory** (lesson digest content vs the design-doc claim) is untouched by this design per the steer; confirm it stays a doc correction only.
