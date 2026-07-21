# fusion — delta

## ADDED Requirements

### Requirement: Unhydrated seeds are reported, never inferable as absent

The fusion `Response` SHALL report every resolved seed the engine failed to
hydrate, distinct from `Misses`: a miss means resolution found nothing for
the query; an unhydrated entry means resolution produced a seed whose entity
fetch returned nothing or failed. Each entry SHALL carry the seed's handle
and a reason from the closed set `not_found` / `error` / `unknown`.
Reconciliation SHALL use ID-set semantics: the handler's missing report is
authoritative; the client synthesizes `unknown` entries only for requested
IDs in neither the returned set nor the handler's report; exactly one entry
per ID. Hydrated entities SHALL be restored to resolve order before ranking
(ranking is position-based, and the batch handler returns cache-hits-first —
today the resolve-top entity is demoted by cache residency; the
reconciliation fixes and pins this). Nothing in the response — including an
empty unhydrated list — SHALL be interpretable as an authoritative absence
claim in either direction: the list says what was not returned, never what
does not exist, and an entry's `not_found` licenses no deletion.

#### Scenario: A dropped seed is visible

- **GIVEN** resolution ranks an entity first and its authoritative-state
  fetch returns not-found (gh#597's confirmed drop path)
- **WHEN** `Fuse` builds the response
- **THEN** the unhydrated list carries that seed with reason `not_found`, and
  the remaining evidence is still returned in resolve order

#### Scenario: Every seed unhydrated is not a Miss

- **GIVEN** resolution produced seeds and every hydration returns not-found
- **WHEN** `Fuse` builds the response
- **THEN** the response carries unhydrated entries for every seed and
  synthesizes no `Miss` — "resolution found nothing" and "resolution found
  seeds I could not fetch" stay distinguishable at the boundary case

#### Scenario: A mixed-version handler cannot reintroduce silent omission

- **GIVEN** a batch handler that omits IDs without reporting them
- **WHEN** the client receives fewer entities than it requested
- **THEN** it synthesizes `unknown`-reason entries for exactly the IDs in
  neither the returned set nor a handler report — one entry per ID, and a
  handler-reported reason is never overwritten

#### Scenario: The list licenses no absence claim

- **GIVEN** an entity absent from both the nodes and the unhydrated list
- **WHEN** a consumer interprets the response
- **THEN** no deletion or reconciliation may treat that absence as
  authoritative (coherent-view consumers use graph-view-subscription)

### Requirement: A Miss licenses no absence claim

A fusion `Miss` SHALL carry no authoritative-absence meaning: with reads
serving under lag (ADR-084), a just-written, not-yet-indexed entity can
produce a miss with near-match suggestions. A consumer needing to establish
its own write is visible SHALL use the read-your-writes revision check, never
the presence or absence of a miss. Contract documentation that states or
implies "a miss only appears when Ready is true" SHALL be removed.

#### Scenario: A miss under lag is not proof of absence

- **GIVEN** an entity written but not yet indexed, and a healthy index under
  lag
- **WHEN** a `Fuse` query for it returns a miss
- **THEN** no consumer flow may treat the miss as proof the entity does not
  exist (create/dedupe/delete decisions need the revision check)

### Requirement: Resolve scores are observable on request

The engine SHALL carry each seed's resolve rank through assembly, and the
resolve similarity where the resolve mode provides one (semantic resolve
carries a similarity; symbol and prefix resolves do not), joining them to
nodes by entity ID — never by slice position. When the request opts in, each
node SHALL report its resolve rank and, when available, similarity; the
fields SHALL be omitted from the wire when not requested. Lens scoring
internals stay private.

#### Scenario: A ranking surprise is diagnosable on the product surface

- **GIVEN** a consumer investigating an unexpected ranking
- **WHEN** it repeats the request with score observability enabled
- **THEN** each node reports its resolve rank (and similarity for semantic
  resolves), without the consumer bypassing fusion over raw NATS

#### Scenario: The default wire shape is unchanged

- **GIVEN** a request that does not opt in
- **WHEN** the response is serialized
- **THEN** no score or rank field appears on any node
