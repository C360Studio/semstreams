# Beta.161 OpenSpec closeout design

Baseline: 1089545bc5eadb78facf657e89a56d61072df6ba

Status: design draft after INVENTORY PASS; pending independent design review and owner acceptance

Accepted inventory dependency SHA-256: b029f68cea409c93517384e3e9152f4508d7a52133b772d32d72e23968fcdd50

Body SHA-256: d8ab2c70c77f3be2aa2cf74215dffe51c78166b9dc886f7ce7c5ab24b841e060

Hash method:

    sed -n '/^## Design body$/,$p' <file> | tail -n +2 | shasum -a 256

## Design body

## Accepted inventory

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

## Draft design handoff: beta.161 OpenSpec truth closeout

Status: design draft after INVENTORY PASS; pending independent design review and owner acceptance.

Accepted inventory dependency: the complete Accepted inventory section above is copied verbatim from
docs/proposals/beta161-openspec-closeout-inventory.md. Its verified body SHA-256 is
b029f68cea409c93517384e3e9152f4508d7a52133b772d32d72e23968fcdd50.

No decision skill triggers: this closeout adds no communication path, payload, orchestration behavior, or query access.

## Options

1. **Archive every original change normally.** Rejected: the immutable #952 package would promote product-owned
   requirements, while `post-g-tag-safety-closeout` fails the OpenSpec merge guard because four frozen deltas omit
   ten current scenario identities.
2. **Rewrite the frozen #952 and post-G artifacts.** Rejected: this invalidates their checkpoint or manifest evidence.
3. **Create merge-safe companion-current-truth changes, archive each companion normally, then archive each frozen
   original with `--skip-specs`.** Recommended. This preserves all frozen bodies and hashes while promoting only
   reviewed framework-owned target truth.
4. **Defer the two frozen closeouts.** Safe but leaves known archive debt and does not complete the beta.161 closeout.

## Recommendation

Use option 3 for both immutable packages:

- `beta161-user-response-current-truth` owns the framework-only #952 promotion.
- `beta161-post-g-current-truth` owns the merge-safe post-G promotion.
- Archive both companions normally.
- Archive `reserve-typed-user-response-subjects` and `post-g-tag-safety-closeout` with `--skip-specs`.
- Leave all ten #952 checkpointed artifacts and all twelve post-G manifest bodies byte-unchanged.

This remains a draft owner decision, not a binding ruling.

## Per-change target state

### stream-capacity-rejection-is-circuit-neutral

Before archive:

- Replace its mutable nats-streaming modified requirement with the union of existing current truth and the new exact
  capacity classification. The present delta would otherwise replace and lose the current connection-outage and
  successful-enqueue scenarios.
- Mark its final task complete with the supplied session-local evidence. Do not claim a retroactive GitHub PR review.

The replacement requirement must retain:

- trace injection, deterministic Nats-Msg-Id, and circuit-on-entry invariants;
- async successful-enqueue liveness reset;
- connection-outage failure accounting;
- the existing outage and successful-enqueue scenarios;
- exact neutral set: typed 10077 with descriptions:
  - maximum bytes exceeded
  - maximum messages exceeded
  - maximum messages per subject exceeded
- caller-visible sync, acknowledged, future, and batch errors;
- all false cases remaining connection failures;
- the new full-stream and similar-error scenarios.

Archive normally:

    openspec archive -y stream-capacity-rejection-is-circuit-neutral

Expected current-spec delta:

- modify openspec/specs/nats-streaming/spec.md
- create openspec/changes/archive/<date>-stream-capacity-rejection-is-circuit-neutral/

Evidence to record:

- reviewer: canonical semstreams-reviewer task /root/gh952_semdev_review
- verdict: APPROVE, no findings
- focused race: PASS, 1.333s
- real-NATS command:

      go test -race -tags=integration ./natsclient \
        -run '^TestIntegration_StreamCapacityRejectionIsCircuitNeutral$' -count=1

