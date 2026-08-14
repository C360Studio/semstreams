# Post-G tag-safety closeout design

**Status:** The prior exact checksum-addressed target state received independent `DESIGN PASS`. The repository owner
approved all binding design rulings on 2026-08-11. Runtime slices #855 and #875, research execute/fusion proof, and
truth/disposition merged as #933, #934, #936, and #937. Decision F implementation, independent implementation review,
and the live pre-candidate #860 proof are complete and green with exact `9/0/3` deltas. Decision G fresh-state truth
propagation, refreshed manifest generation, strict validation, and submission of the immutable amended package for
review are complete. Candidate freeze requires external independent approval over this exact package; that verdict
does not mutate covered artifacts. Candidate selection and proof have not begun; no product tag exists.

**Baseline:** `4593996ef56f50766dcf58fe2200081b72a59133`

**Accepted inventory:** `docs/proposals/post-g-foundation-remap-inventory.md`, SHA-256
`8368e9b17e869561ca5c2123c8028d1311e449dae930c483d450c627a4acfcc6`.

**Package content root:** `openspec/changes/post-g-tag-safety-closeout/manifest.sha256`. The manifest covers every
OpenSpec artifact beneath this change directory except itself. It is regenerated once as the final in-tree preparation
step before candidate selection, then only verified on the immutable candidate. No covered artifact records or
requires the manifest's digest. Candidate proof records the selected manifest's digest after the candidate exists.

## Boundary

#855 and #875 remain the original runtime correctness slices. Decision F adds only the bounded `processor/rule`
observability/optional-notification correction required to make #860 truthful. Research work proves an existing
execution path. Release work corrects truth and establishes the external candidate-proof and release-attestation
contracts.

The bounded pre-candidate correction changes test truth plus one narrow production observability and optional-
notification seam. The existing crud-tools scenario must fail closed when #860's required metrics proof is
unreachable or does not converge. Rule processors must expose admission through a dedicated gate-pass counter and
must treat an absent optional `rule_events` output as disabled, not as a failed publish. The integration-runner
contract test must give `Cmd.Wait` one owner across timeout and cleanup. Exact-candidate Go commands bypass the test
cache and the focused command covers core graph packages as well as processor wrappers. No workflow behavior changes.

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
| Rule processor operator | Nothing for shipped configurations; `rule_events` remains optional. | Actions still execute. No notification is attempted and no missing-port warning is emitted; admitted gates remain visible by rule name. | `semstreams_rule_action_gate_passes_total{rule_name}` and existing explicit malformed/publish failure telemetry. | No dummy notification port or inference from a delivery counter. |
| Release owner | Select one immutable SHA, publish its non-product candidate proof, then publish a separate product-Release attestation. | Tag authorization is blocked until every pre-tag gate is green. | The checksum-addressed change package and evidence schema. | No evidence cycle, self-reference, inference from issue labels, wrapper silence, or downstream guesses. |
| Downstream repository | Pin the exact published tag, then migrate and test. | It remains safely on its prior pin. | Product Release notes and version-independent migration guide. | No pre-tag lockstep or compatibility shim. |

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
- adds version-independent release guidance; exact tag and artifact facts remain product-Release-only.

Owner-approved policy (2026-08-11): #301, #844, and #860 remain advertised. Each exact candidate must run its named
path green or tag authorization stops. D authorizes neither a fix nor removal if one is red, and an honest nonzero
wrapper result cannot be relabeled as harness success.

The remaining owner dispositions are binding:

- DI-01 through DI-03, #619, #672, and temporal malformed/reverse cleanup defer to the Derived-Index Convergence
  Program;
- DI-04 defers to the Anomaly Lifecycle and Retention Program;
- #839 is an accepted measured community-value limitation for this tag;
- #857 defers to the Payload Bounds and Retention Program; and
- #829 defers to the Semantic Summary Content/Quality Program.

Each accepted or deferred limitation is published in product GitHub Release notes. Inventory presence alone is not
conformance, does not authorize implementation, and cannot silently expand runtime scope.

