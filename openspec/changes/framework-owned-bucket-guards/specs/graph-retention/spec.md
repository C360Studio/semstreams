## MODIFIED Requirements

### Requirement: The live graph carries no lifecycle retention

The live graph KV buckets MUST NOT use NATS TTL (`MaxAge`) or a binding `MaxBytes`
as a lifecycle mechanism. This covers the **complete framework-owned bucket set**
(`graph.FrameworkOwnedBuckets()` — `ENTITY_STATES` and every derived index it owns,
including `PREDICATE_INDEX`, `INCOMING_INDEX`, `OUTGOING_INDEX`, `NAME_INDEX`,
`ALIAS_INDEX`, `CONTEXT_INDEX`, `SPATIAL_INDEX`, `TEMPORAL_INDEX`,
`TEMPORAL_INDEX_REVERSE`, `EMBEDDING_INDEX`, `EMBEDDING_DEDUP`, `COMMUNITY_INDEX`,
`COMMUNITY_SUMMARIES`, `ANOMALY_INDEX`, `STRUCTURAL_INDEX`, `ENTITY_SUFFIX_INDEX`,
plus the two framework operational buckets `GRAPH_INGEST_APPLIED_SEQ` (the ADR-072
redelivery-guard stamps) and `GRAPH_STATUS` (the ADR-083 readiness envelopes) — both
correctness-critical no-eviction state), **excluding the one rebuildable cache
`EMBEDDINGS_CACHE`** — whose capacity bounding is owned by a separate storage-limits
capability, not by this retention guard. Retention is a semantic operation (ADR-068),
never a storage-policy side effect: age/size eviction is reachability-blind and would
drop an entity that still has live inbound edges. When the guard strips retention it
clears ONLY `MaxAge`/`MaxBytes`; any other backing-stream configuration a bucket
legitimately carries (e.g. `GRAPH_STATUS`'s bounded `History`) is left untouched.

Enforcement is boot-time and self-healing, and **covers the full owned set through a
two-pass sweep** (not only the buckets a single component happens to create). On each
pass, every guarded bucket that exists is inspected via its backing stream
(`KV_<bucket>`); any binding `MaxAge`/`MaxBytes` is stripped in place and logged
(covering legacy buckets a create-or-get path would otherwise never reconcile), then
re-asserted against the shared no-lifecycle-retention predicate — if retention is still
binding, startup fails closed rather than proceeding to silently expire graph state. A
guarded bucket that does not yet exist is skipped (its true owner creates it clean), so
the sweep imposes no bucket-creation ordering and never forces a resourceless deploy to
provision a tier-gated bucket. The two passes run at fixed boot seams: a **pre-start
belt** before component start (takes down prior-boot / out-of-band dirt early) and a
**post-start coverage pass** after every owning component has created its bucket and
before the process reports healthy (catches a bucket created dirty during this boot's
own startup — a create-race — that the pre-start belt necessarily skipped as absent).

Enforcement scope is **boot-time**: foreign retention applied to an owned bucket while
the process is already running is not continuously reconciled; it is picked up at the
next boot's sweep. This matches the ObjectStore precedent's boot-time posture and is
sufficient because a foreign TTL only takes semantic effect over time, and the graph
itself never sets one.

#### Scenario: No component defaults a shared graph bucket to a TTL

- **GIVEN** the graph-query client builds its default KV configuration
- **WHEN** `DefaultConfig()` is constructed
- **THEN** the `ENTITY_STATES`, `SPATIAL_INDEX`, and `INCOMING_INDEX` bucket TTLs
  are `0` (no expiry)

#### Scenario: boot strips a legacy retention config on any owned bucket and warns

- **GIVEN** a framework-owned bucket other than `ENTITY_STATES` — e.g. `EMBEDDING_INDEX`
  or `COMMUNITY_INDEX` — whose backing stream already carries a non-zero `MaxAge` or a
  binding `MaxBytes` (e.g. because another process won the get-or-create race with a
  retention config, as in #610/#611)
- **WHEN** the boot-time owned-bucket sweep inspects that bucket's backing-stream config
- **THEN** the retention is cleared in place via a stream update and a warning is logged
  naming the bucket and the removed retention
- **AND** no stored key is deleted by the reconciliation

#### Scenario: boot reconciles a bucket created dirty during startup (create-race)

- **GIVEN** a framework-owned bucket that does not yet exist when the pre-start belt runs
  (so the belt skips it), and which is then created carrying a foreign `MaxAge`/`MaxBytes`
  during this boot's own startup — because a component's get-or-create adopts a
  bucket a racing process created dirty, unchanged
- **WHEN** the post-start coverage pass inspects the owned set after component start and
  before the process reports healthy
- **THEN** the create-race retention is stripped in place, a warning is logged naming the
  bucket, and no stored key is deleted — the two-pass model closes the window the
  pre-start belt alone cannot see

#### Scenario: the retention strip preserves other backing-stream configuration

- **GIVEN** the `GRAPH_STATUS` readiness bucket, created with a bounded `History`
  (`MaxMsgsPerSubject`) and additionally carrying a foreign `MaxAge`
- **WHEN** the boot-time sweep strips the retention
- **THEN** the `MaxAge` is cleared but the bucket's `History` is left unchanged, so the
  strip never collaterally shortens readiness replay depth

#### Scenario: boot fails closed when retention cannot be stripped

- **GIVEN** a framework-owned bucket whose backing stream carries a binding
  `MaxAge`/`MaxBytes` that the reconciliation could not clear
- **WHEN** the sweep re-asserts the backing-stream configuration after reconciliation
- **THEN** startup fails with a fatal error naming the bucket and its offending
  retention, rather than proceeding

#### Scenario: the rebuildable cache is excluded from the retention sweep

- **GIVEN** the `EMBEDDINGS_CACHE` bucket
- **WHEN** the boot-time owned-bucket retention sweep runs
- **THEN** `EMBEDDINGS_CACHE` is not asserted retention-free by this guard (its capacity
  policy is owned elsewhere), while it remains a member of the write-ownership–protected
  set

#### Scenario: a clean graph carrying the full owned set boots normally

- **GIVEN** every existing framework-owned bucket has `MaxAge` `0` and no binding
  `MaxBytes`
- **WHEN** the boot-time sweep inspects them
- **THEN** the guardrail passes for every guarded bucket and startup proceeds

#### Scenario: graph-ingest retains a create-time retention refusal for its authoritative bucket

- **GIVEN** the boot-time sweep runs before graph components create their buckets, so it
  cannot observe a retention config applied to `ENTITY_STATES` during this boot's own
  component start (a narrow create-time race another process wins)
- **WHEN** graph-ingest creates or opens `ENTITY_STATES` and inspects its backing-stream
  config
- **THEN** graph-ingest's own create-time guard still refuses to boot if the bucket
  carries a binding `MaxAge`/`MaxBytes`, covering the window the sweep cannot — a
  refusal that remains consistent with failing closed on unremediable retention

## ADDED Requirements

### Requirement: Framework-owned buckets reject generic KV writes

A generic KV writer — specifically a rule `update_kv` action — MUST NOT target a bucket
enumerated by `graph.FrameworkOwnedBuckets()`, which are written exclusively by their
owning graph components, and this MUST be enforced both when a rule pack is loaded and
at action execution time. The owned set MUST include `ENTITY_SUFFIX_INDEX`, which the
graph-ingest component creates and owns; prior to this change it was absent from the set
and therefore writable by a generic `update_kv`, which this requirement closes.

The owned set MUST additionally include two framework operational buckets that were
likewise writable by a generic `update_kv` before this change:

- `GRAPH_INGEST_APPLIED_SEQ` — graph-ingest's ADR-072 redelivery-guard durable tier
  (`(entityID/streamName) → last-applied stream sequence`). A forged sequence stamp
  would make graph-ingest treat a not-yet-applied event as already applied and silently
  drop it, reopening the restart/cache-eviction overwrite the guard closes.
- `GRAPH_STATUS` — the ADR-083 readiness distribution bucket (producers' liveness
  envelopes). A forged envelope would let a rule fake "graph is ready" and defeat the
  health gate that fails closed on a stale or absent reading.

`COMPONENT_STATUS` is deliberately NOT added by this change (it is a cross-layer,
many-writer operational bucket with a different retention/ownership posture); it is
tracked as a separate follow-up.

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
  of `FrameworkOwnedBuckets()`
- **WHEN** the rule configuration is validated and the action executes
- **THEN** the write is permitted, so the guard constrains only framework-owned buckets