- result: PASS, package github.com/c360studio/semstreams/natsclient, 4.276s
- GitHub PR #947 review collection remains empty; this is current-session independent review.

### durable-max-delivery-occurrences

Mark task 4.4 complete with the supplied evidence, then archive normally:

    openspec archive -y durable-max-delivery-occurrences

Expected current-spec deltas:

- add openspec/specs/max-delivery-observability/spec.md
- modify openspec/specs/stream-provisioning/spec.md
- create openspec/changes/archive/<date>-durable-max-delivery-occurrences/

The promoted stream-provisioning requirement must include all lines 1–31 of the active delta: framework ownership, no
adopter declaration, boot ordering, exact collision failure, and restrictive-permission failure.

Evidence to record:

- reviewer: canonical semstreams-reviewer task /root/gh952_semdev_review
- verdict: APPROVE, no findings
- focused race:
  - config: PASS, 1.552s
  - internal/maxdelivery: PASS, 1.402s
  - scenario helpers: PASS, 1.404s
- sequential real-NATS gates:

      go test -race -tags=integration ./config -count=1
      go test -race -tags=integration ./internal/maxdelivery -count=1

- results:
  - config: PASS, 28.088s
  - internal/maxdelivery: PASS, 9.644s
  - restrictive authorization and disposable three-node proof included
- strict OpenSpec: 48/48
- reviewer accepted the previously recorded task 4.3 assembled task e2e:core proof and found no archive-only reason to
  rerun it.
- GitHub PR #948 review collection remains empty; do not relabel session evidence as GitHub review.

### normalize-agent-terminal-settlement

Its sole unchecked SemTeams task is product-owned under AGENTS.md:38-46 and issue #753. Keep the recognized unchecked
marker and replace its text with:

```markdown
- [ ] **SUPERSEDED AS AN ARCHIVE GATE — PRODUCT-OWNED/NONBLOCKING.**
  The actual SemTeams path was not verified in this repository. The sister-repository
  cutover remains product-owned and is tracked through the durable operator checklist
  and issue handoff; it does not block the SemStreams archive.
```

> OpenSpec 1.7.0 recognizes only checked and unchecked task truth here. The task therefore remains `[ ]`; the explicit
> disposition, rather than an unsupported marker, explains why `-y` archive confirmation is intentional. Do not mark
> it `[x]` without actual sister-repository evidence.

Then archive normally:

    openspec archive -y normalize-agent-terminal-settlement

Expected current-spec delta:

- add openspec/specs/agentic-terminal-events/spec.md
- create openspec/changes/archive/<date>-normalize-agent-terminal-settlement/

Do not add SemTeams product behavior to the current capability spec.

### reserve-typed-user-response-subjects

Do not edit any of its ten checkpoint-covered artifacts.

Create a temporary companion change:

    openspec/changes/beta161-user-response-current-truth/
      proposal.md
      design.md
      tasks.md
      specs/user-response-subject-ownership/spec.md

The companion delta copies the framework-owned requirements from the immutable #952 delta:

- The user-response subject family SHALL carry one registered type.
- Every arbitrary rule publisher SHALL enforce the reservation twice.
- Governance SHALL expose no orphan user-notification surface.
- Message-logger declaration truth SHALL reflect governance removal.

It must omit:

- SemDev park-post SHALL be an exact product-owned JetStream request.
- SemTeams adoption SHALL remove only its unconsumed flat writers.

Its fresh-state requirement must be reframed as SemStreams-owned truth:

> SemStreams-owned sources, ports, schemas, configurations, fixtures, and tests SHALL use the typed response contract
> and start on newly provisioned NATS storage. No legacy reader, flat/typed union, dual format, dual subscription,
> alias, bridge, forwarding subject, retained-state migration, or rollback lane SHALL exist. Downstream product
> migration and validation belong to the downstream owner and SHALL NOT block SemStreams capability archive.
> Discovery of retained deployed state SHALL stop only that adoption for separate owner-reviewed migration or
> recovery design.

