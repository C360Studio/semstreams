# R1 decomposed execution design

Status: **pre-owner review draft; not approved**.

## 1. Replacement and authority

This design supersedes the composite R1 implementation boundary without changing its reviewed semantic outcomes.

Composite evidence retained unchanged:

- artifact: `post-gs01-r1-acquisition-lifecycle-retry-design.md`
- lines/bytes: 639 / 40,982
- SHA-256: `7c5154a4026818f51e72158c67756617f9bda1c444e24f0623e8186da138e837`
- disposition: `DESIGN REVIEW PASS`, but superseded before owner acceptance

Accepted inventory:

- artifact: `post-gs01-r1-acquisition-lifecycle-retry-inventory.md`
- lines/bytes: 487 / 35,930
- SHA-256: `b5bb0fa79f584a7ec8e06965d9885b9cd87629791f0accd620d5043c2bbfc22c`
- disposition: `INVENTORY PASS`

Frozen authority:

- foundation design SHA-256:
  `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- merged roadmap SHA-256:
  `0f16d7de739ea70c09312a897089ca01b79c28c9e43fbf0b78bf596bdc1504a2`
- runtime baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`
- R0 merge: `6ce137009fe6cf019dcb0a9a2a5122e81c2f9d27`

The SemStreams identity packet and no-compatibility rule remain binding. This design changes implementation boundaries,
reservations, and verification allocation only.

## 2. Why the composite execution unit is rejected

The composite R1 design joined six independent ownership domains: catalog acquisition, lifecycle coordination, retry
policy, port vocabulary, gated-DAG behavior, and HTTP diagnostics. Review repeatedly found additional configuration,
documentation, and test surfaces because a single PR could not isolate those bills.

Keeping the composite target as evidence is useful. Implementing it as one unit is not.

## 3. Execution options

### Option A — one composite PR

Rejected. Failure attribution, review, rollback, and E2E ownership span unrelated behaviors.

### Option B — five coherent linear slices

Recommended:

```text
R1a acquisition and narrow interfaces
  → R1b lifecycle poison localization
  → R1c retry contract truth
  → R1d component declaration truth
  → R1e message-logger boundary
  → R1 complete
  → R2
```

Costs: more handoffs and batons. Benefits: one behavioral owner per merge, bounded proof, narrow rollback, and no
parallel branches silently undoing capability narrowing.

### Option C — branch after R1a

Rejected for this program. Although R1d and R1e are file-disjoint after R1a, linear execution better preserves context
and keeps the cumulative pattern/deletion ledger authoritative.

### Option D — extract shared infrastructure first

Rejected unless the pattern gate below passes. Similar-looking KV calls do not justify a general source, watcher,
poison, retry, readiness, or boot-coordination runtime.

## 4. Mandatory pattern census and extraction gate

Every slice repeats this gate before target tests or code. It is not a one-time program ceremony.

The slice artifact and baton enumerate:

1. the semantic job;
2. every exported/unexported spelling, serialized token, resource ID, helper, interface, error, status, and log shape;
3. every current owner and present consumer;
4. exact method sets;
5. error classifications;
6. lifecycle, shutdown, and concurrency behavior;
7. policy differences;
8. do-nothing, local-owner, existing-owner extension, and shared-extraction options;
9. measured production/adopter cost; and
10. rejected extraction candidates with evidence required to revisit.

### 4.1 Default ownership

Interfaces are package-local and consumer-owned by default.

A shared interface, package, runtime, or policy helper requires at least three present consumers with:

- identical method sets;
- identical error classification;
- identical lifecycle/shutdown behavior;
- identical concurrency/handle-sharing semantics;
- identical policy;
- fewer authored production lines after extraction;
- less adopter knowledge; and
- no callbacks, hooks, modes, policy injection, or owner switches.

### 4.2 Stateless-helper exception

A stateless, policy-free Go helper may be shared with fewer consumers only when inputs and outputs express the complete
behavior, ownership stays with the caller, and it owns no retry, watcher, poison, readiness, lifecycle, or policy.
Anonymous minimal input interfaces are preferred over new exported framework types.

### 4.3 Exported surface