`disposition-ledger.md` is the authoritative in-tree decision record. The repository owner owns binding decisions;
the technical writer owns faithful transcription and conservative task truth. The ledger records decision date and
coverage/publication plan, but it cannot contain the SHA of the candidate commit that contains it. Candidate identity,
exact command/results, timestamps, and evidence pointers live in the immutable `candidate-proof-<fullSHA>` Release
asset. Tag, artifact, fresh-state publication, and final decision facts live in the separate immutable product-Release
attestation. The in-tree `candidate-evidence.md` is only the schema for those external records.

## Same-class collision result

| Dimension | Result |
|---|---|
| Community writer | The detector remains the sole partition writer. No manifest, generation, or rollback writer is added. |
| Community lifecycle | Save candidate, require completeness, then prune remains the sole replacement lifecycle. |
| Store identity | `StoreRegistry` is the sole `StorageInstance → store` authority. The owned fallback is removed. |
| Ports | Existing store-provide/store-read federation remains; no second port or config vocabulary appears. Shipped rule processors remain valid without `rule_events`. |
| Rule observability | One gate-pass counter observes admitted actions independently of optional notification delivery. |
| Research | Existing rules, subjects, payloads, components, and fusion remain. Fixtures observe them. |
| E2E ownership | Existing research task owns both branch runs; no parallel tier capability is created. |
| Release truth | `disposition-ledger.md` binds decisions; `candidate-evidence.md` defines both external schemas. Exact-SHA pre-tag proof and post-publication attestation remain separate. Both in-tree artifacts are covered by `manifest.sha256`. |
| Frozen change | `semantic-tier-split` stays suspended and does not own this release proof. |

## Decision E: bounded pre-candidate proof truth correction

The retained #860 proof is meaningful only when the crud-tools scenario observes the required rule metrics. The
scenario SHALL fail when the required metrics scrape is unreachable, when the active-rule gauge does not converge
after hot reload, or when the exact Decision F deltas do not appear. An absent labeled CounterVec series before its
first increment remains a valid observed zero only after a successful scrape has established that the metrics
collector is reachable. Absence is not equivalent to an unreachable scrape.

The integration-runner contract helper SHALL give exactly one goroutine ownership of `Cmd.Wait`. A caller may time
out without creating a second waiter; cleanup kills the process when necessary and waits on the same owner's
completion signal. Targeted race coverage proves timeout cleanup reaps the process and preserves the killed result for
subsequent observations.

Every exact-candidate `go test` command uses `-count=1`. The focused command includes `./graph/clustering` and
`./graph/embedding` alongside both processor wrappers, store registry, test infrastructure, and the retained scenario
packages. These proof corrections require focused verification plus independent review before candidate selection.

## Decision F: optional rule notification and action-gate observability

The repository's shipped rule processors do not declare a `rule_events` output port. That port is an optional
notification seam, not a prerequisite for rule action execution. When the port is absent, the processor SHALL skip
notification construction and publication without a publish attempt, error, or warning. Rule execution and graph-
event delivery continue through their existing paths.

Absence is distinct from explicit failure. If `rule_events` is present but its facts or subject declaration is
malformed, that failure remains observable through the existing error path. If its configured publication fails, the
failure remains observable through the existing warning/error telemetry. Neither case may be silently downgraded to
the absent-port behavior.

The rule processor adds
`semstreams_rule_action_gate_passes_total{rule_name}`. It increments exactly once after a match is admitted by
`FireEveryNEvents` and before rule-event execution or any delivery attempt. It does not increment for matches rejected
by the gate. Because its position is independent of optional notification, empty action lists, malformed actions,
notification publication, and graph-event delivery cannot fabricate or erase an admission already counted.

The retained #860 live proof sends nine matching events to a rule configured with `FireEveryNEvents = 3`. For the
named rule it requires exact deltas of nine from
`semstreams_rule_evaluations_total{result="triggered"}`, zero from
`semstreams_rule_evaluations_total{result="not_triggered"}`, and three from
`semstreams_rule_action_gate_passes_total`. It no longer reads or infers admission from
`semstreams_rule_events_published_total`. A missing metric, scrape failure, non-converging delta, or any different
exact delta is red. The live pre-candidate run is green at exact `9/0/3`; #860 remains a retained gate that must be
rerun on the exact candidate under E.4 after candidate selection.