Retain the unmigrated-rule boot-failure scenario. Do not carry the historical SemDev landing gate into current
capability truth.

Archive the companion normally:

    openspec archive -y beta161-user-response-current-truth

Then archive the immutable historical package without promotion:

    openspec archive -y --skip-specs reserve-typed-user-response-subjects

Expected results:

- add framework-only openspec/specs/user-response-subject-ownership/spec.md
- archive the companion change normally
- archive the original #952 package unchanged as historical evidence
- no product-owned requirement enters current SemStreams specs

Checkpoint mechanics:

1. Before either archive, verify the original ten artifacts directly:

       grep -v '^#' \
         openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256 |
         sha256sum -c -

2. Do not edit or regenerate checkpoint.sha256.
3. After the move, verify the five repository-global paths directly and verify the five moved change-local bodies by
   substituting only the archived directory prefix at verification time.
4. Record that mapped verification command and all ten OK results in the new closeout evidence document.
5. Treat path relocation as archive transport, not new artifact content.

Unchecked #952 task disposition belongs in the new evidence document:

- 5.1–5.4: downstream-owned SemTeams adoption, not performed and not an archive blocker.
- 6.2: record a fresh SemStreams-only negative search for bridge/alias/union/dual-format implementation.
- 6.4: its cross-repository archive gate is superseded by the hard repository boundary and #753's owner ruling.
- 7.6: require final SemStreams review of the exact archive diff; downstream reviews remain downstream-owned.

### post-g-tag-safety-closeout: immutable evidence plus merge-safe companion

The original change is an immutable evidence package. Its `manifest.sha256` covers twelve bodies:

- `candidate-evidence.md`
- `design.md`
- `disposition-ledger.md`
- `proposal.md`
- seven capability deltas
- `tasks.md`

None of those twelve bodies may change.

A normal archive of the original is not merge-safe. Compared with current capability truth, its frozen modified
deltas omit exactly ten current scenario identities:

| Capability | Current scenarios omitted by frozen delta |
|---|---:|
| `entity-id-contract` | 2 |
| `framework-composition` | 3 |
| `graph-clustering` | 2 |
| `graph-index` | 3 |

Create `openspec/changes/beta161-post-g-current-truth/` with exactly these companion artifacts:

```text
proposal.md
design.md
tasks.md
specs/entity-id-contract/spec.md
specs/framework-composition/spec.md
specs/graph-clustering/spec.md
specs/graph-embedding/spec.md
specs/graph-index/spec.md
specs/release-candidate-proof/spec.md
specs/rule-action-observability/spec.md
```

The companion must:

- cite the accepted beta.161 inventory and the original post-G manifest;
- state that it changes specification truth only, with no runtime or sister-repository work;
- preserve all seven frozen delta outcomes;
- preserve every scenario identity already present in each of the four modified current capabilities;
- harmonize retained scenario bodies with Decision G rather than restoring destructive migration behavior;
- add the three remaining post-G capabilities without weakening their frozen requirements;
- use `[x]` only after each companion artifact, validation, and review action has actually completed.

Archive the companion normally, then archive the immutable original with `--skip-specs`.

#### Merge-safe post-G target truth

The companion shall express this union:

- **`entity-id-contract`**
  - Retain both current scenario identities.
  - Express clean adoption as newly provisioned canonical state, never a framework-owned wipe.
  - Retain malformed-current-state fail-closed behavior.
  - Add the frozen retained-state rule: retained state blocks only the affected adoption.
  - Preserve the bound-gate scenarios and real-NATS proof semantics.
- **`framework-composition`**
  - Retain the three current scenarios for absent, partial, and complete graph research configuration.
  - Add the frozen direct-construction and walked-composition behavior.
  - Preserve optional composition and explicit failure behavior.
