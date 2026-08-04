# Graph state read/write foundation program

**Status:** Active program state.

**Program baseline:** `fe725f82` (`main`, 2026-08-04), after #896 merged the GS-00 archive.

**Binding decision:**
[ADR-090](../adr/090-authoritative-current-state-and-materialized-views.md).

**Frozen evidence:**
[inventory](graph-state-read-write-inventory.md) and
[accepted decision record](graph-state-read-write-decision.md).

This is the only living program record for ADR-090 implementation. ADR-090 is a
decision, the inventory is frozen evidence, and the accepted proposal is a frozen
ruling record. Update this file—not those records—when program state changes.
The pre-v1 core-hardening program is suspended and frozen; its historical Next
Action, WIP, active, and priority text is non-executable until this program
explicitly records its release.

OpenSpec physically reports three in-progress change directories. The GS-01
[`establish-authority-read-and-recovery`][gs01-design-baton] directory is an
unapproved, design-only baton with reviewed inventory and no accepted design. `semantic-tier-split` and
`discovery-under-stream-shapes` are suspended and frozen in their own
proposal/task records. Only the single Next Action in this program is executable;
none of these changes authorizes runtime implementation or spec promotion.

## Identity and priorities

SemStreams is an **offline-first, edge-capable, tiered semantic graph framework**.
It is not an event log product, a general CQRS runtime, or a collection of storage
adapters exposed as an architecture.

Priorities are ordered:

1. predictable graph results;
2. pragmatic operation on an edge node with local NATS;
3. easy adoption through one default path;
4. easy comprehension by a developer who has not read the implementation;
5. optional statistical and semantic enhancement without weakening lower tiers;
6. performance after the contract is understandable and correct.

An optimization, abstraction, compatibility layer, or downstream usage pattern
does not outrank these priorities.

## Canonical authority, write, and read model

### Authority

`ENTITY_STATES` is canonical current shared semantic state with history 1. It is
not a retained event ledger. Its supported recovery unit is current state plus
the separately declared content and ingest-guard state needed by the deployment.

### Writes

There are two supported adopter seams:

1. **Asynchronous fact ingestion:** an event implements a registered `Graphable`
   envelope and enters the configured entity stream. Graph-ingest owns authority
   acceptance. The lane declares delivery, redelivery, idempotency, rejection,
   and observability; it promises no synchronous authority commit receipt.
2. **Command-like semantic mutation:** the adopter selects a supported typed
   semantic intent. The typed client uses internal transport, and graph-ingest
   returns committed, not committed, or unknown commit knowledge. Projection
   visibility remains a separate view concern.

GS-02 records a complete lane disposition for fact arrival, create,
replace/transition, append, retract, delete, and generic create/update. Each lane
is admitted through one of these semantic seams with an owner, retained as
internal-only transport, or retired. Raw mutation subjects and wire structs are
not adopter APIs.

The GS-00 seed baseline is:

| Current lane | GS-02 candidate disposition |
|---|---|
| Configured `Graphable` stream, default `entity.>` | Admit async fact seam |
| `create_with_triples` | Typed `Create`; raw transport internal |
| `update_with_triples`, revision 0 | Typed `ReplaceOwned` |
| `update_with_triples`, revision >0 | Typed `Transition` |
| `triple.add` | Typed `AppendEvidence` or consolidate; raw transport internal |
| `add_batch` | Internal batching only |
| `triple.remove` | Retire public predicate-wide operation |
| Exact retraction | `ReplaceOwned` empty or new typed `RetractEvidence` |
| `entity.delete` | Typed `Delete`/`Reclaim` with explicit authority |
| Bare `entity.create` and `entity.update` | Retire |

GS-02 entry requires a fresh commit-pinned ingress census covering every handler,
`Graphable` input, exported/in-process method, rule/gateway/inference/lifecycle/
agentic writer, raw publisher, generic authority-KV action, and configured or
generated write port. Every row records intent, sync/async behavior,
create/must-exist semantics, envelope, authorization/ownership, atomicity,
conflict/CAS, retry/redelivery/idempotency, visible outcome, typed owner,
unknown recovery policy, internal transport, and admitted/internal/retire
disposition. New rows may be added; none may be silently dropped. Retraction and
entity death require an owner-approved ADR-shaped ruling.

GS-02 also dispositions the misleading `pkg/projection` name. Authoritative
semantic write intents move to a graph-write/semantic-mutation home; materialized
view terminology remains on the read side. The old mixed name and imports are
removed under the pre-v1 clean break.

