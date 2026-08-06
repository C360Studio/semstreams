# Post-GS-01 roadmap amendment — decompose R1 execution

Status: **proposed content-addressed amendment; not approved**.

## 1. Authority and immutability

The merged roadmap remains byte-identical:

- artifact: `post-gs01-graph-read-derived-foundation-roadmap.md`
- SHA-256: `0f16d7de739ea70c09312a897089ca01b79c28c9e43fbf0b78bf596bdc1504a2`

This amendment supersedes only R1's composite implementation boundary, R1 reservations/owning truth/verification,
and R2 prerequisite wording. It changes no R0 or R2–R9 semantic outcome.

Evidence:

- accepted inventory SHA-256:
  `b5bb0fa79f584a7ec8e06965d9885b9cd87629791f0accd620d5043c2bbfc22c`
- frozen foundation design SHA-256:
  `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- superseded composite R1 design SHA-256:
  `7c5154a4026818f51e72158c67756617f9bda1c444e24f0623e8186da138e837`
- replacement decomposed design:
  `post-gs01-r1-decomposed-execution-design.md`, 426 lines / 17,180 bytes,
  SHA-256 `e1d7c47898824b4bfdca33a4e53da75dd4d59af147315ba2871f2cbebe2c017f`

The composite design was independently reviewed but not owner-accepted. It remains evidence only.

## 2. Amended dependency order

Replace the single R1 unit with:

```text
R0 → R1a → R1b → R1c → R1d → R1e → R1 complete → R2
```

Linear order is deliberate. It preserves cumulative interface/deletion truth and prevents later branches from undoing
earlier narrowing.

## 3. Cross-slice pattern gate

Every R1 slice begins with repository-first enumeration of all spellings, owners, consumers, method sets, error
classifications, lifecycle/concurrency behavior, and policy.

Local consumer-owned interfaces are the default. Shared interfaces/packages/runtimes require at least three present
consumers with identical semantics on every enumerated dimension, fewer authored production lines, less adopter
knowledge, and no hooks/modes/callbacks. Stateless policy-free Go helpers may be shared with fewer consumers only when
the mechanism is literally identical and ownership stays local. Exported symbols require present cross-package
consumers at birth.

`graph.OpenCatalogBucket` remains the shared acquisition mechanism. Owner-local retry, watcher, poison, readiness,
closure, boot, and logging policy is not generalized without the complete proof.

Each slice appends a rejected-extraction ledger to its baton.

R1 may inventory index result/API patterns, but every index query subject, operation, DTO, serialized result field,
error classification, pagination rule, readiness meaning, and gateway response shape is frozen through R1. A required
change stops the owning slice and is assigned to R3–R6; it MUST NOT broaden R1 or create a preparatory/dual contract.

## 4. R1a — catalog acquisition and narrow interfaces

Prerequisite: R0 plus accepted amendment.

Atomic outcome:

- graph-ingest remains sole authority Ensure/writer;
- scoped readers use catalog Open;
- exact package-local capability interfaces replace broad reader handles;
- all runtime behavior outside acquisition/static capability remains unchanged;
- index result and API contracts remain unchanged;
- lifecycle truthfully retains interim `ListKeys/Watch/WatchAll` until R1b;
- message logger remains reserved for R1e.

Delete: raw/generic reader acquisition, broad reader fields/signatures, unapproved source/waiter abstractions.

Proof: focused race/contract and `task check:push`; no E2E.

## 5. R1b — lifecycle poison localization

Prerequisite: merged R1a.

Atomic outcome: delete Manager-wide guard/latch and lifecycle `WatchAll`; localize exact/List/Watch poison; close only
the affected subscription; structured entity/revision warning; no status or metric.

Delete: every accepted guard/latch identifier, global fixture/test, and interim lifecycle `WatchAll` capability.

Owning truth: add `docs/adr/092-lifecycle-poison-localization.md` as the narrow successor decision that supersedes
ADR-081's lifecycle-wide sticky-guard ruling; preserve ADR-081 bytes as historical evidence. The lifecycle OpenSpec
separately owns current mechanics.

Proof: focused race/contract, `task check:push`, and recorded RED→green `task e2e:lifecycle`.

## 6. R1c — retry contract truth

Prerequisite: merged R1b.

Atomic outcome: rule one read/one mutation/no replay; lifecycle full-intent reread/revalidation; no shared retry helper
or knob.

Delete: stale retry prose and any implementation contradicted by characterization.

Owning truth includes `docs/concepts/28-governed-semantic-state.md`.

Proof: focused rule/projection/lifecycle race and `task check:push`; no E2E unless runtime premise is falsified and
owner reauthorizes scope.

## 7. R1d — component declaration truth

Prerequisite: merged R1c.

Atomic outcome: distinct metadata-only `KVReadPort`; unchanged StoreRead federation; clustering three required reads
and no authority watch; gated-DAG optional exact prefix watch; no alias or dual declaration.

Delete: clustering `kv-watch`, alternate spellings, Store KV modes/cross-matches, universal KV-read runtime.

The exact checked-in configuration set is `configs/statistical.json`, `configs/semantic.json`,
`configs/semantic-8b.json`, and `configs/semantic-frontier.json`. Current declaration truth also includes
`processor/graph-ingest/README.md` and `docs/basics/06-configuration.md`.

Proof: focused race/contract, tagged clustering integration, `task check:push`, `task e2e:statistical`, and linked
gated-DAG structural coverage gap assigned to `@cglusky`.

## 8. R1e — message-logger boundary

Prerequisite: merged R1d.

Atomic outcome: catalog-only request-local query/watch; off-catalog 400; absent owner 503/`index_not_ready` with owner;
no creation; product middleware remains authorization owner.

Delete: generic bucket provider, off-catalog success/404, overrides, compatibility routes, and framework auth policy.

Owning truth includes the diagnostics wording in `taskfiles/dev.yml`.

Proof: focused service/E2E-client race, `task check:push`, and recorded RED→green `task e2e:core`.

## 9. Amended shared-file order

| Surface | Order |
|---|---|
| `graph/{constants,kvcatalog}.go` | R1a → R2 → R3 → R4 |
| narrowly required natsclient helpers | R1a, then released |
| graph-index component | R1a → R3 → R4 → R5a |
| graph-index query/watermark | R1a only for necessary method narrowing; otherwise R3 → R4 → R5a |
| lifecycle Manager/query/doc/tests | R1a → R1b → R1c |
| lifecycle OpenSpec and successor ADR-092 | R1b → R1c for OpenSpec; ADR-092 freezes in R1b |
| clustering reader fields/declaration/config | R1a → R1d → R5c |
| gated-DAG executor/component | R1a → R1d |
| component port/flowgraph vocabulary | R1d |
| message-logger service/OpenAPI | R1e |
| lifecycle E2E | R1b |
| statistical config/generated artifacts | R1d |
| graph-roundtrip/message-logger E2E | R1e |

No unmerged slices edit the same reservation concurrently.

## 10. Amended owning truth and E2E

| Slice | Owning truth | E2E |
|---|---|---|
| R1a | catalog acquisition, bucket docs, interface/census tests | none |
| R1b | lifecycle doc/OpenSpec, successor ADR-092, E2E | lifecycle |
| R1c | rule-projection and lifecycle retry truth | none unless falsified |
| R1d | framework-composition, clustering, gated-DAG specs/docs/config | statistical plus gated gap |
| R1e | message-logger diagnostics spec/OpenAPI/operator docs | core |

Do not run unrelated E2E tiers or the full ladder because R1 as a whole is breaking.

## 11. R2 prerequisite

“Prerequisite: R1” now means all five slices are merged/reviewed, delete proofs are zero, relevant E2E is green, the
gated-DAG gap is linked, the cumulative extraction ledger is current, no compatibility exists, and one R1 completion
baton names every slice commit and review.

## 12. Baton requirements

Each slice records prerequisite commit, accepted hashes, reservations, pattern census, extraction decision, rejected
ledger delta, additions/replacements/deletions, focused proof, relevant E2E or explicit none, authored/exported concept
delta, adopter-knowledge delta, index result/API freeze evidence or an owner-stopping falsification,
successor-decision evidence where applicable, and reviewer disposition.

## 13. Complexity and identity

This amendment adds no runtime concept. R1 remains bounded to no new bucket, stream, service, status, metric, query
family, recovery system, general client, or universal runtime. One `KVReadPort` is permitted only in R1d with present
consumers and collision proof. Local interfaces remain default. The final program remains net-negative against
`d1570ef81b23096021af0d7bf3321b4c08c7e54b`.

## 14. Proposed owner rulings

1. Accept interim lifecycle `ListKeys/Watch/WatchAll` in R1a and deletion in R1b.
2. Accept linear R1a → R1b → R1c → R1d → R1e execution.
3. Assign the gated-DAG structural coverage gap to `@cglusky` under the foundation program.
4. Require exact design/amendment hashes, independent review, and owner acceptance before R1a.