- **`graph-clustering`**
  - Retain the current reader-mid-rebuild, stale-community-removal, and removal-failure scenario identities.
  - Preserve the frozen candidate publication, permanent rejection, partial mapping, removal attempt, removal failure,
    and empty-graph outcomes.
  - Reconcile duplicate wording into one coherent requirement without dropping either current or frozen behavior.
- **`graph-index`**
  - Retain all current scenario identities across activation, key cutover, and context behavior.
  - Rewrite any stale destructive-cutover premise to Decision G: fresh storage may activate; retained or contrary state
    stops the affected adoption for separate owner review.
  - Preserve the frozen retained-state and contrary-state outcomes.
  - Preserve readiness, watermark, no-premature-ready, no-context-bucket, and hierarchy-provenance behavior.
- **`graph-embedding`**
  - Carry forward the frozen added requirement without semantic reduction.
- **`release-candidate-proof`**
  - Carry forward the frozen added requirement without semantic reduction.
- **`rule-action-observability`**
  - Carry forward the frozen added requirement without semantic reduction.

Before archive, record a scenario-preservation table proving that the companion closes the exact
`2 + 3 + 2 + 3 = 10` merge-guard omissions.

#### Post-G checksum mechanics

Verify the frozen package before movement:

```bash
(
  cd openspec/changes/post-g-tag-safety-closeout
  sha256sum -c manifest.sha256
)
```

After its `--skip-specs` archive, verify the same relative manifest from the generated archive directory:

```bash
(
  cd openspec/changes/archive/<actual-date>-post-g-tag-safety-closeout
  sha256sum -c manifest.sha256
)
```

No path rewriting is required because the post-G manifest uses paths relative to its package root.

Keep the existing mapped-path procedure for the #952 checkpoint: its ten recorded active-tree paths must be mapped to
the generated archive directory before comparing hashes. A mismatch in either package is a hard stop.

The unchecked E.1–E.10 boxes remain historical candidate-era text. Their beta.160 completion remains recorded only in
`docs/proposals/post-beta160-repository-truth-checkpoint.md` and the immutable external Release assets. No beta.160
proof is copied forward as beta.161 proof.

### semantic-tier-split

No file or task change. Do not archive or promote it.

Post-transaction verification must show it as the only active pre-existing change:

    openspec list --changes
    git diff --exit-code -- openspec/changes/semantic-tier-split

The second command must be clean.

## Repository file delta

Add these durable documents:

- `docs/proposals/beta161-openspec-closeout-design.md`
- `docs/proposals/beta161-openspec-closeout-evidence.md`
- `docs/operations/31-sister-repo-cutover-checklist.md`

Add two temporary companion changes:

- four artifacts under `beta161-user-response-current-truth`;
- ten artifacts under `beta161-post-g-current-truth`.

Both companions are consumed by archive and must not remain active.

Modify before archive:

- `openspec/changes/stream-capacity-rejection-is-circuit-neutral/specs/nats-streaming/spec.md`
- `openspec/changes/stream-capacity-rejection-is-circuit-neutral/tasks.md`
- `openspec/changes/durable-max-delivery-occurrences/tasks.md`
- `openspec/changes/normalize-agent-terminal-settlement/tasks.md`

Generated by normal archive:

- `openspec/specs/nats-streaming/spec.md`
- `openspec/specs/stream-provisioning/spec.md`
- `openspec/specs/max-delivery-observability/spec.md`
- `openspec/specs/agentic-terminal-events/spec.md`
- `openspec/specs/user-response-subject-ownership/spec.md`
- `openspec/specs/entity-id-contract/spec.md`
- `openspec/specs/framework-composition/spec.md`
- `openspec/specs/graph-clustering/spec.md`
- `openspec/specs/graph-embedding/spec.md`
- `openspec/specs/graph-index/spec.md`
- `openspec/specs/release-candidate-proof/spec.md`
- `openspec/specs/rule-action-observability/spec.md`
- exactly seven dated archive directories: three mutable originals, two companion-current-truth changes, and two
  immutable originals archived with `--skip-specs`.

