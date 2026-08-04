## Why

`STRUCTURAL_INDEX` is recomputed and persisted after every graph-clustering cycle,
but no SemStreams or scanned sister-repository production code reads it. Anomaly
detectors consume the freshly computed `KCoreIndex` and `PivotIndex` pointers in
memory. Persistence therefore adds graph-wide writes, a bucket, public config and
port surfaces, failure coupling, tests, and misleading documentation without
serving a semantic consumer.

This is the second bounded retirement approved by ADR-090. It depends on
`retire-context-index` ([PR #894](https://github.com/C360Studio/semstreams/pull/894))
being merged and archived first because both changes modify the same
`graph-retention` requirement. The target state intentionally contains neither
`CONTEXT_INDEX` nor `STRUCTURAL_INDEX`.

## What Changes

- Remove durable `STRUCTURAL_INDEX`, its catalog descriptor, and its storage
  abstraction.
- Preserve the pure K-core and pivot algorithms and their behavior tests.
- Make anomaly detection own fresh in-memory structural computation as an
  internal prerequisite, using `structural.DefaultPivotCount`.
- Remove `enable_structural`, `pivot_count`, `max_hop_distance`, and the
  `structural_index` output port. Stale fields and ports fail loudly with deletion
  guidance rather than being ignored.
- Preserve the existing isolation boundary: structural computation sees explicit
  plus EntityID-derived edges, never semantic virtual edges.
- Replace physical-storage tests and E2E stages with algorithm/anomaly outcomes
  and a hard fresh-stack absence assertion.
- Correct current documentation, schemas, configurations, and generated surfaces;
  retain historical records unchanged.

## Non-goals

- No removal of anomaly detection or `ANOMALY_INDEX`.
- No change to community detection or community-query behavior.
- No new structural query, graph-search ranking contract, or replacement durable
  view.
- No general materialized-view or CQRS runtime.
- No rewrite of ADR-054 or other historical decisions and archived changes.

## Breaking release and sequencing

This is a pre-v1 breaking change. Merge and archive `retire-context-index` before
archiving this change. Reversing the archive order is unsafe: the older context
delta's target still contains `STRUCTURAL_INDEX` and would restore it after this
change removed it. After the ordered archives, wipe and reseed beta state; no
legacy bucket alias, reader, translation, dual write, online migration, or
compatibility layer is provided. Known downstream configurations and generated
bindings must delete the three retired fields before adopting this release.

`task e2e:statistical` is the breaking E2E gate because it composes
graph-clustering and currently carries the retired configuration and physical
stages. Checked-in statistical/semantic deployments intentionally keep anomaly
detection disabled, so the fresh in-memory anomaly path requires a real-NATS
component integration test in addition to the E2E gate.

## Impact

- **Affected specs:** `graph-clustering` and `graph-retention`.
- **Affected runtime:** graph-clustering configuration, generated ports,
  structural computation, anomaly initialization, and the framework KV catalog.
- **Affected tests:** structural storage tests, graph-clustering integration,
  statistical E2E bucket clients/stages/results, and comparison DTOs.
- **Affected adopters:** SemStreams, SemDragon, and SemSpec configurations and
  generated schema/type bindings; stale clean-cutover lists remain only as retired
  beta-state cleanup.
- **Preserved contract:** anomaly detectors receive fresh K-core and pivot inputs
  computed from the same cycle's explicit plus EntityID-derived structural graph.

## Downstream breaking migration list

A read-only sibling-repository census found no structural-bucket reader. The
following adopter surfaces carry the three retired configuration fields and must
delete them before consuming this release:

- SemDragon: `config/semdragons.json` and
  `config/semdragons-semsource.json`.
- SemSpec: `configs/e2e-claude.json`, `e2e-gemini.json`,
  `e2e-hybrid.json`, `e2e-hybrid-gpt5.json`, `e2e-local.json`,
  `e2e-openrouter.json`, and `e2e-sparky.json`.

The following generated mirrors must be regenerated from the new SemStreams
schema rather than manually preserving the removed fields:

- SemSpec: `ui/src/lib/types/semstreams.generated.ts`.
- SemTeams: `schemas/graph-clustering.v1.json` and
  `ui/src/lib/types/api.generated.ts`.
- SemStreams UI: `src/lib/types/api.generated.ts`.

No replacement fields or output port are required. An adopter that needs anomaly
detection keeps only `enable_anomaly_detection`; an adopter that does not enable
it does no structural computation.
