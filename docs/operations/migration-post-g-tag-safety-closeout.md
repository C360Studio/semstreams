# Post-G tag-safety migration guide

> **Status: version-independent operator guidance, not a release notice.** Exact candidate, tag, retained-path, and
> artifact outcomes exist only in immutable candidate proof and product GitHub Release material. This file is never
> edited after candidate proof to inject release facts.

This closeout prepares one stable SemStreams tag for downstream pin-and-migrate work. It keeps SemStreams' existing
flow, graph mutation, storage-reference, NATS, and eventual-consistency model. It adds no compatibility shim or
deprecated route.

## Clean break: storage references resolve by exact instance

`StorageReference.StorageInstance` is the logical storage owner name. Graph embedding now reads an offloaded body only
from the live `StoreRegistry` entry registered under that exact name.

Operators and component authors must ensure the storage component that created a reference is running and registered
under the same `StorageInstance` value. Do not configure or expect a default bucket, unnamed content store, or fallback
store.

If the exact name is absent:

- the framework increments `semstreams_graph_embedding_content_unresolved_total` and emits a bounded warning;
- the offloaded body is excluded for that entity revision;
- inline identity text may still be embedded;
- an entity with no remaining text takes the existing no-text skip and stale-vector cleanup; and
- the miss alone does not make embedding failed or degraded.

If the exact store resolves but `Open` or `Read` fails, the result remains a real content failure with existing
failed/degraded accounting. A later registration does not automatically replay an already excluded revision; this is
accepted eventual consistency.

## Community detection after an incomplete save

A record-local permanent rejection no longer permits the candidate partition to prune prior state or report complete
success. Writable sibling communities at the same level may still persist before the detector returns the classified
error. Partial community and entity-mapping writes are not rolled back, so readers may temporarily observe a mixed
prior/candidate projection. A later complete run converges the view and attempts prune.

This protects prior state from destructive prune; it does not make oversized community values succeed. #839 remains
an accepted limitation for this tag: a community value can exceed the NATS payload ceiling and make that detection run
incomplete.

## Research path proof

The existing `task e2e:research-graph` now runs two isolated fixtures:

1. the preserved `synthesize_directly` path, including negative execute/assessment assertions; and
2. a deterministic `walk_seeds` path through production execute, `fusion.Fuse`, assessment, synthesis, completion
   envelope, and R6 continuation.

This is proof of existing behavior. It adds no production rule, subject, payload, component, or fusion policy.

## Exact-candidate gates retained

The following advertised paths remain part of SemStreams and must be green on the exact candidate:

| Finding | Required path | Authorization behavior |
|---|---|---|
| #301 | `task e2e:crud-tools` | Any red result stops tag authorization. |
| #844 | `task e2e:ops` | Any red result stops tag authorization. |
| #860 | `task e2e:crud-tools`, including its rule assertions | Any red result stops tag authorization. |

The D documentation slice authorizes no fix if a retained path is red. A fix requires a separately approved change and
creates a new candidate.

## Accepted and deferred limitations

These limitations are disclosed rather than patched during closeout:

- **Accepted for this tag — #839:** a community value can exceed the NATS payload ceiling. #855 prevents destructive
  prune and false completion; it does not add chunking or a larger-value protocol.
- **Derived-Index Convergence Program:** DI-01 suffix collision/retraction, DI-02 alias collision/retraction, DI-03
  spatial stale/malformed aggregates, temporal malformed/reverse cleanup, #619 BM25/dedup lifecycle, and #672
  clustering identity-cache lifecycle.
- **Anomaly Lifecycle and Retention Program:** DI-04 anomaly cleanup, secondary-index, suppression, and retention
  semantics.
- **Payload Bounds and Retention Program:** #857 framework payload bounds and retention policy.
- **Semantic Summary Content/Quality Program:** #829 semantic summary content and quality.

Deferral names an owner boundary, not conformance. None of these dispositions authorizes runtime work or closes an
issue.

## Candidate and release evidence

The release owner first selects one clean immutable candidate SHA and collects cache-disabled command results, active
semantic polling, independent review, exact-SHA CI, retained-path results, and the owner-approved fresh-state ruling
and decision reference. Only a fully green candidate gets the non-product `candidate-proof-<fullSHA>` GitHub Release
and tag authorization. A red candidate is rejected without publishing a failed proof Release.

After the tag boundary, a separate immutable asset on the product Release links the candidate proof and records tag
resolution, artifacts, Release-note inclusion of the fresh-storage premise, no destructive storage operation during
publication, and final limitations. Every downstream product adopting the stable release starts on
newly provisioned NATS storage. Discovery of retained deployed state stops only that adoption and requires a separate owner-reviewed
migration or recovery design.

## Downstream migration

Downstream projects should:

1. remain pinned to their current SemStreams version until the stable tag and product attestation are published;
2. pin the exact published tag;
3. remove any assumption that graph embedding can read an offloaded body from an unnamed or merely wired store;
4. ensure every `StorageReference.StorageInstance` has the intended live provider in the deployed flow;
5. update broken APIs/configuration directly, with no compatibility shim or deprecated surface; and
6. run the downstream project's own product-parity suite after fresh-state adoption.

The ten downstream projects are useful holdout feedback, but they are not exhaustive pre-tag blockers. A downstream
migration failure is fixed in that project or promoted through a new owner-approved framework change; it does not
retroactively add compatibility code to this closeout.