Every admitted command intent states what the caller does on `unknown`: issue
desired-state reconciliation, reread and revalidate CAS, rely on append dedup, or
stop and escalate. It also states what happens if the adopter does nothing. There
is no generic command retry rule. The asynchronous fact lane separately uses
stream acknowledgment and redelivery semantics.

GS-02 exits only when both seams have conformance; typed commands cover `Create`,
`ReplaceOwned`, `Transition`, `AppendEvidence`, `RetractEvidence`, and
`Delete`/`Reclaim`; command outcomes are honest; and the fact seam proves envelope
validation, Ack/Nak/Term, keyed ordering, redelivery guard, and observability with
no synchronous receipt. No adopter-facing `triple.remove`, bare create/update, or
raw publisher remains outside approved ingest adapters/tests. Documentation and
examples use semantic methods. Concurrency, no-op, conflict, lost reply, later
competing commit, retraction identity, and idempotent delete tests are green; the
per-intent unknown recovery policies and do-nothing behavior are documented; the
complexity delta is accepted; and GS-02 archives before GS-03.
The exit evidence includes the package/import migration and proves no public
authority writer remains under `projection` terminology.

Callers do not infer ownership from timestamps, content, provenance, request-ID
echo, or a materialized-view hit.

### Reads

Callers use one two-step model:

1. **Choose an admitted front door.** Remote applications use only admitted HTTP
   operations. Embedded services use an operation-specific typed adapter when one
   exists. The aggregate `graph/query.Client` is provisional mixed direct-KV/RPC,
   not an adopter default. There is no canonical general reactive semantic
   subscription today and none is scheduled without measured adopter evidence.
   Controlled framework internals may use a declared raw owner/dependency seam.
   Projection owners use declared
   dependencies; operators may use raw KV diagnostics. There is no MCP graph-read
   front door.
2. **Read the operation's answer-source contract.** Today the entity query
   returns a raw value without revision, and materialized-view behavior is
   operation-specific. GS-01 targets authority value plus revision. GS-03
   through GS-10 make lifecycle, health, coverage, staleness, cycle, and
   capability status normative one owner at a time.

The GS target makes exact-entity authority available through whichever admitted
front door implements that operation; adopters do not choose an implementation
bucket to get stronger truth. Protocol does not impose a universal consistency
promise. The declared answer source determines what the result proves.

Before GS-12 there is no canonical general remote or embedded graph front door.
A controlled remote caller may consciously use an enumerated admitted facade
operation. Raw KV and subjects are not a fallback when no typed operation exists.

## Binding invariants

| Invariant | Binding source |
|---|---|
| One current semantic authority and one writer | [ADR-090] and [state contract] |
| Typed intent is distinct from transport | [ADR-055](../adr/055-graph-write-intent-taxonomy.md) |
| Product predicate ownership is exclusive | [ADR-056](../adr/056-authoritative-semantic-state.md) |
| Live graph retention is not lifecycle deletion | [ADR-068](../adr/068-graph-retention-deletion-lifecycle.md) |
| Local shared views are not a durable-view runtime | [ADR-081](../adr/081-graph-view-subscription.md) |
| Readiness belongs to each producer | [ADR-088](../adr/088-readiness-is-per-producer-aggregation-is-the-consumers.md) |
| Durable views survive only with a semantic consumer | [ADR-090] |

[ADR-090]: ../adr/090-authoritative-current-state-and-materialized-views.md
[state contract]: ../../openspec/specs/graph-state-contract/spec.md

## Evidence registry

