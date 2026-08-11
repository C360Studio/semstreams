# Post-G tag-safety closeout design

**Status:** Corrected exact checksum-addressed target state received independent `DESIGN PASS`. The repository owner
approved all binding design rulings on 2026-08-11. Runtime slices #855 and #875 and the research execute/fusion proof
merged as #933, #934, and #936. The bounded truth/disposition slice is in progress; candidate proof has not begun.

**Baseline:** `4593996ef56f50766dcf58fe2200081b72a59133`

**Accepted inventory:** `docs/proposals/post-g-foundation-remap-inventory.md`, SHA-256
`8368e9b17e869561ca5c2123c8028d1311e449dae930c483d450c627a4acfcc6`.

**Package content root:** `openspec/changes/post-g-tag-safety-closeout/manifest.sha256`. The manifest covers every
OpenSpec artifact beneath this change directory except itself and is regenerated after any covered edit. It is the
in-tree package content root; no covered artifact records or requires the manifest's digest. The detached attestation
records the candidate's manifest digest after the candidate exists; no additional in-tree sidecar is required.

## Boundary

Only #855 and #875 are runtime correction slices. Research work proves an existing execution path. Release work
corrects truth and establishes the detached candidate-evidence contract.

DI-01 through DI-03, #619, #672, and temporal poison/cleanup are deferred to the Derived-Index Convergence Program.
DI-04 is deferred to the Anomaly Lifecycle and Retention Program. #857 is deferred to the Payload Bounds and
Retention Program, and #829 is deferred to the Semantic Summary Content/Quality Program. #839 is an accepted measured
community-value limitation for this tag. Hierarchy, reclamation, and generic readiness remain disposition-only.
Promoting any of them requires a separate owner-approved amendment.

The design stays on current NATS KV, component ports, `StorageReference`, `StoreRegistry`, research rules, and fusion.
It adds no generic abstraction or adopter-facing knob.

## Adopter seam

| Adopter | Must know | If they do nothing | Discovery | Ideal bill |
|---|---|---|---|---|
| `Storable` producer | Stamp the existing exact logical `StorageInstance`. | A foreign or missing name excludes the body; inline identity may still embed. The miss alone never selects another store or degrades embedding. | Existing storage registration contract, warning, and `content_unresolved` metric. | No bucket, default store, subject, readiness, or payload-ceiling prediction. |
| Embedding operator | Run the named storage component that owns the reference. | Exact registered refs resolve; missing names are excluded. | Existing component/store discovery and metrics. | No duplicate wiring or fallback store. |
| Clustering/query adopter | Nothing new. | An incomplete run cannot prune or claim completion, but successful or partial writes may overwrite entity mappings and readers may observe a mixed prior/candidate projection. | Existing clustering query behavior and error telemetry. | No knowledge of NATS payload ceilings or partition internals. |
| Research adopter | Existing direct and non-trivial routes remain. | The capability behaves as today; the repository gains deterministic proof of both. | Existing `research_graph` result and E2E evidence. | No rule subjects, KV keys, or fixture mechanics. |
| Release owner | Keep decisions in the in-tree ledger, then publish an immutable detached GitHub Release attestation keyed to the exact candidate SHA. | Candidate freeze is blocked until every required detached field is complete. | The checksum-addressed change package and attestation schema. | No self-referential SHA, inference from issue labels, wrapper silence, or downstream guesses. |
| Downstream repository | Pin the exact published tag, then migrate and test. | It remains safely on its prior pin. | Release notes and migration guide. | No pre-tag lockstep or compatibility shim. |

## Options

### Community persistence

| Option | Result | Ruling |
|---|---|---|
| Keep partial permanent failures as success | Preserves writable siblings but permits destructive prune and false completion. | Rejected. |
| Predict payload size or chunk values | Expands into #839/#857 and asks the framework to predict a store outcome. | Rejected. |
| Save writable siblings, observe all outcomes, fail incomplete candidate before prune | Preserves #837, prevents deletion and false success, and uses the actual store result. | Recommended. |
| Add a transaction/generation manifest | Adds durable coordination, recovery, and reader semantics beyond the defect. | Rejected. |

### Storage-reference resolution

| Option | Result | Ruling |
|---|---|---|
| Keep registry-first plus unnamed fallback | Preserves the wrong-store ambiguity. | Rejected. |
| Resolve only the exact registered name; exclude misses | One identity authority, no prediction, existing exclusion semantics. | Owner-approved on 2026-08-11. |
| Validate a configured fallback against inferred bucket/name | Creates a second identity authority and compatibility mechanism. | Rejected. |
| Redesign generic storage and ports | Expands beyond #875. | Rejected. |

### Research proof