A new exported symbol requires a present cross-package consumer at birth. Tests and future plans do not count.

### 4.4 Catalog boundary

All catalog readers reuse existing `graph.OpenCatalogBucket`. Startup retry budgets, watcher ownership, cancellation,
poison, transport closure, readiness, and logging remain owner-local unless the complete extraction gate passes.

### 4.5 Cumulative rejected-extraction ledger

Each baton appends:

| Candidate | Consumers compared | Failed dimension | Disposition | Revisit evidence |
|---|---|---|---|---|

Later agents must read the ledger before proposing the same abstraction.

### 4.6 Index result and API freeze

R1 may inventory index result and API patterns as evidence, but it MUST NOT change or pre-design:

- NATS query subjects or operation names;
- request, response, or error-classification contracts;
- result DTOs, serialized fields, pagination, or absence/ambiguity semantics;
- readiness or currency meaning; or
- gateway, GraphQL, MCP, or other adopter-facing response shapes.

Package-local acquisition narrowing MUST remain invisible to result consumers. If any R1 implementation requires an
index result/API change, stop that slice, record the falsified premise in its baton, and assign the decision to its
owning R3–R6 increment. Do not expand R1, add a preparatory abstraction, or preserve both shapes.

## 5. R1a — catalog acquisition and narrow interfaces

### Outcome

R1a is behavior-preserving:

- graph-ingest alone ensures/writes `ENTITY_STATES`;
- scoped readers use `OpenCatalogBucket`;
- successful handles are retained behind exact package-local interfaces;
- no full reader `jetstream.KeyValue` field/signature survives;
- startup, watcher, poison, retry, readiness, and logging policy stays owner-local;
- index query subjects, operations, DTOs, schemas, classifications, and response behavior remain byte/behavior
  compatible with the R0 baseline; and
- message logger is excluded until R1e.

Lifecycle truthfully retains interim `ListKeys`, `Watch`, and `WatchAll`; R1b deletes the global guard and `WatchAll`
together. This is current behavior, not a compatibility path.

### Pattern decisions

- reuse `OpenCatalogBucket`;
- reject shared `GraphSource`, exported KV reader, startup waiter, and watcher supervisor;
- allow only stateless anonymous-interface narrowing for helpers such as last-sequence and filtered-key collection when
  it reduces code.

### Exact interfaces

| Consumer | Methods |
|---|---|
| graph-index watcher/repair | `WatchAll`, `Get` |
| graph-index revision target | `Status` on a second independently opened handle |
| spatial / temporal | `WatchAll` |
| embedding | `WatchAll`, `Get`, `Status` |
| clustering entity | `Keys`, `Get` |
| clustering outgoing | `Get` |
| clustering incoming | `ListKeysFiltered` |
| rule watcher | `Watch`; zero patterns open nothing |
| lifecycle | `ListKeys`, `Watch`, `WatchAll` until R1b |
| gated-DAG | `Watch` |

Broad handles remain only for proven bucket owners that invoke write methods.

### Reservations

- graph catalog and focused tests;
- narrowly required natsclient helpers;
- graph-index, spatial, temporal, embedding, clustering reader surfaces;
- rule watcher;
- lifecycle acquisition;
- gated-DAG acquisition;
- catalog docs and AST/census contract tests.

### TDD, delete proof, and verification

Start with failing Open/no-create/owner-name, zero-pattern, exact-method-set, dual-handle, no-broad-reader, and
owner/write-allowlist tests.

Delete raw/generic reader acquisition, broad reader fields/signatures, and any unapproved provider/waiter introduced
during development. Preserve every current behavior outside acquisition/static capability.

```bash
go test -race ./graph ./natsclient
go test -race ./processor/graph-index ./processor/graph-index-spatial ./processor/graph-index-temporal
go test -race ./processor/graph-embedding ./processor/graph-clustering ./processor/rule
go test -race ./pkg/lifecycle ./processor/gated-dag
go test ./test/contract/...
task check:push
```

E2E: none.

## 6. R1b — lifecycle poison localization

Prerequisite: merged R1a.

### Outcome

