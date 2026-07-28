# graph-retention — delta for reopen-framework-owned-bucket-guards

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
correctness-critical no-eviction state). The retention sweep guards the owned set with
**no exceptions**: the formerly excluded `EMBEDDINGS_CACHE` bucket is deleted outright —
it was a created-but-never-read-or-written dead surface, and no framework bucket may
exist solely to carry a guard exemption. Retention is a semantic operation (ADR-068),
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
**post-start coverage pass** whose ordering is provided by the composition root's
**component-start barrier**: `Manager.StartAll` does not run the pass until every
lifecycle component's `Start` call has returned (successfully or not), so every owning
component holds its bucket handle — or has failed boot — before the pass ranges the
set, and it runs before the HTTP surface comes up. This catches a bucket created dirty
during this boot's own startup (a create-race) that the pre-start belt necessarily
skipped as absent. The barrier is load-bearing for the guarantee: without it the
post-start pass races component startup and the create-race window silently reopens.

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
- **WHEN** the post-start coverage pass runs — after the component-start barrier has
  observed every lifecycle component's `Start` return, and before the process reports
  healthy
- **THEN** the create-race retention is stripped in place, a warning is logged naming the
  bucket, and no stored key is deleted — the barrier guarantees the pass cannot range the
  owned set before the adopting component holds its handle

#### Scenario: the post-start pass is driven through the production component wire

- **GIVEN** the integration test locking the create-race guarantee
- **WHEN** it arranges the dirty-bucket adoption
- **THEN** the adopting component starts through the real asynchronous `ComponentManager`
  launch path (the production concurrency shape), not a synchronous stand-in — a
  synchronous mock would prove an ordering the production boot does not provide

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

#### Scenario: a clean graph carrying the full owned set boots normally

- **GIVEN** every existing framework-owned bucket has `MaxAge` `0` and no binding
  `MaxBytes`
- **WHEN** the boot-time sweep inspects them
- **THEN** the guardrail passes for every guarded bucket and startup proceeds

#### Scenario: graph-ingest retains a create-time retention refusal for its authoritative bucket

- **GIVEN** the pre-start belt runs before graph components create their buckets, so it
  cannot observe a retention config applied to `ENTITY_STATES` during this boot's own
  component start (a narrow create-time race another process wins)
- **WHEN** graph-ingest creates or opens `ENTITY_STATES` and inspects its backing-stream
  config
- **THEN** graph-ingest's own create-time guard refuses to start if the bucket carries a
  binding `MaxAge`/`MaxBytes`
- **AND** that `Start` error fails boot at the process level via the component-start
  barrier (the process exits rather than serving HTTP with an expiring authoritative
  bucket) — the refusal is no longer swallowed by a fire-and-forget component launch