| Option | Result | Ruling |
|---|---|---|
| Keep direct-only proof | Leaves execute/fusion unproven. | Rejected. |
| Replace direct proof | Loses the direct-route negative assertions. | Rejected. |
| Run isolated direct and execute fixtures under the existing research task | Deterministic attribution with no production surface. | Owner-approved on 2026-08-11; the execute fixture uses `walk_seeds`. |
| Add another E2E tier | Collides with the frozen tier work and adds unnecessary operator surface. | Rejected. |

## Decision A: complete-candidate gate for #855

`SaveCommunity` remains observational: call the store and classify its real result. No size preflight is introduced.

For a record-local permanent/invalid rejection, detection continues attempting the remaining communities at that
level so writable siblings persist. Context cancellation, transient failure, and other non-invalid failures continue
to abort immediately.

After the level's attempts, any rejected community makes the candidate incomplete. The detector returns an error that
wraps the existing classified storage error and preserves `errs.IsInvalid`. It returns no successful run to its caller.
No new exported error or result type is introduced.

Because `DetectCommunities` receives an error:

- it does not construct higher levels from an incomplete lower level;
- it does not invoke `Prune`;
- `runCommunityDetection` does not increment processed/activity state;
- detection duration is not recorded as a completed run;
- `community detection complete` is not logged; and
- structural/anomaly processing does not run.

Successful and partial candidate writes may already have overwritten a same-key community record or one or more entity mappings. Readers may therefore observe a mixed prior/candidate projection. The exact incomplete-run guarantee is narrower and testable: detection does not invoke `Prune`, the storage layer performs no prune-driven `Delete`, and the run does not report complete success. This is not a byte-identical rollback or stale-superset guarantee. A later complete run overwrites current rows and SHALL attempt the existing prune.

A genuinely empty authority graph is a complete candidate and SHALL attempt `Prune(ctx, nil)`. Every complete non-empty candidate SHALL likewise attempt prune. A prune failure remains nonfatal because every candidate community persisted; readers may retain stale keys until a later complete prune succeeds.

No payload limit, member limit, chunk, manifest, new bucket, or configuration is introduced.

## Decision B: instance-exact resolution for #875

`StorageReference.StorageInstance` is the logical owner name. Only
`StoreRegistry.Streamable(ref.StorageInstance)` may produce the store used for that reference.

This explicitly supersedes the registry-miss owned fallback in ADR-063 lines 362-372. Implementation removes:

- the worker's owned `contentStore` resolution fallback;
- `WithContentStore`;
- graph-embedding's duplicate direct ObjectStore construction, ownership, and close path; and
- tests and comments that make any wired store sufficient for a reference.

The existing `store-read` declaration and injected registry lifecycle remain. Port/schema redesign is outside this
slice.

For the ordinary path, the component checks the exact registry name. A miss takes the existing explicit exclusion
path, increments `content_unresolved`, emits a bounded actionable warning, and continues through inline text
extraction. Inline text may embed; an entity with no text reaches the existing skipped/no-text terminal and stale-vector
cleanup. Neither outcome enters failed/degraded state merely because the instance was unresolved.

Resolution remains lazy per fetch, so deregistration can occur after component admission but before worker fetch. The
worker must therefore represent a registry miss as a private unresolved/excluded outcome. It reports
`content_unresolved`, combines no body with the record's existing `IdentityText`, and either embeds that identity or
takes the no-text skip. It must not call `SaveFailed`, increment failure reasons, or degrade readiness. A private
sentinel, callback, or equivalent internal branch is acceptable; no public type is added.

An exact name that resolves to a store whose `Open` or `Read` fails remains a content failure. That is an observed
infrastructure fault after successful identity resolution and continues to enter bounded failed/degraded accounting.

A miss followed by later registration may leave the body excluded for that entity revision. This is accepted eventual
consistency; no registry watch, retry knob, or alternate resolution path is added.

## Decision C: deterministic research execution proof

Production orchestration is unchanged:

- R2 dispatches `walk_seeds` and `decompose` to `execute_subqueries`;
- R3 dispatches execute completion to assessment;
- R4 sends sufficient assessment to synthesis and bounds refinement; and
- production `executeAll` calls `fusion.Fuse`.

The existing `synthesize_directly` fixture remains intact and continues asserting:

- positive classifier candidate count;
- exact direct route;
- execute and assess stamps absent;
- search-result completion;
- completion envelope; and
- R6 continuation.

A second explicit fixture selects a deterministic non-trivial route. The owner-approved bounded implementation is an
isolated mock/scenario mode returning `walk_seeds` with the existing seeded candidate index, sufficient assessment, and
synthesis quoting the returned evidence. Direct and execute modes run as isolated compose rounds under the existing
`task e2e:research-graph`. No prompt-quality inference chooses the test branch.

The execute fixture must assert:

- exact non-trivial route;
- `research.execute.complete`;
- positive integer `research.execute.evidence-count`;
- nonempty execution evidence containing the controlled seeded entity or exact controlled reference;
- `research.assess.complete`;
- `research.assess.sufficient = true`;
- terminal search-result completion;
- synthesis evidence references drawn from execution evidence;
- completion envelope; and
- R6 continuation.

This proves the production execute/fusion path. A unit hook that bypasses the component/rule/NATS path is insufficient.

## Decision D: truth and disposition

The implementation documentation slice:

- updates ADR-063 to remove the accepted fallback ruling and record exact-name-only resolution;
- corrects ADR-068 to current raw-predicate, hashed-name, source-owned incoming/outgoing cleanup; records
  `CONTEXT_INDEX` as retired, not cataloged, and not created; and records that no `PREDICATE_CATALOG` exists;
- adds a premise-status annotation to suspended `semantic-tier-split` without unfreezing it or completing tasks; and
- corrects stale comments in `e2e-ladder.yml` and `sister-validation.yml` without changing workflow behavior; and
- adds a candidate-aware migration draft that cannot claim an exact tag or outcome before detached proof exists.

Owner-approved policy (2026-08-11): #301, #844, and #860 remain advertised. Each exact candidate must run its named
path green or candidate freeze stops. D authorizes neither a fix nor removal if one is red, and an honest nonzero
wrapper result cannot be relabeled as harness success.

The remaining owner dispositions are binding:

- DI-01 through DI-03, #619, #672, and temporal malformed/reverse cleanup defer to the Derived-Index Convergence
  Program;
- DI-04 defers to the Anomaly Lifecycle and Retention Program;
- #839 is an accepted measured community-value limitation for this tag;
- #857 defers to the Payload Bounds and Retention Program; and
- #829 defers to the Semantic Summary Content/Quality Program.

Each accepted or deferred limitation is published with the candidate. Inventory presence alone is not conformance,
does not authorize implementation, and cannot silently expand runtime scope.

`disposition-ledger.md` is the authoritative in-tree decision record. The repository owner owns binding decisions;
the technical writer owns faithful transcription and conservative task truth. The ledger records decision date and
coverage/publication plan, but it cannot contain the SHA of the candidate commit that contains it. Candidate identity,
exact command/results, timestamps, and evidence pointers live in the immutable detached GitHub Release attestation
keyed to the candidate's full SHA. The in-tree `candidate-evidence.md` is only the schema for that external record.

## Same-class collision result

| Dimension | Result |
|---|---|
| Community writer | The detector remains the sole partition writer. No manifest, generation, or rollback writer is added. |
| Community lifecycle | Save candidate, require completeness, then prune remains the sole replacement lifecycle. |
| Store identity | `StoreRegistry` is the sole `StorageInstance → store` authority. The owned fallback is removed. |
| Ports | Existing store-provide/store-read federation remains; no second port or config vocabulary appears. |
| Research | Existing rules, subjects, payloads, components, and fusion remain. Fixtures observe them. |
| E2E ownership | Existing research task owns both branch runs; no parallel tier capability is created. |
| Release truth | `disposition-ledger.md` binds decisions; `candidate-evidence.md` defines the schema only. Exact-SHA proof lives in the detached GitHub Release attestation. Both in-tree artifacts are covered by `manifest.sha256`. |
| Frozen change | `semantic-tier-split` stays suspended and does not own this release proof. |

## Candidate and tag proof

The candidate commit cannot contain or predict its own SHA. The authoritative evidence artifact is therefore an
immutable detached GitHub Release attestation keyed to the candidate's full SHA and created only after that commit
exists. `candidate-evidence.md`, covered by the package manifest, is the in-tree schema/template and MUST NOT be
completed as evidence or redefine candidate identity. The release owner owns the detached attestation; the technical
writer has custody of its schema and validates faithful completion. A missing required detached field blocks freeze.
The attestation body does not contain or require its own SHA-256. A digest may appear only in external GitHub Release
metadata or a sibling checksum asset created after upload, and it does not redefine candidate or attestation identity.

Candidate freeze records one clean SHA and confirms generated schemas/specs are clean. All proof is rerun on that SHA:

- focused affected-package tests;
- `task lint`;
- `go test -race ./...`;
- `task test:integration`;
- `task schema:generate` followed by a clean generated-schema/spec diff;
- `go test ./test/contract/...`;
- strict OpenSpec validation;
- statistical E2E;
- semantic E2E with active 30–60 second polling of `/readyz`, authoritative counters, and stage output;
- agentic E2E;
- direct and execute research-graph branches;
- deep-research E2E; and
- retained advertised crud-tools/ops/rule paths required by the disposition ledger.

A provably wedged paid run is aborted rather than left to timeout. Any fix changes the candidate SHA and invalidates
earlier review, CI, and detached candidate evidence.

