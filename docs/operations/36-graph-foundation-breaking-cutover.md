# Graph foundation breaking cutover

This note coordinates the pre-v1 cutover to the graph read/write foundation established by
[ADR-091](../adr/091-graph-mutation-authority-without-semantic-ownership.md). It is a migration notice, not a
compatibility plan. SemStreams ships no aliases, deprecated APIs, dual subjects, ownership adapter, or legacy todo
decoder.

For the later strict port, declaration-generation, service-composition, and stream-planning breaks, see the
[port and declaration-generation cutover](./37-port-and-declaration-generation-cutover.md).

The ten named downstream repositories are a holdout set for feature/API parity. Their current implementation choices do
not constrain the foundation or block the SemStreams tag. A finding means the repository must migrate after the tag; it
does not reopen the SemStreams design.

## Runtime contract

- Mutations use one typed `nats-request` port interface: type `semstreams.graph.mutation`, version `v1`, family
  `graph.mutation.>`.
- The four request/reply operations are `entity.create`, `entity.reconcile`, `triple.append`, and `entity.delete`.
- Create is atomic create-or-conflict. Reconcile and delete require a nonzero exact-read revision. Append is must-exist,
  set-valued, and reports one explicit result per subject.
- A framework client performs one request. It reports success, definite non-commit, or `commit_unknown`; it does not
  retry for the caller.
- Missing relationship targets remain valid eventual graph state. Mutating a missing entity returns not-found and does
  not create a stub.
- Exact entity reads return the entity and its same-entry KV revision.
- Projection contracts validate local birth, reconcile, and append intent. They do not reserve predicates or identify a
  semantic owner.

## Source migration mapping

- Replace `pkg/ownership` modes and registries with local `pkg/projection.Contract` values.
- Replace `ModeReplaceOwned` with `projection.ModeReconcile`; use `projection.ModeAppend` only for evidence that is
  genuinely append-only.
- Replace `OwnedReplacer.ReplaceOwned(ReplaceOwnedMutation)` with
  `PredicateReconciler.Reconcile(ReconcileMutation)`.
- Remove owner IDs, owner tokens, registration/bind calls, heartbeats, presence checks, and revival/quiesce wiring.
- Replace raw legacy mutation subjects with `projection.MutationClient` or an operation-specific typed adapter resolved
  from the declared mutation port. Do not copy subject tables into downstream packages.
- Replace rule action `replace_owned` with `reconcile_predicates` plus its local projection contract/group selector.
- Replace value-only reads of `graph.ingest.query.entity` with `graph.ExactEntityReader` or the admitted exact GraphQL
  result when a following mutation needs a revision.
- Replace `OpenCatalogBucket` with `OpenCatalogReader` for readers or `EnsureCatalogBucket` for declared bucket owners.
- Treat `entity_not_found`, `revision_mismatch`, and `commit_unknown` as different outcomes. A component may choose a
  later retry policy, but must not reinterpret ambiguity as success.
- If consuming `write_todos`, use exported `TodoReader`/`TodoState`. The graph representation is one rule-opaque
  `agent.todo.record` JSON literal per item; the five field predicates no longer exist.

Every shipped flow that contains graph-ingest must declare exactly one required typed mutation provider, and every
component that mutates the graph must declare a compatible requester output. Static flow validation should fail before
component allocation when that contract is missing or ambiguous.

## Adopter seam

- A downstream Go or configuration author must know the new typed interfaces, four operations, exact-read revision,
  classified outcomes, and local projection contract named above. They should not need subjects, KV APIs, ownership
  tokens, or todo storage details.
- An adopter who upgrades without migrating gets explicit compile, schema, or flow-validation failures. No fallback
  silently accepts the old contract.
- A product that remains pinned to its earlier, internally consistent SemStreams stack can keep scheduling its
  migration, but mixed-version operation is unsupported and it has not demonstrated parity with the breaking tag. That
  holdout neither blocks nor changes the tag.
- The tagged API, generated schemas, ADR-091, and this mapping are the discovery surfaces. A missing capability found
  during downstream migration is reported separately; ordinary breaking edits are not framework blockers.

