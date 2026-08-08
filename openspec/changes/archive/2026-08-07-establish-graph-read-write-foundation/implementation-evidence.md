# Implementation evidence — graph read/write foundation

This table maps the sixteen owner-approved rulings to the coordinated runtime cutover. Paths and line numbers refer to
the implementation branch before merge. The lifecycle operator-window correction discovered by E2E is recorded below;
it preserves `ENTITY_STATES` `History=1` and adds no store or compatibility surface.

| # | Ruling | Evidence | Disposition |
|---:|---|---|---|
| 1 | Option B: local projection contracts without runtime ownership | `pkg/projection/contract.go:22`; `pkg/ownership/` deleted | Conformant |
| 2 | Four-operation algebra and exact subjects | `internal/graphmutation/protocol.go:11`; `processor/graph-ingest/canonical_mutations.go:128` | Conformant |
| 3 | Nonzero revision for reconcile and delete | `graph/mutation_requests.go:18`; `processor/graph-ingest/canonical_mutations.go:263`; `processor/graph-ingest/canonical_mutations.go:420` | Conformant |
| 4 | Absent non-create targets return typed not-found | `processor/graph-ingest/canonical_mutations.go:271`; `processor/graph-ingest/canonical_mutations.go:353`; `processor/graph-ingest/canonical_mutations.go:423` | Conformant |
| 5 | Append is partial per subject; caller selects retry | `graph/mutation_responses.go:145`; `processor/graph-ingest/canonical_mutations.go:334`; `processor/graph-ingest/canonical_mutations.go:390`; `processor/graph-ingest/canonical_mutations_test.go:369`; `processor/graph-ingest/canonical_mutations_test.go:400`; `processor/graph-ingest/canonical_mutations_test.go:429` | Conformant |
| 6 | `commit_unknown` has no exactly-once/authorship claim | `internal/graphmutation/client.go:30`; `internal/graphmutation/client_test.go:70`; `pkg/projection/mutation_client.go:396`; `gateway/lifecycle-gateway/handlers.go:578` | Conformant |
| 7 | Referenced-object absence is valid and observed on read | `pkg/lifecycle/manager_query.go:622`; `pkg/lifecycle/manager_test.go:425`; `test/e2e/scenarios/tiered.go:266`; `openspec/specs/graph-ingest/spec.md:187` | Conformant |
| 8 | Delete stubs, restamp, foreign modes, inverse gate, pending spelling | `graph/stub.go` deleted; `pkg/lifecycle/workflow.go:354`; `test/e2e/scenarios/graph_roundtrip.go`; `pkg/ownership/inverse_gate.go` deleted | Conformant |
| 9 | Keep local schema; delete claims, leases, owner binding | `pkg/projection/contract.go:22`; `pkg/ownership/` deleted; `service/ownership_service.go` deleted | Conformant |
| 10 | One graph-ingest process; no runtime election | `openspec/specs/graph-ingest/spec.md:31`; no new coordinator/service/store | Conformant |
| 11 | Typed component ports are the Core NATS mutation API | `internal/graphmutation/protocol.go:11`; `processor/graph-ingest/mutation_runtime.go:17`; `gateway/lifecycle-gateway/component.go:81`; `test/shipped_graph_mutation_ports_test.go:17`; observation-only milestone subscriber at `agentic/agentrun/agentrun.go:388` and `agentic/agentrun/agentrun.go:396` | Conformant |
| 12 | Exactly-one provider is static flow validation only | `component/flowgraph/flowgraph.go:362`; `component/flowgraph/flowgraph.go:386`; `component/flowgraph/graph_mutation_contract_test.go:24` | Conformant |
| 13 | Complete package/bucket/service/config/schema/wire/spec/ADR cutover | `graph/kvcatalog.go:60`; `graph/owned_bucket_retention.go:39`; `service/rule_pack_bind.go:45`; `schemas/graph-ingest.v1.json`; `schemas/lifecycle-gateway.v1.json`; `gateway/lifecycle-gateway/handlers.go:578`; `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:1` | Conformant |
| 14 | Pre-v1 clean break; communicate-only downstream census | `docs/operations/36-graph-foundation-breaking-cutover.md:1`; retired production subjects/fields have no handlers | Conformant |
| 15 | Atomic Create plus observed-revision CAS on both lanes | `processor/graph-ingest/canonical_mutations.go:238`; `processor/graph-ingest/component.go:1962`; `processor/graph-ingest/component.go:2109`; `natsclient/kv.go:287`; `processor/graph-ingest/cas_integration_test.go:24` | Conformant |
| 16 | Hierarchy only on Graphable ingest with real Create/CAS writes | `processor/graph-ingest/component.go:1937`; `processor/graph-ingest/component.go:2109`; `graph/inference/hierarchy.go:347`; `processor/graph-ingest/canonical_mutations.go:238`; `test/e2e/scenarios/tiered.go:265` | Conformant |