## Decision G: fresh-state stable-release premise

Accepted on 2026-08-11 from `/private/tmp/fresh-state-release-inventory.md`, SHA-256
`b91a8d24a22eae2f44f42864798f593e61428d7e13fc05dce0081b73d0dfa348`, against baseline
`c893cd53a958e5f79c23b93cc2b0ba23f2f342a1`.

Every downstream product adopting the stable release starts on newly provisioned NATS storage. Owner-confirmed
deployment state contains no existing NATS data to migrate, preserve, wipe, or reseed. The release provides no
compatibility reader, alias, dual format, online migration, or rollback. Discovery of retained deployed state blocks
that adoption and requires a separate owner-reviewed migration or recovery design.

This is a release premise, not runtime behavior. Historical cutover records remain evidence, while typed
`graph_state_reset_required` handling remains the scoped response to observed graph poison. Cold replay stays
fail-closed until the authoritative watermark, ordinary backup/recovery remains valid, and optional
`AGENT_TRAJECTORIES` degradation remains independent.

The release owner records the binding ruling and decision reference; they do not predict an operator, window, or
destructive action for absent state. Product Release notes tell downstream owners to provision fresh storage and stop
only the affected adoption if retained deployed state is found. This truth must be propagated through current specs,
evidence schemas, role contracts, and live guidance before candidate freeze.

## Candidate and tag proof

The candidate commit cannot contain or predict its own SHA. Candidate freeze therefore means only selecting one clean
immutable SHA after all in-tree preparation, including the one final manifest regeneration. The selected candidate
verifies the manifest; it does not regenerate or edit it.

After selection, proof runs collect local/run evidence. Only a fully green candidate may receive a non-product GitHub
Release tag named `candidate-proof-<fullSHA>` at that exact SHA and its immutable proof asset. A red gate rejects the
candidate without requiring a failed proof Release. The tag's non-`v` name prevents the current product release and
container workflows from treating it as a product release. The asset body does not contain or require its own URL or
SHA-256; GitHub Release metadata or a sibling checksum may supply those facts after upload.

All proof is rerun on the selected SHA using these exact commands:

```text
go test -count=1 -race ./graph/clustering ./graph/embedding ./processor/graph-clustering ./processor/graph-embedding ./storage/storeregistry ./test/testinfra ./test/e2e/scenarios/crud-tools ./test/e2e/scenarios/research-graph
task lint
go test -count=1 -race ./...
task test:integration
task schema:generate
task schema:check-changes
go test -count=1 ./test/contract/...
task openspec:validate
task e2e:statistical
task e2e:semantic
task e2e:agentic
task e2e:research-graph
task e2e:deep-research
task e2e:crud-tools
task e2e:ops
```

The semantic gate adds active 30–60 second polling of `/readyz`, authoritative counters, and stage timestamps. The
single research invocation proves isolated direct and execute/fusion rounds. The single crud-tools invocation proves
distinct #301 and #860 assertions; #860 records exact deltas of nine triggered, zero not-triggered, and three action-
gate passes without using `semstreams_rule_events_published_total`. The ops invocation proves #844.

A provably wedged paid run is aborted rather than left to timeout. Independent SemStreams review and green GitHub CI
are tied to the same complete candidate SHA. Any correction selects a new candidate SHA and invalidates affected
proof, review, and CI. The release owner may authorize tagging only after every required pre-tag gate is green and
the binding fresh-storage ruling and decision reference are recorded.

At the product tag boundary, the tag must resolve to the authorized SHA and binary and container outputs must report
the intended version. No destructive storage operation is part of release publication.

After publication, a separate immutable asset on the product GitHub Release links and digests candidate proof. It
records tag resolution, binary/container identity, inclusion of the fresh-storage premise in Release notes, that no
destructive storage operation was performed, the final release decision, and limitations. The candidate tree is
never edited after proof. Downstream repositories pin that tag afterward and are not exhaustive pre-tag blockers;
discovery of retained deployed state blocks only the affected adoption pending separate owner review.

## Risks

