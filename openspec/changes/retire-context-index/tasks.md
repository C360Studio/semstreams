# Tasks — retire-context-index

## 1. Lock the decision and current truth

- [x] 1.1 Land accepted ADR-090 and record the owner-approved current-state/materialized-view
      architecture.
- [x] 1.2 Apply the graph-index and graph-retention spec deltas without rewriting historical ADRs.
- [x] 1.3 Update the project product boundary so it no longer declares context as a durable index.

## 2. Remove the unconsumed durable view

- [x] 2.1 Remove `BucketContextIndex` and its framework KV catalog descriptor.
- [x] 2.2 Remove graph-index context bucket acquisition, storage handles, desired-state planning,
      writes, deletion/reconciliation, failure/readiness coupling, and metrics.
- [x] 2.3 Delete the context physical-key codec and its representation-specific tests.
- [x] 2.4 Prove a fresh graph-index start does not create `CONTEXT_INDEX`.
- [x] 2.5 Preserve `message.Triple.Context` and its authoritative encoding unchanged.

## 3. Replace implementation-shaped E2E evidence

- [x] 3.1 Remove E2E helpers that enumerate or decode `CONTEXT_INDEX`.
- [x] 3.2 Add a bounded E2E authority helper that enumerates hierarchy-predicate triples in
      `ENTITY_STATES` independently of `Context` so missing or incorrect provenance is observable.
- [x] 3.3 Replace context-index scenarios with a hard assertion that hierarchy triples retain
      `Context == "inference.hierarchy"` in authority.
- [x] 3.4 Assert the fresh tier stack has no `CONTEXT_INDEX` bucket and rename stage/result
      vocabulary so no production provenance-query capability is implied.

## 4. Correct current documentation and generated surfaces

- [x] 4.1 Remove current `CONTEXT_INDEX` catalog/configuration/API examples while retaining
      historical ADR/change evidence.
- [x] 4.2 Update the clean-wipe runbook to remove stale `CONTEXT_INDEX` state before reseed.
- [x] 4.3 Run schema generation and commit any intentional generated artifact changes.

## 5. Conformance and release gates

- [x] 5.1 `go test -race ./graph ./processor/graph-index ./test/e2e/client` — passed.
- [x] 5.2 `go test -race -tags=integration -count=1 ./processor/graph-index` — passed in 35.605s.
- [x] 5.3 `task lint` — passed.
- [x] 5.4 `task schema:generate` — passed; the OpenAPI example change is intentional.
- [x] 5.5 `task e2e:structural` — passed 37/37 stages on a fresh stack; every observed
      `hierarchy.*` triple retained the expected authoritative context and the retired bucket was absent.
- [x] 5.6 SemStreams reviewer approval on the full diff — APPROVE with no remaining
      blocking, high, or medium findings.

## Conformance evidence required before merge

| Decision | Required proof |
|---|---|
| No unconsumed durable provenance view | Catalog omits `CONTEXT_INDEX`; fresh graph-index startup does not create it |
| Authority retains provenance | Hierarchy triples in `ENTITY_STATES` retain `Context == "inference.hierarchy"` |
| Readiness covers only served views | No context write/delete can fail reconciliation or withhold readiness |
| No compatibility machinery | No alias, legacy reader, translation, migration, or dual write exists |
| Clean beta cutover | Fresh E2E stack lacks the bucket; wipe/reseed is documented |
| No phantom query API | E2E scans bounded authority but exposes no new application query |
| Current truth matches code | Current specs and project boundary omit the durable context view |
| End-to-end semantics survive | `task e2e:structural` proves hierarchy ingestion and authoritative provenance |