Independent SemStreams review is tied to the complete exact-candidate diff and SHA. GitHub CI must be green on that
same commit. Release notes name clean breaks and accepted limitations.

The #827 wipe/reseed is scheduled at the tag boundary. If the permitted pre-v1 window closes first, tagging halts and
the operation becomes an explicit migration.

The tag must resolve to the approved SHA. Binary and container outputs must report the intended version, and the
container tag/digest must be recorded in the detached attestation. Downstream repositories pin that tag afterward;
they are not exhaustive pre-tag blockers.

## Risks

- Partial community writes can overwrite mappings and expose a mixed prior/candidate projection. Preventing prune/deletion and false completion is the bounded #855 guarantee; atomic rollback is not introduced.
- Removing the fallback may exclude bodies in deployments that relied on an unnamed bucket. This is an intentional
  clean correction: register the named owner rather than add a shim.
- A store deregistration race can exclude one revision's body. The outcome remains observable and non-degrading.
- Two isolated research branch runs increase E2E time. Isolation provides deterministic attribution and avoids
  state-dependent mock sequencing.
- Release disposition can block freeze. That is the purpose of the gate.

## Binding owner rulings

The repository owner approved these rulings on 2026-08-11:

- Exact `StorageInstance` registration in `StoreRegistry` is the sole resolution authority; implementation removes the
  unnamed fallback accepted by ADR-063.
- #301, #844, and #860 are retained. Each named path must run green on the exact candidate or freeze stops. D does not
  authorize a fix if red, and wrapper output is not relabeled to manufacture success.
- DI-01 through DI-03, #619, #672, and temporal malformed/reverse cleanup defer to the Derived-Index Convergence
  Program and publish as limitations.
- DI-04 defers to the Anomaly Lifecycle and Retention Program and publishes as a limitation.
- #839 is an accepted measured community-value limitation for this tag.
- #857 defers to the Payload Bounds and Retention Program and publishes as a limitation.
- #829 defers to the Semantic Summary Content/Quality Program and publishes as a limitation.
- Decisions and templates stay in-tree. Exact candidate identity and run evidence live in an immutable detached GitHub
  Release attestation keyed to that SHA; the candidate commit does not predict itself.
- The deterministic research execute fixture uses `walk_seeds` with controlled nonzero evidence and the assertions
  above.

## Binding owner-ruling conformance

| Owner-approved ruling | Result | Target-state locations |
|---|---|---|
| Exact `StorageInstance` registration in `StoreRegistry` is the sole authority; remove the ADR-063 unnamed fallback. | CONFORMS | `docs/adr/063-store-substrate-and-resolver.md:11`; `docs/adr/063-store-substrate-and-resolver.md:12`; `docs/adr/063-store-substrate-and-resolver.md:371`; `docs/adr/063-store-substrate-and-resolver.md:373`; `openspec/changes/post-g-tag-safety-closeout/design.md:98`; `openspec/changes/post-g-tag-safety-closeout/design.md:100`; `openspec/changes/post-g-tag-safety-closeout/specs/graph-embedding/spec.md:5`; `openspec/changes/post-g-tag-safety-closeout/specs/graph-embedding/spec.md:6` |
| Retain #301/#844/#860 and require each exact-candidate path green; do not relabel wrapper failure. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md:179`; `openspec/changes/post-g-tag-safety-closeout/design.md:180`; `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md:19`; `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md:21`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md:5`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md:7` |
| Defer derived-index, anomaly, payload, and semantic-summary findings to their named programs; accept #839 for this tag. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md:183`; `openspec/changes/post-g-tag-safety-closeout/design.md:190`; `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md:22`; `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md:31`; `docs/operations/migration-post-g-tag-safety-closeout.md:66`; `docs/operations/migration-post-g-tag-safety-closeout.md:78` |
| Keep decisions/templates in-tree and exact-SHA evidence in a detached immutable attestation. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md:214`; `openspec/changes/post-g-tag-safety-closeout/design.md:221`; `openspec/changes/post-g-tag-safety-closeout/candidate-evidence.md:11`; `openspec/changes/post-g-tag-safety-closeout/candidate-evidence.md:28`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md:53`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md:59` |
| Bind the deterministic research execute fixture to `walk_seeds`. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md:130`; `openspec/changes/post-g-tag-safety-closeout/design.md:148`; `openspec/changes/post-g-tag-safety-closeout/design.md:151`; `openspec/changes/post-g-tag-safety-closeout/specs/framework-composition/spec.md:11`; `openspec/changes/post-g-tag-safety-closeout/specs/framework-composition/spec.md:47`; `openspec/changes/post-g-tag-safety-closeout/tasks.md:53`; `openspec/changes/post-g-tag-safety-closeout/tasks.md:57` |
