# graph-retention — delta for framework-bucket-catalog

## ADDED Requirements

### Requirement: Framework KV buckets are acquired through a declared descriptor catalog

Every framework-guaranteed KV bucket MUST be declared in exactly one descriptor catalog — name,
owner, class (authoritative | derived | operational | diagnostic), retention policy (a discriminated
Kind with parameters: no-lifecycle-retention | bounded-ttl | unmanaged), write policy (owner-only |
open), create posture (owner-creates | reader-must-exist), and bucket configuration (History,
Replicas) — and every list the framework enforces from (the owned-bucket write-guard set, the
retention backstop set) MUST be a derived view of that catalog, never a parallel hand-maintained
list. Owners acquire their buckets through one seam that creates-or-opens, reconciles the live
bucket to the declared policy (retention per Kind; History to the declared value, warning when an
adopted bucket diverged), verifies by re-read, and fails the owner's start closed on an
unreconcilable divergence or an unknown retention Kind. Readers bind must-exist through a seam that
NEVER creates and never reconciles: an absent bucket yields a classified not-ready error naming the
catalog owner. A reader whose registration outlives the binding moment (e.g. a tool registry built
once per process, before component start) registers its capability unconditionally and resolves the
bucket LAZILY at use time through the same open seam — reporting not-ready until the owner has
provisioned, never skipping itself permanently and never creating. A configuration-supplied KV
bucket name on a framework component's ports MUST
resolve to a catalog descriptor or fail boot naming the unresolved subject — an operator typo may
not silently create a stray unguarded bucket. The catalog covers only buckets whose
write-ownership or retention the framework guarantees; application/product buckets are outside it
by rule, so the catalog cannot grow by accretion. Owner enforcement is by call-site selection
(owners call the ensure seam, readers the open seam) — the framework does not verify caller
identity at runtime; that boundary is review-enforced and stated here deliberately.

#### Scenario: an operator-supplied bucket name must resolve to the catalog

- **GIVEN** a graph-index configuration whose KV output port subject names a bucket absent from
  the descriptor catalog (e.g. a typo of `OUTGOING_INDEX`)
- **WHEN** the component starts
- **THEN** boot fails naming the unresolved subject, rather than silently creating a stray bucket
  that no guard protects and no reader consumes

#### Scenario: reader acquisition never creates

- **GIVEN** a reader (e.g. the graph-query client, or a query tool resolving its bucket lazily at
  execution time) binding a catalog bucket whose owner has not yet provisioned it
- **WHEN** the reader's open-seam bind runs — at first use, not necessarily at registration
- **THEN** it returns a classified not-ready error naming the catalog owner, and the bucket
  remains absent afterwards — a reader can never become an emitter of divergent configuration,
  and a once-per-process registration remains registered (not-ready is a per-use outcome, never a
  permanent registration-time skip)

#### Scenario: retention policy is a per-descriptor fact, not a global rule

- **GIVEN** the seam acquiring `OWNER_PRESENCE` (declared bounded-ttl) and `EMBEDDING_INDEX`
  (declared no-lifecycle-retention), each carrying a `MaxAge`
- **WHEN** each acquisition reconciles
- **THEN** `OWNER_PRESENCE`'s declared TTL is preserved (converged to, never stripped) while
  `EMBEDDING_INDEX`'s foreign TTL is stripped — the same seam, opposite outcomes, driven by the
  descriptor

#### Scenario: an unknown retention Kind fails closed

- **GIVEN** a descriptor carrying a retention Kind the running binary does not know (a newer
  catalog on an older binary)
- **WHEN** the seam validates the spec
- **THEN** acquisition fails with an invalid-policy error rather than silently applying no policy

#### Scenario: adopted configuration divergence is reconciled at acquisition

- **GIVEN** a catalog bucket created earlier by another path with a divergent History (the
  `ENTITY_STATES` History race between graph-ingest and the query-tool registration)
- **WHEN** the owner's ensure-seam acquisition runs
- **THEN** the bucket's History is reconciled to the catalog's declared value with a warning
  naming both values, so bucket configuration is no longer decided by boot order

#### Scenario: enforcement sets derive from the catalog

- **GIVEN** a catalog entry declared write-policy owner-only
- **WHEN** the owned-bucket set is computed and a rule `update_kv` targets that bucket
- **THEN** the entry appears in the derived owned set and the write is rejected at load and at
  runtime — with no hand-maintained list to forget it from