Delete lifecycle's Manager-wide guard/latch and narrow its interface to `ListKeys`/`Watch`.

- exact validates only the entity touched;
- List filters workflow scope before decode;
- Watch/WatchEvents decode matching values before projection;
- malformed matching state emits no participant/event/mutation, warns once with workflow/entity/revision/code/reason,
  and closes only that subscription;
- unrelated work continues;
- no lifecycle `WatchAll`, global preflight/barrier, status, or new metric remains.

### Pattern decisions

Reject a shared poison coordinator, watcher supervisor, poison status, and per-entity metric. Rule and derived owners
have different scope, readiness, recovery, and closure policies. Keep lifecycle's structured terminal warning local.

### Reservations and delete proof

Own lifecycle Manager/query/doc/tests, lifecycle OpenSpec, successor ADR-092, and lifecycle E2E. Delete every accepted
`graphStateGuard*`/`graphStatePoison*` identifier, global fixture/test, and R1a interim `WatchAll` capability. Preserve
touched-entity typed errors, graph-ingest validation, rule-local latches, derived readiness, and transition retry.

Add `docs/adr/092-lifecycle-poison-localization.md` as the narrow successor decision that supersedes ADR-081's
lifecycle-wide sticky-guard ruling. Preserve the accepted ADR-081 bytes as historical evidence. The lifecycle
OpenSpec separately owns the current mechanics; do not leave the old ruling presented as current framework behavior.

### TDD and proof

Record RED for malformed A/valid B continuity, zero mutations, independent subscriptions, one warning, zero
`WatchAll`, quiet cancellation, and local transport closure.

Extend the production lifecycle scenario by injecting malformed nonmatching A into the existing authority bucket, then
prove valid B list/exact/operator-patch/WebSocket behavior through production gateways.

```bash
go test -race ./pkg/lifecycle
go test ./test/contract/...
task check:push
task e2e:lifecycle
```

## 7. R1c — retry contract truth

Prerequisite: merged R1b.

### Outcome

- rule remains one exact read and one mutation request;
- mismatch is visible and commit-unknown is never retried;
- old `ExecutionContext` is never replayed;
- lifecycle transition rereads and reconstructs phase, edge, audit chain, projection, and mutator every definite
  conflict attempt;
- no shared retry helper, knob, or coordinator.

### Pattern decisions

Reject shared CAS retry because rule and lifecycle differ in intent reconstruction, commit-unknown policy, and stale
context safety. Preserve lifecycle's owner-local full-intent loop.

### Reservations, truth, and proof

Own rule/projection characterization, lifecycle retry tests, and retry-specific rule/lifecycle spec text. Delete stale
“one bounded conflict retry” prose and any retry implementation that characterization contradicts.

Update `docs/concepts/28-governed-semantic-state.md` so its retry description matches the characterized rule and
lifecycle contracts.

Rule tests are characterization-first and may be green. Lifecycle tests script fresh authority between attempts.

```bash
go test -race ./processor/rule ./pkg/projection ./pkg/lifecycle
go test ./test/contract/...
task check:push
```

E2E: none unless characterization falsifies runtime and the owner reauthorizes scope.

## 8. R1d — component declaration truth

Prerequisite: merged R1c under the selected linear topology.

### Outcome

- add distinct metadata-only `component.KVReadPort`;
- preserve StoreRead content federation unchanged;
- clustering declares required `kv-read` for entity/outgoing/incoming buckets and no authority watch;
- gated-DAG declares its existing optional exact prefix watch;
- no alias or dual declaration.

### Pattern decisions

`StoreReadPort` cannot represent exact-bucket KV reads: it uses producer-selected content federation, every-provider
fan-in, different resource/connection identities, and a different runtime substrate. A mode would add conditional
semantics. Omitting a declaration hides dependencies. Admit distinct `KVReadPort` because clustering and flowgraph are
present cross-package consumers.

Contract: token `kv-read`, resource `kvread:<bucket>`, non-exclusive, exact-bucket connection, same-bucket
`KVWritePort` match only, no Store cross-match, and no Watch/replay/cadence/injection/runtime implication.

