## 0. Review Gate (blocks everything below)

- [x] 0.1 Adversarial 5-lens review of ADR-076 against the implemented code on
      `codex/entity-id-contract-completion`; resolve the four open questions (alert occurrence-vs-state identity,
      PackID alphabet `.`/`=`, event-type extension rule, digest v2 policy) and mark the ADR Accepted or amend the
      implementation to match the resolutions
      Evidence: ADR-076 is Accepted with all four resolutions recorded; the review also found and closed the
      missing bounded graph-event rejection metric promised by the normative contract.

## 1. Frame the Existing Implementation

The implementation exists on the branch (graph-event constructors, alert/trigger digests, PackID enforcement,
batch preflight, golden digest tests, publisher-spy zero-side-effect tests). These tasks verify it against the
accepted ADR rather than rewrite it.

- [x] 1.1 Carry the implementation into this change's PR (slice 4 of the landing map) with the complete manifest:
      `graph/event*`, `pkg/rulepack`, `frameworkcapabilities/rulepacks`, `service/component_manager.go`,
      `service/rule_pack_bind.go`, the identity/config/schema parts of `processor/rule`,
      `schemas/rule-processor.v1.json` plus regenerated schema/OpenAPI, `cmd/*` wiring, shipped configs, and their
      tests — reset out of slice 1. Predicate-lineage content from 1c8d595d goes to `predicate-contract-enforcement`,
      not here
- [x] 1.2 Verify the implemented digests, framing, constants, and PackID grammar match ADR-076 exactly as accepted,
      including any amendments from task 0.1; pin the golden 103-byte alert and 105-byte trigger identities
      Evidence: `graph/events_test.go` and `processor/rule/graph_event_identity_test.go` pin the full golden IDs,
      exact lengths, input sensitivity, and maximum-source behavior.
- [x] 1.3 Verify batch preflight atomicity in both integration modes with publisher-spy proof: `[valid, invalid]`,
      `[invalid, valid]`, and typed-nil batches fail with zero marshal/NATS/retry/callback/success-metric side
      effects; a later JSON-unencodable member may marshal an earlier frame but produces zero publication side
      effects; every rejection records exactly one bounded lane/reason metric with no identity bytes in labels
      Evidence: `processor/rule/publisher_graph_event_contract_test.go` covers both integration modes and the shared
      fire path with zero publication/generic-error counters and one `batch_preflight` rejection, including a valid
      prefix followed by a `NaN` property that fails JSON encoding before the first publish.
- [x] 1.4 Verify duplicate enabled-PackID composition rejection before binding, watching, activation, and
      publication; verify empty-PackID rejection at schema, config constructor, direct factory, and activation
- [x] 1.5 Replace this change's scaffold `graph-events` delta with the VERBATIM normative requirement block
      "Graph-event construction is canonical, deterministic, and side-effect free" lifted from the
      `entity-id-contract` delta (minus predicate-lineage clauses, which move to `predicate-contract-enforcement`);
      delete that block from the `entity-id-contract` delta in the same commit. No normative sentence is demoted to
      design or ADR prose

## 2. Migration and Cutover

- [x] 2.1 Migrate every SemStreams-local constructor call site and direct rule-event producer, including expression
      and test-rule factories; migrate every shipped rule configuration in either integration mode to an explicit
      stable PackID; no ignored constructor error remains
- [x] 2.2 Source- and compile-audit every owned repository for graph-event constructor calls, direct event
      producers, and old alert-ID assertions; migrate them to `(*Event, error)` and the digest identities; require
      every owned rule-processor config to declare a stable unique PackID and explicit graph-integration mode. Identify
      each `graph.events.*` consumer and prove first-trigger create/upsert plus repeated-trigger replacement semantics;
      a must-exist update or append-only consumer violates this contract (pre-v1 release gate, not a local merge gate)
      — **SCOPE CORRECTED 2026-07-30 (owner ruling).** SemStreams' obligation is to note the
      breaking change and publish migration guidance; **conforming to the framework is the sister
      repo's job**, and further problems they hit become new issues in this queue. Guidance is
      published (see `docs/operations/31-sister-repo-cutover-checklist.md` and the per-contract
      guides); adoption is tracked on **gh#753** and does NOT gate this archive.
- [x] 2.3 Fold the identity changes into the announced pre-v1 wipe/reseed; no compatibility constructor, dual
      identity, alias ledger, or rollback

## 3. Gates and Documentation

- [x] 3.1 Run `task lint`, `go test -race ./...`, contract tests, and `task schema:generate` drift check on the
      extracted PR
      Evidence: all named gates plus `task entity-id:audit predicate:audit predicate:test-audit` passed on
      2026-07-16; serialized real-NATS race tests passed for `processor/rule` and `service`.
- [x] 3.2 Run the affected e2e tiers (structural + agentic at minimum — rule firing to graph write to query) green
      before the BREAKING commit lands
      Evidence: structural passed 37/37 and agentic completed successfully on 2026-07-16.
- [x] 3.3 Update graph-event API docs and operations guide 30 for the constructor break, batch preflight, property
      ownership, derived
      identities, and the `pack_id` schema contract; document the default-config constructor change
- [x] 3.4 Publish the BREAKING changelog entries: `(*Event, error)` signature, legacy `alert_...` replacement,
      legacy three-part trigger replacement, universal PackID requirement, duplicate-pack composition rejection,
      unconditional disabled-mode preflight, and the `enable_graph_integration` default flip from true to false
- [x] 3.5 Before v1 release and archive, update owned-product graph-event API and operator documentation for the
      constructor, identity, PackID, configuration, and clean-cutover contracts (restores old task 6.5c)
      — **SCOPE CORRECTED 2026-07-30 (owner ruling).** SemStreams' obligation is to note the
      breaking change and publish migration guidance; **conforming to the framework is the sister
      repo's job**, and further problems they hit become new issues in this queue. Guidance is
      published (see `docs/operations/31-sister-repo-cutover-checklist.md` and the per-contract
      guides); adoption is tracked on **gh#753** and does NOT gate this archive.
- [x] 3.6 Before v1 release and archive, publish coordinated product release notes with the owned-reference update
      checklist and recorded product e2e evidence for the identity changes (restores old task 6.6a)
      — **SCOPE CORRECTED 2026-07-30 (owner ruling).** SemStreams' obligation is to note the
      breaking change and publish migration guidance; **conforming to the framework is the sister
      repo's job**, and further problems they hit become new issues in this queue. Guidance is
      published (see `docs/operations/31-sister-repo-cutover-checklist.md` and the per-contract
      guides); adoption is tracked on **gh#753** and does NOT gate this archive.
- [x] 3.7 **ARCHIVE GATE REWRITTEN 2026-07-30 (owner ruling)** — archive no longer waits on owned-repo
      migration or coordinated product release notes; guidance is published
      (`docs/operations/30-rule-event-identity-clean-cutover.md`) and adoption is tracked on gh#753.
      Strict-validate the change; archive on SemStreams-local completeness. Superseded text: archive only
      after owned-repo migration (2.2), owned-product docs (3.5), and
      release notes (3.6) evidence is recorded for the v1 rollout