## MODIFIED Requirements

### Requirement: The live graph carries no lifecycle retention

The live graph KV buckets MUST NOT use NATS TTL (`MaxAge`) or a binding `MaxBytes`
as a lifecycle mechanism. This covers every catalog descriptor declared
no-lifecycle-retention — `ENTITY_STATES` and every derived index it owns,
including `PREDICATE_INDEX`, `INCOMING_INDEX`, `OUTGOING_INDEX`, `NAME_INDEX`,
`ALIAS_INDEX`, `CONTEXT_INDEX`, `SPATIAL_INDEX`, `TEMPORAL_INDEX`,
`TEMPORAL_INDEX_REVERSE`, `EMBEDDING_INDEX`, `EMBEDDING_DEDUP`, `COMMUNITY_INDEX`,
`COMMUNITY_SUMMARIES`, `ANOMALY_INDEX`, `STRUCTURAL_INDEX`, `ENTITY_SUFFIX_INDEX`,
the framework operational buckets `GRAPH_INGEST_APPLIED_SEQ` (the ADR-072
redelivery-guard stamps), `GRAPH_STATUS` (the ADR-083 readiness envelopes), and
`OWNER_CLAIMS` — all correctness-critical no-eviction state. Retention is a
per-descriptor policy, not a global rule: `OWNER_PRESENCE` is declared bounded-ttl
(its TTL is the liveness contract and is converged to, never stripped), and
`COMPONENT_STATUS` is declared unmanaged (the framework guarantees no retention
posture for it — an explicit catalog fact, not an omission). Retention is a
semantic operation (ADR-068), never a storage-policy side effect: age/size
eviction is reachability-blind and would drop an entity that still has live
inbound edges. When the guard strips retention it clears ONLY
`MaxAge`/`MaxBytes`; any other backing-stream configuration a bucket legitimately
carries (e.g. `GRAPH_STATUS`'s bounded `History`) is left untouched.

Enforcement is **seam-primary**: every owner acquisition (boot-time or a post-boot
dynamic component add/edit re-acquiring its buckets) creates-or-opens, reconciles
to the declared policy, verifies by re-read, and fails the owner closed on an
unreconcilable divergence — at creation, inside the owner's `Start`, before that
`Start` returns, which composes with the composition root's fail-closed boot. One
**pre-start legacy-drift backstop** remains, and its scope is exactly the class
the seam cannot reach: a catalog bucket whose owner is NOT deployed in this
composition (e.g. an `EMBEDDING_INDEX` left by a prior semantic deploy when
booting a statistical configuration) never has its seam called, so one boot-time
pass over the catalog strips or fails closed on prior-boot/out-of-band dirt for
owner-absent buckets. A backstop bucket that does not exist is skipped (no
creation ordering imposed, no resourceless deploy forced to provision). The
composition root's component-start barrier remains load-bearing for fail-closed
boot; it is no longer load-bearing for retention coverage — the seam holds that
guarantee at each acquisition.

Enforcement scope is **boot-and-acquisition-time**: foreign retention applied to
an owned bucket while the process is already running is not continuously
reconciled; it is picked up at the next acquisition of that bucket (a dynamic
component restart) or the next boot. This matches the ObjectStore precedent's
posture and is sufficient because a foreign TTL only takes semantic effect over
time, and the graph itself never sets one.

#### Scenario: the backstop strips a legacy retention config on an owner-absent bucket and warns

- **GIVEN** a catalog bucket declared no-lifecycle-retention whose owner is not part of this
  composition, carrying a non-zero `MaxAge` or binding `MaxBytes` from a prior deploy
- **WHEN** the pre-start legacy-drift backstop inspects its backing stream
- **THEN** the retention is cleared in place via a stream update and a warning is logged naming
  the bucket and the removed retention
- **AND** no stored key is deleted by the reconciliation

#### Scenario: an owner acquisition reconciles dirt at the seam with no sweep involved

- **GIVEN** a catalog bucket carrying a foreign `MaxAge` (created dirty by a racing process, or
  dirtied out-of-band while the process ran)
- **WHEN** its owner acquires it through the ensure seam — at boot, or on a post-boot dynamic
  component add or edit that re-acquires it
- **THEN** the retention is stripped in place (WARN naming the bucket) or the owner's start fails
  closed if it cannot be, before the owner proceeds — with no boot sweep pass involved

#### Scenario: the retention strip preserves other backing-stream configuration

- **GIVEN** the `GRAPH_STATUS` readiness bucket, created with a bounded `History`
  (`MaxMsgsPerSubject`) and additionally carrying a foreign `MaxAge`
- **WHEN** the seam or backstop strips the retention
- **THEN** the `MaxAge` is cleared but the bucket's `History` is left unchanged, so the
  strip never collaterally shortens readiness replay depth

#### Scenario: acquisition fails closed when retention cannot be stripped

- **GIVEN** a no-lifecycle catalog bucket whose backing stream carries a binding
  `MaxAge`/`MaxBytes` that reconciliation could not clear
- **WHEN** the seam (or backstop) re-asserts the configuration after reconciling
- **THEN** the acquiring owner's start (or boot, for the backstop) fails with a fatal error
  naming the bucket and its offending retention, rather than proceeding

#### Scenario: a clean graph carrying the full owned set boots normally

- **GIVEN** every existing catalog bucket conforms to its declared policy
- **WHEN** the seam acquisitions and the backstop run
- **THEN** every check passes and startup proceeds

### Requirement: Framework-owned buckets reject generic KV writes

A generic KV writer — specifically a rule `update_kv` action — MUST NOT target a bucket
whose catalog descriptor declares write-policy owner-only, and this MUST be enforced both
when a rule pack is loaded and at action execution time; the owned set is the DERIVED
write-policy view of the descriptor catalog, never a parallel list, and the rejection
names the catalog owner. The owned set includes `ENTITY_SUFFIX_INDEX` (graph-ingest),
`GRAPH_INGEST_APPLIED_SEQ` (a forged sequence stamp would make graph-ingest treat a
not-yet-applied event as already applied and silently drop it), and `GRAPH_STATUS` (a
forged envelope would fake "graph is ready" past the fail-closed health gate).

`COMPONENT_STATUS` is deliberately declared write-open (class diagnostic, retention
unmanaged): it has many cross-layer writers and ZERO production readers (only the e2e
harness consumes it), so write-protecting it would guard state nothing reads. Its
posture is a catalog fact; a future operational retention decision for it is a one-line
catalog edit, not a code change.

#### Scenario: a rule update_kv into a framework-owned bucket fails to load

- **GIVEN** a rule pack with an `update_kv` action whose target bucket is a member of
  `FrameworkOwnedBuckets()` (with a literal, non-substituted bucket name)
- **WHEN** the rule configuration is validated at load
- **THEN** validation fails, naming the framework-owned bucket the action may not write

#### Scenario: ENTITY_SUFFIX_INDEX is a framework-owned bucket

- **GIVEN** the framework-owned bucket set
- **WHEN** `ENTITY_SUFFIX_INDEX` is tested against it
- **THEN** it is reported as framework-owned, so a generic `update_kv` targeting it is
  rejected at both load and runtime

#### Scenario: GRAPH_INGEST_APPLIED_SEQ is a framework-owned bucket

- **GIVEN** a rule `update_kv` action targeting `GRAPH_INGEST_APPLIED_SEQ` — with a
  literal bucket name at load, and with a substituted (`$`-resolved) bucket name at
  runtime
- **WHEN** the rule configuration is validated at load, and when the action executes
- **THEN** the write is rejected at both load and runtime, naming the framework-owned
  bucket, so a rule cannot forge a redelivery-guard sequence stamp

#### Scenario: GRAPH_STATUS is a framework-owned bucket

- **GIVEN** a rule `update_kv` action targeting `GRAPH_STATUS` — with a literal bucket
  name at load, and with a substituted (`$`-resolved) bucket name at runtime
- **WHEN** the rule configuration is validated at load, and when the action executes
- **THEN** the write is rejected at both load and runtime, naming the framework-owned
  bucket, so a rule cannot forge a readiness envelope

#### Scenario: a rule update_kv into a non-owned bucket is still permitted

- **GIVEN** a rule pack with an `update_kv` action whose target bucket is not a member
  of `FrameworkOwnedBuckets()` (including the write-open `COMPONENT_STATUS`)
- **WHEN** the rule configuration is validated and the action executes
- **THEN** the write is permitted, so the guard constrains only owner-only buckets
