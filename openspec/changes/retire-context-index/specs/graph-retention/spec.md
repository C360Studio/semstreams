## MODIFIED Requirements

### Requirement: The live graph carries no lifecycle retention

The live graph KV buckets MUST NOT use NATS TTL (`MaxAge`) or a binding `MaxBytes`
as a lifecycle mechanism. This covers every catalog descriptor declared
no-lifecycle-retention — `ENTITY_STATES` and every derived index it owns,
including `PREDICATE_INDEX`, `INCOMING_INDEX`, `OUTGOING_INDEX`, `NAME_INDEX`,
`ALIAS_INDEX`, `SPATIAL_INDEX`, `TEMPORAL_INDEX`,
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