Must remain byte-unchanged:

- all ten #952 checkpoint-covered artifacts before and after their move;
- all twelve post-G manifest-covered artifacts before and after their move;
- `docs/proposals/post-beta160-repository-truth-checkpoint.md`;
- every `semantic-tier-split` artifact.

No Go, Svelte, runtime, Docker, sister-repository, or generated schema file enters scope. `semantic-tier-split` remains
active and untouched.

## Current-spec `Purpose` mechanics

After each normal archive materializes its new current capability spec, insert the following owner-reviewable
`Purpose` draft before final strict validation:

### `max-delivery-observability`

```markdown
## Purpose

Provide bounded, framework-owned capture and durable observation of NATS MaxDeliver exhaustion occurrences without changing component disposition or readiness.
```

### `agentic-terminal-events`

```markdown
## Purpose

Define repository-internal terminal decoding, routing reconciliation, idempotent response publication, and bounded-retention settlement shared by SemStreams terminal consumers.
```

### `user-response-subject-ownership`

```markdown
## Purpose

Define framework ownership of `user.response.>` as the single registered `agentic.user_response.v1` family, including framework rule guards and governance removal.
```

### `release-candidate-proof`

```markdown
## Purpose

Define evidence and identity requirements that bind deterministic pre-tag proof, independent review, the exact candidate SHA, publication attestation, and fresh-storage truth.
```

### `rule-action-observability`

```markdown
## Purpose

Provide operator visibility into rule action-gate admission and define the optional rule-trigger notification contract.
```

Preserve the existing `Purpose` text of every modified current capability, including `graph-embedding`. Do not add or
update any other current capability. All five drafts remain subject to owner approval.

## Fresh-state adopter guide draft

docs/operations/31-sister-repo-cutover-checklist.md:

~~~markdown
# Sister-repository adoption checklist

SemStreams agents mutate only SemStreams. Downstream owners implement and validate their own adoption.

For a published breaking SemStreams version:

1. Update the downstream's owned literals, patterns, configuration, schemas, tools, fixtures, seed data, and queries.
2. Start the adopting deployment on newly provisioned NATS storage.
3. Do not migrate, preserve, wipe, or reseed absent state as part of release adoption.
4. If retained deployed NATS state is discovered, stop only that adoption. Perform no destructive action; obtain a
   separate owner-reviewed migration or recovery design.
5. Prove cold-start readiness and run the downstream product's native contract and E2E gates.

SemStreams provides no compatibility alias, dual reader, dual writer, online conversion, or rollback lane for these
pre-v1 clean breaks.

Historical destructive cutover documents remain evidence of earlier beta procedures. They are not current
stable-release adoption instructions.
~~~

## Exact archive transaction

1. Verify the active-tree #952 checkpoint against all ten recorded paths.
2. Verify the active post-G package with `sha256sum -c manifest.sha256`.
3. Archive the capacity change normally.
4. Archive the max-delivery change normally.
5. Draft the new current `max-delivery-observability` `Purpose`.
6. Archive the normalize change normally.
7. Draft the new current `agentic-terminal-events` `Purpose`.
8. Archive `beta161-user-response-current-truth` normally.
9. Draft the new current `user-response-subject-ownership` `Purpose`.
10. Archive `reserve-typed-user-response-subjects` with `--skip-specs`.
11. **Immediately** map the ten checkpoint paths to the generated #952 archive directory and verify every recorded
    hash. No unrelated mutation may occur between steps 10 and 11.
12. Archive `beta161-post-g-current-truth` normally.
13. Confirm that all ten previously omitted current scenario identities and all seven frozen post-G delta outcomes
    are represented.