## E2E-discovered lifecycle correction

The lifecycle tier exposed an older internal contradiction: `Manager.History` replayed KV revisions while the authority
bucket intentionally retained only one revision. gh#843 already contained the owner-confirmed ruling. The correction
stores a fixed 64-occurrence operator window in the current participant entity, sharing a transition ID in
`Triple.Context`; phase and records reconcile atomically at one observed revision. `History` exact-reads and strictly
decodes that window. `ENTITY_STATES` remains `History=1`; no audit promise, new bucket, stream, service, knob, or legacy
reader was introduced (`pkg/lifecycle/transition_records.go:13`, `pkg/lifecycle/manager_query.go:515`,
`graph/kvcatalog.go:60`, `openspec/specs/lifecycle/spec.md:77`). `DespawnWith` passes the exact successful transition
revision to conditional delete without a fresh read (`pkg/lifecycle/manager.go:962`); both the deterministic unit race
(`pkg/lifecycle/manager_test.go:1012`) and real-KV race (`pkg/lifecycle/manager_integration_test.go:194`) prove a newer
revision survives with `revision_mismatch`.

Create and Append provenance is rejected before transport when request ID or source is absent, Append is empty, or an
explicit triple timestamp conflicts with mutation metadata (`pkg/projection/mutation_client.go:331`,
`pkg/projection/mutation_client_test.go:38`, `pkg/projection/mutation_client_test.go:71`,
`pkg/projection/mutation_client_test.go:103`, `pkg/projection/mutation_client_test.go:129`,
`pkg/projection/mutation_client_test.go:154`). Lifecycle gateway translates the closed mutation failure vocabulary to
typed operator responses, including 503 `commit_unknown` with inspect-authority guidance and no blind-retry advice
(`gateway/lifecycle-gateway/handlers.go:578`, `gateway/lifecycle-gateway/component_test.go:1455`).

## Pre-merge review follow-ups

Reconcile equality explicitly retains persisted annotations as desired state. An annotation-only change applies once
and its exact repeat is unchanged (`processor/graph-ingest/canonical_mutations_test.go:345`); the adopter contract says
to preserve observed timestamps, confidence, and expiry when unchanged is intended
(`docs/operations/34-projection-mutation-client.md:108`, `openspec/specs/projection-mutation-client/spec.md:62`).

`natsclient.KVStore.DeleteAtRevision` owns conditional delete, input validation, and typed not-found/revision-mismatch
mapping (`natsclient/kv.go:463`, `natsclient/kv_delete_revision_test.go:27`). Graph-ingest now holds one wrapped
`ENTITY_STATES` handle for exact reads, Create/CAS, conditional delete, and the bounded boot snapshot sweep through
`Watch(">")` (`processor/graph-ingest/component.go:1158`, `processor/graph-ingest/component.go:2149`). The real-NATS
test proves a stale delete preserves authority and the matching revision deletes it
(`natsclient/kv_integration_test.go:205`). Triple-less canonical Create is explicitly legal and persists a valid
entity carrying only framework-injected facts (`processor/graph-ingest/canonical_mutations_test.go:106`).

## Verification record

- `task lint` — green after final reviewer corrections.
- `go test -race ./...` — green after correcting one line-number audit annotation shifted by the new tests.
- `task test:integration` — green after final reviewer corrections, including `natsclient` (90.427 s).
- Focused append/lifecycle/port/requester tests — green, including canceled append, complete accounting, monotonic
  transition records, source-derived absent references, lifecycle port validation, and shipped config census.
- `task e2e:core` — green, including exact GraphQL graph round trip.
- `task e2e:structural` — final post-fix run green, all 38 stages, including
  `validate-canonical-create-no-hierarchy` and `validate-relationship-no-stub` with zero unintended births.
- `task e2e:lifecycle` — final post-fix run green after strict record-chain validation in 355 ms.
- `task e2e:agentic` — final post-fix run green in 563 ms after hidden milestone publisher deletion.
- `task e2e:semantic` — green, all 48 stages in 12m49s; embedding queue resolved 68 with zero failures/pending,
  graph round-trip passed, thematic recorder completed, and all seven known-answer assertions passed.
- `task schema:generate` — green with stable intentional deltas, including lifecycle gateway's declared request port
  and corrected OpenAPI wording.
- `go build ./...` — green.
- `go test ./test/contract/...` — green.
- `openspec validate establish-graph-read-write-foundation --strict` — green.
- Static production search — no ownership package/imports, owner token/lease fields, legacy mutation operations, or
  compatibility handlers. Overall staged diff is net-negative by more than 20,000 lines. No bucket, stream, status key,
  coordination service, NATS CLI dependency, or `8222` dependency was added; existing `natsclient` testcontainer
  monitoring-port use is unchanged.