Gated-DAG declares optional `unit_entity_watch`, bucket `ENTITY_STATES`, keys `UnitEntityPrefix + ".>"`; failure warns
once while periodic correctness continues.

### Reservations and clean break

Own component port/schema/flowgraph, clustering and gated-DAG declarations/tests/specs, generated artifacts, and the
tagged clustering no-watcher integration test. The checked-in configuration census is exact:

- `configs/statistical.json`;
- `configs/semantic.json`;
- `configs/semantic-8b.json`; and
- `configs/semantic-frontier.json`.

Update the current declaration truth in `processor/graph-ingest/README.md` and
`docs/basics/06-configuration.md`.

Delete clustering `kv-watch`, alternate spellings, Store KV modes/cross-matches, and universal KV-read runtime.

### TDD and proof

Move checked-in configs to unsupported `kv-read` and record RED. Add serialization, no-alias, resource, same/different
bucket matching, Store nonmatch, config-enumeration, clustering declaration, and gated-DAG tests.

Before landing, file the structural coverage gap `test(e2e): prove gated-DAG periodic continuity after optional
unit-watch loss`, owned by the foundation program and assigned to `@cglusky`, target `task e2e:structural`; add no
production fault hook.

```bash
go test -race ./component ./component/flowgraph
go test -race ./processor/graph-clustering ./processor/gated-dag
go test -race -tags=integration ./processor/graph-clustering \
  -run TestIntegration_ClusteringHoldsNoEntityStatesWatcher
go test ./test/contract/...
task check:push
task e2e:statistical
```

## 9. R1e — message-logger catalog boundary

Prerequisite: merged R1d under the selected linear topology.

### Outcome

Message-logger KV query/watch becomes catalog-only operational diagnostics:

- query interface `Keys`/`Get`;
- SSE interface `Watch`/`WatchAll`;
- request-local Open after validation and before SSE headers;
- off-catalog HTTP 400;
- absent catalog owner HTTP 503/`index_not_ready`, owner named;
- no creation/write;
- product middleware remains authorization owner.

### Pattern decisions

Delete the generic application-bucket provider. Reject framework auth, documentation-only “operator-only,” and
allowlist overrides. Reuse catalog Open while keeping request/HTTP policy local.

### Reservations and clean break

Own message-logger query/watch/OpenAPI/tests, E2E client, shared graph-roundtrip probe, new diagnostics spec, operator
docs, `taskfiles/dev.yml` diagnostics wording, and generated artifacts. Delete generic lookup, off-catalog success/404,
and any override, alternate route, legacy parameter, or framework auth policy.

### TDD and proof

Record RED for catalog allow, fixed off-catalog 400/no-create, core's absent `EMBEDDING_INDEX`
503/`index_not_ready`/owner/no-create, and Open-before-SSE.

```bash
go test -race ./service ./test/e2e/scenarios/...
go test ./test/contract/...
task check:push
task e2e:core
```

## 10. R1 completion

R1 completes only after R1a–R1e merge linearly and the completion baton proves:

- each semantic outcome and delete proof;
- independent review for every slice;
- cumulative rejected-extraction ledger;
- no compatibility/dual path;
- E2E only in its owning slice;
- linked gated-DAG gap;
- index result/API freeze proof or a recorded owner-stopping falsification;
- cumulative authored production and adopter-concept deltas;
- continued final-program net-negative trajectory.

Only then may R2 begin.

## 11. Complexity and rollback

Every slice reports authored lines, exported/serialized concepts, front doors, resources, adopter knowledge, and
rejected abstractions. R1d may be locally positive; R1b should remove substantial coordination. No slice hides its
cost behind future deletion.

Rollback is slice-local before merge. After merge, forward-fix. Never reintroduce a retired path through compatibility.

## 12. Proposed owner rulings

1. Accept R1a's truthful interim lifecycle `ListKeys/Watch/WatchAll`; R1b deletes `WatchAll` with the guard.
2. Require linear R1a → R1b → R1c → R1d → R1e execution to preserve handoff context.
3. Assign the gated-DAG structural coverage gap to `@cglusky` under this foundation program.
4. Require exact materialized design/amendment hashes, independent review, and explicit owner acceptance before R1a.