## Bounded holdout census

The following read-only census was taken on 2026-08-05. Counts are source/config text-hit files after excluding docs,
OpenSpec, tickets, evidence, vendor, and `*_test.go`. Comments and generated artifacts can still produce false
positives. They are migration sizing signals, not proven compile errors. No downstream file was edited.

Count keys: `O` ownership/projection symbols, `W` legacy mutation wire, `R` value-only exact-read subject, `A` legacy
rule action, `T` retired todo predicates.

- `semdev`: beta.159, branch `m0-walking-skeleton-spine`, head `e5362dffa309`, clean; O=33 W=4 R=3 A=0 T=0.
- `semmachina`: beta.159, branch `main`, head `16917294fddc`, clean; O=4 W=11 R=2 A=3 T=0.
- `semsource`: beta.158, branch `chore/dockerignore-exclude-ui`, head `5a0f0307f484`, clean; O=5 W=2 R=1 A=0
  T=0.
- `semboids`: beta.158, branch `chore/bump-semstreams-beta158`, head `12723db3317d`, three local changes; O=0 W=4
  R=0 A=0 T=0.
- `semdragon`: beta.135, branch `codex/canonical-graph-gate-closeout`, head `07f4de9b6588`, clean; O=3 W=0 R=1
  A=0 T=0.
- `semstreams-ui`: no Go module, branch `main`, head `3814b3d59dab`, clean; O=1 W=0 R=0 A=0 T=0. Regenerate its
  API types from the tagged framework.
- `semteams`: beta.115, branch `codex/archive-sdd-openspec`, head `089a5eb5d8ab`, thirteen local changes; O=2 W=7
  R=0 A=11 T=0.
- `semconnect`: beta.159, branch `codex/qualify-semstreams-beta159`, head `7c7c114f6344`, clean; O=2 W=12 R=1
  A=0 T=0.
- `semlink`: beta.141, branch `main`, head `a223955eb9a3`, six local changes; O=3 W=1 R=2 A=0 T=0.
- `semops`: beta.145, branch `codex/cop-component-telemetry`, head `602c619a9f1c`, five local changes; O=68 W=23
  R=0 A=0 T=0.

The broadest migrations are semops, semdev, semteams, semconnect, and semmachina. Semops and semdev have concrete
ownership packages/writers to delete rather than translate. Semteams has shipped rule JSON using `replace_owned`.
Semconnect and semmachina have substantial raw wire adapters. No holdout contained a production hit for the retired todo
field predicates.

## Per-repository proof after the tag

For each holdout, record the migration commit and run:

1. Bump the SemStreams dependency or regenerate the UI client from the exact breaking tag.
2. Compile to expose deleted Go symbols and generated API drift.
3. Replace every live hit from the mapping above; delete adapters made unnecessary by the framework client.
4. Validate every flow's typed mutation provider/requester pairing.
5. Run the repository's unit, race, integration, and relevant E2E suites.
6. Prove feature parity for its use case: birth, exact read, reconcile, append partial result, delete, missing-reference
   observation, restart, and eventual derived-view convergence as applicable.

Do not require all ten migrations before tagging SemStreams. The tag is the stable migration target. Downstream work
starts from that target and reports genuine missing capability separately from ordinary breaking-API edits.

## State and operator procedure

NATS may run single-node or clustered. SemStreams does not implement backup, checkpoint, restore, or forensic recovery.
Operators own normal NATS backup/checkpoint procedures for their topology; this note adds no framework recovery gate,
tool, or compatibility workflow.

Before adoption, take whatever deployment backup/checkpoint the operator's normal policy requires. Every downstream
product adopting this stable release starts on newly provisioned NATS storage. Discovery of retained deployed state
stops that adoption and requires a separate owner-reviewed migration or recovery design; it does not authorize copied
wildcard deletion commands. Typed graph poison remains governed by the scoped recovery runbook. Missing references
after fresh startup are observable eventual state, not a reason to fail startup.