14. Draft the new current `release-candidate-proof` and `rule-action-observability` `Purpose` texts.
15. Archive `post-g-tag-safety-closeout` with `--skip-specs`.
16. Immediately verify its twelve-body manifest from the generated archive directory.
17. Run final strict validation and repository-truth review.

Executable archive commands, in transaction order:

```bash
openspec archive -y stream-capacity-rejection-is-circuit-neutral
openspec archive -y durable-max-delivery-occurrences
openspec archive -y normalize-agent-terminal-settlement

openspec archive -y beta161-user-response-current-truth
openspec archive -y --skip-specs reserve-typed-user-response-subjects

openspec archive -y beta161-post-g-current-truth
openspec archive -y --skip-specs post-g-tag-safety-closeout
```

The five `Purpose` drafts are required interstitial edits at the numbered points above; they must all exist before:

```bash
openspec validate --strict --all
```

## Validation and proof gates

Pre-archive:

    # Accepted inventory identity
    sed -n '/^## Inventory body$/,$p' \
      docs/proposals/beta161-openspec-closeout-inventory.md |
      tail -n +2 |
      shasum -a 256

    # Immutable package checks
    grep -v '^#' \
      openspec/changes/reserve-typed-user-response-subjects/checkpoint.sha256 |
      sha256sum -c -

    (
      cd openspec/changes/post-g-tag-safety-closeout
      sha256sum -c manifest.sha256
    )

    task openspec:queue
    openspec validate --strict --all
    git diff --check

After every archive:

    openspec validate --strict --all
    git diff --check

Final validation:

    openspec validate --strict --all

Additionally require:

- both companion changes validate strictly before archive;
- the post-G companion's modified requirements contain every current scenario identity, with zero omissions;
- the post-G companion represents all seven frozen deltas;
- the #952 ten-artifact checkpoint verifies before and after movement;
- the post-G twelve-body manifest verifies before and after movement;
- final current specs contain the expected requirement union and owner-reviewed `Purpose` text;
- only `semantic-tier-split` remains active;
- exactly seven closeout archive directories were created for this transaction.

No Docker, integration, E2E, sister-repository, or runtime execution is required solely for this archival transaction.

Final review must be by `semstreams-reviewer` against the exact complete archive diff:

- [ ] No unsupported `[~]` marker remains.
- [ ] The SemTeams item remains `[ ]` with explicit superseded, product-owned, nonblocking disposition.
- [ ] Both immutable originals remain byte-identical to their accepted hash evidence.
- [ ] The #952 companion promotes framework-owned truth only.
- [ ] The post-G companion closes all ten merge-guard omissions.
- [ ] Every post-G current scenario identity remains represented with Decision G semantics.
- [ ] All seven post-G delta outcomes are present in current truth.
- [ ] Both immutable originals were archived with `--skip-specs`.
- [ ] All five newly materialized current specs have owner-reviewed `Purpose` text: `max-delivery-observability`,
      `agentic-terminal-events`, `user-response-subject-ownership`, `release-candidate-proof`, and
      `rule-action-observability`.
- [ ] The #952 checkpoint verified against active paths before archive and against mapped archive paths immediately
      after the immutable original moved.
- [ ] Exactly seven archive directories were produced.
- [ ] `semantic-tier-split` is the only remaining active unrelated change.
- [ ] Reviewer evidence is described as session-local independent review, not retroactive GitHub approval.
- [ ] No runtime, Docker, sister-repository, issue mutation, or other external action occurred during design or archive
      preparation.

## GitHub issue sequencing

These are owner-authorized external mutations, not technical-writer file changes and not part of the archive commands.

### Issue #753

Keep it open. After the repository change merges, edit its adoption steps to:

~~~markdown
## What adoption looks like

Per sister repository, on its own schedule, against a published SemStreams version carrying the contracts:

