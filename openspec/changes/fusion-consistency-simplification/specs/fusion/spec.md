# fusion — delta

## ADDED Requirements

### Requirement: Unhydrated seeds are reported, never inferable as absent

The fusion `Response` SHALL report every resolved seed the engine failed to
hydrate, distinct from `Misses`: a miss means resolution found nothing for the
query; an unhydrated entry means resolution produced a seed whose entity fetch
returned nothing or failed. Each entry SHALL carry the seed's handle and a
reason distinguishing not-found from fault; `fusionnats` SHALL reconcile every
batch hydration against the requested ID set and synthesize entries (reason
unknown) for IDs an older handler omitted without explanation. Nothing in the
response — including an empty unhydrated list — SHALL be interpretable as an
authoritative absence claim: the list says what was not returned, never what
does not exist.

#### Scenario: A dropped seed is visible

- **GIVEN** resolution ranks an entity first and its authoritative-state fetch
  returns not-found (gh#597's confirmed drop path)
- **WHEN** `Fuse` builds the response
- **THEN** the response's unhydrated list carries that seed with reason
  not-found, and the remaining evidence is still returned

#### Scenario: A mixed-version handler cannot reintroduce silent omission

- **GIVEN** a batch handler that omits IDs without reporting them
- **WHEN** `fusionnats` receives fewer entities than it requested
- **THEN** the client synthesizes unhydrated entries for the difference

#### Scenario: The list licenses no absence claim

- **GIVEN** an entity absent from both the nodes and the unhydrated list
- **WHEN** a consumer interprets the response
- **THEN** no deletion or reconciliation may treat that absence as
  authoritative (coherent-view consumers use graph-view-subscription)

### Requirement: Resolve scores are observable on request

The engine SHALL carry each seed's resolve similarity and rank through
assembly instead of discarding them, and SHALL expose them per node when the
request opts in; the fields SHALL be omitted from the wire when not requested.
Lens scoring internals stay private — only the resolve-stage similarity and
final rank are exposed.

#### Scenario: A ranking surprise is diagnosable on the product surface

- **GIVEN** a consumer investigating an unexpected ranking
- **WHEN** it repeats the request with score observability enabled
- **THEN** each node reports its resolve similarity and rank, without the
  consumer bypassing fusion over raw NATS

#### Scenario: The default wire shape is unchanged

- **GIVEN** a request that does not opt in
- **WHEN** the response is serialized
- **THEN** no score or rank field appears on any node