- Partial community writes can overwrite mappings and expose a mixed prior/candidate projection. Preventing prune/deletion and false completion is the bounded #855 guarantee; atomic rollback is not introduced.
- Removing the fallback may exclude bodies in deployments that relied on an unnamed bucket. This is an intentional
  clean correction: register the named owner rather than add a shim.
- A store deregistration race can exclude one revision's body. The outcome remains observable and non-degrading.
- Two isolated research branch runs increase E2E time. Isolation provides deterministic attribution and avoids
  state-dependent mock sequencing.
- Omitting `rule_events` suppresses only the optional notification. Explicitly configured malformed ports and publish
  failures remain observable, so operators do not lose signal for configuration or transport defects.
- Red candidate proof can block tag authorization. That is the purpose of the gate.

## Binding owner rulings

The repository owner approved these rulings on 2026-08-11:

- Exact `StorageInstance` registration in `StoreRegistry` is the sole resolution authority; implementation removes the
  unnamed fallback accepted by ADR-063.
- #301, #844, and #860 are retained. Each named path must run green on the exact candidate or tag authorization
  stops. D does not authorize a fix if red, and wrapper output is not relabeled to manufacture success.
- DI-01 through DI-03, #619, #672, and temporal malformed/reverse cleanup defer to the Derived-Index Convergence
  Program and publish as limitations.
- DI-04 defers to the Anomaly Lifecycle and Retention Program and publishes as a limitation.
- #839 is an accepted measured community-value limitation for this tag.
- #857 defers to the Payload Bounds and Retention Program and publishes as a limitation.
- #829 defers to the Semantic Summary Content/Quality Program and publishes as a limitation.
- Decisions and templates stay in-tree. Exact candidate identity and run evidence live in an immutable non-product
  candidate-proof Release; post-publication facts live in a separate product-Release attestation.
- The deterministic research execute fixture uses `walk_seeds` with controlled nonzero evidence and the assertions
  above.
- Shipped rule processors need no `rule_events` output. Its absence silently disables only the optional notification;
  explicit malformed/publish failures remain observable. The dedicated per-rule gate-pass counter increments once
  after `FireEveryNEvents` admission and before execution or delivery, and #860 requires exact live deltas `9/0/3`.

## Binding owner-ruling conformance

| Owner-approved ruling | Result | Target-state locations |
|---|---|---|
| Exact `StorageInstance` registration in `StoreRegistry` is the sole authority; remove the ADR-063 unnamed fallback. | CONFORMS | `docs/adr/063-store-substrate-and-resolver.md`; `openspec/changes/post-g-tag-safety-closeout/design.md`; `openspec/changes/post-g-tag-safety-closeout/specs/graph-embedding/spec.md` |
| Retain #301/#844/#860 and require each exact-candidate path green; do not relabel wrapper failure. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md`; `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md` |
| Defer derived-index, anomaly, payload, and semantic-summary findings to their named programs; accept #839 for this tag. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md`; `openspec/changes/post-g-tag-safety-closeout/disposition-ledger.md`; `docs/operations/migration-post-g-tag-safety-closeout.md` |
| Keep decisions/templates in-tree; separate exact-SHA candidate proof from post-publication attestation. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md`; `openspec/changes/post-g-tag-safety-closeout/candidate-evidence.md`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md` |
| Bind the deterministic research execute fixture to `walk_seeds`. | CONFORMS | `openspec/changes/post-g-tag-safety-closeout/design.md`; `openspec/changes/post-g-tag-safety-closeout/specs/framework-composition/spec.md`; `openspec/changes/post-g-tag-safety-closeout/tasks.md` |
| Keep `rule_events` optional and prove #860 through the dedicated post-gate counter with exact `9/0/3` live deltas. | CONFORMS; PRE-CANDIDATE LIVE PROOF GREEN, EXACT-CANDIDATE RERUN PENDING | `openspec/changes/post-g-tag-safety-closeout/design.md`; `openspec/changes/post-g-tag-safety-closeout/specs/rule-action-observability/spec.md`; `openspec/changes/post-g-tag-safety-closeout/specs/release-candidate-proof/spec.md`; `openspec/changes/post-g-tag-safety-closeout/tasks.md` |