1. update owned literals/patterns, configuration, schemas, tools, fixtures, seed data, and queries;
2. start on newly provisioned NATS storage — do not migrate, preserve, wipe, or reseed absent state;
3. if retained deployed NATS state is discovered, stop only that adoption and obtain a separate owner-reviewed
   migration or recovery design before any destructive action;
4. prove cold-start readiness and run that product's native contract and E2E gates.
~~~

Retain the owner ruling that sister adoption does not block SemStreams archive. Retain the link to the now-present
docs/operations/31-sister-repo-cutover-checklist.md.

### Issue #952

Do not close before merge.

After the archive/current-spec commit merges unchanged with green CI, close #952 with a comment linking:

- PR #966 implementation;
- the merged closeout commit/PR;
- openspec/specs/user-response-subject-ownership/spec.md;
- docs/operations/migration-beta160-user-response-subjects.md;
- issue #753 for downstream-owned adoption.

The closure comment must state that SemStreams framework work is complete and archived, while SemTeams adoption remains
product-owned and nonblocking. Closing #952 does not authorize any sister mutation or tag.

## Adopter seam

| Adopter | Must know | Does nothing | Discovery | Should know |
|---|---|---|---|---|
| Rule/config author | user.response.> is typed-only; remove violations.notify_user. | Static/dynamic rule targeting or retired config fails visibly. | Validation or boot error, then migration guide. | Only the typed interface and the actionable error. |
| Restrictive NATS operator | Do not declare MAX_DELIVERY_EVENTS; grant fixed framework permissions. | Omitted config works; collision or missing permission fails boot. | Boot error and current stream-provisioning spec. | Permission boundary only; no name/bounds prediction. |
| AgentRun callback adopter | Existing callback covers three terminal classes within bounded retention. | Existing API remains; invalid events fail closed. | Existing API and current terminal spec. | Existing callback contract only. |
| natsclient publisher | Nothing new. Capacity refusal remains returned to the caller. | Full stream rejects that publish without poisoning unrelated circuit health. | Existing typed/wrapped error. | Nothing about the private classifier. |
| Sister owner | Own its migration and native proof after publication; start fresh and stop on retained state. | No effect on framework archive; incompatible adoption fails in its own seams. | Cross-cutting guide and #753. | One unambiguous fresh-state rule. |
| Release owner | Frozen beta.160 proof applies only to its exact candidate. | Historical proof remains valid but cannot authorize another SHA. | Current release-candidate-proof spec and immutable Release assets. | Exact candidate identity and current evidence only. |

## Stop and rollback conditions

Stop and return to the owner if:

- any #952 checkpoint hash or post-G manifest hash fails;
- any frozen body would need editing;
- OpenSpec still reports one of the ten current scenarios as omitted;
- the post-G companion cannot preserve current scenario identity while expressing Decision G;
- any of the seven post-G delta outcomes is absent from the companion;
- either immutable original would require normal spec promotion;
- current `Purpose` text cannot be supplied without an owner ruling;
- archive output differs from the expected seven directories;
- a task would need to be marked complete without evidence;
- runtime, Docker, E2E, sister-repository, or external issue mutation becomes necessary.

Before merge, treat all archives as one uncommitted transaction; publish none of a partial sequence. Restart from the
clean accepted baseline rather than editing archived evidence into consistency.

After merge, never rewrite archived history to correct a mistake. Open a new bounded corrective OpenSpec change.

These revisions remain subject to fresh canonical design review.

## Required handoffs

1. Technical writer materializes this design with the accepted inventory verbatim and records its body hash.
2. Independent semstreams-reviewer performs pre-owner design review.
3. Owner accepts or revises the checkpoint/companion mechanics and GitHub sequencing.
4. Technical writer executes only the accepted documentation/OpenSpec transaction.
5. Independent semstreams-reviewer reviews the exact archive diff.
6. Owner merges after CI, then separately performs the #753 edit and #952 closure.

No tag, sister access, Docker operation, runtime implementation, or #963 work is included.
