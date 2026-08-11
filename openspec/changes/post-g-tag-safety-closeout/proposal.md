<!-- markdownlint-disable MD041 -->

## Why

The accepted post-G inventory found two bounded runtime correctness defects before a stable downstream pin:

- a permanently rejected community can leave an incomplete saved partition that is later treated as complete,
  destructively pruning prior valid community state and reporting success (#855); and
- graph-embedding can reinterpret an unresolved `StorageInstance` through an unrelated owned fallback store, turning
  an identity miss into a wrong-store read and failed/degraded embedding state (#875).

The same inventory found a deterministic proof gap: the current research-graph E2E deliberately proves
`synthesize_directly` while requiring execute and assess to be absent. It therefore does not prove the admitted
`execute_subqueries → fusion.Fuse → assess → synthesize` branch.

This change closes those exact gaps and establishes the release-evidence boundary for the stable tag. It does not
promote the wider derived-state findings into runtime work.

## What Changes

- Preserve writable community siblings after record-local permanent rejection, but classify any candidate with a failed
  save as incomplete. An incomplete candidate cannot advance hierarchical detection, prune prior state, or enter
  complete-success accounting.
- Withhold prune and complete-success accounting for an incomplete candidate. Successful or partial writes may overwrite community records and entity mappings, so readers may observe a mixed prior/candidate projection until a later complete run converges.
- Resolve an offloaded body only through the live store registered under the reference's exact `StorageInstance`.
- Remove graph-embedding's unnamed owned-store fallback as a resolution authority.
- Treat an unresolved or concurrently deregistered instance as the existing explicit content exclusion: inline
  identity may continue, no-text skips, and the miss alone never creates failed/degraded embedding state.
- Preserve resolved-store read failures as real operational failures.
- Add deterministic full-stack research proof for `execute_subqueries`, fusion, assessment, and synthesis while
  retaining the existing `synthesize_directly` proof as a separate branch.
- Correct accepted architectural and workflow commentary that describes superseded storage/index/release behavior.
- Retain #301, #844, and #860 as exact-candidate gates; record named future programs for the deferred findings and the
  accepted #839 limitation without authorizing runtime work.
- Make #860's existing crud-tools rule proof fail closed when required metrics cannot be scraped or observed. Treat an
  absent pre-increment CounterVec label series as observed zero only after collector availability is established.
- Give one test helper sole ownership of `Cmd.Wait` across timeout and cleanup so exact-candidate race proof cannot be
  failed by a second waiter.
- Disable the Go test cache for exact-candidate proof and include the core clustering/embedding packages in the focused
  command, not only their processor wrappers.
- Keep decisions and evidence templates in-tree. Tie pre-tag proof, review, and CI to an immutable SHA-specific
  candidate-proof Release, then record tag/artifact identity and the actual coordinated #827 outcome in a separate
  immutable product-Release attestation.

## Capabilities

### New Capabilities

- `release-candidate-proof`: Defines deterministic-path disposition, exact-candidate proof, and exact-tag identity.

### Modified Capabilities

- `graph-clustering`: Makes candidate completeness an explicit prerequisite for prune and complete success.
- `graph-embedding`: Makes `StorageReference` resolution instance-exact and distinguishes unresolved identity from a
  resolved-store read failure.
- `framework-composition`: Requires deterministic proof of both admitted graph-research branches.

## Impact

Runtime changes are limited to `graph/clustering`, `processor/graph-clustering`, `graph/embedding`, and
`processor/graph-embedding`. Research changes are test fixtures and E2E assertions over the existing rule,
component, subject, payload, and fusion paths.

The bounded pre-candidate correction touches only test truth and release documentation:
`test/e2e/scenarios/crud-tools` makes the retained #860 assertion fail closed, and
`test/testinfra/integration_runner_contract_test.go` gives process waiting one owner across timeout and cleanup. It
changes no production or workflow behavior.

ADR-063 is corrected because its accepted registry-miss fallback ruling conflicts with instance-exact resolution.
ADR-068, the suspended semantic-tier change, and two workflow comment blocks receive truth corrections without
runtime or workflow activation.

The candidate commit cannot contain or predict its own SHA. `candidate-evidence.md` is therefore a schema/template,
not a completed proof record. Candidate freeze selects one clean immutable SHA. Only a fully green candidate publishes
exact pre-tag proof under `candidate-proof-<fullSHA>`; product tag and artifact facts follow in the separate
product-Release attestation.
The in-tree migration guide remains version-independent. Tag-specific guidance exists only in product Release notes.

External producers keep the existing `StorageReference` shape and exact logical `StorageInstance`. No new public
symbol, configuration field, subject, port, bucket, stream, service, query, or compatibility layer is added.

## Non-goals

- No #839/#857 payload preflight, chunking, size prediction, storage layout, or general payload-ceiling solution. #839
  is an accepted tag limitation; #857 belongs to the Payload Bounds and Retention Program.
- No runtime work for DI-01 through DI-04, #619, #672, spatial/temporal malformed-aggregate handling or cleanup,
  hierarchy, anomaly retention, #829 summary quality, reclamation, or generic readiness.
- No community transaction, generation manifest, checkpoint, rollback store, or clustering status producer.
- No generic store resolver, default store, bucket inference, alternate resolution authority, or store-port redesign.
- No new research rule, subject, payload, component, top-level E2E tier, or task family.
- No compatibility shim, deprecated fallback, dual route, or downstream implementation audit.
- No activation or task completion in the suspended `semantic-tier-split` change.
