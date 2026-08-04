# Tasks — retire-structural-index

## 1. Sequence the approved breaking change

- [x] 1.1 Merged #894 and archived `retire-context-index` before archiving this
      change.
- [x] 1.2 Bind this slice to ADR-090's accepted retirement of durable
      `STRUCTURAL_INDEX` while preserving in-memory structural computation.
- [x] 1.3 Define graph-clustering and graph-retention deltas against the
      post-context target; neither target contains `CONTEXT_INDEX`.

## 2. Retire the unconsumed durable view

- [x] 2.1 Remove `BucketStructuralIndex`, its framework KV catalog descriptor,
      ownership-set assertions, and retention membership.
- [x] 2.2 Delete the NATS structural storage implementation and its
      representation-shaped unit and integration tests.
- [x] 2.3 Preserve `types.go`, `kcore.go`, `pivot.go`, `structural.Indices`, and
      their pure algorithm tests.
- [x] 2.4 Remove structural bucket/storage fields, acquisition, save calls, and
      persistence-dependent anomaly initialization.
- [x] 2.5 Prove a fresh graph-clustering start does not create
      `STRUCTURAL_INDEX`.

## 3. Make anomaly detection own its prerequisites

- [x] 3.1 Compute K-core and pivot inputs only when an anomaly orchestrator has
      initialized successfully.
- [x] 3.2 Use `structural.DefaultPivotCount` internally and pass the freshly
      computed pointers directly into the same cycle's anomaly detectors.
- [x] 3.3 Preserve the explicit plus EntityID-derived structural provider so
      semantic virtual edges cannot affect K-core, pivot distance, or anomaly
      scores.
- [x] 3.4 Add real-NATS component integration proving the in-memory anomaly path
      receives populated structural inputs and persists expected
      `ANOMALY_INDEX` outcomes.
- [x] 3.5 Replace the persisted semantic-edge-isolation inspection seam with an
      in-process assertion over freshly computed indices.

## 4. Delete adopter-facing structural persistence

- [x] 4.1 Remove `enable_structural`, `pivot_count`, and `max_hop_distance`; add
      their raw names to `removedConfigFields` with guidance that anomaly
      detection now computes its prerequisites internally.
- [x] 4.2 Remove the auto-added `structural_index` output and reject an explicit
      `STRUCTURAL_INDEX` output as retired.
- [x] 4.3 Regenerate the component schema and delete the retired fields and port
      from all SemStreams configurations and generated surfaces.
- [x] 4.4 Publish the breaking migration list for SemDragon, SemSpec, and known
      generated schema/type mirrors; do not add compatibility shims.

## 5. Replace persistence-shaped tests and documentation

- [x] 5.1 Remove E2E structural bucket clients, metadata/result/comparison DTOs,
      and physical validation stages.
- [x] 5.2 Add a hard statistical-tier assertion that a fresh stack has no
      `STRUCTURAL_INDEX`; retain community outcome assertions.
- [x] 5.3 Remove current docs that advertise structural persistence, a structural
      query, or gateway access while preserving pure algorithm/anomaly concepts.
- [x] 5.4 Keep stale-bucket deletion in clean-wipe runbooks, labeled retired beta
      state rather than a current catalog member.
- [x] 5.5 Preserve historical ADRs and archived changes unchanged.

## 6. Conformance and release gates

- [x] 6.1 Race tests passed for `graph/structural`, `graph`, graph-clustering,
      E2E client/scenarios, and the E2E command.
- [x] 6.2 Full graph-clustering integration suite passed in 27.582s; the focused
      fresh-start, anomaly, default-disabled, and isolation cases passed in 6.661s.
- [x] 6.3 `task lint` passed.
- [x] 6.4 `task schema:generate` passed twice with identical schema and OpenAPI
      hashes; generated output is idempotent.
- [x] 6.5 `task e2e:statistical` passed 41/41 in 29.274s with 18 communities,
      retired-bucket absence, zero retired-bucket presence, and full teardown.
- [x] 6.6 SemStreams reviewer approved the complete implementation diff with no
      remaining blocking, high, or medium findings.

## Conformance evidence required before merge

| Decision | Required proof |
|---|---|
| No durable structural view | Catalog and fresh component/E2E starts omit `STRUCTURAL_INDEX` |
| Anomaly owns prerequisites | Real-NATS integration passes fresh K-core/pivot inputs in the same cycle |
| Structural semantics survive | Algorithm and isolation tests preserve explicit plus EntityID-only behavior |
| No accidental computation | Deployments with anomaly detection disabled perform no structural computation |
| No compatibility machinery | No alias, migration, dual write, or ignored stale field exists |
| Adopter surface shrinks | Retired config and port surfaces fail loudly |
| Stack ordering is safe | Context retirement archives first; final retention spec contains neither retired bucket |
| End-to-end behavior survives | Statistical E2E proves community outcomes and retired-bucket absence on a fresh stack |

The initial red test rejected none of the four retired surfaces: each of the three
configuration fields and an explicit structural output was accepted. The green
implementation rejects all four with deletion guidance.