| Evidence | Status | Program use |
|---|---|---|
| [Inventory evidence] | `c6ef4541`; frozen and reviewed | Defect classes, callers, and owners |
| [Inventory review] | Reported 64/65 citations confirmed | Evidence confidence, not task order |
| [Decision evidence] | Owner-approved 2026-08-03; frozen | ADR-090 rulings |
| [Ruling conformance] | GS-00 exact file:line map | Architecture review evidence only |
| [#894 evidence] | Merged and archived | Removed the provenance view |
| [#895 evidence] | Merged and archived | Removed structural persistence |
| Current catalog | 14 `ClassDerived` descriptors | Complexity baseline |
| Current concepts | 31 files at `1c17958a` | Consolidation input |
| Issues and pre-v1 baton | Point-in-time defects | Class frequency and falsification |
| Holdout observations | Point-in-time adoption notes | Coordinated migration planning |
| Runtime `/mcp` placeholder | Config, stub handler, OpenAPI advertise it | GS-12 remove or implement |
| Active OpenSpec control | GS-01 design-only baton plus two frozen changes | Enforce WIP 1; block runtime/spec work |

The adversarial review found one line-range drift and no unsupported
load-bearing claim. The frozen inventory preserves the full pre-refactor detail.

[inventory evidence]: graph-state-read-write-inventory.md
[inventory review]: graph-state-read-write-inventory-review.md
[decision evidence]: graph-state-read-write-decision.md
[ruling conformance]: graph-state-read-write-ruling-conformance.md
[#894 evidence]: ../../openspec/changes/archive/2026-08-04-retire-context-index/proposal.md
[#895 evidence]: ../../openspec/changes/archive/2026-08-04-retire-structural-index/proposal.md

## Defect-class map

Issue numbers are examples of recurring classes. Their queue order, labels, or
assignees do not schedule this program.

| Defect class | Evidence examples | Foundation contract |
|---|---|---|
| Ambiguous command outcome | #861, #869–#871, #874 | Mutation outcome and observation |
| Authority revision meaning | #681, #851, #892 | Typed authoritative read and per-entity revision |
| Dependency change and failed redrive | #875, #881, #887, PR #893 | Lifecycle declaration and repair obligation |
| Projection readiness and convergence | #795, #820, #868 | Owner-specific status unit and read behavior |
| Query and wire drift | #784–#786, #819, #822, #883–#886 | Surface inventory and typed clients |
| Partial writes and limits | #837, #839, #855, #857 | Ownership and failure behavior |
| Stale derived state | #672 and inventory findings | Removal, dependency-redrive, and rebuild contracts |
| E2E proof gaps | #766, #769, #811, #830, #844, #888 | Reproducible capability evidence |

No package-level issue is implemented merely because it appears here. A fix must
belong to the current GS increment and satisfy its stop/go gate.

## GS-01 process correction

GS-01 has a fresh inventory independently reviewed with `INVENTORY PASS`. It has no accepted design, target state, or
runtime mechanism. The owner revoked any apparent prior GS-01 acceptance; acceptance was not granted. The GS-00
candidates and the increment gate below constrain the problem boundary, but they do not choose GS-01 representation
or implementation.

The failed design attempt is retained as correction evidence, not as input to
implement:

- proposed `GRAPH_INGEST_ACTIVE` duplicated territory already occupied by
  `GRAPH_STATUS` and graph-ingest behavior; treating that semantic class as empty
  was a failed premise;
- the proposed NATS CLI requirement is withdrawn; inventory must enumerate the
  real operator and recovery seams before design chooses any interface; and
- prompts, briefings, and prior proposals are hypotheses to falsify, not an
  existing-surface inventory.

The unapproved, design-only
[`establish-authority-read-and-recovery`][gs01-design-baton] OpenSpec change is
the durable baton. It records the problem, accepted process gates, reviewed
inventory, and `INVENTORY PASS`. Design, independent pre-owner design review, and
explicit owner acceptance remain. No runtime implementation, runtime-capable spec
delta, or spec promotion may begin before that final acceptance.

## GS-00 acceptance candidates

The GS-00 OpenSpec change is
[`establish-graph-state-foundation`][gs00-archive].
It records non-normative acceptance candidates and program governance only. It
adds no runtime spec delta or surface. After acceptance, GS-00 archives. Each
bounded GS-01+ change later adds, implements, validates, and archives the relevant
capability delta.

### Typed authoritative read

A successful entity read returns the canonical value and its per-entity KV
revision together. Not-found, poison, unavailable, and canceled are classified
outcomes. No global revision order or historical reconstruction is implied.

### Typed mutation outcome

Mutation results have two independent axes.

Commit knowledge resolves to one of:

- **committed:** this request committed and carries its authoritative revision;
- **not committed:** no authority write is attributed to this request, with a
  classified reason such as no-op, conflict, validation, or not found;
- **unknown:** transport ended before attribution was known and remains unknown.

An optional authority observation reports found/not-found/poison/unavailable or
canceled, its observed revision when found, and whether current state satisfies,
diverges from, or cannot determine the requested intent. Observation never
attributes a prior commit. A request ID echoed in the causal reply is transport
correlation only; GS-00 adds no durable receipt or query-by-request-ID contract.

#869 remains a separate post-foundation candidate. It requires its own atomicity,
retention, recovery, and adopter-seam design and is not silently solved by GS-02.

Projection coverage is never part of the mutation outcome.

### Reactive subscription boundary

GS-00 does not admit or schedule a general typed reactive subscription. Raw
watches remain internal to an authority or projection owner, or available as
operator diagnostics; they are not an adopter fallback. `pkg/graphview` has no
production graph-state adopter and is precedent evidence, not justification.

An owner-specific typed observation operation may be proposed only after a fresh
named-adopter census, a before/after public-surface and adopter-decision table, and
owner acceptance show it reduces knowledge and total surface. A future proposal
must own its representation and conformance; GS-03 does not pre-authorize it.

### Role and lifecycle declaration

The GS-00 role/lifecycle acceptance candidate asks every current role instance to
declare:

- role and semantic consumer;
- authoritative and derived dependencies;
- trigger and execution-time reread behavior;
- desired-state and key ownership boundaries;
- update, removal, and dependency-change behavior;
- transient, poison, permanently excluded, and partial-write outcomes;
- repair, clean rebuild, and destructive scope;
- active-instance model;
- readiness/capability unit and read behavior during bootstrap, lag, degradation,
  reset, and absence;
- offline behavior and lower-tier fallback; and
- whether any step can cause an authoritative or external side effect.

This becomes normative only when GS-03 lands the representation and conformance
harness. Declarations share questions; they do not force unlike owners into one
runtime.

Every surviving durable owner defaults to one active runtime instance until it
proves active/active convergence. Queue groups are reserved for query-only
responders whose request/reply contract proves that scaling model safe.

## Three-owner proof matrix

| Concern | graph-index | graph-embedding | graph-clustering |
|---|---|---|---|
| Role | Required query views | Optional enrichment/dedup | Partition; optional summary/anomaly |
| Inputs | `ENTITY_STATES` | Authority, content, model capability | Authority, topology views, optional embeddings |
| Trigger | KV watch plus repair | Two-hop KV work state | Periodic current-state read |
| Currency unit | Authority KV revision | Source revision plus work state | Detection cycle and wall-clock staleness |
| Removal | Entity-owned membership reconciliation | Delete/dedup lifecycle | New partition plus prune |
| Failure | Failed set, retry, repair | Pending/failed/stranded taxonomy | Cycle failure or stale superset |
| Rebuild | Replay/reconcile authority | Recompute eligible content | Recompute partition from current dependencies |
| Lower tier | No topology fallback | BM25 or capability unavailable | Explicit edges and statistical summaries |

All three need declarations, bounded failure evidence, reset semantics, and
operator proof. Their triggers, ordering units, removal algorithms, and fallback
semantics differ. GS-00 therefore records acceptance vocabulary only.

A shared durable-view runtime may be proposed later only when all three owners:

1. independently require the same mechanism, not merely the same noun;
2. can use it without owner-specific escape hatches;
3. have repeated defects or duplicate conformance code proving the cost; and
4. provide a before/after adopter-seam table for one outside component author,
   counting required public types, subjects/buckets, config knobs, lifecycle
   choices, failure/recovery choices, and documents to read; and
5. show fewer total adopter decisions and production lines with no owner-specific
   escape hatches.

## Offline and tier contract

- Tier 0 remains useful with local NATS and explicit graph relationships.
- Tier 1 adds local statistical capabilities without requiring an external model.
- Tier 2 adds external semantic services but cannot weaken Tier 0 or Tier 1 truth.
- Optional capability absence is explicit and uses a declared lower-tier result;
  it is not reported as an empty authoritative answer.
- Restart from preserved local current state must not require a cloud control
  plane. Authority disaster recovery remains a separate snapshot/restore design.

## Frozen holdouts and coordinated migration

The following ten downstream projects are frozen holdouts:

`semdev`, `semmachina`, `semsource`, `semboids`, `semdragon`, `semstreams-ui`,
`semteams`, `semconnect`, `semlink`, and `semops`.

They are not per-slice gates and do not vote on SemStreams architecture. Usage in
a holdout never preserves a SemStreams anti-pattern. No fresh downstream source
census is required for each foundation increment.

Holdout migration cannot begin until the named Foundation tag-candidate gate
records `PASS` with its evidence, exact tag/version, and owner-approved migration
window. Then migrate all ten projects in that window, run their relevant contract
and E2E gates, and take stock of missing framework seams. New evidence can change
the next SemStreams program; it does not retroactively make a holdout an
architecture authority.

## Complexity budget and ratchet

| Dimension | Baseline | Ratchet |
|---|---|---|
| Durable derived buckets | 14 | New bucket needs a consumer and lifecycle declaration |
| Concept documentation | 31 files | Do not increase; normally consolidate or delete |
| Public read paths | Overlapping fronts | One documented default per adopter class |
| Public write paths | Typed and raw subjects | Move raw subjects inward; add no transport spelling |
| Shared view machinery | Independent owners | No runtime abstraction before the three-owner proof passes |
| Active implementation | Historical parallel patching | WIP is exactly one GS increment |

Every increment reports public types, subjects, buckets, config knobs, concept
documents, and production lines added and removed. Any increase must state the
adopter who pays for it, what happens by default, where they learn it, and why the
framework could not absorb it internally.

The 31 concept files receive a bounded inventory and consolidation plan before the
foundation tag. GS-00 does not rewrite them wholesale. Pragmatic use, one obvious
default, and a coherent mental model outrank preserving documentation structure.

### Derived catalog disposition

The deletion test runs first: a durable capability without a present semantic
consumer is deleted before convergence machinery is added.

All 14 `ClassDerived` descriptors require an accepted declaration, inherited
owner declaration, or approved deletion before the tag-candidate gate can pass.

| Descriptor | Disposition | Owning capability | Increment |
|---|---|---|---|
| `ENTITY_SUFFIX_INDEX` | Keep | Suffix resolution; graph-ingest owner | GS-05 |
| `OUTGOING_INDEX` | Keep | Graph-index core | GS-04 |
| `INCOMING_INDEX` | Keep | Graph-index core | GS-04 |
| `ALIAS_INDEX` | Keep | Graph-index core | GS-04 |
| `PREDICATE_INDEX` | Keep | Graph-index core | GS-04 |
| `NAME_INDEX` | Keep | Graph-index core | GS-04 |
| `SPATIAL_INDEX` | Keep while admitted | Spatial query | GS-06 |
| `TEMPORAL_INDEX` | Keep | Temporal query | GS-07 |
| `TEMPORAL_INDEX_REVERSE` | Inherit | Temporal query reverse bookkeeping | GS-07 |
| `EMBEDDING_INDEX` | Keep | Semantic embedding | GS-08 |
| `EMBEDDING_DEDUP` | Inherit | Semantic embedding deduplication | GS-08 |
| `COMMUNITY_INDEX` | Keep | Community partition | GS-09 |
| `COMMUNITY_SUMMARIES` | Optional facet | Graph-clustering summaries | GS-09 |
| `ANOMALY_INDEX` | Conditional | Declare owner/effects or delete | GS-10 |

### GS-11 deterministic E2E proof harness

GS-11 owns the deterministic proof harness needed by the final release gate. Each
relevant tier names a fixed input/seed and spec-owned deterministic population
and assertion invariants. The slice must eliminate the observed statistical-tier
drift in which repeated clean runs changed `entity_count` from 125 to 129 and
moved every derived count with the population.

GS-11 also dispositions relevant suspended test work. It either absorbs the
requirements from `semantic-tier-split` into its bounded change or records an
owner-accepted reason to release that work to a named later program. Suspended
work never becomes a second executable increment. This slice establishes the
harness and proves the known drift fixed; the three consecutive clean isolated
runs remain a final tag-candidate gate, not every slice's feedback loop.

### GS-12 read-front gate

GS-12 dispositions every remote facade method, embedded client method and
subject, and the `/mcp` placeholder. The required graph gateway becomes a
conformant GraphQL parser/schema executor with truthful introspection,
operation/variable validation, selection projection, and GraphQL error behavior.
The aggregate `graph/query.Client` is retired or internalized to declared owner
seams. Only measured operation-specific typed adapters survive. GS-12 archives
before GS-13 begins.

### GS-13 concept gate

GS-13 begins with the measured concept baseline: 31 files, 9,705 lines, and
50,804 words. The pinned commit-tree baseline was measured with `git ls-tree`
plus `git show` and `wc`; working-tree counts are not the baseline. It produces
a 31-row disposition manifest. Each row records purpose and audience;
keep/merge/move/split/delete; destination and truth owner;
default/advanced/reference/operations/history class; stale conflicts; and inbound
links.

The default mental model teaches, in order:

1. SemStreams is a governed graph substrate, not a product event bus.
2. `ENTITY_STATES` is ingest-owned current shared authority with history 1, not a
   ledger.
3. Graphable fact ingestion and typed command mutation are separate write seams.
4. Derived indexes are named materialized views with owners, sources, lifecycle,
   readiness, and failure behavior.
5. Read front door is separate from answer source; watches observe facts, streams
   and request/reply carry work, and raw buckets/subjects are owner/diagnostic.

GS-13 exits with rows 31, undisposed 0, an explicit before/after file/line/word
report, at most five default-path documents including `docs/README.md`, zero
contradictory canonical claims, zero normative contracts living only in concept
prose, and zero broken in-repo links. It reports additions, deletions, merges, and
moves; records advanced and historical destinations; and keeps the ordinary
concept-file ratchet at no more than 31. Reduction alone is not success.

## Increment-specific accepted-ruling gates

- **GS-01 authority recovery:** deliver and verify the coordinated
  snapshot/restore runbook for `ENTITY_STATES`, referenced content, and
  `GRAPH_INGEST_APPLIED_SEQ` reset-or-restore. Fact-stream replay is not accepted
  as disaster recovery. Authority startup validation remains fail-closed for
  reads while preserving writer availability and states its history-1 dependency.
  Graph-ingest also proves single-active enforcement or accepted active/active
  safety; GS-02 re-verifies that proof under both mutation seams.
- **GS-02 write identity:** complete the two semantic write seams and remove the
  misleading `pkg/projection` authority-writer namespace as specified above.
- **GS-03 role declaration and deletion test:** land role declaration
  representation/conformance plus a commit-pinned census and disposition of
  reactive consumers and serving caches. This adds no general subscription or
  shared runtime. Disposition `COMPONENT_STATUS`, which has writers but no
  production semantic reader: delete it or retain it only with a named consumer
  and status contract. Every census row names an exact later owner increment,
  deletes/internalizes the instance, or proves its conformance in GS-03; no row
  exits with an implementation home of “dispositioned.”
- **GS-04 through GS-07 required/internal roles:** prove required-query-view,
  internal-accelerator, and reverse-bookkeeping obligations one owner at a time.
- **GS-08/GS-09 optional/serving roles:** prove optional-enrichment,
  inherited embedding-dedup, periodic-partition, summary/serving-cache, and
  explicit unavailable/lower-tier obligations.
- **GS-04 through GS-10 owner safety:** every remaining durable owner proves enforced
  single-active deployment or owner-specific active/active safety before its
  slice exits.
- **GS-04 through GS-10 rebuild:** every surviving view family implements a
  capability-scoped operator rebuild with completion/readiness evidence and a
  semantic comparison proving expected keys plus stale-key absence.
- **GS-10 inference boundary:** remove graph-gateway direct writes to
  `ANOMALY_INDEX`. Retire the `graph.events.relationship.create` applier unless an
  explicit producer/consumer contract and authoritative-write bridge are
  accepted. Rebuild remains effect-free. Any surviving inference application
  declares its live authorization condition, durable correlation/idempotency,
  loop bound, typed authoritative mutation outcome and revision, and failure
  independently from anomaly detection.

## Ordered increments

WIP is one. An increment starts only after the prior increment's gate is recorded
here.

| Increment | Outcome | State |
|---|---|---|
| Pre-GS #894 | Retire `CONTEXT_INDEX`; keep provenance in authority | Complete and archived |
| Pre-GS #895 | Retire `STRUCTURAL_INDEX`; keep in-memory anomaly inputs | Complete and archived |
| GS-00 | Bind design candidates, program control, and canonical guidance | Complete, archived, and merged (#896) |
| GS-01 | Authority read/recovery and graph-ingest safety | Inventory reviewed; design next; no accepted design |
| GS-02 | Two write seams, lane matrix, honest mutation outcome/observation | Not started |
| GS-03 | Role declaration representation, census, and conformance | Not started |
| GS-04 | Graph-index core declarations and conformance | Not started |
| GS-05 | Suffix-resolution/graph-ingest declaration and conformance | Not started |
| GS-06 | Spatial declaration and conformance | Not started |
| GS-07 | Temporal declaration and conformance | Not started |
| GS-08 | Semantic-embedding declaration and conformance | Not started |
| GS-09 | Community/clustering declaration and conformance | Not started |
| GS-10 | Anomaly disposition and effect-free rebuild boundary | Not started |
| GS-11 | Deterministic E2E proof harness and statistical drift elimination | Not started |
| GS-12 | Conformant GraphQL and exhaustive read-front/client disposition | Not started |
| GS-13 | 31-file concept disposition and consolidation | Not started |
| GS-14 | Record Foundation tag-candidate gate `PASS`; prepare exact tag | Not started |
| Migration | After recorded `PASS`, move all ten holdouts and take stock | Blocked on GS-14 |

Issue work can be absorbed, split, or closed by an increment. Issue numbering does
not reorder this table.

## Stop/go gates

### GS-00 foundation-entry gate

Runtime work may begin only when:

- ADR-090, the frozen evidence, and this canonical program agree;
- the GS-00 OpenSpec change passes strict validation and architecture review;
- authoritative read, mutation outcome, and lifecycle declaration vocabulary is
  accepted;
- architecture review accepts the two-axis mutation result and confirms GS-00
  adds no durable receipt, request lookup, or idempotency primitive;
- the three-owner matrix demonstrates why no shared runtime is justified yet;
- canonical query/KV guidance stops promising MCP, universal eventual
  consistency, or authority history that does not exist; and
- exactly one Next Action is recorded below.

### Per-increment go gate

Each increment first passes the architect contract's distinct inventory review
and pre-owner design review, followed by explicit owner acceptance. Only then may
it create a runtime-capable spec delta or begin implementation. Proceed only when
the current increment has a reviewed delta, behavior-focused tests, bounded proof
relevant to that increment, documentation truth, and an updated complexity
statement. GS-11 owns the deterministic E2E harness; the three-run release proof
is not part of every slice's iterative loop. The active change remains unarchived
while its target behavior is not implemented.

### Foundation tag-candidate gate

GS-14 records `PASS` only when all objective evidence is present:

- GS-01 through GS-13 are accepted and implemented, with target requirements
  promoted or archived as current truth where appropriate;
- authority-read, mutation-outcome, and role/lifecycle conformance
  suites are green;
- every accepted role family and current instance has a recorded disposition and
  applicable conformance evidence, including authority startup validation,
  reactive consumers, serving caches, and effectful inference; every GS-03 census
  row has an implemented/conformant owner home or is deleted/internal-only;
- the authority snapshot/restore runbook proves coordinated restore of authority,
  referenced content, and ingest-guard state;
- `COMPONENT_STATUS` has a recorded delete-or-retain disposition with any retained
  semantic consumer and status contract;
- every durable owner has enforced single-active deployment or accepted
  owner-specific active/active proof;
- every surviving view family has a capability-scoped operator rebuild with
  completion/readiness evidence and stale-key removal proof;
- graph-gateway no longer writes `ANOMALY_INDEX`, and the relationship-create
  event applier is retired unless its explicit bridge contract was accepted;
- any surviving inference application proves live authorization, durable
  correlation/idempotency, a loop bound, typed authoritative mutation outcome
  with revision evidence, and failure distinct from anomaly detection;
- each relevant E2E tier names a fixed input/seed and spec-owned deterministic
  population and assertion invariants;
- at the tag candidate, three consecutive clean isolated runs on the same commit
  have identical seeded input population and assertion counts, with every
  deterministic invariant green;
- external-model outputs may use bounded semantic assertions, but harness
  population and case counts still match across all three runs;
- the final complexity report is accepted and the 31-file concept consolidation
  is complete;
- the GS-13 concept manifest has 31 rows, zero undisposed rows, at most five
  default-path documents, zero canonical contradictions, zero concept-only
  normative contracts, and zero broken in-repo links;
- every remote method and embedded method/subject is dispositioned as admitted,
  internalized, rewritten behind a named typed adapter, or retired;
- no provisional remote facade or aggregate embedded client is presented as a
  canonical general front door;
- the required graph gateway passes GraphQL parser, schema, introspection,
  selection, variable, and error conformance; the aggregate embedded client is
  retired or internalized and only measured named adapters remain;
- the registered `/mcp` config, handler, and OpenAPI advertisement are removed or
  replaced by a specified implementation so capability discovery is truthful;
- canonical documentation and decision skills match the verified runtime;
- effect-free rebuild is separated from authorized inference application; and
- the owner approves the exact tag/version and coordinated migration window.

The gate also confirms every current mutation lane has an accepted semantic seam,
internal-only transport boundary, or retirement, and that adopter guidance/config
does not expose raw mutation subjects or wire structs.

The program log records `PASS` with links to every evidence item, the exact
tag/version, and the migration window. Without that record, no foundation tag
candidate is released and no holdout migration begins.

### Stop conditions

Stop and return to the owner when:

- a second GS increment becomes active;
- a package issue asks for behavior outside the active contract;
- a new abstraction appears before the three-owner proof;
- a caller must predict framework-owned state before acting;
- a lower tier becomes dependent on a higher tier or external service;
- a test cannot distinguish absence, not-ready, poison, and an empty answer;
- evidence required by the current increment or tag gate has population or
  assertion drift, lacks its declared fixed input/seed, or fails a deterministic
  invariant; at the tag gate, external-model variability does not excuse harness
  count drift across the three required clean isolated runs;
- public surface or conceptual surface grows without passing the ratchet; or
- a tag candidate or holdout migration is proposed without a recorded Foundation
  tag-candidate gate `PASS`.

## Compaction and resume protocol

After a context compaction or session handoff:

1. read ADR-090;
2. read this file completely;
3. read the active GS OpenSpec proposal, design, and tasks;
4. verify branch, worktree, `main`, open GS work, and test evidence;
5. perform only the single Next Action;
6. update task truth, complexity deltas, evidence, and the append-only log; and
7. replace the Next Action with exactly one successor or a named blocker.

Do not reconstruct program order from the issue queue, old baton, chat history,
or an inventory recommendation.

## Append-only program log

- **2026-08-03:** Owner accepted ADR-090 and authorized breaking pre-v1
  simplification without compatibility layers.
- **2026-08-04:** #894 merged; `CONTEXT_INDEX` retirement archived.
- **2026-08-04:** #895 merged; `STRUCTURAL_INDEX` retirement archived.
- **2026-08-04:** GS-00 opened on `1c17958a`; ten holdouts frozen until one
  coordinated SemStreams tag candidate.
- **2026-08-04:** GS-00 documentation and target deltas passed strict OpenSpec,
  diff, and Markdown line-length validation; architecture review remains open.
- **2026-08-04:** The pre-v1 core-hardening baton was suspended and frozen so
  this program retains the only executable Next Action and WIP authority.
- **2026-08-04 correction:** Architecture review ruled that GS-00 carries no
  runtime target deltas. The earlier validation entry describes the superseded
  draft; accepted candidates live non-normatively in design until bounded GS
  changes implement and promote them.
- **2026-08-04:** `semantic-tier-split` and
  `discovery-under-stream-shapes` were suspended and frozen in place. OpenSpec
  still reports their historical directories, but only GS-00 is executable.
- **2026-08-04 owner correction:** The graph gateway is required and GS-12 must
  make it conformant GraphQL. The frozen ruling's general embedded
  `graph.query.*` client promise is superseded: the aggregate client is retired
  or internalized, and only measured operation-specific adapters survive.
- **2026-08-04:** GS-04 through GS-10 were split to one durable owner per
  increment; GS-11 owns E2E proof, GS-12 reads, GS-13 concepts, and GS-14 release.
- **2026-08-04:** SemStreams architecture review APPROVED GS-00 with no findings.
  `git diff --check`, Markdown 120-column checks, and strict OpenSpec validation
  for GS-00 and both frozen changes are green. GS-00 is ready to archive.
- **2026-08-04:** `openspec archive establish-graph-state-foundation --skip-specs`
  succeeded and moved GS-00 to
  `openspec/changes/archive/2026-08-04-establish-graph-state-foundation` without
  spec promotion. The reviewed archive is pending merge.
- **2026-08-04:** #896 merged the reviewed GS-00 archive at `fe725f82`; GS-00 is
  complete, archived, and on `main`.
- **2026-08-04:** GS-01 opened from `fe725f82` on
  `codex/gs01-authority-recovery` for architecture inventory/design only. No
  GS-01 OpenSpec change or runtime implementation begins before owner acceptance.
  **Superseded/narrowed:** the owner-approved design-only baton permits
  pre-acceptance proposal, design, and task-control artifacts. Runtime-capable
  spec deltas and implementation remain prohibited before owner acceptance.
- **2026-08-04 owner correction:** Prior GS-01 acceptance is revoked/not granted.
  The proposed `GRAPH_INGEST_ACTIVE` duplicated existing `GRAPH_STATUS` and
  graph-ingest territory, so the premise and resulting design do not carry
  forward. The proposed NATS CLI requirement is withdrawn.
- **2026-08-04:** The unapproved, design-only
  `establish-authority-read-and-recovery` OpenSpec change became the durable
  baton. Runtime implementation and spec promotion remain prohibited until a
  fresh inventory and design pass independent reviews and receive owner
  acceptance.
- **2026-08-04:** The corrected fifth-pass GS-01 repository inventory was
  preserved in the design-only baton. Independent review recorded
  `INVENTORY PASS` after confirming the admission, owner-lease,
  lifecycle-history, `GRAPH_STATUS`, and collision-table evidence. The
  troubleshooting guide is recorded as a broader operator expectation rather
  than direct lifecycle-audit evidence. No design is accepted, and no capability
  spec delta or runtime work is authorized.

## Next Action

Have the architect produce GS-01 options, costs, measured premises, adopter-seam
effects, and a recommendation from the reviewed inventory, then stop for
independent pre-owner design review. Add no capability spec delta or runtime work.

[gs00-archive]: ../../openspec/changes/archive/2026-08-04-establish-graph-state-foundation/proposal.md
[gs01-design-baton]: ../../openspec/changes/establish-authority-read-and-recovery/proposal.md
