# Beta.161 OpenSpec closeout inventory

Baseline: `1089545bc5eadb78facf657e89a56d61072df6ba`

Phase: `inventory-only`

Body SHA-256: `b029f68cea409c93517384e3e9152f4508d7a52133b772d32d72e23968fcdd50`

Hash method: `sed -n '/^## Inventory body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Inventory body

# Inventory-only handoff: beta.161 truth closeout

Baseline: clean `origin/main` at `1089545bc5eadb78facf657e89a56d61072df6ba`.

Scope: SemStreams only. No files changed. No Docker, integration-tag, E2E, teardown, or cleanup commands run.

## Problem statement

Six OpenSpec changes remain active:

- `reserve-typed-user-response-subjects`
- `durable-max-delivery-occurrences`
- `normalize-agent-terminal-settlement`
- `stream-capacity-rejection-is-circuit-neutral`
- `post-g-tag-safety-closeout`
- suspended `semantic-tier-split`

The inventory question is why each remains active, which evidence is current, and which remaining gates conflict with repository ownership. This handoff intentionally contains no target state, closeout recommendation, artifact delta, or archive commands pending `INVENTORY PASS`.

## Surface inventory

### 1. Claimed gaps

`reserve-typed-user-response-subjects`

- Framework implementation exists on current main:
  - private subject classifier: `processor/rule/user_response_subject_reservation.go:5-22`
  - definition validation: `processor/rule/config_validation.go:324-332`
  - runtime guards for `publish`, `publish_agent`, and `approve`: `processor/rule/actions.go:872-886,1482-1496,1860-1870`
  - governance boot rejection for any `violations.notify_user` presence: `processor/agentic-governance/component.go:56-64,176-195`
  - typed dispatch declaration: `processor/agentic-dispatch/config.go:93-96`
  - migration guidance: `docs/operations/migration-beta160-user-response-subjects.md:1-36`
- The active #952 package is checksum-bound, including the task file that contains the ownership conflicts.
- `openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256:1-13` records three baselines and exactly ten tracked artifacts:
  1. `docs/proposals/gh952-user-response-contract-inventory.md`
  2. `docs/proposals/gh952-user-response-contract-design.md`
  3. `docs/adr/093-typed-user-response-subject-ownership.md`
  4. `docs/operations/26-nats-kv-key-migration-ledger.md`
  5. `docs/operations/migration-beta160-user-response-subjects.md`
  6. `openspec/changes/reserve-typed-user-response-subjects/proposal.md`
  7. `openspec/changes/reserve-typed-user-response-subjects/design.md`
  8. `openspec/changes/reserve-typed-user-response-subjects/tasks.md`
  9. `openspec/changes/reserve-typed-user-response-subjects/specs/user-response-subject-ownership/spec.md`
  10. `openspec/changes/reserve-typed-user-response-subjects/implementation-evidence.md`
- `sha256sum -c openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256` reports all ten artifact bodies `OK`; the three baseline comment lines produce only the expected “improperly formatted” warning.
- The immutability claim is explicit in:
  - `openspec/changes/reserve-typed-user-response-subjects/proposal.md:10-17`
  - `openspec/changes/reserve-typed-user-response-subjects/design.md:3-13`
  - `docs/proposals/gh952-user-response-contract-inventory.md:9-12`
  - `docs/proposals/gh952-user-response-contract-design.md:6-8`
- Consequently, the ownership-conflicting task claims at `tasks.md:44-50,54-59,69` are not ordinary unbound task text: they are inside the verified checkpoint relationship. This includes SemTeams implementation/testing, cross-repository archive gating, and downstream-review requirements.
- The implementation evidence also records SemTeams/tag/adoption as pending and final SemStreams review as pending at `implementation-evidence.md:109-116`; that evidence file is checkpoint-covered too.
- The current capability spec remains absent at `openspec/specs/user-response-subject-ownership/spec.md`. The active delta exists only inside the checkpoint-covered package.
- PR #966 merged at current HEAD with green CI and no GitHub review objects.

`durable-max-delivery-occurrences`

- Framework implementation exists:
  - fixed bounded stream declaration:
    `config/streams.go:184-196,271-293`
  - operator/component collision handling:
    `config/stream_bounds.go:228-245`
  - durable typed observer, metrics, disposition, and fixed durable:
    `internal/maxdelivery/observer.go:24-104,119-215,217-260`
  - both binaries:
    `cmd/semstreams/main.go:145-162`,
    `cmd/e2e-semstreams/main.go:124-140`
- The only unchecked task is missing independent SemStreams review:
  `openspec/changes/durable-max-delivery-occurrences/tasks.md:34-45`.
- The change modifies two capabilities, not only `max-delivery-observability`.
- The complete `stream-provisioning` delta at `openspec/changes/durable-max-delivery-occurrences/specs/stream-provisioning/spec.md:1-31` requires:
  - framework ownership of ledger name, subject, bounds, storage, retention, discard, and replicas;
  - no adopter configuration;
  - provisioning before component consumers;
  - pre-I/O rejection of operator or component declarations named `MAX_DELIVERY_EVENTS`, even when values match;
  - loud boot failure when restrictive permissions are insufficient.
- Its runtime spelling exists at:
  - fixed declaration: `config/streams.go:184-196,271-293`
  - operator collision: `config/stream_bounds.go:228-245`
  - component-derived collision: `config/stream_bounds.go:259-303`
  - observer binding: `internal/maxdelivery/observer.go:217-260`
- The current `openspec/specs/stream-provisioning/spec.md` does not contain this fixed-ledger contract. The search
  `rg -n 'MaxDeliver advisory ledger|MAX_DELIVERY_EVENTS|fixed bounded ledger|advisory configuration|fixed-consumer permission|fixed framework stream' openspec/specs/stream-provisioning/spec.md`
  returned no matches.
- Adjacent overlap exists with the current capability's general rule that stream bounds must be declared at a named source and not appear as silent defaults: `openspec/specs/stream-provisioning/spec.md:20-23,70-113`. The active delta's framework-owned fixed declaration is the only specification spelling that explains how `MAX_DELIVERY_EVENTS` relates to that rule; it has not been promoted.
- No current capability spec exists at `openspec/specs/max-delivery-observability/spec.md`; the fixed-ledger requirement is also absent from current `stream-provisioning` capability truth.
- PR #948 merged with green CI and no GitHub review objects.

`normalize-agent-terminal-settlement`

- Framework implementation exists:
  - shared repo-internal normalizer:
    `internal/agentterminal/terminal.go:1-4,15-41,64-181`
  - route reconciliation, stable response identity, synchronous PubAck:
    `processor/agentic-dispatch/terminal_settlement.go:16,38-80,110-152,155-209`
  - unlimited retained terminal consumers:
    `processor/agentic-dispatch/component.go:407-425,456-474`
  - representative existing callback fixture:
    `test/compat/semteams/agentrun_terminal_compat_test.go:1-80`
- SemStreams review is checked complete at
  `openspec/changes/normalize-agent-terminal-settlement/tasks.md:78-85`.
- Its sole remaining task is actual SemTeams behavioral evidence:
  `openspec/changes/normalize-agent-terminal-settlement/tasks.md:48-58`.
- No current capability spec exists:
  `openspec/specs/agentic-terminal-events/spec.md` is absent.
- PR #953 merged with green CI and no GitHub review objects. The task ledger, rather than GitHub, records the independent SemStreams review.

`stream-capacity-rejection-is-circuit-neutral`

- Framework implementation exists:
  - private exact typed classifier and shared accounting:
    `natsclient/client.go:309-334`
  - sync and async accounting seams:
    `natsclient/client.go:1035-1053,1094-1156`
  - exact positive and false-case unit coverage:
    `natsclient/stream_capacity_circuit_test.go:13-84`
  - real-NATS coverage exists in:
    `natsclient/stream_capacity_circuit_integration_test.go:14-138`
- Current `nats-streaming` spec does not contain the three exact `10077` capacity descriptions. Search:
  `rg -n 'maximum bytes exceeded|maximum messages per subject exceeded' openspec/specs/nats-streaming/spec.md`
  returned no matches.
- The only unchecked task is missing independent review and recorded integration-gate provenance:
  `openspec/changes/stream-capacity-rejection-is-circuit-neutral/tasks.md:12-15`.
- PR #947 merged with green CI and no GitHub review objects.

`post-g-tag-safety-closeout`

- Its implementation and truth-correction slices P through PG are checked complete. Candidate/publication tasks E.1–E.10 remain textually unchecked:
  `openspec/changes/post-g-tag-safety-closeout/tasks.md:120-158`.
- The twelve-file package is checksum-covered:
  `openspec/changes/post-g-tag-safety-closeout/manifest.sha256:1-12`.
- The post-beta.160 checkpoint records that E.1–E.10 were externally completed for candidate `8403a221…`, while intentionally preserving the unchecked manifest-covered task file:
  `docs/proposals/post-beta160-repository-truth-checkpoint.md:9-39`.
- The same checkpoint explicitly says those historical artifacts remain frozen and are not proof for another candidate:
  `docs/proposals/post-beta160-repository-truth-checkpoint.md:37-39`.
- Current spec truth remains incomplete:
  - `release-candidate-proof` and `rule-action-observability` current specs are absent.
  - current `entity-id-contract` still requires destructive wipe/reseed at lines `323,339,361`.
  - current `graph-index` still carries coordinated wipe/reseed language at lines `233-256,293-303,345`.
  - the post-G deltas instead describe newly provisioned storage:
    `openspec/changes/post-g-tag-safety-closeout/specs/entity-id-contract/spec.md:3-23`,
    `.../graph-index/spec.md:3-57`.
  - current graph-clustering, graph-embedding, and framework-composition specs do not contain the post-G incomplete-candidate, instance-exact, and `walk_seeds` additions.

`semantic-tier-split`

- Both proposal and task ledger say `SUSPENDED AND FROZEN`, and explicitly forbid implementation, archive, or promotion:
  `openspec/changes/semantic-tier-split/proposal.md:3-14`,
  `openspec/changes/semantic-tier-split/tasks.md:3-12`.
- No task has been reopened or completed.

### 2. Current spellings of the modeled facts

- Repository task truth: the six active change directories listed above.
- Runtime truth: implementation locations enumerated under each change.
- Capability truth: current specs plus the still-unpromoted active deltas.
- Review truth:
  `gh pr view 947 948 953 966 --json ...reviews,statusCheckRollup` returned green CI and `reviews: []` for all four PRs.
- Historical release truth:
  `post-g-tag-safety-closeout/manifest.sha256` plus
  `docs/proposals/post-beta160-repository-truth-checkpoint.md`.
- Downstream adoption truth:
  issue #753 remains the owner-managed adoption tracker; its 2026-07-30 ruling says framework work archives when SemStreams work and migration guidance are complete.
- Migration truth:
  `docs/operations/migration-beta160-user-response-subjects.md:7-36` already names downstream actions and the hard repository boundary.

### 3. Adjacent claims and conflicts

- Hard repository boundary:
  `AGENTS.md:38-46` permits sister inspection but prohibits mutation and assigns implementation/validation to downstream owners.
- Adopter seam rule:
  `AGENTS.md:48-65`.
- Issue #753 contains two distinct live claims:
  1. Its owner ruling says SemStreams owes breaking notes and migration guidance, sister conformance belongs to sister owners, and adoption does not block framework archive.
  2. Its "What adoption looks like" section instructs each sister to "wipe incompatible NATS state and reseed."
- The second claim conflicts explicitly with post-G Decision G:
  - Every adoption starts on newly provisioned NATS storage:
    `openspec/changes/post-g-tag-safety-closeout/design.md:274-283`.
  - There is no existing state to migrate, preserve, wipe, or reseed:
    `design.md:280-283`.
  - Discovery of retained deployed state stops only that adoption for a separate owner-reviewed migration or recovery design:
    `design.md:282-293,340-344`.
  - Release publication performs no destructive storage operation:
    `design.md:337-342`.
  - The same premise appears in:
    - `post-g-tag-safety-closeout/proposal.md:55-58,98-103`
    - `post-g-tag-safety-closeout/tasks.md:117-131,148-158`
    - `post-g-tag-safety-closeout/candidate-evidence.md:97-105,123-132`
    - `docs/adr/090-authoritative-current-state-and-materialized-views.md:9-11`
    - `.agents/contracts/semstreams-architect.md:140-145`
- The conflict is not merely historical wording inside an annotated document: issue #753 is open and presents wipe/reseed as its live adoption checklist.
- Additional adjacent evidence:
  - Issue #753 links `docs/operations/31-sister-repo-cutover-checklist.md`, but that file is absent on current main.
  - `docs/operations/29-entity-id-contract-clean-cutover.md:3-7,156-161` labels its destructive body historical and states current adoption uses newly provisioned storage without wipe/reseed.
  - `docs/operations/migration-canonical-entity-id-contract.md:3-7` likewise labels its body historical, while its operational section still contains destructive instructions at lines `35-43`.
  - `docs/operations/17-predicate-cutover-clean-wipe.md:1-5` limits its destructive procedure to typed poison recovery, not stable-release adoption.
- Thus the current repository/external guidance inventory contains four state-action spellings:

  | Spelling | Current claim |
  |---|---|
  | Issue #753 adoption section | Wipe incompatible NATS state and reseed. |
  | Post-G Decision G and architect contract | Start on newly provisioned storage; do not wipe/reseed absent state; stop on discovered retained state. |
  | Annotated historical operation guides | Destructive bodies retained as history, explicitly not active release procedures. |
  | Current entity-ID and graph-index specs | Still contain older wipe/reseed requirements, as already inventoried. |

  No single source currently reconciles all four spellings.
- Issue #753's 2026-08-12 comment records five owner-reported beta.160 adopters with no blockers, explicitly as product evidence rather than framework proof.
- Conflicts now present in active task truth:
  - `reserve-typed-user-response-subjects` treats SemTeams implementation/review as an archive gate.
  - `normalize-agent-terminal-settlement` treats actual SemTeams behavioral evidence as its last gate.
  - those claims conflict with `AGENTS.md:38-46` and issue #753’s owner ruling.
- `post-g-tag-safety-closeout` is additionally constrained by its immutable manifest and the checkpoint’s prohibition on rewriting candidate-era evidence.
- ADR-090’s current annotation says adoption starts on newly provisioned storage and preserves its historical body:
  `docs/adr/090-authoritative-current-state-and-materialized-views.md:3-14`.
- ADR-093 still describes SemDev/SemTeams lockstep consequences:
  `docs/adr/093-typed-user-response-subject-ownership.md:48-60`; the later hard repository boundary changes who performs and validates downstream work, not the recorded historical decision.
- ADR-063’s #875 supersession already establishes exact-instance storage resolution:
  `docs/adr/063-store-substrate-and-resolver.md:3-24`.
- ADR-068 warns that its historical retention proposal is not current implementation authority:
  `docs/adr/068-graph-retention-deletion-lifecycle.md:3-41`.

### 4. Consumer at birth

The closeout itself proposes no new exported symbol, port, subject, bucket, configuration field, communication path, or runtime primitive.

Present consumers of the already-implemented surfaces are:

- Reserved response family: dispatch typed producer and external channel adapters; rule/config authors encounter validation or boot errors.
- Max-delivery ledger: internal fixed observer; operators consume its Prometheus counter and structured ERROR log.
- Terminal normalization: dispatch, AgentRun, and OTel consume the internal projection; external adopters retain the existing AgentRun callback type.
- Capacity classification: existing `natsclient` sync, acknowledged, async, and batch publish callers receive unchanged errors.
- Post-G graph changes: existing clustering, embedding, research-graph, rule-metric, and release-owner paths.
- No zero-consumer exported closeout surface was found.

## Same-class collision table: durable MaxDeliver occurrence capture

| Dimension | Inventory evidence |
|---|---|
| Semantic class | Durable ledger of NATS server MaxDeliver exhaustion occurrences, distinct from consumer retry configuration or a current parked-message count. |
| Owners | NATS server writes the advisory; central stream provisioning owns storage; `internal/maxdelivery` owns observation and settlement; both binaries own lifecycle binding. |
| Catalogs | The fixed runtime catalog entry is `config/streams.go:184-196,271-293`; collision resolution is `config/stream_bounds.go:228-303`. The corresponding active capability delta is `openspec/changes/durable-max-delivery-occurrences/specs/stream-provisioning/spec.md:1-31`. Current `openspec/specs/stream-provisioning/spec.md` has no `MAX_DELIVERY_EVENTS` or fixed-ledger requirement. |
| Status | No readiness/status key. Operator state is exposed through three bounded metrics and structured ERROR logs at `internal/maxdelivery/observer.go:119-189`. |
| Lifecycle | Capture is provisioned before observer start; observer uses DeliverAll, explicit ACK, unlimited delivery, and stop preserves the durable floor: `internal/maxdelivery/observer.go:217-260`. |
| Ownership | One fixed durable identity shared across replicas: `internal/maxdelivery/observer.go:29-35,251-260`. R=1 storage is explicit at `config/streams.go:188-196`. |
| Readers | Internal observer; E2E and integration tests; operators through metrics/logs. |
| Writers | NATS server advisory writer; poison/test writers exist only in tests. No application writer is declared. |
| Recovery | Retained pre-bind occurrence replay; telemetry failure NAK; poison telemetry then ACK; ACK/NAK settlement failure leaves the durable floor unclaimed: `internal/maxdelivery/observer.go:191-215`. |

Search across `config`, `internal/maxdelivery`, `cmd`, `natsclient`, `processor`, `test`, and `docs/operations` found no second durable MaxDeliver occurrence ledger. Numerous `MaxDeliver` fields configure individual consumer retry posture; they do not persist the server occurrence and therefore are adjacent owners, not duplicate ledgers.

## Adopter seam inventory

| Specific adopter | What they must know | If they do nothing | Discovery | What they should have to know |
|---|---|---|---|---|
| External rule/config author | `user.response.>` is typed-only; retired `violations.notify_user` must be deleted. | Fixed rule subjects fail validation; dynamic subjects fail before publication; retired config fails boot. | Boot/typed validation error, then migration document. | Only the named typed interface; no product payload unions or bridge knowledge. |
| Restrictive NATS operator or component author | No ledger declaration is required or allowed; the runtime principal still needs the fixed stream, consumer, inbox, and ACK permissions. | An omitted declaration succeeds through framework provisioning. A colliding declaration or insufficient permission fails boot. | Boot error and the active `stream-provisioning` delta; the current capability spec does not yet disclose this contract. | No stream identity, subject, bounds, replica, or durable-name prediction; only the deployment permission boundary. |
| AgentRun callback adopter | Existing callback receives success, failure, and cancellation production envelopes; cancellation travels on the completion subject. | Existing API compiles; malformed events fail closed; retention eviction limits guarantees. | Existing callback API and terminal-settlement operations document. | Existing callback type and bounded delivery declaration only. |
| `natsclient` publish caller | Nothing new: the same capacity error remains visible. | A full target stream rejects the publish, but the unrelated connection circuit remains usable. | Existing returned error/future/batch aggregate. | Nothing about internal breaker classification. |
| StorageReference producer/operator | The exact logical `StorageInstance` must be live; another wired store is not equivalent. | Body is visibly excluded while inline identity may continue; resolved-store read errors remain failures. | Runtime metric/log and ADR-063/operator guidance. | Existing logical instance identity, not bucket reconstruction or fallback prediction. |
| Sister-repository owner adopting the framework | The ownership ruling assigns product implementation/testing to them. Separately, post-G Decision G says adoption starts on newly provisioned storage and stops for owner review if retained deployed state exists. Issue #753 currently gives the contrary instruction to wipe and reseed. | Following #753 literally can authorize a destructive action that Decision G forbids for stable adoption; following Decision G avoids prediction but leaves the open issue's checklist contradicted. | Conflicting documentation and issue text only. The linked cross-cutting checklist is absent. | One unambiguous current storage premise and the product-owned migration boundary. |
| Release owner | Frozen beta.160 evidence proves only candidate `8403a221…`; it cannot authorize a later candidate. | Historical evidence remains valid, but provides no proof for a different SHA. | Immutable Release assets and post-beta.160 checkpoint. | Candidate identity and current gate result, without editing historical packages. |

No inventory row asks an adopter to predict a framework-owned limit or state before acting. The known bills are subject/interface ownership, permissions, exact logical storage identity, and bounded retention guarantees.

## Evidence state at this baseline

- The #952 checkpoint currently verifies all ten tracked artifacts, including `tasks.md`, its delta, migration guidance, ADR, and implementation evidence. Ownership-conflicting task truth is therefore part of the immutable evidence relationship.
- The #948 current-spec gap affects both `max-delivery-observability` and `stream-provisioning`; neither fixed-ledger requirement is fully represented in current capability truth.
- Issue #753's archive-boundary ruling and its wipe/reseed instruction must be inventoried separately: the former aligns with the hard repository ownership boundary, while the latter conflicts with Decision G.
- Current-main tree is clean.
- PR CI is green for #947, #948, #953, and #966.
- GitHub records no formal review on any of those PRs.
- Active task truth records an independent review only for terminal normalization.
- No fresh current-main local unit, race, integration, or E2E result was produced during this inventory.
- Integration/E2E evidence was intentionally not refreshed because the shared Docker host is in use.
- Frozen beta.160 candidate and publication proof remains externally complete and checksum-addressed; it is not fresh beta.161 proof.
- Sister adoption evidence is product-owned and non-blocking under issue #753 and `AGENTS.md:38-46`.
- No Docker, integration, E2E, sister-repository, or mutation work was performed while making these corrections.

## Open evidence questions for inventory review

- Whether any current surface spelling or active/archived overlap was missed.
- Whether the task-ledger conflicts with the hard repository boundary are correctly classified as ownership conflicts rather than missing framework implementation.
- Whether the post-G checksum package and post-beta.160 checkpoint have any additional immutable linkage that must be inventoried before closeout design.
- Whether the task-recorded independent terminal review is sufficient evidence despite the empty GitHub review collection.
- Which SemStreams-local review and current-main test evidence is still absent; no sufficiency ruling is made in this inventory.

The caller or technical writer must now materialize this text as a line-addressable artifact, record baseline `1089545bc5eadb78facf657e89a56d61072df6ba` and its content hash, and submit it for independent SemStreams inventory review. Target-state work remains gated on `INVENTORY PASS`.
